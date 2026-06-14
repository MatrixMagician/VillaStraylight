---
phase: 27-install-addon-preflight-gates-villa-verify-agent
plan: 01
subsystem: install
tags: [install, addon, agent, crush, coder-gguf, readiness, config, permissions, fsl-consent, stride]

# Dependency graph
requires:
  - phase: 26-agent-delivery-core-lockdown-launcher
    provides: "agent.Install (checksum-before-extract), agent.Render (crush.json), crush-policy.json pin (pinned binary sha), LSPProbe/Deps types"
  - phase: 24-coder-fit-math
    provides: "recommend.CoderFit (rec.Coder), catalog schema v3 (Role:coder, Shards), frozen coder entries"
  - phase: 19-memory-install-addon
    provides: "install_memory.go addon pattern (pre-stage source → presence-skip → ensure-download → readiness verdict), memoryProof shape"
provides:
  - "config.VillaConfig.AgentEnabled gate field (agent_enabled,omitempty) — gates the v1.4 coding-agent addon, default off (D-01)"
  - "agent.Render restrictive-tools STRIDE pass: permissions.allowed_tools (view/edit/write) + options.disabled_tools (fetch/agentic_fetch/download/sourcegraph)"
  - "agent.LoadCrushPolicy() exported loader for cmd/villa to compose the verified binary install"
  - "cmd/villa/install_agent.go: coderShardFor (catalog-resolved, D-02/D-04), pre-stage seams (D-03), liveInstallAgentBinary (composes agent.Install), liveRenderCrushConfig, evalAgentProof (D-05), agentLicenseNotice (FSL-1.1-MIT), liveAgentToolCallProbe"
  - "install.go --coding-agent flag + loadedAgentEnabled gate + agent pre-stage/install/render/readiness block"
affects: [27-02-preflight-agent, 27-03-verify-agent, 27-04-on-hardware, uninstall-agent-teardown]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Addon gate field mirrors MemoryEnabled/CodingMode: plain bool, ,omitempty, NOT self-healed in normalizeVilla"
    - "Catalog-resolved shard (no hard-coded literal) — coderShardFor scans for the picked id's Shards[0], making D-02 (pick selects) + D-04 (single source) hold by construction"
    - "Tool-call round-trip readiness verdict (PASS only on a real planted edit; health-200 never an input) — the agentProof twin of memoryProof"

key-files:
  created:
    - cmd/villa/install_agent.go
    - cmd/villa/install_agent_test.go
    - internal/agent/render_test.go
  modified:
    - internal/config/villaconfig.go
    - internal/config/villaconfig_test.go
    - internal/agent/render.go
    - internal/agent/policy.go
    - internal/agent/testdata/crush.json.golden
    - cmd/villa/install.go
    - cmd/villa/install_test.go

key-decisions:
  - "Exported agent.LoadCrushPolicy() (delegating to the unexported loadCrushPolicy) rather than adding an agent.InstallPinned wrapper — keeps asset/URL resolution testable in cmd/villa (install_agent.go) so a future asset-rename/URL-template change is exercised by the install flow, while the verify/extract stays behind the agent seam"
  - "Reused a parallel agentProof{status,detail} of the IDENTICAL memoryProof shape (not the memoryProof type itself) so the agent readiness verdict reads independently — both are PASS/FAIL only, no WARN"
  - "Agent pre-stage block (FSL notice → coder shard resolve → GGUF pre-stage → binary install → config render) placed at step 6c BEFORE saveConfig+start; the tool-call readiness proof at step 10c AFTER the stack is up"
  - "crushPermissions.AllowedTools and crushOptions.DisabledTools rendered UNCONDITIONALLY (dropped the omitempty on AllowedTools, added DisabledTools as non-omitempty) — an omitted denylist is the STRIDE FAIL, never an acceptable runtime degrade"

patterns-established:
  - "Restrictive-tools render is the security control (T-27-21): absence of disabled_tools is a planning bug, never a runtime degrade — keys PINNED against the v0.76.0 frozen schema, rendered without runtime re-confirmation"
  - "Agent readiness probe uses NO image literal (host binary exec only via agentBinPath()) so TestSeamGrepGate stays green"

requirements-completed: [INSTALL-03]

# Metrics
duration: ~40min
completed: 2026-06-14
---

# Phase 27 Plan 01: Coding-Agent Install Addon Summary

**The Crush coding agent is now an optional `villa install --coding-agent` addon — a persisted `AgentEnabled` gate, a catalog-resolved coder-GGUF pre-stage, a checksum-verified binary install, a locked-down (outbound-tools-off) crush.json render, and a real tool-call round-trip readiness proof — with agent-off staying byte-identical to v1.3.**

## Performance

- **Duration:** ~40 min
- **Tasks:** 3 (all TDD)
- **Files modified:** 10 (3 created, 7 modified)

## Accomplishments
- **D-01 gate:** `config.VillaConfig.AgentEnabled` (default off, `,omitempty`, not self-healed). `--coding-agent` overrides+persists it before the gate; a bare `villa install` gates on the persisted value. Agent-off fires zero agent seams (byte-identical to v1.3, asserted).
- **Phase-27 STRIDE pass (T-27-21):** `agent.Render` now emits `permissions.allowed_tools` = view/edit/write (top-level) and `options.disabled_tools` = fetch/agentic_fetch/download/sourcegraph (under options) — the agent's outbound tools are off by construction; the agent render golden was refrozen intentionally.
- **D-02/D-04 single source:** `coderShardFor(rec, cat)` resolves the recommend-picked coder entry's `Shards[0]` — no hard-coded literal; the staged filename and the served `-m` path derive from one catalog entry.
- **D-03 composition:** `liveInstallAgentBinary` composes the Phase-26 `agent.Install` (checksum-before-extract) via the newly-exported `agent.LoadCrushPolicy()`; `liveEnsureCoderModel` pre-stages the GGUF through the single sanctioned outbound window (`pullFn`). Neither re-implements the verify.
- **D-05 honest readiness:** `evalAgentProof` PASSES only on a real planted read→edit round-trip; a no-edit or an error FAILS with remediation; a health-200 is never an input. The live driver execs the villa-owned binary fixed-arg (no image literal — seam gate stays green).
- **FSL-1.1-MIT consent notice** surfaced before staging the binary.

## Task Commits

1. **Task 1: AgentEnabled config field + restrictive-tools crush.json render (STRIDE pass)** — `d299cd3` (feat)
2. **Task 2: install_agent.go coder-GGUF pre-stage + agent-binary install seams** — `afe7dea` (feat)
3. **Task 3: --coding-agent flag, FSL notice, agent install block + evalAgentProof readiness** — `6352536` (feat)

_TDD: RED tests written first for the config encode-omit + restrictive-tools render (Task 1), the coderShardFor/presence tables (Task 2), and evalAgentProof/coder-single-source/flow (Task 3); GREEN implementations followed._

## Files Created/Modified
- `internal/config/villaconfig.go` — added `AgentEnabled bool` (`agent_enabled,omitempty`), gates the v1.4 addon; plain-bool omitempty drops the key on a default-false marshal (no marshalVilla zeroing needed).
- `internal/agent/render.go` — added `crushOptions.DisabledTools` (non-omitempty) + made `crushPermissions.AllowedTools`/`crushConfig.Permissions` render unconditionally; pinned `allowedTools`/`disabledTools` values wired into `Render`.
- `internal/agent/policy.go` — exported `LoadCrushPolicy()` delegating to the unexported loader (shared panic-on-malformed discipline).
- `internal/agent/render_test.go` (new) — asserts both restrictive-tool placements decode from the rendered bytes.
- `internal/agent/testdata/crush.json.golden` — refrozen (intentional; internal agent contract, not a status/dashboard golden).
- `cmd/villa/install_agent.go` (new) — `coderShardFor`, `liveCoderModelPresent`, `liveEnsureCoderModel`, `liveInstallAgentBinary`, `liveRenderCrushConfig`, `liveLSPProbes`, `liveLoadedAgentEnabled`, `agentLicenseNotice`, `agentProof`/`evalAgentProof`, `liveAgentToolCallProbe`.
- `cmd/villa/install_agent_test.go` (new) — `TestCoderShard`, `TestCoderModelPresent` tables.
- `cmd/villa/install.go` — `--coding-agent` flag + `installOpts.codingAgent`; 7 new `installDeps` agent seams; `cfg.AgentEnabled` seed+override; the step-6c pre-stage block + step-10c readiness block; live wiring.
- `cmd/villa/install_test.go` — fake-deps agent seams + defaults; `TestEvalAgentProof`, `TestCoderShardSingleSource`, `TestInstallCodingAgentFlow` (stage/persist/prove, readiness-FAIL blocks, no-fit refuses, agent-off byte-identical).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `liveRenderCrushConfig` needed host LSP probes**
- **Found during:** Task 2
- **Issue:** `agent.Render` requires `[]agent.LSPProbe`; the plan named the composition but not the probe source.
- **Fix:** Added `liveLSPProbes()` (gopls/pyright/rust-analyzer/typescript-language-server) using a `lookPathFn` PATH-lookup seam — references only, never auto-installs (the Render contract: a missing server is a WARN-and-omit).
- **Files modified:** cmd/villa/install_agent.go
- **Commit:** afe7dea

Otherwise the plan executed as written. The pinned binary SHA was already landed on-hardware in 26-03, so `agent.LoadCrushPolicy()` returns a confidently-pinned policy (no sentinel path exercised here).

## Decisions Made
- Exported `agent.LoadCrushPolicy()` (per plan Task 2's "pick the wrapper that keeps asset/URL resolution testable") rather than `agent.InstallPinned` — asset/URL resolution lives in `cmd/villa/install_agent.go` and is exercised by the install-flow tests.

## TDD Gate Compliance
This is a `type: execute` plan (not a plan-level `type: tdd`). Each task followed RED→GREEN at the task level; commits are squashed per task as `feat(...)` (RED and GREEN landed together per task), which is the project's per-task atomic-commit convention. The seam grep-gate (`TestSeamGrepGate`) and the byte-frozen golden discipline were honored throughout.

## Verification
- `make check` (vet + `go test ./...`) — GREEN across all 24 packages.
- `go test ./internal/inference/ -run TestSeamGrepGate` — GREEN (no leaked backend-marker literal; the readiness driver execs the host binary, no image literal).
- `internal/agent/render_test.go` asserts allowed_tools + disabled_tools placements.
- `TestCoderShardSingleSource` proves the staged shard + served id resolve from one catalog entry (D-04).
- `TestEvalAgentProof` proves PASS only on a real edit; FAIL on no-edit and on err (D-05).
- Agent-off install fires no agent seam (D-01 byte-identical).

## Known Stubs
None. The on-hardware `crush run` tool-call payload (exact prompt phrasing / token constants) is pinned as defaults in `install_agent.go` (`agentProbePrompt`, `agentProbeToken{A,B}`) and is confirmed on-hardware in Plan 04 per the plan — the pure verdict core and the live driver are fully wired; only the on-box payload phrasing is subject to Plan-04 confirmation.

## Self-Check: PASSED
- Created files verified on disk: `cmd/villa/install_agent.go`, `cmd/villa/install_agent_test.go`, `internal/agent/render_test.go`, `27-01-SUMMARY.md`.
- Task commits verified in history: `d299cd3`, `afe7dea`, `6352536`.
