# Phase 24: Coder Fit Math, Catalog & On-Hardware Model Qualification - Context

**Gathered:** 2026-06-12
**Mode:** --auto (recommended defaults selected; decisions logged in 24-DISCUSSION-LOG.md)
**Status:** Ready for planning

<domain>
## Phase Boundary

`villa recommend` produces an honest, hardware-qualified coding-model recommendation:

1. Catalog schema 2→3 ships `role:"coder"` entries (Qwen3-Coder-30B-A3B for all tiers; Qwen3-Coder-Next for 96/128 GB tiers) with revision-pinned GGUF artifacts and template provenance — append-only, existing chat entries untouched.
2. Recommend schema 2→3 adds a coder fit stage computed at agent-profile context, AFTER the embed reservation and chat fit, emitting an honest residency mode (`swap`/`shared`) that is purely an output of the fit math — one golden re-freeze.
3. Every shipped coder entry has passed a real multi-step agent-in-the-loop tool-call loop through llama-server `--jinja` on the pinned toolbox image, with KV footprint MEASURED at agent ctx on the gfx1151 box. Failing entries are deleted or re-pinned, never shipped on hope.
4. The toolbox re-pin decision (Qwen3-Coder-Next arch support + tool-call parser vintage + per-model `--cache-reuse` compatibility) is recorded as a decision with evidence before the catalog freezes.

NOT in this phase: render delta / coding-mode swap verb (Phase 25), Crush delivery/pin policy (Phase 26), install addon + egress proofs (Phase 27), any surfacing/status changes (Phase 28).

</domain>

<decisions>
## Implementation Decisions

### Catalog schema 3 shape (CODER-01)
- **D-01:** Coder entries extend `CatalogModel` with append-only OPTIONAL fields: `role` (absent/empty ⇒ chat — existing entries stay byte-untouched), `agent_ctx` (per-entry agent-profile context the fit is computed at), `cache_reuse_safe` (catalog-declared per-model `--cache-reuse` compatibility; absent ⇒ false, fail-closed), an agent sampling-preset field, and `template_provenance` (HF repo + revision + where the chat/tool-call template comes from). `schema_version` 2→3; loader's schema window widened accordingly.
- **D-02:** Coder GGUF shard URLs are revision-pinned at repo+revision level (`resolve/{revision}/...`, never `main`) because the embedded chat template is part of the artifact (PITFALLS #3). Per-shard `sha256` + `size_bytes` exactly as today.
- **D-03:** Chat-pick behavior is bit-for-bit unchanged: `role:"coder"` entries are excluded from the existing chat `pickBest` path (a role-aware filter, with absent-role ⇒ chat), so the v1.3 chat recommendation and its goldens are unaffected except for the deliberate schema-3 additions.

### Coder fit stage & residency derivation (CODER-02)
- **D-04:** The coder fit is computed at the entry's catalog-declared `agent_ctx` — the SAME value Phase 25 will render into `--ctx-size`. Never the chat `default_ctx`, never a global constant (fit-at-rendered-ctx, STATE.md Phase 24 risk note).
- **D-05:** Stage ordering inside `Pick`: envelope → embed reservation (D-01 v1.3, unchanged) → chat fit (unchanged) → coder fit stage against the same post-reservation envelope. The coder fit is standalone (weights + KV@agent_ctx + headroom ≤ post-reservation envelope), NOT additive to the chat claimant, because `swap` unloads the chat model.
- **D-06:** Residency mode is a pure inequality output: `swap` when the best qualified coder entry fits standalone at its `agent_ctx`; `shared` when no coder entry fits (the agent rides the existing chat endpoint). Never a preference, never a tier special-case, no co-resident output in v1.4 (deferred CODER-V2-01).
- **D-07:** The recommendation surfaces an always-stamped append-only `coder` block (model, quant, agent ctx, fit terms, residency mode, fits verdict) placed ABOVE `SchemaVersion`, stamped unconditionally through `finalizeRecommendation` exactly like the D-03 memory fields precedent — honest on refusals too. Recommend `schema_version` 2→3, exactly ONE golden re-freeze for this contract in this phase.

### On-hardware qualification protocol (CODER-03)
- **D-08:** Qualification is a dev-time on-hardware activity on the gfx1151 box (this IS the dev host), driven by a real agent loop: a locally-fetched Crush v0.76.0 used as a QUALIFICATION TOOL ONLY (not a shipped artifact — pinned delivery is Phase 26), pointed at llama-server `--jinja` on the pinned toolbox image, exercising a real multi-step tool-call task (read→edit→verify shape). Benchmark scores alone never qualify an entry.
- **D-09:** KV footprint is MEASURED at `agent_ctx` on the box (llama-server `/metrics` + GTT delta, MEM-DOC pattern) and recorded against the computed estimate; catalog `agent_ctx`/fit numbers must reflect measured reality before the catalog freezes. KV-quantization may only enter as a catalog-declared, benched choice — never an implicit default (aggressive K-cache quant corrupts tool-call JSON).
- **D-10:** Entries that fail qualification are deleted or re-pinned (different quant/revision), never shipped on hope. Qualification evidence (task transcript shape, pass/fail, measured KV) is recorded in the phase verification artifacts.

### Toolbox re-pin decision (CODER-03 / SC#4)
- **D-11:** Before the catalog freezes, verify the pinned `vulkan-radv` toolbox digest's llama.cpp vintage against: Qwen3-Next/DeltaNet arch support (llama.cpp PR #16095), the Feb-2026 tool-call parser fixes, and per-model `--cache-reuse` semantics (known incompatible with recurrent/hybrid models incl. the current chat model; verify for Qwen3-Coder-Next's hybrid attention). Record the keep/re-pin decision as a numbered decision with evidence. Any re-pin is digest-pinned (never floating/nightly — v1.1 standing decision).
- **D-12:** Qwen3-Coder-Next entries ship ONLY if the (re-)pinned image proves the arch + tool-call path on hardware; otherwise the 96/128 GB tiers ship 30B-A3B only and Next is deferred honestly — deletion over hope.

### Claude's Discretion
- Exact Go field/JSON key names for the new catalog and recommend fields (follow existing naming conventions; `SchemaVersion` stays the last tagged field).
- Golden test organization for the schema-3 re-freeze (coder-present variants; `-update` flag discipline as shipped).
- The exact qualification task script/steps and how its evidence is captured in phase docs.
- Human-readable CLI rendering of the coder block in `villa recommend` output.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Milestone verdicts & requirements
- `.planning/research/SUMMARY.md` — ratified v1.4 research verdicts (Crush-not-OpenCode, swap-based residency, Qdrant code-RAG rejected); Phase-1 (=24) deliverables + research flags
- `.planning/research/PITFALLS.md` — Pitfall 3 (tool-call/jinja template landmines, revision pinning), Pitfall 4 (agent-scale KV OOM, fit-at-rendered-ctx, KV-quant caution); `--cache-reuse` hybrid-model incompatibility
- `.planning/research/STACK.md` — Qwen3-Coder-30B-A3B / Qwen3-Coder-Next GGUF selections (unsloth, UD quants, sizes), `--jinja` server preset
- `.planning/research/ARCHITECTURE.md` — catalog/recommend modified-component inventory (schema 2→3 each, append-only)
- `.planning/REQUIREMENTS.md` — CODER-01, CODER-02, CODER-03 (this phase's requirement set)
- `.planning/ROADMAP.md` — Phase 24 goal + success criteria 1–4

### Contracts & code this phase modifies
- `internal/catalog/seed.json` — schema-2 catalog being extended (entry shape, shards/sha256 pattern)
- `internal/catalog/catalog.go` / `internal/catalog/load.go` — schema window + `CatalogModel` struct to extend
- `internal/recommend/recommend.go` — `Pick` pure core: reservation-before-fit (D-01 v1.3), `finalizeRecommendation` unconditional-stamp precedent (D-03 v1.3), append-only `SchemaVersion`-last discipline
- `internal/memory/footprint.go` — embedding reservation source (consumed, not modified)
- `cmd/villa/testdata/recommend.golden.json` — the byte-frozen recommend contract to re-freeze exactly once

### Standing constraints
- `.planning/STATE.md` §Blockers/Concerns — Phase 24 risk notes (template landmines, KV at agent ctx, toolbox re-pin)
- `docs/ARCHITECTURE.md` / `CLAUDE.md` — pure-core + seam conventions, golden-test evolve-append-only rules

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `recommend.Pick` reservation-before-fit pipeline (v1.3 D-01): the coder fit stage slots in after the existing chat fit against the already-shrunken envelope — no new envelope plumbing needed.
- `finalizeRecommendation` (recommend.go): the proven place to stamp new unconditional append-only contract fields; the coder block follows the `EmbeddingReservationBytes`/`MemoryConsidered` precedent exactly.
- `kvCacheBytes`/`headroomBytes`/`addSaturating` fit math: reusable verbatim for the coder fit at `agent_ctx` (saturating-sum WR-07 guard already handles absurd ctx).
- Catalog shard/sha256/size verification path + `internal/download`: revision-pinned coder GGUFs reuse the existing pull/verify machinery untouched.

### Established Patterns
- Append-only byte-frozen contracts: new fields above `SchemaVersion`, schema bump, one golden re-freeze with `-update` (catalog 2→3 and recommend 2→3 are this phase's two contracts, landed once).
- Typed-Unknown/fail-closed: absent `role` ⇒ chat; absent `cache_reuse_safe` ⇒ false; never silent defaults that widen capability.
- Honest refusal: `pickBest`'s no-fit path returns an honest empty pick with notes — the coder stage mirrors this with `shared` mode rather than refusing.
- On-hardware verification discipline: this box IS the gfx1151 dev host — qualification checkpoints run for real, never deferred or auto-approved (graphmind convention 7699b139).

### Integration Points
- `internal/catalog` (`seed.json`, `CatalogModel`, schema window) — schema 2→3.
- `internal/recommend` (`Pick`, `Recommendation`) — schema 2→3, coder fit stage.
- `cmd/villa/recommend.go` rendering + `cmd/villa/testdata/recommend.golden.json` — single re-freeze.
- Phase 25 consumes: qualified entries' `agent_ctx`/sampling/`cache_reuse_safe` metadata + the residency-mode output; the toolbox re-pin decision (if re-pin) lands at the inference seam in Phase 25, but the DECISION + evidence is recorded in this phase.

</code_context>

<specifics>
## Specific Ideas

- Model set is research-fixed: Qwen3-Coder-30B-A3B-Instruct (unsloth GGUF, UD-Q4_K_XL-class) for all tiers; Qwen3-Coder-Next (UD-Q4_K_XL 49.6 GB / UD-Q3_K_XL 36.3 GB) for 96/128 GB tiers — subject to on-hardware qualification (D-10/D-12).
- Agent-scale KV expectation from research: ~6 GiB at 64k f16 (~3 GiB q8_0), ~12 GiB at 128k for 30B-A3B — computed estimates that D-09 replaces with measured values.
- The `--cache-reuse` incompatibility with the CURRENT chat model (Qwen3.6-35B-A3B, hybrid) is already established; the open question this phase answers with evidence is Qwen3-Coder-Next.

</specifics>

<deferred>
## Deferred Ideas

- Co-resident `villa-coder` Quadlet unit — CODER-V2-01 (128 GB fit-gated v2 stretch; design shelf-ready in `.planning/research/ARCHITECTURE.md`).
- Qdrant/villa-embed-backed MCP semantic code search — CODER-V2-02 (v2-only, behind a pre-declared numeric eval it must WIN vs grep/LSP).
- KV-cache quantization as a default — only ever as a catalog-declared, benched, per-entry choice; not pursued in v1.4 unless qualification demands it (D-09).
- Coding-mode render delta, swap verb, Crush pin policy, install addon, surfacing — Phases 25–28 (boundary, not loss).

</deferred>

---

*Phase: 24-Coder Fit Math, Catalog & On-Hardware Model Qualification*
*Context gathered: 2026-06-12 (--auto mode, single pass)*
