---
phase: 7
slug: rocm-render-unit-preflight-detect
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-06
validated: 2026-06-06
---

# Phase 7 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Phase 7 is OFF-HARDWARE — every requirement is verifiable with `go test`
> (pure render/parse/verdict logic + byte-goldens + JSON round-trip). No live
> ROCm host needed; that is Phase 8.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (Go stdlib testing) |
| **Config file** | none — `go.mod` at repo root |
| **Quick run command** | `go test ./internal/orchestrate/ ./internal/preflight/ ./internal/detect/ ./cmd/villa/` |
| **Full suite command** | `go test ./...` |
| **Estimated runtime** | ~30–60 seconds |

---

## Sampling Rate

- **After every task commit:** Run the quick command for the touched package (e.g. `go test ./internal/preflight/`)
- **After every plan wave:** Run `go test ./...`
- **Before `/gsd-verify-work`:** Full suite must be green AND both goldens frozen (Vulkan unchanged, ROCm new)
- **Max feedback latency:** ~60 seconds

---

## Per-Task Verification Map

> Task IDs reconciled to executed plans (07-01 render, 07-02 preflight, 07-03
> detect). Test names are the actual committed functions — the draft's `-run`
> patterns were pre-execution placeholders. All green as of the 2026-06-06
> audit (`go test ./...` — 481 tests, 17 packages).

| Task ID | Plan | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 7-RENDER | 07-01 | ROCM-03 | — | ROCm unit renders kfd+dri devices, render group, ordered HSA/ROCBLAS env, rocm image; Vulkan golden byte-identical | golden | `go test ./internal/orchestrate/ -run 'TestRenderROCmContainerGolden\|TestRenderROCmEnvGroupFrozen'` | ✅ | ✅ green |
| 7-PARSER | 07-01 | ROCM-03 | — | parseContainerArgs collects multiple --device, second --group-add, and --env flags (no silent drop) | unit | `go test ./internal/orchestrate/ -run TestParseContainerArgsMultiValue` | ✅ | ✅ green |
| 7-POLICY | 07-02 | PRE-06 | T-7-01 | go:embed rocm-policy.json loads floors + denylists; migrated v1.0 floors are a behavior no-op | unit/golden | `go test ./internal/preflight/ -run 'TestLoadROCmPolicyMatchesV1Floors\|TestFloorsSourcedFromPolicy\|TestPolicyCarriesROCmDenylists'` | ✅ | ✅ green |
| 7-RUNROCM | 07-02 | PRE-06 | T-7-01 | RunROCm: known-bad→FAIL (firmware 20251125, nightlies, kernel<6.18.4, no HSA, non-gfx1151); missing signal→WARN; off-hardware zero-FAIL over-block guard | unit | `go test ./internal/preflight/ -run TestRunROCm` | ✅ | ✅ green |
| 7-PFCLI | 07-02 | PRE-06 | — | `villa preflight --backend rocm` renders ROCM-PRE-* table off-hardware (exit 2 WARN, never blocked); standalone path unchanged | cli/unit | `go test ./cmd/villa/ -run 'TestPreflightBackendROCmOffHardware\|TestPreflightStandalonePathUnchanged'` | ✅ | ✅ green |
| 7-DETECT | 07-03 | DET-04 | T-7-01/T-7-02 | nested rocm_readiness object appended; schema 1→2; existing fields/order unchanged; typed-Optional UNSET-when-unknown (no false-green) | golden+unit | `go test ./internal/detect/ -run 'ROCmReadiness\|KernelFloor\|ImagePolicy\|RoundTrip' && go test ./cmd/villa/ -run TestJSONGolden` | ✅ | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [x] `internal/orchestrate/testdata/villa-llama-rocm.container.golden` — NEW ROCm byte-golden (frozen, `TestRenderROCmContainerGolden` green)
- [x] `internal/preflight/rocm-policy.json` — embedded policy data fixture (`go:embed`, `policy_test.go` proves v1.0 no-op)
- [x] ROCm preflight fixture profiles (synthetic `HostProfile`s in `checks_rocm_test.go`: per-signal known-bad→FAIL / unknown→WARN + bare-profile zero-FAIL over-block guard)
- [x] `cmd/villa/testdata/detect.golden.json` — re-frozen with appended `rocm_readiness` + schema 2 (verified present; `TestJSONGolden` green)

*Existing infrastructure (`go test`, table-test + golden patterns) covers the rest — no framework install needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live ROCm offload / HSA-override runtime behavior | (Phase 8) | Requires real gfx1151 hardware | Deferred to Phase 8 on-hardware UAT — NOT in Phase 7 scope |

*All Phase-7 behaviors have automated verification (off-hardware by design).*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (new goldens + policy JSON + fixtures)
- [x] No watch-mode flags
- [x] Feedback latency < 60s
- [x] `nyquist_compliant: true` set in frontmatter (planner/executor sets when map is complete)

**Approval:** validated 2026-06-06 — all 6 tasks green, zero gaps.

---

## Validation Audit 2026-06-06

State A audit of the pre-execution draft against the executed implementation
(plans 07-01/02/03, all `requirements-completed` confirmed). Every requirement
already had committed, green automated tests — no gaps to fill, no auditor
spawn, no new test files generated. The audit reconciled placeholder task/test
names to the actual committed functions and froze status to green.

| Metric | Count |
|--------|-------|
| Requirements audited | 3 (ROCM-03, PRE-06, DET-04) |
| Tasks mapped | 6 |
| COVERED (green) | 6 |
| PARTIAL | 0 |
| MISSING (gaps found) | 0 |
| Tests generated | 0 (already present) |
| Escalated | 0 |
| Full suite | `go test ./...` → 481 tests, 17 packages, all PASS |
