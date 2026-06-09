# Stack Research

**Domain:** Strictly-local memory/RAG additions for a single-static-binary Go control-plane CLI orchestrating Open WebUI + llama.cpp via rootless Podman/Quadlet on AMD Strix Halo (gfx1151) / Fedora — v1.3 "Memory & Knowledge"
**Researched:** 2026-06-09
**Confidence:** HIGH — all Open WebUI env vars + defaults verified against the authoritative source (`backend/open_webui/config.py` @ `main`); Qdrant version/digests verified against GitHub releases + Docker Hub manifest API; llama-server `--embedding` / OpenAI-compatible `/v1/embeddings` verified against llama.cpp server docs. The embedding-runtime choice (dedicated llama-server vs Open WebUI built-in) is a design call with HIGH confidence on the constraint analysis.

## Scope note

This is a SUBSEQUENT-milestone study. The shipped v1.0–v1.2 stack — Go 1.26.2, cobra v1.10.2, chi v5.3.0, ghw v0.24.0, BurntSushi/toml v1.6.0, charmbracelet/huh, rootless Podman v5 + Quadlet, llama.cpp Vulkan/ROCm backends behind the `Backend` seam, Open WebUI `:main`-pinned-by-digest — is **fixed and NOT re-researched**. This file covers only the NEW capabilities the memory/RAG milestone adds, and how they wire into the existing orchestrate/Quadlet/config plane.

**The single biggest finding:** This milestone needs **essentially zero new first-party Go libraries.** Open WebUI already ships native Memory + RAG + a built-in vector-DB abstraction and an OpenAI-compatible embedding-engine client. The work is **orchestration + config**, not code: stand up two new rootless Quadlet services (a vector DB and a local embedding server) and set the right Open WebUI env vars. That is exactly the "Go is control-plane only; integrate, don't rebuild" constraint.

## Recommended Stack

### Core Technologies (NEW containerized services)

| Technology | Version (pin) | Purpose | Why Recommended |
|------------|---------------|---------|-----------------|
| **Qdrant** (vector DB) | `qdrant/qdrant:v1.18.2-unprivileged@sha256:9f7a04503dbdc17531752927bf0f822b5e71a3b713db547eab92c22210430fc8` | The vector store Open WebUI persists RAG + memory embeddings into | First-class Open WebUI backend (`VECTOR_DB=qdrant`), single static Rust binary, zero external deps, embedded on-disk storage, no SQL server to babysit, runs happily as ONE rootless container. The `-unprivileged` variant runs as non-root → clean fit for rootless Podman. v1.18.2 is the latest stable (released 2026-06-04). See "Vector DB rationale" below for why over pgvector/Chroma/Milvus. |
| **llama.cpp embedding server** (`llama-server --embedding`) | REUSE the already-pinned toolbox image `docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…ac7aad` | A second `llama-server` instance serving an OpenAI-compatible `/v1/embeddings` endpoint for a local embedding GGUF | The toolbox image already ships `llama-server`, which natively supports `--embedding` + OpenAI-compatible `/v1/embeddings`. **No new image, no new backend seam** — it's the SAME binary already proven for inference, run with `--embedding`. Keeps embeddings GPU-accelerated on the iGPU and strictly local. Open WebUI's `openai` embedding engine points straight at it. |
| **Embedding model (GGUF weight)** | `nomic-embed-text-v1.5` (Q8_0 GGUF) — 137M params, 768-dim, 8192-ctx | The actual embedding model the embedding server loads | Best quality-to-size ratio for a local single-user box: ~140–300 MB resident, Matryoshka-truncatable 768→64 dims, 8192-token context (long chunks), Apache-2.0, ubiquitous GGUF availability. Tiny memory-envelope cost next to the main inference model (see memory-envelope analysis). `bge-m3` is the multilingual upgrade if needed (~1.2 GB) — offer as an alternate, not the default. |

### Supporting Libraries (first-party Go — minimal)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| **(none required)** | — | The milestone adds NO new Go module dependency in the happy path | Orchestration reuses existing `internal/orchestrate` Quadlet rendering; config reuses `internal/config`; health reuses `internal/status`/`doctor`; backup reuses `internal/backup`. New code is new *templates* + *config fields* + *status probes*, not new libraries. |
| `net/http` (stdlib) | — | Optional readiness probe of Qdrant `/readyz` and the embedding `/health` during install/doctor | Only if you add a memory-stack readiness gate to `doctor`/`status` — reuses the existing bounded-loopback-scrape pattern (cf. metrics `/metrics`). No new dependency. |

> **Hard rule honored:** no cgo-SQLite, no native libs, no Go RAG/embeddings engine. Qdrant and the embedding server are *containers*; the `villa` binary stays a single static `CGO_ENABLED=0` build.

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `podman volume` (fixed-arg seam) | Persist Qdrant storage + embedding weights | Two new rootless volumes: `villa-qdrant.volume` (vector data) and reuse/extend the models volume for the embedding GGUF. Backup/restore (v1.2) must be extended to cover `villa-qdrant.volume` (weights still excluded; embedding GGUF is re-pullable, vector data is regenerable but worth archiving). |
| Quadlet `*.tmpl` (go:embed) | New unit templates | `qdrant.container.tmpl`, `qdrant.volume.tmpl`, and an `embeddings.container.tmpl` (a second llama-server). All join the existing `villa.network`. |

## Open WebUI configuration surface (exact env vars — VERIFIED against `config.py` @ main)

These are set on the **`villa-openwebui` container** (the existing Open WebUI Quadlet unit), wired via container-DNS to the new services on `villa.network`. All defaults below are quoted from `backend/open_webui/config.py`.

### Vector DB selection
| Env var | Value to set | Default in OWUI | Notes |
|---------|--------------|-----------------|-------|
| `VECTOR_DB` | `qdrant` | `chroma` | Selects the Qdrant backend. |
| `QDRANT_URI` | `http://villa-qdrant:6333` | `None` | Container-DNS to the new Qdrant unit (HTTP REST port). |
| `QDRANT_API_KEY` | (unset / empty) | `None` | Not needed loopback-internal on a private podman network; leave unset. |
| `QDRANT_ON_DISK` | `true` | `false` | Persist vectors to disk (the volume) rather than holding all in RAM — correct for a memory-envelope-constrained box. |
| `QDRANT_PREFER_GRPC` | `false` | `false` | HTTP is fine single-user; leave default. (`QDRANT_GRPC_PORT` default `6334` if ever enabled.) |
| `ENABLE_QDRANT_MULTITENANCY_MODE` | `true` (OWUI default) | `true` | Default; leave as-is. |
| `QDRANT_COLLECTION_PREFIX` | `open-webui` (default) | `open-webui` | Leave default. |

### Local embeddings (point Open WebUI at the local llama-server embedding endpoint)
| Env var | Value to set | Default in OWUI | Notes |
|---------|--------------|-----------------|-------|
| `RAG_EMBEDDING_ENGINE` | `openai` | `''` (built-in SentenceTransformers) | Accepted values: `''`, `ollama`, `openai`, `azure_openai`. `openai` makes OWUI call an OpenAI-compatible `/v1/embeddings` — i.e. our local llama-server. **This moves embedding compute OUT of the OWUI Python process** (frees its RAM, GPU-accelerates embeddings). |
| `RAG_OPENAI_API_BASE_URL` | `http://villa-embeddings:8080/v1` | inherits `OPENAI_API_BASE_URL` | Container-DNS to the new embedding llama-server unit. |
| `RAG_OPENAI_API_KEY` | any non-empty placeholder (e.g. `sk-local`) | inherits `OPENAI_API_KEY` | llama-server ignores it but OWUI's client wants a value. **Stays local — never leaves the box.** |
| `RAG_EMBEDDING_MODEL` | `nomic-embed-text-v1.5` (the model name llama-server advertises) | `sentence-transformers/all-MiniLM-L6-v2` | Must match the GGUF loaded by the embedding server. |
| `RAG_EMBEDDING_BATCH_SIZE` | `1`–`8` | `1` | Keep small to cap peak memory; raise only if embedding throughput on bulk uploads is too slow. |

> **Alternative (zero new service):** Leave `RAG_EMBEDDING_ENGINE=''` and let Open WebUI run `all-MiniLM-L6-v2` (~50 MB) in-process on CPU. Viable for a true minimal install, but it (a) loads a model into the OWUI worker's RAM, (b) is CPU-only (no iGPU), and (c) caps quality at a 384-dim MiniLM. The dedicated llama-server path is the recommended default; the built-in is the documented fallback for the leanest envelope.

### RAG / document knowledge-base settings
| Env var | Suggested value | Default in OWUI | Notes |
|---------|-----------------|-----------------|-------|
| `CHUNK_SIZE` | `1000` (default) | `1000` | Tune later; default is reasonable. |
| `CHUNK_OVERLAP` | `100` (default) | `100` | Default. |
| `RAG_TOP_K` | `3`–`5` | `3` | Retrieved chunks injected per query. |
| `RAG_RELEVANCE_THRESHOLD` | `0.0` (default) | `0.0` | Raise to filter weak matches once tuned. |
| `RAG_FULL_CONTEXT` | `false` (default) | `False` | Keep false; full-context bypasses retrieval and blows the context window on a memory-constrained box. |
| `RAG_EMBEDDING_QUERY_PREFIX` / `RAG_EMBEDDING_CONTENT_PREFIX` | set IF the chosen model needs task prefixes (nomic-v1.5 uses `search_query:` / `search_document:`) | `None` | nomic-embed-text-v1.5 benefits from prefixes; set them for retrieval quality. |

### Personalized memory (cross-chat)
| Env var | Value | Default in OWUI | Notes |
|---------|-------|-----------------|-------|
| `ENABLE_MEMORIES` | `true` (default) | `True` | Native Memory feature — stores user facts and injects them into future chats. Uses the SAME embedding engine + vector store configured above. Capture is both automatic (LLM-assisted extraction) and explicit user save/edit/delete via the UI. No extra service needed. |

> The "conversational recall (RAG over past chats)" and "personalized memory" features both ride the SAME Qdrant + embedding plumbing as document RAG — there is no separate memory store to provision. One vector DB + one embedding endpoint covers all three target features.

## Vector DB rationale — why Qdrant (one recommendation, not a survey)

Open WebUI's `VECTOR_DB` accepts `chroma` (default), `pgvector`, `qdrant`, `milvus`, `opensearch`, `elasticsearch`, `pinecone`, `mariadb-vector`, `oracle23ai`. Only **pgvector** and **chroma** are "consistently maintained by the Open WebUI team"; the rest are community-supported — but Qdrant is mature, widely used, and well-exercised with OWUI.

For a **strictly-local, single-user, rootless-Podman, memory-envelope-constrained** box:

- **Qdrant (CHOSEN):** single self-contained Rust binary, no companion DB server, embedded on-disk storage (Gridstore as of v1.17+), low idle footprint, runs as ONE rootless `-unprivileged` container, first-class OWUI support, supports the hybrid-search/payload features OWUI uses. Best "just works after install" fit — matches the product's bar and DreamServer prior art.
- **pgvector (alternative):** team-maintained, but requires running a **full Postgres server** as a second stateful service (or making Postgres OWUI's primary DB). More moving parts, more memory, more backup surface. Choose only if you later want OWUI's app DB and vectors co-located in one Postgres.
- **Chroma (default, rejected as the pick):** team-maintained and zero-config, but historically heavier/flakier at scale and its embedded mode lives inside the OWUI process rather than as an independently orchestrable/monitorable Quadlet service — which is exactly what `villa` wants to manage and back up. Fine as the do-nothing fallback; not the orchestrated target.
- **Milvus (rejected):** powerful but operationally heavy (etcd + MinIO + multiple services in standalone mode) — disproportionate for a single-user local box.

## Installation (orchestration, not package installs)

```bash
# No `go get`. The milestone is delivered as new Quadlet units + config, pulled by villa:

# 1. Vector DB image (rootless, unprivileged)
podman pull qdrant/qdrant:v1.18.2-unprivileged@sha256:9f7a04503dbdc17531752927bf0f822b5e71a3b713db547eab92c22210430fc8

# 2. Embedding runtime — REUSE the already-pinned toolbox image (no new pull beyond the GGUF)
#    docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…ac7aad  (already present)
#    + pull the embedding GGUF weight (e.g. nomic-embed-text-v1.5.Q8_0.gguf) into the models volume

# 3. Embedding server invocation (inside the second llama-server Quadlet unit):
#    llama-server -m /models/nomic-embed-text-v1.5.Q8_0.gguf --embedding --pooling cls \
#                 --host 0.0.0.0 --port 8080 -ub 8192
#    → serves OpenAI-compatible POST /v1/embeddings
```

## Memory-envelope impact on gfx1151 (the load-bearing tradeoff)

The embedding model **competes with the main inference model for the unified-memory (GTT) envelope** that `recommend`'s fit math already governs (`model_bytes + KV@ctx + headroom ≤ envelope`). The new costs:

| Component | Approx resident cost | Notes |
|-----------|----------------------|-------|
| nomic-embed-text-v1.5 (Q8_0, 137M) | **~140–300 MB** | Negligible vs a multi-GB main model. Recommended default. |
| bge-m3 (568M, multilingual) | ~1.2 GB (up to ~5.7 GB under heavy batching) | Only if multilingual recall is required; raises envelope pressure. |
| Qdrant idle + on-disk vectors | **low hundreds of MB RAM** with `QDRANT_ON_DISK=true` | Disk-backed; minimal RAM idle. |
| all-MiniLM-L6-v2 (built-in, fallback) | ~50 MB, CPU-only, inside OWUI | Cheapest but lower quality, no iGPU. |

**Recommendation for `recommend`:** subtract a small embedding-model reservation (≈300 MB for the nomic default) from the envelope before the main-model fit, OR document the embedding cost as an envelope line item. A second always-resident `llama-server` does add a process, but a 137M embedder is a rounding error next to a 20–70 GB inference model on a 64–128 GB unified-memory box. **Do NOT run a >1 GB embedder by default** — it eats headroom the main model needs.

## Reranker / hybrid search — DEFER (but know the knobs)

Open WebUI exposes `ENABLE_RAG_HYBRID_SEARCH` (default off; combines BM25 + vector), `RAG_RERANKING_ENGINE`/`RAG_RERANKING_MODEL` (default empty), `RAG_TOP_K_RERANKER` (default 3), `RAG_RERANKING_BATCH_SIZE` (default 32). A reranker (e.g. `bge-reranker`) measurably improves precision **but adds a THIRD resident model to the envelope and more first-token latency.**

**Recommendation: ship v1.3 WITHOUT a reranker and with hybrid search OFF by default.** Get the core three features (memory, conversational recall, document KB w/ citations) correct and within the envelope first. Leave the env knobs documented so a power user can opt in, and flag "reranker / hybrid search tuning" as a clean follow-up milestone. This matches the project's pattern of shipping the honest minimum, then proving value before adding load.

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| Qdrant (`VECTOR_DB=qdrant`) | pgvector | If you later move Open WebUI's primary DB to Postgres and want vectors co-located; accept the extra Postgres service. |
| Qdrant | Chroma (built-in default) | Absolute-minimal "do nothing" install; accept in-process storage that `villa` can't independently orchestrate/back up cleanly. |
| Dedicated llama-server embedding endpoint (`RAG_EMBEDDING_ENGINE=openai`) | OWUI built-in SentenceTransformers (`RAG_EMBEDDING_ENGINE=''`) | Leanest envelope / no second service; accept CPU-only, in-process RAM cost, 384-dim MiniLM quality ceiling. |
| nomic-embed-text-v1.5 (768-dim) | bge-m3 (multilingual, ~1.2 GB) | Non-English / cross-lingual document corpora; accept the larger envelope cost. |
| No reranker (v1.3) | bge-reranker + `ENABLE_RAG_HYBRID_SEARCH=true` | Precision-critical retrieval once the core is stable and envelope headroom is confirmed — a later milestone. |

## What NOT to Use / NOT to Add (scope-creep guard)

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| A custom Go RAG / embeddings / chunking engine | Violates "Go is control-plane only; integrate, don't rebuild." Open WebUI already does RAG, chunking, citations, memory. | Open WebUI native RAG + Memory, configured via env. |
| cgo-SQLite or any native vector lib in the `villa` binary | Breaks the single static `CGO_ENABLED=0` build (CI-gated). | Vector data lives in the **Qdrant container**, not the binary. |
| pure-Go embedded vector DB inside `villa` | Same "rebuild" + envelope/architecture mismatch; vectors belong to OWUI's plane. | Qdrant Quadlet service. |
| Any cloud embedding API (OpenAI/Cohere/Voyage) | Violates strictly-local / zero-outbound. `RAG_EMBEDDING_ENGINE=openai` here points at a LOCAL llama-server only. | Local llama-server embedding endpoint. |
| Milvus / OpenSearch / Elasticsearch | Multi-service, heavyweight; disproportionate for single-user local. | Qdrant. |
| Reranker + hybrid search in v1.3 | Adds a third resident model + latency; premature. | Defer; document the knobs. |
| Ollama as the embedding engine (`RAG_EMBEDDING_ENGINE=ollama`) | Would introduce a whole new runtime (Ollama) the stack doesn't use — the project standardized on llama.cpp. | Reuse the existing llama-server image with `--embedding`. |
| Freezing the Open WebUI RAG env surface against `:main` without re-verifying at the pinned digest | RAG/memory env surface evolves between releases; the project pins `:main@digest`. | Pin a tested digest (the `v1.18.x` release line now exists) and re-verify `config.py` at that digest before freezing. |

## Version Compatibility

| Component | Compatible With | Notes |
|-----------|-----------------|-------|
| `qdrant/qdrant:v1.18.2` | Open WebUI `:main` / `v1.18.x` | First-class `VECTOR_DB=qdrant` backend; HTTP REST on `:6333`, gRPC on `:6334`. Use the `-unprivileged` variant for rootless Podman. |
| llama-server `--embedding` | Open WebUI `RAG_EMBEDDING_ENGINE=openai` | OpenAI-compatible `/v1/embeddings`; requires `--pooling` other than `none` (use `cls` or `mean`). Same toolbox image already in use. |
| nomic-embed-text-v1.5 GGUF | llama-server (Vulkan) | 768-dim, 8192-ctx; set query/content prefixes for best retrieval. |
| `QDRANT_ON_DISK=true` | Qdrant v1.17+ Gridstore | v1.17 removed RocksDB for Gridstore — on-disk persistence is the supported path. |

## Sources

- **Open WebUI `backend/open_webui/config.py` @ `main`** (raw GitHub) — AUTHORITATIVE for ALL env var names + defaults: `VECTOR_DB` (default `chroma`), `QDRANT_URI/API_KEY/ON_DISK/PREFER_GRPC/GRPC_PORT/TIMEOUT/HNSW_M/COLLECTION_PREFIX`, `ENABLE_QDRANT_MULTITENANCY_MODE` (default true), `RAG_EMBEDDING_ENGINE` (default `''`), `RAG_EMBEDDING_MODEL` (default `sentence-transformers/all-MiniLM-L6-v2`), `RAG_OPENAI_API_BASE_URL/KEY`, `RAG_OLLAMA_BASE_URL/API_KEY`, `RAG_EMBEDDING_BATCH_SIZE` (default 1), `RAG_RERANKING_ENGINE/MODEL`, `RAG_TOP_K` (3), `RAG_TOP_K_RERANKER` (3), `RAG_RELEVANCE_THRESHOLD` (0.0), `ENABLE_RAG_HYBRID_SEARCH`, `RAG_FULL_CONTEXT`, `CHUNK_SIZE` (1000), `CHUNK_OVERLAP` (100), `RAG_EMBEDDING_QUERY/CONTENT_PREFIX`, `ENABLE_MEMORIES` (default True). — **HIGH**
- **Open WebUI `backend/open_webui/retrieval/utils.py` @ `main`** — embedding-engine branch values (`''`, `ollama`, `openai`, `azure_openai`) and reranking `external` engine. — **HIGH**
- **Qdrant GitHub releases API** — latest stable `v1.18.2` (2026-06-04); v1.17 removed RocksDB → Gridstore. — **HIGH**
- **Docker Hub manifest API (qdrant/qdrant)** — amd64 digests: `v1.18.2` `sha256:da65a06b…7b7071`, `v1.18.2-unprivileged` `sha256:9f7a0450…430fc8`. — **HIGH**
- **llama.cpp `tools/server/README.md` + community guides** — `llama-server --embedding --pooling cls`; OpenAI-compatible `/v1/embeddings` requires pooling ≠ none. — **HIGH**
- **Open WebUI docs: Memory & Personalization / RAG** (docs.openwebui.com) — native Memory (auto-extract + explicit save/edit/delete), RAG over docs with citations, vector-based retrieval; Memory feature is Beta. — **MEDIUM** (feature behavior; env vars cross-checked against config.py = HIGH).
- **2026 embedding-model benchmarks** (promptquorum, milvus.io, morphllm) — footprints: all-MiniLM ~50 MB, nomic-embed-text ~300 MB, mxbai ~700 MB, bge-m3 ~1.2 GB; nomic best quality-to-size, Matryoshka 768→64, 8192-ctx. — **MEDIUM**
- **Project files** `.planning/PROJECT.md`, `CLAUDE.md`, `internal/orchestrate/openwebui.go`, `internal/inference/backend_vulkan.go` — current pins (OWUI `:main@sha256:7f1b0a1a…`, toolbox `vulkan-radv@sha256:9a74e555…`), constraints, house style. — **HIGH**

---
*Stack research for: strictly-local memory/RAG additions to VillaStraylight (v1.3)*
*Researched: 2026-06-09*
