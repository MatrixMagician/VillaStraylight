---
phase: 23-surfacing-backup-memory-aware-swap
plan: 04
subsystem: model swap / recall index guards (control plane)
tags: [ctrl-05, skew-guard, fail-closed, invariant-tests, d-09, d-10, d-11, pitfall-6]
requires:
  - recall.EmbeddingSkew + recall.Load (Plan 23-01 — THE single D-10 comparison)
  - modelswap.Run Deps seam (Phase 03-05 extraction)
  - orchestrate.Render memory-on path (Phase 19/20)
provides:
  - D-09 invariant pinned by tests (render byte-identity + restart scope + Deps-surface reflect pin)
  - D-10 fail-closed refusal in `villa recall index` (exitBlocked on confident skew; --rebuild sanctioned bypass)
  - D-10/D-11 read-only WARN in `villa install`'s memory readiness flow (readRecallState seam)
  - liveRecallStateLoad — the ONE shared fail-closed recall-state.json reader (recall verbs + install WARN)
affects:
  - 23-05 (on-hardware drill proves the --rebuild path end-to-end)
tech-stack:
  added: []
  patterns:
    - reflect-based Deps-surface pin (field-set drift forces a conscious D-09 review)
    - guard-between-read-and-stamp placement (Pitfall 6 — recorded truth survives every refusal)
    - nil-safe optional install seam (mirrors runMemoryChecks/doctor pattern)
key-files:
  created:
    - cmd/villa/install_memory_test.go
  modified:
    - internal/orchestrate/memory_test.go
    - internal/modelswap/modelswap_test.go
    - cmd/villa/recall.go
    - cmd/villa/recall_test.go
    - cmd/villa/install_memory.go
    - cmd/villa/install.go
decisions:
  - "OQ4 locked in code: --rebuild bypasses the refusal (the rebuild path id-preservingly resets the KB and clean-replaces content; the fresh stamp records the new identity) — proven by the rebuild-path test, on-hardware proof deferred to 23-05"
  - "Install WARN placement: (10b-pre) at the top of the memory-on readiness block, BEFORE the proof — the operator learns retrieval is corrupt even when the proof then fails; no-op installs skip it (status v3 skew indicator from 23-01 covers that path)"
  - "liveRecallStateLoad extracted as the single live recall-state reader shared by recallDeps.readState and installDeps.readRecallState — the two guards can never drift onto different readers"
metrics:
  duration: ~10 min
  completed: 2026-06-10
  tasks: 2
  commits: 3
---

# Phase 23 Plan 04: Memory-Aware Model Swap Summary

**One-liner:** CTRL-05 landed off-hardware — the D-09 invariant (chat swap never touches memory units, OWUI units, or the restart scope) is pinned by structural tests including a reflect Deps-surface pin, and the D-10 guards are live: `villa recall index` refuses fail-closed (exitBlocked) on a confident embedding model/dim skew against the recall-state stamp with `--rebuild` as the sanctioned bypass, while `villa install`'s memory flow WARNs read-only on the same skew through a nil-safe seam — no guard mutates anything (D-11).

## What was built

### Task 1 — D-09 invariant proofs (commit 0a8074c, test-only)
- `internal/orchestrate/memory_test.go` + `TestRenderChatSwapLeavesMemoryUnitsByteIdentical`: two memory-on `RenderInput`s differing ONLY in the chat model (`Cfg.Model`/`Cfg.Quant` plus `ModelFile`, the catalog-resolved projection of `Model` that `liveModelFile` derives) render byte-identical (`==`) texts for `villa-qdrant.container`, `villa-qdrant.volume`, `villa-embed.container`, and every `villa-openwebui.*` unit (discovered by prefix, with a non-zero-count sanity fatal); `villa-llama.container` MUST differ (anti-vacuity check that the swap is real).
- `internal/modelswap/modelswap_test.go`: the existing `:119` ordering test already recorded every Restart and asserted exactly one with `InstallServiceName` — extended its doc comment with the Phase-23 D-09/SC#3 framing (dashboard `handleSwitch` coverage via the shared `Run`) per the plan's extend-don't-duplicate directive. Added `TestSwapDepsSurfaceRestartIsOnlyServiceMutator`: a reflect pin of the exact 9-field Deps set, making "Restart is the only service mutator" structural — ANY new Deps field breaks the test and forces a conscious D-09 review.

### Task 2 — D-10 guards (TDD: RED a2e6be7, GREEN 9149b71)
- `cmd/villa/recall.go` (step 4a): `if !rebuild && recall.EmbeddingSkew(state, cfg.EmbeddingModel, cfg.EmbeddingDim) == recall.SkewMismatch` → refusal to errOut naming both identities ("the index was built with X (dim N) but config now says Y (dim M)"), the consequence ("corrupts retrieval"), and both fixes (`--rebuild` / revert the config) → `exitBlocked`. Placed immediately after the `deps.readState()` error check and BEFORE the rebuild block / `:343-344` stamp overwrite (which is unmoved) — Pitfall 6 placement, made permanent by the consecutive-runs regression test.
- `liveRecallStateLoad` extracted from the inline `liveRecallDeps.readState` closure: the ONE shared fail-closed reader (absent ⇒ empty/typed-Unknown; corrupt fail-closes inside `recall.Load`; only real I/O faults error).
- `cmd/villa/install_memory.go`: `warnRecallEmbeddingSkew(errOut, cfg, readRecallState)` — read-only WARN with the mirrored remediation ("run `villa recall index --rebuild` to re-index, or revert embedding_model/embedding_dim in config.toml"). Nil seam, read error, empty stamp, and match are ALL silent (typed-Unknown, never a fabricated alarm); never an exit-code change, never a write (D-11/T-23-18).
- `cmd/villa/install.go`: `readRecallState` seam added to `installDeps` (nil-safe, doctor optional-seam pattern), called at (10b-pre) — top of the memory-on readiness block before the proof — and live-wired to `liveRecallStateLoad`.
- Tests: `TestRecallIndexSkewGuard` (refusal text + zero-mutation + stamp-preserved, consecutive-runs Pitfall 6 regression, --rebuild bypass with fresh-stamp assert, empty-stamp no-alarm) and `TestInstallMemorySkewWarn` (mismatch WARN + read-only + exit unchanged, empty/match/unreadable silent, memory-off never reads, nil-seam safe).

## Deviations from Plan

### Auto-fixed / plan-consistent adjustments

**1. [Rule 3 - Blocking] `cmd/villa/install.go` edited (not in `files_modified`)**
- **Found during:** Task 2
- **Issue:** the plan directs routing the install WARN's state read "through the existing injectable seam conventions" — but the `installDeps` seam struct, the `runInstall` body (the only call site on the install flow), and `liveInstallDeps` all live in `install.go`, not `install_memory.go`.
- **Fix:** added the nil-safe `readRecallState` seam field, the (10b-pre) call, the live wiring, and the `internal/recall` import. The helper itself lives in `install_memory.go` as planned.
- **Commit:** 9149b71

**2. [Note] modelswap restart-recording already existed**
- The plan's "if the existing :119 test already records restarts, extend it rather than duplicating" branch applied: the len==1/`InstallServiceName` assert was already present, so the work became the D-09 doc-comment extension plus the new reflect surface pin.

**3. [Note] Three test assertions rebound through a local**
- The acceptance grep `grep -rn "state.EmbeddingModel !=" cmd/villa` → 0 initially matched three TEST assertions (stamp-outcome checks, not re-rolled comparisons). Rebound via `st := env.state` so the criterion holds literally; no logic change.

## Authentication Gates

None.

## Known Stubs

None — both guards are live-wired; the only deliberately deferred proof is the on-hardware `--rebuild` drill (Plan 23-05, per the plan's OQ4 note).

## Threat Flags

None new — all four register entries carry their mitigations: T-23-15 (refusal, fail-closed), T-23-16 (guard-before-stamp + consecutive-runs test), T-23-17 (byte-identity + restart-scope + Deps pin), T-23-18 (WARN proven read-only by test; no auto-reindex exists anywhere).

## Verification

- `make check` green (vet + full suite).
- `go test ./internal/inference -run TestSeamGrepGate` green (no new literals; the refusal/WARN texts carry no backend/image tokens).
- `go test ./cmd/villa -run 'TestRecallIndex|TestInstallMemory'` — 34 passed.
- Guard placement: the skew check sits textually between `deps.readState()` and the `state.EmbeddingModel` stamp assignment; the stamp lines are unmoved (diff-verified).
- `grep -n "SkewMismatch" cmd/villa/recall.go cmd/villa/install_memory.go` → both consumers use the Plan 23-01 helper; `grep -rn "state.EmbeddingModel !=" cmd/villa` → 0.
- Pre-existing `gofmt` violations in `cmd/villa/bench_compare.go` / `verify_memory_test.go` remain out of scope (already logged in deferred-items by 23-01).

## Commits

| Commit | Type | Content |
|--------|------|---------|
| 0a8074c | test | Task 1: D-09 render byte-identity + restart-scope/Deps-surface pins |
| a2e6be7 | test | RED: skew refusal + install WARN behavior matrix (verified failing) |
| 9149b71 | feat | GREEN: recall-index refusal, install WARN helper + seam, shared live reader |

## TDD Gate Compliance

Task 2 (`tdd="true"`): RED commit a2e6be7 verified failing (compile error on the not-yet-existing `readRecallState` seam, gating the whole `cmd/villa` test package) before the GREEN commit 9149b71. No refactor commit needed. Task 1 was test-only (invariant proof, no production code) per plan.

## Self-Check: PASSED

All 6 created/modified files and all 3 task commits verified present (2026-06-10).
