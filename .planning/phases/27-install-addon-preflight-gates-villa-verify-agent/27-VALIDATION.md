---
phase: 27
slug: install-addon-preflight-gates-villa-verify-agent
status: approved
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-14
audited: 2026-06-15
---

> **Wave 0 model:** No separate Wave 0 plan — test-first is folded into every implementation task (all of 27-01/02/03 are `tdd=true`: each writes its test FIRST and carries an `<automated>` verify command). `wave_0_complete` is now `true`: the 2026-06-15 audit confirmed every mapped Wave-0 test exists on disk and runs green (see Validation Audit below). The validation *strategy* was nyquist-compliant at planning (every requirement has a mapped automated verify, no 3-consecutive-task gap); it is now *realized*. Ratified 2026-06-14, audited 2026-06-15.

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

| Req ID | Behavior | Test Type | Automated Command | Status |
|--------|----------|-----------|-------------------|--------|
| INSTALL-03 | `--coding-agent` gate + agent-off byte-identical render | unit | `go test ./cmd/villa/ -run TestInstallCodingAgent -count=1` | ✅ green (`install_test.go` `TestInstallCodingAgentFlow` + agent-off byte-identical) |
| INSTALL-03 | coder-shard resolved from picked entry == served `-m` (single source, D-04) | unit | `go test ./cmd/villa/ -run TestCoderShardSingleSource -count=1` | ✅ green (`install_test.go`) |
| INSTALL-03 | served model == staged coder (CR-01: `RenderInput.CodingMode` + crush.json id == `rec.Coder.Model`) | unit | `go test ./cmd/villa/ -run TestInstallCodingAgent -count=1` | ✅ green (served-id + chat-only off-path subtests, 27-05) |
| INSTALL-03 | `evalAgentProof` PASS only on a real tool-call edit; FAIL on health-200/no-edit | unit | `go test ./cmd/villa/ -run TestEvalAgentProof -count=1` | ✅ green (`install_test.go`) |
| INSTALL-03 | `agentProbeReplaced` real replacement (TOKEN_B present AND TOKEN_A absent — WR-05) | unit | `go test ./cmd/villa/ -run TestAgentProbeReplaced -count=1` | ✅ green (`install_agent_test.go` truth table, 27-05) |
| INSTALL-04 | disk BLOCK / envelope BLOCK / cloud-cred WARN; typed-Unknown→WARN | unit | `go test ./cmd/villa/ -run 'TestAgentPreflight|TestAgentCloudCred' -count=1` | ✅ green (`preflight_agent_test.go` — disk/envelope/cloud-cred/unprobeable tiers + allowlist pin) |
| INSTALL-04 | uninstall removes binary + crush.json (ordered); config.toml left; GGUF via flag | unit | `go test ./cmd/villa/ -run TestUninstall -count=1` | ✅ green (`uninstall_test.go` — ordered/idempotent/error-aborts/no-config-touch) |
| PRIV-06 | `evalAgentVerify` negative-control-FIRST: egress-open→FAIL, blocked task→PASS, llama-down answer→FAIL | unit | `go test ./cmd/villa/ -run TestEvalAgentVerify -count=1` | ✅ green (`verify_agent_test.go` 5-outcome table + false-green spies) |
| PRIV-06 | egress negative control FAILs on broken probe infra (WR-01: `classifyEgressProbe`) | unit | `go test ./cmd/villa/ -run 'TestClassifyEgressProbe|TestEvalAgentVerifyInfraErrorFails' -count=1` | ✅ green (`verify_agent_test.go`, 27-06) |
| PRIV-06 | llama-down restore failure surfaced, never silently swallowed (WR-06) | unit | `go test ./cmd/villa/ -run 'TestLlamaDownRestore|TestRestoreLlamaWarning' -count=1` | ✅ green (`verify_agent_test.go`, 27-06) |
| PRIV-06 | in-network endpoint single-source (DNS/port lockstep, no host literal) | unit | `go test ./internal/orchestrate/ -run TestLlamaInNetworkEndpoint -count=1` | ✅ green (`endpoint_test.go`, 27-06) |
| INSTALL-03/PRIV-06 | real `crush run` tool-call round-trip + egress block + llama-down | on-hardware | manual acceptance (gfx1151 box) | ✅ ACCEPTED on-hardware (27-04 — readiness PASS via real edit; egress nft-block proven; llama-down → FAIL, restored) |
| all | seam-gate green (no leaked literal in cmd/villa) | unit | `go test ./internal/inference/ -run TestSeamGrepGate -count=1` | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [x] `cmd/villa/install_agent_test.go` — covers INSTALL-03 (`TestCoderShard`/`TestCoderModelPresent` pre-stage seams, single-source filename, `TestAgentProbeReplaced` readiness verdict)
- [x] `cmd/villa/verify_agent_test.go` — covers PRIV-06 (`TestEvalAgentVerify` negative-control-first table, `TestEvalAgentVerifyNegativeControlFirst` false-green spies, `TestClassifyEgressProbe`, `TestLlamaDownRestore`/`TestRestoreLlamaWarning`; mirrors `evalRagSmoke`)
- [x] `cmd/villa/preflight_agent_test.go` — covers INSTALL-04 preflight tiers (`TestAgentPreflightDiskBlock/Pass/UnprobeableWARN`, `TestAgentPreflightEnvelopeBlock/Pass`, `TestAgentPreflightCloudCredWARN/AbsentNoBlock/Multiple`, allowlist pin)
- [x] Extend `cmd/villa/uninstall_test.go` — agent teardown ordering + config.toml-left invariant + GGUF-via-flag (`TestUninstallRemovesAgentArtifacts`, `TestUninstallAgentRemovalIdempotent`, `TestUninstallAgentRemovalErrorAborts`, `TestUninstallAgentTeardownNoConfigTouch`)
- [x] Extend `cmd/villa/install_test.go` — agent-off byte-identical render assertion + `TestEvalAgentProof`/`TestCoderShardSingleSource`/`TestInstallCodingAgentFlow` (incl. served-coder CR-01 subtest)

*No new framework install — the Go testing toolchain covers all phase requirements. All Wave-0 tests written via TDD during execution (27-01/02/03) and hardened by gap-closure plans 27-05/06; confirmed green by the 2026-06-15 audit.*

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

---

## Validation Audit 2026-06-15

State A re-audit of the post-execution codebase (phase complete: plans 27-01..27-06, VERIFICATION passed 5/5). Every mapped requirement test was confirmed to exist on disk and run green; the on-hardware manual items were accepted in 27-04. No 3-consecutive-task gap. No auditor spawn needed (zero gaps).

| Metric | Count |
|--------|-------|
| Requirements mapped | 3 (INSTALL-03, INSTALL-04, PRIV-06) |
| Gaps found | 0 |
| Resolved | 0 (none needed — Wave-0 tests already written + green) |
| Escalated | 0 |
| Manual-only (accepted on-hardware) | 2 (27-04 acceptance) |

**Commands re-run green (2026-06-15):**
- `go test ./cmd/villa/ -run 'TestInstall|TestCoderShard|TestEvalAgentProof|TestAgentPreflight|TestAgentCloudCred|TestUninstall|TestEvalAgentVerify|TestAgentProbeReplaced|TestClassifyEgressProbe|TestLlamaDownRestore|TestRestoreLlamaWarning|TestVerifyAgentRegistered|TestRunVerifyAgentGate' -count=1` → ok
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` → ok
- `go test ./internal/orchestrate/ -run TestLlamaInNetworkEndpoint -count=1` → ok
- `go test ./internal/agent/ -run TestRender -count=1` → ok

**Verdict:** Phase 27 is NYQUIST-COMPLIANT (realized) — `wave_0_complete` flipped `false → true`. No test files generated by this audit (all coverage pre-existing).
