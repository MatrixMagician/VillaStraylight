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
| 6-rocm-args | 02 | 2 | ROCM-01 | T-6 V12 | `/dev/kfd` device token present ONLY on ROCm backend args | unit | `go test ./internal/inference/ -run TestROCmContainerArgs` | ❌ W0 | ⬜ pending |
| 6-rocm-digest | 02 | 2 | ROCM-01 | T-6 V10 | Image is digest-pinned (`@sha256:`+64hex), never a bare tag | unit | `go test ./internal/inference/ -run TestROCmImageDigestPinned` | ❌ W0 | ⬜ pending |
| 6-backendfor | 02 | 2 | ROCM-01/04 | T-6 V5 | `"vulkan"`/`""`→Vulkan; unknown→actionable error (fail-closed) | unit | `go test ./internal/inference/ -run TestBackendFor` | ❌ W0 | ⬜ pending |
| 6-running-rocm | 02 | 2 | ROCM-02 | — | ROCm0 N/N→PASS, CPU-only→FAIL, fault→FAIL, empty→WARN | unit | `go test ./internal/inference/ -run TestRunningServerOffloadVerdict` | ⚠️ extend | ⬜ pending |
| 6-gpu-busy | 01/02 | 1/2 | ROCM-02 (D-06) | T-6-09 | gpu_busy_percent signal folds via combineOffload: Known non-zero corroborates PASS, Known-zero→FAIL, absent/Unknown neutral-for-PASS (never false-FAIL); driven by detect.GPUBusyPercentForTest | unit | `go test ./internal/inference/ -run TestRunningServerOffloadVerdict` | ⚠️ extend (busy cases) | ⬜ pending |
| 6-start-rocm | 02 | 2 | ROCM-02 | — | device+offloaded N/N→PASS, partial N<M→FAIL, CPU→FAIL | unit | `go test ./internal/inference/ -run TestScrapeOffloadLog` | ⚠️ extend | ⬜ pending |
| 6-vulkan-regress | 01 | 1 | ROCM-02 | T-6-02 | Vulkan offload byte-identical after ResidencyProof refactor (incl. absent/Unknown busy neutral-for-PASS) | unit | `go test ./internal/inference/` (existing Vulkan cases) | ✅ exists | ⬜ pending |
| 6-marker-gate | 02 | 2 | ROCM-02 | T-6 V1 | `ROCm0`/HSA/kfd markers present in backend_rocm.go | unit | `go test ./internal/inference/ -run TestROCmMarkerPresence` | ❌ W0 | ⬜ pending |
| 6-seam-gate | 02 | 2 | ROCM-02 | T-6 V1 | negative seam gate also covers the rocm image token | unit | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ verify | ⬜ pending |
| 6-noop-proof | 03 | 3 | ROCM-04 | — | 8 call sites compile + v1.0 suite green under Vulkan default | integration | `go test ./...` | ✅ exists | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

> Note: `6-gpu-busy` spans Plan 06-01 (the `gpuBusyFloor` helper + `RunningOffloadInput.GPUBusyPercent`
> field + the Known-zero→FAIL and absent/Unknown-neutral unit proofs) and Plan 06-02 (the ROCm
> PASS-corroborate + Unknown-neutral fixture cases via `detect.GPUBusyPercentForTest`). The LIVE
> decode-time read (Known-zero during an active decode → confident FAIL) is the Phase-8 follow-on
> per CONTEXT D-07 — NOT validated off-hardware here.

---

## Wave 0 Requirements

- [ ] `internal/inference/backend_rocm.go` — the ROCm backend impl (covers ROCM-01)
- [ ] `internal/inference/backend_rocm_test.go` — ContainerArgs + digest-pin tests
- [ ] `internal/inference/testdata/load_tensors_rocm.txt` — ROCm0 N/N PASS (running path)
- [ ] `internal/inference/testdata/load_tensors_rocm_cpu.txt` — CPU-only FAIL
- [ ] `internal/inference/testdata/load_tensors_rocm_fault.txt` — "Memory access fault by GPU node" FAIL
- [ ] `internal/inference/testdata/rocm_devinfo_pass.stderr` — start-time device + offloaded N/N PASS
- [ ] `internal/inference/testdata/rocm_offloaded_partial.stderr` — N<M partial FAIL
- [ ] `RunningOffloadInput.GPUBusyPercent detect.Int` field + `gpuBusyFloor` helper folded via combineOffload (Plan 06-01 Task 2; D-06 gpu_busy_percent signal — Known non-zero corroborate, Known-zero FAIL, absent/Unknown neutral-for-PASS)
- [ ] gpu_busy_percent ROCm fixture cases via `detect.GPUBusyPercentForTest` against a temp drmRoot (Plan 06-02 Task 3; PASS-corroborate + Unknown-neutral — live decode-time read deferred to Phase 8)
- [ ] Extend `offload_test.go` + `running_offload_test.go` with ROCm marker-driven cases (Vulkan cases unchanged)
- [ ] `TestROCmMarkerPresence` positive grep-gate (new test)

*Note: ResidencyProof()-driven dual-scrape refactor (offload.go + running_offload.go) must keep Vulkan cases byte-identical — the regression test is the guard. The gpu_busy_percent signal must be combine-neutral on an absent/Unknown reading so Vulkan (which supplies no busy reading) stays byte-identical.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real ROCm offload — non-zero `gpu_busy_percent` during a LIVE decode (Known-zero-during-decode → confident FAIL); absence of a live fault | ROCM-02 (D-06) | Requires real gfx1151 hardware + a running ROCm llama-server mid-generation (off-hardware in Phase 6 by D-07; Phase 6 wires the busy signal as an INPUT and proves PASS-corroborate / Known-zero-FAIL / Unknown-neutral with fixtures) | Deferred to Phase 8 switch-verb on-hardware UAT: read `GPUBusyPercent()` from cmd/villa during an active decode and assert the residency verdict reflects the live busy reading |

*All Phase-6 pure logic (incl. the gpu_busy_percent fold) has automated fixture-based verification; only the live decode-time hardware exercise is deferred (D-07).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
