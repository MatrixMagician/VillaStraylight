# Feature Research

**Domain:** Strictly-local memory / RAG system for a self-hosted Open-WebUI-based AI stack (VillaStraylight v1.3)
**Researched:** 2026-06-09
**Confidence:** HIGH (Open WebUI native behavior, verified against official docs.openwebui.com); MEDIUM on community Function/Pipeline patterns and the past-chats-recall gap (GitHub discussions/issues)

> **One-line verdict for the requirements step.** Of the four chosen v1.3 capabilities, **three are essentially NATIVE to Open WebUI** (personalized memory, document knowledge base, automatic LLM-assisted extraction) and need villa only to *orchestrate + wire + back up* them. **One is a genuine gap**: semantic *conversational recall over past chats* is NOT native RAG — Open WebUI only does literal text search over chat titles/messages (`search_chats`/`view_chat`), and true semantic chat-history recall is an open upstream feature request. That gap is the single biggest scoping decision in this milestone.

---

## Native-vs-Must-Build, per chosen capability

This is the table the requirements step needs most. "Native" = ships in Open WebUI today; villa's job is install/wire/surface/back-up. "Build/External" = needs a villa-side or external component.

| Capability (v1.3) | Native to Open WebUI? | What's native | What villa / external must add |
|-------------------|-----------------------|---------------|--------------------------------|
| **Personalized memory** (facts remembered across chats) | **YES** (Beta) | Settings > Personalization > Memory; facts stored in `webui.db`, scoped per user; manual add/edit/delete; agentic `add_memory`/`search_memories`/`replace_memory_content`/`delete_memory`/`list_memories` tools; memories injected as context | Nothing to build — only **enable + surface + back up**. Quality of *auto*-capture depends on the local model (see anti-features). |
| **Document knowledge base** (RAG over uploads + citations) | **YES** | Knowledge collections (upload file/dir, sync dir, add text); LangChain `RecursiveCharacterTextSplitter`/`TokenTextSplitter` chunking; vector + optional BM25 hybrid search + reranking; **citations** rendered inline | **Wire the backing services**: a vector DB (default Chroma is single-process SQLite — see below) and a **local** embedding path. No custom Go RAG engine. |
| **Capture: automatic LLM-assisted extraction** | **YES (native) + community alternatives** | End-of-conversation background model analyzes the chat and saves memories; agentic-mode tools let the model save mid-conversation | **Choose + configure** the extraction path; community Functions/Filters ("Adaptive Memory", "Auto Memory") exist if the native flow underperforms on small models. Largely a **config/recommendation** problem, not a build. |
| **Capture: explicit user save/pin + edit/delete** | **YES** | Manual memory CRUD in Personalization settings; manual Knowledge curation in Workspace | Nothing to build — surface it in docs/dashboard. |
| **Conversational recall over PAST CHATS (semantic)** | **NO — GAP** | Only **literal/fuzzy text search** across chat titles, message content, tags (`search_chats` returns IDs+snippets, `view_chat` fetches full history). No embedding/RAG indexing of chat history. | **The build/decision item.** Options: (a) accept native keyword search as "recall"; (b) periodically export prior chats into a Knowledge collection so they become RAG-retrievable (semi-automatic, villa-orchestrated); (c) wait on upstream. True semantic chat-history RAG is an **open feature request** (open-webui #13568/#13041/#13595), not shipping. |

**Reading for scope:** memory, document-KB, and auto-extraction are **integration/orchestration** work (villa's wheelhouse). Conversational-recall-as-semantic-RAG is **net-new behavior** — decide early whether v1.3 ships native keyword recall (cheap, honest) or a villa-driven "chats → Knowledge" indexer (more value, more surface).

---

## Backing-services facts (drive STACK + orchestrate scope)

- **Default vector DB = ChromaDB**, embedded as a local SQLite `PersistentClient` — **single-process, not fork/multi-worker safe**. For a separate, durable, restart-surviving Quadlet service the right move is `VECTOR_DB=qdrant` (or pgvector/milvus). Supported: `chroma | qdrant | milvus | pgvector | opensearch | elasticsearch | pinecone` via the `VECTOR_DB` env var. Qdrant matches the PROJECT.md candidate and runs cleanly as a rootless container. *(MEDIUM — DeepWiki + GitHub discussions; HIGH that the var/options exist.)*
- **Default embeddings = `sentence-transformers/all-MiniLM-L6-v2`, loaded IN-PROCESS inside the Open WebUI container** (downloads from HuggingFace on first use — an outbound pull). Alternatives via `RAG_EMBEDDING_ENGINE`: `ollama` or `openai` (any OpenAI-compatible base URL). *(HIGH — env-config docs.)*
- **Local-only embeddings path that fits villa's posture:** run a **dedicated `llama-server --embeddings`** instance on its own port with a GGUF embedding model (e.g. an e5/arctic-embed/nomic-embed GGUF), then point Open WebUI at it with `RAG_EMBEDDING_ENGINE=openai` + a loopback base URL. This reuses villa's existing llama.cpp / OpenAI-compatible contract and the inference `Backend` seam, and keeps everything on-box. Caveat: llama.cpp's `/v1/embeddings` has had version-sensitive breakage — **pin by digest and assert it on install** (villa already pins images by digest). *(MEDIUM — llama.cpp issues/README + Open WebUI env docs.)*
- **Re-index cost:** changing the embedding model invalidates all vectors (different vector spaces) → a full Knowledge re-index. Pin the embedding model as carefully as the inference image; treat a change as a migration. *(HIGH.)*
- **First-use HF download** for embeddings violates "zero new outbound beyond image/model pulls" unless the embedding model is **pre-pulled like other model weights**. Fold it into the existing weight-pull + recommend flow. *(HIGH — this is a privacy-posture requirement, not optional.)*

---

## Feature Landscape

### Table Stakes (Users Expect These)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Personalized memory ON + injected into chats | "It should remember I prefer Python / live in Vienna." Baseline of any 2026 memory product | LOW (villa) | Native OW feature; villa enables + ensures persistence across restarts. |
| Manual save / edit / delete of memories | Users demand control over what's remembered (privacy product) | LOW (villa) | Native Personalization CRUD; surface, don't build. |
| Document upload → RAG answers **with citations** | "Upload my PDFs and ask questions, show me where it came from" | LOW–MED (villa) | Native Knowledge + citations; cost is wiring vector DB + local embeddings. |
| Local vector DB as a durable, boot-surviving service | A KB that loses its index on restart feels broken | MED | Qdrant Quadlet `.container` + `.volume`; default Chroma-SQLite is too fragile for the daily-driver bar. |
| Local-only embeddings (zero new outbound) | Strict-local is the product's core value; cloud embeddings are disqualifying | MED | `llama-server --embeddings` or pre-pulled SentenceTransformers; **pre-pull, don't first-use-download**. |
| Memory + KB included in backup/restore | v1.2 shipped backup/restore; users expect memory to be covered too | MED | Extend `internal/backup` to the new volumes (vector DB volume; OW volume already covered; embedding weights as identities). |
| Health/status visibility of the memory stack | Daily-driver bar = "is my KB / vector DB healthy?" | MED | Extend `villa status`/`doctor`/dashboard to the new services (append-only, schema-bump per frozen-contract rule). |

### Differentiators (Competitive Advantage)

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| **Hardware-aware memory-stack recommend** (vector DB + embedding model sized to the memory envelope) | Nobody else picks a *fitting* embedding model + reserves headroom for it alongside the chat model | MED | Extend `recommend`: embedding model adds resident MB + a second llama-server; fit math must account for it. This is villa's signature move. |
| **One-command, digest-pinned, rootless memory stack** | Integrate-and-orchestrate done right: Qdrant + embeddings come up healthy with the rest, no compose, no Docker | MED | New Quadlet units behind `orchestrate`; config remains single source of truth. |
| **Memory-stack residency/health proof** (`villa doctor` covers KB + vector DB + embeddings) | "Just works after install" extended to memory; offload-asserting honesty applied to embeddings too | MED | Embedding server should be offload-asserted like inference (no silent CPU fallback false-green). |
| **villa-orchestrated "past chats → Knowledge" indexer** (optional) | Closes the conversational-recall gap *honestly* by reusing native RAG instead of faking semantic search | HIGH | Periodic export of prior chats into a Knowledge collection so they become citable/retrievable. Net-new behavior; scope carefully or defer. |
| **Backup/restore that round-trips the whole memory brain** | Your "second brain" is portable + recoverable, weights excluded/re-pulled (matches v1.2 archive discipline) | MED | Extends the proven v1.2 transactional restore to vector-DB + memory volumes. |

### Anti-Features (Commonly Requested, Often Problematic)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| **Custom Go RAG / embedding engine** | "Full control, single binary" | Reinvents mature OW RAG + llama.cpp embeddings; violates "Go = control plane only" and "integrate, don't rebuild"; massive surface | Wire OW native RAG to Qdrant + a local embedding server. Explicitly out of scope per PROJECT.md. |
| **Faking semantic chat recall by silently dumping recent chats into context** | Mimics ChatGPT "reference chat history" | Blows the context window, degrades the model, hides what's happening; small local models choke | Ship native keyword `search_chats` recall, or the explicit "chats → Knowledge" indexer; be honest it's keyword vs semantic. |
| **Aggressive always-on auto-memory on a small local model** | "Make it remember everything automatically" | Small local models store junk/PII, mis-select, hallucinate facts → memory pollution the user must clean up | Default auto-extract conservative or off; favor explicit save; document the model-quality dependency. |
| **External / cloud vector DB or cloud embeddings (Pinecone, OpenAI embeddings)** | Easy, scalable, "just works" | Sends user content off-box — disqualifying for a zero-telemetry strict-local product | Local Qdrant + local embeddings only; never expose a cloud embedding default. |
| **Multi-user / shared memory + RBAC tuning** | OW supports per-role memory permissions | Out of v1 scope (strictly-local single-user); adds auth surface | Single-user defaults; remote/multi-user already deferred in PROJECT.md. |
| **First-use HuggingFace auto-download of embeddings** | It's the OW default, zero config | Silent outbound at runtime breaks the zero-new-outbound posture and "fails" on an air-gapped box | Pre-pull the embedding model in the install/weight-pull flow; assert presence in preflight. |

---

## Feature Dependencies

```
Local embedding server / model (llama-server --embeddings OR pre-pulled ST model)
    └──required by──> Document Knowledge Base (RAG)
    └──required by──> "Past chats → Knowledge" semantic recall (if built)

Local vector DB service (Qdrant Quadlet)
    └──required by──> Document Knowledge Base (RAG)
    └──required by──> "Past chats → Knowledge" semantic recall (if built)

Personalized Memory (native, webui.db)
    └──independent of──> vector DB / embeddings   (memory ≠ RAG; stored as facts in webui.db)

Automatic LLM-assisted extraction ──enhances──> Personalized Memory
    └──quality-gated-by──> local chat model capability

Hardware-aware recommend (embedding-model fit)
    └──must-precede──> install of the memory stack (envelope must include embeddings)

Memory-stack backup/restore  ──extends──> v1.2 internal/backup (new volumes)
status / doctor / dashboard surfacing ──extends──> v1.2 read-models (append-only + schema bump)
```

### Dependency Notes

- **KB requires both a vector DB and a local embedding path.** Neither alone is enough; ship them together or the KB is non-functional. Order: stand up Qdrant + embeddings, *then* enable Knowledge.
- **Personalized Memory is independent of the RAG stack.** It lives in `webui.db` as facts, not vectors — it can ship in an earlier/separate phase than the vector-DB work, de-risking the milestone.
- **Recommend must precede install.** The embedding model consumes memory and (if llama-server-based) a second service; the fit math and recommendation must account for it before anything is installed, or the "runs healthy" bar breaks.
- **Auto-extraction quality is gated by the chat model**, not by villa code — a config/recommendation concern, and a documented limitation.
- **Conversational-recall semantic option depends on the same vector DB + embeddings** as the KB, so if built it should follow (not precede) the KB phase.

---

## MVP Definition

### Launch With (v1.3 core)

- [ ] **Local vector DB (Qdrant) + local embedding path** as rootless Quadlet services, digest-pinned, boot-surviving — essential substrate for KB.
- [ ] **Document Knowledge Base wired to them**, with native chunking + citations — the headline RAG deliverable.
- [ ] **Personalized Memory enabled + persistent**, manual save/edit/delete surfaced — cheap, high-value, independent of RAG.
- [ ] **Hardware-aware recommend extended to the embedding model** (fit math includes it) — protects the "runs healthy" bar.
- [ ] **Pre-pulled local embeddings (zero new outbound)** + preflight assertion — non-negotiable privacy posture.
- [ ] **Backup/restore + status/doctor/dashboard extended to the memory stack** — daily-driver operability parity with v1.2.

### Add After Validation (v1.3.x)

- [ ] **Automatic LLM-assisted memory extraction tuned for local models** — ship conservative/off first, enable once recall quality is validated on the recommended model.
- [ ] **"Past chats → Knowledge" semantic recall indexer** — only if native keyword `search_chats` proves insufficient; reuses the KB stack.
- [ ] **Hybrid search + reranking knobs** exposed via recommend/config — refinement once base RAG is solid.

### Future Consideration (v2+)

- [ ] **True upstream semantic chat-history RAG** — adopt natively when Open WebUI ships it (open feature request); don't fork it now.
- [ ] **Community Adaptive-Memory Function** as a supported option — only if native memory underperforms broadly.

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Local vector DB + embeddings (Quadlet, pinned, local-only) | HIGH | MEDIUM | P1 |
| Document KB wired + citations | HIGH | LOW–MED | P1 |
| Personalized Memory enabled + CRUD surfaced | HIGH | LOW | P1 |
| Recommend extended for embedding-model fit | HIGH | MEDIUM | P1 |
| Backup/restore + status/doctor/dashboard for memory stack | HIGH | MEDIUM | P1 |
| Automatic LLM-assisted extraction (local-tuned) | MEDIUM | LOW (config) / MED (validate) | P2 |
| Past-chats → Knowledge semantic recall indexer | MEDIUM | HIGH | P2/P3 |
| Hybrid search + reranking tuning | MEDIUM | MEDIUM | P3 |

## Competitor Feature Analysis

| Feature | ChatGPT / Claude (cloud) | DreamServer (local, Docker-Compose) | VillaStraylight approach |
|---------|--------------------------|--------------------------------------|--------------------------|
| Personalized memory | Cloud memory, auto + manage | Open WebUI native memory | OW native memory, strictly local, villa surfaces + backs up |
| Document RAG + citations | Native, cloud-indexed | Open WebUI + Qdrant | OW Knowledge + **local** Qdrant + **local** embeddings, villa-orchestrated |
| Semantic past-chat recall | Native ("reference chat history") | Not solved (OW limitation) | Native keyword recall first; honest villa "chats→Knowledge" indexer only if needed — no faking |
| Auto memory extraction | Native, frontier model | OW background model | OW native, **tuned/gated for local models**, conservative by default |
| Vector DB / embeddings | Managed cloud | Qdrant container, often cloud-ish embeddings | Rootless Qdrant Quadlet + on-box embeddings, digest-pinned, offload-asserted |
| Hardware-aware sizing | N/A (cloud) | Manual model/quant tiers | **villa recommend includes the embedding model in the fit math** (signature differentiator) |
| Backup / portability | Vendor-locked | Manual volume backup | v1.2 transactional archive extended to the memory brain |

## Sources

- [Open WebUI — Memory & Personalization (docs)](https://docs.openwebui.com/features/chat-conversations/memory/) — HIGH
- [Open WebUI — Knowledge (docs)](https://docs.openwebui.com/features/workspace/knowledge/) — HIGH
- [Open WebUI — Retrieval Augmented Generation (RAG) (docs)](https://docs.openwebui.com/features/chat-conversations/rag/) — HIGH
- [Open WebUI — History & Search (docs)](https://docs.openwebui.com/features/chat-conversations/chat-features/history-search/) — HIGH (confirms chat search is text-based, not semantic)
- [Open WebUI — Environment Variable Configuration (VECTOR_DB, RAG_EMBEDDING_ENGINE/MODEL)](https://docs.openwebui.com/reference/env-configuration/) — HIGH
- [Open WebUI — Vector Database Integration (DeepWiki)](https://deepwiki.com/open-webui/open-webui/7.5-vector-database-integration) — MEDIUM
- [feat: Reference Chat History · Discussion #13041](https://github.com/open-webui/open-webui/discussions/13041) — MEDIUM (confirms semantic chat recall is a gap/open request)
- [feat: Add Previous Chats Recall · Issue #13568 / Discussion #13595](https://github.com/open-webui/open-webui/issues/13568) — MEDIUM
- [Open WebUI Adaptive Memory (community Function)](https://open-webui.com/open-webui-adaptive-memory/) — MEDIUM (community extraction patterns)
- [tutorial: compute embeddings using llama.cpp · Discussion #7712](https://github.com/ggml-org/llama.cpp/discussions/7712) + [llama.cpp server README](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) — MEDIUM (local `--embeddings` path)
- [llama-server /v1/embeddings version-sensitivity · Issue #15406](https://github.com/ggml-org/llama.cpp/issues/15406) — MEDIUM (pin-and-assert rationale)

---
*Feature research for: strictly-local memory/RAG on an Open-WebUI stack (VillaStraylight v1.3)*
*Researched: 2026-06-09*
