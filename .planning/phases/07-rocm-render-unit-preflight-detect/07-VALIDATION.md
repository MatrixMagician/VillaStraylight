---
phase: 7
slug: rocm-render-unit-preflight-detect
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-06
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

> Task IDs are placeholders until the planner finalizes plan/wave numbering. The
> contract below maps each requirement to its automated proof.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 7-RENDER | render | 1 | ROCM-03 | — | ROCm unit renders kfd+dri devices, render group, ordered HSA/ROCBLAS env, rocm image; Vulkan golden byte-identical | golden | `go test ./internal/orchestrate/ -run TestRender` | ✅ | ⬜ pending |
| 7-PARSER | render | 1 | ROCM-03 | — | parseContainerArgs collects multiple --device, second --group-add, and --env flags (no silent drop) | unit | `go test ./internal/orchestrate/ -run TestParseContainerArgs` | ❌ W0 | ⬜ pending |
| 7-POLICY | preflight | 1 | PRE-06 | T-7-01 | go:embed rocm-policy.json loads ranges + denylists; migrated v1.0 floors are a behavior no-op | unit/golden | `go test ./internal/preflight/` | ✅ | ⬜ pending |
| 7-RUNROCM | preflight | 2 | PRE-06 | T-7-01 | RunROCm: known-bad→FAIL (firmware 20251125, nightlies, kernel<6.18.4, no HSA, non-gfx1151); missing signal→WARN | unit | `go test ./internal/preflight/ -run TestRunROCm` | ❌ W0 | ⬜ pending |
| 7-PFCLI | preflight | 2 | PRE-06 | — | `villa preflight --backend rocm` renders ROCm CheckResult table off-hardware | cli/unit | `go test ./cmd/villa/ -run TestPreflight` | ✅ | ⬜ pending |
| 7-DETECT | detect | 2 | DET-04 | — | nested rocm_readiness object appended; schema 1→2; existing fields/order unchanged; typed-Optional null-when-unknown | golden | `go test ./internal/detect/ ./cmd/villa/ -run Detect` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/orchestrate/testdata/villa-llama-rocm.container.golden` (or equivalent) — NEW ROCm byte-golden stub
- [ ] `internal/preflight/rocm-policy.json` — embedded policy data fixture
- [ ] ROCm preflight fixture profiles (synthetic `HostProfile`s: clean-host PASS, firmware-20251125 FAIL, nightlies FAIL, kernel<floor FAIL, missing-rocminfo WARN)
- [ ] `cmd/villa/testdata/detect.golden.json` — re-frozen with appended `rocm_readiness` + schema 2

*Existing infrastructure (`go test`, table-test + golden patterns) covers the rest — no framework install needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live ROCm offload / HSA-override runtime behavior | (Phase 8) | Requires real gfx1151 hardware | Deferred to Phase 8 on-hardware UAT — NOT in Phase 7 scope |

*All Phase-7 behaviors have automated verification (off-hardware by design).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (new goldens + policy JSON + fixtures)
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter (planner/executor sets when map is complete)

**Approval:** pending
