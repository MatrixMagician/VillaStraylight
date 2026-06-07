---
phase: 12
slug: rocm-6-4-4-alternate-backend
status: approved
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-07
---

# Phase 12 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (Go stdlib `testing` — table-driven, golden fixtures; no third-party assert/mock) |
| **Config file** | none — `go.mod` toolchain (Go 1.26+), `make check` = vet + test |
| **Quick run command** | `go test ./internal/inference/... ./internal/preflight/... ./cmd/villa/...` |
| **Full suite command** | `make check` (`go vet ./...` + `go test ./...`) |
| **Estimated runtime** | ~30–60 seconds (off-hardware; live ROCm switch/bench are manual on-host) |

---

## Sampling Rate

- **After every task commit:** Run the quick command for the touched package(s) (always include `internal/inference` so `TestSeamGrepGate` + `TestROCmMarkerPresence` run).
- **After every plan wave:** Run `make check`.
- **Before `/gsd-verify-work`:** Full suite must be green.
- **Max feedback latency:** ~60 seconds (off-hardware). On-hardware switch/bench proof is a manual checkpoint (see Manual-Only Verifications).

---

## Per-Task Verification Map

> Populated/refined by the planner per PLAN.md task. Seed rows below anchor the
> non-negotiable invariants for ROCM-ALT-01.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 12-01-xx | 01 | 1 | ROCM-ALT-01 | — | `BackendFor("rocm-6.4.4")`/`...-rocwmma` resolve; unknown still errors (fail-closed) | unit | `go test ./internal/inference/...` | ❌ W0 | ⬜ pending |
| 12-01-xx | 01 | 1 | ROCM-ALT-01 | — | New image literals stay behind the seam (SC#4) | unit | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ | ⬜ pending |
| 12-02-xx | 02 | 2 | ROCM-ALT-01 | T-12-01 | ROCm preflight fires for ALL ROCm-family backends; floor failure → refuse-with-remediation (SC#2) | unit | `go test ./internal/preflight/... ./cmd/villa/...` | ✅ | ⬜ pending |
| 12-03-xx | 03 | 3 | ROCM-ALT-01 | — | `bench --ab` reports pp/tg separately for the new image pair (SC#3) | unit | `go test ./internal/bench/... ./cmd/villa/...` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Extend `internal/inference/seam_test.go` image regex (`+rocm-6\.4\.4`) in the SAME commit that introduces the literal — `TestSeamGrepGate` must stay green.
- [ ] New unit tests in `internal/inference` for `BackendFor` resolution of both new backends + the `IsROCmFamily` predicate.

*Existing infrastructure (`go test`, golden fixtures, the two grep-gates) otherwise covers all phase requirements.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `villa backend set rocm-6.4.4` switches transactionally with residency proof (SC#1) | ROCM-ALT-01 | Needs the live gfx1151 host (/dev/kfd, podman, real generation probe) | On host: `villa backend set rocm-6.4.4` → expect transactional switch + residency PASS; force a fault → expect verbatim rollback. |
| `villa bench --ab` proves which digest recovers Δtg (SC#3) | ROCM-ALT-01 | Real throughput numbers require the host GPU | On host: bench rocm-6.4.4 (and `-rocwmma`) vs rocm-7.2.4 / vulkan; confirm pp/tg deltas reported separately. |
| Digest re-verification of the rolling `rocm-6.4.4` tag | ROCM-ALT-01 | Upstream re-pushes the rolling tag | `skopeo inspect docker://docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4` — confirm digest matches the pinned `sha256:c81f30a7…` (and `-rocwmma` `sha256:9a97129a…`). |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 60s (off-hardware)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-06-07
