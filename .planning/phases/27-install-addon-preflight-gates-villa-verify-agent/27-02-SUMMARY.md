---
phase: 27-install-addon-preflight-gates-villa-verify-agent
plan: 02
subsystem: preflight
tags: [preflight, uninstall, agent, disk-block, envelope-block, cloud-credential, teardown, stride]

# Dependency graph
requires:
  - phase: 27-install-addon-preflight-gates-villa-verify-agent
    plan: 01
    provides: "config.VillaConfig.AgentEnabled gate, loadedAgentEnabled seam, install_agent.go (coderShardFor, evalAgentProof), agentBinPath()/crushConfigPath(), rec.Coder (CoderFit)"
  - phase: 24-coder-fit-math
    provides: "recommend.CoderFit (rec.Coder) — the envelope-BLOCK basis, read never re-derived"
  - phase: 19-memory-install-addon
    provides: "the runMemoryChecks preflight-fold pattern + the uninstall ordered-teardown contract this plan mirrors"
provides:
  - "runAgentChecks (cmd/villa/preflight_agent.go): AGENT-PRE-disk staged-footprint BLOCK, AGENT-PRE-envelope post-coder BLOCK driven by rec.Coder, AGENT-PRE-cloud-cred env-credential WARN, typed-Unknown→WARN (D-09)"
  - "cloudCredentialAllowlist: the 11-key cloud-LLM provider env-var scan (27-RESEARCH A1)"
  - "installDeps.runAgentChecks seam + agentEnabledForGate fold (gated on --coding-agent override OR persisted agent_enabled); agent-off preflight byte-identical"
  - "liveAgentStatfs / existingAncestorDir: cmd-tier syscall.Statfs copy of preflight.liveStatfs"
  - "uninstallDeps.removeAgentBinary + removeCrushConfig ALWAYS-removed ordered seams; removeAgentBinaryLive/removeCrushConfigLive traversal-guarded idempotent removals (D-10)"
affects: [27-03-verify-agent, 27-04-on-hardware]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Agent preflight checks built as CheckResult literals at the cmd tier (the preflight pass/warn/fail builders are package-private; the agent gate needs rec.Coder + the staged size the install flow resolves) — the runMemoryChecks fold's structural twin, but taking rec"
    - "Honest-gating D-09: confident known-bad → BLOCK (disk shortage, no coder fit); unprobeable → typed-Unknown WARN; cloud credential → WARN-never-BLOCK (the render + egress proof neutralize it structurally)"
    - "Envelope BLOCK reads rec.Coder (Fits/TotalBytes/Residency) — never re-derived, so the gate cannot drift from the Phase-24 pickCoder output (anti-pattern guard)"
    - "Uninstall agent teardown: two ALWAYS-removed idempotent seams at a deterministic asserted position (after dashboard, before container stop); GGUF via existing keep/remove-models; config.toml left (no seam)"

key-files:
  created:
    - cmd/villa/preflight_agent.go
    - cmd/villa/preflight_agent_test.go
  modified:
    - cmd/villa/install.go
    - cmd/villa/install_test.go
    - cmd/villa/uninstall.go
    - cmd/villa/uninstall_test.go

key-decisions:
  - "Built the agent CheckResults as cmd-tier literals (not new exported preflight helpers) because the agent gate needs rec.Coder + the staged-footprint size the install flow already resolves, and the preflight pass/warn/fail builders are package-private — this mirrors how the memory checks are constructed and keeps the agent-specific logic next to its consumers (install.go)"
  - "Added agentEnabledForGate(opts, d) folding the --coding-agent override into the persisted agent_enabled gate so a FIRST-TIME `villa install --coding-agent` is gated too (disk/envelope checked before staging), matching how cfg.AgentEnabled is later resolved (loadedAgentEnabled || codingAgent)"
  - "Injected statfs + lookupEnv as agentCheckInput seams (pure core) rather than calling syscall/os in runAgentChecks, so every tier is deterministically testable; the live closure binds liveAgentStatfs + os.LookupEnv and resolves the staged size from the SAME catalog entry + policy pin the install flow stages (no drift)"
  - "Placed the two uninstall agent removals after the dashboard teardown and before the container stop — a deterministic position asserted by TestUninstallRemovesAgentArtifacts (ordering IS the contract, D-10); reused agentBinPath()/crushConfigPath() + assertUnitInsideDir for the traversal guard (DRY)"

patterns-established:
  - "Cloud-credential scan is a WARN surface, never a control: presence of a provider key is information-disclosure RISK surfaced to the operator; the real control is the rendered loopback-only provider + disable_default_providers + the (Plan-03) egress proof"
  - "typed-Unknown→WARN honesty extends to the agent disk gate: a statfs failure degrades to WARN, never a false BLOCK"

requirements-completed: [INSTALL-04]

# Metrics
duration: ~6min
completed: 2026-06-14
---

# Phase 27 Plan 02: Preflight Gates & Uninstall Coverage Summary

**The coding-agent addon is now honestly gated at preflight — a staged-footprint disk BLOCK, a post-coder envelope BLOCK read from `rec.Coder` (never re-derived), an 11-key cloud-credential WARN (never a BLOCK), and typed-Unknown→WARN — appended to the install gate ONLY when the addon is enabled (agent-off byte-identical); and `villa uninstall` now always removes the villa-owned crush binary + rendered crush.json at a deterministic asserted position, idempotently, with the staged GGUF following keep/remove-models and config.toml left untouched.**

## Performance
- **Duration:** ~6 min
- **Tasks:** 2 (both TDD)
- **Files modified:** 6 (2 created, 4 modified)

## Accomplishments
- **D-09 honest preflight (Task 1):** `runAgentChecks` builds three tiered `preflight.CheckResult`s — `AGENT-PRE-disk` (BLOCK: free disk < staged coder GGUF + binary → FAIL; unprobeable → typed-Unknown WARN), `AGENT-PRE-envelope` (BLOCK: `rec.Coder.Fits == false` → FAIL with the basis cited from `rec.Coder.TotalBytes`/`Residency`, NEVER re-derived; `Fits == true` → PASS), `AGENT-PRE-cloud-cred` (WARN: a present cloud-LLM key names the var(s) + the neutralization; absent → PASS; NEVER a BLOCK).
- **Fold gating:** the `runAgentChecks` seam is appended at the memory-fold site gated on `agentEnabledForGate` (the `--coding-agent` override folded into the persisted `agent_enabled`), so the checks flow through the SAME `gateInstall` (inheriting refuse-with-remediation) and an agent-off install is byte-identical. Nil-safe like `runMemoryChecks`.
- **Single-source staging size:** the live closure resolves the staged footprint from `coderShardFor(rec, cat).SizeBytes + agent.LoadCrushPolicy().Assets["linux/amd64"].Size` — the SAME catalog entry + policy pin install_agent.go stages, so the disk gate can never drift from what is written.
- **D-10 uninstall coverage (Task 2):** `removeAgentBinary` + `removeCrushConfig` are ALWAYS-removed seams inserted at a deterministic position (after the dashboard teardown, before the container stop); live wiring reuses `agentBinPath()`/`crushConfigPath()` with a traversal-guarded idempotent `os.Remove` (mirrors `removeUnitFileLive`/`assertUnitInsideDir`). The staged GGUF follows the existing keep/remove-models choice; config.toml is left in place (no seam touches it).

## Task Commits
1. **Task 1: preflight_agent.go — disk/envelope BLOCK, cloud-cred WARN, typed-Unknown WARN; folded into install gate** — `d697c01` (feat)
2. **Task 2: uninstall agent teardown — removeAgentBinary + removeCrushConfig ordered seams** — `af09710` (feat)

_TDD: RED tier/fold tests written first in `preflight_agent_test.go` + `install_test.go` (Task 1) and the ordered/idempotent/error/config-left tests in `uninstall_test.go` (Task 2); GREEN implementations followed per task._

## Files Created/Modified
- `cmd/villa/preflight_agent.go` (new) — `runAgentChecks` (pure D-09 core) + `agentDiskCheck`/`agentEnvelopeCheck`/`agentCloudCredCheck` + `agentCheckInput` (statfs/lookupEnv seams) + `cloudCredentialAllowlist` (11 keys) + the three stable check IDs.
- `cmd/villa/preflight_agent_test.go` (new) — disk BLOCK/PASS/unprobeable-WARN, envelope BLOCK/PASS, cloud-cred WARN/PASS/multi, and the allowlist-complete pin.
- `cmd/villa/install.go` — `runAgentChecks` seam on `installDeps`; the `(3a')` fold gated on `agentEnabledForGate`; `agentEnabledForGate` helper; live wiring resolving the staged size from the catalog entry + policy pin; `liveAgentStatfs` + `existingAncestorDir`; added `agent`/`syscall` imports.
- `cmd/villa/install_test.go` — `agentChecksCalls`/`agentChecks` fake controls; `runAgentChecks` fake seam; `TestInstallAgentPreflightFold` (folds-once / agent-off-never / BLOCK-refuses).
- `cmd/villa/uninstall.go` — `removeAgentBinary`/`removeCrushConfig` seams on `uninstallDeps`; the `(0b)` ordered teardown in `runUninstall`; live wiring + `removeAgentBinaryLive`/`removeCrushConfigLive` (traversal-guarded idempotent).
- `cmd/villa/uninstall_test.go` — agent-removal fake seams/counters; `TestUninstallRemovesAgentArtifacts` (ordered position), `TestUninstallAgentRemovalIdempotent`, `TestUninstallAgentRemovalErrorAborts`, `TestUninstallAgentTeardownNoConfigTouch`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `preflight.liveStatfs` is package-private — added a cmd-tier copy**
- **Found during:** Task 1
- **Issue:** The plan said to "reuse the existing install disk-statfs machinery (`preflight.ResourceReq.MinDiskBytes` + `liveStatfs`)", but `liveStatfs` is unexported in `internal/preflight`, and calling `preflight.RunWithResources` would run the entire install check set (not just a statfs). The agent disk check needs a standalone free-bytes probe at the models dir.
- **Fix:** Added `liveAgentStatfs` (+ `existingAncestorDir`) in `cmd/villa/install.go` — a faithful copy of `preflight.liveStatfs`'s `syscall.Statfs` + existing-ancestor-walk discipline (no shelling to `df`, locale-proof). Injected into `runAgentChecks` via the `agentCheckInput.statfs` seam so the disk tier stays deterministically testable.
- **Files modified:** cmd/villa/install.go
- **Commit:** d697c01

Otherwise the plan executed as written. The cloud-credential allowlist matches 27-RESEARCH A1 exactly (11 keys); the on-disk Crush auth store is covered structurally by the same WARN surface (the env scan is the load-bearing signal on the Strix Halo target per A2) and is not a separate BLOCK.

## Decisions Made
- Built the agent `CheckResult`s as cmd-tier literals (next to install.go) rather than adding exported `preflight` helpers, because the agent gate consumes `rec.Coder` and the staged-footprint size that the install flow resolves — the same construction style as the memory checks at the cmd tier.
- `agentEnabledForGate` folds the `--coding-agent` override into the persisted gate so a first-time opt-in is gated before staging.

## TDD Gate Compliance
This is a `type: execute` plan. Each task followed RED→GREEN at the task level; commits are the project's per-task atomic `feat(...)` (RED + GREEN landed together per task). The seam grep-gate (`TestSeamGrepGate`) was run per task — the new files carry NO backend-marker literal (no image tag / device / `Vulkan0`/`ROCm0`).

## Verification
- `go test ./cmd/villa/ -run 'TestAgentPreflight|TestInstall|TestUninstall' -count=1` — GREEN.
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` — GREEN (no leaked backend-marker literal).
- `go vet ./cmd/villa/` — clean.
- `make check` (vet + `go test ./...`) — GREEN across all 24 packages.
- Envelope BLOCK reads `rec.Coder` (asserted by `TestAgentPreflightEnvelopeBlock`/`Pass`, never re-derived).
- typed-Unknown → WARN proven by `TestAgentPreflightDiskUnprobeableWARN` (unprobeable disk → WARN, not BLOCK).
- Uninstall ordered teardown removes binary + crush.json at the asserted position (`TestUninstallRemovesAgentArtifacts`); config.toml untouched (`TestUninstallAgentTeardownNoConfigTouch`); GGUF via flag (existing keep/remove-models tests).

## Known Stubs
None. The cloud-credential allowlist is the researched 11-key set (27-RESEARCH A1); the live `runAgentChecks` closure resolves the staged size and statfs target from real seams. The runtime egress/cloud-fallback PROOF (negative-control-first `villa verify agent`) is Plan 03's surface, not a stub here — preflight only surfaces the credential risk; the structural control lands in 27-03.

## Threat Flags
None. The new surface (env-var read in the cloud-cred scan, two XDG-confined removal paths) is exactly the trust boundary the plan's threat register (T-27-07..T-27-11) anticipated and mitigates: the env scan is read-only and never BLOCKs (T-27-08); both removals are traversal-guarded inside their XDG dirs with no raw `os.Remove(userInput)` (T-27-09); no backend-marker literal leaked (T-27-11).

## Self-Check: PASSED
- Created files verified on disk: `cmd/villa/preflight_agent.go`, `cmd/villa/preflight_agent_test.go`, `27-02-SUMMARY.md`.
- Task commits verified in history: `d697c01`, `af09710`.
