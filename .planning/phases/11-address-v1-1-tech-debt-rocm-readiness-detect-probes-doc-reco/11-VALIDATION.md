---
phase: 11
slug: address-v1-1-tech-debt-rocm-readiness-detect-probes-doc-reco
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-06
---

# Phase 11 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` (table tests + `-update` goldens) |
| **Config file** | none — `go test` discovers `*_test.go` |
| **Quick run command** | `go test ./internal/detect/...` |
| **Full suite command** | `go test ./...` |
| **Estimated runtime** | ~30 seconds (full suite) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/detect/...`
- **After every plan wave:** Run `go test ./internal/detect/... ./cmd/villa/... ./internal/status/... ./internal/recommend/...`
- **Before `/gsd-verify-work`:** `go test ./...` must be green
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 11-01-xx | 01 | 1 | DET-04 (firmware Known-good) | — | Known clear date → `KnownBool(true)` | unit | `go test ./internal/detect/ -run TestFirmwareDate` | ❌ W0 | ⬜ pending |
| 11-01-xx | 01 | 1 | DET-04 (firmware Known-bad) | — | deny `20251125` / sub-floor → `KnownBool(false)` | unit | `go test ./internal/detect/ -run TestFirmwareDate` | ❌ W0 | ⬜ pending |
| 11-01-xx | 01 | 1 | DET-04 (firmware Unknown) | — | rpm absent/unparseable → UNSET (no-false-green) | unit | `go test ./internal/detect/ -run TestFirmwareDate` | ❌ W0 | ⬜ pending |
| 11-01-xx | 01 | 1 | DET-04 (HSA Known-good) | — | gfx1151 + substrate → `KnownBool(true)` | unit | `go test ./internal/detect/ -run TestHSAOverride` | ❌ W0 | ⬜ pending |
| 11-01-xx | 01 | 1 | DET-04 (HSA Known-bad) | — | non-gfx1151 → `KnownBool(false)` | unit | `go test ./internal/detect/ -run TestHSAOverride` | ❌ W0 | ⬜ pending |
| 11-01-xx | 01 | 1 | DET-04 (HSA Unknown) | — | gfx-id Unknown → UNSET (no-false-green) | unit | `go test ./internal/detect/ -run TestHSAOverride` | ❌ W0 | ⬜ pending |
| 11-01-xx | 01 | 1 | D-04 (golden byte-identical) | — | `villa detect --json` over fixture == golden | golden | `go test ./cmd/villa/ -run TestJSONGolden` | ✅ exists (no `-update`) | ⬜ pending |
| 11-01-xx | 01 | 1 | D-04 (off-hardware UNSET) | — | fixture Unknowns → both probes UNSET | unit | `go test ./internal/detect/ -run TestComputeROCmReadinessOffHardware` | ✅ extend | ⬜ pending |
| 11-01-xx | 01 | 1 | DASH-06 SC#1 (badge fold) | — | all-Known-good → `ready` | unit | `go test ./internal/status/ -run TestFoldROCmReadiness` | ✅ exists | ⬜ pending |
| 11-02-xx | 02 | 2 | D-05 (doc cross-check) | — | 6 SUMMARYs tagged, REVIEW prose fixed | grep | `grep -rl requirements-completed .planning/phases/{07,08,09,10}-*` | manual/grep | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/detect/readiness_rocm_test.go` — add `TestFirmwareDateOK` (Known-good / Known-bad-deny / Known-bad-subfloor / Unknown) and `TestHSAOverrideViable` (gfx1151+substrate / non-gfx1151 / Unknown) table cases — covers DET-04 probe wiring.
- [ ] `internal/detect/readiness_rocm_test.go` — extend `TestComputeROCmReadinessOffHardware` to assert both new probes stay UNSET when their source facts are Unknown (no-false-green regression guard).
- No framework install needed (Go stdlib `testing`). No new test file required — extend existing `readiness_rocm_test.go`.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live ROCm readiness badge reads `ready` (non-`unknown`) | DASH-06 SC#1 residual | Probe values are host-dependent; only a live gfx1151 ROCm host produces real Known firmware/HSA facts | On the gfx1151 host with `backend=rocm`: `villa detect --json` shows `firmware_date_ok:true` + `hsa_override_viable:true`; `villa status` / dashboard ROCm-readiness badge reads `ready`. UAT-gated like Phases 8–10. |
| REQUIREMENTS.md ROCM-02 note edit | D-05 (Open Q1) | Research found the audit's "stale note at line ~88" does not exist as described — ROCM-02 entries (line 19, 104) are accurate | Human confirms intent before any edit; if no stale note exists, record "no edit needed" rather than inventing one. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
</content>
