# Requirements: VillaStraylight — v1.3 Memory & Knowledge (local RAG)

**Defined:** 2026-06-09
**Core Value:** Run a capable local AI workspace that "just works" after install — now extended so the assistant *remembers* the user across chats and can recall past conversations and uploaded documents, with zero data leaving the box.

## v1.3 Requirements

Requirements for the v1.3 milestone. Each maps to exactly one roadmap phase (see Traceability). Delivery is **integrate + orchestrate** — Go stays the control plane; no custom RAG/embeddings engine is built.

### Memory Infrastructure (INFRA)

- [x] **INFRA-01**: `villa install` orchestrates a local Qdrant vector DB as a rootless Podman Quadlet service on `villa.network` (digest-pinned image, named `:Z` volume, no published/host port — loopback/container-DNS only)
- [x] **INFRA-02**: `villa install` orchestrates a local embeddings `llama-server` exposing an OpenAI-compatible `/v1/embeddings` endpoint (reuses the existing pinned toolbox image, container-DNS only), serving a pinned default embedding model
- [x] **INFRA-03**: Open WebUI is wired (env-only, behind the orchestrate seam) to use Qdrant as its vector DB and the local embeddings endpoint, with `ENABLE_PERSISTENT_CONFIG=false` so `villa` config stays the single source of truth
- [x] **INFRA-04**: The memory stack is config-driven — new `config.toml` fields (enable flag, embedding model, service ports/addrs) regenerate the Quadlet units; units are never hand-edited as the authority

### Personalized Memory (MEM)

- [x] **MEM-01**: The assistant remembers user-stated facts across chats (Open WebUI Memory enabled) and injects them into future conversations
- [x] **MEM-02**: User can explicitly save a specific message/fact to memory from a chat
- [x] **MEM-03**: Memory is automatically extracted from conversations (LLM-assisted), configurable on/off
- [x] **MEM-04**: User can view, edit, and delete stored memories

### Conversational Recall (RECALL)

- [x] **RECALL-01**: A `villa`-orchestrated indexer semantically indexes past conversations into the vector store (chats → Knowledge), running locally
- [ ] **RECALL-02**: The assistant can retrieve relevant past-chat content *by meaning* (semantic, not just keyword) into the current conversation's context
- [x] **RECALL-03**: The chat index stays current as conversations grow — incremental/re-index is `villa`-controllable and reports honest state (no silent staleness)

### Document Knowledge Base (KB)

- [x] **KB-01**: User can upload documents into a local knowledge collection in Open WebUI
- [x] **KB-02**: The assistant answers using retrieved document content and shows citations
- [x] **KB-03**: Document chunking, embedding, and retrieval run entirely through the local embeddings + Qdrant path (no cloud API, no runtime model download)

### Control-Plane Integration (CTRL)

- [ ] **CTRL-01**: `villa recommend` reserves the embedding-model footprint in the unified-memory fit math *before* the chat-model fit, so the recommended config never OOMs or silently CPU-falls-back on gfx1151
- [ ] **CTRL-02**: `villa status` and the control dashboard surface memory-stack health (Qdrant + embeddings service rows, active embedding model) as an append-only, schema-bumped contract change (golden re-frozen once)
- [ ] **CTRL-03**: `villa doctor` includes memory-stack health checks (services up, offload-asserting residency under embedding load, vector-disk/headroom), folded into its existing PASS/WARN/FAIL exit contract
- [ ] **CTRL-04**: `villa backup`/`restore` cover the Qdrant memory volume — clean-recreate-before-import (no stale-vector leak) with the embedding dimension recorded in the manifest for version-skew warning
- [ ] **CTRL-05**: `villa model swap` is memory-aware — it warns/guards when changing the embedding model would invalidate existing vectors (dimension mismatch / no auto-reindex)
- [ ] **CTRL-06**: `villa preflight` gates host fitness for the memory stack (disk space for the vector index, memory headroom for the embedder) with refuse-with-remediation

### Privacy & Zero-Outbound (PRIV) — continues v1.0 PRIV-01/02/03

- [x] **PRIV-04**: No embedding/reranker model is downloaded from the internet at runtime — the embedding model is pre-staged at install and offline mode is enforced (`OFFLINE_MODE`/`HF_HUB_OFFLINE`/`*_AUTO_UPDATE=false`)
- [x] **PRIV-05**: The memory stack emits no telemetry (Qdrant chosen over telemetry-posting ChromaDB; `ANONYMIZED_TELEMETRY=False`), verified by a **runtime** firewalled document-upload zero-outbound smoke test — not just install-time green

## v2 Requirements

Deferred to a future release. Tracked but not in the v1.3 roadmap.

### Retrieval Quality (RAG-Q)

- **RAG-Q-01**: Hybrid (vector + BM25) search with a local reranker model (`ENABLE_RAG_HYBRID_SEARCH` / `RAG_RERANKING_MODEL`) — deferred to keep within the gfx1151 unified-memory envelope (a reranker is a third resident model + added latency)

### Search & Agents

- **SRCH-01**: SearXNG local search integration
- **CODE-01**: OpenCode (local-model coding agent) wiring

### Access

- **REMOTE-01**: Authenticated remote / multi-user access (memory currently assumes the strictly-local single-user posture)

## Out of Scope

Explicitly excluded for v1.3. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| macOS / Apple Silicon / Metal backend | **Permanently out of scope** (decided 2026-06-09); the `Backend` interface keeps a port architecturally possible but it is no longer a planned goal |
| Custom Go RAG / embeddings engine | Go is the control plane only; integrate Open WebUI's native Memory/RAG + OSS services, never rebuild them |
| Cloud embedding / LLM APIs | Violates the strictly-local, zero-new-outbound posture; embeddings must run locally |
| Embedded SQLite / cgo vector store in `villa` | Breaks the single static CGO-free binary constraint; vector data lives in the Qdrant podman volume |
| Reranker / hybrid search (v1.3) | Deferred to v2 (RAG-Q-01) — third resident model + latency on a constrained envelope |
| Multi-user memory scoping / per-user auth | Strictly-local single-user posture; remote/multi-user auth is a separate deferred milestone (REMOTE-01) |
| Web-search-augmented RAG | Would introduce runtime outbound; conflicts with zero-outbound (and SearXNG is a separate deferred item) |

## Traceability

Which phases cover which requirements. Populated during roadmap creation (v1.3 phases continue from v1.2 — numbering starts at **Phase 18**).

| Requirement | Phase | Status |
|-------------|-------|--------|
| INFRA-01 | Phase 19 | Complete |
| INFRA-02 | Phase 19 | Complete |
| INFRA-03 | Phase 20 | Complete |
| INFRA-04 | Phase 18 | Complete |
| MEM-01 | Phase 20 | Complete |
| MEM-02 | Phase 20 | Complete |
| MEM-03 | Phase 20 | Complete |
| MEM-04 | Phase 20 | Complete |
| RECALL-01 | Phase 21 | Complete |
| RECALL-02 | Phase 21 | Pending |
| RECALL-03 | Phase 21 | Complete |
| KB-01 | Phase 20 | Complete |
| KB-02 | Phase 20 | Complete |
| KB-03 | Phase 20 | Complete |
| CTRL-01 | Phase 22 | Pending |
| CTRL-02 | Phase 23 | Pending |
| CTRL-03 | Phase 22 | Pending |
| CTRL-04 | Phase 23 | Pending |
| CTRL-05 | Phase 23 | Pending |
| CTRL-06 | Phase 22 | Pending |
| PRIV-04 | Phase 19 | Complete |
| PRIV-05 | Phase 20 | Complete |

**Coverage:**

- v1.3 requirements: 22 total
- Mapped to phases: 22 ✓ (Phases 18–23)
- Unmapped: 0 ✓
- Duplicates (mapped to >1 phase): 0 ✓

**Per-phase requirement counts:**

| Phase | Requirements | Count |
|-------|--------------|-------|
| Phase 18 — Memory Spine (config core + research spike) | INFRA-04 | 1 |
| Phase 19 — Vector Store + Local Embeddings Services | INFRA-01, INFRA-02, PRIV-04 | 3 |
| Phase 20 — Open WebUI Memory/RAG Wiring + Offline Lockdown | INFRA-03, MEM-01, MEM-02, MEM-03, MEM-04, KB-01, KB-02, KB-03, PRIV-05 | 9 |
| Phase 21 — Conversational Recall Indexer | RECALL-01, RECALL-02, RECALL-03 | 3 |
| Phase 22 — Control-Plane Fit + Host Gate | CTRL-01, CTRL-03, CTRL-06 | 3 |
| Phase 23 — Surfacing, Backup & Memory-Aware Swap | CTRL-02, CTRL-04, CTRL-05 | 3 |

---
*Requirements defined: 2026-06-09*
*Last updated: 2026-06-09 — roadmap created; all 22 v1.3 requirements mapped to Phases 18–23 (100% coverage, no orphans, no duplicates). v1.3 phase numbering continues from v1.2 (last phase 17).*
