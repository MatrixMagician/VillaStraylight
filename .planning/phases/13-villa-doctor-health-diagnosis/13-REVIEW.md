---
phase: 13-villa-doctor-health-diagnosis
reviewed: 2026-06-07T00:00:00Z
depth: deep
files_reviewed: 5
files_reviewed_list:
  - internal/doctor/doctor.go
  - internal/doctor/doctor_test.go
  - cmd/villa/doctor.go
  - cmd/villa/doctor_test.go
  - cmd/villa/testdata/doctor-rocm-superseded.golden
findings:
  critical: 0
  warning: 2
  info: 3
  total: 5
status: resolved
resolution:
  fixed_in: 00cb7e8
  WR-01: "fixed — liveDoctorDeps now fails closed on a BackendFor error in the image gate (mirrors DriftPlan), closing the latent false-green path"
  WR-02: "fixed — corrected the Aggregate doc comments (doctor.go:160, :260) to state all WARN-status superseded host-prep findings are down-ranked, not only the typed-Unknown subset"
  IN-03: "fixed — dropped the inaccurate 'three' count in the rocmSupersededReport fixture doc"
  IN-01: "accepted — Deps.LoadConfig kept as a documented reserved forward seam (stable Deps shape)"
  IN-02: "accepted — fixture detail strings are internally consistent with the renderer golden; not a contract break"
---

# Phase 13: Code Review Report (gap-closure: ROCm residency-supersession)

**Reviewed:** 2026-06-07
**Depth:** deep
**Files Reviewed:** 5
**Status:** issues_found

## Summary

Reviewed the Phase 13 gap-closure "ROCm residency-supersession" feature (commits since
`f1de18d`) at deep depth, with adversarial focus on the five honesty-critical invariants.
This review supersedes the earlier Phase-13 review (CR-01/WR-01/WR-02/IN-01, resolved in
`e9d4002`); those concerned the base doctor verb, not the supersession feature.

**Honesty-critical invariants — all hold:**

1. **No-false-green (DOCTOR-02): PASS.** The supersession predicate
   (`internal/doctor/doctor.go:278-280`) is the correct CONJUNCTION
   `rocmResidencyProven && f.Status == statusWarn && supersededROCmHostPrepID(f.ID)`.
   A confident `statusFail` on any of `ROCM-PRE-firmware/-hsa/-image` is never suppressed
   (the `f.Status == statusWarn` clause excludes it) and still folds to `Overall=FAIL`.
   I traced FAIL-reachability of all three superseded IDs in
   `internal/preflight/checks_rocm.go`: firmware-denylist FAIL (`:142`), HSA-wrong FAIL
   (`:194`), image-denylist FAIL (`:221`) — none can be down-ranked because each carries
   `Status==FAIL`, not `WARN`. The iota-0 zero-value trap is correctly defended:
   `rocmResidencyProven` is gated on `s.OffloadApplies && IsROCmFamily && Offload.Status==StatusPass`
   (`:213-217`), so a zero-value Verdict on a non-offload service cannot spuriously prove
   residency. The suppression touches no non-ROCm-host-prep finding (drift, health, loopback,
   offload are not in `supersededROCmHostPrepID`).

2. **Seam grep-gate: PASS.** No backend marker literals (`ROCm0`/`Vulkan0`/`HSA_OVERRIDE`/
   image tags) appear in either changed `.go` file. The `ROCM-PRE-*` constants are
   finding-ID strings, not marker tokens. The running image is resolved only via
   `inference.BackendFor(cfg.Backend).Image()` (`cmd/villa/doctor.go:174-175`); the image
   string never appears as a literal. `go test` of both packages passes (192 tests),
   including `TestSeamGrepGate`.

3. **Read-only (D-03): PASS.** No `MkdirAll`/`WriteUnits`/model-load probe added.
   `unitDirReadOnly` (`cmd/villa/doctor.go:143-149`) is the directory-creation-free twin,
   and the lone `os.Stat` (`:223`) is read-only.

4. **Contract stability: PASS.** `schema_version` stays `1` (`internal/doctor/doctor.go:54`);
   no new serialized `Report` field. Superseded findings stay VISIBLE in `Findings` — only
   their rank contribution is dropped (`:283-284` `continue` skips rank, not append).
   `TestDoctorJSON` and `doctor.json.golden` remain unchanged.

5. **Error / nil-safety: PASS (with one robustness gap, WR-01).** A nil `RunROCmImage`
   falls back to `preflight.RunROCm` (`:181-182`); a non-ROCm backend takes the
   `preflight.Run` default. No panic path. `BackendFor` error in the image-gate wiring is
   handled (degrades to nil gate) — see WR-01 for the silent-swallow nuance.

Ordinary deep pass surfaced no logic bugs in the changed code. The two WARNINGs below are
robustness/maintainability concerns; the INFO items are minor.

## Warnings

### WR-01: `BackendFor` error in image-gate wiring is silently swallowed (asymmetric with DriftPlan)

**File:** `cmd/villa/doctor.go:173-180`
**Issue:** In `liveDoctorDeps`, when the backend is ROCm-family, a `BackendFor` error is
discarded by the `if ... berr == nil` guard — `rocmImageGate` stays nil and `Aggregate`
silently falls back to `preflight.RunROCm` (the un-image-aware gate). This is currently
*unreachable* for valid ROCm names (all three resolve cleanly), so it is not a live bug.
But it is a latent honesty hazard: `IsROCmFamily` and `BackendFor` enumerate the ROCm-name
set in two independent places (`internal/inference/backend.go:24` and `:47`). If a future
ROCm digest is added to `IsROCmFamily` but missed in `BackendFor`, this path would silently
downgrade the image-aware denied-image FAIL to the un-evaluated "no image requested" WARN —
a false-green the supersession could then swallow. The *same* `BackendFor` call in the
`DriftPlan` closure (`:196-199`) correctly surfaces its error, making the handling asymmetric.
**Fix:** Surface the error rather than swallowing it, mirroring the DriftPlan path:
```go
if inference.IsROCmFamily(cfg.Backend) {
    b, berr := inference.BackendFor(cfg.Backend)
    if berr != nil {
        return doctor.Deps{}, fmt.Errorf("resolve ROCm backend image: %w", berr)
    }
    image := b.Image()
    rocmImageGate = func(p detect.HostProfile) []preflight.CheckResult {
        return preflight.RunROCmForImage(p, image)
    }
}
```

### WR-02: Supersession down-ranks the sub-floor-firmware WARN too, widening past the documented "typed-Unknown" scope

**File:** `internal/doctor/doctor.go:278-280` (predicate); doc `:162`, `:264`
**Issue:** The supersession down-ranks ALL `WARN`-status findings on the three superseded
IDs, including the firmware *floor* advisory (`checks_rocm.go:153-156`) — a firmware version
that is below the recommended floor but not denylisted. That is a real (Known, non-Unknown)
WARN, not the structural "could-not-evaluate off-host" advisory the supersession was scoped
to ("typed-Unknown ROCm host-prep WARNs", per the package doc at `:162` and `:264`). Under
proven residency this sub-floor WARN is suppressed from the rank, so a host on below-floor
firmware reports `Overall=PASS` with the advisory only visible in the table. This is
*defensible* (residency IS proven, so the floor concern is empirically moot) and not a
false-green per se — but the doc comment and the implemented predicate disagree on scope,
which is a correctness-of-documentation hazard for the next maintainer reasoning about the
invariant.
**Fix:** Correct the doc comments at `internal/doctor/doctor.go:162` and `:264` to state that
*all* WARN-status superseded host-prep findings are down-ranked under proven residency (not
only the typed-Unknown subset), so the stated invariant matches the code. (Narrowing the
predicate instead is not feasible — the doctor layer consumes the CheckResult opaquely and
cannot distinguish the typed-Unknown firmware WARN from the sub-floor firmware WARN.)

## Info

### IN-01: `LoadConfig` Deps field is wired but never read by the core

**File:** `internal/doctor/doctor.go:121-122`; `cmd/villa/doctor.go:183`
**Issue:** `Deps.LoadConfig` is documented as "Reserved … the core reads it only if a future
finding needs config directly" and `Aggregate` never calls it. `liveDoctorDeps` wires it
(`config.LoadVilla`) and `newDoctorDeps` stubs it, so it is dead plumbing today. Harmless,
but it forces a reader to consult the doc to learn the field is intentionally unused.
**Fix:** Either drop the field until a finding needs it (YAGNI), or keep the explicit
"reserved" doc comment — acceptable if the team prefers a stable Deps shape.

### IN-02: Golden fixture firmware/HSA detail strings diverge from the real preflight output

**File:** `cmd/villa/doctor_test.go:80-81`; `cmd/villa/testdata/doctor-rocm-superseded.golden:5-6`
**Issue:** The hand-authored fixture uses "firmware version not probed; ensure recent and
avoid the denied build", whereas the real `checkROCmFirmware` (`checks_rocm.go:136-138`) emits
"firmware version not probed; ensure ≥ {floor} and avoid {denied}". The cmd-tier golden is
internally consistent with its own `rocmSupersededReport()` fixture (it tests the renderer,
not the core), so this is not a contract break — but the golden does not reflect a string the
production path would render, weakening it as a documentation sample.
**Fix:** Optionally align the fixture detail/remediation strings with the real
`checkROCmFirmware`/`checkROCmHSA` output so the golden doubles as accurate sample output.

### IN-03: Fixture doc comment says "three … advisories" but lists/includes only two

**File:** `cmd/villa/doctor_test.go:68-74`
**Issue:** The `rocmSupersededReport` doc comment says "the three typed-Unknown ROCm host-prep
advisories (ROCM-PRE-firmware/-hsa) still VISIBLE" — it names only two IDs and omits
`ROCM-PRE-image`, and the fixture itself includes only firmware + hsa WARN rows. The fixture
is valid (image simply absent), but "three" contradicts the two listed/present.
**Fix:** Reword to "the typed-Unknown ROCm host-prep advisories (ROCM-PRE-firmware/-hsa)" and
drop the "three" count, or add a `ROCM-PRE-image` WARN row for completeness.

---

_Reviewed: 2026-06-07_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
