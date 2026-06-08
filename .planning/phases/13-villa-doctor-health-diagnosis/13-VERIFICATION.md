---
phase: 13-villa-doctor-health-diagnosis
verified: 2026-06-07T18:00:00Z
status: passed
score: 15/15 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: human_needed
  previous_score: 13/13
  gaps_closed:
    - "Live healthy gfx1151 install → exit 0 (closed by 13-03 residency-supersession; confirmed live in 13-UAT.md Test 1)"
    - "Live induced CPU fallback → exit 1 BLOCK no-false-green (confirmed live in 13-UAT.md Test 2)"
    - "Live hand-touched unit → drift WARN exit 2 (confirmed live in 13-UAT.md Test 3)"
    - "Residency-proven opt-in ROCm install reaches Overall=PASS without weakening no-false-green (gap-closure goal — verified in code + UAT)"
  gaps_remaining: []
  regressions: []
---

# Phase 13: `villa doctor` Health Diagnosis Verification Report

**Phase Goal:** Users can run a single `villa doctor` command to get an honest, read-only health diagnosis of a running install — composing the shipped preflight + status + residency-proof cores plus a config-vs-disk drift check — with actionable remediation for every non-healthy finding and a preflight-mirroring 0/2/1 exit contract.
**Verified:** 2026-06-07T18:00:00Z
**Status:** passed
**Re-verification:** Yes — after gap-closure plan 13-03 (residency-supersession + Option-B image gate) + code-review fix 00cb7e8, with all three prior human_needed UAT items now confirmed LIVE (13-UAT.md, 3/3 pass).

## Goal Achievement

### Observable Truths

| #   | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1 | `villa doctor` exits 0 healthy / 1 blocking FAIL / 2 WARN, mirroring AUTHORITATIVE preflight constants | ✓ VERIFIED | `renderDoctor` (cmd/villa/doctor.go:96-110) maps Overall FAIL→exitBlocked(1), WARN→exitWarn(2), else exitPass(0); `TestDoctorExitCodes` PASS asserts {healthy→0, drift→2, residency-FAIL→1, rocm-superseded→0} — NOT inverted |
| 2 | `villa doctor` verb registered + runnable | ✓ VERIFIED | `newDoctor()` registered in root.AddCommand; `go build ./cmd/villa` OK; live `./villa doctor` re-run by orchestrator → exit 0 post-fix |
| 3 | Every non-PASS Finding carries non-empty Remediation (DOCTOR-02) | ✓ VERIFIED | every non-PASS branch in doctor.go sets Remediation; `nonEmpty()` fallback (doctor.go:388); `TestRemediationPresent` PASS |
| 4 | A confident offload FAIL DOMINATES a HealthReady → Overall FAIL → exit 1 (no false-green over health-200) | ✓ VERIFIED | `offloadFinding` maps `inference.StatusFail`→tierBlock/statusFail (doctor.go:373-377); `TestOffloadFailDominatesHealth` PASS; LIVE Test 2 confirmed exit 1 + remediation over a health-200 |
| 5 | Config-vs-disk drift via render+`orchestrate.Reconcile` non-empty `Plan.Changed`→WARN (DOCTOR-03) | ✓ VERIFIED | doctor.go:240-249 emits drift WARN; `TestDriftWarn` PASS; LIVE Test 3 confirmed exit 2 + reconcile remediation |
| 6 | Down/not-installed stack → WARN (exit 2), never crash, never blocking FAIL | ✓ VERIFIED | `healthFinding` all branches stay tierWarn (doctor.go:333-354); absent unit dir → typed-Unknown WARN (cmd/villa/doctor.go:230-232); `TestDownStackWarnsNotBlocks` PASS |
| 7 | doctor owns its OWN `--json` golden (schema_version 1), does NOT extend status.Report | ✓ VERIFIED | `reportSchemaVersion=1` (doctor.go:54); `Report` is a distinct type; `doctor.json.golden` schema_version:1 frozen |
| 8 | Read-only: no MkdirAll, no WriteUnits, no generation probe | ✓ VERIFIED | `grep MkdirAll cmd/villa/doctor.go`=0; `unitDirReadOnly` resolves without creating; lone `os.Stat` (doctor.go:230) is read-only; code review §3 PASS |
| 9 | No backend marker literals leak from inference seam (TestSeamGrepGate) | ✓ VERIFIED | running image resolved ONLY via `inference.BackendFor(cfg.Backend).Image()` (cmd/villa/doctor.go:179-183); no literal; `TestSeamGrepGate` green (full run, 268 tests) |
| 10 | doctor.Aggregate folds preflight + status health + offload Verdict + drift into one worst-wins Report | ✓ VERIFIED | `Aggregate` (doctor.go:171-309) routes preflight via IsROCmFamily, folds health+offload+drift, worst-wins via statusRank |
| 11 | `--json` schema_version:1 emitted + frozen WITHOUT -update | ✓ VERIFIED | `TestDoctorJSON` PASS without `-update`; golden contains `"schema_version": 1` |
| 12 | Build + test gates green | ✓ VERIFIED | `CGO_ENABLED=0 go build ./cmd/villa` OK; `go test ./internal/doctor ./cmd/villa ./internal/inference` → 268 passed, exit 0 |
| 13 | Code review resolved — no unresolved BLOCKER | ✓ VERIFIED | `13-REVIEW.md` status: resolved (0 critical); WR-01/WR-02/IN-03 fixed in 00cb7e8; IN-01/IN-02 accepted with rationale |
| 14 | **Gap-closure:** a residency-proven opt-in-ROCm install reaches Overall=PASS / exit 0 (supersession down-ranks WARN host-prep, keeps it visible) | ✓ VERIFIED | Conjunction predicate `rocmResidencyProven && Status==statusWarn && supersededROCmHostPrepID` (doctor.go:284-285); `rocmResidencyProven` gated on `OffloadApplies && IsROCmFamily && StatusPass` (doctor.go:218, defends iota-0); `TestROCmResidencySupersedesHostPrepWARN` PASS (Overall==PASS, findings stay VISIBLE); LIVE Test 1 confirmed exit 0 (ROCm0 20583.34 MiB resident + sysfs GTT ≥ footprint) |
| 15 | **Gap-closure:** no-false-green is NOT weakened — a confident StatusFail on a SUPERSEDED ID still folds to FAIL; Option-B threads the RUNNING image so a denied running image is a confident FAIL | ✓ VERIFIED | predicate excludes statusFail by the `Status==statusWarn` clause; `TestConfidentROCmFAILStillDominatesResidency` PASS (confident FAIL on idROCmImage → Overall==FAIL, finding present as FAIL); `TestROCmResidencyDoesNotFireOnStatusFail` PASS (supersession does NOT fire without proven residency); Option B wires `RunROCmForImage` fail-closed on BackendFor error (cmd/villa/doctor.go:179-186); `TestLiveDoctorDepsWiresRunROCmImage` PASS |

**Score:** 15/15 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/doctor/doctor.go` | Pure core: Finding/Report/Deps/Aggregate + residency-supersession + Option-B RunROCmImage seam, schemaVersion=1 | ✓ VERIFIED | 394 lines; pure (no os.Exit/print/host I/O); supersession conjunction at :284-285; iota-0 defense at :218; `RunROCmImage` Deps field nil-safe |
| `internal/doctor/doctor_test.go` | Supersession + gating + no-false-green guard tests | ✓ VERIFIED | TestROCmResidencySupersedesHostPrepWARN, TestROCmResidencyDoesNotFireOnStatusFail, TestConfidentROCmFAILStillDominatesResidency — all PASS (3 passed, not skipped) |
| `cmd/villa/doctor.go` | cobra verb, liveDoctorDeps (Option-B image gate fail-closed), renderDoctor, read-only resolver | ✓ VERIFIED | image gate via BackendFor+RunROCmForImage, fail-closed (WR-01 fix); no MkdirAll/WriteUnits |
| `cmd/villa/doctor_test.go` | TestDoctorExitCodes (incl. rocm-superseded→exitPass), TestLiveDoctorDepsWiresRunROCmImage | ✓ VERIFIED | both PASS; exit table asserts rocm-superseded→0 |
| `cmd/villa/testdata/doctor-rocm-superseded.golden` | Frozen post-supersession render | ✓ VERIFIED | Present (684B); passes without `-update` |
| `cmd/villa/testdata/doctor.json.golden` | Frozen --json schema_version:1 | ✓ VERIFIED | Present; frozen |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| cmd/villa/doctor.go | internal/doctor.Aggregate | runDoctor → doctor.Aggregate | ✓ WIRED | doctor.go:69 |
| cmd/villa/doctor.go | preflight exit constants | renderDoctor reuses exitBlocked/exitWarn/exitPass | ✓ WIRED | doctor.go:105-109 |
| cmd/villa/doctor.go | inference.BackendFor(...).Image() | Option-B running-image thread-through, fail-closed | ✓ WIRED | doctor.go:179-186 |
| internal/doctor | preflight.RunROCmForImage | RunROCmImage Deps seam → image-aware host-prep gate | ✓ WIRED | doctor.go:183-185 (cmd) → doctor.go:179-183 (core) |
| internal/doctor | inference.StatusPass + OffloadApplies | rocmResidencyProven keying | ✓ WIRED | doctor.go:218 |
| cmd/villa/root.go | newDoctor() | root.AddCommand | ✓ WIRED | registered |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Build | `CGO_ENABLED=0 go build ./cmd/villa` | success | ✓ PASS |
| Full doctor+cmd+seam tests | `go test ./internal/doctor ./cmd/villa ./internal/inference` | 268 passed | ✓ PASS |
| Supersession guards | `go test ./internal/doctor -run 'TestROCmResidency...|TestConfidentROCmFAIL...'` | 3 passed (not skipped) | ✓ PASS |
| Exit + JSON goldens frozen | `go test ./cmd/villa -run 'TestDoctorExitCodes|TestDoctorJSON'` (no -update) | 6 passed | ✓ PASS |
| Gap-closure commits exist | `git log --oneline c1b9f6c 1e4e765 02265f3 00cb7e8` | all 4 present | ✓ PASS |

### On-Hardware UAT (live evidence — previously human_needed, now CONFIRMED)

All three prior human_needed items executed LIVE on the gfx1151 host (kernel 7.0.11-200.fc44, backend=rocm, model qwen3.6-35b-a3b UD-Q4_K_M, ctx 131072, stack up), recorded in 13-UAT.md (status: complete, 3/3 pass).

| # | Test | Expected | Live Result | Status |
| - | ---- | -------- | ----------- | ------ |
| 1 | Healthy ROCm install (post-fix) | exit 0, all-healthy, offload PASS over real residency | overall PASS / exit 0; ROCm0 20583.34 MiB resident + sysfs GTT ≥ weight footprint; all /health 200; drift PASS; ROCM-PRE-image PASS (Option B); firmware/HSA visible-but-down-ranked WARN | ✓ PASS |
| 2 | Induced CPU fallback (-ngl 0) | exit 1 BLOCK residency-FAIL + remediation, no false-green over health-200 | overall FAIL / exit 1; health:villa-llama PASS(200) but offload BLOCK FAIL dominates with no-false-green message + remediation; backend restored, residency re-proven | ✓ PASS |
| 3 | Hand-touched Quadlet unit | exit 2 drift WARN + reconcile remediation | drift WARN / exit 2 with reconcile remediation; restored clean | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| DOCTOR-01 | 13-01, 13-02, 13-03 | One-shot health diagnosis composing preflight+status+residency cores, preflight-mirroring exit contract; exit 0 = healthy reachable on opt-in ROCm | ✓ SATISFIED | Aggregate composition + renderDoctor exit map + residency-supersession (Truth 1,10,14); LIVE Test 1 exit 0 |
| DOCTOR-02 | 13-01, 13-02, 13-03 | Every non-healthy finding carries remediation; silent CPU fallback = FAIL (no false-green) — preserved across supersession | ✓ SATISFIED | TestRemediationPresent + TestOffloadFailDominatesHealth + TestConfidentROCmFAILStillDominatesResidency (Truth 3,4,15); LIVE Test 2 |
| DOCTOR-03 | 13-01, 13-02 | Detects/reports config-vs-disk drift | ✓ SATISFIED | TestDriftWarn + orchestrate.Reconcile wiring (Truth 5); LIVE Test 3 |

All three declared requirement IDs accounted for. REQUIREMENTS.md maps ONLY DOCTOR-01/02/03 to Phase 13 (lines 83-85, 105) — no orphaned requirements. 13-03 declares the subset {DOCTOR-01, DOCTOR-02} (the gap-closure scope: exit-0-on-ROCm + no-false-green); DOCTOR-03 was fully satisfied by 13-01/13-02 and untouched by the gap-closure — correct, not a gap.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| — | — | No TBD/FIXME/XXX debt markers in any changed phase file | ℹ️ Info | None |
| internal/doctor/doctor.go | 120-122 | `Deps.LoadConfig` populated but unread (IN-01) | ℹ️ Info | Reviewer accepted as documented reserved forward seam (stable Deps shape); not dead-by-accident |

No blocker anti-patterns. The supersession `continue` (doctor.go:290) skips rank contribution, NOT the append — findings stay visible (verified by TestROCmResidencySupersedesHostPrepWARN asserting hasFinding for all three IDs).

### Note on the inverted exit parenthetical

ROADMAP SC#1 / REQUIREMENTS.md DOCTOR-01 contain an inverted parenthetical. CONTEXT.md D-04 + 13-RESEARCH Pitfall 1 resolve it: "mirror the preflight exit contract" is BINDING, and the shipped constants (`exitBlocked=1`, `exitWarn=2`) are AUTHORITATIVE. The code implements blocking→1 / warning→2 correctly (LIVE Test 2 exit 1, Test 3 exit 2 confirm). Documented-correct resolution, NOT a gap.

### Human Verification Required

None remaining. All three previously human_needed on-hardware items were executed LIVE this session and recorded in 13-UAT.md (3/3 pass); the orchestrator independently re-ran `./villa doctor` → exit 0 post-fix. The automated logic (15/15 truths) and the live hardware evidence jointly satisfy the goal.

### Gaps Summary

No gaps. The pure `internal/doctor` core composes preflight + status health + offload Verdict + drift into a worst-wins Report; the cmd-tier verb maps Overall to the authoritative preflight exit constants (blocking→1, warning→2, healthy→0). The gap-closure goal is achieved in BOTH code and live UAT: a residency-proven opt-in-ROCm install now reaches Overall=PASS / exit 0 via a residency-supersession that down-ranks (but keeps visible) WARN-status ROCM-PRE-firmware/-hsa/-image findings — WITHOUT weakening no-false-green, because the down-rank predicate is the strict `(superseded-ID AND Status==statusWarn)` conjunction (a confident StatusFail on the same IDs still folds to FAIL, proven by TestConfidentROCmFAILStillDominatesResidency), the proven-residency key is gated on `OffloadApplies` to defend the iota-0 zero-value trap, and Option B threads the RUNNING image through the inference seam so a denied running image is a confident FAIL. Build + test gates green (268 tests), seam grep-gate green, read-only preserved, code review resolved with no unresolved BLOCKER, and all three prior human_needed on-hardware items now confirmed live (13-UAT.md 3/3).

---

_Verified: 2026-06-07T18:00:00Z_
_Verifier: Claude (gsd-verifier)_
