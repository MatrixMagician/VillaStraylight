---
phase: 13
slug: villa-doctor-health-diagnosis
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-07
---

# Phase 13 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` (table-driven + byte goldens) |
| **Config file** | none (`go test`) |
| **Quick run command** | `go test ./internal/doctor/ ./cmd/villa/ -run Doctor` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~30 seconds (quick); ~90s (full suite) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/doctor/ ./cmd/villa/ -run Doctor`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** `make check` green AND `go test ./internal/inference/ -run TestSeamGrepGate` green
- **Max feedback latency:** ~30 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 13-01-01 | 01 | 1 | DOCTOR-01 | — | worst-wins exit rollup mirrors preflight constants (0/exitBlocked=1/exitWarn=2) | unit (table) | `go test ./cmd/villa/ -run TestDoctorExitCodes` | ❌ W0 (mirror `TestPreflightExitCodes`) | ⬜ pending |
| 13-01-02 | 01 | 1 | DOCTOR-01 | — | `--json` contract frozen | golden | `go test ./cmd/villa/ -run TestDoctorJSON` | ❌ W0 (new `doctor.json.golden`) | ⬜ pending |
| 13-01-03 | 01 | 1 | DOCTOR-02 | — | every non-healthy finding carries remediation text | unit | `go test ./internal/doctor/ -run TestRemediationPresent` | ❌ W0 | ⬜ pending |
| 13-01-04 | 01 | 1 | DOCTOR-02 | T-13 (false-green) | confident CPU fallback → BLOCK FAIL; never false-green over health-200 | unit | `go test ./internal/doctor/ -run TestOffloadFailDominatesHealth` | ❌ W0 | ⬜ pending |
| 13-01-05 | 01 | 1 | DOCTOR-03 | T-13 (read-only) | non-empty `Plan.Changed` → drift WARN; unit-dir read-only (no MkdirAll) | unit | `go test ./internal/doctor/ -run TestDriftWarn` | ❌ W0 | ⬜ pending |
| (invariant) | 01 | 1 | — | T-13 (seam leak) | no leaked backend literals in `cmd/villa`/`internal/doctor` | existing | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ exists | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/doctor/doctor_test.go` — core aggregation + worst-wins + remediation-present + offload-FAIL-dominates-health + drift-WARN
- [ ] `cmd/villa/doctor_test.go` — `TestDoctorExitCodes` (mirror `TestPreflightExitCodes`) + `TestDoctorJSON` golden
- [ ] `cmd/villa/testdata/doctor.json.golden` — new frozen `--json` contract (`schema_version:1`)
- [ ] Framework install: none (stdlib `testing` already in use)

*Existing infrastructure (Go `testing` + golden idiom + `TestSeamGrepGate`) covers the rest.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| On-hardware: `villa doctor` over a live, healthy gfx1151 install exits 0 | DOCTOR-01 | needs the real running stack (containers + GPU) | bring stack up, run `villa doctor`, assert exit 0 + all-healthy report |
| On-hardware: forced CPU-fallback backend → exit 1 BLOCK with remediation | DOCTOR-02 | needs a real degraded backend to prove no false-green | induce CPU fallback, run `villa doctor`, assert exit 1 + residency-FAIL finding + remediation |
| On-hardware: hand-touch a rendered unit, `villa doctor` reports drift WARN exit 2 | DOCTOR-03 | needs real on-disk units vs config | mutate a unit file, run `villa doctor`, assert drift WARN + reconcile remediation |

*Off-hardware automated tests cover the logic; on-hardware UAT confirms the live composition (consistent with prior phases' on-hardware verification).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
