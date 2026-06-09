# Roadmap: VillaStraylight

## Overview

VillaStraylight is a control plane + managed workload: a single Go binary that inspects an AMD Strix Halo (gfx1151) Fedora host, recommends a hardware-fitting model/quant/context, generates rootless Podman Quadlet units, and brings up llama.cpp inference + Open WebUI chat + a read-only control dashboard — with zero data leaving the box. The north star throughout is "runs healthy after install" — every phase ends in something runnable and verifiable on the real hardware. v1.2 (Operability) extended that bar to "and stays operable, recoverable, and measurable over time." v1.3 (Memory & Knowledge) extends it again to "and remembers the user and their documents across chats — strictly local."

## Milestones

- ✅ **v1.0 MVP** — Phases 1–5 (shipped 2026-06-05, tag `v1.0`)
- ✅ **v1.1 ROCm Opt-In Backend** — Phases 6–11 (shipped 2026-06-06, tag `v1.1`)
- ✅ **v1.2 Operability** — Phases 12–17 (shipped 2026-06-08, tag `v1.2`)
- ⏳ **v1.3 Memory & Knowledge (local RAG)** — Phases 18–23 (active)

Full per-phase detail for shipped milestones is archived under `.planning/milestones/`:

- `milestones/v1.0-ROADMAP.md` · `milestones/v1.0-REQUIREMENTS.md`
- `milestones/v1.1-ROADMAP.md` · `milestones/v1.1-REQUIREMENTS.md` · `milestones/v1.1-MILESTONE-AUDIT.md`
- `milestones/v1.2-ROADMAP.md` · `milestones/v1.2-REQUIREMENTS.md` · `milestones/v1.2-MILESTONE-AUDIT.md`

## Phases

<details>
<summary>✅ v1.0 MVP (Phases 1–5) — SHIPPED 2026-06-05</summary>

- [x] Phase 1: Hardware Foundation & Preflight Gate (3/3 plans) — completed 2026-06-03
- [x] Phase 2: GPU-Validated Inference Slice (3/3 plans) — completed 2026-06-04
- [x] Phase 3: Orchestrated Install & Lifecycle (6/6 plans) — completed 2026-06-04
- [x] Phase 4: Chat Integration (3/3 plans) — completed 2026-06-05
- [x] Phase 5: Control Dashboard (8/8 plans) — completed 2026-06-05

</details>

<details>
<summary>✅ v1.1 ROCm Opt-In Backend (Phases 6–11) — SHIPPED 2026-06-06</summary>

**Milestone goal:** Add an opt-in ROCm/HIP inference backend for higher throughput on AMD Strix Halo (gfx1151), gated hard enough to preserve the v1.0 "just works" bar, switchable on a running install with transactional rollback, and benchmarked honestly to prove the per-model win — while Vulkan RADV remains the safe default.

- [x] Phase 6: ROCm Backend + Resolver Spine (3/3 plans) — completed 2026-06-05
- [x] Phase 7: ROCm Render Unit + Preflight/Detect (3/3 plans) — completed 2026-06-06
- [x] Phase 8: `villa backend set` Switch Verb + Rollback (2/2 plans) — completed 2026-06-06 (4/4 on-hardware UAT)
- [x] Phase 9: `villa bench` (Honest A/B) (3/3 plans) — completed 2026-06-06 (3/3 on-hardware UAT; Δpp +4.84 / Δtg −11.15)
- [x] Phase 10: Backend + tok/s Surfacing (3/3 plans) — completed 2026-06-06
- [x] Phase 11: Address v1.1 tech debt — rocm_readiness detect probes + doc reconciliation (2/2 plans) — completed 2026-06-06

See `milestones/v1.1-ROADMAP.md` for full phase detail, success criteria, and plan breakdowns.

</details>

<details>
<summary>✅ v1.2 Operability (Phases 12–17) — SHIPPED 2026-06-08</summary>

**Milestone goal:** Harden VillaStraylight into an operable, recoverable daily-driver — self-diagnosis, backup/restore, comparative benchmarking, usage history, a guided install, and a TG-tuned ROCm option — without weakening the v1.0 "just works" bar or the strictly-local / zero-telemetry posture.

**Build order was research-converged:** seam-locked + composition features first, then the two persistence features with their byte-frozen evolutions staggered so only one byte-frozen contract evolved at a time, then the destructive backup, then the TUI capstone over the finished surface.

- [x] Phase 12: `rocm-6.4.4` Alternate Backend (3/3 plans) — completed 2026-06-07 (capability shipped; honest A/B disproved the perf premise — Vulkan stays tg default)
- [x] Phase 13: `villa doctor` Health Diagnosis (3/3 plans) — completed 2026-06-07 (on-hardware UAT 3/3)
- [x] Phase 14: Saved Bench Reports + `--compare` (3/3 plans) — completed 2026-06-07 (UAT PASS; Δtg +10.39 vulkan>rocm)
- [x] Phase 15: Cumulative Usage Tracking (4/4 plans) — completed 2026-06-07
- [x] Phase 16: Backup / Restore (3/3 plans) — completed 2026-06-07 (UAT 4/5 + 1 documented cross-host limitation)
- [x] Phase 17: Guided TUI Install (Capstone) (3/3 plans) — completed 2026-06-08 (on-hardware UAT 3/3)

Audit PASSED — 13/13 requirements, 5/5 integration flows, 6/6 phases Nyquist-compliant. See `milestones/v1.2-ROADMAP.md` for full phase detail, success criteria, and plan breakdowns.

</details>

### ⏳ v1.3 Memory & Knowledge (local RAG) — Phases 18–23 (ACTIVE)

**Milestone goal:** Give VillaStraylight a strictly-local memory system so the assistant remembers the user across chats and can recall past conversations and uploaded documents — **integrate + orchestrate, never rebuild** (Go stays control-plane only). Two new digest-pinned managed-service Quadlet units (Qdrant + a dedicated embeddings llama-server) are wired into Open WebUI's native Memory/RAG by ENV only; `villa` recommends, installs, gates, surfaces, and backs up the new stack — with zero new outbound.

**Build order is research-converged (four researchers + synthesizer agreed) — preserve it.** This is an INTEGRATION milestone: **zero new first-party Go libraries**. The two new image literals live behind the `orchestrate` managed-service seam (the same category as `openWebUIImage`), NOT behind the inference `BackendFor` / `TestSeamGrepGate` scope. Dependencies are strict: **Qdrant + embeddings must exist before the Open WebUI env wiring; wiring before the recall indexer and before surfacing/backup. Surfacing + backup + memory-aware swap land LAST** (mirrors v1.x discipline — surface/back-up after the thing exists; exactly one byte-frozen contract evolves, append-only + schema bump 2→3, golden re-frozen once).

- [x] **Phase 18: Memory Spine — config core + embeddings/wiring research spike** — Land the `internal/memory` pure core + `config.toml` memory fields (the spine touched by render/recommend/preflight), and de-risk the version-sensitive integration by deciding the embeddings runtime, re-verifying the Open WebUI env contract against the pinned OWUI digest, and confirming the embedding model + footprint. (completed 2026-06-09)
- [x] **Phase 19: Vector Store + Local Embeddings Services** — Render the two new rootless Quadlet managed services (Qdrant + a dedicated embeddings llama-server) onto `villa.network`, container-DNS only, with a durable named `:Z` volume and the embedding model pre-staged at install (zero runtime download). (completed 2026-06-09)
- [ ] **Phase 20: Open WebUI Memory/RAG Wiring + Offline Lockdown** — Wire Open WebUI (env-only) to Qdrant + the local embeddings endpoint with config-authoritative `ENABLE_PERSISTENT_CONFIG=false` and full offline/telemetry lockdown, delivering personalized memory (both capture modes + edit/delete) and the document knowledge base with citations — proven by a runtime firewalled zero-outbound smoke test.
- [ ] **Phase 21: Conversational Recall Indexer** — A `villa`-orchestrated, strictly-local indexer that semantically indexes past chats into Knowledge so the assistant retrieves relevant past-chat content by meaning, with `villa`-controllable incremental re-index that reports honest state (no silent staleness).
- [ ] **Phase 22: Control-Plane Fit + Host Gate** — Make `recommend` reserve the embedding footprint before the chat-model fit, and gate host fitness for the memory stack in `preflight` + `doctor` (vector disk, embedder headroom, offload-asserting residency under embedding load) — so the memory stack "runs healthy after install."
- [ ] **Phase 23: Surfacing, Backup & Memory-Aware Swap** — Land the milestone's single byte-frozen contract evolution LAST: surface memory-stack health in `status` + dashboard (schema bump 2→3, golden re-frozen once), extend `backup`/`restore` to the Qdrant volume (clean-recreate-before-import, dimension in manifest), and make `villa model swap` memory-aware (guard the embedding-dimension-invalidates-vectors hazard).

## Phase Details

### Phase 18: Memory Spine — config core + embeddings/wiring research spike

**Goal**: `villa` has a pure memory-decision core and config fields that make the memory stack opt-in and config-driven, and the version-sensitive integration choices (embeddings runtime, exact Open WebUI env keys, embedding model + footprint) are resolved and pinned before any unit or env block is frozen.
**Depends on**: Nothing (first v1.3 phase; builds on the shipped v1.2 control plane)
**Requirements**: INFRA-04
**Success Criteria** (what must be TRUE):

  1. A user can set `memory_enabled` (plus embedding model / service ports/addrs) in `config.toml`, and an existing v1.2 install stays byte-identical until they opt in (default `memory_enabled=false`, self-healing/defaulted fields).
  2. The new `internal/memory` pure core computes the memory-stack decisions (embedding footprint, enablement-and-fields-valid gate, render-view inputs) with no host I/O — it imports neither `os/exec` nor a container image literal, and the seam gate stays green.
  3. The embeddings runtime decision (dedicated `villa-embed` llama-server vs OWUI built-in), the exact Open WebUI RAG/Memory env keys (re-verified against the pinned OWUI digest), and the pinned embedding model + its memory footprint are recorded as decisions the later phases build on.

**Plans**: 2 plansPlans:
**Wave 1**

- [x] 18-01-PLAN.md — config memory fields (default-off, self-healing, byte-identical) + recorded spike decisions D-07/D-08/D-09 (SC#1, SC#3)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 18-02-PLAN.md — new pure `internal/memory` core: Footprint / Decide / RenderView triad; seam gate stays green (SC#2)

### Phase 19: Vector Store + Local Embeddings Services

**Goal**: `villa install` brings up a local Qdrant vector DB and a local OpenAI-compatible embeddings endpoint as rootless Podman Quadlet managed services on `villa.network`, reachable by container DNS only, with durable storage and the embedding model pre-staged so nothing is downloaded at runtime.
**Depends on**: Phase 18 (config flag + pure core spine)
**Requirements**: INFRA-01, INFRA-02, PRIV-04
**Success Criteria** (what must be TRUE):

  1. With `memory_enabled=true`, `villa install` renders and starts `villa-qdrant` (digest-pinned image, named `:Z` volume, no published host port) and a dedicated embeddings `llama-server` exposing `/v1/embeddings` (reusing the pinned toolbox image, container-DNS only) — both regenerated from config, never hand-edited.
  2. The Qdrant knowledge store survives a reboot (durable named volume + lingering) and Qdrant is writable (no rootless UID / SELinux `:Z` permission failure).
  3. The embedding model is present and served from the local stack at install time — a first embedding request succeeds with no internet access (no runtime HuggingFace/model pull).
  4. Neither new service is bound beyond loopback / `villa.network` — the loopback-only privacy audit stays green.

**Plans**: 3 plansPlans:
**Wave 1**

- [x] 19-01-PLAN.md — orchestrate managed-service render path: Qdrant + villa-embed units + Qdrant `:Z` volume, conditional on `memory_enabled` (byte-identical when off), three goldens, mandatory seam-gate allowlist extension (INFRA-01, INFRA-02)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 19-02-PLAN.md — install wiring: pre-stage the nomic Q8_0 GGUF (zero runtime download) + start villa-qdrant/villa-embed + offline 768-dim `/v1/embeddings` + Qdrant-writable readiness proof (INFRA-02, PRIV-04)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 19-03-PLAN.md — on-hardware freeze: confirm the Qdrant dev-box RepoDigest + curl `/v1/embeddings` against the pinned kyuz0 digest (legitimacy gate), prove Qdrant writability + reboot survival + offline first embedding (INFRA-01, INFRA-02, PRIV-04)

### Phase 20: Open WebUI Memory/RAG Wiring + Offline Lockdown

**Goal**: Open WebUI's native Memory and RAG are wired (env-only, behind the orchestrate seam) to Qdrant and the local embeddings endpoint, config stays the single source of truth, and the assistant can remember user facts and answer from uploaded documents with citations — all strictly local, proven by a runtime zero-outbound test.
**Depends on**: Phase 19 (Qdrant + embeddings DNS targets must exist)
**Requirements**: INFRA-03, MEM-01, MEM-02, MEM-03, MEM-04, KB-01, KB-02, KB-03, PRIV-05
**Success Criteria** (what must be TRUE):

  1. The assistant remembers a user-stated fact across separate chats and injects it into a later conversation; the user can explicitly save a message/fact, and can view/edit/delete stored memories so a deleted memory stops being injected.
  2. Automatic LLM-assisted memory extraction is available and toggleable on/off (opt-in, not silently default-on given the local-model quality caveat).
  3. A user can upload a document into a local knowledge collection and the assistant answers using the retrieved content with visible citations — chunking, embedding, and retrieval run entirely through the local embeddings + Qdrant path (no cloud API, no runtime model download).
  4. `ENABLE_PERSISTENT_CONFIG=false` plus the full offline/telemetry lockdown (`OFFLINE_MODE`, `HF_HUB_OFFLINE`, `*_AUTO_UPDATE=false`, `ANONYMIZED_TELEMETRY=False`) are set so a config.toml-driven change actually takes effect after the OWUI DB is populated, and a **runtime firewalled document-upload smoke test reaches no external host** (not just install-time green).

**Plans**: 3 plans
Plans:
- [ ] 20-01-PLAN.md — Parameterize buildOpenWebUIView: append the D-09 RAG/Qdrant/memory env block (incl. ENABLE_PERSISTENT_CONFIG=False) only when memory_enabled; new memory-on golden + memory-aware telemetry test (INFRA-03)
- [ ] 20-02-PLAN.md — Runtime zero-outbound RAG smoke harness: pure evalRagSmoke core + liveRagSmoke seam + `villa verify memory` subcommand, negative-control-first (KB-01/02/03, MEM-03, PRIV-05)
- [ ] 20-03-PLAN.md — On-hardware verification: live OWUI Memory (MEM-01/02/04), auto-extraction default-off (MEM-03), document upload + citations (KB-01/02/03), firewalled zero-outbound proof with negative control (PRIV-05); docs/MEMORY.md
**UI hint**: yes

### Phase 21: Conversational Recall Indexer

**Goal**: A `villa`-orchestrated, strictly-local indexer turns the user's past chat history into a searchable Knowledge collection so the assistant can recall relevant past conversations by meaning, and the index can be kept current under explicit `villa` control with honest staleness reporting.
**Depends on**: Phase 20 (Open WebUI RAG/Knowledge + embeddings + Qdrant wiring must be live)
**Requirements**: RECALL-01, RECALL-02, RECALL-03
**Success Criteria** (what must be TRUE):

  1. A `villa` command semantically indexes past conversations into the vector store (chats → Knowledge), running entirely locally (no new outbound).
  2. In a new chat, the assistant retrieves relevant past-chat content by meaning (semantic, not just keyword) into the current conversation's context.
  3. The chat index can be incrementally updated / re-indexed under explicit `villa` control as conversations grow, and `villa` reports the index's honest current state (indexed count / last-indexed / staleness) — never silently stale.

**Plans**: TBD

### Phase 22: Control-Plane Fit + Host Gate

**Goal**: The recommended configuration accounts for the embedding model so the memory stack fits the unified-memory envelope, and `villa` refuses or warns up front when the host can't host the memory stack — preserving the "runs healthy after install" bar with no OOM or silent CPU fallback under embedding load.
**Depends on**: Phase 19 (services to gate/fit) and Phase 18 (footprint function); composes cleanly with Phase 20's running stack
**Requirements**: CTRL-01, CTRL-03, CTRL-06
**Success Criteria** (what must be TRUE):

  1. `villa recommend` reserves the embedding-model footprint in the unified-memory fit math *before* the chat-model fit (envelope shrinks first), so the recommended config never OOMs or silently CPU-falls-back on gfx1151 — surfaced as an append-only `recommend` field + schema bump.
  2. `villa preflight` gates host fitness for the memory stack (disk space for the vector index, memory headroom for the embedder) with refuse-with-remediation.
  3. `villa doctor` folds memory-stack health into its existing PASS/WARN/FAIL exit contract: services up, vector-disk/headroom checks, and an offload-asserting residency proof that the chat model survives an embedding/import workload (a silent/partial CPU fallback under embedding load is a FAIL, never a false-green).

**Plans**: TBD

### Phase 23: Surfacing, Backup & Memory-Aware Swap

**Goal**: The memory stack becomes observable, recoverable, and swap-safe — health rows appear in `status` + the dashboard, `backup`/`restore` cover the Qdrant volume safely, and `villa model swap` guards the dimension-mismatch hazard — landing the milestone's single byte-frozen contract evolution exactly once.
**Depends on**: Phases 19 + 20 (the services and the volume must exist before they can be surfaced or backed up)
**Requirements**: CTRL-02, CTRL-04, CTRL-05
**Success Criteria** (what must be TRUE):

  1. `villa status` and the control dashboard show memory-stack health (Qdrant + embeddings service rows, active embedding model) as an append-only, schema-bumped contract change (`reportSchemaVersion` 2→3, golden re-frozen once); non-GPU services fold health/active into the verdict without a spurious offload PASS/FAIL.
  2. `villa backup` includes the Qdrant memory volume and `villa restore` clean-recreates it before import (no stale-vector leak), with the embedding dimension recorded in the manifest so a version/dimension skew WARNs rather than silently corrupting retrieval.
  3. `villa model swap` is memory-aware: it warns/guards when changing the embedding model would invalidate existing vectors (dimension mismatch / no auto-reindex), and a chat-model swap leaves the embedding model and vector collections intact.

**Plans**: TBD

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Hardware Foundation & Preflight Gate | v1.0 | 3/3 | Complete | 2026-06-03 |
| 2. GPU-Validated Inference Slice | v1.0 | 3/3 | Complete | 2026-06-04 |
| 3. Orchestrated Install & Lifecycle | v1.0 | 6/6 | Complete | 2026-06-04 |
| 4. Chat Integration | v1.0 | 3/3 | Complete | 2026-06-05 |
| 5. Control Dashboard | v1.0 | 8/8 | Complete | 2026-06-05 |
| 6. ROCm Backend + Resolver Spine | v1.1 | 3/3 | Complete | 2026-06-05 |
| 7. ROCm Render Unit + Preflight/Detect | v1.1 | 3/3 | Complete | 2026-06-06 |
| 8. `villa backend set` Switch Verb + Rollback | v1.1 | 2/2 | Complete | 2026-06-06 |
| 9. `villa bench` (Honest A/B) | v1.1 | 3/3 | Complete | 2026-06-06 |
| 10. Backend + tok/s Surfacing | v1.1 | 3/3 | Complete | 2026-06-06 |
| 11. Address v1.1 tech debt | v1.1 | 2/2 | Complete | 2026-06-06 |
| 12. `rocm-6.4.4` Alternate Backend | v1.2 | 3/3 | Complete | 2026-06-07 |
| 13. `villa doctor` Health Diagnosis | v1.2 | 3/3 | Complete | 2026-06-07 |
| 14. Saved Bench Reports + `--compare` | v1.2 | 3/3 | Complete | 2026-06-07 |
| 15. Cumulative Usage Tracking | v1.2 | 4/4 | Complete | 2026-06-07 |
| 16. Backup / Restore | v1.2 | 3/3 | Complete | 2026-06-07 |
| 17. Guided TUI Install | v1.2 | 3/3 | Complete | 2026-06-08 |
| 18. Memory Spine — config core + research spike | v1.3 | 2/2 | Complete    | 2026-06-09 |
| 19. Vector Store + Local Embeddings Services | v1.3 | 3/3 | Complete    | 2026-06-09 |
| 20. Open WebUI Memory/RAG Wiring + Offline Lockdown | v1.3 | 0/? | Not started | - |
| 21. Conversational Recall Indexer | v1.3 | 0/? | Not started | - |
| 22. Control-Plane Fit + Host Gate | v1.3 | 0/? | Not started | - |
| 23. Surfacing, Backup & Memory-Aware Swap | v1.3 | 0/? | Not started | - |
