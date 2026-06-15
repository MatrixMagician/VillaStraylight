---
phase: 24-coder-fit-math-catalog-on-hardware-model-qualification
plan: 02
subsystem: recommend
tags: [go, recommend, fit-math, residency, schema-bump, golden-contract, pure-core]

# Dependency graph
requires:
  - phase: 24-coder-fit-math-catalog-on-hardware-model-qualification
    plan: 01
    provides: catalog schema 3 with Role/AgentCtx/CacheReuseSafe fields + three role:"coder" seed entries
provides:
  - Coder fit stage in recommend.Pick (pickCoder, agent_ctx-locked, post-reservation envelope, D-04/D-05)
  - Residency mode as a pure inequality output — "swap" when the best coder entry fits standalone, "shared" otherwise incl. refusals (D-06)
  - Recommendation schema 3 with an always-stamped append-only coder block above schema_version (D-07)
  - Chat pick bit-identical with coder entries present (pickBest role filter, D-03); --model override of a coder id warns-and-allows
  - cmd/villa human-table "Coder (agent profile)" section + the phase's single recommend.golden.json re-freeze
affects: [24-04 reconciliation, phase-25 swap verb (consumes residency), phase-28 surfacing]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Agent-profile fit isolation: pickCoder's signature takes (catalog, envelope) only — Overrides/effectiveCtx unreachable by construction (D-04, T-24-04)"
    - "Conservative-floor contract block: sharedCoderFit() stamped on the no-envelope refusal path so residency basis is never hidden (D-06/D-07, T-24-05)"

key-files:
  created:
    - internal/recommend/coder.go
    - internal/recommend/coder_test.go
  modified:
    - internal/recommend/recommend.go
    - internal/recommend/recommend_test.go
    - cmd/villa/recommend.go
    - cmd/villa/recommend_test.go
    - cmd/villa/testdata/recommend.golden.json

key-decisions:
  - "Coder block computed once in Pick after the reservation shrink and threaded to finalizeRecommendation as a 5th parameter (3 call sites) — the refusal path passes sharedCoderFit()"
  - "Override of a coder id resolved warn-and-allow (Open Question 1): chat KV still uses effectiveCtx; only pickCoder is agent_ctx-locked"
  - "Human table renders compactly per CONTEXT discretion: full fit inequality when a coder entry fits, one honest line when none does — JSON block always stamped"

patterns-established:
  - "Residency values as unexported consts (residencySwap/residencyShared) in coder.go — single source for the swap-verb contract strings"

requirements-completed: [CODER-02]

# Metrics
duration: 7min
completed: 2026-06-12
---

# Phase 24 Plan 02: Coder Fit Math + Recommend Schema 3 Summary

**Pure-core coder fit at catalog agent_ctx against the post-reservation envelope, residency as a pure swap/shared inequality output stamped on every path including refusals, and the recommend 2→3 contract frozen in exactly one golden re-freeze**

## Performance

- **Duration:** ~7 min
- **Started:** 2026-06-12T21:29:14Z
- **Completed:** 2026-06-12T21:36:13Z
- **Tasks:** 2 (Task 1 TDD)
- **Files modified:** 7

## Accomplishments

- `internal/recommend/coder.go` (new, kv.go house style): `pickCoder(c, envelope)` mirrors pickBest's eligibility loop (Bootstrap/UnifiedMemorySafe/MinEnvelopeBytes guards, most-capable-wins) with ctx ALWAYS `m.AgentCtx` — `effectiveCtx`/`Overrides` unreachable from its signature (D-04, grep-gate criterion green); total uses the saturating form verbatim (`addSaturating(addSaturating(weights, kvCacheBytes@agent_ctx), headroomBytes)`, WR-07)
- `CoderFit` contract block: model/quant/agent_ctx/4 fit terms/fits/residency, NO omitempty keys (Pitfall 6); residency "swap" only on a proven standalone fit, "shared" as the conservative floor — including the no-envelope refusal path, which stamps `sharedCoderFit()` (D-06/D-07)
- `recommendSchemaVersion` 2→3 with appended history sentence; `Coder` field placed directly above `SchemaVersion`; coder block computed in `Pick` after the embed-reservation envelope shrink (D-05 ordering) and threaded through all 3 `finalizeRecommendation` call sites
- D-03 bit-identity proven: `pickBest` skips `role:"coder"` entries (test includes a coder entry that would otherwise be the LARGEST fitting chat footprint); alternatives carry no coder entries; `pickOverride` warns-and-allows a coder id on the chat pick with a loud D-07-style note
- 9 new behavior tests in `coder_test.go` (swap-when-fits with exact term equality, shared floor, agent_ctx lock vs `--ctx`, bit-identical chat pick, refusal stamping, override warn-and-allow, post-reservation ordering with tight ±512 MiB margins, eligibility guards, most-capable-wins); existing SchemaVersion pins flipped 2→3
- `cmd/villa` human table gains the gated "Coder (agent profile)" section (full inequality + `residency: swap` when fitting; one honest no-fit line otherwise); JSON path unchanged (struct encodes automatically); `saveRecommendation` deliberately untouched (coder persistence is Phase 25)
- `recommend.golden.json` re-frozen EXACTLY ONCE: diff is a pure insertion of the populated `"coder"` object between `"memory_considered"` and `"schema_version"` plus the 2→3 value flip — verified by the plan's python key-order assertion and `git log` (one new commit touching the golden this phase)
- `make check` green (full suite incl. `TestSeamGrepGate` — no backend/image literals added); single package-level `update` flag (count 1)

## Task Commits

1. **Task 1 RED: failing coder fit stage tests + schema pins flipped to 3** - `0a03efe` (test)
2. **Task 1 GREEN: coder fit stage, residency derivation, schema-3 contract** - `2afd6b9` (feat)
3. **Task 2: human-table coder section + the one schema-3 golden re-freeze** - `be8ee0e` (feat)

## Files Created/Modified

- `internal/recommend/coder.go` - pickCoder + CoderFit + residency consts + sharedCoderFit (pure, no new imports beyond catalog)
- `internal/recommend/recommend.go` - schema const 3, Coder field above SchemaVersion, Pick coder stage after reservation shrink, finalizeRecommendation 5th param, pickBest role filter, pickOverride warn-and-allow
- `internal/recommend/coder_test.go` - 9 behavior tests covering all 8 planned behaviors plus eligibility guards
- `internal/recommend/recommend_test.go` - SchemaVersion 2→3 pin flips (3 sites + doc comments)
- `cmd/villa/recommend.go` - gated "Coder (agent profile)" table section after the ROCm advice block
- `cmd/villa/recommend_test.go` - fixture gains populated CoderFit + SchemaVersion 3 + history sentence; table test extended; new no-fit-line test
- `cmd/villa/testdata/recommend.golden.json` - the ONE re-freeze (pure append above schema_version, value 2→3)

## Decisions Made

- Coder block computed once in `Pick` (after the envelope shrink, before the chat branch) and passed to `finalizeRecommendation` — keeps the stamp unconditional on all 3 return paths with the refusal path passing the conservative floor
- Override-of-coder-id resolved warn-and-allow per the unsafe-override D-07 precedent; the note names the id and states the chat-pick-only usage
- Human table renders compactly (CONTEXT Claude's-discretion item): the no-fit case is one honest line naming residency "shared" rather than a zero-filled table

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Reverted unrelated `go fmt` drift in two untouched files**
- **Found during:** Task 2 (formatting pass)
- **Issue:** `go fmt ./cmd/villa/` reformatted `bench_compare.go` and `verify_memory_test.go` (pre-existing gofmt-version drift in a doc-comment list marker and struct alignment) — out of this task's scope
- **Fix:** `git checkout --` on both files; only task files were committed
- **Files modified:** none (revert)
- **Verification:** `git status` clean of unrelated Go files; `make check` green

---

**Total deviations:** 1 (scope-boundary revert; no code deviation from the plan's design)
**Impact on plan:** None — plan executed as written.

## TDD Gate Compliance

- Task 1 RED (`0a03efe`): test-only commit; package fails to build (`rec.Coder` undefined) — verified failing before implementation
- Task 1 GREEN (`2afd6b9`): all 45 recommend-package tests pass
- No refactor commit needed (implementation landed clean)

## Issues Encountered

None beyond the documented scope-boundary revert.

## User Setup Required

None.

## Next Phase Readiness

- Plan 24-03 (on-hardware qualification) is independent of this plan's code (wave 2 sibling); plan 24-04 reconciles measured dims against this fit math
- Phase 25's swap verb can consume `Recommendation.Coder.Residency` ("swap"/"shared" consts live in `internal/recommend/coder.go`)
- The recommend `--json` contract is now frozen at schema 3 — no later phase may touch `cmd/villa/testdata/recommend.golden.json`

---
*Phase: 24-coder-fit-math-catalog-on-hardware-model-qualification*
*Completed: 2026-06-12*

## Self-Check: PASSED

- All created files verified on disk (coder.go, coder_test.go, 24-02-SUMMARY.md)
- All task commits verified in git log (0a03efe, 2afd6b9, be8ee0e)
