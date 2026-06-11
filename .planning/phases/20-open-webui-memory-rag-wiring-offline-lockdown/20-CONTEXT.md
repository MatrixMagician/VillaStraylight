# Phase 20: Open WebUI Memory/RAG Wiring + Offline Lockdown - Context

**Gathered:** 2026-06-09
**Status:** Ready for planning

> Captured in `--auto` mode (single pass). Each decision below was auto-selected
> as the recommended option, grounded in the **PINNED** Phase-18 env contract
> (`18-DECISIONS.md` D-09, re-verified 2026-06-09), the on-hardware Phase-19 stack
> (`villa-qdrant` + `villa-embed` live on `villa.network`, container-DNS only),
> ROADMAP Phase 20, and REQUIREMENTS INFRA-03 / MEM-01..04 / KB-01..03 / PRIV-05.
> Review before planning. The single load-bearing choice that was genuinely open —
> `ENABLE_QDRANT_MULTITENANCY_MODE` — is decided in D-01 below and MUST be
> re-verified against the pinned OWUI digest at research time **before any vectors
> exist** (toggling it later disconnects existing collections).

<domain>
## Phase Boundary

Wire **Open WebUI's native Memory and RAG** (env-only, behind the `orchestrate`
seam) to the Phase-19 services so the assistant can (a) remember user facts across
chats and (b) answer from uploaded documents with citations — **all strictly local**,
with `config.toml` as the single source of truth, proven by a **runtime firewalled
zero-outbound smoke test** (not just install-time green).

**In scope:**
- Add the RAG/Qdrant/memory env block to the existing `internal/orchestrate`
  `openwebui.go` ordered `Env` slice, per the **PINNED D-09 key table**:
  `VECTOR_DB=qdrant`, `QDRANT_URI=http://villa-qdrant:6333`, `QDRANT_API_KEY` (empty),
  `ENABLE_QDRANT_MULTITENANCY_MODE` (D-01), `QDRANT_COLLECTION_PREFIX=open-webui`,
  `RAG_EMBEDDING_ENGINE=openai`, `RAG_OPENAI_API_BASE_URL=http://villa-embed:8080/v1`,
  `RAG_OPENAI_API_KEY=sk-no-key-required`, `RAG_EMBEDDING_MODEL=nomic-embed-text-v1.5`,
  `RAG_EMBEDDING_MODEL_AUTO_UPDATE=False`, `ENABLE_MEMORIES=True`,
  and the **MANDATORY load-bearing** `ENABLE_PERSISTENT_CONFIG=False`.
- Render the new env block **conditional on `memory_enabled=true`** so a memory-off
  install stays **byte-identical** to v1.2/v1.1 (Phase-18 SC#1 continuity).
- A single deliberate **golden re-freeze** of the OWUI container golden + the
  telemetry-frozen test re-audit (the env block is byte-frozen; this is an
  intentional, append-only evolution).
- An automatic-memory-extraction **toggle** that is **opt-in / default-off** (MEM-03 / SC#2).
- A **runtime firewalled zero-outbound smoke test** that drives the real
  document-upload → chunk → embed → retrieve path and asserts **no external host is
  reached** (PRIV-05 / SC#4), extending the Phase-19 proof-seam pattern.

**Out of scope (later phases — do NOT build here):**
- The chats→Knowledge semantic recall indexer — **Phase 21**.
- `recommend` footprint reservation / `preflight` host gating / `doctor` memory
  health — **Phase 22**.
- `status`/dashboard memory rows (schema bump 2→3), backup/restore of the Qdrant
  volume, memory-aware model swap — **Phase 23**.
- Rendering/starting the Qdrant + `villa-embed` units themselves — **done in Phase 19**.

This phase wires the **OWUI env + proves runtime privacy**; the services already exist.

</domain>

<decisions>
## Implementation Decisions

### Qdrant multitenancy mode — the load-bearing pending choice (INFRA-03, D-09)
- **D-01:** Set **`ENABLE_QDRANT_MULTITENANCY_MODE=True`** explicitly (OWUI's own
  default), locked NOW **before any vectors exist**. Rationale: (a) it matches
  OWUI's default (integrate-not-rebuild); (b) it is **Qdrant's own recommended
  layout** — one collection with tenant-partitioned payloads scales far better than
  collection-per-knowledge (which wastes RAM and degrades on the shared gfx1151 GTT
  envelope); (c) it is the forward-compatible substrate for the Phase-21 indexer and
  Phase-23 backup (one shared collection is simpler to snapshot than N collections).
  **Hard constraint:** this value is byte-frozen the moment the first document is
  embedded — flipping it later silently disconnects existing collections.
  **Research MUST re-verify** the key name + behaviour against the **pinned OWUI
  digest** (the D-09 table was verified against `:main` docs, not the pin) and
  confirm the Phase-21/23 collection-layout implications before the env block freezes.

### OWUI env block evolution + golden re-freeze (INFRA-03, MEM-01)
- **D-02:** Add the D-09 RAG/Qdrant/memory keys as an **ordered group appended
  after the existing env entries** in `buildOpenWebUIView()` (`internal/orchestrate/
  openwebui.go`). Preserve the existing order; the existing block already sets
  `ANONYMIZED_TELEMETRY=False`, `DO_NOT_TRACK=True`, `SCARF_NO_ANALYTICS=True`,
  `OFFLINE_MODE=True`, `ENABLE_VERSION_UPDATE_CHECK=False`, `HF_HUB_OFFLINE=1`,
  `WEBUI_AUTH=True`. **Net-new keys** to append: the VECTOR_DB/QDRANT_*/RAG_* block,
  `ENABLE_MEMORIES`, `RAG_EMBEDDING_MODEL_AUTO_UPDATE=False`, and the **mandatory**
  `ENABLE_PERSISTENT_CONFIG=False`.
- **D-03:** **`ENABLE_PERSISTENT_CONFIG=False` is MANDATORY, not optional** (D-09).
  The RAG/embedding/memory keys are OWUI PersistentConfig (`ConfigVar`) values read
  from OWUI's DB unless this flag is `False`; without it the env appears wired but is
  **silently ignored after first boot** and "config is the single source of truth"
  (INFRA-03/INFRA-04) does not hold. Planning MUST treat its absence as a phase failure.
- **D-04:** The new env block renders **only when `memory_enabled=true`**; a
  memory-off install is **byte-identical** to the pre-memory render (Phase-18 SC#1).
  The env values are sourced from the resolved config / Phase-18 `internal/memory`
  render-view (`QdrantAddr`/`QdrantPort`/`EmbedAddr`/`EmbedPort`/`EmbeddingModel`/
  `EmbeddingDim`) — **no re-typed host literals**; image/marker tokens stay
  seam-locked (`TestSeamGrepGate` stays green).
- **D-05:** Treat the env change as a **single deliberate golden re-freeze** of the
  OWUI container golden (`internal/orchestrate/testdata/*openwebui*.golden`) plus the
  telemetry-frozen test re-audit. Re-freeze once, intentionally, append-only — never
  silently widen.

### Personalized memory (MEM-01/02/04) + auto-extraction toggle (MEM-03 / SC#2)
- **D-06:** Personalized memory is enabled via `ENABLE_MEMORIES=True` (D-09). The
  manual save / view / edit / delete flows (MEM-02, MEM-04) and cross-chat injection
  (MEM-01) are **native OWUI features** behind that flag — integrate, do not rebuild.
  Verification exercises the real OWUI Memory UI/API: a deleted memory MUST stop
  being injected (SC#1).
- **D-07:** **Automatic LLM-assisted memory extraction defaults OFF and is
  user-toggleable** (MEM-03 / SC#2). Given the local-model quality caveat it is NOT
  silently default-on. Research MUST identify the exact OWUI mechanism that controls
  automatic extraction (env / `ConfigVar` / function-filter) and confirm it is
  honored under `ENABLE_PERSISTENT_CONFIG=False` — if it is a runtime UI/function
  toggle rather than an env key, record that the "default-off" guarantee is enforced
  by not enabling it, and document where the user flips it.

### Document knowledge base + citations (KB-01/02/03)
- **D-08:** Document upload → chunk → embed → retrieve runs **entirely through the
  local `villa-embed` + Qdrant path** (`RAG_EMBEDDING_ENGINE=openai` pointed at
  `villa-embed:8080/v1`, `VECTOR_DB=qdrant`). No cloud API, no runtime model download
  (offline lockdown + the dedicated local embedder already remove the HF runtime
  vector). Citations (KB-02) are native OWUI RAG behaviour — verification confirms
  they render; research confirms whether any setting gates citation display.
- **D-09:** **nomic retrieval-prefix caveat (Phase-18 D-08):** `nomic-embed-text-v1.5`
  expects `search_document:` / `search_query:` task-instruction prefixes for optimal
  retrieval. Research MUST check whether the pinned OWUI digest exposes an embedding
  prefix/instruction setting; **if exposed, set it; if not, accept functional
  (sub-optimal) retrieval and record the limitation** — do NOT block the phase on
  best-possible recall. KB-02/03 require working local retrieval + citations, not
  maximal recall.

### Runtime zero-outbound proof (PRIV-05 / SC#4) — the headline new gate
- **D-10:** Add a **runtime firewalled document-upload zero-outbound smoke test**
  that **extends the Phase-19 proof-seam pattern** (`evalMemoryProof` pure core +
  injected probes in `cmd/villa/install_memory.go`; `runProbeCurl` /
  `liveMemoryProof` over `villa.network` with FIXED-arg `podman run` — no shell
  interpolation, no host port). The test drives the **real RAG path** (upload a
  document, embed it, retrieve + cite) under an **egress-blocked network** and
  asserts **no external host is reached** — only `villa.network` container-DNS
  traffic. **Honesty-by-construction:** a silent skip / unevaluable result is a
  **FAIL**, never a false-green (mirrors the offload-assert discipline). This is a
  **runtime** assertion distinct from install-time green (SC#4 says so explicitly).
- **D-11:** The existing **loopback-only privacy audit stays green** — neither the
  env change nor the smoke test opens a new published host port; OWUI keeps its one
  existing PublishPort (chat UI), Qdrant/embed remain container-DNS only.

### Claude's Discretion
- Exact Go symbol/spelling, env-pair ordering within the appended group, and whether
  the smoke test is wired into `install` readiness vs a dedicated verify subcommand
  vs an on-hardware verification-wave step — planner's call within D-02/D-10.
- The exact egress-blocking mechanism for the runtime test (`--network none` for the
  pure-offline embedding leg vs a firewalled/`villa`-only network with an outbound
  sentinel) — planner/researcher's call, as long as it proves a real upload reaches
  no external host.
- Whether `QDRANT_API_KEY` stays empty (private `villa.network`) or a generated key
  is added (no schema change either way) — defer unless research shows OWUI requires
  a non-empty value.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope, requirements & PINNED env contract
- `.planning/ROADMAP.md` → **Phase 20** section — goal + 4 success criteria.
- `.planning/REQUIREMENTS.md` — **INFRA-03** (OWUI wired to Qdrant + local embeddings,
  `ENABLE_PERSISTENT_CONFIG=false`), **MEM-01/02/03/04** (cross-chat memory, manual
  save, toggleable auto-extraction, view/edit/delete), **KB-01/02/03** (upload,
  cited answers, fully-local chunk/embed/retrieve), **PRIV-05** (no telemetry +
  **runtime** firewalled zero-outbound smoke test).
- `.planning/phases/18-memory-spine-config-core-embeddings-wiring-research-spike/18-DECISIONS.md`
  — **PINNED D-09** is the canonical OWUI env-key table Phase 20 freezes (every key,
  target value, PersistentConfig flag, and the `ENABLE_QDRANT_MULTITENANCY_MODE`
  PENDING flag now resolved in D-01). Also D-07 (`villa-embed` runtime) and D-08
  (model/dim/768 + the retrieval-prefix usage caveat).
- `.planning/phases/18-…/18-RESEARCH.md` → "Spike Decisions (PINNED with evidence)"
  + `## Sources` — OWUI `docs/reference/env-configuration.mdx` evidence (fetched
  2026-06-09); the **currency-watch** note: env reference tracks OWUI `:main`, so
  Phase 20 MUST re-verify keys against the **pinned OWUI digest**.
- `.planning/phases/19-vector-store-local-embeddings-services/19-CONTEXT.md` — the
  live service targets (`villa-qdrant:6333`, `villa-embed:8080/v1/embeddings`,
  `--embeddings --pooling mean`, 768-dim) Phase 20 wires OWUI to.
- `.planning/PROJECT.md` — v1.3 milestone goal, integrate-not-rebuild, out-of-scope
  (custom Go RAG/embeddings engine; cloud embedding APIs).

### Code touchpoints (this phase edits / extends these)
- `internal/orchestrate/openwebui.go` — **primary edit site**: `buildOpenWebUIView()`
  ordered `Env []envPair` slice (append the D-09 block, D-02..D-05); the existing
  telemetry/offline keys and the `WEBUI_AUTH=True` / `sk-no-key-required` sentinel
  precedent.
- `internal/orchestrate/quadlet/openwebui.container.tmpl` — `{{range .Env}}Environment=`
  loop (no template change needed; new keys flow through the existing range).
- `internal/orchestrate/testdata/*openwebui*.golden` — the byte-frozen container
  golden to re-freeze once (D-05).
- `internal/orchestrate/render.go` / `internal/orchestrate/memory.go` — the
  `memory_enabled` conditional render path (D-04, byte-identical when off).
- `internal/config/villaconfig.go` — resolved memory fields the env values source
  from: `MemoryEnabled`, `EmbeddingModel`, `EmbeddingDim`, `QdrantAddr`/`QdrantPort`,
  `EmbedAddr`/`EmbedPort` (no re-typed literals).
- `cmd/villa/install_memory.go` — the Phase-19 proof seam to extend for D-10:
  `evalMemoryProof` (pure core), `memoryProofInput`, `liveMemoryProof`,
  `runProbeCurl` (`podman run --rm --network villa …`, FIXED-arg), `qdrantWritableProbe`.
- `cmd/villa/install_test.go`, `internal/orchestrate/memory_test.go` — existing test
  patterns the new smoke test + golden re-freeze mirror.
- `internal/inference/seam_test.go` — `TestSeamGrepGate` must stay green (env values
  are config-sourced, not backend/image tokens).

### External (re-verify against the PINNED OWUI digest at research time)
- Open WebUI `docs/reference/env-configuration.mdx` — confirm every D-09 key + the
  `ENABLE_QDRANT_MULTITENANCY_MODE` name/behaviour + any embedding-prefix /
  citation-display setting **against the pinned digest**, not `:main`.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `buildOpenWebUIView()` ordered `Env []envPair` (`openwebui.go`) — the exact,
  golden-frozen slice the D-09 keys append to; the telemetry/offline keys and the
  `sk-no-key-required` sentinel are direct precedent for the new RAG key + key-field.
- The Phase-19 **proof seam** (`evalMemoryProof` pure core + injected
  `embedProbe`/`qdrantProbe`; `liveMemoryProof`/`runProbeCurl` over `villa.network`)
  — the template for D-10's runtime zero-outbound smoke test (pure core, unit-testable
  off-hardware, FIXED-arg podman, no host port).
- `memory_enabled` conditional render (`render.go`/`memory.go`) — reused so the env
  block appears only when memory is on (byte-identical off, Phase-18 SC#1).
- Resolved config memory fields (`villaconfig.go`) — supply the env values without
  re-typing any host literal.

### Established Patterns
- **Config is the single source of truth; units regenerate from config** (INFRA-03/04)
  — `ENABLE_PERSISTENT_CONFIG=False` is what makes this actually true for OWUI (D-03).
- **Byte-frozen `--json`/render contracts evolve append-only + deliberate re-freeze**
  — the env block grows append-only; one intentional golden re-freeze (D-05).
- **orchestrate is the only intentionally impure module; seam-locked literals** —
  env values are config-sourced; `TestSeamGrepGate` stays green.
- **Honesty-by-construction / offload-asserting** — the runtime smoke test asserts a
  real upload reaches no external host; a silent skip is a FAIL (D-10).
- **Loopback / container-DNS only; no shell interpolation** — no new published port;
  fixed-arg podman/curl (D-11).

### Integration Points
- `config.toml` `memory_*` → `internal/memory` render-view → `orchestrate`
  `openwebui.go` env block (this phase: INFRA-03/MEM/KB wiring).
- OWUI → `villa-qdrant:6333` (`VECTOR_DB=qdrant`) + `villa-embed:8080/v1`
  (`RAG_EMBEDDING_ENGINE=openai`) over `villa.network` (Phase-19 targets).
- `ENABLE_QDRANT_MULTITENANCY_MODE` (D-01) + `QDRANT_COLLECTION_PREFIX=open-webui`
  → **Phase-21** indexer collection layout + **Phase-23** backup manifest/snapshot.
- Pinned dimension **768** (config) → **Phase-23** memory-aware swap guard.

</code_context>

<specifics>
## Specific Ideas

- The full D-09 env table is the freeze target — copy values verbatim, re-verified
  against the pinned OWUI digest.
- `ENABLE_QDRANT_MULTITENANCY_MODE=True` is the one previously-unmade decision, now
  pinned (D-01) — must be locked before any vector is written.
- The #1 runtime-privacy risk this phase finally *enforces* (Phases 18/19 recorded /
  built; Phase 20 proves): OWUI HF lazy-download + ChromaDB PostHog telemetry — closed
  by `VECTOR_DB=qdrant` + local embedder + offline lockdown + the runtime zero-outbound test.
- The runtime test is the headline deliverable — install-time green is NOT sufficient
  for SC#4 (must be a runtime firewalled document-upload smoke test).

</specifics>

<deferred>
## Deferred Ideas

- chats→Knowledge semantic recall indexer — **Phase 21**.
- `recommend` footprint reservation + `preflight` memory gating + `doctor` memory
  health — **Phase 22**.
- `status`/dashboard memory rows (schema 2→3), backup/restore of the Qdrant volume,
  memory-aware model swap — **Phase 23**.

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 20-open-webui-memory-rag-wiring-offline-lockdown*
*Context gathered: 2026-06-09*
