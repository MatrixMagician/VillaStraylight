---
phase: 26
slug: agent-delivery-core-lockdown-launcher
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-13
---

# Phase 26 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` (table-driven + golden fixtures); no third-party assert/mocking — seams are injected `func` fields |
| **Config file** | none (`go test`) |
| **Quick run command** | `go test ./internal/agent/... ./cmd/villa/... -count=1` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~30 seconds (quick: ~5s `internal/agent`; full `make check`: ~30s) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/agent/... ./cmd/villa/... -count=1`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green; `TestSeamGrepGate` + `TestNoAutoFlipStructuralGuard` green
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 26-01-01 | 01 | 1 | AGENT-01 | T-26-01 / T-26-05 | checksum mismatch → refuse-with-remediation, no install (fail-closed); malformed embed panics at build-time, never a runtime attacker path | unit | `go test ./internal/agent/ -run 'TestPolicyLoad\|TestChecksumGate\|TestVersionCompare' -count=1` | ❌ W0 (`internal/agent/agent_test.go`) | ⬜ pending |
| 26-01-02 | 01 | 1 | AGENT-02 | T-26-02 / T-26-03 / T-26-04 | rendered values are fixed metachar-free literals (no `$(`/backtick/`${`); both kill switches; exactly one loopback openai-compat provider; non-empty villa- prefixed models[] | golden + unit | `go test ./internal/agent/ -run 'TestRenderGolden\|TestRenderContract\|TestLSPMissingWarn' -count=1` | ❌ W0 (`internal/agent/agent_test.go` + `internal/agent/testdata/crush.json.golden`) | ⬜ pending |
| 26-01-03 | 01 | 1 | AGENT-04 | T-26-06 | binary + config drift detected with remediation, never auto-corrected; sentinel binary hash → typed-Unknown WARN (not false drift) | unit + structural | `go test ./internal/agent/ -run 'TestBinaryDrift\|TestConfigDrift' -count=1 && go test ./internal/inference -run TestSeamGrepGate -count=1` | ❌ W0 (`internal/agent/agent_test.go`) | ⬜ pending |
| 26-02-01 | 02 | 2 | AGENT-01 / AGENT-04 | T-26-07 / T-26-08 / T-26-11 | install verifies SHA-256 BEFORE extraction (refuse-with-remediation on mismatch); extraction confined to villa bin (traversal guard); `agent.Run` config-absent → render+launch, drift → surface+exit (never auto-correct) | unit + structural | `go test ./internal/agent/ -run 'TestRun\|TestInstall' -count=1 && go test ./internal/inference -run TestSeamGrepGate -count=1` | ❌ W0 (`internal/agent/install_test.go`; extends `internal/agent/agent_test.go`) | ⬜ pending |
| 26-02-02 | 02 | 2 | AGENT-03 | T-26-09 / T-26-10 / T-26-12 / T-26-13 | three lockdown env vars set before exec; execs the EXPLICIT villa-owned path (no PATH hijack); fixed-arg `syscall.Exec` (no shell); no `CodingMode = ` literal (no-auto-flip guard) | unit + structural | `go test ./cmd/villa/ -run 'TestCode' -count=1 && go test ./cmd/villa -run TestNoAutoFlipStructuralGuard -count=1 && go test ./internal/inference -run TestSeamGrepGate -count=1` | ❌ W0 (`cmd/villa/code_test.go`) | ⬜ pending |
| 26-03-01 | 03 | 3 | AGENT-01 / AGENT-04 | T-26-14 / T-26-15 | pin the EXTRACTED-binary SHA-256 only from the SHA-256-verified tarball; record command evidence (no fabricated value); flip drift test to confident signal | unit | `go test ./internal/agent/ -run 'TestPolicyLoad\|TestBinaryDrift' -count=1 && make check` | ❌ W0 (extends `internal/agent/agent_test.go`) | ⬜ pending |
| 26-03-02 | 03 | 3 | AGENT-01 / AGENT-03 | T-26-16 | on-hardware `villa code` launch wired to local loopback inference; observe NO telemetry/autoupdate fetch; record honestly (pass or caveat) | manual | MANUAL — on-hardware checkpoint (see Manual-Only Verifications) | N/A (checkpoint) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/agent/agent_test.go` — policy load, checksum gate, version compare, render contract/golden, LSP WARN, drift (binary/config), `agent.Run` flow — covers AGENT-01/AGENT-02/AGENT-04
- [ ] `internal/agent/testdata/crush.json.golden` — rendered-config determinism golden (NEW append-only fixture) — covers AGENT-02
- [ ] `internal/agent/install_test.go` — verify-before-extract + traversal guard for the install seam — covers AGENT-01 (live half)
- [ ] `cmd/villa/code_test.go` — lockdown env, binary-absent remediation, drift surfacing, coding-mode-OFF WARN — covers AGENT-03
- [ ] Framework install: none — Go stdlib `testing` already in use.

*Regression anchors (already exist, must stay green — not new): `TestSeamGrepGate` (`internal/inference`), `TestNoAutoFlipStructuralGuard` (`cmd/villa`).*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| On-hardware `villa code` launch (26-03 Task 2) | AGENT-01 / AGENT-03 | Requires the live gfx1151 box: a real Crush binary installed at `$XDG_DATA_HOME/villa/bin/crush`, a rendered `~/.config/crush/crush.json`, and a serving villa-llama loopback endpoint — none reproducible off-hardware (mirrors the Phase-25 Task-3 on-hardware acceptance) | `make build`; install the verified binary + render config (first-run render path); run `villa code`; expect a coding-mode-OFF WARN (if chat mode) then Crush's TUI launches wired to `http://127.0.0.1:8080/v1` with the villa- model id, no drift/binary-absent; observe no telemetry/autoupdate fetch; record honestly (pass or caveat — a graceful refusal/WARN is itself a valid recorded outcome) |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (only 26-03-02 is manual; it is the terminal on-hardware acceptance checkpoint)
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 30s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-06-13
