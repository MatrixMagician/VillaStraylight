---
phase: 24-coder-fit-math-catalog-on-hardware-model-qualification
plan: 04
subsystem: catalog
tags: [catalog, coder, qwen3-coder, cache-reuse, freeze, toolbox-decision, fit-math]

# Dependency graph
requires:
  - phase: 24-01
    provides: schema-3 coder catalog entries (role:"coder", agent_ctx, agent_sampling, fail-closed cache_reuse_safe encoding)
  - phase: 24-02
    provides: coder fit at agent-profile ctx + the single recommend schema-3 golden re-freeze (be8ee0e)
  - phase: 24-03
    provides: on-hardware qualification evidence (3/3 PASS on pinned build 9496; A2 DeltaNet constant; A3 cache-reuse-true finding), operator-approved
provides:
  - Frozen, measurement-reconciled schema-3 coder catalog (cache_reuse_safe truth-up; no FAIL entry shipped)
  - D-13 toolbox keep-decision record (KEEP pinned digest sha256:9a74e555 / build 9496, no re-pin) ratifying the D-11 question with on-hardware evidence
  - Per-entry qualification evidence summary (verdict, measured-vs-computed KV, GTT, cache-reuse, D-10/D-12 disposition)
  - Operator-ratified catalog freeze (SC#1/SC#4 gate closed)
affects: [phase-25-cmode, inference-seam, modelswap, render]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Build-scoped truth claim: cache_reuse_safe:true is scoped to build 9496 (mechanism = DeltaNet recurrent-state context checkpoints, not GQA KV shifting); re-probe required if a future toolbox re-pin removes context-checkpoint support"
    - "Decision-before-freeze gate: the numbered toolbox keep/re-pin decision is recorded with static + functional evidence BEFORE the catalog is frozen, then ratified at a blocking human checkpoint"

key-files:
  created:
    - .planning/phases/24-coder-fit-math-catalog-on-hardware-model-qualification/24-TOOLBOX-DECISION.md
    - .planning/phases/24-coder-fit-math-catalog-on-hardware-model-qualification/24-QUALIFICATION-EVIDENCE.md
  modified:
    - internal/catalog/seed.json
    - internal/catalog/catalog_test.go

key-decisions:
  - "Decision D-13: KEEP the pinned vulkan-radv digest sha256:9a74e555 (build 9496) — no re-pin; fallback build 9579 not needed (9496 has full Qwen3-Next arch + working Qwen3-Coder tool-call parser)"
  - "cache_reuse_safe truth-up: all three coder entries set to true as the literal probe verdict (build-9496-scoped; Next entries reuse via recurrent-state context checkpoints, FINDING A3)"
  - "D-10/D-12: no entry failed — no deletion or re-pin; delete-over-hope contingency recorded as not-triggered"

patterns-established:
  - "Pattern: every shipped catalog value/disposition in a freeze traces to a named qualification/*/verdict.md evidence file; the freeze is ratified at a blocking human checkpoint (T-24-13 mitigation)"
  - "Pattern: digest-pinned only — any toolbox re-pin would be digest-pinned and land at the internal/inference seam in a later phase, never as a Phase-24 Go literal (TestSeamGrepGate)"

requirements-completed: [CODER-01, CODER-03]

# Metrics
duration: ~6min
completed: 2026-06-13
---

# Phase 24 Plan 04: Catalog Reconciliation, D-11 Toolbox Decision & Operator-Ratified Freeze Summary

**Frozen schema-3 coder catalog reconciled to measured reality (cache_reuse_safe:true truth-up on all three entries, no FAIL shipped) plus the D-13 KEEP-build-9496 toolbox decision recorded with static+functional evidence and ratified at the operator freeze checkpoint.**

## Performance

- **Duration:** ~6 min (continuation agent; Tasks 1–2 executed in a prior session)
- **Completed:** 2026-06-13
- **Tasks:** 3 (2 auto + 1 blocking checkpoint, operator-approved)
- **Files modified:** 4 (2 product: seed.json + catalog_test.go; 2 planning artifacts)

## Accomplishments

- **Catalog reconciled and frozen:** `cache_reuse_safe: true` truthed-up on the two hybrid Next entries from the probe evidence (exactly 2 lines added to `seed.json`; the 30B entry already carried true). All three surviving coder entries trace to a PASS verdict — no FAIL entry shipped, no deletion/re-pin required (D-10/D-12 contingency recorded as not-triggered).
- **D-13 toolbox decision recorded before the freeze:** KEEP the pinned `vulkan-radv` digest `sha256:9a74e555…` (build 9496) — no re-pin. All three D-11 checks (Qwen3-Next/DeltaNet arch, tool-call parser vintage, per-model `--cache-reuse` semantics) closed with BOTH static binary-string evidence and a functional on-hardware proof.
- **FINDING A3 reconciled honestly:** the Next hybrids returned cache-reuse `true` not via `n_cache_reuse` chunk reuse but via recurrent-state context checkpoints (75.376 MiB restore); the claim is recorded as **build-9496-scoped**, to be re-probed if a future re-pin drops checkpoint support.
- **Operator-ratified freeze (SC#1/SC#4):** the blocking checkpoint was approved — seed diff, D-13 decision, evidence summary, untouched recommend golden, and green `make check` (incl. `TestSeamGrepGate`) all confirmed. Catalog is FROZEN; CODER-01 and CODER-03 satisfied.

## Task Commits

1. **Task 1: Reconcile measured values into the catalog (cache_reuse_safe truth-up, D-09/D-10/D-12)** — `e18ce7b` (fix) — *committed prior session*
2. **Task 2: Record D-11 toolbox decision + qualification evidence summary** — `cff6449` (docs) — *committed prior session*
3. **Task 3: Operator freeze approval — catalog FROZEN (SC#1/SC#4 blocking checkpoint)** — `bd9a2f0` (docs)

**Plan metadata:** committed with this SUMMARY (docs: complete plan).

## Files Created/Modified

- `internal/catalog/seed.json` — `cache_reuse_safe: true` added to the two Qwen3-Coder-Next entries (Task 1; build-9496-scoped truth-up).
- `internal/catalog/catalog_test.go` — expected-value map tests track the reconciled `cache_reuse_safe` values (Task 1).
- `.planning/phases/24-.../24-TOOLBOX-DECISION.md` — D-13 numbered decision record (KEEP build 9496, no re-pin) with three-check static+functional evidence; freeze-ratification section appended in Task 3.
- `.planning/phases/24-.../24-QUALIFICATION-EVIDENCE.md` — per-entry summary table (verdict, measured-vs-computed KV, GTT, cache-reuse, disposition).

## Final Frozen Coder Catalog State

| Entry | tier | agent_ctx | cache_reuse_safe | verdict |
|-------|------|-----------|------------------|---------|
| qwen3-coder-30b-a3b | 64 | 65536 | true | PASS |
| qwen3-coder-next-q4 | 128 | 131072 | true | PASS |
| qwen3-coder-next-q3 | 96 | 131072 | true | PASS |

Python gate confirms: coder entries survive, no `/resolve/main/` floating URLs in any coder shard.

## Decisions Made

- **D-13 (KEEP build 9496, no re-pin):** the pinned digest qualified all three entries on hardware with full Qwen3-Next arch support and a working Qwen3-Coder tool-call parser; the pre-identified fallback (build 9579, `sha256:9df33843…`) was not needed. Any re-pin would have been digest-pinned and applied at the `internal/inference` seam in Phase 25 — nothing lands there on account of this decision.
- **Build-9496-scoped cache_reuse_safe:true:** the catalog claim is the literal probe verdict; its honest meaning is "rendering `--cache-reuse 256` (Phase 25) is safe on this entry under build 9496." The Next-entry reuse mechanism is checkpointing, recorded so Phase 25 / future re-pins know the basis is not GQA KV shifting.

## Deviations from Plan

None - plan executed exactly as written. Tasks 1–2 reconciled the catalog and recorded the decision per the plan; Task 3's blocking checkpoint was approved by the operator with no requested changes. The recommend golden (`cmd/villa/testdata/recommend.golden.json`) was not touched — last re-freeze remains 24-02 (`be8ee0e`).

## Issues Encountered

None. `make check` is green over the final frozen catalog; `TestSeamGrepGate` green (no backend literals leaked; no Phase-24 Go image literal from the keep/re-pin decision).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- **Phase 25 (CMODE) is unblocked.** Phase 25 consumes the frozen entries' `agent_ctx` / `agent_sampling` / `cache_reuse_safe` and the D-13 decision record. The KEEP decision means no inference-seam digest change is owed from Phase 24; Phase 25 renders `--cache-reuse 256` against entries whose safety is build-9496-scoped (re-probe gate documented in 24-TOOLBOX-DECISION.md Check 3 if the toolbox is ever re-pinned).
- Phase 24 is the FINAL plan of the phase — all 4 plans complete; CODER-01/02/03 satisfied.

## Self-Check: PASSED

- Files verified on disk: `24-04-SUMMARY.md`, `24-TOOLBOX-DECISION.md`, `24-QUALIFICATION-EVIDENCE.md`.
- Commits verified in git: `e18ce7b` (Task 1), `cff6449` (Task 2), `bd9a2f0` (Task 3 freeze approval), `ea4db0d` (SUMMARY).

---
*Phase: 24-coder-fit-math-catalog-on-hardware-model-qualification*
*Completed: 2026-06-13*
