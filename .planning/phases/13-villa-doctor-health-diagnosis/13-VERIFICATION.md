---
phase: 13-villa-doctor-health-diagnosis
verified: 2026-06-07T00:00:00Z
status: human_needed
score: 13/13 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  previous_score: n/a
human_verification:
  - test: "On a live, healthy gfx1151 install (stack up), run `villa doctor`"
    expected: "Exit 0; all-healthy findings report; offload PASS over real residency proof"
    why_human: "Requires the real running stack (containers + GPU) — off-hardware tests cover the fold logic only"
  - test: "Induce a CPU-fallback backend on real hardware, run `villa doctor`"
    expected: "Exit 1 (exitBlocked); a BLOCK-class residency-FAIL finding with actionable remediation; never a false-green over a health-200"
    why_human: "Requires a real degraded backend to prove the no-false-green path end-to-end"
  - test: "Hand-touch a rendered Quadlet unit on disk, run `villa doctor`"
    expected: "Exit 2 (exitWarn); a config-vs-disk drift WARN finding with reconcile remediation"
    why_human: "Requires real on-disk units diverging from the config source of truth"
---

# Phase 13: `villa doctor` Health Diagnosis Verification Report

**Phase Goal:** Users can run a single `villa doctor` command to get an honest, read-only health diagnosis of a running install — composing the shipped preflight + status + residency-proof cores plus a config-vs-disk drift check — with actionable remediation for every non-healthy finding and a preflight-mirroring 0/2/1 exit contract.
**Verified:** 2026-06-07
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| #   | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1 | `villa doctor` exits 0 healthy / 1 blocking FAIL / 2 WARN, mirroring AUTHORITATIVE preflight constants (exitPass=0, exitBlocked=1, exitWarn=2) | ✓ VERIFIED | `cmd/villa/preflight.go:20-23` defines the constants; `renderDoctor` (cmd/villa/doctor.go:95-109) maps Overall FAIL→exitBlocked(1), WARN→exitWarn(2), else exitPass(0); `TestDoctorExitCodes` PASS asserts {healthy→0, drift→2, residency-FAIL→1} — NOT inverted |
| 2 | `villa doctor` verb registered + runnable | ✓ VERIFIED | `cmd/villa/root.go:35` `newDoctor()` in AddCommand list; `go run ./cmd/villa doctor --help` renders usage |
| 3 | Every non-PASS Finding carries non-empty Remediation (DOCTOR-02) | ✓ VERIFIED | `internal/doctor/doctor.go` every non-PASS branch sets Remediation; `nonEmpty()` guarantees fallback; `TestRemediationPresent` PASS |
| 4 | A confident offload FAIL DOMINATES a HealthReady → Overall FAIL → exit 1 (no false-green over health-200) | ✓ VERIFIED | `offloadFinding` maps `inference.StatusFail`→tierBlock/statusFail (doctor.go:286-290); `TestOffloadFailDominatesHealth` PASS asserts Overall=="FAIL" + BLOCK-class FAIL finding over HealthReady |
| 5 | Config-vs-disk drift via render+`orchestrate.Reconcile` non-empty `Plan.Changed`→WARN (DOCTOR-03) | ✓ VERIFIED | doctor.go:181-190 emits drift WARN on `len(plan.Changed)>0`; independent of health; `TestDriftWarn` PASS asserts Overall=="WARN" |
| 6 | Down/not-installed stack → WARN (exit 2), never crash, never blocking FAIL (D-08, CR-01 fix) | ✓ VERIFIED | `healthFinding(HealthDown)`→statusWarn/tierWarn (doctor.go:257-260); `TestDownStackWarnsNotBlocks` PASS; verifier ad-hoc test confirms WARN-tier WARN → renderDoctor returns exitWarn(2) |
| 7 | doctor owns its OWN `--json` golden (schema_version 1), does NOT extend status.Report (D-02) | ✓ VERIFIED | `cmd/villa/testdata/doctor.json.golden` schema_version:1 last field; `internal/doctor.Report` is a distinct type; `git log internal/status/` last touched Phase 10 (untouched by Phase 13) |
| 8 | Read-only: no MkdirAll, no WriteUnits, no generation probe (D-03/Pitfall 2) | ✓ VERIFIED | `grep -c MkdirAll cmd/villa/doctor.go`=0; `unitDirReadOnly` resolves path without creating; WriteUnits appears only in a doc-comment; no model-load probe |
| 9 | No backend marker literals leak from inference seam (TestSeamGrepGate) | ✓ VERIFIED | `go test ./internal/inference/ -run TestSeamGrepGate` green; seam test walks BOTH internal/ and cmd/villa (seam_test.go:88-119); only comment-mentions of Vulkan0/ROCm0 (excluded by gate) |
| 10 | doctor.Aggregate folds preflight + status health + offload Verdict + drift Plan into one worst-wins Report | ✓ VERIFIED | `Aggregate` (doctor.go:127-222) routes preflight via IsROCmFamily, folds Services health+offload, drift, then worst-wins via statusRank |
| 11 | `--json` schema_version:1 emitted + frozen WITHOUT -update | ✓ VERIFIED | `TestDoctorJSON` PASS (no -update); golden contains `"schema_version": 1`; Raw field correctly absent (json:"-") |
| 12 | Build + test gates green | ✓ VERIFIED | `CGO_ENABLED=0 go build ./cmd/villa` OK; `make check` (vet + go test ./...) all packages ok |
| 13 | Code review resolved — no unresolved BLOCKER | ✓ VERIFIED | `13-REVIEW.md` status: resolved; CR-01 BLOCKER + WR-01/WR-02/IN-01 fixed in e9d4002; WR-03/IN-02 accepted-as-is with rationale |

**Score:** 13/13 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/doctor/doctor.go` | Pure core: Finding/Report/Deps/Aggregate, reportSchemaVersion=1 | ✓ VERIFIED | 307 lines; `func Aggregate`, SchemaVersion last field, const reportSchemaVersion=1; pure (no os.Exit/print/host I/O) |
| `internal/doctor/doctor_test.go` | Off-hardware table tests w/ stubbed Deps | ✓ VERIFIED | 5 tests incl. TestOffloadFailDominatesHealth, TestDownStackWarnsNotBlocks — all PASS |
| `cmd/villa/doctor.go` | cobra verb, liveDoctorDeps, renderDoctor, read-only resolver | ✓ VERIFIED | `func renderDoctor`/`liveDoctorDeps`/`unitDirReadOnly`; reuses exitPass/Warn/Blocked; no MkdirAll |
| `cmd/villa/doctor_test.go` | TestDoctorExitCodes + TestDoctorJSON | ✓ VERIFIED | Both PASS; exit table asserts FAIL→1, drift→2 (not inverted); does not redeclare `update` |
| `cmd/villa/testdata/doctor.json.golden` | Frozen --json, schema_version:1 | ✓ VERIFIED | Present; schema_version:1; frozen (passes without -update) |
| `cmd/villa/root.go` | `newDoctor()` registration | ✓ VERIFIED | Line 35 in AddCommand list |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| cmd/villa/doctor.go | internal/doctor.Aggregate | runDoctor → doctor.Aggregate | ✓ WIRED | doctor.go:68 |
| cmd/villa/doctor.go | preflight exit constants | renderDoctor reuses exitBlocked/exitWarn/exitPass | ✓ WIRED | doctor.go:104-108, constants from preflight.go |
| cmd/villa/doctor.go | internal/status | StatusReport: status.Run(*liveStatusDeps()) | ✓ WIRED | doctor.go:156-167 |
| cmd/villa/doctor.go | orchestrate.Reconcile | DriftPlan closure renders + reconciles read-only | ✓ WIRED | doctor.go:173-208; stat-guard avoids false "no longer match" on never-installed host (WR-01 fix) |
| internal/doctor | inference.IsROCmFamily | backend-keyed preflight routing | ✓ WIRED | doctor.go:134 |
| cmd/villa/root.go | newDoctor() | root.AddCommand | ✓ WIRED | root.go:35 |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Doctor verb runnable | `go run ./cmd/villa doctor --help` | Usage + read-only/0/2/1 description | ✓ PASS |
| Exit contract (healthy/drift/fail) | `go test ./cmd/villa/ -run TestDoctorExitCodes` | 0/2/1 all PASS | ✓ PASS |
| Down-stack folds to exit 2 (CR-01) | ad-hoc renderDoctor over WARN-tier WARN | exit 2 (exitWarn) | ✓ PASS |
| JSON golden frozen | `go test ./cmd/villa/ -run TestDoctorJSON` (no -update) | PASS | ✓ PASS |
| Seam grep gate | `go test ./internal/inference/ -run TestSeamGrepGate` | green | ✓ PASS |
| Full gate | `make check` | all packages ok | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| DOCTOR-01 | 13-01, 13-02 | One-shot health diagnosis composing preflight+status+residency cores, preflight-mirroring exit contract | ✓ SATISFIED | renderDoctor exit map + TestDoctorExitCodes; Aggregate composition |
| DOCTOR-02 | 13-01, 13-02 | Every non-healthy finding carries remediation; silent CPU fallback = FAIL (no false-green) | ✓ SATISFIED | TestRemediationPresent + TestOffloadFailDominatesHealth |
| DOCTOR-03 | 13-01, 13-02 | Detects/reports config-vs-disk drift | ✓ SATISFIED | TestDriftWarn; orchestrate.Reconcile drift wiring |

All three declared requirement IDs accounted for; no orphaned requirements (REQUIREMENTS.md maps only DOCTOR-01/02/03 to Phase 13, all claimed by both plans).

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| — | — | No TBD/FIXME/XXX debt markers in phase files | ℹ️ Info | None |
| internal/doctor/doctor.go | 99 | `Deps.LoadConfig` populated but unread (WR-03) | ℹ️ Info | Reviewer accepted as documented reserved forward seam; not dead-by-accident |

No blocker anti-patterns. The `return null/[]/{}` and empty-value greps surface only in test fixtures and typed-Unknown initial values that are deliberately overwritten — not user-facing stubs.

### Note on the inverted exit parenthetical

ROADMAP SC#1 and REQUIREMENTS.md DOCTOR-01 contain an inverted parenthetical
("`2` (blocking fault) / `1` (warning)"). This is a known documentation artifact.
CONTEXT.md D-04 + 13-RESEARCH Pitfall 1 explicitly resolve it: "mirror the preflight
exit contract" is the BINDING constraint, and the shipped preflight constants
(`exitBlocked=1`, `exitWarn=2`) are AUTHORITATIVE. The code correctly implements
blocking→1 / warning→2. This is the documented-correct resolution, NOT a gap.

### Human Verification Required

The phase goal references a "health diagnosis of a *running* install." Off-hardware
automated tests fully cover the fold/exit/drift logic. Live composition over a real
gfx1151 stack is deliberately out of scope for automated verification (consistent with
prior phases) and captured as manual-only items in 13-VALIDATION.md:

1. **Healthy live install → exit 0** — bring the stack up, run `villa doctor`, assert exit 0 + all-healthy report.
2. **Forced CPU fallback → exit 1 BLOCK** — induce a degraded backend, assert exit 1 + residency-FAIL finding + remediation (no false-green).
3. **Hand-touched unit → drift WARN exit 2** — mutate a rendered unit, assert drift WARN + reconcile remediation.

### Gaps Summary

No gaps. All 13 must-haves are verified in the codebase: the pure `internal/doctor`
core composes preflight + status health + offload Verdict + drift into a worst-wins
Report; the cmd-tier verb maps Overall to the authoritative preflight exit constants
(blocking→1, warning→2, healthy→0, not inverted); every non-PASS finding carries
remediation; a confident offload FAIL dominates a health-200 (no false-green); drift is
detected via render+Reconcile; a down stack degrades to WARN (CR-01 fixed, regression-
tested); doctor owns its own frozen schema_version:1 golden without touching
status.Report; the verb is strictly read-only (no MkdirAll/WriteUnits/probe); the seam
grep-gate stays green; and the code review is resolved with no unresolved BLOCKER.

Status is `human_needed` solely because the live-hardware UAT items above require a
running gfx1151 stack — the automated logic is complete and green.

---

_Verified: 2026-06-07_
_Verifier: Claude (gsd-verifier)_
