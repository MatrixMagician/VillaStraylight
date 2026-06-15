---
phase: 27-install-addon-preflight-gates-villa-verify-agent
plan: 05
subsystem: install-addon
gap_closure: true
closes_gaps: [CR-01, WR-05]
tags: [install, coding-agent, coding-mode, readiness-proof, honesty, CR-01, WR-05]
requires:
  - "rec.Coder fit block (Phase 24-02 CoderFit: Model/Quant/AgentCtx/Residency)"
  - "Phase-25 coding-mode helpers: codingServedTarget / codingModelFile / codingDescriptor"
  - "orchestrate.RenderInput.CodingMode + CoderAgentCtx (Phase-25 CMODE-01)"
provides:
  - "villa install --coding-agent SERVES the staged coder (rec.Coder.Model) at rec.Coder.AgentCtx"
  - "agentProbeReplaced(content) replacement predicate (TOKEN_B present AND TOKEN_A absent)"
affects:
  - "cmd/villa/install.go (--coding-agent render path)"
  - "cmd/villa/install_agent.go (readiness probe + shared verify agentTask driver)"
tech-stack:
  added: []
  patterns:
    - "Reuse the Phase-25 coding-mode render helpers on the install path (no new render contract)"
    - "Pure predicate extraction for off-hardware truth-table testing of a host-execing driver"
key-files:
  created: []
  modified:
    - cmd/villa/install.go
    - cmd/villa/install_test.go
    - cmd/villa/install_agent.go
    - cmd/villa/install_agent_test.go
    - cmd/villa/coding-mode_test.go
decisions:
  - "--coding-agent install ENTERS coding-mode (CR-01 fix option a): it serves the staged coder so the readiness proof exercises the model crush.json advertises (honesty-by-construction, D-05)"
  - "install.go is a sanctioned ONE-SHOT coding-mode entry surface added to the D-06 no-auto-flip structural guard allow-list; the runtime toggle remains `villa coding-mode enter|exit`"
  - "CodingMode entry is gated on opts.codingAgent && rec.Coder.Model != \"\" — a bare `villa install` never auto-flips, and a shared-residency (empty-coder) path never serves an empty -m"
  - "WR-05 readiness predicate factored into pure agentProbeReplaced so the replace contract is asserted off-hardware without a live crush binary"
metrics:
  duration: ~4 min
  completed: 2026-06-14
  tasks: 2
  files: 5
---

# Phase 27 Plan 05: Serve-the-Coder + Real-Replacement Readiness (CR-01 + WR-05) Summary

**One-liner:** `villa install --coding-agent` now enters coding-mode and serves the coder GGUF it stages and gates on (CR-01 closed), and the install/verify readiness probe asserts a real TOKEN_A→TOKEN_B replacement instead of mere TOKEN_B presence (WR-05 closed) — both proven by new off-hardware seam tests, with no golden contract change.

## What This Plan Closed

This is a gap-closure plan for two findings in `27-VERIFICATION.md`:

- **CR-01 (BLOCKER):** `runInstall` set `cfg.AgentEnabled = true` but never set `cfg.CoderModel` / `cfg.CoderQuant` / `cfg.CoderAgentCtx` / `cfg.CodingMode`, and threaded no `CodingMode` into `d.render`. The inference unit kept serving the CHAT model, `crush.json` advertised the chat model, and both proofs (install readiness + `villa verify agent`) passed against the wrong model. The staged coder GGUF was dead disk.
- **WR-05:** `liveAgentToolCallProbe` declared success via `strings.Contains(edited, TOKEN_B)` only — an append, a partial write, or a transcript echoing TOKEN_B false-greened the readiness contract.

## Task 1 — Serve the coder on the --coding-agent install path (CR-01)

**Commit:** `ff28631`

- In `runInstall`, the `if opts.codingAgent` block now single-sources the coder render inputs from the SAME `rec.Coder` the disk/envelope preflight gates and the staged shard derive from: `cfg.CoderModel = rec.Coder.Model`, `cfg.CoderQuant`, `cfg.CoderAgentCtx`, and `cfg.CodingMode = true` — gated on `rec.Coder.Model != ""` so a shared-residency path never serves an empty `-m` and a bare `villa install` never auto-flips the mode.
- The render call now builds a `renderIn` that, on the coding-agent path (`cfg.CodingMode`), resolves the served `-m` from the coder shard via the proven Phase-25 helpers `codingServedTarget` → `codingModelFile`, and threads a non-nil `CodingMode` descriptor (built by `codingDescriptor`) + `CoderAgentCtx`. The chat-only path keeps `ModelFile: modelFile` and `CodingMode == nil` — byte-identical to v1.3.
- `internal/agent/render.go`'s `servedModelID` now resolves the coder (because `cfg.CoderModel` is set), so `crush.json` advertises `villa-<coder>` at the coder ctx. The persisted config carries the coder fields, so a subsequent bare `villa up`/`restart` re-renders the coder unit (config is the single source of truth).
- Tests: `install_test.go` adds a served-id assertion subtest (captured `RenderInput.CodingMode != nil`, `Cfg.CoderModel == rec.Coder.Model`, `Cfg.CodingMode == true`, `CoderAgentCtx == rec.Coder.AgentCtx`, and persisted coder fields) plus a chat-only off-path guard (`CodingMode == nil`, `CoderModel == ""`). The render seam now captures its `RenderInput`. The default fake `pick`/`agentCat` were updated to a REAL embedded-catalog coder id (`qwen3-coder-30b-a3b`) because `codingModelFile`/`codingDescriptor` read the embedded catalog via `modelCatalogPath`, not the fake.

## Task 2 — Assert a REAL replacement in the readiness probe (WR-05)

**Commit:** `37474d5`

- Extracted a pure predicate `agentProbeReplaced(content string) bool` = `Contains(TOKEN_B) && !Contains(TOKEN_A)`, and `liveAgentToolCallProbe` now returns `agentProbeReplaced(string(edited)), nil`. This closes the append/echo/transcript false-green. The driver is shared by the install readiness proof AND `villa verify agent`'s agentTask, so the fix hardens both (no change needed in `verify_agent.go`).
- The probe stays fixed-arg (no shell, no image literal) so `TestSeamGrepGate` is green.
- Tests: `install_agent_test.go` adds `TestAgentProbeReplaced` truth table — TOKEN_B-only → true; TOKEN_A+TOKEN_B → false; TOKEN_A-only → false; empty → false; unrelated → false; transcript echoing both → false.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] D-06 no-auto-flip structural guard required updating for the sanctioned install-time coding-mode entry**
- **Found during:** Task 1
- **Issue:** `TestNoAutoFlipStructuralGuard` (`coding-mode_test.go`, D-06 / T-25-09) walks `cmd/villa` + `internal/` and fails the build if `CodingMode = true|false` is mutated outside `codingmode.Run`. The plan's resolved architectural choice (CR-01 fix option a) explicitly sets `cfg.CodingMode = true` in `install.go`, which the guard correctly flagged.
- **Fix:** Added `install.go` to the guard's allow-list with a documented rationale tying it to the CR-01 decision: `villa install --coding-agent` is a sanctioned ONE-SHOT coding-mode ENTRY surface (gated on `opts.codingAgent && rec.Coder.Model != ""`, never a bare-install auto-flip); the runtime toggle remains the explicit `villa coding-mode enter|exit` verb (D-04/D-06). This preserves the invariant's intent (no runtime auto-flip) while admitting the install-time entry the plan mandates.
- **Files modified:** `cmd/villa/coding-mode_test.go`
- **Commit:** `ff28631`

## Verification

- `make check` — green (vet + full test suite, all packages ok).
- `go test ./cmd/villa/ -run 'TestInstallCodingAgent|TestInstallCodingAgentServesCoder|TestAgentProbeReplaced|TestEvalAgentProof' -count=1` — 19 passed.
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` — ok (no leaked backend/image/marker literal in `cmd/villa`).
- `go test ./internal/orchestrate/ -count=1` — ok with NO golden update (`-update` not run; `git diff HEAD~2 HEAD` touches no golden file).
- Manual read: `install.go` `--coding-agent` branch sets `cfg.CoderModel`/`CoderQuant`/`CoderAgentCtx`/`CodingMode` and threads a non-nil `CodingMode` into `RenderInput`; `install_agent.go` success predicate is `Contains(TOKEN_B) && !Contains(TOKEN_A)`.

## Success Criteria

- [x] CR-01 closed: served model id (RenderInput.CodingMode descriptor + crush.json id) == `rec.Coder.Model` when `--coding-agent`; staged coder GGUF is the served model; served-id seam test asserts it.
- [x] WR-05 closed: readiness (and the shared verify agentTask) succeeds only on a real TOKEN_A→TOKEN_B replacement; an off-hardware truth-table test proves the append/echo false-green is gone.
- [x] No false-green remains: gate basis (`rec.Coder`), staged GGUF, served unit, crush.json, and the readiness proof all agree on the coder model.
- [x] Chat-only install render and the v1.3 unit goldens are byte-identical (off-path unchanged; no `-update`).

## Known Stubs

None. Both gaps are fully wired and asserted; no placeholder values or dead data paths introduced.

## Self-Check: PASSED

- FOUND: cmd/villa/install.go (modified)
- FOUND: cmd/villa/install_test.go (modified)
- FOUND: cmd/villa/install_agent.go (modified)
- FOUND: cmd/villa/install_agent_test.go (modified)
- FOUND: cmd/villa/coding-mode_test.go (modified)
- FOUND commit: ff28631 (Task 1)
- FOUND commit: 37474d5 (Task 2)
