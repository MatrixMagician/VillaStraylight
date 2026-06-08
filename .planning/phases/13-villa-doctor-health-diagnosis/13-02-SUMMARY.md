---
phase: 13-villa-doctor-health-diagnosis
plan: 02
subsystem: cli
tags: [go, doctor, cmd-tier, cobra, exit-codes, golden, read-only, seam-clean, drift]

# Dependency graph
requires:
  - phase: 13-villa-doctor-health-diagnosis
    provides: "Pure internal/doctor core (Aggregate/Deps/Report/Finding/reportSchemaVersion=1) — Plan 01"
  - phase: 03-preflight-gate
    provides: "Authoritative exitPass(0)/exitWarn(2)/exitBlocked(1) constants + renderPreflight exit-rollup idiom (cmd/villa/preflight.go)"
  - phase: 05-status-read-model
    provides: "liveStatusDeps + status.Run reused wholesale for the running-stack read-model"
  - phase: 04-orchestrate
    provides: "orchestrate.Render/Reconcile/Plan for the config-vs-disk drift check (read-only, no WriteUnits)"
provides:
  - "`villa doctor` end-to-end: read-only one-shot health diagnosis registered on the root tree"
  - "renderDoctor worst-wins exit rollup reusing the authoritative preflight constants (FAIL→1, WARN/drift→2, healthy→0)"
  - "liveDoctorDeps: reuses liveStatusDeps wholesale + a no-WriteUnits DriftPlan closure"
  - "unitDirReadOnly: quadletUnitDir twin with NO directory creation (read-only invariant)"
  - "doctor-OWNED byte-frozen doctor.json.golden (schema_version:1) + table goldens"
affects: [villa-doctor, milestone-doctor]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Thin cobra caller over a pure core (mirrors cmd/villa/status.go + preflight.go) — verb wires liveDoctorDeps, core decides"
    - "Read-only resolver twin: unitDirReadOnly drops the quadletUnitDir directory-creation step so a diagnosis mutates nothing"
    - "doctor-owned golden frozen via the shared -update idiom; never extends a sibling's frozen status.Report golden"

key-files:
  created:
    - cmd/villa/doctor.go
    - cmd/villa/doctor_test.go
    - cmd/villa/testdata/doctor.json.golden
    - cmd/villa/testdata/doctor-pass.golden
    - cmd/villa/testdata/doctor-warn.golden
  modified:
    - cmd/villa/root.go

key-decisions:
  - "Exit mapping reuses the AUTHORITATIVE shipped preflight constants — confident FAIL→exitBlocked(1), WARN/drift/typed-Unknown→exitWarn(2), healthy→exitPass(0); the inverted ROADMAP parenthetical was NOT followed (D-04/Pitfall 1)"
  - "unitDirReadOnly is quadletUnitDir MINUS the directory-creation step — doctor never creates the Quadlet dir (Pitfall 2/D-03); doctor.go contains zero MkdirAll"
  - "liveDoctorDeps reuses liveStatusDeps wholesale (no re-wired HTTP/journald/GTT probes — RESEARCH A1); DriftPlan Renders from config + Reconciles, NEVER WriteUnits"
  - "DriftPlan returns Reconcile read errors verbatim so the core degrades an absent unit dir to a typed-Unknown WARN (D-08), never swallows them"
  - "No --force and no generation probe (D-03/D-07); doctor is strictly read-only"
  - "No backend marker literals in cmd/villa/doctor.go; ROCm routed via the core's inference.IsROCmFamily, backends resolved via inference.BackendFor (TestSeamGrepGate green)"

patterns-established:
  - "Read-only unit-dir resolver twin for diagnose-only verbs (vs the write-path quadletUnitDir)"
  - "Exit table fixtures built directly as doctor.Report (no live host) so renderDoctor's worst-wins rollup is asserted deterministically"

requirements-completed: [DOCTOR-01, DOCTOR-02, DOCTOR-03]

# Metrics
duration: 8min
completed: 2026-06-07
---

# Phase 13 Plan 02: `villa doctor` Command-Tier Wiring Summary

**Thin `cmd/villa/doctor.go` cobra verb wiring the pure Plan-01 doctor core to live host seams — a read-only `villa doctor` whose worst-wins exit rollup reuses the authoritative preflight constants (confident FAIL→1, drift/WARN→2, healthy→0), wired via `liveDoctorDeps` (reusing `liveStatusDeps` + a no-WriteUnits drift Reconcile), a `unitDirReadOnly` resolver that creates no Quadlet dir, doctor's OWN frozen `--json` golden at schema_version 1, and verb registration on the root tree.**

## Performance
- **Duration:** ~8 min
- **Completed:** 2026-06-07T14:32:33Z
- **Tasks:** 2
- **Files modified:** 6 (5 created, 1 edited)

## Accomplishments
- Created `cmd/villa/doctor.go` — `newDoctor`/`runDoctor`/`renderDoctor`/`renderDoctorTable`/`liveDoctorDeps`/`unitDirReadOnly`. The verb is a thin caller: `liveDoctorDeps` → `doctor.Aggregate` → `renderDoctor`; the worst-wins exit rollup mirrors `renderPreflight` exactly and REUSES the package-level `exitPass`/`exitWarn`/`exitBlocked` constants.
- Exit contract (DOCTOR-01 / Pitfall 1, load-bearing): a confident BLOCK-class FAIL → `exitBlocked`(1); any WARN/drift/typed-Unknown → `exitWarn`(2); all healthy → `exitPass`(0). Asserted by `TestDoctorExitCodes` — the residency-FAIL row asserts 1 and the drift row asserts 2 (NOT inverted).
- Read-only invariant (DOCTOR-03 / Pitfall 2 / D-03): `unitDirReadOnly` resolves the Quadlet unit dir WITHOUT the directory-creation step; `doctor.go` contains zero `MkdirAll` and never `WriteUnits`. `liveDoctorDeps`'s `DriftPlan` Renders from config and Reconciles only, returning the Plan (and any read error verbatim, so the core maps an absent unit dir to a typed-Unknown WARN — D-08).
- doctor's OWN byte-frozen contracts (DOCTOR-02 / D-02): `cmd/villa/testdata/doctor.json.golden` at `schema_version:1` (`Raw` excluded via `json:"-"`), plus `doctor-pass.golden`/`doctor-warn.golden` table fixtures. `status.Report`'s golden was untouched.
- Seam-clean (Pitfall 3 / T-13-02): no `Vulkan0`/`ROCm0`/`HSA_OVERRIDE`/image literals in `cmd/villa/doctor.go`; ROCm routed only via the core's `inference.IsROCmFamily`, backends resolved via `inference.BackendFor`. `TestSeamGrepGate` stays green.
- Registered `newDoctor()` on the root command tree (`cmd/villa/root.go`); `villa doctor --help` renders.

## Task Commits
1. **Task 1: doctor cobra verb + liveDoctorDeps + read-only unit-dir resolver + exit rollup** — `1a5cd3a` (feat)
2. **Task 2: TestDoctorExitCodes + TestDoctorJSON golden (schema_version 1)** — `7cf4f2f` (test)

**Plan metadata:** committed with this SUMMARY (docs).

## Files Created/Modified
- `cmd/villa/doctor.go` (created) — thin cobra caller: `newDoctor`, `runDoctor`, `renderDoctor` (worst-wins exit rollup), `renderDoctorTable`, `liveDoctorDeps` (reuses `liveStatusDeps` + no-WriteUnits `DriftPlan`), `unitDirReadOnly` (no directory creation).
- `cmd/villa/doctor_test.go` (created) — `TestDoctorExitCodes` (exit table over `healthyReport`/`driftReport`/`offloadFailReport` fixtures), `TestDoctorJSON` (frozen `--json` golden); reuses the shared `update` flag + `assertGolden` (no redeclaration).
- `cmd/villa/testdata/doctor.json.golden` (created) — byte-frozen `--json` contract, `schema_version:1`.
- `cmd/villa/testdata/doctor-pass.golden`, `doctor-warn.golden` (created) — frozen table renderings.
- `cmd/villa/root.go` (modified) — one-line `newDoctor()` registration alongside `newStatus()`.

## Decisions Made
- Followed the plan and 13-CONTEXT/13-RESEARCH decisions exactly. The single load-bearing correction (D-04/Pitfall 1, already resolved upstream) was honored: FAIL→exitBlocked(1), WARN/drift→exitWarn(2), never inverted.
- Reworded the two descriptive comments that mentioned the directory-creation API so `cmd/villa/doctor.go` contains the literal `MkdirAll` zero times — satisfying the read-only acceptance criterion against a grep-based check while keeping the comment intent (the resolver is the `quadletUnitDir` twin minus directory creation).

## Deviations from Plan
None — plan executed exactly as written. (The comment rewording above is a wording adjustment to satisfy the stated acceptance grep, not a behavior change.)

## Issues Encountered
None.

## User Setup Required
None — no external service configuration; no new dependencies (`go.mod` unchanged).

## Next Phase Readiness
- `villa doctor` is end-to-end and registered. Remaining phase work is the on-hardware UAT (gfx1151, per 13-VALIDATION Manual-Only Verifications): healthy install → exit 0; induced CPU fallback → exit 1 + residency-FAIL finding + remediation; hand-touched unit → drift WARN exit 2 + reconcile remediation.
- No blockers.

## Self-Check: PASSED
- FOUND: cmd/villa/doctor.go
- FOUND: cmd/villa/doctor_test.go
- FOUND: cmd/villa/testdata/doctor.json.golden
- FOUND: cmd/villa/testdata/doctor-pass.golden
- FOUND: cmd/villa/testdata/doctor-warn.golden
- FOUND commit: 1a5cd3a (Task 1, feat)
- FOUND commit: 7cf4f2f (Task 2, test)
- Verification: `make check` exit 0 (full suite green); `CGO_ENABLED=0 go build ./cmd/villa` Success; `go test ./internal/inference/ -run TestSeamGrepGate` green; `go test ./cmd/villa/ -run 'TestDoctorExitCodes|TestDoctorJSON'` green WITHOUT -update (golden frozen); `grep -c MkdirAll cmd/villa/doctor.go` = 0; 0 leaked marker literals; `grep -c newDoctor() cmd/villa/root.go` = 1; `villa doctor --help` renders.

---
*Phase: 13-villa-doctor-health-diagnosis*
*Completed: 2026-06-07*
