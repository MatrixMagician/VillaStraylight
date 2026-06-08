---
phase: 13-villa-doctor-health-diagnosis
plan: 03
subsystem: api
tags: [doctor, rocm, residency, preflight, golden-test, tdd, seam]

# Dependency graph
requires:
  - phase: 13-villa-doctor-health-diagnosis (13-01)
    provides: pure internal/doctor core (Aggregate worst-wins fold, Finding/Report, offloadFinding/findingFromCheck/statusRank)
  - phase: 13-villa-doctor-health-diagnosis (13-02)
    provides: cmd/villa/doctor.go (liveDoctorDeps, renderDoctor, exit-code mapping, golden idiom)
provides:
  - Residency-supersession rule in doctor.Aggregate — a proven ROCm-family offload StatusPass down-ranks the three typed-Unknown ROCm host-prep WARNs (ROCM-PRE-firmware/-hsa/-image) so a fully-healthy opt-in ROCm install reaches Overall=PASS / exit 0 (closes 13-UAT Test 1)
  - Nil-safe doctor.Deps.RunROCmImage seam + image-aware live wiring (preflight.RunROCmForImage via inference.BackendFor(cfg.Backend).Image()) so a denied RUNNING ROCm image is a confident FAIL, not an un-evaluated WARN
  - No-false-green guards proving the down-rank keys on the (ID AND Status==WARN) conjunction, never ID-alone
affects: [doctor, status, rocm, future-doctor-fix-verb]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Residency-supersession: a proven downstream proof (offload StatusPass) down-ranks structurally-unevaluable upstream advisories by an (ID + Status) conjunction, kept VISIBLE but non-rank-raising"
    - "Image-aware host-prep via a nil-safe Deps func seam: the backend image literal stays behind inference.BackendFor(...).Image(), supplied by the live wiring, never typed in the pure core"

key-files:
  created:
    - cmd/villa/testdata/doctor-rocm-superseded.golden
  modified:
    - internal/doctor/doctor.go
    - internal/doctor/doctor_test.go
    - cmd/villa/doctor.go
    - cmd/villa/doctor_test.go

key-decisions:
  - "Supersession down-ranks by suppressing the rank contribution of (superseded-ID AND Status==statusWarn) findings in the worst-wins fold; findings stay VISIBLE (no serialized field, no schema bump)"
  - "The superseded set is exactly ROCM-PRE-firmware/-hsa/-image (the structurally typed-Unknown checks); the Probe-driven ROCM-PRE-gfx/-kernel are deliberately NOT superseded"
  - "Doctor-local idROCm* string consts duplicate the unexported preflight check IDs (matched by stable ID string, not by importing) — seam-safe, not backend marker literals"

patterns-established:
  - "Residency-supersession with an ID+Status conjunction guard preserving no-false-green (a confident FAIL on a superseded ID still folds to FAIL)"
  - "RunROCmImage nil-fallback Deps seam: ROCm-family → preflight.RunROCmForImage(image); else nil → preflight.RunROCm/Run"

requirements-completed: [DOCTOR-01, DOCTOR-02]

# Metrics
duration: 38min
completed: 2026-06-07
---

# Phase 13 Plan 03: Doctor ROCm Residency-Supersession Gap-Closure Summary

**A proven ROCm-family offload StatusPass now down-ranks the three typed-Unknown ROCm host-prep WARNs (firmware/hsa/image) so a fully-healthy opt-in ROCm install reaches exit 0, with an Option-B image seam catching a denied running image as a confident FAIL — and no-false-green preserved by an ID+Status conjunction guard.**

## Performance

- **Duration:** ~38 min
- **Started:** 2026-06-07
- **Completed:** 2026-06-07
- **Tasks:** 3 (TDD: RED → GREEN → Option-B/guards/golden)
- **Files modified:** 4 (+1 golden created)

## Accomplishments
- Closed 13-UAT Test 1: a residency-proven ROCm install no longer folds to Overall=WARN (exit 2) — the three structurally typed-Unknown ROCm host-prep advisories are down-ranked under a proven offload StatusPass, restoring the DOCTOR-01 "exit 0 = healthy" contract on the recommended opt-in ROCm path.
- Preserved no-false-green (DOCTOR-02): the down-rank predicate is the `(superseded-ID AND Status==statusWarn)` CONJUNCTION; a confident StatusFail on the very same IDs (e.g. a denied running image) is NEVER suppressed and still folds to FAIL. A pure ID-set match was explicitly avoided.
- Added the nil-safe `doctor.Deps.RunROCmImage` seam + image-aware live wiring (`preflight.RunROCmForImage` bound to `inference.BackendFor(cfg.Backend).Image()`) so a denied RUNNING ROCm image is actively caught as a confident FAIL instead of an un-evaluated WARN — with the image literal resolved only through the inference seam (no leak into cmd/villa or internal/doctor).
- Kept the contract stable: status.Report untouched (D-02), read-only preserved (D-03, no MkdirAll/WriteUnits), doctor schema_version stays 1, and the three pre-existing goldens are byte-identical (only `doctor-rocm-superseded.golden` is new).

## Task Commits

Each task was committed atomically on `gsd/phase-13-villa-doctor-health-diagnosis`:

1. **Task 1: RED — supersession + Probe-reachable gating tests** - `c1b9f6c` (test)
2. **Task 2: GREEN — residency-supersession rule in doctor.Aggregate** - `1e4e765` (feat)
3. **Task 3: Option B image seam + no-false-green guard + cmd-tier golden** - `02265f3` (feat)

_Note: TDD RED/GREEN landed as two commits; Task 3 bundled B1/B2/B3 as one atomic feat commit._

## Files Created/Modified
- `internal/doctor/doctor.go` - Added idROCm* ID consts + supersededROCmHostPrepID predicate; computed `rocmResidencyProven` in the health-fold loop (IsROCmFamily + OffloadApplies + StatusPass); new fold step 4a down-ranks the three superseded typed-Unknown WARNs (ID+Status conjunction); added the nil-safe `RunROCmImage` Deps seam wired into the host-conditions step.
- `internal/doctor/doctor_test.go` - `rocmDoctorDeps()` helper (Known gfx1151 + kernel 6.18.9 so gfx/kernel PASS, isolating firmware/hsa/image WARNs); `TestROCmResidencySupersedesHostPrepWARN`, `TestROCmResidencyDoesNotFireOnStatusFail`, `TestConfidentROCmFAILStillDominatesResidency`.
- `cmd/villa/doctor.go` - `liveDoctorDeps` resolves the running ROCm image via `inference.BackendFor(cfg.Backend).Image()` and binds `preflight.RunROCmForImage` for ROCm-family backends (nil for vulkan); added preflight import.
- `cmd/villa/doctor_test.go` - `rocmSupersededReport()` fixture + `TestDoctorExitCodes` `rocm-superseded` row (exit 0); `TestLiveDoctorDepsWiresRunROCmImage` (non-nil for rocm, nil for vulkan via XDG_CONFIG_HOME).
- `cmd/villa/testdata/doctor-rocm-superseded.golden` - New golden: overall PASS with the ROCM-PRE-firmware/-hsa WARN advisories still visible.

## Decisions Made
- **Superseded set scoped to the structural typed-Unknown checks only.** ROCM-PRE-firmware/-hsa are hardcoded typed-Unknown (checks_rocm.go:66-67) and ROCM-PRE-image is empty-image WARN in the standalone gate — these are the structurally-unevaluable advisories a proven residency already answers. ROCM-PRE-gfx/-kernel are genuine Probe-driven signals and are deliberately NOT superseded. The test fixture sets a Known-good gfx1151 + kernel 6.18.9 so those two PASS, isolating the three structural WARNs (a Task-2 fixture refinement so the GREEN assertion is reachable).
- **Rank-suppression mechanism over a serialized field.** The fold skips the rank contribution of superseded findings via a predicate; no new tagged Report/Finding field was added, so schema_version stays 1 and the existing goldens stay byte-frozen. Findings remain rendered (visible-but-non-rank-raising).
- **The strong no-false-green guard targets the superseded idROCmImage at confident FAIL** (reachable only via the new RunROCmImage seam, since firmware/hsa FAILs are Probe-unreachable per checks_rocm.go:66-67) — exactly where an ID-only match would have wrongly swallowed a fault.

## Deviations from Plan

None - plan executed exactly as written.

The Task-1 RED gate ran via `rtk proxy go test` because the project's `rtk` shell proxy reformats `go test` output and strips the literal `--- FAIL:` line the gate greps for; `rtk proxy` passes the raw command through unchanged. This is an output-formatting accommodation, not a change to the test or the verify semantics — the RED still failed for the mandated right reason (`Overall = "WARN", want PASS`).

## Issues Encountered
- **Initial GREEN failure: Overall still WARN.** The first `rocmDoctorDeps()` used a bare `detect.HostProfile{}`, under which ROCM-PRE-gfx and ROCM-PRE-kernel are ALSO typed-Unknown WARN (not just firmware/hsa/image). Since those two are correctly NOT in the superseded set, they held Overall at WARN. Resolved by setting the fixture Probe to Known-good gfx1151 + kernel 6.18.9 so gfx/kernel PASS — isolating exactly the three structural firmware/hsa/image WARNs the supersession targets. This confirmed the supersession is correctly narrow (it does not over-fire on Probe-driven host-prep signals).

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- DOCTOR-01 ("exit 0 = healthy") holds on the opt-in ROCm path; DOCTOR-02 (no-false-green / offload-asserting) preserved. `make check` and `make lint` (go vet fallback) green; `TestSeamGrepGate` green.
- The `RunROCmImage` nil-safe seam is a clean extension point for any future doctor evaluation that needs the resolved running image.
- No blockers. The diagnosed UAT gap (13-UAT.md Test 1) is closed.

## Self-Check: PASSED

- Created files verified present: `cmd/villa/testdata/doctor-rocm-superseded.golden`, `.planning/phases/13-villa-doctor-health-diagnosis/13-03-SUMMARY.md`.
- Task commits verified present: `c1b9f6c` (RED), `1e4e765` (GREEN), `02265f3` (Option B/guards/golden).

---
*Phase: 13-villa-doctor-health-diagnosis*
*Completed: 2026-06-07*
