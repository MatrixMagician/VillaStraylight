# Phase 20: Open WebUI Memory/RAG Wiring + Offline Lockdown - Research

**Researched:** 2026-06-09
**Domain:** Open WebUI native Memory + RAG env wiring (behind the `orchestrate` seam) → Qdrant + local `villa-embed`; offline/telemetry lockdown; runtime zero-outbound proof
**Confidence:** HIGH on env contract + code-integration; MEDIUM on auto-memory mechanism specifics and the exact egress-blocking choice for the runtime proof

## Summary

This phase is an **env-only wiring + runtime-proof** phase: no new Go RAG/embeddings engine, no new published port, no schema change to the service render beyond an append-only OWUI env block and one new memory-on golden. Phases 18/19 already pinned the contract (D-09) and built the services (`villa-qdrant:6333`, `villa-embed:8080/v1`); Phase 20 sets the OWUI env keys that point OWUI at them, **enforces** offline/telemetry lockdown, and **proves** zero-outbound at runtime (not just install-time green).

The single highest-value research result: I **re-verified the D-09 keys against the OWUI source on `main`** (`backend/open_webui/config.py`), which the pinned-digest image is built from. The findings confirm most of D-09 and resolve the three open questions decisively: (1) `ENABLE_QDRANT_MULTITENANCY_MODE` **defaults to `True`** in source (`os.getenv('ENABLE_QDRANT_MULTITENANCY_MODE','true')`) and is a **plain env var (NOT PersistentConfig)** — so D-01's `True` choice matches the default and is always honored regardless of `ENABLE_PERSISTENT_CONFIG`; (2) **automatic LLM-assisted memory extraction is NOT a single env toggle** — OWUI's native auto-memory is driven either by **Native Function Calling / Agentic Mode** (model-driven memory tools, enabled per-model in the UI) or by a **community Filter Function** (Adaptive Memory / Auto Memory) — so MEM-03's "default-off, toggleable" guarantee is enforced **by not enabling Agentic Mode and not installing a memory filter**, with `ENABLE_MEMORIES` (the manual store) staying the only env we set; (3) the **nomic prefix mechanism exists** as plain env vars (`RAG_EMBEDDING_QUERY_PREFIX` / `RAG_EMBEDDING_CONTENT_PREFIX` / `RAG_EMBEDDING_PREFIX_FIELD_NAME`).

A code finding that materially shapes the plan: **`buildOpenWebUIView()` is currently UNCONDITIONAL** — the OWUI container golden is identical whether or not memory is enabled, and the telemetry-frozen test counts `Environment=` lines against `buildOpenWebUIView().Env`. D-04 requires the env block to render only when `memory_enabled=true`, so `buildOpenWebUIView()` must become **parameterized** by the memory render-view + an enabled flag, the telemetry test must derive its expected env from the **same** memory-on view, and a **new memory-on fixture + golden** is required (the existing memory-off fixture/golden must stay byte-identical — Phase-18 SC#1 continuity).

**Primary recommendation:** Parameterize `buildOpenWebUIView(mv memory.MemoryRenderInput, memoryEnabled bool)`; append the D-09 keys as one ordered group **only when `memoryEnabled`** (sourcing values from `mv` + the existing config fields, no re-typed host literals); add `RAG_EMBEDDING_QUERY_PREFIX=search_query:` / `RAG_EMBEDDING_CONTENT_PREFIX=search_document:` (plain env, honored); set `ENABLE_QDRANT_MULTITENANCY_MODE=True` and `ENABLE_PERSISTENT_CONFIG=False`; do a single deliberate re-freeze adding a `villa-openwebui.container.memory.golden`; and add a **runtime zero-outbound document-upload smoke test** that drives the real OWUI REST RAG path under host-level egress monitoring (the OWUI container itself stays on `villa.network` and cannot use `--network none`).

## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** `ENABLE_QDRANT_MULTITENANCY_MODE=True`, locked NOW before any vectors exist (one tenant-partitioned collection; forward-compatible substrate for Phase-21 indexer + Phase-23 backup). Byte-frozen the moment the first document is embedded — flipping later silently disconnects existing collections. **Re-verify key name + behaviour against the pinned digest before freezing.**
- **D-02:** Add the D-09 RAG/Qdrant/memory keys as an **ordered group appended after the existing env entries** in `buildOpenWebUIView()`. Preserve existing order. Existing block already sets `ANONYMIZED_TELEMETRY=False`, `DO_NOT_TRACK=True`, `SCARF_NO_ANALYTICS=True`, `OFFLINE_MODE=True`, `ENABLE_VERSION_UPDATE_CHECK=False`, `HF_HUB_OFFLINE=1`, `WEBUI_AUTH=True`. Net-new: VECTOR_DB/QDRANT_*/RAG_* block, `ENABLE_MEMORIES`, `RAG_EMBEDDING_MODEL_AUTO_UPDATE=False`, mandatory `ENABLE_PERSISTENT_CONFIG=False`.
- **D-03:** `ENABLE_PERSISTENT_CONFIG=False` is **MANDATORY**. The RAG/embedding/memory keys are PersistentConfig (`ConfigVar`) values read from OWUI's DB unless this flag is False; without it the env appears wired but is silently ignored after first boot. Its absence is a phase failure.
- **D-04:** The new env block renders **only when `memory_enabled=true`**; memory-off is byte-identical to the pre-memory render. Values source from the resolved config / Phase-18 render-view (`QdrantAddr`/`QdrantPort`/`EmbedAddr`/`EmbedPort`/`EmbeddingModel`/`EmbeddingDim`) — no re-typed host literals; image/marker tokens seam-locked (`TestSeamGrepGate` green).
- **D-05:** Single deliberate **golden re-freeze** of the OWUI container golden + the telemetry-frozen test re-audit. Append-only; never silently widen.
- **D-06:** `ENABLE_MEMORIES=True`; manual save/view/edit/delete + cross-chat injection are native OWUI features behind that flag — integrate, do not rebuild. Verification: a deleted memory MUST stop being injected.
- **D-07:** Automatic LLM-assisted memory extraction **defaults OFF, user-toggleable** (not silently default-on). Research must identify the exact OWUI mechanism + whether it is honored under `ENABLE_PERSISTENT_CONFIG=False`; if it is a runtime UI/function toggle, record that "default-off" is enforced by not enabling it and document where the user flips it.
- **D-08:** Upload → chunk → embed → retrieve runs entirely through `villa-embed` + Qdrant (`RAG_EMBEDDING_ENGINE=openai` → `villa-embed:8080/v1`, `VECTOR_DB=qdrant`). No cloud API, no runtime model download. Citations are native OWUI RAG behaviour — confirm whether any setting gates citation display.
- **D-09:** nomic retrieval-prefix caveat — check whether the pinned digest exposes an embedding prefix/instruction setting; if exposed set it, if not accept functional (sub-optimal) retrieval and record the limitation. Do NOT block the phase on best-possible recall.
- **D-10:** Add a **runtime firewalled document-upload zero-outbound smoke test** extending the Phase-19 proof-seam pattern (`evalMemoryProof` pure core + injected probes; `runProbeCurl`/`liveMemoryProof` over `villa.network`, fixed-arg `podman run`, no host port). Drive the real RAG path under an egress-blocked network; assert no external host reached. A silent skip/unevaluable result is a FAIL.
- **D-11:** Loopback-only privacy audit stays green — no new published host port; OWUI keeps its one PublishPort (chat UI); Qdrant/embed stay container-DNS only.

### Claude's Discretion
- Exact Go symbol/spelling, env-pair ordering within the appended group, and whether the smoke test wires into `install` readiness vs a dedicated verify subcommand vs an on-hardware verification-wave step (within D-02/D-10).
- The exact egress-blocking mechanism for the runtime test (`--network none` for the pure-offline embedding leg vs a firewalled/`villa`-only network with an outbound sentinel) — researcher/planner's call, as long as it proves a real upload reaches no external host.
- Whether `QDRANT_API_KEY` stays empty or a generated key is added (no schema change either way) — defer unless research shows OWUI requires a non-empty value.

### Deferred Ideas (OUT OF SCOPE)
- chats→Knowledge semantic recall indexer — **Phase 21**.
- `recommend` footprint reservation + `preflight` memory gating + `doctor` memory health — **Phase 22**.
- `status`/dashboard memory rows (schema 2→3), backup/restore of the Qdrant volume, memory-aware model swap — **Phase 23**.
- Reranker / hybrid search (`ENABLE_RAG_HYBRID_SEARCH` / `RAG_RERANKING_MODEL`) — v2 (RAG-Q-01).

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| INFRA-03 | OWUI wired (env-only, behind orchestrate seam) to Qdrant + local embeddings, `ENABLE_PERSISTENT_CONFIG=false` | D-09 env contract re-verified against source; `buildOpenWebUIView()` parameterization path; `ENABLE_PERSISTENT_CONFIG=False` semantics confirmed (DB-over-env vs env-only) |
| MEM-01 | Remembers user facts across chats, injects them | `ENABLE_MEMORIES=True` (PersistentConfig, honored under `ENABLE_PERSISTENT_CONFIG=False`); native cross-chat injection |
| MEM-02 | User can explicitly save a message/fact | Native UI (Settings → Personalization → Memory; "+" add) and per-message save; no extra env |
| MEM-03 | Auto-extraction, toggleable on/off | **No single env toggle** — auto extraction is Native Function Calling (Agentic Mode, per-model UI) OR a community Filter Function. Default-off = not enabling either |
| MEM-04 | View/edit/delete stored memories | Native Settings → Personalization → Memory; deleted memory stops being injected |
| KB-01 | Upload documents into a local knowledge collection | `POST /api/v1/knowledge/create` + `POST /api/v1/files/` + `POST /api/v1/knowledge/{id}/file/add` |
| KB-02 | Answers using retrieved content with citations | Citations are automatic with RAG (chunked retrieval). Native FC/Agentic Mode changes injection — keep it off for deterministic citations |
| KB-03 | Chunk/embed/retrieve entirely local | `VECTOR_DB=qdrant` + `RAG_EMBEDDING_ENGINE=openai`→`villa-embed`; offline lockdown removes the HF runtime vector |
| PRIV-05 | No telemetry; verified by runtime firewalled zero-outbound smoke test | `VECTOR_DB=qdrant` (no PostHog), `ANONYMIZED_TELEMETRY=False`; runtime proof design below (D-10) |

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| OWUI memory/RAG env wiring | orchestrate (render) | config (source of truth) | Env block is a managed-service render concern; values flow from config via `memory.RenderView` |
| Pointing OWUI at Qdrant/embed | OWUI container (runtime) | orchestrate (env) | OWUI's own RAG/vector subsystem performs chunk/embed/retrieve; villa only configures it |
| Offline/telemetry lockdown | orchestrate (env) | OWUI runtime | Plain env vars consumed by OWUI/HF libs at process start |
| Document upload + retrieval + citations | OWUI runtime | villa-embed + Qdrant services | Native OWUI RAG pipeline; villa builds nothing here |
| Runtime zero-outbound proof | cmd/villa (impure seam) | orchestrate accessors (image), host firewall | Drives the real OWUI REST path; asserts no external host reached |
| Memory storage/injection | OWUI runtime + Qdrant | — | Native OWUI Memory feature persists to its DB + (for KB) Qdrant |

## Standard Stack

No new packages. This phase wires existing pinned OSS services via env and extends existing Go test/proof seams.

### Core (existing, pinned)
| Component | Version/Pin | Purpose | Why Standard |
|-----------|-------------|---------|--------------|
| Open WebUI | `ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a…a9184e` | Chat UI + native Memory + native RAG | Already integrated (`internal/orchestrate/openwebui.go`); the integrate-not-rebuild target |
| Qdrant | `docker.io/qdrant/qdrant:v1.18.2-unprivileged@sha256:b79aaa49…5962e5` | Vector store (`VECTOR_DB=qdrant`) | Phase-19 service; telemetry-free vs ChromaDB PostHog |
| villa-embed (llama-server) | kyuz0 vulkan-radv `@sha256:9a74e555…ac7aad` | `/v1/embeddings`, nomic-embed-text-v1.5, 768-dim | Phase-19 service; local, no HF runtime download |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `RAG_EMBEDDING_ENGINE=openai`→villa-embed | OWUI built-in SentenceTransformers | Built-in lazily downloads from HF at first upload — breaks `OFFLINE_MODE`/`HF_HUB_OFFLINE` (the #1 privacy risk). REJECTED (D-07/D-08). |
| Multitenancy `True` (1 collection) | `False` (collection-per-KB) | False wastes RAM on the shared gfx1151 GTT envelope and complicates Phase-23 snapshot. `True` matches OWUI default. (D-01) |
| Native FC/Agentic auto-memory | Adaptive Memory filter function | Both are opt-in; neither is set by villa (MEM-03 default-off). Filter functions are community-maintained, not a villa dependency. |

**Installation:** N/A — no `go get`/package install. Env-only + Go test code.

## Package Legitimacy Audit

> Not applicable — this phase installs **no** new external packages (Go modules or container images). All three container images are already pinned and legitimacy-audited in Phases 4/19. No `go get`. The only "external" artifacts referenced are env-var names consumed by the already-pinned OWUI image.

## OWUI Env Contract — Re-verified Against Source

> Verified 2026-06-09 against `backend/open_webui/config.py` on `open-webui/open-webui` `main` (the branch the `:main` digest is built from) plus the env-configuration reference. The pinned digest `7f1b0a1a…` could not be date-resolved on registry mirrors `[ASSUMED: digest is a recent main build, behavior tracks main]`; the source-level checks below are the authoritative signal. **Plan should include an on-hardware confirmation step** that runs `podman run --rm <pinned-digest> python -c "import open_webui.config"` style introspection OR simply asserts behavior via the runtime smoke test (the proof that actually matters).

| Env key | Phase-20 value | Source default | PersistentConfig? | Verified |
|---------|---------------|----------------|-------------------|----------|
| `VECTOR_DB` | `qdrant` | `chroma` | No (plain env) | `[VERIFIED: config.py]` |
| `QDRANT_URI` | `http://villa-qdrant:6333` | `None` | No | `[VERIFIED: config.py]` |
| `QDRANT_API_KEY` | empty/unset | `None` | No | `[VERIFIED: config.py]` — empty is accepted (default None); no non-empty requirement found. **Keep empty** (D-discretion) |
| `ENABLE_QDRANT_MULTITENANCY_MODE` | `True` | **`True`** (`os.getenv(...,'true')`) | No (plain env) | `[VERIFIED: config.py]` — **resolves D-01**: True is the source default, always honored |
| `QDRANT_COLLECTION_PREFIX` | `open-webui` | `open-webui` | No | `[VERIFIED: config.py]` |
| `RAG_EMBEDDING_ENGINE` | `openai` | `''` (=SentenceTransformers) | **YES (`rag.embedding_engine`)** | `[VERIFIED: config.py]` — requires `ENABLE_PERSISTENT_CONFIG=False` to be honored after first boot |
| `RAG_OPENAI_API_BASE_URL` | `http://villa-embed:8080/v1` | falls back to `OPENAI_API_BASE_URL` | **YES** | `[VERIFIED: config.py]` |
| `RAG_OPENAI_API_KEY` | `sk-no-key-required` | falls back to `OPENAI_API_KEY` | **YES** | `[VERIFIED: config.py]` — non-empty sentinel (mirrors chat path); llama-server does no auth |
| `RAG_EMBEDDING_MODEL` | `nomic-embed-text-v1.5` | `sentence-transformers/all-MiniLM-L6-v2` | **YES** | `[VERIFIED: config.py]` — must match what villa-embed serves (it does, D-08) |
| `RAG_EMBEDDING_MODEL_AUTO_UPDATE` | `False` | `True` | No | `[CITED: env-configuration]` |
| `ENABLE_MEMORIES` | `True` | `True` | **YES (`memories.enable`)** | `[VERIFIED: config.py + env-configuration]` |
| `ENABLE_PERSISTENT_CONFIG` | **`False`** | `True` | n/a (controls the system) | `[VERIFIED: env-configuration]` — **LOAD-BEARING**, semantics confirmed below |
| `OFFLINE_MODE` | `True` (already set) | `False` | No | `[CITED: env-configuration]` |
| `HF_HUB_OFFLINE` | `1` (already set) | `0` | No | `[CITED: HF + existing render]` |
| `ANONYMIZED_TELEMETRY` | `False` (already set) | telemetry on | No | `[CITED: existing render]` |
| `RAG_EMBEDDING_QUERY_PREFIX` | `search_query:` (NEW — D-09) | `None` | No (plain env) | `[VERIFIED: config.py]` — nomic prefix mechanism, see D-09 section |
| `RAG_EMBEDDING_CONTENT_PREFIX` | `search_document:` (NEW — D-09) | `None` | No (plain env) | `[VERIFIED: config.py]` |
| `RAG_EMBEDDING_PREFIX_FIELD_NAME` | (leave unset) | `None` | No | `[VERIFIED: config.py]` — only needed if the embed server requires the prefix in a named field; llama-server does not |

### `ENABLE_PERSISTENT_CONFIG=False` semantics (D-03, load-bearing) — CONFIRMED

`[VERIFIED: env-configuration + DeepWiki PersistentConfig]` Open WebUI's PersistentConfig system stores PersistentConfig (`ConfigVar`) values in its DB. On first launch the env value seeds the DB; **on subsequent boots the DB value wins and the env is ignored** — unless `ENABLE_PERSISTENT_CONFIG=False`, which "disables persistent config behavior and forces Open WebUI to always use environment variables (ignoring the database)." This is exactly why D-03 is mandatory: `RAG_EMBEDDING_ENGINE`, `RAG_OPENAI_API_BASE_URL`, `RAG_OPENAI_API_KEY`, `RAG_EMBEDDING_MODEL`, and `ENABLE_MEMORIES` are all `ConfigVar`s. Without `ENABLE_PERSISTENT_CONFIG=False`, a config.toml-driven change after the OWUI DB is populated has **no effect** — INFRA-03's "config is the single source of truth" fails. SC#4 calls this out explicitly ("so a config.toml-driven change actually takes effect after the OWUI DB is populated").

**Plain-env keys (VECTOR_DB, QDRANT_*, RAG_EMBEDDING_QUERY/CONTENT_PREFIX, OFFLINE_MODE, HF_HUB_OFFLINE, ANONYMIZED_TELEMETRY) are honored regardless** of `ENABLE_PERSISTENT_CONFIG` — they are read directly from `os.environ`, never DB-backed.

### `ENABLE_QDRANT_MULTITENANCY_MODE` (D-01) — RESOLVED

`[VERIFIED: config.py]` Source line: `ENABLE_QDRANT_MULTITENANCY_MODE = os.getenv("ENABLE_QDRANT_MULTITENANCY_MODE", "true").lower() == "true"`. **Default is `True`** on current `main`. It is a **plain env var, not PersistentConfig**, so the value villa sets is always honored.

- `True` → **one shared collection** (named via `QDRANT_COLLECTION_PREFIX`), with OWUI's logical collections (each knowledge base, each user's memory) separated by a **payload index** (tenant partitioning). This is Qdrant's own recommended layout `[CITED: discussion #13930, scaling docs]`.
- `False` → **a new physical Qdrant collection per knowledge base and per memory store** (the older behavior).
- **Toggling after vectors exist disconnects collections** `[ASSUMED — inferred from the two distinct layouts; no migration path documented]`: the layouts are physically different (one partitioned collection vs N collections), so a flip orphans existing vectors with no auto-reindex. This is exactly why D-01 freezes it before the first embed. **Lock `True` now.**
- Phase-21 implication: the indexer writes into the single prefixed collection with a tenant/payload key. Phase-23 implication: backup snapshots one collection, not N — record the multitenancy flag + dim in the manifest so a restore onto a False-mode instance is refused.

> **Discrepancy noted & resolved:** an early WebSearch summary and the feature-proposal discussion #13930 described `False` (collection-per-KB) as "the default." That described the *original* behavior at proposal time; the **current source default is `True`**. The D-09 table's `True` default is correct for the pinned-main image. Confidence HIGH (read from source).

### nomic retrieval-prefix (D-09 / KB-02/03) — MECHANISM FOUND

`[VERIFIED: config.py]` OWUI exposes `RAG_EMBEDDING_QUERY_PREFIX`, `RAG_EMBEDDING_CONTENT_PREFIX`, and `RAG_EMBEDDING_PREFIX_FIELD_NAME` (all plain env, default `None`). These are exactly the nomic task-instruction prefixes. **Recommendation: set them** (`search_query:` for queries, `search_document:` for stored chunks) — they are cheap, plain-env (honored under `ENABLE_PERSISTENT_CONFIG=False`), and improve recall. Leave `RAG_EMBEDDING_PREFIX_FIELD_NAME` unset (only needed when the embedding server expects the prefix in a separate JSON field; llama-server's OpenAI `/v1/embeddings` takes the prefix inline in `input`). If on-hardware testing shows the prefix degrades results (unlikely), D-09's fallback ("accept functional retrieval, record limitation") applies — but the mechanism existing means we are NOT in the degraded fallback case.

### Automatic memory extraction (D-07 / MEM-03) — MECHANISM CLARIFIED

`[CITED: OWUI memory docs + community function library]` There is **no single `ENABLE_AUTOMATIC_MEMORY` env var**. OWUI's native automatic memory has two paths, both **opt-in**:

1. **Native Function Calling / Agentic Mode** (built-in, current native path): when a model has Native Function Calling enabled (a per-model setting in *Workspace → Models → Edit*), OWUI exposes memory tools (`add_memory`, `search_memories`, `replace_memory_content`, `delete_memory`, `list_memories`) the model calls proactively. This is the modern native auto-memory. It is **off unless the user enables Agentic/Native Function Calling for the model**.
2. **Community Filter Functions** (Adaptive Memory / Auto Memory): installed via the UI function library; LLM-extract + store on each turn. Off unless the user installs/enables one.

**MEM-03 enforcement:** `[VERIFIED via mechanism]` "Default-off" is enforced by villa **not** enabling Native Function Calling on the default model and **not** installing any memory filter function. `ENABLE_MEMORIES=True` only enables the **manual** memory store + injection (MEM-01/02/04). The "toggle on" is a documented user action (enable Native Function Calling for the model, or install Adaptive Memory). **There is no env to force auto-extraction off under `ENABLE_PERSISTENT_CONFIG=False`** — none is needed, because the default state is off. **Plan must document where the user flips it on** (per-phase UAT note + a doctor/help line is a Phase-22/23 concern, not here). **Caveat:** Native FC auto-memory requires the local model to support reliable function calling; with a small local model this can be unreliable — which is precisely why D-07 keeps it default-off (the local-model quality caveat).

> **Important interaction (KB-02):** when Native Function Calling is ON, "attached knowledge isn't automatically injected; the model must call tools to retrieve it." For deterministic, citation-bearing RAG in the SC#3 smoke test, **keep Native Function Calling OFF** so knowledge is injected via the standard RAG pipeline (chunk → retrieve → inject with citations). The smoke test should drive the standard RAG path, not the agentic path.

### Citation display (KB-02) — CONFIRMED automatic

`[CITED: knowledge docs + API discussion]` Citations are produced automatically by the standard RAG pipeline ("cites where it found the answer"). No env setting gates citation display in the standard path. The `/api/chat/completions` response with a knowledge collection attached returns citation/source metadata. The only thing that *changes* citation behavior is **Native Function Calling** (which bypasses auto-injection) and **Full Context Mode** (injects whole docs, bypassing RAG chunking). Keep both OFF for the verified RAG + citations path.

## Architecture Patterns

### System Architecture Diagram

```
                      config.toml (single source of truth; memory_enabled gate)
                                   │
                                   ▼
                       memory.RenderView(cfg)  ──►  MemoryRenderInput
                       {EmbeddingModel, EmbeddingDim,                  (resolved values,
                        QdrantAddr/Port, EmbedAddr/Port}                no host literals)
                                   │
                                   ▼
        ┌────────────────── internal/orchestrate (PURE render) ──────────────────┐
        │  buildOpenWebUIView(mv, memoryEnabled)                                  │
        │    Env = [ existing 11 entries ]                                        │
        │        + IF memoryEnabled: [ VECTOR_DB, QDRANT_URI(from mv),            │
        │            ENABLE_QDRANT_MULTITENANCY_MODE, QDRANT_COLLECTION_PREFIX,   │
        │            RAG_EMBEDDING_ENGINE, RAG_OPENAI_API_BASE_URL(from mv),      │
        │            RAG_OPENAI_API_KEY, RAG_EMBEDDING_MODEL(from mv),            │
        │            RAG_EMBEDDING_*_PREFIX, RAG_EMBEDDING_MODEL_AUTO_UPDATE,     │
        │            ENABLE_MEMORIES, ENABLE_PERSISTENT_CONFIG=False ]            │
        │            ──► openwebui.container.tmpl {{range .Env}}Environment=      │
        └─────────────────────────────────────────────────────────────────────────┘
                                   │ rendered unit (golden-frozen)
                                   ▼
                      villa-openwebui.container  (on villa.network, loopback PublishPort 3000)
                                   │
        ┌──────────────────────────┼──────────────────────────────┐
        ▼ (container-DNS only)      ▼ (container-DNS only)          ▼ (loopback REST API)
   villa-qdrant:6333          villa-embed:8080/v1            127.0.0.1:3000/api/...
   (VECTOR_DB store)          (/v1/embeddings)               (upload / knowledge / chat)
        ▲                          ▲                                │
        └──────── RAG: chunk → embed(villa-embed) → store(Qdrant) ──┘
                                                                    │
   RUNTIME ZERO-OUTBOUND PROOF (D-10):                              │
   drive real upload→embed→retrieve→cite via loopback API ─────────┘
   while host egress is blocked/monitored; assert NO external host reached
   (silent skip / unevaluable = FAIL)
```

### Recommended Project Structure (touchpoints only — no new packages)
```
internal/orchestrate/
├── openwebui.go                       # buildOpenWebUIView → parameterize (D-02/D-04)
├── quadlet/openwebui.container.tmpl    # no change ({{range .Env}} already loops)
├── render.go                          # pass memory.RenderView + MemoryEnabled to buildOpenWebUIView
└── testdata/
    ├── villa-openwebui.container.golden          # UNCHANGED (memory-off fixture)
    └── villa-openwebui.container.memory.golden   # NEW (memory-on golden, D-05)
internal/orchestrate/render_test.go     # new memory-on fixture + golden test; telemetry test made memory-aware
cmd/villa/
├── install_memory.go                  # extend proof seam for D-10 (ragSmokeProof)
└── (verify subcommand OR install readiness — planner's call per D-10 discretion)
```

### Pattern 1: Parameterize the OWUI view (D-04 conditional env)
**What:** `buildOpenWebUIView()` currently takes no args and is unconditional. Make it `buildOpenWebUIView(mv memory.MemoryRenderInput, memoryEnabled bool)`.
**When to use:** This phase, to satisfy D-04 byte-identical-when-off.
**Example:**
```go
// Source: pattern derived from internal/orchestrate/render.go memory branch (verified in repo)
func buildOpenWebUIView(mv memory.MemoryRenderInput, memoryEnabled bool) openWebUIView {
    env := []envPair{
        {Key: "OPENAI_API_BASE_URL", Value: "http://" + containerName + ":8080/v1"},
        // ... existing 11 entries UNCHANGED, in order ...
        {Key: "WEBUI_AUTH", Value: "True"},
    }
    if memoryEnabled {
        env = append(env,
            envPair{Key: "VECTOR_DB", Value: "qdrant"},
            envPair{Key: "QDRANT_URI", Value: fmt.Sprintf("http://%s:%d", mv.QdrantAddr, mv.QdrantPort)},
            envPair{Key: "ENABLE_QDRANT_MULTITENANCY_MODE", Value: "True"},
            envPair{Key: "QDRANT_COLLECTION_PREFIX", Value: "open-webui"},
            envPair{Key: "RAG_EMBEDDING_ENGINE", Value: "openai"},
            envPair{Key: "RAG_OPENAI_API_BASE_URL", Value: fmt.Sprintf("http://%s:%d/v1", mv.EmbedAddr, mv.EmbedPort)},
            envPair{Key: "RAG_OPENAI_API_KEY", Value: "sk-no-key-required"},
            envPair{Key: "RAG_EMBEDDING_MODEL", Value: mv.EmbeddingModel},
            envPair{Key: "RAG_EMBEDDING_QUERY_PREFIX", Value: "search_query:"},
            envPair{Key: "RAG_EMBEDDING_CONTENT_PREFIX", Value: "search_document:"},
            envPair{Key: "RAG_EMBEDDING_MODEL_AUTO_UPDATE", Value: "False"},
            envPair{Key: "ENABLE_MEMORIES", Value: "True"},
            envPair{Key: "ENABLE_PERSISTENT_CONFIG", Value: "False"},
        )
        // QDRANT_API_KEY intentionally omitted (empty default accepted; private villa.network)
    }
    return openWebUIView{ /* ... */ Env: env}
}
```
Note: `QDRANT_URI`/`RAG_OPENAI_API_BASE_URL` are composed in orchestrate from the resolved addr/port pieces (consistent with `internal/memory` doc: "orchestrate composes the URLs"). `fmt` import is needed (`render.go`'s memory branch already uses composed values; `openwebui.go` will import `fmt`).

### Pattern 2: Make the telemetry-frozen test memory-aware (D-05)
**What:** `TestRenderOpenWebUITelemetryFrozen` derives expected env from `buildOpenWebUIView().Env` and counts `Environment=` lines. After parameterization, call it with the memory-on view for the memory-on unit, and keep the memory-off assertion against the memory-off unit.
**Example:**
```go
// Source: internal/orchestrate/render_test.go:300 (verified) — make the env source memory-aware
env := buildOpenWebUIView(memory.RenderView(memoryOnCfg), true).Env   // for memory-on unit
// existing assertion loop + exact-count check unchanged; now also assert the memory-off unit
// renders exactly the 11 baseline lines (byte-identical to today's golden).
```

### Pattern 3: Runtime zero-outbound RAG smoke proof (D-10) — extend the Phase-19 seam
**What:** A pure eval core + injected probes mirroring `evalMemoryProof`/`liveMemoryProof`. Drives the **real OWUI REST RAG path** and asserts no external host is reached.
**The egress topology problem & recommendation:** The OWUI container is long-lived on `villa.network` and MUST stay reachable to villa-qdrant/villa-embed — so it **cannot** use `--network none` (that would break the very path under test). The honest design:
- **Block/monitor host egress at the host layer**, not the container network. Recommended: assert no external connection by **monitoring outbound** during the drive. Two viable mechanisms (planner's discretion per D-10):
  - (a) **Sentinel/firewall approach:** with the host's outbound to the public internet blocked (or a deny-all egress firewall rule on the host for the duration), drive the RAG path; success proves the path completes using only `villa.network` container-DNS. Pair with a **negative control**: a probe that attempts a known external host (e.g. `podman run --rm --network villa <img> curl -sf https://huggingface.co` with a short timeout) and MUST fail — proving egress is actually blocked, not merely unused (honesty-by-construction; a silent skip if the negative control can't run = FAIL).
  - (b) **`--network none` only for the embedding leg sanity check:** already covered by the Phase-19 install proof (offline `/v1/embeddings`). For the *document-upload* path, (a) is required because OWUI needs network to reach Qdrant/embed.
- **Drive the upload non-interactively via the OWUI REST API** (loopback `127.0.0.1:3000`, the existing PublishPort — no new port, D-11):
  1. obtain an API token (Settings → Account API key; or sign-in `POST /api/v1/auths/signin` to mint a token for the seeded admin — `WEBUI_AUTH=True`),
  2. `POST /api/v1/knowledge/create` → knowledge id,
  3. `POST /api/v1/files/` (multipart) → file id (async; poll `GET /api/v1/files/{id}/process/status`),
  4. `POST /api/v1/knowledge/{id}/file/add` `{"file_id": ...}`,
  5. `POST /api/chat/completions` with `"files":[{"type":"collection","id":<kid>}]` and a question whose answer is only in the uploaded doc,
  6. assert the response **contains the planted fact AND citation/source metadata** (proves chunk→embed→retrieve→cite ran via the local path).
- **Honesty:** a silent skip / unevaluable result (token mint failed, async never completed, negative control couldn't run) is a **FAIL**, never a false-green — mirrors `evalMemoryProof`.
**Example (probe shape, mirrors install_memory.go):**
```go
// Source: cmd/villa/install_memory.go evalMemoryProof/runProbeCurl (verified in repo)
type ragSmokeInput struct { owuiAddr string; owuiPort int /* loopback */; question, wantFact string }
func evalRagSmoke(uploadCite func() (answer string, citedSource bool, err error),
                  egressBlocked func() (blocked bool, err error)) memoryProof {
    // 1) negative control: external host MUST be unreachable
    blocked, err := egressBlocked()
    if err != nil { return fail("could not run the egress negative-control probe (...) — refusing to declare zero-outbound") }
    if !blocked { return fail("egress is NOT blocked: an external host was reachable during the test (...)") }
    // 2) real RAG path
    ans, cited, err := uploadCite()
    if err != nil { return fail("the document-upload RAG path did not complete (...)") }
    if !strings.Contains(ans, wantFact) || !cited { return fail("answer did not use the uploaded content / no citation (...)") }
    return pass("document upload retrieved + cited with zero outbound")
}
```

### Anti-Patterns to Avoid
- **Setting auto-memory ON by default** (enabling Native FC or installing a filter for the user) — violates D-07/MEM-03.
- **Driving the smoke test through Native Function Calling** — bypasses auto-injection + citations; use the standard RAG path (FC off).
- **Re-typing `villa-qdrant`/`villa-embed`/ports as literals in `openwebui.go`** — source from `mv` (memory.RenderView). Keeps WR-01 + golden-from-config.
- **`--network none` on the OWUI container for the upload path** — breaks reachability to Qdrant/embed; the path under test needs villa.network. Block egress at the host layer instead.
- **Forgetting `ENABLE_PERSISTENT_CONFIG=False`** — the ConfigVar keys are silently ignored after first boot; phase failure (D-03).
- **Asserting zero-outbound by absence only** — without a negative control proving egress is actually blocked, a green is meaningless (false-green). Pair with a must-fail external probe.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Document chunking/embedding/retrieval | Custom Go RAG engine | OWUI native RAG (env-wired) | Out of scope (PROJECT.md); OWUI does chunking/citation/retrieval |
| Memory extraction/storage | Custom memory store | OWUI native Memory (`ENABLE_MEMORIES`) + Qdrant | Integrate-not-rebuild; deletion/injection are native |
| Embeddings server | Custom embedding service | `villa-embed` (Phase-19) | Already built + proven |
| Composing service URLs | Re-typed `http://villa-...` literals | `memory.RenderView` pieces composed in orchestrate | Config is single source of truth; no drift from proof target |
| OWUI auth for the smoke test | Custom auth bypass | OWUI REST `signin`/API-key (`WEBUI_AUTH=True`) | Native; the admin account already persists in the volume |

**Key insight:** Everything substantive (chunk/embed/retrieve/cite/remember) is OWUI's job; villa's job is exactly two things — set the right env (behind the seam, from config) and prove the runtime is private.

## Common Pitfalls

### Pitfall 1: `buildOpenWebUIView()` is unconditional today
**What goes wrong:** Appending the env block unconditionally makes the **memory-OFF** OWUI golden change, breaking Phase-18 SC#1 byte-identical continuity.
**Why it happens:** The current signature takes no args; the memory-off fixture would suddenly carry RAG env.
**How to avoid:** Parameterize with `(mv, memoryEnabled)`; gate the appended group on `memoryEnabled`. Keep the existing `villa-openwebui.container.golden` (memory-off fixture) byte-identical; add a NEW `*.memory.golden` for the memory-on fixture.
**Warning signs:** The existing memory-off golden test goes red.

### Pitfall 2: Telemetry-frozen test counts env lines from a no-arg builder
**What goes wrong:** `TestRenderOpenWebUITelemetryFrozen` does `buildOpenWebUIView().Env` and asserts `strings.Count(text,"Environment=") == len(env)`. After parameterization this won't compile / will mis-count.
**Why it happens:** The test is bound to the old signature.
**How to avoid:** Update it to derive expected env from the memory-on view for the memory-on unit, and add a memory-off assertion (exactly 11 lines). This IS the D-05 "re-audit" — re-confirm the telemetry-kill posture when touching it.
**Warning signs:** Compile error or count mismatch in `render_test.go`.

### Pitfall 3: `ENABLE_PERSISTENT_CONFIG=False` missing → silent ignore after first boot
**What goes wrong:** RAG/memory ConfigVars seed the DB on first boot, then the env is ignored; a later config.toml change does nothing.
**How to avoid:** Always emit `ENABLE_PERSISTENT_CONFIG=False` in the memory-on block. The runtime smoke test (after the DB is populated) is what actually catches its absence.
**Warning signs:** Changing `RAG_EMBEDDING_MODEL` in config has no runtime effect on a re-installed host.

### Pitfall 4: Multitenancy flag flipped after vectors exist
**What goes wrong:** `True`→`False` (or vice-versa) orphans existing collections — different physical layout, no auto-reindex.
**How to avoid:** Lock `True` now (D-01), before any embed. Record the flag in the Phase-23 manifest so restore refuses a mode mismatch.
**Warning signs:** Knowledge/memory "disappears" after an env change.

### Pitfall 5: Native Function Calling silently changes RAG behavior
**What goes wrong:** With Native FC enabled on a model, attached knowledge is NOT auto-injected; the smoke test sees no citations and fails confusingly.
**How to avoid:** Keep Native FC OFF for the default model and the smoke test; drive the standard RAG path.
**Warning signs:** Empty/uncited answers despite a populated knowledge collection.

### Pitfall 6: Async file processing race in the smoke test
**What goes wrong:** Querying right after upload returns empty (processing not finished).
**How to avoid:** Poll `GET /api/v1/files/{id}/process/status` until done before the chat query. Treat a timeout as FAIL, not skip.
**Warning signs:** Intermittent empty retrieval in CI/on-hardware.

### Pitfall 7: Zero-outbound asserted by absence (false-green)
**What goes wrong:** The path completes and no outbound is seen — but maybe egress was never actually blocked, so the assertion is vacuous.
**How to avoid:** Negative control — a deliberate external-host probe that MUST fail. If it can't run, FAIL.
**Warning signs:** The test passes even on a host with open internet.

## Runtime State Inventory

> This phase adds env to a long-lived container with a **populated DB**. The ConfigVar seeding-on-first-boot behavior makes existing OWUI DB state load-bearing.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | OWUI DB in `villa-openwebui.volume` (`/app/backend/data`) holds PersistentConfig `ConfigVar` rows once OWUI has booted. On a host where OWUI already ran **before** these keys were set, the DB may hold default RAG config (chroma/SentenceTransformers). | With `ENABLE_PERSISTENT_CONFIG=False`, env wins regardless of stale DB rows — so no data migration is needed for config. **But** any vectors written under a prior `VECTOR_DB=chroma` or a different multitenancy mode are orphaned (live in the old store/layout) — acceptable for v1.3 (memory is opt-in/new); document that enabling memory on a previously-booted OWUI starts a fresh Qdrant store. |
| Live service config | OWUI's runtime RAG settings are normally editable in Settings → Admin → Documents; with `ENABLE_PERSISTENT_CONFIG=False` the env is authoritative and the UI fields reflect env. No git-external config to export. | None — env is the source of truth. |
| OS-registered state | `villa-openwebui.service` (Quadlet) already registered; env change re-renders the unit → `systemctl --user daemon-reload` + restart picks it up (standard reconcile). | Reconcile + restart OWUI after re-render (existing install flow). |
| Secrets/env vars | `RAG_OPENAI_API_KEY=sk-no-key-required` is a non-secret sentinel (not a credential). `QDRANT_API_KEY` left empty (private net). No new real secret. | None. |
| Build artifacts | Go golden `villa-openwebui.container.golden` becomes stale once the memory-on path is added; a NEW `*.memory.golden` is created. | Re-freeze once (`go test ./internal/orchestrate -run OpenWebUI -update`), intentionally (D-05). |

**Nothing found** that requires a data migration of existing memories (none exist yet — memory is brand-new in v1.3).

## Code Examples

### Composing the Qdrant/embed URLs from the render-view (no re-typed literals)
```go
// Source: internal/memory/memory.go doc (verified) — "orchestrate composes the URLs from these pieces"
{Key: "QDRANT_URI",            Value: fmt.Sprintf("http://%s:%d", mv.QdrantAddr, mv.QdrantPort)},  // villa-qdrant:6333
{Key: "RAG_OPENAI_API_BASE_URL", Value: fmt.Sprintf("http://%s:%d/v1", mv.EmbedAddr, mv.EmbedPort)}, // villa-embed:8080/v1
{Key: "RAG_EMBEDDING_MODEL",   Value: mv.EmbeddingModel},  // nomic-embed-text-v1.5
```

### Driving the OWUI RAG path over loopback (smoke test; fixed-arg, no shell)
```bash
# Source: OWUI API endpoints docs (CITED). All over 127.0.0.1:3000 (existing PublishPort).
# 1) token (admin seeded via WEBUI_AUTH=True):
#    POST /api/v1/auths/signin {"email":..,"password":..} -> {token}
# 2) POST /api/v1/knowledge/create {"name":"villa-probe","description":"..."} -> {id}
# 3) POST /api/v1/files/ (multipart file=@doc.txt) -> {id};  poll GET /api/v1/files/{id}/process/status
# 4) POST /api/v1/knowledge/{kid}/file/add {"file_id":"{fid}"}
# 5) POST /api/chat/completions {"model":..,"messages":[{"role":"user","content":Q}],
#       "files":[{"type":"collection","id":"{kid}"}]}
# 6) assert answer contains the planted fact AND a citation/source block.
```

### Negative control (egress must actually be blocked)
```go
// Source: cmd/villa/install_memory.go runProbeCurl pattern (verified) — must FAIL
out, err := runProbeCurl(ctx, helperImage, "-sf", "--max-time", "5", "https://huggingface.co/")
blocked := err != nil // a reachable external host => egress NOT blocked => FAIL the proof
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Qdrant collection-per-knowledge-base | Single tenant-partitioned collection (`ENABLE_QDRANT_MULTITENANCY_MODE=True`, now default) | Multitenancy added (disc. #13930) then defaulted True on main | D-01 `True` matches default; simpler backup/snapshot (Phase-23) |
| OWUI auto-memory via filter functions only | Native Function Calling memory tools (`add_memory`, etc.) built-in | Recent main | Native auto-memory exists but is per-model opt-in; keep OFF (MEM-03/local-model caveat) |
| OWUI built-in SentenceTransformers embedder (HF download) | External OpenAI-compatible embedder (`RAG_EMBEDDING_ENGINE=openai`) | n/a (always available) | Removes the HF runtime-download privacy vector (D-07/D-08) |

**Deprecated/outdated:**
- The discussion-#13930 claim that multitenancy `False` is "the default" is outdated — source default is now `True`.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Pinned digest `7f1b0a1a…` tracks `main` behavior (env keys/defaults read from `main` config.py apply to the pin) | Env Contract | LOW–MED: if the pin predates a key, that key would be ignored. Mitigation: the runtime smoke test verifies actual behavior; add an on-hardware key-presence check. |
| A2 | Toggling `ENABLE_QDRANT_MULTITENANCY_MODE` after vectors exist orphans collections | D-01 | LOW: layouts are physically distinct; even if a migration existed, freezing `True` now is harmless. |
| A3 | `RAG_EMBEDDING_PREFIX_FIELD_NAME` is unnecessary for llama-server `/v1/embeddings` (inline prefix in `input` works) | D-09 prefix | LOW: if prefixes need a named field, recall is sub-optimal (D-09 fallback already accepts this). |
| A4 | `QDRANT_API_KEY` empty is accepted by OWUI's Qdrant client on a private net | Env Contract | LOW: source default is `None`; if a non-empty value is required, add a generated key (no schema change, per discretion). Verify in the smoke test. |
| A5 | The OWUI admin token can be minted non-interactively via `POST /api/v1/auths/signin` for the seeded admin | Smoke test | MED: if signin requires interactive first-run setup, the test must seed the admin first (or use a pre-provisioned API key). Verify on-hardware in the planning/verify wave. |
| A6 | Citations are returned in the `/api/chat/completions` response metadata for collection-attached RAG | KB-02 | MED: citation surface may be UI-rendered from `sources`/`citations` fields; the smoke test should assert on whichever field the pinned digest returns (confirm on-hardware). |

## Open Questions

1. **Exact OWUI admin-token mint path for a non-interactive smoke test.**
   - What we know: `WEBUI_AUTH=True` seeds a local admin on first visit; API uses `Authorization: Bearer`.
   - What's unclear: whether `POST /api/v1/auths/signin` works headless before any UI visit, or whether first-run signup is required.
   - Recommendation: in the planning wave, confirm on-hardware; if needed, seed the admin via `POST /api/v1/auths/signup` (first user becomes admin) inside the test setup. FAIL (not skip) if token cannot be obtained.
2. **Where do citation fields appear in the chat-completions response for the pinned digest?**
   - What we know: citations are automatic with standard RAG.
   - What's unclear: field name (`sources`, `citations`) in the API response vs UI-only rendering.
   - Recommendation: assert on the actual returned field on-hardware; if UI-only, drive a query whose answer provably requires the doc and assert the planted fact is present (retrieval proven) while logging the citation field for the human-verify step.
3. **Negative-control egress mechanism on the dev/CI host.**
   - What we know: an external `curl` from a `villa.network` container that MUST fail proves egress is blocked.
   - What's unclear: whether the host firewall is under the test's control or must be set up as a verification-wave precondition.
   - Recommendation: prefer the container-from-villa.network negative-control probe (self-contained); document a host-egress precondition for the on-hardware verification wave.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| podman (rootless) | render/reconcile/proof | ✓ (project baseline) | v5 | — |
| `villa-qdrant` service | RAG vector store | ✓ (Phase-19) | qdrant v1.18.2-unpriv | — (memory-on requires it) |
| `villa-embed` service | embeddings | ✓ (Phase-19) | nomic-embed-text-v1.5 768d | — |
| OWUI pinned image | env target | ✓ (Phase-4 pin) | main@7f1b0a1a | — |
| Host outbound-block / firewall for negative control | D-10 proof honesty | ✗ (must arrange) | — | Container-from-villa.network external-host probe that must fail (self-contained) |

**Missing dependencies with no fallback:** none (memory-on legitimately requires the Phase-19 services; that's the gate, not a blocker).
**Missing dependencies with fallback:** host egress block → use the container-based negative-control probe.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven, golden fixtures, injected `func` seams) — no third-party libs |
| Config file | none (`go test`) |
| Quick run command | `go test ./internal/orchestrate/... ./cmd/villa/... -run 'OpenWebUI|Memory|RagSmoke|Telemetry' -count=1` |
| Full suite command | `make check` (vet + `go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| INFRA-03 | memory-on OWUI unit carries the full D-09 env block, sourced from config | unit/golden | `go test ./internal/orchestrate -run 'OpenWebUIMemory' -count=1` | ❌ Wave 0 (new memory-on golden + test) |
| INFRA-03 | memory-OFF OWUI unit byte-identical to v1.2 golden | unit/golden | `go test ./internal/orchestrate -run 'OpenWebUIContainerGolden' -count=1` | ✅ (must stay green) |
| INFRA-03 | `ENABLE_PERSISTENT_CONFIG=False` present + telemetry posture re-audited | unit | `go test ./internal/orchestrate -run 'TelemetryFrozen' -count=1` | ✅ (make memory-aware) |
| D-04 | env values are config-sourced; `TestSeamGrepGate` green | unit | `go test ./internal/inference -run 'SeamGrepGate' -count=1` | ✅ |
| MEM-01/02/04 | manual save → cross-chat injection; delete stops injection | integration (on-hardware/UAT) | manual + REST: add memory, new chat asserts injection, delete asserts no-injection | ❌ Wave 0 / UAT |
| MEM-03 | auto-extraction default-off, toggleable | manual/doc | UAT: confirm no auto-save by default; document enable path (Native FC / filter) | ❌ UAT |
| KB-01/02/03 | upload → retrieve → cite via local path | integration (smoke) | `go test ./cmd/villa -run 'RagSmoke' -count=1` (pure eval core) + on-hardware drive | ❌ Wave 0 (pure core unit) + on-hardware |
| PRIV-05 | runtime zero-outbound document-upload (negative control passes) | integration (on-hardware) | on-hardware verify-wave: drive RAG path with egress blocked; negative control must fail | ❌ on-hardware verify wave |

### Sampling Rate
- **Per task commit:** `go test ./internal/orchestrate/... -run 'OpenWebUI|Telemetry' -count=1` + `go vet ./...`
- **Per wave merge:** `make check`
- **Phase gate:** `make check` green + on-hardware runtime zero-outbound smoke (PRIV-05) green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/orchestrate/testdata/villa-openwebui.container.memory.golden` — new memory-on golden (D-05)
- [ ] `internal/orchestrate/render_test.go` — new memory-on fixture + golden test; make `TestRenderOpenWebUITelemetryFrozen` memory-aware
- [ ] `cmd/villa/install_memory.go` (or new `verify_memory.go`) — `evalRagSmoke` pure core + `liveRagSmoke` seam (mirrors `evalMemoryProof`/`liveMemoryProof`); unit-test with injected probes
- [ ] On-hardware verification-wave step driving the real REST RAG path with egress blocked + negative control (cannot be a pure unit test)

*(Pure off-hardware: the env golden + telemetry test + `evalRagSmoke` core with injected probes. The actual zero-outbound assertion is on-hardware by nature — that is SC#4's whole point.)*

## Security Domain

> `security_enforcement: true`, ASVS level 1.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes (OWUI admin for smoke test) | OWUI `WEBUI_AUTH=True` local admin; Bearer token for REST; no new auth surface |
| V3 Session Management | no | — (single-user local; no change) |
| V4 Access Control | partial | Qdrant/embed remain container-DNS only (no host port); OWUI loopback-only (D-11) |
| V5 Input Validation | yes | All `podman`/`curl` args fixed; URLs composed from config (no shell interpolation, T-19-10); model/coll ids config/REST-resolved |
| V6 Cryptography | no | No new crypto; `sk-no-key-required` is a non-secret sentinel |
| V7 Errors/Logging | yes | Proof refuses-with-remediation; no secrets logged |
| V12/V13 Network/API | yes | No new published port; zero-outbound is the headline control (PRIV-05) |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Telemetry/data exfil to PostHog/HF | Information Disclosure | `VECTOR_DB=qdrant`, `ANONYMIZED_TELEMETRY=False`, `OFFLINE_MODE`/`HF_HUB_OFFLINE`; runtime zero-outbound proof |
| Runtime model download (egress) | Information Disclosure | `RAG_EMBEDDING_MODEL_AUTO_UPDATE=False` + dedicated local embedder; no HF runtime fetch |
| Config silently ignored (env appears wired, DB wins) | Tampering/Repudiation | `ENABLE_PERSISTENT_CONFIG=False` (mandatory) |
| Shell injection via service args | Tampering | Fixed-arg `exec.Command`; config-resolved URLs; no interpolation |
| New host-port exposure | Elevation/Spoofing | No new PublishPort (D-11); container-DNS only; loopback audit stays green |
| False-green zero-outbound (egress never actually blocked) | Repudiation | Negative-control external probe that MUST fail; silent skip = FAIL |

## Sources

### Primary (HIGH confidence)
- `backend/open_webui/config.py` (open-webui/open-webui, `main`) — exact defaults + PersistentConfig status for VECTOR_DB, QDRANT_*, ENABLE_QDRANT_MULTITENANCY_MODE (default `true`, plain env), RAG_EMBEDDING_* (ConfigVar), ENABLE_MEMORIES (ConfigVar), RAG_EMBEDDING_QUERY/CONTENT_PREFIX/PREFIX_FIELD_NAME (plain env)
- Repo (read in-session): `internal/orchestrate/openwebui.go`, `render.go`, `memory.go`, `render_test.go`, `internal/memory/memory.go`, `cmd/villa/install_memory.go`, `cmd/villa/install.go`, `villa-openwebui.container.golden` — integration touchpoints + the unconditional-view + telemetry-test findings

### Secondary (MEDIUM confidence)
- https://docs.openwebui.com/reference/env-configuration/ — ENABLE_MEMORIES default True/ConfigVar; ENABLE_PERSISTENT_CONFIG semantics
- https://docs.openwebui.com/features/chat-conversations/memory/ — native memory UI, Native Function Calling memory tools (auto-memory path)
- https://docs.openwebui.com/features/workspace/knowledge/ — upload flow, `#` reference, citations automatic, Full Context Mode / hybrid caveats
- https://docs.openwebui.com/reference/api-endpoints/ + Discussion #16402 — REST endpoints (files, knowledge, chat/completions with collection)
- https://deepwiki.com/open-webui/open-webui/12.2-persistentconfig-system — DB-over-env precedence detail

### Tertiary (LOW confidence)
- GitHub Discussion #13930 (multitenancy proposal) — collection layout description (its "default False" is outdated; resolved against source)
- Community function pages (Adaptive Memory / Auto Memory) — confirm auto-extraction is opt-in filter/agentic, not a core env toggle

## Metadata

**Confidence breakdown:**
- Env contract (key names/defaults/PersistentConfig): HIGH — read from OWUI source `config.py`
- Code integration (parameterize view, golden re-freeze, telemetry test): HIGH — read the actual repo files
- Multitenancy default + behavior (D-01): HIGH on default `True` (source); MEDIUM on the post-vector toggle-orphan claim (inferred)
- Auto-memory mechanism (D-07/MEM-03): MEDIUM — docs describe Native FC tools + community filters; exact per-model toggle UX should be confirmed in UAT
- Runtime zero-outbound proof design (D-10): MEDIUM — pattern is sound (extends a proven seam); the admin-token mint + citation field + egress mechanism need on-hardware confirmation (Open Questions 1–3)

**Research date:** 2026-06-09
**Valid until:** 2026-07-09 (OWUI `:main` is fast-moving; the pinned digest insulates the running stack, but re-verify keys if the digest is bumped)
