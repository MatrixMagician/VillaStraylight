---
phase: 13-villa-doctor-health-diagnosis
plan: 01
subsystem: api
tags: [go, doctor, health-diagnosis, preflight, status, offload, drift, worst-wins, pure-core]

# Dependency graph
requires:
  - phase: 05-status-read-model
    provides: "status.Report read-model with per-service offload Verdict (status.Run/Aggregate)"
  - phase: 03-preflight-gate
    provides: "preflight.Run/RunROCm host-prep CheckResult gate (BLOCK/WARN, remediation-carrying)"
  - phase: 06-rocm-backend-resolver-spine
    provides: "inference.IsROCmFamily single ROCm-family enumeration point; inference.Verdict typed offload result"
  - phase: 04-orchestrate
    provides: "orchestrate.Reconcile config-vs-disk diff (non-empty Plan.Changed = drift)"
provides:
  - "Pure internal/doctor core: Finding, Report, Deps, Aggregate, reportSchemaVersion=1"
  - "Worst-wins health diagnosis composing preflight + status health + offload Verdict + drift Plan"
  - "doctor-OWNED Report type (status.Report untouched, D-02)"
  - "Off-hardware table-driven core tests with stubbed Deps"
affects: [13-02 (cmd/villa doctor verb + liveDoctorDeps + renderDoctor + golden), villa-doctor]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Pure-core + injectable-Deps composition (mirrors internal/status)"
    - "Worst-wins severity fold normalizing four upstream signal vocabularies into one PASS/WARN/FAIL grammar"
    - "doctor-owned Report contract (SchemaVersion-last, append-only) — never extends a sibling's frozen golden"

key-files:
  created:
    - internal/doctor/doctor.go
    - internal/doctor/doctor_test.go
  modified: []

key-decisions:
  - "doctor defines its OWN Report (reportSchemaVersion=1); status.Report only READ, never extended (D-02)"
  - "Worst-wins maps to AUTHORITATIVE shipped preflight tiers — confident BLOCK/offload FAIL→FAIL(exit 1), WARN/drift/typed-Unknown→WARN(exit 2); the inverted ROADMAP parenthetical was NOT followed (D-04/Pitfall 1)"
  - "Offload FAIL is folded as a BLOCK-class FAIL that dominates HealthReady (no false-green over a health-200, Pitfall 3)"
  - "Drift is independent of running-stack health: a healthy stack on stale units still WARNs (Pitfall 4)"
  - "A drift read error (absent unit dir) degrades to a typed-Unknown WARN, never a panic (D-08)"
  - "inference.Verdict consumed opaquely; ROCm routed via inference.IsROCmFamily — no leaked marker literals (TestSeamGrepGate green)"

patterns-established:
  - "Pure doctor core composes shipped cores via Deps func-fields; the cmd tier (Plan 02) wires liveDoctorDeps"
  - "Normalized Finding wrapper keeps doctor's golden contract-independent of upstream CheckResult/Verdict struct shapes"

requirements-completed: [DOCTOR-01, DOCTOR-02, DOCTOR-03]

# Metrics
duration: 3min
completed: 2026-06-07
---

# Phase 13 Plan 01: `villa doctor` Pure Health-Diagnosis Core Summary

**Pure `internal/doctor.Aggregate` worst-wins core that composes the shipped preflight gate, the status read-model + its per-service offload Verdict, and an orchestrate.Reconcile drift Plan into one doctor-owned PASS/WARN/FAIL Report — offload FAIL dominates a health-200, drift is health-independent, and every non-PASS finding carries remediation.**

## Performance

- **Duration:** ~3 min (143s)
- **Started:** 2026-06-07T14:17:02Z
- **Completed:** 2026-06-07T14:19:25Z
- **Tasks:** 2
- **Files modified:** 2 (both created)

## Accomplishments
- Built the pure `internal/doctor` core (`Finding`, `Report`, `Deps`, `Aggregate`, `reportSchemaVersion=1`) — all DOCTOR-01/02/03 decision logic, zero host I/O (every host touch is an injected `Deps` func-field).
- Worst-wins fold composing four shipped signals (preflight host-prep checks, status service health, the per-service offload `inference.Verdict`, a drift `orchestrate.Plan`) into one normalized PASS/WARN/FAIL vocabulary mapped to the authoritative shipped preflight tiers.
- Offload FAIL folded as a BLOCK-class FAIL that dominates a `HealthReady` (no false-green over a health-200); drift folded independently of health; a drift read error degrades to a typed-Unknown WARN (no panic).
- Off-hardware table-driven test scaffold (stubbed `Deps` mirroring `newStatusDeps`) freezing the four behaviors; full suite (588 tests) and `TestSeamGrepGate` stay green; `status.Report` untouched.

## Task Commits

Each task was committed atomically (TDD RED → GREEN):

1. **Task 1: Wave-0 doctor core test scaffold (RED)** - `09e08fe` (test)
2. **Task 2: Implement the pure doctor core — Finding/Report/Deps/Aggregate (GREEN)** - `e3f4f5f` (feat)

**Plan metadata:** committed with this SUMMARY (docs).

_TDD: RED commit established the failing/non-compiling tests; GREEN commit implemented the core to pass them._

## Files Created/Modified
- `internal/doctor/doctor.go` - Pure doctor core: package doc (pure/no-exit/no-print contract), `Finding`/`Report`/`Deps` types, `reportSchemaVersion=1`, `Aggregate(Deps) Report` worst-wins fold, plus `findingFromCheck`/`healthFinding`/`offloadFinding` normalizers.
- `internal/doctor/doctor_test.go` - Table-driven core tests with a stubbed-`Deps` builder: `TestRemediationPresent`, `TestOffloadFailDominatesHealth`, `TestDriftWarn`, `TestDriftReadErrorDegrades`.

## Decisions Made
- Followed the plan and 13-CONTEXT/13-RESEARCH decisions exactly. The single load-bearing correction applied (per D-04 / Pitfall 1, already resolved in the plan): a confident offload/BLOCK FAIL maps to the FAIL/blocked tier (exit 1 downstream) and WARN/drift/typed-Unknown maps to the WARN tier (exit 2) — the inverted ROADMAP parenthetical was deliberately NOT followed.
- Added an extra degradation test (`TestDriftReadErrorDegrades`) beyond the three required tests to lock D-08 (absent unit dir → typed-Unknown WARN, never a panic). The behavior was specified in the plan's `<behavior>`; the test makes it executable.
- Added a `loopback` BLOCK FAIL finding when `status.Report.LoopbackOnly` is false (PRIV-01 breach), consistent with `status.Aggregate`'s own loopback FAIL — keeps doctor honest about a privacy regression rather than only diagnosing offload/health/drift.

## Deviations from Plan
None - plan executed exactly as written. (The loopback-finding and the fourth degradation test are within the plan's stated behaviors and the offload/health/drift fold scope, not unplanned scope.)

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required (pure core, no host I/O, no new dependencies; `go.mod` unchanged).

## Next Phase Readiness
- Plan 02 (cmd tier) can now wire `cmd/villa/doctor.go` (`newDoctor`/`runDoctor`/`renderDoctor`/`liveDoctorDeps`/`unitDirReadOnly`) over this core, freeze `cmd/villa/testdata/doctor.json.golden`, and register the verb in `root.go`.
- The exit-code mapping for Plan 02 must reuse the package-level `exitPass`(0)/`exitWarn`(2)/`exitBlocked`(1) constants and map the doctor `Report.Overall` string accordingly (FAIL→exitBlocked, WARN→exitWarn, PASS→exitPass) — NOT the inverted prose.
- No blockers.

## Self-Check: PASSED
- FOUND: internal/doctor/doctor.go
- FOUND: internal/doctor/doctor_test.go
- FOUND commit: 09e08fe (Task 1, test RED)
- FOUND commit: e3f4f5f (Task 2, feat GREEN)
- Verification: `go test ./internal/doctor/` 4 passed; `go test ./internal/inference/ -run TestSeamGrepGate` green; `go vet ./...` clean; `go test ./...` 588 passed; `git diff --stat internal/status/` empty (status.Report untouched, D-02); 0 leaked marker literals.

---
*Phase: 13-villa-doctor-health-diagnosis*
*Completed: 2026-06-07*
