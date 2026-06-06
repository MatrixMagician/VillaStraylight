---
phase: 07-rocm-render-unit-preflight-detect
reviewed: 2026-06-06T00:00:00Z
depth: deep
files_reviewed: 19
files_reviewed_list:
  - cmd/villa/detect_test.go
  - cmd/villa/preflight.go
  - cmd/villa/preflight_test.go
  - cmd/villa/testdata/detect.golden.json
  - internal/detect/detect.go
  - internal/detect/gpu_amd.go
  - internal/detect/profile.go
  - internal/detect/profile_test.go
  - internal/detect/readiness_rocm.go
  - internal/detect/readiness_rocm_test.go
  - internal/orchestrate/parseargs_test.go
  - internal/orchestrate/quadlet/container.tmpl
  - internal/orchestrate/render.go
  - internal/orchestrate/render_test.go
  - internal/orchestrate/testdata/villa-llama-rocm.container.golden
  - internal/preflight/checks_rocm.go
  - internal/preflight/checks_rocm_test.go
  - internal/preflight/floors.go
  - internal/preflight/policy_test.go
  - internal/preflight/rocm-policy.json
findings:
  critical: 0
  warning: 4
  info: 4
  total: 8
status: issues_found
---

# Phase 7: Code Review Report

**Reviewed:** 2026-06-06
**Depth:** deep
**Files Reviewed:** 19 (plus cross-file trace into `internal/inference/backend_rocm.go`, `backend_vulkan.go`, `backend.go`, `internal/detect/value.go`, `cmd/villa/root.go`)
**Status:** issues_found

## Summary

I reviewed the ROCm render/preflight/detect slice adversarially against the stated
invariants and traced the call chains across the `orchestrate → inference` and
`preflight/detect` module boundaries.

The phase's core invariants HOLD and I verified each one directly:

- **Vulkan container golden is byte-identical.** `git diff` of
  `villa-llama.container.golden` against the base commit is empty; the template
  change is purely additive (`{{range}}` blocks + `{{.BackendLabel}}`), and the
  Vulkan zero-env/single-device path still renders the exact prior bytes.
- **HostProfile v1 contract is append-only.** `rocm_readiness` is inserted before
  `schema_version` (the documented last field); no v1 field was reordered or
  retyped; schema bumped 1→2. The golden JSON confirms field order.
- **Off-hardware-undetectable signals serialize UNSET, never false-green.**
  `hsa_override_viable` / `firmware_date_ok` / `rocminfo_gfx1151` all emit
  `known:false`; the ROCm preflight gate degrades every Unknown signal to WARN and
  off-hardware maps to exit 2, never exit 1. Verified by running the suites (all green).
- **ROCm image is digest-pinned `kyuz0:rocm-7.2.4`, never a nightly.** The pin lives
  in `backend_rocm.go`; the policy denylist (`rocm7-nightlies`) is enforced in both
  the detect readiness scorer and the preflight image check.

No blockers found. The findings below are robustness/maintainability gaps and one
genuine latent correctness trap in the arg parser that future backends will hit.

All test suites in scope pass (`go test ./internal/orchestrate/... ./internal/detect/...
./internal/preflight/... ./cmd/villa/...` → ok).

## Warnings

### WR-01: `parseContainerArgs` silently drops a second `--security-opt` (last-wins overwrite)

**File:** `internal/orchestrate/render.go:202-205`
**Issue:** The `--device`, `--group-add`, and `--env` cases correctly `append` to
slices, so multiple occurrences are preserved (and the ROCm tests assert this). But
the `--security-opt` case ASSIGNS rather than appends:

```go
case flSecOpt:
    if i+1 < len(flags) {
        cv.PodmanArgs = flSecOpt + " " + flags[i+1]   // overwrite — last wins
        i++
    }
```

`cv.PodmanArgs` is a single string and the template renders exactly one
`PodmanArgs=` line. If any current or future backend's `ContainerArgs` emits two
`--security-opt` tokens (a very plausible ROCm/AMDVLK hardening or
`label=disable` addition), only the LAST survives and the first is silently dropped
— exactly the Pitfall-1 "silent drop" class this phase's env/group handling was
written to prevent. The asymmetry (slices for device/group/env, scalar overwrite for
security-opt) is a latent trap, not a present-day bug: both shipped backends emit a
single `--security-opt`.

**Fix:** Accumulate security opts the same way as the other multi-value flags and
join them into a Quadlet-legal form (Quadlet `PodmanArgs=` accepts the full token
string), or render one `PodmanArgs=` line per opt:

```go
var secOpts []string
// in the loop:
case flSecOpt:
    if i+1 < len(flags) {
        secOpts = append(secOpts, flSecOpt+" "+flags[i+1])
        i++
    }
// after the loop:
cv.PodmanArgs = strings.Join(secOpts, " ")
```

At minimum, add a parse-time guard that errors if more than one `--security-opt` is
seen while `cv.PodmanArgs` remains a scalar, so the silent-drop becomes a loud failure.

### WR-02: `checkROCmFirmware` never compares against `FirmwareFloor` — an old-but-not-denied firmware passes the gate

**File:** `internal/preflight/checks_rocm.go:116-139`
**Issue:** The firmware check only does exact-match membership against
`pol.FirmwareDeny` (`["20251125"]`). `pol.FirmwareFloor` (`20260110`) is loaded,
embedded, named in the remediation string — but never actually compared. A host with
a Known firmware date that is OLDER than the floor yet NOT the single denied build
(e.g. `20251201`) returns `StatusPass` ("firmware 20251201 is not on the denylist"),
asserting readiness it has not earned. The denylist catches exactly one known-bad
point release; everything below the floor slips through as a confident PASS.

This is partially mitigated by the gate being a refuse-with-remediation BLOCK that
only FAILs on positively-known-bad (so it correctly does not over-block), but emitting
PASS for a sub-floor firmware is a false-green on the one axis (`FirmwareFloor`) the
policy explicitly carries. The detect-side mirror `firmwareDateOK()` sidesteps this by
staying permanently UNSET — but here a Known sub-floor value is actively mis-scored.

**Fix:** Add a floor comparison before the PASS, treating sub-floor (but not denied) as
a WARN advisory rather than a PASS (consistent with the WARN-tier floors elsewhere):

```go
if compareVersions(fw.Value, floor) < 0 {
    return warn(idROCmFirmware, name, TierBlock,
        fmt.Sprintf("firmware %s is below the recommended %s floor", fw.Value, floor),
        remediation, fw.Source, fw.Raw)
}
```

(Note `compareVersions` orders YYYYMMDD date stamps correctly — verified `20251125 <
20260110`.) If a sub-floor PASS is intentional, document that `FirmwareFloor` is
advisory-only in remediation and not a gate, so the next reader does not assume it is
enforced.

### WR-03: `--env` token without `=` renders a malformed `Environment=<KEY>=` line

**File:** `internal/orchestrate/render.go:194-201`
**Issue:** The env case uses `strings.Cut(flags[i+1], "=")` and ignores the `ok`
return:

```go
k, v, _ := strings.Cut(flags[i+1], "=")
cv.Env = append(cv.Env, envPair{Key: k, Value: v})
```

If a backend ever passes a bare `--env FOO` (no `=`, i.e. "inherit from host
environment", which is legal `podman run` syntax), `Cut` returns `k="FOO", v="",
ok=false`. The renderer then emits `Environment=FOO=`, which is a DIFFERENT semantic
(set FOO to empty) than the intended host-inheritance, and silently. Both shipped
backends always use `KEY=VALUE`, so this is latent, but the defensive
all-fields-non-empty check at line 228 does NOT cover env (env is intentionally
unchecked), so nothing catches it.

**Fix:** Branch on the `ok` return and either skip/normalize the no-`=` form or reject
it explicitly:

```go
k, v, ok := strings.Cut(flags[i+1], "=")
if !ok {
    return containerView{}, fmt.Errorf("orchestrate: --env %q has no '=' (host-inherit form unsupported)", flags[i+1])
}
cv.Env = append(cv.Env, envPair{Key: k, Value: v})
```

### WR-04: `RunE` calls `os.Exit` directly, bypassing cobra's deferred teardown and making the seam harder to test end-to-end

**File:** `cmd/villa/preflight.go:55-57`
**Issue:**

```go
code := renderPreflight(cmd.OutOrStdout(), results, jsonOut, verbose, force)
os.Exit(code)
return nil   // unreachable
```

Calling `os.Exit` inside `RunE` means the `return nil` is dead, any deferred cleanup
registered up the cobra chain never runs, and the command can only be exercised
end-to-end via a subprocess. The team already split out `renderPreflight` precisely so
the code mapping is testable WITHOUT `os.Exit` — but the `Probe()`→`Run`/`RunROCm`→exit
wiring in `RunE` itself remains untested except through the global. Today this is benign
(no deferred resources), but it is fragile: if a future `Probe()` opens a file/socket
that needs closing, `os.Exit` will leak it.

**Fix:** Have `RunE` stash the code and let a `PersistentPostRunE` / a small wrapper at
the `main` entrypoint perform the single `os.Exit`, or return a typed error the root
command translates to an exit code. Keeps cobra's lifecycle intact and makes the full
`RunE` path unit-testable.

## Info

### IN-01: `splitVersion` doc comment overstates the result (`[6 18 9]` vs actual `[6 18 9 0 0]`)

**File:** `internal/preflight/floors.go:171-172` (and the mirror
`internal/detect/gpu_amd.go:338-339`)
**Issue:** Both comments claim `splitVersion("6.18.9-300.fc44.x86_64")` → `[6 18 9]`.
The actual output is `[6 18 9 0 0]` (the `-300`, `fc44`, `x86_64` dotted segments each
contribute a leading-`0` entry). This is behaviorally harmless for floor comparison
(trailing zeros do not change ordering against a 3-segment floor, verified), and the
two splitters DO agree with each other (no cross-package divergence) — but the comment
misleads the next maintainer into thinking suffix segments are dropped entirely.
**Fix:** Correct the comment to `[6 18 9 0 0]`, or note that suffix dotted-segments
collapse to `0` and are order-neutral.

### IN-02: `firmwareDateOK()` is permanently unwired and will read UNSET forever

**File:** `internal/detect/readiness_rocm.go:56-58`
**Issue:** `firmwareDateOK()` unconditionally returns `UnknownBool(...)`. This is the
documented, intentional no-false-green stub (firmware date is not probed in the detect
path). It is correct for this phase, but it is dead-by-design: there is no probe seam
threaded toward it, so a future contributor wiring real firmware detection must hunt
for the call site. **Fix:** None required now; consider a `// TODO(phaseN): wire to a
real firmware-date probe` so the placeholder is discoverable, matching the
`TODO(phase-2)` convention already used in `floors.go:34`.

### IN-03: `MesaFloor` remains loaded, embedded, and entirely unconsumed (carried dead policy data)

**File:** `internal/preflight/floors.go:42`, `rocm-policy.json:4`
**Issue:** `MesaFloor` / `pol.MesaFloor` is migrated into the embedded policy and
asserted by `policy_test.go`, but no check consumes it (the `TODO(phase-2)` at
`floors.go:34` explains why — a driverVersion-vs-release numbering mismatch). It is
documented dead data, not a bug, but it adds a tested-yet-inert field that can read as
"this floor is enforced" to a casual reader. **Fix:** Keep as-is (the rationale is
sound and well-documented); no action needed beyond the existing TODO.

### IN-04: ROCm preflight `pass`/`warn`/`fail` for unprobed signals are indistinguishable from genuinely-evaluated ones in the table

**File:** `internal/preflight/checks_rocm.go:116-162`
**Issue:** `checkROCmFirmware` and `checkROCmHSA` source typed-Unknown values that are
NOT HostProfile fields (firmware date, HSA env are never probed in v1.0), so
off-hardware they ALWAYS WARN regardless of the host. The remediation text is good, but
the rendered row reads identically to a signal that was actually evaluated and found
unknown. A user cannot tell "we checked and could not read it" from "this is structurally
never probed yet." **Fix:** Optional — distinguish "not yet probed in this version" in the
WARN detail (e.g. a `(not probed in v1.0)` suffix) so the table communicates that re-running
on real hardware will not change these two rows until the probe exists.

---

_Reviewed: 2026-06-06_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
