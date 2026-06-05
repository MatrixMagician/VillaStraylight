---
phase: 6
slug: rocm-backend-resolver-spine
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-05
---

# Phase 6 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` (table tests + `testdata/` fixtures) |
| **Config file** | none — go test convention |
| **Quick run command** | `go test ./internal/inference/...` |
| **Full suite command** | `go vet ./... && go test ./...` |
| **Estimated runtime** | ~25 seconds (full suite); ~3 seconds (inference package) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/inference/...` (the blast-radius package)
- **After every plan wave:** Run `go test ./...` (proves the 7-site re-route is a no-op)
- **Before `/gsd-verify-work`:** `go vet ./... && go test ./...` must be green
- **Max feedback latency:** ~25 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 6-rocm-args | TBD | 1 | ROCM-01 | T-6 V12 | `/dev/kfd` device token present ONLY on ROCm backend args | unit | `go test ./internal/inference/ -run TestROCmContainerArgs` | ❌ W0 | ⬜ pending |
| 6-rocm-digest | TBD | 1 | ROCM-01 | T-6 V10 | Image is digest-pinned (`@sha256:`+64hex), never a bare tag | unit | `go test ./internal/inference/ -run TestROCmImageDigestPinned` | ❌ W0 | ⬜ pending |
| 6-backendfor | TBD | 1 | ROCM-01/04 | T-6 V5 | `"vulkan"`/`""`→Vulkan; unknown→actionable error (fail-closed) | unit | `go test ./internal/inference/ -run TestBackendFor` | ❌ W0 | ⬜ pending |
| 6-running-rocm | TBD | 1 | ROCM-02 | — | ROCm0 N/N→PASS, CPU-only→FAIL, fault→FAIL, empty→WARN | unit | `go test ./internal/inference/ -run TestRunningServerOffloadVerdict` | ⚠️ extend | ⬜ pending |
| 6-start-rocm | TBD | 1 | ROCM-02 | — | device+offloaded N/N→PASS, partial N<M→FAIL, CPU→FAIL | unit | `go test ./internal/inference/ -run TestScrapeOffloadLog` | ⚠️ extend | ⬜ pending |
| 6-vulkan-regress | TBD | 1 | ROCM-02 | — | Vulkan offload byte-identical after ResidencyProof refactor | unit | `go test ./internal/inference/` (existing Vulkan cases) | ✅ exists | ⬜ pending |
| 6-marker-gate | TBD | 1 | ROCM-02 | T-6 V1 | `ROCm0`/HSA/kfd markers present in backend_rocm.go | unit | `go test ./internal/inference/ -run TestROCmMarkerPresence` | ❌ W0 | ⬜ pending |
| 6-seam-gate | TBD | 1 | ROCM-02 | T-6 V1 | negative seam gate also covers the rocm image token | unit | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ verify | ⬜ pending |
| 6-noop-proof | TBD | 2 | ROCM-04 | — | 7 call sites compile + v1.0 suite green under Vulkan default | integration | `go test ./...` | ✅ exists | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/inference/backend_rocm.go` — the ROCm backend impl (covers ROCM-01)
- [ ] `internal/inference/backend_rocm_test.go` — ContainerArgs + digest-pin tests
- [ ] `internal/inference/testdata/load_tensors_rocm.txt` — ROCm0 N/N PASS (running path)
- [ ] `internal/inference/testdata/load_tensors_rocm_cpu.txt` — CPU-only FAIL
- [ ] `internal/inference/testdata/load_tensors_rocm_fault.txt` — "Memory access fault by GPU node" FAIL
- [ ] `internal/inference/testdata/rocm_devinfo_pass.stderr` — start-time device + offloaded N/N PASS
- [ ] `internal/inference/testdata/rocm_offloaded_partial.stderr` — N<M partial FAIL
- [ ] Extend `offload_test.go` + `running_offload_test.go` with ROCm marker-driven cases (Vulkan cases unchanged)
- [ ] `TestROCmMarkerPresence` positive grep-gate (new test)

*Note: ResidencyProof()-driven dual-scrape refactor (offload.go + running_offload.go) must keep Vulkan cases byte-identical — the regression test is the guard.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real ROCm offload (`gpu_busy_percent` during a live decode; absence of a live fault) | ROCM-02 | Requires real gfx1151 hardware + a running ROCm llama-server (off-hardware in Phase 6 by D-07) | Deferred to Phase 8 switch-verb on-hardware UAT; Phase 6 wires the signal as an INPUT and proves the verdict logic with fixtures |

*All Phase-6 pure logic has automated fixture-based verification; only the live hardware exercise is deferred (D-07).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
