# Phase 11: Address v1.1 tech debt — rocm_readiness detect probes + doc reconciliation - Pattern Map

**Mapped:** 2026-06-06
**Files analyzed:** 4 code (3 modified, 0 new) + 1 unchanged-by-design + 8 doc (non-code)
**Analogs found:** 4 / 4 (every code file has an in-tree analog; all in `internal/detect`)

> This phase replicates existing `internal/detect` idioms — it adds NO new architecture.
> The closest analog for every change lives in the SAME package (`gpu_amd.go` /
> `readiness_rocm.go`). The planner should copy these patterns near-verbatim.

## File Classification

| Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---------------|------|-----------|----------------|---------------|
| `internal/detect/gpu_amd.go` | backend-seam (detect) | host-probe (exec) + pure transform | `igpuGfxID`/`runTool` (probe), `kernelMeetsROCmFloor`/`compareVersionSegments` (policy compare) — same file | exact (same file, same idioms) |
| `internal/detect/readiness_rocm.go` | readiness-compute (pure fold) | transform | `kernelFloorOK` / `rocminfoGfx1151` — same file | exact |
| `internal/detect/detect.go` | orchestrator (Probe) | request-response (compose-and-return) | existing `computeROCmReadiness` call site (line 36) | exact (1-line signature extension) |
| `internal/detect/readiness_rocm_test.go` | test (table) | transform-under-test | `TestKernelFloorKnownBelowFloor` / `TestKernelFloorUnknownKernel` / `TestImagePolicyOK` — same file | exact |
| `internal/detect/profile.go` | model (struct) | — | n/a — UNCHANGED (fields already exist, schema stays 2) | n/a |
| `cmd/villa/testdata/detect.golden.json` | fixture | — | n/a — UNCHANGED byte-identical (fixture-driven, D-04) | n/a |

**Doc-only (non-code, no analog needed) — see Shared Patterns / Doc Reconciliation:**
6 SUMMARY frontmatters, 1 REVIEW prose line, 1 REQUIREMENTS.md note (the last is an Open Question — verify before editing).

---

## Pattern Assignments

### `internal/detect/gpu_amd.go` (backend-seam) — ADD firmware probe + policy seam

This file is the ONLY file in `internal/detect` allowed to carry ROCm/firmware/kernel
literals (line 12-15 header comment). Both new helpers land HERE so `readiness_rocm.go`
stays literal-free (D-02). There are TWO distinct analog shapes to copy:

**Analog A — fixed-arg host probe** (`igpuGfxID`, lines 424-430; `runTool`, lines 239-250):
```go
// runTool — the fixed-arg, bounded, LookPath-guarded exec primitive to reuse verbatim.
func runTool(name string, args ...string) (out string, ok bool) {
	if _, err := exec.LookPath(name); err != nil {
		return "", false
	}
	cmd := exec.Command(name, args...) // fixed args; no shell interpolation
	raw, err := cmd.Output()
	bounded := raw
	if len(bounded) > maxToolOutput {
		bounded = bounded[:maxToolOutput]
	}
	return string(bounded), err == nil
}

// igpuGfxID — the probe-shape to mirror: runTool → ok-guard → UnknownStr on miss → parse.
func igpuGfxID() Str {
	out, ok := runTool("rocminfo")
	if !ok {
		return UnknownStr("rocminfo unavailable (gfx id not enumerated)", "")
	}
	return parseGfxID(out)
}
```
**New `firmwareDateProbe()` copies this shape exactly** — `runTool("rpm", "-q", "--qf", "%{VERSION}", "linux-firmware")` (fixed args, per D-01), `!ok → UnknownStr`, then a `YYYYMMDD` parse guard (len==8 + all-digit) before returning `KnownStr`; an unparseable stamp → `UnknownStr(reason, capRaw(out))` (no-false-green). Use `capRaw` (lines 252-258) for the raw field.

**Analog B — policy-literal seam + numeric compare** (`kernelMeetsROCmFloor` + const, lines 293-307; `compareVersionSegments`, lines 314-336):
```go
// The kernel-floor precedent: literal const lives HERE, compare reuses compareVersionSegments.
const rocmKernelFloorTarget = "6.18.4"

func kernelMeetsROCmFloor(kernelVersion string) bool {
	return compareVersionSegments(kernelVersion, rocmKernelFloorTarget) >= 0
}
```
**New `firmwareDatePolicyOK(date string) bool` copies this** — two new consts beside the kernel one (`rocmFirmwareFloor = "20260110"`, `rocmFirmwareDeny = []string{"20251125"}` — DUPLICATE the values, do NOT import preflight; preflight→detect already exists so the reverse is a cycle, see Shared Patterns). FAIL semantics (D-02): denylist match → `false`; `compareVersionSegments(date, rocmFirmwareFloor) < 0` → `false`; else `true`. Reuse `compareVersionSegments` (suffix-tolerant, panic-free) — do NOT roll a new comparator.

> Policy values verified against `internal/preflight/rocm-policy.json:2,5,6,8`:
> `kernelFloor 6.18.4`, `firmwareFloor 20260110`, `firmwareDeny ["20251125"]`, `requiredHSAOverride 11.5.1`.

**Image-policy verdict precedent** (`rocmImagePolicyOK`, lines 282-291) — the denylist-then-floor verdict ordering to mirror:
```go
func rocmImagePolicyOK(image string) Bool {
	switch {
	case strings.Contains(image, rocmNightlyDenyTag):
		return KnownBool(false, "denied ROCm nightly image (64 GB allocation cap)")
	case strings.Contains(image, rocmStableImageTag):
		return KnownBool(true, "pinned stable ROCm image")
	default:
		return UnknownBool("resolved ROCm image not recognized by pin policy", image)
	}
}
```
Note: `firmwareDatePolicyOK` returns a bare `bool` (the floor/deny logic), like `kernelMeetsROCmFloor` — the `Bool` wrapping happens in `readiness_rocm.go`'s `firmwareDateOK`. Keep the literal-bearing helper returning `bool`; keep the typed-Optional wrapping in the readiness file.

---

### `internal/detect/readiness_rocm.go` (readiness-compute, pure fold) — make 2 stubs real

**Analog** (same file): `kernelFloorOK` (lines 45-50) + `rocminfoGfx1151` (lines 34-39) — the exact `!Known → UnknownBool` / else `KnownBool(seam(...))` template:
```go
func kernelFloorOK(kernel Str) Bool {
	if !kernel.Known {
		return UnknownBool("kernel version unknown (floor unevaluable)", kernel.Raw)
	}
	return KnownBool(kernelMeetsROCmFloor(kernel.Value), "kernel >= gfx1151 floor")
}

func rocminfoGfx1151(gfxID Str) Bool {
	if !gfxID.Known {
		return UnknownBool("rocminfo gfx id not enumerated (rocm readiness unevaluable)", gfxID.Raw)
	}
	return KnownBool(gfxID.Value == "gfx1151", "rocminfo:Name")
}
```

**`firmwareDateOK(date Str) Bool`** — replace the stub (current lines 52-58). Mirror `kernelFloorOK` exactly: gate on `date.Known`, else `UnknownBool`; else `KnownBool(firmwareDatePolicyOK(date.Value), "firmware date vs floor/denylist")`. The policy literal stays behind the `gpu_amd.go` seam — NO firmware version string in this file (D-02).

**`hsaOverrideViable(gfxID Str, rocmPresent Bool) Bool`** — replace the stub (current lines 60-65). Pure derivation, NO I/O, NO `os.Getenv` (D-03 / Pitfall 4). Gate Known-ness on `gfxID.Known` (per Assumptions A1: `rocmPresent` is ALWAYS Known so gating on it alone would break off-hardware UNSET):
```go
func hsaOverrideViable(gfxID Str, rocmPresent Bool) Bool {
	if !gfxID.Known {
		return UnknownBool("HSA override viability unevaluable (gfx id not enumerated)", gfxID.Raw)
	}
	return KnownBool(gfxID.Value == "gfx1151" && rocmPresent.Value, "gfx1151 + rocm substrate present (HSA 11.5.1 applies)")
}
```

**Thread the new source facts in `computeROCmReadiness`** (current lines 20-28) — extend the signature, keeping the other 3 fields untouched:
```go
// CURRENT: computeROCmReadiness(gfxID Str, kernel Str, resolvedImage string)
// NEW:     computeROCmReadiness(gfxID Str, kernel Str, rocmPresent Bool, firmwareDate Str, resolvedImage string)
//   HSAOverrideViable: hsaOverrideViable(gfxID, rocmPresent),
//   FirmwareDateOK:    firmwareDateOK(firmwareDate),
//   (KernelFloorOK / RocminfoGfx1151 / ImagePolicyOK unchanged)
```
Also update the now-inaccurate stub doc comments (lines 52-65) to describe the live probe + off-hardware UNSET path.

---

### `internal/detect/detect.go` (orchestrator) — thread live facts into the call

**Analog:** the existing single call site (line 36):
```go
// CURRENT:
rocmReadiness := computeROCmReadiness(gpu.gfxID, kernel, resolvedROCmImage())
// NEW (pass the already-probed gpu.rocmPresent + the new firmwareDateProbe()):
rocmReadiness := computeROCmReadiness(gpu.gfxID, kernel, gpu.rocmPresent, firmwareDateProbe(), resolvedROCmImage())
```
`gpu.rocmPresent` already exists on `gpuInfo` (gpu_amd.go:39, populated by `probeGPU` at line 53). The ONLY new call is `firmwareDateProbe()`. Keep ALL I/O in gpu_amd.go (probe) — detect.go just wires it, matching how it already passes `gpu.gfxID` / `resolvedROCmImage()`.

---

### `internal/detect/readiness_rocm_test.go` (test, table) — add 2 tests, extend 1

**Analog** (same file): the existing Known-bad / Unknown / good-vs-bad triplet to copy:
```go
// TestKernelFloorKnownBelowFloor — the "Known input → confident KnownBool(false)" shape.
func TestKernelFloorKnownBelowFloor(t *testing.T) {
	r := computeROCmReadiness(
		UnknownStr("rocminfo unavailable", ""),
		KnownStr("6.17.0-100.fc44.x86_64", "/proc/sys/kernel/osrelease"),
		resolvedROCmImage(),
	)
	if !r.KernelFloorOK.Known { t.Fatalf(...) }
	if r.KernelFloorOK.Value { t.Errorf("should be false for 6.17.0 (< 6.18.4 floor)") }
}
```

**Add `TestFirmwareDateOK`** (table) — call `firmwareDateOK(...)` directly:
- `KnownStr("20260519")` (clear) → `Known && Value` (true)
- `KnownStr("20260110")` (== floor) → `Known(true)`
- `KnownStr("20251125")` (denylist) → `Known && !Value` (false)
- `KnownStr("20251231")` (sub-floor, not denied) → `Known(false)`
- `UnknownStr(...)` → `!Known` (UNSET)

**Add `TestHSAOverrideViable`** (table) — call `hsaOverrideViable(...)` directly:
- `KnownStr("gfx1151")` + `KnownBool(true,...)` → `Known(true)`
- `KnownStr("gfx1100")` + `KnownBool(true,...)` → `Known(false)` (non-gfx1151)
- `KnownStr("gfx1151")` + `KnownBool(false,...)` (no substrate) → `Known(false)`
- `UnknownStr(...)` gfx-id → `!Known` (UNSET, no-false-green)

**Extend `TestComputeROCmReadinessOffHardware`** (lines 9-30) — its existing assertions (`FirmwareDateOK` / `HSAOverrideViable` should be UNSET) MUST still pass with Unknown source facts. Update the call to the new signature, passing `rocmPresent` and an `UnknownStr` firmware date so both stay UNSET (no-false-green regression guard, D-06):
```go
r := computeROCmReadiness(
	UnknownStr("rocminfo unavailable (gfx id not enumerated)", ""), // gfxID Unknown
	KnownStr("7.0.10-201.fc44.x86_64", "/proc/sys/kernel/osrelease"),
	rocmPresent(), // or KnownBool(false,...) — always Known; gfxID gates UNSET
	UnknownStr("firmware date not probed (test)", ""),
	resolvedROCmImage(),
)
// existing assertions unchanged: FirmwareDateOK.Known==false, HSAOverrideViable.Known==false
```
NOTE: the OTHER existing tests in this file (`TestKernelFloorKnownBelowFloor:35`, `TestKernelFloorUnknownKernel:51`, `TestImagePolicyOK:69,74`) ALSO call `computeROCmReadiness` with the 3-arg signature — all must be updated to the new 5-arg signature (add a `rocmPresent` Bool + an `UnknownStr` firmware date). This is mechanical but mandatory or the package won't compile.

---

## Shared Patterns

### No-false-green / typed-Optional `Bool` (D-08 — the core invariant)
**Source:** `internal/detect/value.go:63-74` (`Bool` + `KnownBool`/`UnknownBool`)
**Apply to:** every probe leaf in this phase.
```go
func KnownBool(v bool, src string) Bool { return Bool{Value: v, Known: true, Source: src} }
func UnknownBool(reason, raw string) Bool { return Bool{Known: false, Source: reason, Raw: raw} }
```
Rule: a field is `KnownBool` ONLY when its source fact is `Known`; any Unknown source → `UnknownBool` (serializes UNSET). This is what keeps the golden byte-identical off-hardware and flips the badge to `ready` on-hardware with zero consumer change.

### Detect-local literal seam (no detect→preflight import — would be a cycle)
**Source:** `internal/detect/gpu_amd.go:293-307` (kernel-floor const + helper); header comment lines 12-15.
**Apply to:** the firmware floor/deny values.
**Verified:** `internal/preflight` imports `internal/detect`; therefore detect importing preflight is a forbidden cycle. DUPLICATE the two firmware values detect-side (established precedent: kernel floor + image tag are already duplicated). Keep them in `gpu_amd.go`, never in `readiness_rocm.go`.
> Caveat (Pitfall 3): `TestSeamGrepGate` (`internal/inference/seam_test.go:34`) does NOT actually match firmware-date stamps — the literal-placement discipline is CONVENTION, not enforced by that gate. Follow it anyway; do not cite the gate as proof.

### Fixed-arg, bounded host exec
**Source:** `internal/detect/gpu_amd.go:239-250` (`runTool`) + `:252-258` (`capRaw`) + `:19` (`maxToolOutput` 8 KiB).
**Apply to:** `firmwareDateProbe()`. Always `runTool(name, fixed-args...)` — NEVER `sh -c`; bound raw via `capRaw`; the package name is a constant literal (no user-controlled arg → no injection surface, ASVS V5).

### Numeric version compare (suffix-tolerant, panic-free)
**Source:** `internal/detect/gpu_amd.go:314-354` (`compareVersionSegments` + `splitNumericSegments`).
**Apply to:** `firmwareDatePolicyOK`. An 8-digit `YYYYMMDD` is one int64-safe segment; works correctly. Gate with an `isYYYYMMDD` (len==8, all-digit) check in the PROBE before comparing, so a rawhide/snapshot VERSION degrades to UNSET, not a misleading compare (Pitfall 2).

### Golden stability — DO NOT re-freeze (D-04)
**Source:** `cmd/villa/detect_test.go` — `fixtureProfile()` (lines 18-50) + `TestJSONGolden` (line 55).
**Apply to:** verification expectations. The golden is FIXTURE-driven (a hand-built `HostProfile` with both new fields explicitly `UnknownBool`), never `detect.Probe()`-derived. The new probes CANNOT perturb it; no seam-stub needed. Any plan task saying "re-freeze golden" or "stub the probe for the golden test" is wrong and D-04-forbidden.

---

## Doc Reconciliation (non-code, D-05) — evidence-checked targets

Pure metadata edits; no code analog. Each confirmed `SATISFIED` in its VERIFICATION.md before tagging.

**`requirements-completed` frontmatter key format** (replicate exactly):
**Source:** `.planning/phases/06-*/06-01-SUMMARY.md:49` etc.
```yaml
requirements-completed: [ROCM-02]
requirements-completed: [ROCM-01, ROCM-02, ROCM-04]
```

| SUMMARY target | Tag to add | VERIFICATION evidence (confirm first) | Insert anchor |
|----------------|-----------|----------------------------------------|---------------|
| `07-03-SUMMARY.md` | `[DET-04]` | 07-VERIFICATION.md:80 `✓ SATISFIED` | top-level key before `metrics:` block |
| `08-01-SUMMARY.md` | `[BSET-01, BSET-02]` | 08-VERIFICATION.md:85-86 `SATISFIED` | before `# Metrics` header |
| `08-02-SUMMARY.md` | `[BSET-03]` | 08-VERIFICATION.md:87 `SATISFIED` | before `# Metrics` header |
| `09-02-SUMMARY.md` | `[BENCH-02]` | 09-VERIFICATION.md:104 `✓ SATISFIED` | before `metrics:` block |
| `09-03-SUMMARY.md` | `[BENCH-02]` | 09-VERIFICATION.md:104 `✓ SATISFIED` | before `metrics:` block |
| `10-02-SUMMARY.md` | `[REC-05]` | 10-VERIFICATION.md:86 `✓ SATISFIED` | before `metrics:` block |

All six currently have 0 `requirements-completed` keys (no duplicate risk).

**`06-REVIEW.md`** — the FRONTMATTER is already correct (`status: resolved`, `critical: 0`, `resolution.cr-01` cites `499644e`). The genuinely-stale string is the PROSE body line 42 `**Status:** issues_found` → change to `**Status:** resolved`. Do NOT touch the frontmatter.

**`REQUIREMENTS.md` ROCM-02 note** — OPEN QUESTION (audit's "line ~88" pointer is imprecise; line 88 is an unrelated Out-of-Scope row; ROCM-02 entries at lines 19/104 are accurate). Recommend a `checkpoint:human-verify` before editing — do NOT invent an edit to satisfy a stale pointer. If no stale line is found, the truthful outcome is "no edit needed."

---

## No Analog Found

None. Every code change has an exact in-tree analog in `internal/detect` (same package, often same file). This phase is pure idiom replication.

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| (none) | — | — | All code changes mirror existing detect helpers. |

---

## Metadata

**Analog search scope:** `internal/detect/` (gpu_amd.go, readiness_rocm.go, detect.go, profile.go, value.go, readiness_rocm_test.go), `internal/preflight/rocm-policy.json` (policy values), `cmd/villa/detect_test.go` (golden harness).
**Files scanned:** 8 (all read directly; line numbers verified against the live tree 2026-06-06).
**Pattern extraction date:** 2026-06-06
