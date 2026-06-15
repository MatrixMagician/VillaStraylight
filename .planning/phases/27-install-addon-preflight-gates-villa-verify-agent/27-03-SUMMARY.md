---
phase: 27-install-addon-preflight-gates-villa-verify-agent
plan: 03
subsystem: verify
tags: [verify, agent, egress, negative-control, cloud-fallback, llama-down, privacy, zero-outbound, stride, priv-06]

# Dependency graph
requires:
  - phase: 27-install-addon-preflight-gates-villa-verify-agent
    plan: 01
    provides: "config.VillaConfig.AgentEnabled gate + liveLoadedAgentEnabled seam; install_agent.go liveAgentToolCallProbe (the crush-run read→edit round-trip driver, reused for both controls); agentBinPath(); installServiceName (villa-llama.service)"
  - phase: 19-memory-install-addon
    provides: "verify_memory.go four-layer seam (evalRagSmoke negative-control-first core, liveRagSmoke, runProbeCurl, egressNegativeControlHost, memoryProof, memoryProofNetwork) — the EXACT analog this plan mirrors"
  - phase: 04-orchestration
    provides: "orchestrate.EmbedImage() helper-image accessor (the ONLY image source — TestSeamGrepGate) + orchestrate.Systemd Stop/Start lifecycle seam"
provides:
  - "evalAgentVerify (cmd/villa/verify_agent.go): the pure runtime strictly-local agent proof core — negative-control-FIRST egress, then crush-run agent task under the block, then llama-down cloud-fallback control; PASS only when ctrl1 passes AND ctrl2 fails-as-expected; PASS/FAIL only, timeout=FAIL"
  - "liveAgentVerify: egress probe via runProbeCurl + orchestrate.EmbedImage(); agentTask reuses liveAgentToolCallProbe (DRY); llamaDownTask stops villa-llama via the injected systemd seam then restores in a deferred Start (T-27-16)"
  - "verifyAgentDeps + liveVerifyAgentDeps() injectable quartet mirroring verifyMemoryDeps/liveVerifyMemoryDeps"
  - "newVerifyAgent()/runVerifyAgent() cobra wiring registered under the verify parent, gated on persisted agent_enabled (addon-off exits 0; PASS→exitPass / FAIL→exitBlocked)"
affects: [27-04-on-hardware]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Two-control runtime proof folded into ONE verb: negative-control-FIRST egress (ctrl1) + llama-down cloud-fallback (ctrl2), so the cloud-fallback control runs routinely rather than as a manual drill"
    - "DRY driver reuse: agentTask and llamaDownTask share the SAME liveAgentToolCallProbe crush-run read→edit mechanism — the install readiness probe and the runtime proof drive identical tool-call rounds"
    - "Deferred service restore: the llama-down control stops villa-llama then ALWAYS Start()s it in a defer regardless of the task outcome — the service is never left stopped (T-27-16)"
    - "Honesty-by-construction PASS/FAIL only (no WARN, no skip): an unevaluable/timeout result is a FAIL; an error from llamaDownTask is the EXPECTED inference-down outcome (the task SHOULD fail with the local model down)"

key-files:
  created:
    - "cmd/villa/verify_agent.go — evalAgentVerify pure core + liveAgentVerify live seam + verifyAgentDeps/liveVerifyAgentDeps + newVerifyAgent/runVerifyAgent cobra wiring"
    - "cmd/villa/verify_agent_test.go — TestEvalAgentVerify table, TestEvalAgentVerifyNegativeControlFirst false-green spies, TestVerifyAgentRegistered, TestRunVerifyAgentGate"
  modified:
    - "cmd/villa/verify.go — cmd.AddCommand(newVerifyAgent()) registration next to newVerifyMemory()"

key-decisions:
  - "Negative-control-FIRST ordering: egress is proven actually blocked BEFORE the agent task is even invoked — absence alone is rejected as a false-green (T-27-12, D-07)"
  - "llama-down control folded into the SAME verb so it runs every time (T-27-13, D-08); an answer with villa-llama stopped is the silent cloud-fallback smoking gun → FAIL"
  - "verdict = ctrl1.pass && ctrl2.failed-as-expected; PASS/FAIL only; a timeout is a FAIL (T-27-SC)"
  - "Helper image strictly from orchestrate.EmbedImage(); no image/device literal in cmd/villa (T-27-15, TestSeamGrepGate)"
  - "Cobra logic lives in verify_agent.go (the verify_memory split allows either file); verify.go keeps only the AddCommand registration"

patterns-established:
  - "evalAgentVerify: the runtime PRIV-06 two-control core — the verify-agent twin of evalRagSmoke"
  - "liveAgentVerify: composes the egress negative control + the DRY crush-run driver + the stop/restore systemd seam, then defers to the pure core"

requirements-completed: [PRIV-06]

# Metrics
duration: 5min
completed: 2026-06-14
---

# Phase 27 Plan 03: `villa verify agent` Runtime Strictly-Local Proof Summary

**Added `villa verify agent` — the runtime PRIV-06 honesty gate that proves zero outbound negative-control-FIRST (an external host MUST be unreachable under the egress block before the agent task is trusted) AND folds in a llama-down cloud-fallback control (with villa-llama stopped, the same `crush run` task MUST fail — an answer is the smoking gun), mirroring verify_memory.go's four-layer seam exactly.**

## Performance

- **Duration:** ~5 min
- **Started:** 2026-06-14T09:25:52Z
- **Completed:** 2026-06-14T09:31:00Z
- **Tasks:** 2
- **Files modified:** 3 (2 created, 1 modified)

## Accomplishments
- `evalAgentVerify` pure core: negative-control-FIRST egress proof, then the real `crush run` agent task under the block, then the llama-down cloud-fallback control — PASS only when ctrl1 passes AND ctrl2 fails-as-expected; PASS/FAIL only, a timeout/unevaluable is a FAIL.
- `liveAgentVerify` live seam: egress probe via `runProbeCurl(ctx, orchestrate.EmbedImage(), …)` (no re-typed image literal — TestSeamGrepGate green), `agentTask` reuses the Plan-01 `liveAgentToolCallProbe` read→edit driver (DRY), `llamaDownTask` stops `villa-llama.service` via the injected `orchestrate.Systemd` seam and restores it in a deferred `Start` (T-27-16 — never left stopped).
- `villa verify agent` registered under the `verify` parent, gated on the persisted `agent_enabled`: addon-off exits 0 with a clear message (not the silent-skip hazard), addon-on maps PASS→exitPass / FAIL→exitBlocked, mirroring `verify memory`.
- Full off-hardware test coverage: the eval table over all five outcomes, the false-green negative-control-first spies (the agent task and the llama-down control must NOT run when egress is not proven blocked), subcommand registration, and the cobra gate exit mapping.

## Task Commits

Each task was committed atomically:

1. **Task 1 (RED): failing test for evalAgentVerify** - `153e09b` (test)
2. **Task 1 (GREEN): evalAgentVerify core + liveAgentVerify seam** - `b3505e4` (feat)
3. **Task 2: register newVerifyAgent() gated on persisted agent_enabled** - `ab0c7dd` (feat)

_Task 1 followed the TDD RED→GREEN cycle (no separate refactor commit was needed)._

## Files Created/Modified
- `cmd/villa/verify_agent.go` (created) — the four-layer seam: `evalAgentVerify` (Layer 2 pure core), `liveAgentVerify` (Layer 3 live seam), `verifyAgentDeps`/`liveVerifyAgentDeps` (injectable quartet), and `newVerifyAgent`/`runVerifyAgent` (the thin cobra wiring + exit mapping). Layer 1 reuses `memoryProof`; Layer 4 reuses `runProbeCurl`.
- `cmd/villa/verify_agent_test.go` (created) — `TestEvalAgentVerify` (table over the five outcomes), `TestEvalAgentVerifyNegativeControlFirst` (false-green spies), `TestVerifyAgentRegistered`, `TestRunVerifyAgentGate`.
- `cmd/villa/verify.go` (modified) — `cmd.AddCommand(newVerifyAgent())` registration next to `newVerifyMemory()`.

## Verification
- `go test ./cmd/villa/ -run 'TestEvalAgentVerify|TestVerify|TestRunVerifyAgent' -count=1` — green.
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` — green (helper image strictly behind `orchestrate.EmbedImage()`; no leaked backend/image literal).
- `make check` (vet + full `go test ./...`) — green.
- The eval table proves: egress-open → FAIL (negative control first, even when the agent task would have completed), blocked + task error/!completed → FAIL, llama-down answered → FAIL (cloud fallback), blocked + completed + llama-down NOT answered (or errored as expected) → PASS.

## Deviations from Plan
None — plan executed exactly as written. Both `agentTask` and `llamaDownTask` reuse the existing `liveAgentToolCallProbe` driver as the plan's DRY directive specified; no new exec/curl tooling was introduced.

## TDD Gate Compliance
Task 1 was TDD: `test(27-03)` RED commit (`153e09b`, failing on undefined `evalAgentVerify`) → `feat(27-03)` GREEN commit (`b3505e4`). Gate sequence satisfied.

## On-Hardware Follow-up
PRIV-06 is on-hardware by nature: the live egress block, the live `crush run` round-trip over the loopback inference endpoint, and the `villa-llama` stop/restore acceptance are exercised in Plan 04 (the verification wave supplies the host-egress precondition and inspects the deferred service restore).

## Notes
This plan changes NO golden/JSON contract (Phase 28 owns dashboard surfacing); the agent-off path stays byte-identical (D-01). The new verb adds no host port and mutates nothing in config — `villa-llama` is stopped only transiently and always restored.

## Self-Check: PASSED
All created files (`cmd/villa/verify_agent.go`, `cmd/villa/verify_agent_test.go`, this SUMMARY) exist on disk; all task commits (`153e09b`, `b3505e4`, `ab0c7dd`) are present in git history.
