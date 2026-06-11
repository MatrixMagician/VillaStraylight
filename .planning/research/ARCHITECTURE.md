# Architecture Patterns

**Domain:** Integrating a strictly-local memory/RAG stack (vector DB + local embeddings + Open WebUI Memory/RAG wiring) into the shipped VillaStraylight Go control plane
**Researched:** 2026-06-09
**Confidence:** HIGH on the codebase integration seams (grounded in the real files cited below); MEDIUM on exact Open WebUI env-var names + the embeddings runtime choice (cross-checked but version-sensitive — pin + re-verify in the phase that wires it)

> This is a **subsequent-milestone** research doc. It does NOT re-derive the v1.0–v1.2 architecture; it identifies exactly where a memory/RAG stack bolts onto it, and what is NEW vs MODIFIED. Every claim cites a real file.

---

## Recommended Architecture

The memory/RAG stack is **two new rootless Quadlet container services + their volumes**, joined to the existing `villa.network`, with Open WebUI's *native* RAG/Memory pointed at them by **env wiring only** (no custom Go RAG engine — PROJECT.md line 30). Go remains the control plane: it renders the units, recommends a fitting embedding model, surfaces health, and backs up the new volume.

```
                       villa.network (existing)
  ┌──────────────┐   ┌──────────────────┐   ┌──────────────────┐
  │ villa-llama  │   │ villa-openwebui  │   │  villa-qdrant    │  NEW
  │ (inference)  │◄──┤  chat + RAG/Mem  │──►│  vector DB :6333 │
  │  :8080 /v1   │   │  :3000 (loopback)│   │ villa-qdrant.vol │  NEW
  └──────────────┘   │                  │   └──────────────────┘
         ▲           │   env-wired:     │   ┌──────────────────┐
         │           │  VECTOR_DB       │   │  villa-embed     │  NEW (option A)
         │           │  QDRANT_URI      │──►│ llama-server     │
         │           │  RAG_EMBEDDING_* │   │ embeddings /v1   │
         │           └──────────────────┘   │ (--embedding)    │
         │                                   └──────────────────┘
   models volume (existing)                  models volume (shared, RO)
```

**Two embeddings options** (decide in a research-spike phase):
- **Option A — dedicated `villa-embed` llama-server** running a small GGUF embedding model (e.g. `nomic-embed-text`, `bge-small`) with `--embedding`, exposing an OpenAI-compatible `/v1/embeddings`. Open WebUI uses `RAG_EMBEDDING_ENGINE=openai` + `RAG_OPENAI_API_BASE_URL=http://villa-embed:8080/v1`. **Recommended** — reuses the proven inference image + the container-DNS env pattern, keeps embeddings off the chat-model's GPU context, and is strictly local.
- **Option B — Open WebUI's built-in SentenceTransformers engine** (`RAG_EMBEDDING_ENGINE=""`, downloads a HF model into the OWUI volume at first use). Fewer moving parts, but pulls a model at runtime (a NEW outbound at first index unless pre-staged — and `HF_HUB_OFFLINE=1` is already set in `openwebui.go:125`) and runs embeddings on CPU inside the OWUI container. **Not recommended** for the zero-outbound + GPU-aware posture.

### Component Boundaries

| Component | Responsibility | Communicates With | NEW / MODIFIED |
|-----------|---------------|-------------------|----------------|
| `villa-qdrant.container` + `villa-qdrant.volume` | Local vector store; persists embeddings | Open WebUI (over `villa.network`, DNS `villa-qdrant:6333`) | **NEW** Quadlet units |
| `villa-embed.container` (Option A) | Local OpenAI-compatible `/v1/embeddings` (llama-server `--embedding`) | Open WebUI; reads weights from shared models volume (RO) | **NEW** Quadlet unit |
| Open WebUI env block | `VECTOR_DB`, `QDRANT_URI`, `RAG_EMBEDDING_ENGINE`, `RAG_OPENAI_API_BASE_URL/KEY`, `RAG_EMBEDDING_MODEL` | Qdrant + embeddings via DNS | **MODIFIED** `internal/orchestrate/openwebui.go` env slice |
| `internal/memory` (proposed pure core) | Memory-stack decision logic: render-view builder inputs, recommend fit contribution, preflight checks, status-row classification — all pure | render.go, recommend, preflight, status, doctor | **NEW** pure core |
| `internal/orchestrate/render.go` | Emit the new units in deterministic order behind the existing managed-service render path | Quadlet templates | **MODIFIED** (append-only emit) |
| `internal/config` | New `config.toml` fields (single source of truth) | everything downstream | **MODIFIED** (append-only fields) |
| `internal/recommend` | Subtract embedding-model footprint from the envelope before the chat-model fit | catalog, detect | **MODIFIED** (append-only) |
| `internal/status` | Health rows for qdrant + embeddings; schema bump 2→3 | dashboard, CLI | **MODIFIED** (append-only) |
| `internal/backup` + `cmd/villa/podman_volume.go` | Export/import the new qdrant volume | restore transactional core | **MODIFIED** (append-only) |
| `internal/preflight` | Disk-for-vectors + memory-headroom-for-embeddings checks | install gate, doctor | **MODIFIED** (new check fn) |

### Data Flow

1. **Install:** `config.toml` gains `memory_enabled` + memory fields → `recommend` picks chat model *after* reserving the embedding-model footprint → `render.Render()` emits the chat/owui units **plus** (when `memory_enabled`) the qdrant + embed units → reconcile + WriteUnits → systemd brings the stack up on `villa.network`.
2. **Indexing (runtime, inside OWUI):** user uploads a doc / chat history → Open WebUI calls `villa-embed:8080/v1/embeddings` → stores vectors in `villa-qdrant:6333`. Strictly local; Go is not in this path.
3. **Recall:** Open WebUI native RAG retrieves from Qdrant + Memory injects user facts. Go never touches the RAG path — it only orchestrates and observes.
4. **Backup:** `villa backup` now exports **two** volumes (owui + qdrant); restore clean-recreates **both** before import.

---

## Patterns to Follow

### Pattern 1: Memory units are MANAGED SERVICES, not inference Backends
**What:** Qdrant and the embeddings server are rendered through the **managed-service render path** (the way `openwebui.go` is), NOT through `inference.Backend` / `parseContainerArgs`.
**When:** Always for Qdrant. For the embeddings server: even though it is *also* llama-server, it is a **fixed managed image with a distinct exec** (`--embedding`), so model it as a managed service to avoid coupling it to the chat-model's `BackendFor` resolution.
**Why:** `render.go:122-131` shows Open WebUI already takes a dedicated path precisely because it "has no device/group/exec args" and routing it through `parseContainerArgs` would trip the defensive all-fields-non-empty check (`render.go:232`). Qdrant is the same shape. `parseContainerArgs` REQUIRES non-empty `AddDevice`, `GroupAdd`, `PodmanArgs`, `Exec` (`render.go:232-234`) — Qdrant has none, so it MUST NOT flow through that helper.
**Example (mirror `buildOpenWebUIView`, `openwebui.go:99`):**
```go
// internal/orchestrate/qdrant.go (NEW)
const (
    qdrantContainerUnitName = "villa-qdrant.container"
    qdrantVolumeUnitName    = "villa-qdrant.volume"
    qdrantContainerName     = "villa-qdrant"          // stable DNS name
    qdrantVolumeName        = "villa-qdrant"
    qdrantImage             = "docker.io/qdrant/qdrant@sha256:<pin>" // digest-pinned, behind seam
)
// View carries NO published port if RAG traffic is network-internal only
// (Open WebUI reaches it by DNS) — keeps the loopback-only audit trivially green.
```
> **Seam note:** Qdrant/embed image literals live in `internal/orchestrate` (managed-service constants), the SAME category as `openWebUIImage` (`openwebui.go:20`) — they are **outside** `internal/inference`'s `TestSeamGrepGate` scope (that gate guards GPU/backend markers like `ROCm0`/`Vulkan0`, per CLAUDE.md). Add an `OpenWebUIImage()`-style accessor (`openwebui.go:28`) so backup can read each digest without re-typing.

### Pattern 2: Env wiring uses the existing ordered-slice + container-DNS discipline
**What:** Add memory env entries to Open WebUI's **ordered `[]envPair`** (NEVER a map — ordering is golden-frozen, `openwebui.go:59-65`). Build the Qdrant URI from the `qdrantContainerName` constant exactly as `OPENAI_API_BASE_URL` is built from `containerName` (`openwebui.go:109`), so the target can never drift from the unit's `ContainerName=`.
**When:** Wiring Open WebUI → Qdrant and Open WebUI → embeddings.
**Example:**
```go
// appended to buildOpenWebUIView().Env, ONLY when memory is enabled:
{Key: "VECTOR_DB", Value: "qdrant"},
{Key: "QDRANT_URI", Value: "http://" + qdrantContainerName + ":6333"},
{Key: "RAG_EMBEDDING_ENGINE", Value: "openai"},
{Key: "RAG_OPENAI_API_BASE_URL", Value: "http://" + embedContainerName + ":8080/v1"},
{Key: "RAG_OPENAI_API_KEY", Value: "sk-no-key-required"}, // same no-auth sentinel as owui:118
{Key: "RAG_EMBEDDING_MODEL", Value: cfg.EmbedModel},
```
> **MEDIUM-confidence caveat:** exact env names (`VECTOR_DB`, `QDRANT_URI`, `RAG_EMBEDDING_ENGINE`, `RAG_OPENAI_API_BASE_URL`) are confirmed against Open WebUI community docs/issues but are **version-sensitive**. Pin the OWUI digest the phase ships against and re-verify the names from THAT image's docs before freezing the golden. The telemetry-frozen test (`TestRenderOpenWebUITelemetryFrozen`, referenced `openwebui.go:18`) will force a deliberate re-audit when the env block changes — that is the right gate.

### Pattern 3: ONE new pure core per feature (`internal/memory`)
**What:** Create `internal/memory` as an **exec-free pure core** holding all memory-stack decision logic; host effects stay in `orchestrate` + cmd seams. This mirrors the v1.2 decision recorded in PROJECT.md Key Decisions (line 136): "Each v1.2 decision-logic feature gets ONE new pure `internal/*` core; host effects stay behind an orchestrate/cmd seam."
**When:** For any decision the memory feature needs that is computed, not I/O: the embedding-model footprint contribution, the "is memory enabled + fields valid" gate, the render-view pure inputs, and the status-row classification.
**Why:** Keeps `SeamGrepGate` green and every decision unit-testable off-hardware. The host-touching pieces (podman volume export/import of the qdrant volume, systemctl) reuse existing seams (`cmd/villa/podman_volume.go`, `internal/orchestrate/systemd.go`).
**Boundary:** `internal/memory` must NOT import `os/exec` and must NOT re-type a container image literal (those stay in `orchestrate`). It is decision logic only.

### Pattern 4: Recommend reserves the embedding footprint BEFORE the chat-model fit
**What:** On gfx1151 the embedding model competes for the SAME unified-memory envelope. Subtract its footprint from `envelope` before `pickBest` runs, so the chat model is sized against the *remaining* memory.
**Where:** `recommend.Pick` (`recommend.go:123`) computes `envelope` via `resolveEnvelope(p)` then passes it to `pickBest` (`recommend.go:146`). Insert the reservation between them.
**Example:**
```go
envelope, degraded, ok := resolveEnvelope(p)
// NEW: when memory enabled, reserve the embedding model's resident footprint
// (weights + its own small KV) so the chat-model fit math sees the real remainder.
if memEnabled {
    embedBytes := memory.EmbedFootprint(embedModel) // pure, from a small embed catalog
    if embedBytes < envelope { envelope -= embedBytes } else { /* note + refuse/degrade */ }
}
```
The fit inequality keeps its shape (`recommend.go:231`: `total = WeightBytes + kvCacheBytes + headroom ≤ envelope`) — only the envelope shrinks. Surface the reserved bytes as a NEW append-only `Recommendation` field (e.g. `EmbedReserveBytes uint64`) placed **above** `SchemaVersion` (the LAST-field rule, `recommend.go:100-102`) and bump `recommendSchemaVersion` (`recommend.go:29`). Embedding models are small (~50–700 MB) so this is a modest, honest reservation — not a blocker on a 64–128 GB Strix Halo box, but it MUST be accounted for to keep "runs healthy after install" true.

### Pattern 5: Status surfacing is append-only, schema-bumped exactly once
**What:** Add health rows for `villa-qdrant.service` and (Option A) `villa-embed.service` to `status.Report`. Both are **non-GPU services** → reuse the `naOffloadVerdict()` / `OffloadApplies=false` pattern (`status.go:71`, `status.go:364-373`) so they fold health/active into the verdict but never record a spurious offload PASS/FAIL.
**Where:** Qdrant/embed services appear in `serviceUnits(units)` automatically once their `.container` units are rendered (`status.go:272-280` derives services from the `.container` suffix). The only NEW code is a per-service health probe seam (a bounded GET to qdrant's `/readyz`/`/healthz`, and to embed's `/health`), wired like `DashboardHealth` (`status.go:259-260`). Bump `reportSchemaVersion` 2→3 (`status.go:156`), re-freeze the golden ONCE — mirroring v1.2 discipline (PROJECT.md line 138).
**Dashboard:** the dashboard reads the same `status.Report` (no new endpoint — `status.go` doc lines 6-12); new rows render for free; add labels in the embedded SPA only.

### Pattern 6: Backup/restore extends to the new volume transactionally
**What:** Add the qdrant volume to the backup archive and the transactional restore.
**Where (backup):** `Backup` currently exports exactly the OWUI volume (`backup.go:121-125`) and reads a fixed `sources` list (`backup.go:134-139`). Add a `villa-qdrant` volume export + a new `EntryQdrantVolume = "qdrant-volume.tar"` manifest entry (`manifest.go:29-35`, append-only). The export uses the existing fixed-arg seam `volumeExportArgs` (`podman_volume.go:24`) — no new impure module.
**Where (restore):** `Restore` clean-recreates the OWUI volume before import because `podman volume import` MERGES and does not auto-create (`restore.go:14-23`). The qdrant volume needs the **identical** clean-recreate→import ordering on BOTH the forward apply and the rollback path (`restore.go:108-115`). `RestoreInput` gains a `QdrantVolumeName` + temp-tar fields (append-only, `restore.go:42-80`).
**Quiesce:** Qdrant must also be **stopped** during backup for a consistent snapshot — extend the deferred-restart quiesce (`backup.go:102-119`) to stop/restart qdrant alongside OWUI. (Embeddings is stateless except for the read-only weights — it need not be archived, only quiesced if it holds the GPU.)
**Manifest:** add a Qdrant image digest field to the manifest (append-only above `SchemaVersion`, `manifest.go:99-101`) sourced via an accessor — feeds `CompareSkew`'s image-digest WARN exactly like `OpenWebUIImage` (`backup.go:297-303`). Bump `backupSchemaVersion` (`manifest.go:23`).

### Pattern 7: Config is the single source of truth — new fields are append-only, defaulted, self-healing
**What:** New `config.toml` fields mirror the existing style (`dashboard_port`, `chat_port`, `backend` — `villaconfig.go:31-51`):
```toml
memory_enabled  = false                # opt-in, like backend=rocm is opt-in
vector_db       = "qdrant"             # selected vector store (future-proofs alternatives)
vector_db_port  = 6333                 # qdrant host/internal port
embed_model     = "nomic-embed-text"   # embedding model id (small catalog)
embed_engine    = "openai"             # "openai" (Option A, dedicated server) vs "" (built-in)
embed_port      = 8081                 # embeddings llama-server host port (distinct from 8080/3000/8888)
```
**Discipline:** add to `VillaConfig` struct (`villaconfig.go:31`) with toml tags; seed defaults in `defaultConfig()` (`villaconfig.go:55`); if any field has a "0/empty == unset" hazard like the dashboard ports, extend `normalizeVilla` (`villaconfig.go:80`) — but keep `memory_enabled=false` the default so existing installs are byte-identical until the user opts in. **Quadlet units regenerate from these fields; never hand-edited** (CLAUDE.md). `Parse` (`villaconfig.go:228`, used by restore) picks the new fields up for free.

---

## Anti-Patterns to Avoid

### Anti-Pattern 1: Routing Qdrant through `inference.BackendFor` / `parseContainerArgs`
**Why bad:** `BackendFor` is the single GPU-backend polymorphism point (`backend.go:24`); Qdrant is not a GPU backend. `parseContainerArgs` rejects it on the all-fields-non-empty guard (`render.go:232`). **Instead:** dedicated managed-service render path like `openwebui.go`.

### Anti-Pattern 2: Putting an embedding image literal in `cmd/villa` or `internal/memory`
**Why bad:** Image digests are managed-service constants that belong behind the `orchestrate` seam (uniform with `openwebui.go:20-28`); leaking them elsewhere breaks the "no re-typed image literal" discipline and the backup-accessor pattern. **Instead:** `orchestrate/qdrant.go` + `orchestrate/embed.go` with `QdrantImage()` / `EmbedImage()` accessors.

### Anti-Pattern 3: Sizing the chat model against the full envelope, then adding embeddings
**Why bad:** On unified memory the embedding model is resident concurrently; the v1.0 fit math (`recommend.go:231`) would over-promise and the install could OOM under load — violating the "runs healthy after install" bar. **Instead:** reserve the embedding footprint FIRST (Pattern 4).

### Anti-Pattern 4: A second byte-frozen contract or a re-ordered Report
**Why bad:** Re-ordering or compounding multiple schema bumps breaks the golden discipline (PROJECT.md line 138). **Instead:** ONE schema bump on `status.Report` (2→3), new fields strictly above `SchemaVersion`, golden re-frozen once.

### Anti-Pattern 5: Letting Open WebUI download an embedding model at runtime (Option B default)
**Why bad:** A first-index HF download is a NEW outbound, violating "zero new outbound beyond image/model pulls" (PROJECT.md line 78). `HF_HUB_OFFLINE=1` is already set (`openwebui.go:125`), which would make built-in embedding silently fail offline. **Instead:** Option A's pre-pulled GGUF + dedicated `villa-embed` server.

### Anti-Pattern 6: Publishing Qdrant/embed on host ports unnecessarily
**Why bad:** Every published port is audited for loopback (`status.go:495-543`, `allLoopback`). If Open WebUI reaches them only by container DNS, they need NO `PublishPort=` at all — keeping the privacy posture trivially green. **Instead:** publish only if a human needs host access (e.g. a Qdrant dashboard); if so, bind `127.0.0.1` exactly like `openWebUIPublishPort` (`openwebui.go:50`).

---

## Scalability Considerations

| Concern | Small (a few docs) | Medium (10k chunks) | Large (100k+ chunks) |
|---------|--------------------|--------------------|----------------------|
| Qdrant disk | tens of MB | hundreds of MB–GB | multi-GB on `villa-qdrant.volume` — preflight disk check must include it |
| Embedding throughput | dedicated llama-server fine | fine; batches | consider a larger embed model only if envelope allows |
| Unified-memory pressure | negligible | embed model resident (~50–700 MB) | reserved in recommend (Pattern 4); does not grow with corpus |
| Vector RAM (Qdrant) | small | mostly disk-bound (mmap) | watch HNSW index RAM — surface in doctor headroom note |

---

## Suggested Build Order (dependency-aware)

The ordering follows the proven v1.x rule that **surfacing/backup land LAST, after the thing they surface exists** (PROJECT.md line 133), and that decision-cores precede their wiring.

1. **`internal/memory` pure core + config fields** — the foundation. Add `VillaConfig` fields (`villaconfig.go`) + the pure `memory` decision functions (footprint, enablement gate, view inputs). No host effects yet. Unit-testable immediately. *Depends on: nothing.*
2. **Qdrant managed-service render** — `orchestrate/qdrant.go` + `qdrant.container.tmpl` + `qdrant.volume.tmpl`; emit in `Render()` behind `if cfg.MemoryEnabled` (append to the fixed emit order, `render.go:135-141`). Golden for the new units. *Depends on: 1 (config flag).*
3. **Embeddings render (Option A)** — `orchestrate/embed.go` + template; same managed-service shape; mounts the models volume RO and runs `--embedding`. *Depends on: 2 (pattern established).*
4. **Open WebUI env wiring** — extend `buildOpenWebUIView().Env` (`openwebui.go`) with the `VECTOR_DB`/`QDRANT_URI`/`RAG_*` entries, gated on `cfg.MemoryEnabled`; re-verify exact names against the pinned OWUI digest; re-freeze the owui container + telemetry goldens once. *Depends on: 2 + 3 (the DNS targets must exist).*
5. **Recommend integration** — reserve embedding footprint before the chat-model fit (`recommend.go`), add the `EmbedReserveBytes` field + schema bump. *Depends on: 1 (footprint fn).*
6. **Preflight/doctor checks** — extend `checkResources` (or add `checkMemoryResources`) for vector disk + embedding-memory headroom (`checks_resources.go`); doctor composes it for free (it already folds preflight). *Depends on: 1, 2.*
7. **Status + dashboard surfacing** — add qdrant/embed health rows + probe seams, bump `reportSchemaVersion` 2→3, re-freeze status golden once, label in SPA. *Depends on: 2, 3 (services must render to appear in `serviceUnits`).*
8. **Backup/restore extension** — add the qdrant volume to backup export + manifest entry + transactional restore clean-recreate, extend quiesce, add manifest image digest + `backupSchemaVersion` bump. **LAST** — it backs up state the prior steps create. *Depends on: 2 (volume must exist).*

**Critical dependencies:** 2 (Qdrant) and 3 (embeddings) before 4 (env wiring) before 7/8 (surface/backup). 1 (config + pure core) is the spine touched by 2,4,5,6.

---

## Confidence Notes

| Claim | Confidence | Basis |
|-------|-----------|-------|
| Managed-service render path (not Backend seam) | HIGH | `render.go:122-131`, `openwebui.go` (real code) |
| Ordered-`[]envPair` + container-DNS wiring | HIGH | `openwebui.go:99-129` |
| ONE pure core per feature; image literals behind orchestrate | HIGH | PROJECT.md line 136; `openwebui.go:20-28` |
| Recommend envelope-reservation insertion point | HIGH | `recommend.go:123-146,231` |
| Status non-GPU N/A-offload row pattern + schema bump | HIGH | `status.go:71,156,272-280,364-373` |
| Backup clean-recreate-before-import + quiesce | HIGH | `backup.go:102-139`, `restore.go:14-23,108-115` |
| Exact Open WebUI env var names + accepted values | MEDIUM | community docs/issues — version-sensitive; re-verify against pinned digest |
| Embedding-model choice + footprint numbers | MEDIUM | general llama.cpp embedding practice — confirm in a research spike |

## Sources

- Codebase (HIGH, read directly): `internal/orchestrate/render.go`, `internal/orchestrate/openwebui.go`, `internal/orchestrate/orchestrate.go`, `internal/orchestrate/quadlet/*.tmpl`, `internal/config/villaconfig.go`, `internal/recommend/recommend.go`, `internal/status/status.go`, `internal/inference/backend.go`, `internal/backup/backup.go`, `internal/backup/manifest.go`, `internal/backup/restore.go`, `internal/preflight/checks_resources.go`, `cmd/villa/podman_volume.go`, `.planning/PROJECT.md`, `CLAUDE.md`.
- Open WebUI vector-DB / RAG env (MEDIUM — version-sensitive): [Vector Database Integration — DeepWiki](https://deepwiki.com/open-webui/open-webui/7.5-vector-database-integration), [Qdrant Support for RAG (issue #15197)](https://github.com/open-webui/open-webui/issues/15197), [Qdrant + Postgres + Open WebUI (discussion #11597)](https://github.com/open-webui/open-webui/discussions/11597).
