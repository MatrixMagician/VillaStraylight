---
phase: 11-address-v1-1-tech-debt-rocm-readiness-detect-probes-doc-reco
reviewed: 2026-06-06T00:00:00Z
depth: deep
files_reviewed: 4
files_reviewed_list:
  - internal/detect/detect.go
  - internal/detect/gpu_amd.go
  - internal/detect/readiness_rocm.go
  - internal/detect/readiness_rocm_test.go
findings:
  critical: 0
  warning: 3
  info: 3
  total: 6
status: issues_found
---

# Phase 11: Code Review Report

**Reviewed:** 2026-06-06
**Depth:** deep
**Files Reviewed:** 4
**Status:** issues_found

## Summary

Phase 11 promotes two stub probes (`firmwareDateOK`, `hsaOverrideViable`) to real
implementations, backed by a new `firmwareDateProbe` (rpm query) and a
`firmwareDatePolicyOK`/`isYYYYMMDD` seam in `gpu_amd.go`, threaded through
`computeROCmReadiness` and `Probe()`.

The design-contract invariants from the phase brief all hold:

- **No-false-green (D-08):** verified. rpm absent / non-zero exit → `UnknownStr`;
  non-YYYYMMDD VERSION → `UnknownStr`; `firmwareDateOK` returns `UnknownBool` on an
  unknown date; `hsaOverrideViable` gates `Known` on `gfxID.Known`, never on the
  always-Known `rocmPresent`. Confirmed by passing tests.
- **No import cycle:** confirmed — `internal/detect` does not import
  `internal/preflight`; the floor (`20260110`) and deny (`20251125`) literals are
  duplicated detect-side behind the `gpu_amd.go` seam.
- **Fixed-arg exec:** confirmed — `firmwareDateProbe` uses `runTool("rpm", "-q",
  "--qf", "%{VERSION}", "linux-firmware")` with a constant package name; no `sh -c`,
  no string interpolation, no command-injection surface.
- **No env reads:** confirmed — `hsaOverrideViable` is a pure derivation from
  `gfxID` + `rocmPresent`; no `os.Getenv` anywhere in the changed code.
- **Date comparison:** the 8-digit YYYYMMDD compare is sound (see analysis under
  WR-01); `isYYYYMMDD` correctly rejects non-8-digit and non-digit values, and the
  8-digit guard bounds the integer well below overflow.

`go build` and `go test ./internal/detect/` both pass (52 tests). No BLOCKER-class
defects found. The findings below are a real cross-module semantic divergence
(WR-01), an honesty gap in raw-capture on tool failure (WR-02), a duplicated
comparator (WR-03), and minor quality items.

## Warnings

### WR-01: detect firmware verdict diverges from preflight — floor enforced in one place, only denylist in the other

**File:** `internal/detect/gpu_amd.go:412-419` (vs `internal/preflight/checks_rocm.go:116-139`)

**Issue:** `firmwareDatePolicyOK` enforces **both** the denylist **and** the floor:

```go
func firmwareDatePolicyOK(date string) bool {
    for _, denied := range rocmFirmwareDeny {
        if date == denied {
            return false
        }
    }
    return compareVersionSegments(date, rocmFirmwareFloor) >= 0  // floor enforced
}
```

But preflight's `checkROCmFirmware` enforces **only the denylist** — a firmware date
below the floor but not denied is a PASS there (it never compares against
`FirmwareFloor`; the floor is used only in the remediation string). This is
asserted by `internal/detect/readiness_rocm_test.go:113` ("sub-floor not denied",
`20251231` → `Value=false`).

Concrete divergence: a host with `linux-firmware-20251231` (below the 20260110
floor, not on the denylist) yields `firmware_date_ok = Known(false)` in detect →
which feeds `foldROCmReadiness` (`status.go:163`) to produce `not-ready` and
`deriveROCmAdvice` (`recommend.go:181`) to withhold advice — while
`villa preflight` would PASS the same firmware. Two first-party commands give
contradictory verdicts on the same host.

The detect-side behavior is arguably the more-correct reading of CLAUDE.md
("linux-firmware >= 20260110"), so the fix is likely to align preflight up to the
floor rather than weaken detect. Either way the two policies must agree.

**Fix:** Make the two checks share one verdict shape. Either add the floor compare
to `checkROCmFirmware` (preferred, matches CLAUDE.md), or drop the floor from
`firmwareDatePolicyOK` so both are denylist-only:

```go
// preflight/checks_rocm.go — enforce the floor too, not just the denylist
if compareVersions(fw.Value, floor) < 0 {
    return fail(idROCmFirmware, name,
        fmt.Sprintf("linux-firmware-%s is below the %s ROCm floor", fw.Value, floor),
        remediation, fw.Source, fw.Raw)
}
```

### WR-02: rpm-probe raw capture loses the error message on failure (stdout-only capture)

**File:** `internal/detect/gpu_amd.go:239-250` (consumed by `firmwareDateProbe`, line 379-389)

**Issue:** `runTool`'s doc comment claims it returns "combined output", but
`cmd.Output()` captures **stdout only** and discards stderr. When `rpm -q` fails
(package not installed), the human-readable reason ("package linux-firmware is not
installed") is printed by rpm to **stderr**, so `firmwareDateProbe`'s
`UnknownStr(..., capRaw(out))` records an empty raw. The provenance/`-v` output then
shows the unknown reason without the actual tool diagnostic, undermining the
debuggability the `Raw` field exists for. Functionally the no-false-green path is
still correct (ok=false → Unknown), so this is a robustness/honesty gap, not a
correctness bug.

Note: this is a pre-existing `runTool` behavior, but the firmware probe is the new
caller that surfaces it (rpm prints failure detail to stderr, unlike
vulkaninfo/rocminfo whose failures are mostly "binary absent" caught by `LookPath`).

**Fix:** Either correct the doc comment to say "stdout only", or capture stderr on
failure so the raw is meaningful:

```go
cmd := exec.Command(name, args...)
var stdout, stderr bytes.Buffer
cmd.Stdout, cmd.Stderr = &stdout, &stderr
err := cmd.Run()
raw := stdout.Bytes()
if err != nil && stdout.Len() == 0 {
    raw = stderr.Bytes() // surface the tool's own failure message
}
// ... bound and return
```

### WR-03: comparator duplicated three ways (preflight + detect, twice in detect)

**File:** `internal/detect/gpu_amd.go:314-354`

**Issue:** `compareVersionSegments`/`splitNumericSegments` re-express
`internal/preflight`'s `compareVersions`/`splitVersion` (acknowledged in the
comments as unavoidable due to the import-cycle constraint). That detect/preflight
duplication is justified. However, within detect the same comparator is now reused
for two semantically different domains — dotted kernel versions
(`kernelMeetsROCmFloor`) and a single-segment YYYYMMDD integer
(`firmwareDatePolicyOK`). For the firmware case the "segment" machinery is
incidental: an 8-digit date has no `.`, so it degrades to a single
`strconv`-equivalent integer compare. This works but obscures intent and invites a
future maintainer to "fix" the comparator for kernels in a way that silently
changes firmware-date semantics.

**Fix:** For the firmware path, compare the validated 8-digit strings directly
(lexical compare is correct for fixed-width zero-padded YYYYMMDD and makes the
intent explicit), decoupling it from the kernel comparator:

```go
func firmwareDatePolicyOK(date string) bool {
    for _, denied := range rocmFirmwareDeny {
        if date == denied {
            return false
        }
    }
    return date >= rocmFirmwareFloor // both are validated 8-digit YYYYMMDD
}
```

(`isYYYYMMDD` already guarantees fixed-width 8 digits on both operands, so the
lexical compare is exact.)

## Info

### IN-01: `rocmFirmwareDeny` is a package-level mutable `var`

**File:** `internal/detect/gpu_amd.go:368`

**Issue:** `var rocmFirmwareDeny = []string{"20251125"}` is a mutable package
global; a sibling file in `package detect` could append/overwrite it. The adjacent
floor/image/kernel literals are all `const`. A slice can't be `const`, but it can be
made tamper-resistant.

**Fix:** Keep it unexported (it already is) and treat as read-only by convention, or
range over a function-local literal inside `firmwareDatePolicyOK` so there is no
shared mutable state. Low priority given the single-package blast radius.

### IN-02: doc comment on `runTool` says "combined output" but returns stdout only

**File:** `internal/detect/gpu_amd.go:236`

**Issue:** Same root as WR-02 but tracked separately for the doc-accuracy fix: the
comment "returns its combined output" is inaccurate for `cmd.Output()`. Correct the
wording even if the capture behavior is left as-is.

**Fix:** Change "combined output" → "stdout, bounded to maxToolOutput bytes".

### IN-03: firmware-date test coverage omits the non-YYYYMMDD UNSET path

**File:** `internal/detect/readiness_rocm_test.go:103-127`

**Issue:** `TestFirmwareDateOK` covers floor/deny/sub-floor and the unprobed-Unknown
path, but there is no direct unit test for `firmwareDateProbe`/`isYYYYMMDD` rejecting
a non-date VERSION (e.g. a rawhide snapshot string) → UNSET. The no-false-green
behavior for the "rpm present but VERSION unparseable" branch (gpu_amd.go:385-387)
is therefore unexercised. `isYYYYMMDD` itself has no dedicated table test.

**Fix:** Add a small table test for `isYYYYMMDD` (8 digits true; 7/9 digits, embedded
letters, empty → false) and assert `firmwareDateProbe`-shaped inputs degrade to
Unknown for a non-date string.

---

_Reviewed: 2026-06-06_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
