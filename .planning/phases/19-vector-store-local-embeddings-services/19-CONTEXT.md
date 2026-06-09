# Phase 19: Vector Store + Local Embeddings Services - Context

**Gathered:** 2026-06-09
**Status:** Ready for planning

> Captured in `--auto` mode (single pass). Each decision below was auto-selected
> as the recommended option, grounded in the **PINNED** Phase-18 spike decisions
> (`18-DECISIONS.md` D-07/D-08/D-09), the milestone scope, ROADMAP Phase 19, and
> REQUIREMENTS INFRA-01/INFRA-02/PRIV-04. Review before planning.

<domain>
## Phase Boundary

Render and start the **two new rootless Podman Quadlet managed services** that
back the v1.3 memory stack, driven entirely from the Phase-18 config spine:

1. **`villa-qdrant`** — a local Qdrant vector DB (digest-pinned image, durable
   named `:Z` volume, **no published host port** — container-DNS / loopback only).
2. **`villa-embed`** — a dedicated embeddings `llama-server` exposing an
   OpenAI-compatible `/v1/embeddings` endpoint (reuses the pinned kyuz0 toolbox
   image, container-DNS only), serving the pinned `nomic-embed-text-v1.5` GGUF
   that is **pre-staged at install** so nothing is pulled from the internet at
   runtime.

Both units join `villa.network`, are **regenerated from `config.toml`** (never
hand-edited), and appear only when `memory_enabled=true` (Phase-18 byte-identical
guarantee continues when off).

**In scope:** the orchestrate managed-service render path for Qdrant + villa-embed
(image literals land here per D-10); the durable Qdrant storage volume; install-time
pre-staging of the embedding GGUF into the model store; the systemd start/reconcile
wiring; an install-time offline proof that `/v1/embeddings` answers and Qdrant is
writable; keeping the loopback-only privacy audit green.

**Out of scope (later phases — do NOT build here):**
- Wiring Open WebUI env to Qdrant/villa-embed + offline/telemetry lockdown +
  zero-outbound runtime smoke test — **Phase 20** (D-09 keys are recorded, not set here).
- The chats→Knowledge recall indexer — **Phase 21**.
- `recommend` footprint reservation / `preflight` host gating / `doctor` memory
  checks — **Phase 22**.
- `status`/dashboard memory rows, backup/restore of the Qdrant volume, memory-aware
  model swap — **Phase 23**.

This phase renders and runs the **services**; it does NOT touch the OWUI env block.

</domain>

<decisions>
## Implementation Decisions

### Qdrant service (INFRA-01)
- **D-01:** Pin the **`qdrant/qdrant` *unprivileged* image variant** (milestone
  target `v1.18.2-unprivileged`), **digest-pinned**, with a Phase-19 legitimacy
  audit before freezing. Rationale: the unprivileged variant runs as a non-root
  UID, which avoids the rootless-Podman UID / SELinux `:Z` write-permission failure
  that SC#2 explicitly guards against (Qdrant must be writable + survive reboot).
- **D-02:** The image literal lives behind the **`orchestrate` managed-service
  render path**, a new `const qdrantImage` + `QdrantImage()` accessor **mirroring
  `openWebUIImage`/`OpenWebUIImage()`** in `internal/orchestrate/openwebui.go` —
  NOT `inference.BackendFor` / `TestSeamGrepGate` (D-10). Same category as the
  Open WebUI image.
- **D-03:** Storage is a **dedicated durable named volume** (e.g.
  `villa-qdrant.volume` → mount `/qdrant/storage:Z`), separate from `villa-models`.
  Durability via the named volume + user lingering (SC#2: survives reboot). No
  published host port — Qdrant is reached only as `villa-qdrant:6333` over
  `villa.network` (PRIV-01 continuity; never widen a bind).

### Embeddings service `villa-embed` (INFRA-02)
- **D-04:** A **dedicated `villa-embed` llama-server** reusing the **pinned kyuz0
  toolbox image** (D-07). The embed image is a **new digest-pinned const in
  `internal/orchestrate`** (managed-service path, mirroring `openWebUIImage`), NOT
  a reference into the `internal/inference` backend seam — keeps `TestSeamGrepGate`
  semantics clean (managed-service image, not a GPU-backend token, per D-10).
- **D-05:** Invocation is **pinned by the Phase-18 spike**: serve
  `nomic-embed-text-v1.5` on container-DNS **`villa-embed:8080`**, OpenAI
  **`/v1/embeddings`**, flags **`--embeddings --pooling mean`** (pooling mode ≠
  `none` is required for the embeddings endpoint), **ctx 8192** (D-07/D-08).
  Container-DNS only — no host bind.
- **D-06:** **Carry-forward risk (D-07):** the kyuz0 image tracks llama.cpp master,
  which once regressed `/v1/embeddings` (#15406). The digest pin mitigates silent
  breakage, and Phase 19 **MUST curl `/v1/embeddings` against the pinned digest**
  as part of the install proof before the unit is considered healthy.

### Embedding-model pre-staging (PRIV-04 / SC#3)
- **D-07:** **Pre-stage the GGUF at install via a one-time controlled pull**
  (reusing the existing `internal/download` weight-pull path) into the **existing
  `villa-models` volume** (single model store, shared with chat weights) — then the
  runtime is fully offline. PRIV-04 forbids *runtime* download, not an install-time
  controlled fetch. The pinned source is **`nomic-ai/nomic-embed-text-v1.5-GGUF`,
  quant Q8_0** (~146 MB file; ~512 MiB conservative resident reservation per D-08).
  `villa-embed` mounts the staged GGUF read-only from `villa-models`.
- **D-08:** The embedding **dimension (768) is a pinned, load-bearing constant**
  (D-08): record it on the rendered service so Phase 23's backup manifest + memory-
  aware swap guard can detect skew. Do NOT Matryoshka-truncate.

### Install-time proof & privacy (SC#3, SC#4)
- **D-09:** Install adds a **memory-stack readiness proof** after start: (a) an
  **offline `/v1/embeddings` smoke probe** that asserts a **768-length** vector
  returns with **no network access** (proves PRIV-04 zero-runtime-download + guards
  the D-06 master-regression risk); (b) a **Qdrant writable/readiness probe**
  (SC#2). A failure refuses-with-remediation; a silent skip is not acceptable.
- **D-10:** The existing **loopback-only privacy audit stays green** (SC#4) — reuse
  it unchanged; neither new service binds beyond loopback / `villa.network`. No new
  published port; no shell interpolation in any rendered arg.

### Conditional rendering (continues Phase-18 spine)
- **D-11:** orchestrate renders the Qdrant + villa-embed units **and the Qdrant
  volume only when `memory_enabled=true`**; with memory off the install output is
  **byte-identical** to a v1.2 install (Phase-18 SC#1 continuity). Units are
  regenerated from config, never hand-edited (INFRA-04). The render-view input is
  the Phase-18 `internal/memory` render-view struct (resolved values only — model
  id, dim, service addrs/ports — no image literals).

### Claude's Discretion
- Exact Go symbol names, Quadlet template filenames (`qdrant.container.tmpl`,
  `embed.container.tmpl`, `qdrant.volume.tmpl`), unit names, and accessor spellings
  are the planner's call within the patterns above.
- Whether the embed GGUF stage is a distinct install sub-step or folded into the
  existing weight-pull flow — planner's call, as long as runtime is zero-download.
- Whether `QDRANT_API_KEY` is left empty for the private container-DNS net or a
  generated key is added (no schema change either way) — defer the OWUI-facing
  choice to Phase 20; Phase 19 only needs the service reachable on `villa.network`.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope, requirements & PINNED spike decisions
- `.planning/ROADMAP.md` → **Phase 19** section — goal + 4 success criteria (INFRA-01/02, PRIV-04).
- `.planning/REQUIREMENTS.md` — **INFRA-01** (Qdrant Quadlet service), **INFRA-02**
  (embeddings llama-server `/v1/embeddings`), **PRIV-04** (no runtime model download).
- `.planning/phases/18-memory-spine-config-core-embeddings-wiring-research-spike/18-DECISIONS.md`
  — **PINNED** D-07 (villa-embed runtime + `--embeddings --pooling mean`), D-08
  (`nomic-embed-text-v1.5`, 768-dim, Q8_0 GGUF, ~512 MiB footprint), D-09 (OWUI env —
  recorded for Phase 20, not set here). **Phase 19 freezes against these values.**
- `.planning/phases/18-…/18-RESEARCH.md` — Qdrant ports (6333/6334), `/qdrant/storage`,
  embeddings flags, pre-staging rationale, GGUF sizes, dimension-mismatch hazard, threat model.
- `.planning/phases/18-…/18-CONTEXT.md` — D-10 seam placement; the config spine D-04/D-05/D-06.
- `.planning/PROJECT.md` — v1.3 milestone goal, integrate-not-rebuild, out-of-scope (custom Go RAG/embeddings engine; cloud embedding APIs).

### Code touchpoints (the services extend these)
- `internal/orchestrate/openwebui.go` — **the managed-service template**:
  `const openWebUIImage` + `OpenWebUIImage()` accessor, `*VolumeUnitName`,
  `:Z` volume mount, `openWebUIVolumeView`. Qdrant + villa-embed mirror this (D-02/D-04).
- `internal/orchestrate/render.go` — `villa-models.volume` / `villa-models`
  (`volumeUnitName`/`volumeName`, lines ~27/32), `execTemplate`, the render pipeline
  the two new units + Qdrant volume slot into (conditional on `memory_enabled`, D-11).
- `internal/orchestrate/quadlet/*.tmpl` (`container.tmpl`, `volume.tmpl`,
  `openwebui.container.tmpl`, `openwebui.volume.tmpl`) — `go:embed` template pattern
  for the new `qdrant`/`embed` units.
- `internal/orchestrate/systemd.go` + `reconcile.go` — start/reconcile + typed
  `ErrToolNotFound`/`ErrCommandFailed` discipline the install proof reuses.
- `internal/config/villaconfig.go` — Phase-18 memory fields (`memory_enabled`,
  embedding model id, dimension, Qdrant/embeddings endpoints) that drive the render.
- `internal/memory` — Phase-18 pure core: the **render-view input struct** orchestrate
  consumes (resolved values, no image literals) and the enablement gate.
- `internal/download/download.go` — existing weight-pull path reused for install-time
  GGUF pre-staging (D-07).
- `internal/inference/seam_test.go` — `TestSeamGrepGate`: must stay green; the new
  image literals are managed-service (orchestrate), not backend tokens (D-10).

### External (re-verify at planning time)
- `qdrant.tech` install/config — default ports 6333 (REST) / 6334 (gRPC),
  `/qdrant/storage` data dir, unprivileged image variant; Phase 19 pins + legitimacy-audits the digest.
- `nomic-ai/nomic-embed-text-v1.5-GGUF` (HuggingFace) — GGUF source/quant for pre-staging.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/orchestrate/openwebui.go` — direct template for both new managed
  services: digest-pinned `const` + exported `*Image()` accessor, named `:Z`
  volume, view struct → template. Copy this shape for Qdrant and villa-embed (D-02/D-04).
- `villa-models` named volume (`render.go`) — reused as the single model store for
  the pre-staged embed GGUF (D-07); no new model volume needed.
- `internal/download` — existing weight-pull flow reused for the one-time install
  GGUF fetch (then runtime offline).
- `internal/memory` render-view struct (Phase 18) — feeds orchestrate resolved
  service addrs/ports/model id/dim with no image literals.
- Existing loopback-only privacy audit — reused unchanged to keep SC#4 green (D-10).
- Typed `ErrToolNotFound`/`ErrCommandFailed` (`systemd.go`) — the install proof's
  refuse-with-remediation reuses this error discipline.

### Established Patterns
- **Config is the single source of truth; units regenerate from config** (INFRA-04) —
  the two units + Qdrant volume render from `memory_*` fields, never hand-edited.
- **orchestrate is the only intentionally impure module** — all host/exec/FS touch
  for the new services stays in `systemd.go`/`WriteUnits`; render/reconcile stay pure.
- **Managed-service image literals live in orchestrate** (`openWebUIImage` precedent),
  distinct from GPU-backend tokens behind `inference` + `TestSeamGrepGate` (D-10).
- **Loopback/container-DNS only; no shell interpolation** — no published host port;
  fixed-arg `exec.Command`; model name catalog/config-resolved.
- **Honesty-by-construction / offload-asserting** — the install proof asserts a real
  offline `/v1/embeddings` answer + Qdrant writability; a silent skip is a FAIL, not a false-green.

### Integration Points
- `config.toml` `memory_*` → `internal/memory` render-view → `orchestrate` render of
  `villa-qdrant` + `villa-embed` + Qdrant volume (this phase, INFRA-01/02).
- `villa-embed:8080/v1/embeddings` + `villa-qdrant:6333` → **Phase 20** OWUI env
  wiring targets (`RAG_OPENAI_API_BASE_URL`, `QDRANT_URI` — D-09, set in Phase 20).
- Pinned embedding **dimension (768)** on the rendered service → **Phase 23** backup
  manifest + memory-aware swap guard.
- Measured `villa-embed` resident GTT footprint (refines D-08's ~512 MiB estimate) →
  **Phase 22** `recommend.Pick` footprint reservation (CTRL-01).

</code_context>

<specifics>
## Specific Ideas

- Qdrant: `qdrant/qdrant:v1.18.2-unprivileged` (digest-pinned, legitimacy-audited),
  `/qdrant/storage:Z` on a dedicated named volume, reached as `villa-qdrant:6333`,
  no host port.
- villa-embed: pinned kyuz0 toolbox image, `villa-embed:8080`, `/v1/embeddings`,
  `--embeddings --pooling mean`, ctx 8192, `nomic-embed-text-v1.5` Q8_0 GGUF
  (`nomic-ai/nomic-embed-text-v1.5-GGUF`) mounted read-only from `villa-models`.
- Install proof: offline `/v1/embeddings` smoke returning a 768-length vector +
  Qdrant writable probe; guards the llama.cpp-master embeddings-regression risk (#15406)
  behind the digest pin.
- Front-of-mind privacy driver (mitigated, mostly Phase 20): OWUI HF lazy-download +
  ChromaDB telemetry — Phase 19's dedicated local embedder + Qdrant already remove the
  HF runtime-download vector for embeddings.

</specifics>

<deferred>
## Deferred Ideas

- OWUI env wiring (`VECTOR_DB=qdrant`, `RAG_EMBEDDING_ENGINE=openai`,
  `ENABLE_PERSISTENT_CONFIG=False`, offline/telemetry lockdown) + zero-outbound
  runtime smoke test — **Phase 20** (keys recorded in 18-DECISIONS.md D-09).
- `ENABLE_QDRANT_MULTITENANCY_MODE` choice (must be fixed before any vectors exist) —
  **Phase 20**.
- chats→Knowledge semantic recall indexer — **Phase 21**.
- `recommend` footprint reservation + `preflight` memory gating + `doctor` memory
  health — **Phase 22**.
- `status`/dashboard memory rows (schema 2→3), backup/restore of the Qdrant volume,
  memory-aware model swap — **Phase 23**.

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 19-vector-store-local-embeddings-services*
*Context gathered: 2026-06-09*
