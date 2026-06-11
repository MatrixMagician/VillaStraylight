# Phase 18 — Canonical Spike Decisions (D-07 / D-08 / D-09)

**Recorded:** 2026-06-09
**Status:** PINNED — later phases (19/20/21/22/23) freeze against these values.
**Scope:** This is a **documentation artifact** (SC#3). It introduces NO runtime
code and NO container-image literal. The image literals it references are pinned
behind the `orchestrate` managed-service render path in **Phase 19** (D-10), not here.

> Evidence for every decision below lives in
> [`18-RESEARCH.md` → "Spike Decisions (PINNED with evidence)"](./18-RESEARCH.md#spike-decisions-pinned-with-evidence),
> with the cited primary sources in that file's `## Sources`.

---

## D-07 — Embeddings runtime: dedicated `villa-embed` llama-server  ✅ CONFIRMED

| Field | Pinned value |
|-------|--------------|
| Runtime | A **dedicated `villa-embed` llama-server** — NOT Open WebUI's built-in SentenceTransformers embedder |
| Image | **Reuse the already-pinned kyuz0 toolbox image** (no new embeddings image to legitimacy-audit; Phase 19 pins the digest behind the orchestrate seam) |
| API | OpenAI **`/v1/embeddings`** over `villa.network` — **container-DNS only**, never a routable host bind (PRIV-01 / D-06) |
| Required flags | **`--embeddings`** (trailing **s**) **`--pooling mean`** (or `cls`) — `/v1/embeddings` requires a pooling mode other than `none` |
| OWUI wiring | `RAG_EMBEDDING_ENGINE=openai` pointed at `villa-embed` (see D-09) |

- **Rationale:** OWUI's built-in path lazily downloads the embed model from
  HuggingFace at first upload — the **#1 runtime-privacy risk** and a break under
  `OFFLINE_MODE`/`HF_HUB_OFFLINE`. A dedicated local server sidesteps the
  downloader entirely and gives memory-fit control while reusing a pinned image.
- **Carry-forward risk (Phase 19):** the kyuz0 image is built from llama.cpp
  **master (auto-rebuilt)**; a master regression once broke `/v1/embeddings`
  (issue #15406). The **digest pin** mitigates silent breakage — Phase 19 MUST
  curl `/v1/embeddings` against the pinned digest before freezing the unit.
- **Downstream consumer:** **Phase 19** (renders + starts the `villa-embed` Quadlet unit).
- **Evidence:** `18-RESEARCH.md` → D-07 (llama.cpp server README, discussion #7712,
  issue #15406; OWUI offline-mode notes).
- **Confidence:** HIGH.

---

## D-08 — Embedding model + footprint: `nomic-embed-text-v1.5`, 768-dim, Q8_0 GGUF  ✅ CONFIRMED

| Field | Pinned value |
|-------|--------------|
| Model | **`nomic-embed-text-v1.5`** |
| Dimension | **768** (native; Matryoshka-truncatable to 512/256/128/64 — **pinned at 768, do NOT truncate**) |
| Context | **8192 tokens** |
| GGUF source | `nomic-ai/nomic-embed-text-v1.5-GGUF` |
| Quant | **Q8_0** (best quality/size for a tiny model; F16 acceptable) |
| Measured GGUF file sizes | Q4_K_M = 84.1 MB · **Q8_0 = 146 MB** · F16 = 274 MB · F32 = 548 MB |
| Footprint reservation | **~512 MiB conservative resident reservation** (weights + small embed-context KV + llama-server overhead) — feeds the D-03 fit math |

- **Rationale:** GGUF *file* size is a floor, not resident footprint. A
  **conservative** ~512 MiB over-reserves (never under-reserves) so the shared
  gfx1151 GTT fit math can never miss an OOM it should have caught.
- **Load-bearing dimension:** 768 is a pinned constant — changing it silently
  corrupts existing Qdrant vectors (no auto-reindex). It is the anchor for the
  Phase-23 memory-aware model-swap guard and the backup manifest.
- **Flagged for refinement:** the **~512 MiB constant is an estimate pending
  on-hardware measurement in Phase 19** (measure the resident GTT delta when
  `villa-embed` is up; refine the constant then). Model/dim/context/GGUF are HIGH
  confidence; only the footprint constant is MEDIUM.
- **Usage caveat (Phase 20/21):** nomic-embed-text-v1.5 expects task-instruction
  prefixes (`search_document:` / `search_query:`) for optimal retrieval — a
  Phase-20 verification item, not a Phase-18 blocker.
- **Downstream consumers:** **Phase 19** (pre-stage the GGUF + measure footprint),
  **Phase 22** (CTRL-01 footprint reservation before chat-model fit),
  **Phase 23** (dimension in the backup manifest / memory-aware swap guard).
- **Evidence:** `18-RESEARCH.md` → D-08 (HuggingFace `nomic-embed-text-v1.5` +
  `-GGUF` repos; zilliz model card).
- **Confidence:** HIGH on model/dim/context/GGUF; MEDIUM on the footprint constant.

---

## D-09 — Open WebUI env contract (re-verified against the pinned-digest env reference)  ✅ VERIFIED

> **RECORDED only.** Phase 18 writes NO env block. **Phase 20** sets these keys in
> the `orchestrate` `openwebui.go` env slice (a deliberate golden re-freeze).
> Verified against the current OWUI `docs/reference/env-configuration.mdx`
> (fetched 2026-06-09). Defaults below are OWUI's defaults.

| Env key | Target value (Phase 20) | OWUI default | PersistentConfig? | Notes |
|---------|------------------------|--------------|-------------------|-------|
| `VECTOR_DB` | `qdrant` | `chroma` | selection var | Qdrant over telemetry-posting ChromaDB (PRIV-05) |
| `QDRANT_URI` | `http://villa-qdrant:6333` | (unset) | no | Container-DNS only, no host port |
| `QDRANT_API_KEY` | empty/unset (local private net) | (unset) | no | No auth surface on `villa.network` (Phase 19/20 may add a generated key without schema change) |
| `ENABLE_QDRANT_MULTITENANCY_MODE` | **CHOICE PENDING — Phase 20** | `True` | no | **Decision NOT yet made.** Must be chosen explicitly in Phase 20 **before any vectors exist** (toggling later disconnects existing collections). Affects Phase 21/23 collection layout. |
| `QDRANT_COLLECTION_PREFIX` | `open-webui` (default) | `open-webui` | no | Namespacing for the Phase-21 indexer |
| `RAG_EMBEDDING_ENGINE` | `openai` | empty (=SentenceTransformers) | **YES (`ConfigVar`)** | Points OWUI at `villa-embed`; requires `ENABLE_PERSISTENT_CONFIG=False` |
| `RAG_OPENAI_API_BASE_URL` | `http://villa-embed:8080/v1` | `${OPENAI_API_BASE_URL}` | **YES (`ConfigVar`)** | Local embeddings endpoint, container-DNS |
| `RAG_OPENAI_API_KEY` | `sk-no-key-required` (sentinel) | `${OPENAI_API_KEY}` | **YES (`ConfigVar`)** | llama-server does no auth; non-empty sentinel like the chat path |
| `RAG_EMBEDDING_MODEL` | `nomic-embed-text-v1.5` | `sentence-transformers/all-MiniLM-L6-v2` | **YES (`ConfigVar`)** | Must match the model `villa-embed` serves (D-08) |
| `RAG_EMBEDDING_MODEL_AUTO_UPDATE` | `False` | `True` | no | Offline lockdown — no runtime model update (PRIV-04) |
| `ENABLE_MEMORIES` | `True` | `True` | **YES (`ConfigVar`)** | Personalized memory feature (MEM-01) |
| `ENABLE_PERSISTENT_CONFIG` | **`False` — MANDATORY** | `True` | n/a | **LOAD-BEARING.** Makes env (config) authoritative over OWUI's DB; without it the `ConfigVar` keys above are silently IGNORED after first boot (INFRA-03/INFRA-04). NOT optional. |
| `OFFLINE_MODE` | `True` | `False` | no | Disables OWUI network/update/model-download; auto-forces `ENABLE_VERSION_UPDATE_CHECK=false` |
| `HF_HUB_OFFLINE` | `1` | `0` | no | Blocks all HuggingFace downloads (PRIV-04). Already in the existing OWUI render |
| `ANONYMIZED_TELEMETRY` | `False` | (telemetry on) | no | Telemetry kill (PRIV-05). Already in the existing OWUI render |
| `ENABLE_VERSION_UPDATE_CHECK` | `False` | `True` | no | Already in the existing OWUI render; auto-false under `OFFLINE_MODE` |

- **The load-bearing key:** **`ENABLE_PERSISTENT_CONFIG=False` is MANDATORY, not
  optional.** The RAG/embedding/memory keys are OWUI PersistentConfig
  (`ConfigVar`) values read from OWUI's database, not the environment, unless this
  flag is `False`. Without it "config is the single source of truth"
  (INFRA-03/INFRA-04) does NOT hold for OWUI — the env appears wired but is ignored
  after first boot.
- **Net-new vs the existing `openwebui.go` env block:** the existing render already
  sets `ANONYMIZED_TELEMETRY=False`, `DO_NOT_TRACK=True`, `SCARF_NO_ANALYTICS=True`,
  `OFFLINE_MODE=True`, `ENABLE_VERSION_UPDATE_CHECK=False`, `HF_HUB_OFFLINE=1`,
  `WEBUI_AUTH=True`. **Phase 20 ADDS** the RAG/Qdrant/memory block +
  `ENABLE_PERSISTENT_CONFIG=False` + `RAG_EMBEDDING_MODEL_AUTO_UPDATE=False` — a
  deliberate golden re-freeze (the telemetry-frozen test forces a re-audit on any
  env change).
- **Unmade decision (explicit):** `ENABLE_QDRANT_MULTITENANCY_MODE` is **CHOICE
  PENDING — Phase 20** (defaults `True`). This is recorded as pending, not omitted,
  so the unmade decision is unambiguous.
- **Currency watch:** the env reference tracks OWUI `:main`, not necessarily the
  pinned digest — Phase 20 MUST re-verify the keys' behaviour against the pinned
  OWUI digest before freezing the env block.
- **Downstream consumer:** **Phase 20** (writes the OWUI RAG/Qdrant/memory env
  block + offline/telemetry lockdown; runs the zero-outbound smoke test).
- **Evidence:** `18-RESEARCH.md` → D-09 (OWUI `docs/reference/env-configuration.mdx`,
  fetched 2026-06-09).
- **Confidence:** HIGH (verified against live docs source 2026-06-09).

---

## Cross-cutting privacy posture (the WHY these decisions exist)

The three decisions together close the **#1 runtime-privacy risk** for the v1.3
memory stack — OWUI HuggingFace lazy-download + ChromaDB PostHog telemetry:

1. **D-07** runs a dedicated local embeddings server (no HF runtime download).
2. **D-09** records `VECTOR_DB=qdrant` (no telemetry) + the offline/telemetry
   lockdown set + the **mandatory** `ENABLE_PERSISTENT_CONFIG=False`.
3. **D-08** pins a local GGUF model + its conservative footprint for honest fit math.

Phase 18 only **records**; Phases 19/20 **enforce** and run the zero-outbound
runtime smoke test (Phase 20).
