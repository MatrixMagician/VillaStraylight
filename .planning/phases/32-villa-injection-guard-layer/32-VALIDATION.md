---
phase: 32
slug: villa-injection-guard-layer
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-19
---

# Phase 32 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (standard library `testing`) |
| **Config file** | none — Go modules; `Makefile` targets |
| **Quick run command** | `go test ./internal/websafe/...` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~30–60 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/websafe/...`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 32-01-* | 01 | 1 | GUARD-02 | T-32-01 / — | active markup stripped + Unicode normalized before fencing | unit | `go test ./internal/websafe/...` | ❌ W0 | ⬜ pending |
| 32-02-* | 02 | 1 | GUARD-03 | T-32-02 / — | untrusted content wrapped in nonced provenance fence | unit | `go test ./internal/websafe/...` | ❌ W0 | ⬜ pending |
| 32-03-* | 03 | 2 | GUARD-03 | T-32-03 / — | heuristic classifier flags injections (flag-not-block); precision/recall must-WIN gate | unit | `go test ./internal/websafe/...` | ❌ W0 | ⬜ pending |
| 32-04-* | 04 | 2 | GUARD-04 | — | "reduces and flags, does not eliminate" copy; markdown-image residual documented; no "injection-safe" string | unit/grep | `go test ./internal/websafe/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky · Task IDs finalized by the planner.*

---

## Wave 0 Requirements

- [ ] `internal/websafe/*_test.go` — guard transform + classifier unit tests for GUARD-02/03/04
- [ ] `internal/websafe/testdata/` — adversarial injection corpus (invisible-Unicode + fence-breakout payloads + benign controls) backing the precision/recall must-WIN eval
- [ ] No framework install needed — `go test` is built in

*The injection-detection precision/recall eval is the must-WIN gate (suggested: recall ≥ 0.90, precision ≥ 0.95, ≥30 positive + ≥30 benign samples; thresholds frozen by the planner before implementation).*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| End-to-end: a real fetched page with an embedded injection reaches the chat model fenced + flagged | GUARD-02/03 | requires live OWUI + llama.cpp + villa-websafe on hardware | Enable web search, ask a question whose top result page contains an injection payload; confirm the answer treats it as data and the guard verdict surfaces honestly |

*Automated unit + corpus eval cover the transform/classifier logic; the on-hardware end-to-end is human verification.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
