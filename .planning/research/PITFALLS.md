# Pitfalls Research

**Domain:** Adding strictly-local memory/RAG (vector DB + local embeddings + Open WebUI Memory/RAG) to a shipped privacy-first, single-static-CGO-free-binary Go control plane on AMD Strix Halo / Fedora (VillaStraylight v1.3)
**Researched:** 2026-06-09
**Confidence:** HIGH (Open WebUI official docs + GitHub issues; corroborated across multiple sources for the privacy/dimension/persistence traps)

> Scope note: these are pitfalls specific to wiring Open WebUI's *native* Memory/RAG to a local vector DB + local embeddings under villa's non-negotiables (zero-outbound, CGO-free, unified-memory envelope, config-as-truth, byte-frozen contracts, honesty-by-construction). Generic RAG advice is omitted. The ones tagged **(NON-NEGOTIABLE THREAT)** would, if missed, break a constraint villa has STRIDE-secured since v1.0.

## Critical Pitfalls

### Pitfall 1: Open WebUI silently pulls the embedding model from HuggingFace on first RAG use **(NON-NEGOTIABLE THREAT — zero-outbound)**

**What goes wrong:**
Open WebUI's default RAG engine is `SentenceTransformers` with default model `sentence-transformers/all-MiniLM-L6-v2`, which is **auto-downloaded from HuggingFace Hub on first RAG/document/web-search use** (via `get_ef()` → `SentenceTransformer(...)`). The same happens for the reranker model and for Whisper. This is a fresh, runtime, un-gated outbound connection that villa has never had before — install today only pulls the image; the *first document upload* would phone out to `huggingface.co`. This directly violates PRIV-01/02/03 ("only outbound is image/model pulls during install/update") because it happens at *runtime*, unattended, after install reports green.

**Why it happens:**
The download is lazy and demand-driven, so it does not appear during `villa install` or a health check — it only fires when a user actually uploads a doc or enables memory. Developers verify "install is zero-outbound", see green, and miss the runtime leak. ChromaDB (OWUI's *default* vector store) **additionally** posts PostHog telemetry (`ClientCreateCollectionEvent`) unless `ANONYMIZED_TELEMETRY=False` — a second, independent leak in the same feature.

**How to avoid:**
- Treat the embedding model as a **first-class install artifact**, pulled and verified *during* `villa install` (the only sanctioned outbound window), into a persistent volume — never lazily at runtime.
- Set, in the Open WebUI Quadlet env (mirroring the existing telemetry-kill from Phase 4): `OFFLINE_MODE=true`, `HF_HUB_OFFLINE=1`, `RAG_EMBEDDING_MODEL_AUTO_UPDATE=false`, `RAG_RERANKING_MODEL_AUTO_UPDATE=false`, `WHISPER_MODEL_AUTO_UPDATE=false`, `ANONYMIZED_TELEMETRY=False`, `DO_NOT_TRACK=true`, `SCARF_NO_ANALYTICS=true`, and point `RAG_EMBEDDING_MODEL` at the pre-staged local path.
- Prefer routing embeddings to the **local llama.cpp `/embeddings` endpoint** (`RAG_EMBEDDING_ENGINE=openai` against the loopback `villa-llama` OpenAI-compatible API) so the embedding model is a *gguf villa already manages*, eliminating the HF/sentence-transformers download path entirely. This also keeps villa's "embeddings must run locally" requirement honest-by-construction rather than env-flag-dependent.
- Extend the existing no-telemetry **assertion** in `internal/status` to actively prove zero-outbound for the memory stack — not just trust env vars. An offload-style honesty check: the memory feature is only "ready" if a residency/connectivity probe confirms it embeds locally.

**Warning signs:**
First document upload hangs or fails on an air-gapped/firewalled host; `journalctl --user -u villa-openwebui` shows attempts to resolve `huggingface.co`; a connection probe during a RAG smoke test shows outbound to HF or `app.posthog.com`.

**Phase to address:**
The orchestration/wiring phase that introduces the Open WebUI Memory/RAG env block and the embeddings backend (earliest RAG phase). Verification belongs in the same phase's UAT (zero-outbound smoke test on a doc upload) and in the `status`/`doctor` surfacing phase.

---

### Pitfall 2: Embedding model is omitted from the unified-memory recommend-math → OOM or silent CPU fallback of the *main* model **(NON-NEGOTIABLE THREAT — honesty/just-works)**

**What goes wrong:**
gfx1151 has ONE unified-memory pool (the GTT envelope `mem_info_gtt_total` ≈ 62.5 GiB) shared by inference + KV cache + *now the embedding model + its KV*. villa's `recommend.Pick()` fits `model_bytes + KV@ctx + headroom ≤ envelope` for the *chat* model only. Adding an embedding model (and re-embedding bursts during a document import) silently eats into the same pool. The failure mode is exactly the one villa polices elsewhere: the chat model gets evicted/partially offloaded to CPU, or an import OOMs — and worse, llama.cpp can **silently fall back to CPU** (a FAIL by villa's offload-asserting doctrine, not a false-green).

**Why it happens:**
The embedding model looks small (all-MiniLM is ~90 MB) so it's dismissed as free. But on a shared budget, a bigger/better local embedder (e.g. a 300M–7B gguf, which is what you'd want for quality) plus its working set during a bulk re-embed is not negligible, and the *headroom* villa reserved was calculated without it. Re-embedding an entire document corpus or chat history is a memory *spike*, not a steady state.

**How to avoid:**
- Add the embedding model footprint (weights + its small KV at chunk-size context, e.g. ctx 512) as an explicit term in `recommend.Pick()`'s envelope math. The recommendation must fit chat + embed + headroom, not chat alone.
- Constrain the embedding context to chunk size (ctx ≈ 512), per llama.cpp guidance — slashes embed KV cost ~75% vs 4096.
- Keep the offload-asserting discipline: the memory stack's "healthy" verdict must include a residency proof for the *chat* model surviving an embed/import workload (no silent CPU eviction). Re-use the `ResidencyProof` seam; a partial fallback under RAG load is a FAIL.
- If embeddings run as a *second* `llama-server` (separate Quadlet) rather than the main one, that second process's resident footprint must be in the envelope too.

**Warning signs:**
tok/s on the chat model drops sharply after a large doc import; `villa doctor` residency proof flips to WARN/FAIL during/after import; GTT delta shows the chat model's pages evicted; OOM-kill in `journalctl` during embedding.

**Phase to address:**
The `recommend`/envelope phase for memory (extend `Pick()`), with verification folded into the same `doctor`/`status` residency check that already exists.

---

### Pitfall 3: Open WebUI `PersistentConfig` bakes settings into its SQLite DB on first boot and then *ignores* the env vars — config drifts off villa's source of truth **(NON-NEGOTIABLE THREAT — config-as-truth)**

**What goes wrong:**
Most RAG/memory settings (`VECTOR_DB`, `RAG_EMBEDDING_*`, `ENABLE_RAG_WEB_SEARCH`, telemetry flags, embedding engine/URL) are **`PersistentConfig`** entries: on first run their env value is copied into `webui.db`, and from then on the **database value wins and the env var is ignored** (with `ENABLE_PERSISTENT_CONFIG=true`, the default). villa's whole model is "config.toml is the single source of truth; Quadlet units are regenerated from config, never hand-edited." But here, after first boot, changing the Quadlet env has **no effect** — the real config lives in an opaque SQLite blob inside the Open WebUI volume. A later `villa` change that *looks* applied (unit regenerated, restarted) is silently a no-op. Worse, a user toggling settings in the OWUI Admin UI silently diverges from config.toml — exactly the hand-edit-as-authority anti-pattern villa forbids for Quadlets, reappearing one layer down.

**Why it happens:**
The PersistentConfig precedence is non-obvious and undocumented in the unit file; it only bites on the *second* configuration change, long after the feature looks done. Teams test by editing env on a fresh volume (works), never re-test after the DB is populated.

**How to avoid:**
- Set `ENABLE_PERSISTENT_CONFIG=false` in the Open WebUI Quadlet so env vars (driven by config.toml) always win and the DB never shadows them. This restores villa's "config is the single source of truth" — at the documented cost that Admin-UI config edits become per-session only. That cost is *aligned* with villa's posture (config changes go through `villa`, not hand-edits), so it's the right trade.
- Document explicitly that memory *content* (the user's facts/vectors/documents) lives in the volume and is durable — only *configuration* is forced back to env/config.toml.
- Detect drift: a `doctor`/`status` check that the live RAG config (queryable via OWUI API) matches what config.toml declares — mirror the existing config-vs-disk Quadlet drift check.

**Warning signs:**
A `villa` config change to embedding model / vector DB doesn't take effect after restart; OWUI Admin UI shows different RAG settings than config.toml; dimension-mismatch errors after a config change that "should" have switched models.

**Phase to address:**
The Open WebUI memory wiring phase (set `ENABLE_PERSISTENT_CONFIG=false` from the start — retrofitting after a populated DB exists is painful). Drift verification in the surfacing phase.

---

### Pitfall 4: Embedding/inference model swap invalidates existing vectors (dimension mismatch) — stale index, hard errors, or silent wrong answers **(quality + just-works)**

**What goes wrong:**
Vectors are only comparable if produced by the *same* embedding model at the *same* dimensionality. If the user (or villa's `recommend`) later changes the **embedding** model — or even swaps the **chat** model in a way that changes the embedding route — existing collections (e.g. 384-dim from all-MiniLM) collide with new vectors (e.g. 768/1024-dim), producing `Embedding dimension 768 does not match collection dimensionality 384` errors, or, more insidiously, retrieval that returns garbage because the index is stale. Open WebUI does **not** auto-reindex on embedding-model change; you must explicitly re-save config AND re-embed every document, and frequently **reset the collection** first.

**Why it happens:**
villa already has a first-class model-swap story (`villa model swap`, `villa backend set`). It's natural to assume swapping the chat model is orthogonal to RAG. But if embeddings are routed through the *chat* `llama-server`/`/embeddings`, changing the chat model silently changes the embedder → every stored vector is now from a different model/dimension. Even when embeddings have a dedicated model, a future `recommend` that picks a different embedder breaks the corpus.

**How to avoid:**
- **Decouple the embedding model from the chat model**: pin a *dedicated* embedding gguf (or sentence-transformer) that does NOT change when `villa model swap` / `backend set` runs. Record the embedding model + dimension as part of the memory stack's identity in config.toml.
- Make `villa model swap` / `backend set` **memory-aware**: a chat-model swap must NOT touch the embedding model or the vector collections. Add a guard/assertion that the embedding identity is unchanged across a chat swap.
- If the embedding model itself ever changes, treat it as a **destructive re-index**: clean-recreate the collection (mirror v1.2's "clean-recreate-before-import" restore lesson) and re-embed — never merge new-dim vectors into an old-dim collection. Surface this as an explicit, confirmed `villa` operation, not a silent side effect.
- Store the embedding model name + dimension in the backup manifest so a restore can detect a mismatch (skew-warn) rather than leak stale vectors.

**Warning signs:**
`dimension … does not match collection dimensionality …` in OWUI logs; RAG citations suddenly irrelevant after a model swap; retrieval returns empty after switching backends.

**Phase to address:**
The model-swap-integration phase (make swap memory-aware) and the embedding-identity/recommend phase (pin + record dimension). Backup/restore phase records + checks the identity.

---

### Pitfall 5: Vector volume persistence / rootless-Podman pitfalls — data loss across restart/reboot, or permission-denied on the Qdrant store **(durability + just-works)**

**What goes wrong:**
A new vector DB (Qdrant) Quadlet stores its data in a volume. Three rootless-Podman traps: (1) **UID mapping** — the container's internal UID maps to a high host subuid; a bind mount or wrong-owned named volume yields `permission denied` on `/qdrant/storage`, and Qdrant either crashes or starts empty. (2) **SELinux** — without `:Z`, Fedora's SELinux denies access; with `:Z` shared incorrectly across containers it relabels for one owner only. (3) **Durability** — if the storage path isn't a proper persistent named volume (or lingering isn't enabled), the index doesn't survive logout/reboot, and the user's entire knowledge base silently vanishes — they re-upload and re-pay the re-embed cost, or worse, think it's fine until it isn't.

**Why it happens:**
villa already solved exactly this for the Open WebUI volume (Phase 4: durable `:Z` volume, lingering, boot-survival) — but a *new* service is a fresh chance to get it wrong, and Qdrant's storage is larger and less forgiving than OWUI's. Named volumes "just work" for ownership in rootless mode via `:U`/auto-chown; bind mounts don't.

**How to avoid:**
- Use a **named Podman volume** (like `villa-models.volume` / OWUI's pattern), not a host bind mount, so rootless UID mapping + ownership are handled automatically (`:U` where needed); apply `:Z` for SELinux exactly as the existing OWUI volume does, and only one owner per `:Z` volume.
- Keep the volume generation in `internal/orchestrate` templates (the only impure module) — reuse the existing `volume.tmpl` pattern; do NOT special-case Qdrant outside the seam.
- Ensure `loginctl enable-linger` coverage (already part of install) extends to the new service so it survives reboot; add boot-survival to UAT.
- Add the vector volume to `villa status`/`doctor` so "memory stack healthy" includes "Qdrant volume mounted, writable, and reachable" — honest, not assumed.

**Warning signs:**
Qdrant logs `permission denied` / `read-only`; knowledge base empty after a reboot; SELinux `AVC denied` in `journalctl`/`ausearch`; collection count resets to zero.

**Phase to address:**
The Quadlet orchestration phase that adds the Qdrant service + volume (reuse Phase-4 volume discipline). Boot-survival in its UAT.

---

### Pitfall 6: Backup/restore of vector volumes — stale-vector leakage on restore, and archive bloat **(data integrity, extends v1.2 BAK-01/02/03)**

**What goes wrong:**
Two distinct traps. (a) **Stale-vector leakage:** `podman volume import` MERGES into an existing volume and does not auto-create — v1.2 already learned this and adopted "clean-recreate-before-import" for the OWUI volume. The vector volume needs the *same* discipline: restoring memory vectors into a non-empty/old-dim Qdrant store leaves orphaned or mismatched vectors that pollute retrieval (and if the embedding model differs between archive and host, you get the Pitfall-4 dimension corruption silently). (b) **Archive bloat:** vector indexes for a large corpus can be hundreds of MB–GB. v1.2 deliberately EXCLUDED model weights from the archive (recorded identities for re-pull). The same judgment applies: a multi-GB vector index can balloon a "self-describing local archive" beyond usefulness.

**Why it happens:**
The natural move is to add the Qdrant volume to the existing `villa backup` tar exactly like the OWUI volume. But vectors are *derived* data (re-embeddable from the source documents) AND much larger — both arguments cut against naïvely tarring them, and the restore-merge trap is a known v1.2 footgun.

**How to avoid:**
- Apply v1.2's **clean-recreate-before-import** transactional discipline to the vector volume on restore (capture → quiesce → clean-recreate → offload-asserting prove → verbatim rollback). Never merge into an existing Qdrant store.
- Record the **embedding model name + dimension** in the backup manifest; on restore, skew-WARN (or BLOCK) if the host's embedding model/dimension differs from the archive's — preventing silent dimension corruption.
- Decide explicitly per data class: source documents + memory facts (small, irreplaceable) → include; the *derived vector index* (large, regenerable) → consider excluding (record identity, re-embed on restore) OR include with a size guard, mirroring the weights-excluded judgment. Keep the archive "self-describing."
- Reuse the existing SHA-256 manifest + fail-closed checksum BLOCK; reuse the shared cmd-tier fixed-arg podman-volume seam (orchestrate stays the only impure module).

**Warning signs:**
Restored knowledge base returns results that shouldn't exist (orphaned vectors); dimension-mismatch errors right after a restore; backup archive size jumps from MB to GB; restore onto a host with a different embedder silently "works" then retrieves garbage.

**Phase to address:**
The phase that extends `villa backup`/`restore` to the memory volumes (directly continues BAK-01/02/03).

---

### Pitfall 7: Auto-extraction stores false/hallucinated "memories" and is not user-correctable — honesty failure

**What goes wrong:**
Open WebUI's autonomous memory ("Adaptive/Auto Memory", model-managed `add_memory`/`replace_memory_content`/`delete_memory`) lets the *chat model* decide what facts to persist. With a small *local* model (which is what runs on gfx1151), extraction quality is far below the frontier models the feature is tuned for — it can store wrong, duplicated, over-eager, or hallucinated facts ("user lives in Vienna" from a hypothetical), which then get **injected into every future chat**, compounding the error. If the user can't easily see/edit/delete these, the assistant becomes confidently, persistently wrong — a direct hit on villa's honesty-by-construction value.

**Why it happens:**
Auto-extraction is the flashy demo feature, so it's tempting to enable it by default. But the local-model quality gap and the every-future-chat injection make false memories high-blast-radius, and Native-Function-Calling autonomous memory needs a capable model villa can't guarantee.

**How to avoid:**
- Ship **explicit user save/pin + edit/delete as the primary path** (Settings → Personalization → Memory is always available and manual), with auto-extraction **opt-in**, not default-on — matching the v1.3 requirement "both modes, with edit/delete controls."
- Surface that memories are **user-correctable**: the UAT for the memory feature must include adding, editing, and deleting a memory, and confirming a deleted/edited memory stops being injected.
- Don't over-promise autonomous extraction quality given the local-model constraint; in `recommend`/docs, advise (don't auto-enable) auto-memory, mirroring the ROCm "advise, never auto-switch" honesty pattern.

**Warning signs:**
Memory bank fills with duplicate/contradictory/irrelevant entries; the assistant repeats a wrong "fact" across chats; users can't find where to delete a memory.

**Phase to address:**
The memory-capture phase (wire manual save/pin/edit/delete first; gate auto-extraction behind opt-in). Verification in its UAT.

---

### Pitfall 8: Open WebUI Memory/RAG/Functions APIs and config keys drift across releases — un-pinned image breaks the wiring

**What goes wrong:**
Open WebUI iterates fast; Memory/RAG/Functions behavior and **config-key names change across releases** (e.g. `ENABLE_RAG_WEB_SEARCH` → `WEB_SEARCH_ENABLE`-style renames; embedding-engine keys; the memory system itself was re-architected from passive injection to model-managed tools). villa wires to specific env keys + specific OWUI API endpoints. A floating `:main` tag (or an unpinned re-pull) can rename a key out from under villa's Quadlet env, silently disabling local-only enforcement (re-opening Pitfall 1) or breaking the RAG wiring entirely — with install still reporting green.

**Why it happens:**
The existing OWUI image is already **digest-pinned** (`@sha256:…`), but it's tempting to "just bump to latest" for the new Memory features, or to trust `:main`. Memory/RAG is exactly the surface that churns most.

**How to avoid:**
- Keep the OWUI image **digest-pinned** (`@sha256:`), exactly as villa already does for every image; pin the specific tested version, and treat a version bump as a deliberate, re-validated change (re-run the zero-outbound + RAG smoke UAT), not a silent re-pull. This mirrors the v1.1/v1.2 "pin the digest the A/B proves" discipline.
- Centralize all OWUI memory/RAG env keys in one place (config-driven, behind the orchestrate seam) so a key rename is a single, reviewed edit.
- On a deliberate version bump, re-verify the full env block (offline flags especially) against that version's documented keys — don't assume keys carried over.

**Warning signs:**
After an image bump, doc upload phones home again (offline flag key renamed/ignored); RAG settings reset; OWUI API endpoint villa calls returns 404; goldens/contracts referencing OWUI behavior shift.

**Phase to address:**
The orchestration phase (pin digest + centralize keys) and any future image-bump change (re-validate).

---

## Technical Debt Patterns

Shortcuts that seem reasonable but create long-term problems.

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Leave Open WebUI's **default ChromaDB** vector store instead of standing up Qdrant | No new Quadlet service; fastest path | ChromaDB posts PostHog telemetry unless explicitly killed; weaker scaling/durability story; still leaves the embedding-download leak | Only as a throwaway spike; not for ship — telemetry + the v1.3 "integrate a local vector DB (e.g. Qdrant)" requirement argue against it |
| Rely on `OFFLINE_MODE`/`HF_HUB_OFFLINE` env flags alone for zero-outbound (no active assertion) | One-line "fix"; looks done | Env flag is silent if a key is renamed (Pitfall 8) or PersistentConfig shadows it (Pitfall 3); honesty-by-construction demands a *proof*, not a flag | Never as the sole control — pair with a runtime zero-outbound assertion |
| Route embeddings through the **chat** `llama-server`/`/embeddings` | Reuses an existing managed model; no second service | Couples embedding dimension to the chat model → every `villa model swap` corrupts the vector index (Pitfall 4) | Acceptable ONLY if the embedding model is pinned/decoupled and a chat-swap guard exists |
| Tar the **full vector index** into `villa backup` like the OWUI volume | Trivially reuses BAK-01 plumbing | Archive bloat (GB); merge-on-restore stale-vector leak; dimension-skew corruption | Acceptable only with clean-recreate-before-import + manifest dimension check + size guard |
| Add embedding model footprint as "negligible, ignore in recommend" | Skips touching `Pick()` | OOM / silent CPU fallback of the chat model under import load (Pitfall 2) — a false-green | Never on a shared unified-memory budget |
| Enable autonomous auto-memory by default | Impressive demo | Local model stores false memories injected into every chat; erodes trust | Never default-on with a local model; opt-in only |

## Integration Gotchas

Common mistakes when connecting to external services.

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| Open WebUI embeddings | Trusting that "local LLM" means RAG is local — it isn't; embeddings default to a HF sentence-transformer downloaded at runtime | Force embedding engine/model to the loopback `villa-llama` `/embeddings` OR pre-stage the model + `HF_HUB_OFFLINE=1`/`OFFLINE_MODE=true`; assert zero-outbound |
| Open WebUI config | Editing the Quadlet env and assuming it applies | `PersistentConfig` makes the DB win after first boot — set `ENABLE_PERSISTENT_CONFIG=false` so config.toml-driven env stays authoritative |
| ChromaDB (OWUI default) | Leaving anonymized telemetry on | `ANONYMIZED_TELEMETRY=False` (+ prefer Qdrant); verify no `app.posthog.com` outbound |
| Qdrant volume | Host bind mount under rootless Podman | Named Podman volume (`:U`/`:Z`) so UID mapping + SELinux are handled; generate via `orchestrate` `volume.tmpl` |
| Open WebUI web-search-in-RAG | Assuming RAG never reaches the internet | `ENABLE_RAG_WEB_SEARCH` is off by default AND per-chat toggle — keep it off / unconfigured (defer SearXNG to its deferred milestone); document that enabling it is an outbound-opening choice |
| Open WebUI image | Floating `:main` tag for new Memory features | Digest-pin (`@sha256:`) the validated version; re-validate offline flags on any bump |

## Performance Traps

Patterns that work at small scale but fail as usage grows.

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Re-embedding the whole corpus on every config touch | Long stalls, GTT spike, chat-model eviction | Treat re-embed as an explicit, confirmed destructive op; don't auto-trigger on incidental config saves | Bulk import / embedding-model change on a large doc set |
| Embedding + chat sharing the unified-memory pool | Chat tok/s tanks during import; OOM-kill | Budget embed footprint in `recommend`; ctx≈512 for embed; residency-assert chat survival under RAG load | When corpus/import size grows or a larger embedder is chosen |
| Wrong chunk size (too large → fewer, blurrier matches; too small → fragmented context) | Poor/irrelevant citations; truncated answers | Set a sane default chunk size + overlap; keep it config-driven so it's tunable | As document variety/length grows |
| Unbounded vector volume | Disk/backup bloat; slow restore | Size-guard the volume; consider excluding the derived index from backup (re-embeddable) | Large/long-lived knowledge base |

## Security Mistakes

Domain-specific security issues beyond general web security.

| Mistake | Risk | Prevention |
|---------|------|------------|
| Embedding/reranker/Whisper model auto-download at runtime | Unconsented outbound to HuggingFace after install reports green — breaks PRIV-01/02/03 | `OFFLINE_MODE=true`+`HF_HUB_OFFLINE=1`+`*_AUTO_UPDATE=false`, pre-staged model, runtime zero-outbound assertion |
| ChromaDB / Scarf / version-check telemetry | Outbound to `app.posthog.com` / analytics endpoints | `ANONYMIZED_TELEMETRY=False`, `DO_NOT_TRACK=true`, `SCARF_NO_ANALYTICS=true`, `OFFLINE_MODE` disables version checks; prefer Qdrant |
| Treating "memory stores PII" as a telemetry/counts problem | Mis-scoping: memory MUST store content (that's the feature) — the real rule is content NEVER LEAVES the box | Zero-outbound is the control, not no-content; user-controllable edit/delete; backup archive stays local-only (it already is) |
| New Qdrant service bound beyond loopback | Exposes the vector store / knowledge base to the LAN | Bind Qdrant to the `villa.network` container-DNS only / loopback; reuse PRIV-01 loopback discipline; never `0.0.0.0` |
| Web-search-in-RAG silently enabled | Sends user query text to an external search engine | Keep `ENABLE_RAG_WEB_SEARCH` off; if ever added, it's an explicit opt-in + (deferred) local SearXNG only |

## UX Pitfalls

Common user experience mistakes in this domain.

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Auto-memory on by default with a local model | Assistant confidently repeats hallucinated "facts" forever | Manual save/pin/edit/delete as primary; auto-extraction opt-in |
| No visible way to inspect/delete memories | Users can't correct wrong memories; lose trust | Surface Settings → Personalization → Memory; UAT must cover edit/delete |
| Silent stale index after a model swap | RAG citations suddenly irrelevant, no explanation | Make swap memory-aware; if embedder changes, prompt for explicit re-index |
| Knowledge base vanishes after reboot | User re-uploads everything, re-pays re-embed cost | Durable named volume + lingering + boot-survival UAT |
| First doc upload hangs (silent HF download) | Looks broken on a privacy-conscious/firewalled host | Pre-stage the embedding model at install; offline flags set |

## "Looks Done But Isn't" Checklist

Things that appear complete but are missing critical pieces.

- [ ] **RAG/embeddings:** Often missing the *runtime* zero-outbound check — verify a doc upload on a firewalled host does NOT reach `huggingface.co`/`app.posthog.com` (install-time green is insufficient).
- [ ] **Offline flags:** Often missing one of the set — verify `OFFLINE_MODE`, `HF_HUB_OFFLINE`, all three `*_AUTO_UPDATE=false`, `ANONYMIZED_TELEMETRY=False` are ALL present AND not shadowed by `PersistentConfig`.
- [ ] **Config authority:** Often missing `ENABLE_PERSISTENT_CONFIG=false` — verify a config.toml-driven env change actually takes effect after the OWUI DB is populated (second-change test).
- [ ] **Recommend math:** Often missing the embedding footprint — verify `Pick()` fits chat + embed + headroom, and that a bulk import doesn't evict the chat model (residency proof holds).
- [ ] **Model swap:** Often missing memory-awareness — verify `villa model swap` / `backend set` leaves the embedding model + vector collections intact (no dimension corruption).
- [ ] **Vector volume:** Often missing boot-survival — verify the knowledge base persists across a reboot and that Qdrant has write permission (rootless UID + SELinux `:Z`).
- [ ] **Backup/restore:** Often missing clean-recreate-before-import + dimension manifest — verify a restore onto a non-empty / different-embedder store does NOT leak stale or mismatched vectors.
- [ ] **Memory edit/delete:** Often missing the correction loop — verify a deleted/edited memory stops being injected into new chats.
- [ ] **Loopback:** Often missing for the *new* Qdrant service — verify it's not bound to `0.0.0.0`/LAN.

## Recovery Strategies

When pitfalls occur despite prevention, how to recover.

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Runtime HF/telemetry leak shipped | MEDIUM | Add the full offline env block + `ENABLE_PERSISTENT_CONFIG=false`, wipe/repopulate the OWUI config rows, pre-stage the model, re-run zero-outbound UAT |
| Dimension-corrupted collection after swap | MEDIUM | Reset/clean-recreate the collection, re-embed all docs with the recorded embedding model; add the chat-swap guard so it can't recur |
| Chat model OOM/CPU-fallback under RAG load | LOW–MEDIUM | Reduce embed ctx to ~512, shrink chat quant/ctx via `recommend`, re-fit the envelope including embed term |
| Vector volume lost on reboot | HIGH (user re-uploads) | Re-create as named volume + enable lingering; re-import source docs and re-embed; add boot-survival test |
| Stale vectors leaked on restore | MEDIUM | Clean-recreate the Qdrant volume, restore source data, re-embed; adopt clean-recreate-before-import + manifest dimension check |
| Memory bank polluted by false auto-memories | LOW | User clears/edits memory bank (Settings → Personalization → Memory); switch auto-extraction to opt-in |

## Pitfall-to-Phase Mapping

How roadmap phases should address these pitfalls. (Phase names are indicative; the roadmapper assigns final numbers. Suggested ordering: vector-DB+volume orchestration → embeddings backend + offline enforcement + recommend math → memory/RAG wiring + capture → swap-integration → status/doctor surfacing → backup/restore extension.)

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| 1. Runtime HF/embedding download (zero-outbound) | Embeddings-backend + offline-enforcement phase | Firewalled doc-upload smoke test reaches no external host; status zero-outbound assertion |
| 2. Embedding footprint OOM / CPU fallback | Recommend/envelope phase (extend `Pick()`) | `Pick()` includes embed term; chat residency proof survives a bulk import |
| 3. PersistentConfig shadows config.toml | OWUI memory-wiring phase | `ENABLE_PERSISTENT_CONFIG=false`; second-change config test takes effect; config-drift check |
| 4. Embedding/chat swap dimension mismatch | Swap-integration + embedding-identity phase | Chat swap leaves vectors intact; dimension recorded; explicit re-index path |
| 5. Vector volume durability / rootless perms | Qdrant-orchestration phase | Named volume `:Z`/`:U`; boot-survival UAT; Qdrant writable |
| 6. Backup stale-vector leak / bloat | Backup/restore-extension phase | Clean-recreate-before-import; manifest dimension skew-warn; size/inclusion decision |
| 7. False auto-memories, not correctable | Memory-capture phase | Manual save/edit/delete primary; auto-extraction opt-in; edit/delete UAT |
| 8. OWUI version/key drift | Orchestration + any image bump | Digest-pinned `@sha256:`; centralized keys; offline flags re-validated on bump |

## Sources

- [Open WebUI — Offline Mode](https://docs.openwebui.com/tutorials/maintenance/offline-mode/) (HIGH — official; `OFFLINE_MODE`, `HF_HUB_OFFLINE`, `*_AUTO_UPDATE=false`, features that still phone home)
- [Open WebUI — RAG Troubleshooting](https://docs.openwebui.com/troubleshooting/rag/) (HIGH — official; default embedding model auto-download, local model path)
- [Open WebUI Discussion #9729 — "always wants to connect to huggingface"](https://github.com/open-webui/open-webui/discussions/9729) (HIGH — corroborates runtime HF download)
- [Open WebUI Issue #15613 — ChromaDB PostHog telemetry](https://github.com/open-webui/open-webui/issues/15613) + [PR #618 — disable Chroma telemetry](https://github.com/open-webui/open-webui/pull/618) (HIGH — ChromaDB outbound to PostHog; `ANONYMIZED_TELEMETRY`)
- [Open WebUI — PersistentConfig system (DeepWiki)](https://deepwiki.com/open-webui/open-webui/12.2-persistentconfig-system) + [Env Configuration docs](https://docs.openwebui.com/reference/env-configuration/) (HIGH — DB shadows env after first boot; `ENABLE_PERSISTENT_CONFIG`)
- [Open WebUI Issue #11279 — embedding dimension 768 vs collection 384](https://github.com/open-webui/open-webui/issues/11279) + [Discussion #9609 — Qdrant dimension mismatch](https://github.com/open-webui/open-webui/discussions/9609) (HIGH — model-swap dimension corruption + re-index requirement)
- [Open WebUI — Memory & Personalization](https://docs.openwebui.com/features/chat-conversations/memory/) (HIGH — manual vs autonomous memory, local DB, edit/delete, model-quality dependence)
- [Open WebUI Discussion #11597 — external Qdrant `VECTOR_DB=qdrant`, `QDRANT_URI`](https://github.com/open-webui/open-webui/discussions/11597) + [Discussion #8628](https://github.com/open-webui/open-webui/discussions/8628) (HIGH — Qdrant wiring + persistent volumes)
- [Fix Podman Volume Permission Issues with SELinux](https://oneuptime.com/blog/post/2026-03-18-fix-podman-volume-permission-issues-selinux/view) + [Rootless Podman volumes](https://www.tutorialworks.com/podman-rootless-volumes/) (HIGH — rootless UID mapping, `:Z`/`:U`, named volumes)
- [Open WebUI — SearXNG / web-search-in-RAG](https://docs.openwebui.com/features/web-search/searxng/) (HIGH — `ENABLE_RAG_WEB_SEARCH` off by default, per-chat toggle, external query)
- [llama.cpp server README — `/embeddings`](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) + [Running multiple local models: memory management](https://www.sitepoint.com/multiple-local-models-memory-management/) (HIGH/MEDIUM — local embeddings endpoint; shared-memory budgeting, ctx≈512)
- VillaStraylight `.planning/PROJECT.md` + `CLAUDE.md` (project non-negotiables: zero-outbound, CGO-free, config-as-truth, offload-asserting, digest-pinning, v1.2 clean-recreate-before-import lesson)

---
*Pitfalls research for: local memory/RAG addition to VillaStraylight (v1.3)*
*Researched: 2026-06-09*
