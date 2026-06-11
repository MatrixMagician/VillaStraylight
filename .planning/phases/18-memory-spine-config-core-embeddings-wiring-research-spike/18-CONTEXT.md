# Phase 18: Memory Spine — config core + embeddings/wiring research spike - Context

**Gathered:** 2026-06-09
**Status:** Ready for planning

> Captured in `--auto` mode (single pass). Each decision below was auto-selected
> as the recommended option, grounded in the locked v1.3 milestone decisions
> (priority memory, ROADMAP, REQUIREMENTS). Review before planning.

<domain>
## Phase Boundary

Land the **spine** of the v1.3 memory stack — the pure `internal/memory` core plus
the `config.toml` memory fields that render/recommend/preflight will later read —
**and** de-risk the version-sensitive integration by resolving and pinning the
three choices later phases freeze: (1) the embeddings runtime, (2) the exact Open
WebUI RAG/Memory env keys (re-verified against the pinned OWUI digest), and (3) the
pinned embedding model + its measured memory footprint.

**In scope:** config schema for the memory stack (opt-in, default-off, byte-identical
when off); the `internal/memory` pure-decision core (no host I/O, no image literals,
seam gate stays green); and a research spike that records the embeddings-runtime,
OWUI-env-contract, and embedding-model/footprint decisions.

**Out of scope (later phases, do NOT build here):** rendering/starting any Quadlet
unit (Qdrant or `villa-embed`) — Phase 19; wiring OWUI env / offline lockdown —
Phase 20; the chats→Knowledge recall indexer — Phase 21; recommend/preflight/doctor
fit & gating integration — Phase 22; status schema bump + backup/swap coverage —
Phase 23. This phase writes the *spine and the decisions*, not the services.

</domain>

<decisions>
## Implementation Decisions

### `internal/memory` pure core API
- **D-01:** A NEW pure core `internal/memory` mirrors the `recommend.Pick` /
  `preflight` idiom: typed inputs → typed decision values, **zero host I/O**. It
  imports neither `os/exec` nor any container-image literal; `TestSeamGrepGate`
  stays green (the gate walks `internal/` + `cmd/villa`).
- **D-02:** The core exposes three responsibilities (exact names are the planner's
  call): (a) an **embedding-footprint** function `model → Bytes` (typed-Unknown on
  miss, never bare 0); (b) an **enablement-and-fields-valid gate** returning a typed
  decision (memory on/off + all required fields present & valid) — refuse-with-reason,
  fail-closed on bad config; (c) a **render-view input struct** that `orchestrate`
  consumes later (the recommend→orchestrate handoff pattern), carrying no image
  literals — only resolved values (model id, dim, service addrs/ports).
- **D-03:** Footprint is reserved **before** chat-model fit on shared gfx1151 GTT
  (this phase provides the function; CTRL-01's call site is Phase 22). The embedding
  **dimension is a load-bearing pinned value** — changing it corrupts existing
  vectors with no auto-reindex (drives the Phase-23 memory-aware swap guard).

### config.toml memory schema
- **D-04:** Memory fields follow the **existing flat, self-healing pattern**
  (`dashboard_*`/`chat_*` precedent in `internal/config/villaconfig.go`), not a new
  config file. Default **`memory_enabled = false`**.
- **D-05:** **Byte-identical guarantee (SC#1):** an existing v1.2 install stays
  byte-identical until the user opts in. Memory fields are defaulted/self-healed on
  load (extend `normalizeVilla` / `defaultConfig` — single source of the default
  literals, never re-hard-coded) and are NOT emitted to disk for a non-opted-in
  install. `SaveVilla`'s XDG-confined, 0600, path-traversal-guarded discipline is
  reused unchanged; no shell interpolation.
- **D-06:** Fields cover: enable flag, pinned embedding model id, embedding
  dimension, and the in-network service endpoints (Qdrant + embeddings) used over
  `villa.network`. Endpoints are **loopback / container-DNS only** — no published
  host port for Qdrant (PRIV-01 continuity); never widen a bind.

### Embeddings runtime (spike decision — recorded here)
- **D-07:** **Dedicated `villa-embed` llama-server** (reusing the pinned kyuz0
  toolbox image, OpenAI `/v1/embeddings`, container-DNS only) — NOT Open WebUI's
  built-in embedder. Rationale: OWUI's built-in path lazily downloads the embed
  model from HuggingFace on first upload (the #1 runtime-privacy risk); a dedicated
  server gives memory-fit control and reuses an already-pinned image (no new
  embeddings image). **Flagged for spike confirmation** against the live image.

### Embedding model + footprint (spike decision — recorded here)
- **D-08:** Pin **`nomic-embed-text-v1.5`** (GGUF, ~768-dim), served by
  `villa-embed`. The spike confirms the exact GGUF source/quant and the **measured**
  footprint in bytes (feeds D-03). Dimension is pinned and recorded for skew warning.

### Open WebUI env contract (spike decision — recorded here)
- **D-09:** The spike **re-verifies against the pinned OWUI digest** the exact env
  keys the wiring phase will freeze: `VECTOR_DB=qdrant` (+ Qdrant URI/key),
  `RAG_EMBEDDING_ENGINE=openai` (+ `RAG_OPENAI_API_BASE_URL`/key + embedding model),
  `ENABLE_MEMORIES`, `ENABLE_PERSISTENT_CONFIG=false`, and the offline lockdown set
  (`OFFLINE_MODE`/`HF_HUB_OFFLINE`/`ANONYMIZED_TELEMETRY=False`/`*_AUTO_UPDATE=false`).
  Phase 18 **records** the verified keys; it does NOT write any env block (Phase 20).

### Seam placement (carried forward — milestone decision)
- **D-10:** The two new image literals (Qdrant + `villa-embed`) live behind the
  **`orchestrate` managed-service render path** (the `openwebui.go` pattern), NOT
  `inference.BackendFor` / `TestSeamGrepGate`. Open WebUI/Qdrant/embeddings are
  managed-service constants, a different category from GPU backend tokens. Phase 18
  introduces no image literal at all — but the core must be designed so the literal
  lands in `orchestrate` later.

### Claude's Discretion
- Exact Go symbol names, struct field layout, and TOML key spellings (flat
  `memory_*` vs grouped) are the planner's call within D-04/D-05.
- Whether the spike confirmations live in RESEARCH.md or a short decisions appendix
  — researcher's call; the three decisions (D-07/D-08/D-09) must be explicitly
  recorded with evidence either way.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` → Phase 18 section — goal, 3 success criteria, INFRA-04.
- `.planning/REQUIREMENTS.md` — INFRA-04 (config-driven memory stack) plus the
  cross-phase requirements this spine feeds: INFRA-01/02/03, PRIV-04/05, CTRL-01,
  CTRL-04, CTRL-05 (traceability table maps each to its phase).
- `.planning/PROJECT.md` — v1.3 milestone goal, integrate-not-rebuild constraint,
  out-of-scope (custom Go RAG/embeddings engine; cloud embedding APIs).

### Code touchpoints (the spine extends these)
- `internal/config/villaconfig.go` — `VillaConfig`, `defaultConfig`,
  `normalizeVilla`, `LoadVilla`/`SaveVilla` — the self-healing flat-field pattern and
  XDG/0600/traversal-guard discipline D-04/D-05 reuse.
- `internal/orchestrate/openwebui.go` — the managed-service render-path pattern
  (image literal + accessor behind the orchestrate seam) that D-10's later units copy.
- `internal/recommend/recommend.go` — the pure `Pick` core idiom `internal/memory`
  mirrors; also the future CTRL-01 footprint-reservation call site.
- `internal/inference/seam_test.go` — `TestSeamGrepGate` scope (must stay green;
  `internal/memory` must introduce no backend/image literal).

### Supporting research (re-verify version: dir header still reads v1.2)
- `.planning/research/STACK.md`, `ARCHITECTURE.md`, `PITFALLS.md`, `FEATURES.md` —
  contain Qdrant/embedding/v1.3 content but `SUMMARY.md` header still says "v1.2
  milestone"; the spike should confirm currency before relying on specifics.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/config` self-healing pattern (`normalizeVilla` + `defaultConfig` as the
  single source of default literals) — directly reused for the byte-identical,
  default-off memory fields (D-04/D-05).
- `internal/orchestrate/openwebui.go` managed-service render path + image accessor —
  the template for where the Qdrant/`villa-embed` literals land in Phase 19 (D-10).
- `internal/recommend` pure-core shape (typed inputs → `Recommendation`, no I/O) —
  the structural model for `internal/memory` (D-01/D-02).
- `internal/detect` typed-Unknown wrappers (`Bytes`/`Bool`) — footprint returns a
  typed-Unknown on a catalog miss, never bare 0 (D-02a/honesty-by-construction).

### Established Patterns
- **Config is the single source of truth; units regenerate from config** — memory
  fields drive later renders, never hand-edited units (INFRA-04).
- **Pure core + injectable seam; orchestrate is the only impure module** —
  `internal/memory` stays pure; host effects come in Phases 19–20.
- **Seam-locked literals** — backend/image tokens stay behind their seam; the new
  memory core must leak none (`TestSeamGrepGate`).
- **Loopback-only binds / no shell interpolation** — endpoint fields are
  container-DNS/loopback; no field is shell-interpolated.

### Integration Points
- `config.toml` → (later) `recommend.Pick` footprint reservation (CTRL-01, P22) and
  `orchestrate` render of Qdrant + `villa-embed` (INFRA-01/02, P19).
- `internal/memory` render-view struct → `orchestrate` managed-service render (P19).
- Embedding dimension field → Phase-23 backup manifest + memory-aware model-swap
  guard (CTRL-04/CTRL-05).

</code_context>

<specifics>
## Specific Ideas

- Embeddings: dedicated `villa-embed` llama-server, OpenAI `/v1/embeddings`,
  `nomic-embed-text-v1.5` (~768-dim), reusing the pinned kyuz0 toolbox image.
- OWUI wiring target keys: `VECTOR_DB=qdrant`, `RAG_EMBEDDING_ENGINE=openai`
  (loopback), `ENABLE_MEMORIES`, `ENABLE_PERSISTENT_CONFIG=false`, plus the
  offline-lockdown env set — to be confirmed against the pinned OWUI digest.
- #1 risk to keep front-of-mind for downstream phases: runtime privacy (OWUI HF
  lazy-download + ChromaDB PostHog telemetry) — mitigated by Qdrant + local
  embeddings + offline env + a runtime zero-outbound smoke test (Phase 20).

</specifics>

<deferred>
## Deferred Ideas

- Rendering/starting Qdrant + `villa-embed` Quadlet units — **Phase 19**.
- OWUI env wiring + offline lockdown + zero-outbound runtime test — **Phase 20**.
- chats→Knowledge semantic recall indexer — **Phase 21** (largest phase).
- recommend footprint-reservation + preflight memory gating + doctor checks —
  **Phase 22** (CTRL-01/CTRL-03/CTRL-06).
- status schema bump (2→3) + dashboard memory rows + backup/swap coverage —
  **Phase 23** (CTRL-02/CTRL-04/CTRL-05).

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 18-memory-spine-config-core-embeddings-wiring-research-spike*
*Context gathered: 2026-06-09*
