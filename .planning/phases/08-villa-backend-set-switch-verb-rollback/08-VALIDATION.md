---
phase: 8
slug: villa-backend-set-switch-verb-rollback
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-06
---

# Phase 8 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib testing + table-driven, matching existing `cmd/villa` and `internal/*` suites) |
| **Config file** | none — `go.mod` at repo root; no separate test config |
| **Quick run command** | `go test ./internal/backendswap/... ./cmd/villa/...` |
| **Full suite command** | `go test ./...` |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/backendswap/... ./cmd/villa/...`
- **After every plan wave:** Run `go test ./...`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

> Filled by the planner against the final task IDs. Every task that creates the rollback state-machine, the cutover gate, or the fit-guard MUST map to an automated `go test` command exercising injected `Deps` seams (no live hardware in unit tests). On-hardware behaviors (real ROCm offload, `load_tensors` hang) are listed under Manual-Only.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 08-01-01 | 01 | 1 | BSET-02 | T-8-01 / — | capture verbatim prior unit+config bytes before any mutation | unit | `go test ./internal/backendswap/...` | ❌ W0 | ⬜ pending |
| 08-01-02 | 01 | 1 | BSET-02 | T-8-02 / — | any prove/cutover failure restores verbatim captured unit+config (no-op to running stack) | unit | `go test ./internal/backendswap/...` | ❌ W0 | ⬜ pending |
| 08-02-01 | 02 | 2 | BSET-02 | — | cutover succeeds only on generation-probe + target ResidencyProof PASS; is-active/health-200 never sufficient | unit | `go test ./internal/backendswap/...` | ❌ W0 | ⬜ pending |
| 08-03-01 | 03 | 2 | BSET-01 | — | fit-guard re-checks memory envelope for target backend; preserves model/quant/context; refuse-with-remediation | unit | `go test ./internal/backendswap/...` | ❌ W0 | ⬜ pending |
| 08-04-01 | 04 | 3 | BSET-03 | — | `villa backend show` reports active backend; `--dry-run` previews without mutating config/units | unit | `go test ./cmd/villa/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/backendswap/backendswap_test.go` — table-driven tests for capture/prove/rollback over injected `Deps` seams (BSET-02)
- [ ] `cmd/villa/backend_test.go` — `villa backend show` / `set` / `--dry-run` command tests (BSET-01, BSET-03)
- [ ] Shared fakes for `orchestrate`, `inference` probe, `preflight.RunROCm`, and `detect.GPUBusyPercent` (mirror existing `cmd/villa/*_test.go` fake patterns)

*Existing `go test` infrastructure covers the framework; new test files above stub the new package + command surface.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real ROCm offload residency (`ROCm0` buffer line, gpu_busy>0, no `Memory access fault`) | BSET-02 | Requires gfx1151 Strix Halo hardware + ROCm 7.2.4 image | On hardware: `villa backend set rocm`, confirm cutover succeeds and `villa backend show` reports rocm with offloaded N/N layers |
| `load_tensors` hang → bounded-timeout → auto-rollback | BSET-02 | Hang only reproduces against a real degraded ROCm bring-up | On hardware: induce a known-bad ROCm config, run `villa backend set rocm`, confirm stack rolls back verbatim within timeout and inference unit re-readies |
| Silent CPU fallback detection via live `detect.GPUBusyPercent()` | BSET-02 | Live sysfs gpu_busy read only meaningful during real generation | On hardware: confirm a CPU-fallback bring-up is classified FAIL and rolled back |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
