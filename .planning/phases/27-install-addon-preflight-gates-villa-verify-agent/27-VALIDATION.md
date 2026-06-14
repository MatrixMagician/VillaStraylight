---
phase: 27
slug: install-addon-preflight-gates-villa-verify-agent
status: approved
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-14
---

> **Wave 0 model:** No separate Wave 0 plan — test-first is folded into every implementation task (all of 27-01/02/03 are `tdd=true`: each writes its test FIRST and carries an `<automated>` verify command). `wave_0_complete` stays `false` until execution writes those tests; the validation *strategy* is nyquist-compliant (every requirement has a mapped automated verify, no 3-consecutive-task gap). Ratified 2026-06-14.

# Phase 27 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution. Derived from 27-RESEARCH.md § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` (table-driven, `httptest`, golden fixtures) — the only framework (CLAUDE.md) |
| **Config file** | none (Go toolchain) |
| **Quick run command** | `go test ./cmd/villa/ ./internal/agent/ -count=1` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~30–60 seconds (full suite) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./cmd/villa/ ./internal/agent/ -count=1`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite green + on-hardware acceptance (PRIV-06 is on-hardware by nature, like verify-memory)
- **Max feedback latency:** ~60 seconds

---

## Per-Task Verification Map

| Req ID | Behavior | Test Type | Automated Command | File Exists |
|--------|----------|-----------|-------------------|-------------|
| INSTALL-03 | `--coding-agent` gate + agent-off byte-identical render | unit | `go test ./cmd/villa/ -run TestInstall -count=1` | ❌ Wave 0 (extend install_test.go) |
| INSTALL-03 | coder-shard resolved from picked entry == served `-m` (single source, D-04) | unit | `go test ./cmd/villa/ -run TestCoderShardSingleSource -count=1` | ❌ Wave 0 (mirror TestEmbedGGUFFilenameSingleSource) |
| INSTALL-03 | `evalAgentProof` PASS only on a real tool-call edit; FAIL on health-200/no-edit | unit | `go test ./cmd/villa/ -run TestEvalAgentProof -count=1` | ❌ Wave 0 |
| INSTALL-04 | disk BLOCK / envelope BLOCK / cloud-cred WARN; typed-Unknown→WARN | unit | `go test ./cmd/villa/ -run TestAgentPreflight -count=1` | ❌ Wave 0 |
| INSTALL-04 | uninstall removes binary + crush.json (ordered); config.toml left; GGUF via flag | unit | `go test ./cmd/villa/ -run TestUninstall -count=1` | ✅ extend uninstall_test.go |
| PRIV-06 | `evalAgentVerify` negative-control-FIRST: egress-open→FAIL, blocked task→PASS, llama-down answer→FAIL | unit | `go test ./cmd/villa/ -run TestEvalAgentVerify -count=1` | ❌ Wave 0 (mirror evalRagSmoke tests) |
| INSTALL-03/PRIV-06 | real `crush run` tool-call round-trip + egress block + llama-down | on-hardware | manual acceptance (gfx1151 box) | ❌ on-hardware checkpoint plan |
| all | seam-gate green (no leaked literal in cmd/villa) | unit | `go test ./internal/inference/ -run TestSeamGrepGate -count=1` | ✅ exists |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `cmd/villa/install_agent_test.go` — covers INSTALL-03 (gate, pre-stage seam, single-source filename, readiness verdict `evalAgentProof`)
- [ ] `cmd/villa/verify_agent_test.go` — covers PRIV-06 (`evalAgentVerify` negative-control-first table; mirror `evalRagSmoke` tests)
- [ ] `cmd/villa/preflight_agent_test.go` (or fold into install_agent_test.go) — covers INSTALL-04 preflight tiers (disk/envelope BLOCK, cloud-cred WARN, typed-Unknown→WARN)
- [ ] Extend `cmd/villa/uninstall_test.go` — agent teardown ordering + config.toml-left invariant + GGUF-via-flag
- [ ] Extend `cmd/villa/install_test.go` — agent-off byte-identical render assertion

*No new framework install — the Go testing toolchain covers all phase requirements.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real `crush run` tool-call round-trip (read→edit→result) passes readiness | INSTALL-03 | Requires the pinned Crush binary + a served coder model on the gfx1151 box; deterministic payload confirmed on-hardware (Open Q1, like Phase-26 PONG) | On the live box: enable addon, `villa install --coding-agent`, observe readiness proof drives a real edit and PASSES; assert health-200-only would not pass |
| `villa verify agent` egress + llama-down controls | PRIV-06 | Negative control needs host egress actually blocked (operator precondition, same mechanism as verify-memory); llama-down requires stopping the live `villa-llama` unit | On the live box: run `villa verify agent` — assert (1) external probe FAILS under block, (2) blocked `crush run` task completes, (3) with `villa-llama` stopped the agent task FAILS (no cloud fallback) |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (test-first folded into every tdd task)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (folded into tdd tasks; no dangling MISSING ref)
- [x] No watch-mode flags
- [x] Feedback latency < 60s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-06-14
