---
gsd_state_version: 1.0
milestone: v1.3
milestone_name: Memory & Knowledge
status: executing
stopped_at: Phase 20 Wave 1 complete (20-01, 20-02); Plan 20-03 Task 1 (docs/MEMORY.md) done — paused at on-hardware checkpoint Tasks 2-5 (gfx1151 required)
last_updated: "2026-06-09T21:10:40.557Z"
last_activity: 2026-06-09 -- Phase 20 execution started
progress:
  total_phases: 6
  completed_phases: 2
  total_plans: 8
  completed_plans: 7
  percent: 33
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-09 — started v1.3 Memory & Knowledge)

**Core value:** Run a capable local AI workspace that "just works" after install — hardware-aware setup that brings inference, chat, and the dashboard up healthy, with zero data leaving the box. v1.2 extended the bar to "and stays operable, recoverable, and measurable over time." v1.3 extends it to "and remembers the user and their documents across chats — strictly local."
**Current focus:** Phase 20 — open-webui-memory-rag-wiring-offline-lockdown

## Current Position

Phase: 20 (open-webui-memory-rag-wiring-offline-lockdown) — EXECUTING
Plan: 3 of 3
Status: Ready to execute
Last activity: 2026-06-09 -- Phase 20 execution started

## v1.3 Build Order (research-converged — preserve)

INTEGRATION milestone: **zero new first-party Go libraries**. Two new digest-pinned managed-service
Quadlet units (Qdrant `v1.18.2-unprivileged` + a dedicated embeddings llama-server) wired into Open WebUI
by ENV only. New image literals live behind the `orchestrate` managed-service seam (same category as
`openWebUIImage`), NOT behind the inference `BackendFor` / `TestSeamGrepGate` scope. Dependencies are
strict: Qdrant + embeddings BEFORE the OWUI env wiring; wiring BEFORE the recall indexer and BEFORE
surfacing/backup. Surfacing + backup + memory-aware swap land LAST (exactly ONE byte-frozen contract
evolution: `status.Report` 2→3, golden re-frozen once).

1. **Phase 18 — Memory Spine (INFRA-04)**: `internal/memory` pure core + `config.toml` memory fields (the spine touched by render/recommend/preflight). Low-code research spike folded in — decide embeddings runtime (dedicated `villa-embed` vs OWUI built-in), re-verify the version-sensitive OWUI env contract against the pinned OWUI digest, confirm embedding model + footprint. De-risks the env golden re-freeze.
2. **Phase 19 — Vector Store + Embeddings Services (INFRA-01, INFRA-02, PRIV-04)**: render the two new rootless Quadlet managed services + named `:Z` volume on `villa.network`, container-DNS only; embedding model pre-staged at install (zero runtime download). Reuse the Phase-4 OWUI volume discipline (boot-survival UAT).
3. **Phase 20 — OWUI Memory/RAG Wiring + Offline Lockdown (INFRA-03, MEM-01..04, KB-01..03, PRIV-05)**: env-only wiring; `ENABLE_PERSISTENT_CONFIG=false` + full offline/telemetry lockdown (`OFFLINE_MODE`/`HF_HUB_OFFLINE`/`*_AUTO_UPDATE=false`/`ANONYMIZED_TELEMETRY=False`); both capture modes + edit/delete; doc KB with citations; **runtime firewalled zero-outbound smoke test** (not just install-time green).
4. **Phase 21 — Conversational Recall Indexer (RECALL-01..03)**: net-new villa behavior; the milestone's biggest single phase (user explicitly chose FULL scope). chats → Knowledge semantic indexer; villa-controllable incremental re-index; honest staleness state (no silent staleness).
5. **Phase 22 — Control-Plane Fit + Host Gate (CTRL-01, CTRL-03, CTRL-06)**: `recommend` reserves the embedding footprint BEFORE the chat-model fit (append-only field + schema bump); `preflight` vector-disk/headroom gate (refuse-with-remediation); `doctor` folds memory checks incl. offload-asserting residency under embedding load (silent/partial CPU fallback = FAIL, no false-green).
6. **Phase 23 — Surfacing, Backup & Memory-Aware Swap (CTRL-02, CTRL-04, CTRL-05)**: LAST. `status` + dashboard memory rows (`reportSchemaVersion` 2→3, golden re-frozen ONCE; non-GPU N/A-offload pattern); `backup`/`restore` extend to the Qdrant volume (clean-recreate-before-import, dimension in manifest, skew-WARN); `villa model swap` memory-aware (embedding-dimension guard).

## Research Flags (deeper planning research likely)

- **Phase 18 (Memory Spine / spike):** the exact Open WebUI RAG/Memory env keys (`VECTOR_DB`, `QDRANT_URI`, `RAG_EMBEDDING_ENGINE`, `RAG_OPENAI_API_BASE_URL`, `RAG_EMBEDDING_MODEL`, the offline/persistent-config flags) are **version-sensitive (MEDIUM-confidence)** — pin the OWUI digest the milestone ships against and re-verify the names from THAT image's docs before freezing the golden (`TestRenderOpenWebUITelemetryFrozen` forces the re-audit). Confirm the embedding model + footprint and the chats→Knowledge indexer approach.
- **Phase 19 (Services):** Qdrant rootless-Podman volume mechanics — named `:Z`/`:U` volume (never host bind mount), boot-survival, writable store. Re-verify the Qdrant image digest at impl time. Pre-stage the embedding GGUF into a persistent volume during install (the only sanctioned outbound window).
- **Phase 20 (Wiring):** PersistentConfig precedence (DB shadows env after first boot unless `ENABLE_PERSISTENT_CONFIG=false`) + the FULL offline flag set + the runtime firewalled zero-outbound smoke test are the load-bearing honesty gates.
- **Phase 22 (Fit/Gate):** constrain embed ctx ≈ 512 (chunk size) to slash embed KV cost; the residency proof must assert the CHAT model survives an embed/import workload (re-use `ResidencyProof`; partial fallback = FAIL).
- **Phase 23 (Backup/swap):** clean-recreate-before-import for the Qdrant volume (podman import MERGES + does not auto-create — v1.2 BAK lesson); record embedding model name + dimension in the manifest for skew-WARN; decide whether to include the derived vector index or re-embed on restore (size guard, mirroring the weights-excluded judgment).

## Performance Metrics

**Velocity:**

- Total plans completed: 52 (across v1.0 + v1.1; v1.2 added 19 more)
- Average duration: 34 min
- Total execution time: 2.2 hours

**By Phase (shipped):**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 01 | 3 | - | - |
| 02 | 3 | - | - |
| 04 | 3 | - | - |
| 06 | 3 | - | - |
| 07 | 3 | - | - |
| 11 | 2 | - | - |
| 12 | 3 | - | - |
| 13 | 3 | - | - |
| 14 | 3 | - | - |
| 15 | 4 | - | - |
| 16 | 3 | - | - |
| 17 | 3 | - | - |
| 18 | 2 | - | - |
| 19 | 3 | - | - |

**Recent Trend:**

- Last 5 plans: —
- Trend: —

*Updated after each plan completion*
| Phase 12 P01 | 15 | 3 tasks | 6 files |
| Phase 12 P02 | ~20 min | 2 tasks | 11 files |
| Phase 12 P03 | ~20 min | 3 of 4 tasks (Task 4 on-hardware pending) | 4 files |
| Phase 13 P01 | 3min | 2 tasks | 2 files |
| Phase 13 P02 | 8min | 2 tasks | 6 files |
| Phase 13 P03 | ~38min | 3 tasks (TDD) | 5 files |
| Phase 14 P01 | 4min | 2 tasks (TDD) | 3 files |
| Phase 15 P01 | 2 min | 2 tasks | 2 files |
| Phase 15 P02 | 2m | 1 tasks | 3 files |
| Phase 15 P03 | 6 min | 2 tasks | 4 files |
| Phase 15 P04 | ~12 min | 2 tasks | 5 files |
| Phase 16 P01 | 6m | 2 tasks | 14 files |
| Phase 16 P02 | ~12m | 2 tasks | 13 files |
| Phase 16 P03 | ~22m | 2 tasks | 7 files |
| Phase 17 P01 | 14min | 2 tasks | 6 files |
| Phase 17 P02 | 22min | 2 tasks | 3 files |
| Phase 17 P03 | 18min | 2 tasks | 2 files |
| Phase 18 P01 | 4min | 3 tasks | 3 files |
| Phase 18 P02 | ~7 min | 3 tasks (TDD) | 3 files |
| Phase 19 P01 | ~14 min | 3 tasks | 10 files |
| Phase 19 P02 | 25min | 2 tasks | 3 files |
| Phase 19 P03 | ~10min | 3 tasks | 0 files |
| Phase 20 P01 | 4 min | 3 tasks | 4 files |
| Phase 20 P02 | ~14min | 3 tasks | 4 files |

## Accumulated Context

### Roadmap Evolution

- **v1.3 Memory & Knowledge roadmap created (2026-06-09): Phases 18–23 mapped from 22 requirements** (INFRA/MEM/RECALL/KB/CTRL/PRIV); phase numbering CONTINUES from v1.2 (last phase 17). Granularity = coarse → 6 phases compressing the research's ~8 + optional spike (spike folded into Phase 18; recommend/preflight/doctor combined into Phase 22; surfacing/backup/swap combined into Phase 23) while honoring all hard dependencies and keeping the single byte-frozen contract evolution isolated to the last phase. 100% coverage, no orphans, no duplicates.
- v1.2 Operability roadmap created (2026-06-07): Phases 12–17 mapped from 13 requirements, research-converged build order preserved.
- Phase 11 added (2026-06-06): Address v1.1 tech debt — rocm_readiness detect probes + doc reconciliation (v1.1, shipped).

### Decisions

Decisions are logged in PROJECT.md Key Decisions table. Recent v1.3 roadmap decisions:

- [v1.3 Roadmap]: INTEGRATION milestone — **zero new first-party Go libraries**. Two new digest-pinned managed-service Quadlet units (Qdrant + dedicated embeddings llama-server) wired into Open WebUI by ENV only. Go stays control-plane.
- [v1.3 Roadmap]: New image literals (Qdrant + embed) live behind the `orchestrate` managed-service seam (same category as `openWebUIImage`), NOT behind the inference `BackendFor` / `TestSeamGrepGate` scope (that gate guards GPU/backend markers like `ROCm0`/`Vulkan0`). Add `QdrantImage()`/`EmbedImage()` accessors so backup can read each digest without re-typing.
- [v1.3 Roadmap]: Qdrant + embeddings are MANAGED SERVICES (rendered like `openwebui.go`), NOT inference `Backend`s — they have no device/group/exec for chat backends and must NOT flow through `parseContainerArgs`.
- [v1.3 Roadmap]: ONE new pure `internal/memory` core for all memory decision logic (footprint, enablement gate, render-view inputs, status-row classification); host effects stay in `orchestrate` + existing cmd seams. `internal/memory` imports neither `os/exec` nor a container image literal (SeamGrepGate stays green).
- [v1.3 Roadmap]: Dependency order is strict and research-converged — Qdrant + embeddings BEFORE the OWUI env wiring; wiring BEFORE the recall indexer and BEFORE surfacing/backup. Surfacing + backup + memory-aware swap land LAST (mirrors v1.x discipline: surface/back-up after the thing exists; one byte-frozen contract evolves at a time).
- [v1.3 Roadmap]: EXACTLY ONE byte-frozen contract evolves — `status.Report` `reportSchemaVersion` 2→3, append-only fields above `SchemaVersion`, golden re-frozen ONCE (Phase 23). `recommend` gets its own append-only field + schema bump (Phase 22, separate contract). `doctor` only READS `status.Report`.
- [v1.3 Roadmap]: Zero-outbound is proven, not flag-trusted — `ENABLE_PERSISTENT_CONFIG=false` + the FULL offline/telemetry env set + a **runtime firewalled document-upload smoke test** (install-time green is insufficient). Embedding model pre-staged at install (the only sanctioned outbound window).
- [v1.3 Roadmap]: Embedding footprint is reserved in `recommend.Pick()` BEFORE the chat-model fit (envelope shrinks first); the chat model must survive an embed/import workload (offload-asserting residency — a silent/partial CPU fallback under embedding load is a FAIL, never a false-green).
- [v1.3 Roadmap]: Embedding model is DECOUPLED from the chat model; `villa model swap` / `backend set` must NOT touch the embedding model or vector collections; an embedding-model change is a destructive re-index (clean-recreate the collection), surfaced as an explicit confirmed op, never a silent side effect. Dimension recorded in config + backup manifest.

Earlier (v1.0 / v1.1 / v1.2) decisions retained below.

- [v1.2 Roadmap]: Build order is research-converged — seam-locked/composition first, only ONE byte-frozen contract evolution in flight at a time, destructive backup BEFORE the TUI front-end.
- [v1.2 Roadmap]: New persistence is flat JSONL/JSON under `$XDG_DATA_HOME/villa/` — NEVER in `config.toml`, NEVER embedded SQLite (CGO breaks the static binary).
- [v1.2 Roadmap]: Each feature with decision logic gets ONE new pure `internal/*` core; host effects stay behind an `orchestrate`-resident or cmd-tier seam — `orchestrate` remains the ONLY intentionally-impure module.
- [Roadmap]: Inference behind a `Backend` interface from day one; the single `BackendFor()` resolver is the only polymorphism point, fail-closed.
- [Roadmap]: Config is the single source of truth; Quadlet units are derived/regenerated, never hand-edited.
- [Roadmap]: `--json`/dashboard contracts are byte-frozen; evolve append-only + schema bump, re-freeze goldens exactly once.
- [Roadmap]: Offload-asserting — a silent/partial CPU fallback is a FAIL, never a false-green; backend marker literals stay behind the `internal/inference` seam (`TestSeamGrepGate`).
- [v1.1]: ROCm is opt-in; Vulkan RADV stays the default; `recommend` advises, never auto-switches. Digest-pin all images (never floating/nightly tags).
- [16-03]: clean-recreate-before-import is the load-bearing fix (podman volume import MERGES + does NOT auto-create) — stale data never leaks; mirror this for the Qdrant volume in v1.3 Phase 23.
- [18-01]: VillaConfig memory_* fields are default-OFF + self-heal from defaultConfig() (single source); NO memory save path added — SC#1 byte-identical for non-opted-in v1.2 installs. MemoryEnabled left as parsed (false is a valid choice); endpoint addrs are container-DNS only, never widened.
- [18-01]: Spike decisions recorded in 18-DECISIONS.md — D-07 dedicated villa-embed llama-server (reuse pinned kyuz0 image); D-08 nomic-embed-text-v1.5 / 768-dim pinned / Q8_0 / ~512 MiB reservation; D-09 OWUI env contract with ENABLE_PERSISTENT_CONFIG=False MANDATORY and ENABLE_QDRANT_MULTITENANCY_MODE choice pending (Phase 20). TOML keys: memory_enabled/embedding_model/embedding_dim/qdrant_addr/qdrant_port/embed_addr/embed_port.
- [18-02]: NEW pure `internal/memory` core landed — `Footprint(modelID) detect.Bytes` (typed-Unknown on miss, 512 MiB single-source constant for nomic-embed-text-v1.5, D-08), `Decide(cfg) Decision` (fail-closed enablement-and-fields-valid gate, accumulates refuse-with-reason, T-18-03), `RenderView(cfg) MemoryRenderInput` (resolved-values-only handoff — no URL, no image literal, D-02c/D-10). Zero new deps; `TestSeamGrepGate` confirmed green over `internal/memory` (no os/exec, no image literal). This phase PROVIDES the functions only — no call site added; Phases 19/22/23 wire them.
- [19-01]: orchestrate memory render path landed — `QdrantImage()`/`EmbedImage()` digest-pinned managed-service consts behind the orchestrate seam (D-02/D-04, `openWebUIImage` precedent, NOT inference `BackendFor`); `seam_test.go` `isSeam` allowlist extended for `orchestrate/memory.go` in the SAME commit (Pitfall 7, `TestSeamGrepGate` green). EXPORTED `EmbedGGUFFilename()` is the single-source embed GGUF filename Plan 19-02's drift test binds against (Pitfall 3). `Render(memory_enabled=true)` appends villa-qdrant.container/.volume + villa-embed.container (Exec=`llama-server … --embeddings --pooling mean -c 8192`, `:ro,z` model mount, no PublishPort); memory-off byte-identical to the v1.2 5-unit output (D-11, 5 existing goldens unchanged). `embedEmbeddingDim=768` recorded (D-08). Qdrant manifest-list digest committed as placeholder; dev-box RepoDigest confirmation deferred to Plan 19-03 checkpoint.
- [Phase ?]: Phase-19 install memory gate keyed off PERSISTED config.LoadVilla().MemoryEnabled via loadedMemoryEnabled seam, not the always-false DefaultVillaConfig seed (T-19-16)
- [Phase ?]: Memory readiness proof asserts offline 768-dim /v1/embeddings + Qdrant writable round-trip; FAIL refuses-with-remediation (exitBlocked), never a silent skip (D-09)
- [Phase ?]: [19-03]: On-hardware freeze PASS — pinned qdrantImage b79aaa49ce… confirmed the OFFICIAL qdrant/qdrant manifest-list digest (EQUALS placeholder, no re-pin/no golden refreeze; the per-arch amd64 child 9f7a0450… reported by RepoDigests is NOT the pin, A5). Pinned kyuz0 embed digest serves a 768-length /v1/embeddings proven offline (--network none), clearing the D-06 #15406 regression risk.
- [Phase ?]: [19-03]: Live villa install (memory_enabled=true) PASS — readiness proof green (offline 768-dim /v1/embeddings + Qdrant writable), villa-qdrant+villa-embed active container-DNS only (no host port, SC#4), Qdrant writes /qdrant/storage as UID 1000 on its :Z named volume (SC#2 writable). SC#2 durability proxy-proven (collection+point survived podman rm + re-run) + linger enabled; literal sudo reboot DEFERRED (would kill the operator session) — recorded honestly, not claimed as a literal reboot.
- [Phase 20]: Phase 20-01: OWUI memory/RAG env wired behind orchestrate seam — D-09 block appended only when memory_enabled (byte-identical off), ENABLE_PERSISTENT_CONFIG=False mandatory, values config-sourced (no re-typed host literals) — INFRA-03 render half; single deliberate golden re-freeze (new memory golden), memory-aware telemetry test, seam gate green
- [Phase ?]: [20-02]: Runtime zero-outbound RAG smoke proof landed (D-10/PRIV-05) — pure evalRagSmoke asserts the egress negative control FIRST (probe-could-not-run OR external host reachable = FAIL) before trusting the upload-and-cite drive; no WARN/skip. liveRagSmoke reuses runProbeCurl over villa.network for the negative control (huggingface.co MUST be unreachable) + a NEW host-side fixed-arg curl for the loopback REST drive; helper image via orchestrate.EmbedImage(), no re-typed literal (TestSeamGrepGate green). villa verify memory is a dedicated gated verb (memory OFF exits 0, memory ON FAIL = exitBlocked); admin-token mint (signin+signup fallback, A5) + citation field (sources/citations, A6) confirmed on-hardware in Plan 03. No new host port (D-11).

### Pending Todos

[From .planning/todos/pending/ — ideas captured during sessions]

- Carryover tech debt (non-blocking, from v1.2 close): extract a shared `rocmpolicy` leaf package (deferred PR-#2 finding, graphmind memory 10e784d6); investigate `rocm-6.4.4-rocwmma` residency FAIL on gfx1151; optional re-pin of the drifted `rocm-6.4.4` rolling tag.

### Blockers/Concerns

[Issues that affect future work]

- **NON-NEGOTIABLE THREAT (Phase 20):** Open WebUI lazily pulls the embedding/reranker/Whisper model from HuggingFace at RUNTIME on first RAG use (not at install) — a fresh un-gated outbound that breaks PRIV-01/02/03. ChromaDB additionally posts PostHog telemetry. Mitigate: pre-stage the embedding model at install + route embeddings to the local `/v1/embeddings` + full offline/telemetry env block + a RUNTIME firewalled zero-outbound smoke test. Choose Qdrant over ChromaDB.
- **NON-NEGOTIABLE THREAT (Phase 22):** the embedding model competes for the SAME gfx1151 unified-memory pool — omitting it from `recommend.Pick()` → OOM or silent CPU fallback of the chat model under import load (a false-green). Reserve the embed footprint first; assert chat-model residency survives an embed/import workload.
- **NON-NEGOTIABLE THREAT (Phase 20):** Open WebUI `PersistentConfig` bakes RAG/memory settings into `webui.db` on first boot and then IGNORES the env — config drifts off `config.toml`. Set `ENABLE_PERSISTENT_CONFIG=false` from the START (retrofitting a populated DB is painful).
- **Phase 23:** embedding model/dimension swap invalidates existing vectors (dimension mismatch) — decouple embedding from chat model; make swap memory-aware; clean-recreate on embedder change; record dimension in manifest.
- On-hardware validation (gfx1151) remains the dominant verification path — Phases 19, 20, 21, 22, 23 all need live-host confirmation; the runtime zero-outbound smoke test (Phase 20) requires a firewalled run.

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260604-wh1 | Fix F-3: villa status OFFLOAD WARN — emit -lv 4 residency line + invocation-scoped ResidencyJournal | 2026-06-04 | d401a52 | [260604-wh1-...](./quick/260604-wh1-fix-f-3-villa-status-offload-warn-emit-l/) |
| 260605-d2q | Fix Makefile build target to produce villa binary | 2026-06-05 | b3a4419 | [260605-d2q-...](./quick/260605-d2q-fix-makefile-build-target-to-produce-vil/) |
| 260605-fast | fix(status): render OFFLOAD N/A for non-GPU services in human table | 2026-06-05 | e5fc1fc | — |
| 260605-tuv | Fix villa uninstall: drop unsupported podman volume rm --ignore flag | 2026-06-05 | 228a4c0 | [260605-tuv-...](./quick/260605-tuv-fix-villa-uninstall-drop-unsupported-pod/) |
| 260606-p3a | Fix villa bench single-mode backend label | 2026-06-06 | 8aa9c90 | [260606-p3a-...](./quick/260606-p3a-fix-villa-bench-single-mode-backend-labe/) |
| 260608-ppy | fix phase-17 UI-SPEC copy gaps | 2026-06-08 | 583b1ee | [260608-ppy-...](./quick/260608-ppy-fix-phase-17-ui-spec-copy-gaps-17-ui-rev/) |
| 260608-pyp | fix remaining 4 phase-17 UI-SPEC warnings | 2026-06-08 | 0cbac58 | [260608-pyp-...](./quick/260608-pyp-fix-remaining-phase-17-ui-spec-warnings-/) |

## Deferred Items

Items acknowledged at v1.2 milestone close (2026-06-08):

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| tech_debt | Extract shared `rocmpolicy` leaf package (PR-#2 finding) | Open | v1.2 close |
| tech_debt | Investigate `rocm-6.4.4-rocwmma` residency FAIL on gfx1151 | Open | v1.2 close |
| tech_debt | Optional re-pin of the drifted `rocm-6.4.4` rolling tag | Open | v1.2 close |
| limitation | Backup cross-host / post-`podman system reset` restore is documented best-effort (UID-remap + SELinux `:Z` repair validated indirectly) | Documented | v1.2 close |

## Session Continuity

Last session: 2026-06-09T21:10:40.551Z
Stopped at: Phase 20 Wave 1 complete (20-01, 20-02); Plan 20-03 Task 1 (docs/MEMORY.md) done — paused at on-hardware checkpoint Tasks 2-5 (gfx1151 required)

## Operator Next Steps

1. **Review the v1.3 roadmap** (`.planning/ROADMAP.md` → "v1.3 Memory & Knowledge" + Phase Details 18–23) and the filled traceability (`.planning/REQUIREMENTS.md`).
2. **Plan Phase 18** — `/gsd-plan-phase 18` (Memory Spine: `internal/memory` pure core + `config.toml` fields + the embeddings/wiring research spike). This phase is low-code and de-risks the env golden re-freeze; consider `--research-phase` to pin the version-sensitive OWUI env keys against the chosen OWUI digest.
3. **Carryover tech debt (non-blocking):** extract shared `rocmpolicy` leaf package; investigate `rocm-6.4.4-rocwmma` residency FAIL; optional re-pin of the drifted `rocm-6.4.4` rolling tag.
