# Phase 19: Vector Store + Local Embeddings Services - Research

**Researched:** 2026-06-09
**Domain:** Rootless Podman Quadlet managed-service orchestration (Qdrant vector DB + a dedicated `villa-embed` llama-server), install-time embedding-model pre-staging, conditional render + byte-identical guarantee, install-time offline readiness proof
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Pin the **`qdrant/qdrant` *unprivileged* image variant** (target `v1.18.2-unprivileged`), **digest-pinned**, with a Phase-19 legitimacy audit before freezing. The unprivileged variant runs as a non-root UID, avoiding the rootless-Podman UID / SELinux `:Z` write-permission failure SC#2 guards against.
- **D-02:** The Qdrant image literal lives behind the **`orchestrate` managed-service render path** — a new `const qdrantImage` + `QdrantImage()` accessor **mirroring `openWebUIImage`/`OpenWebUIImage()`** in `internal/orchestrate/openwebui.go` — NOT `inference.BackendFor` / `TestSeamGrepGate` (D-10).
- **D-03:** Storage is a **dedicated durable named volume** (e.g. `villa-qdrant.volume` → mount `/qdrant/storage:Z`), separate from `villa-models`. Durability via the named volume + user lingering. **No published host port** — Qdrant is reached only as `villa-qdrant:6333` over `villa.network`.
- **D-04:** A **dedicated `villa-embed` llama-server** reusing the **pinned kyuz0 toolbox image** (D-07). The embed image is a **new digest-pinned const in `internal/orchestrate`** (managed-service path, mirroring `openWebUIImage`), NOT a reference into the `internal/inference` backend seam.
- **D-05:** Invocation pinned by the Phase-18 spike: serve `nomic-embed-text-v1.5` on container-DNS **`villa-embed:8080`**, OpenAI **`/v1/embeddings`**, flags **`--embeddings --pooling mean`**, **ctx 8192** (D-07/D-08). Container-DNS only — no host bind.
- **D-06:** Carry-forward risk (D-07): the kyuz0 image tracks llama.cpp master, which once regressed `/v1/embeddings` (#15406). The digest pin mitigates silent breakage, and Phase 19 **MUST curl `/v1/embeddings` against the pinned digest** as part of the install proof before the unit is considered healthy.
- **D-07:** **Pre-stage the GGUF at install via a one-time controlled pull** (reusing the existing `internal/download` weight-pull path) into the **existing `villa-models` volume** — then runtime is fully offline. Pinned source **`nomic-ai/nomic-embed-text-v1.5-GGUF`, quant Q8_0** (~146 MB; ~512 MiB conservative resident reservation). `villa-embed` mounts the staged GGUF read-only from `villa-models`.
- **D-08:** The embedding **dimension (768) is a pinned, load-bearing constant**: record it on the rendered service so Phase 23's backup manifest + memory-aware swap guard can detect skew. Do NOT Matryoshka-truncate.
- **D-09:** Install adds a **memory-stack readiness proof** after start: (a) an **offline `/v1/embeddings` smoke probe** asserting a **768-length** vector returns with **no network access**; (b) a **Qdrant writable/readiness probe** (SC#2). Failure refuses-with-remediation; silent skip is not acceptable.
- **D-10:** The existing **loopback-only privacy audit stays green** (SC#4) — reuse it unchanged; neither new service binds beyond loopback / `villa.network`. No new published port; no shell interpolation in any rendered arg.
- **D-11:** orchestrate renders the Qdrant + villa-embed units **and the Qdrant volume only when `memory_enabled=true`**; with memory off the install output is **byte-identical** to a v1.2 install. Units regenerated from config, never hand-edited. The render-view input is the Phase-18 `internal/memory` render-view struct (resolved values only — no image literals).

### Claude's Discretion
- Exact Go symbol names, Quadlet template filenames (`qdrant.container.tmpl`, `embed.container.tmpl`, `qdrant.volume.tmpl`), unit names, and accessor spellings are the planner's call within the patterns above.
- Whether the embed GGUF stage is a distinct install sub-step or folded into the existing weight-pull flow — planner's call, as long as runtime is zero-download.
- Whether `QDRANT_API_KEY` is left empty for the private container-DNS net or a generated key is added (no schema change either way) — defer the OWUI-facing choice to Phase 20; Phase 19 only needs the service reachable on `villa.network`.

### Deferred Ideas (OUT OF SCOPE)
- OWUI env wiring (`VECTOR_DB=qdrant`, `RAG_EMBEDDING_ENGINE=openai`, `ENABLE_PERSISTENT_CONFIG=False`, offline/telemetry lockdown) + zero-outbound runtime smoke test — **Phase 20** (D-09 keys recorded in 18-DECISIONS.md, not set here).
- `ENABLE_QDRANT_MULTITENANCY_MODE` choice — **Phase 20**.
- chats→Knowledge semantic recall indexer — **Phase 21**.
- `recommend` footprint reservation + `preflight` memory gating + `doctor` memory health — **Phase 22**.
- `status`/dashboard memory rows (schema 2→3), backup/restore of the Qdrant volume, memory-aware model swap — **Phase 23**.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| INFRA-01 | `villa install` orchestrates a local Qdrant vector DB as a rootless Podman Quadlet service on `villa.network` (digest-pinned image, named `:Z` volume, no published/host port — loopback/container-DNS only) | `## Standard Stack` (Qdrant image + digest), `## Architecture Patterns` (Pattern 1 Qdrant managed-service render mirroring `openwebui.go`; Pattern 4 conditional render), `## Code Examples` (qdrant view + template), `## Common Pitfalls` (Pitfall 1 SC#2 UID/`:Z`/`:U`; Pitfall 6 PublishPort omission), `## Validation Architecture` (golden re-freeze + SC#2 writable probe) |
| INFRA-02 | `villa install` orchestrates a local embeddings `llama-server` exposing `/v1/embeddings` (reuses the pinned toolbox image, container-DNS only), serving a pinned default embedding model | `## Standard Stack` (kyuz0 image reuse + llama-server flags), `## Architecture Patterns` (Pattern 2 villa-embed render; compare to `backend_vulkan.go` exec), `## Code Examples` (embed view + flags + `/v1/embeddings` curl), `## Common Pitfalls` (Pitfall 2 `--embeddings`/pooling/#15406; Pitfall 3 model path mount) |
| PRIV-04 | No embedding model is downloaded at runtime — pre-staged at install, offline enforced | `## Architecture Patterns` (Pattern 3 GGUF pre-staging via `internal/download` into `villa-models`), `## Code Examples` (catalog Shard for nomic GGUF — verified size/sha256), `## Common Pitfalls` (Pitfall 4 model-not-present crash-loop; Pitfall 5 offline-proof must assert no network), `## Security Domain` (offline posture) |
</phase_requirements>

## Summary

Phase 19 is an **integrate-not-rebuild infrastructure phase**: it adds zero first-party Go libraries and renders/starts two new OSS containers as rootless Podman Quadlet managed-service units, driven entirely from the Phase-18 config spine. The two services are **`villa-qdrant`** (the `qdrant/qdrant:v1.18.2-unprivileged` vector DB on a dedicated durable `:Z` named volume, container-DNS only) and **`villa-embed`** (a dedicated `llama-server` reusing the already-pinned kyuz0 toolbox image, serving `nomic-embed-text-v1.5` Q8_0 on `/v1/embeddings` over container-DNS). The embedding GGUF is pre-staged at install into the existing `villa-models` volume via the proven `internal/download` path, so runtime is zero-download.

The codebase already supplies the exact templates to copy: `internal/orchestrate/openwebui.go` is the managed-service precedent for both image literals and accessors (`const openWebUIImage` + `OpenWebUIImage()`, the `openWebUIView`/`openWebUIVolumeView` view structs, the `:Z` named-volume mount, and the dedicated `openwebui.container.tmpl`/`openwebui.volume.tmpl` that bypass `parseContainerArgs`). The Phase-18 `internal/memory.RenderView` already produces the resolved-values-only `MemoryRenderInput` (model id, dim, addr/port pieces, no image literals) that orchestrate consumes; `internal/config` already has the `memory_*` fields, defaults (`villa-qdrant`/6333, `villa-embed`/8080, 768, `nomic-embed-text-v1.5`), and the byte-identical `marshalVilla` drop-when-off logic. The loopback-only privacy audit (`internal/status.publishedPorts`) reads only `PublishPort=` lines — since neither new unit publishes a host port, **SC#4 stays green automatically** with no change.

Three implementation risks dominate and are de-risked below: (1) **SC#2 Qdrant writability** — the unprivileged image runs as UID:GID **1000:1000** (`USER_ID=1000` build arg, verified in the qdrant release workflow), and its Dockerfile `chown`s `/qdrant/storage` to 1000 at build; a *fresh named volume* inherits that ownership on first init, but the `:Z` SELinux relabel is required on Fedora and the `:U` flag is the documented belt-and-suspenders fix if a permission failure surfaces. (2) **`/v1/embeddings` on the pinned kyuz0 digest** — a llama.cpp master regression (#15406, build range 5630–5686) once returned 501 on `/v1/embeddings`; the digest pin protects the user, and D-06's install-time curl smoke against the *pinned digest* is the gate. (3) **conditional byte-identical render** — the two units + Qdrant volume must be appended to `Render()`'s output slice ONLY when `memory_enabled=true`, keeping a memory-off install byte-for-byte identical to v1.2.

**Primary recommendation:** Add `qdrantImage`/`QdrantImage()` + `embedImage`/`EmbedImage()` consts + accessors and `qdrant`/`embed` view builders to a new `internal/orchestrate/memory.go` (mirroring `openwebui.go`), add three templates (`qdrant.container.tmpl`, `qdrant.volume.tmpl`, `embed.container.tmpl`), thread the Phase-18 `memory.RenderView` into `RenderInput`, and conditionally append the three new units in `Render()` keyed on `Cfg.MemoryEnabled`. Pre-stage the GGUF via a catalog Shard (`url`/`filename`/`sha256`/`size_bytes` verified below) through `download.PullModel` into `villa-models`. Add a memory-stack install proof (offline `/v1/embeddings` 768-dim curl + Qdrant `/readyz` + writable probe) with refuse-with-remediation. Re-freeze the existing orchestrate goldens and add three new golden files.

**Primary recommendation (one-liner):** Mirror `openwebui.go` + its templates for `villa-qdrant` (UID-1000 unprivileged, `:Z`(+`:U`) durable named volume, no host port) and `villa-embed` (kyuz0 image, `--embeddings --pooling mean`, ctx 8192, GGUF mounted read-only from `villa-models`), gated on `memory_enabled`, with a pre-stage download + an offline 768-dim embeddings/Qdrant-writable install proof.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Qdrant + villa-embed image literals + view builders | orchestrate managed-service path (`internal/orchestrate/memory.go`, NEW) | — | Managed-service constants, same category as `openWebUIImage` — NOT inference-backend tokens (D-02/D-04/D-10), so outside `TestSeamGrepGate` scope. |
| Quadlet unit rendering (container + volume templates) | orchestrate (`render.go` + `quadlet/*.tmpl`) | — | `Render()` is the single PURE render point; the new units slot in after the OWUI units, conditional on `memory_enabled` (D-11). |
| Resolved memory values (model id, dim, addr/port) | pure core `internal/memory.RenderView` (Phase 18) → orchestrate | — | The recommend→orchestrate handoff already built; orchestrate composes URLs + adds image literals (D-10/D-11). |
| Embedding GGUF pre-staging | `internal/download.PullModel` + `internal/catalog` Shard | install cmd tier (`cmd/villa/install.go` `ensureModel` seam) | Reuse the proven HEAD-verify/resume/atomic-rename/SHA256 weight-pull path; the embed GGUF is a catalog Shard into the existing `villa-models` dir (D-07). |
| Conditional render gate | `internal/memory.Decide` + `Cfg.MemoryEnabled` | orchestrate `Render()` | Fail-closed enablement gate already exists; render keys the conditional append off `Cfg.MemoryEnabled` (and may validate via `Decide`). |
| Install-time start + readiness proof | install cmd tier (`cmd/villa/install.go`) + `orchestrate.Systemd` (`systemd.go`) | — | Lifecycle start/reconcile via the existing Systemd seam; HTTP smoke (`/v1/embeddings`, Qdrant `/readyz`) lives in the cmd/install layer (systemd.go deliberately holds no HTTP poll). |
| Loopback-only privacy audit | `internal/status.publishedPorts` (UNCHANGED) | — | Reads only `PublishPort=` lines; the two new units have none → vacuously loopback-only, SC#4 green with zero change (D-10). |

## Standard Stack

### Core
| Component | Version / Identity | Purpose | Why Standard |
|-----------|-------------------|---------|--------------|
| Qdrant (unprivileged image) | `qdrant/qdrant:v1.18.2-unprivileged` (manifest-list digest `sha256:b79aaa49ce7a7e5b7e9cf3fe76be400c911457084b4b7af47487c1c9ae5962e5`) | Local vector DB managed service (`villa-qdrant`) | Official Qdrant image; the *unprivileged* variant runs as non-root UID 1000 (SC#2). [VERIFIED: Docker Hub registry API + qdrant Dockerfile/release workflow] |
| kyuz0 Strix-Halo toolbox (reused) | `docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…ac7aad` | `villa-embed` llama-server (reuse the already-pinned chat image) | Already pinned + approved in v1.0 (`internal/inference/backend_vulkan.go`); no new embeddings image to audit (D-04/D-07). [VERIFIED: codebase] |
| `nomic-embed-text-v1.5` Q8_0 GGUF | `nomic-ai/nomic-embed-text-v1.5-GGUF` → `nomic-embed-text-v1.5.Q8_0.gguf` | The pre-staged embedding model | Pinned by Phase-18 D-08 (768-dim, 8192 ctx); Q8_0 best quality/size for a tiny model. [VERIFIED: HuggingFace HEAD — see Code Examples] |
| `internal/download.PullModel` | in-repo | One-time install-time GGUF fetch into `villa-models` | The proven HEAD-verify/resume/atomic/SHA256 weight-pull path; runner-agnostic (D-07). [VERIFIED: codebase] |
| `internal/orchestrate` managed-service path | in-repo (`openwebui.go` precedent) | Image consts + accessors + view builders + templates | The exact pattern for Qdrant/villa-embed (D-02/D-04/D-10). [VERIFIED: codebase] |

### Supporting
| Component | Identity | Purpose | When to Use |
|-----------|----------|---------|-------------|
| `internal/memory.RenderView` / `MemoryRenderInput` | in-repo (Phase 18) | Resolved-values handoff (model id, dim, addr/port; no image literal) | Thread into `RenderInput` so `Render()` consumes resolved values (D-11). [VERIFIED: codebase] |
| `internal/memory.Decide` | in-repo (Phase 18) | Fail-closed enablement-and-fields-valid gate | Optionally validate memory config before conditional render (refuse-with-reason). [VERIFIED: codebase] |
| `orchestrate.Systemd` (`systemd.go`) | in-repo | Start/enable/is-active/daemon-reload over the rootless user manager | Start the two new services; reuse `ErrToolNotFound`/`ErrCommandFailed` discipline for the proof's refuse-with-remediation. [VERIFIED: codebase] |
| Go stdlib `net/http` | Go 1.26 | Offline `/v1/embeddings` + Qdrant `/readyz` install probes | The smoke probe is a fixed loopback/container-reachable HTTP call (mirrors the existing `pollReady` readiness poll). [VERIFIED: codebase — `installReadiness`/`pollReady`] |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `qdrant/qdrant:v1.18.2-unprivileged` (D-01) | Default root `qdrant/qdrant` image | Default image runs as root → on a rootless named volume the in-container root maps to the host user (works), but the unprivileged UID-1000 variant is the explicit SC#2 choice and matches the privacy/least-privilege posture. Locked: unprivileged. |
| Pre-stage as a catalog Shard via `download.PullModel` (D-07) | llama.cpp `-hf` runtime pull | `-hf` pulls at *runtime* (violates PRIV-04) and bypasses the repo's owned resume/checksum/atomic guarantees. Locked: install-time `download` path. |
| GGUF on the shared `villa-models` volume (D-07) | A new dedicated embed-model volume | A second volume is needless — `villa-models` is the single model store; the embed GGUF is just another file in it, mounted read-only by `villa-embed`. Locked: reuse `villa-models`. |
| Two new image consts in `internal/orchestrate` (D-02/D-04) | Reference `inference.VulkanBackend().Image()` for villa-embed | Routing the embed image through the inference seam would conflate a managed-service image with a GPU-backend token and entangle `TestSeamGrepGate` semantics. Locked: orchestrate managed-service const (a SECOND const equal to the kyuz0 digest, mirroring how `openWebUIImage` is independent). |

**Installation:**
```bash
# No new Go packages. go.mod is UNCHANGED — Go is control-plane only.
# New container images are digest-pinned consts behind the orchestrate seam.
# Resolve the platform digest on the dev box (mirror the openWebUIImage method):
podman pull docker.io/qdrant/qdrant:v1.18.2-unprivileged && \
  podman image inspect docker.io/qdrant/qdrant:v1.18.2-unprivileged \
    --format '{{index .RepoDigests 0}}'
```

**Version verification:**
- Qdrant tag/digest verified via Docker Hub registry API 2026-06-09: `v1.18.2-unprivileged` → manifest-list `sha256:b79aaa49ce7a7e5b7e9cf3fe76be400c911457084b4b7af47487c1c9ae5962e5` (also tagged `v1.18-unprivileged`, `latest-unprivileged`, `v1-unprivileged`). [VERIFIED: hub.docker.com/v2 API]
- **Digest-pin nuance:** the registry-API digest above is the multi-arch **manifest list** digest. The repo's convention (see `openWebUIImage` comment) is to resolve the digest on the dev box via `podman image inspect … RepoDigests`, which yields the *platform-specific* digest for linux/amd64. The planner MUST resolve and pin the digest the dev box reports (it should match the linux/amd64 child of this manifest list) and record the resolution date in the const comment, exactly as `vulkanImage`/`openWebUIImage` do.
- nomic GGUF verified via HuggingFace HEAD 2026-06-09: `X-Linked-Size: 146146432` bytes, `X-Linked-Etag (git-LFS oid/sha256): 3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7`. [VERIFIED: huggingface.co HEAD]

## Package Legitimacy Audit

> The repo-wide legitimacy seam (`gsd-tools query package-legitimacy check`) covers npm/PyPI/crates only — NOT container images, and `go.mod` is unchanged this phase. Container-image legitimacy in this project is enforced by **digest-pinning + a manual provenance audit** (the documented `openWebUIImage`/`vulkanImage` discipline). The two images below are audited accordingly.

| Image | Registry | Provenance | Age / Currency | Digest-pinned | Verdict | Disposition |
|-------|----------|-----------|----------------|---------------|---------|-------------|
| `qdrant/qdrant:v1.18.2-unprivileged` | Docker Hub (official `qdrant/qdrant` org) | Official Qdrant publisher; built from `github.com/qdrant/qdrant` `Dockerfile` with `--build-arg USER_ID=1000` in the org's release workflow (verified) | v1.18.2 current as of 2026-06-09; `unprivileged` variant maintained back to v1.6 | YES (resolve on dev box, pin RepoDigest) | **OK** | Approved — pin digest, record resolution date + legitimacy note in the const comment (mirror `openWebUIImage`) |
| `docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…ac7aad` | Docker Hub | Already pinned + approved in v1.0 (`backend_vulkan.go`) | Auto-rebuilt from llama.cpp master; **the pinned digest is the protection** | YES (already) | **OK** (re-use) | Approved — reuse the existing pinned digest; a SECOND orchestrate const holds the same literal (D-04) |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

> **Audit caveat for the planner (`checkpoint:human-verify` warranted):** the **embed image digest** const must equal the *currently-pinned* `vulkanImage` digest, AND the **Qdrant digest** must be resolved on the dev box and confirmed to be the official `qdrant/qdrant` image before freezing. Add a `checkpoint:human-verify` task that (a) confirms `podman image inspect` on the dev box reports a `docker.io/qdrant/qdrant@sha256:…` RepoDigest matching the pinned const, and (b) curls `/v1/embeddings` against the pinned kyuz0 digest (D-06) to confirm the endpoint is not regressed before the unit is frozen.

## Architecture Patterns

### System Architecture Diagram

```
   user: villa install            config.toml (memory_enabled=true)     ── SINGLE SOURCE OF TRUTH
        │                                  │
        ▼                                  │ LoadVilla → normalizeVilla (self-heal)
   ┌─────────────────────────────┐         │
   │ cmd/villa/install.go        │◄────────┘
   │  detect→recommend→preflight │
   │  gate→render→reconcile→     │
   │  writeUnits→start→PROOF     │
   └───┬──────────────┬──────────┘
       │ (A) render    │ (B) pre-stage GGUF              │ (C) start + proof
       ▼               ▼                                 ▼
 ┌──────────────┐  ┌──────────────────────────┐   ┌──────────────────────────────┐
 │ orchestrate  │  │ internal/download        │   │ orchestrate.Systemd (start)  │
 │ .Render(in)  │  │ .PullModel(nomic Shard)  │   │  + HTTP install proof (D-09) │
 │  consumes    │  │  → villa-models/<gguf>   │   │  offline /v1/embeddings 768  │
 │  memory      │  └──────────────────────────┘   │  Qdrant /readyz + writable   │
 │  RenderView  │            (PRIV-04)             └──────────────────────────────┘
 │  (Phase 18)  │
 └───┬──────────┘
     │ appends 3 units ONLY when memory_enabled (D-11)
     ▼
 ┌─────────────────────────────────────────────────────────────────────────────┐
 │  Quadlet units written to ~/.config/containers/systemd/  (regenerated, D-11)  │
 │   villa-qdrant.container  ── Image=qdrant…-unprivileged (UID 1000)            │
 │                              Volume=villa-qdrant.volume:/qdrant/storage:Z(:U) │
 │                              NO PublishPort  (container-DNS only) ── SC#4      │
 │   villa-qdrant.volume     ── durable named volume                            │
 │   villa-embed.container   ── Image=kyuz0…  Exec=llama-server --embeddings     │
 │                              --pooling mean -c 8192 --host 0.0.0.0 --port 8080│
 │                              Volume=villa-models:/models:ro,z  NO PublishPort │
 └─────────────────────────────────────────────────────────────────────────────┘
                                   │ all join villa.network (container DNS)
                                   ▼
   Runtime (Phase 20 wires OWUI to these — NOT this phase):
     Open WebUI ──/v1/embeddings──► villa-embed:8080      (nomic-embed-text-v1.5)
     Open WebUI ──QDRANT_URI──────► villa-qdrant:6333     (vectors, 768-dim)
```

### Recommended Project Structure
```
internal/orchestrate/
├── openwebui.go            # EXISTING precedent (copy its shape)
├── memory.go               # NEW: qdrantImage/QdrantImage(), embedImage/EmbedImage()
│                           #      consts + accessors; qdrantView/qdrantVolumeView/
│                           #      embedView structs + build*View() builders
├── render.go               # EXTEND Render(): conditionally append the 3 memory units
│                           #      when in.Cfg.MemoryEnabled (consume in.Memory render-view)
├── orchestrate.go          # EXTEND RenderInput: add a MemoryRenderInput-derived field
└── quadlet/
    ├── qdrant.container.tmpl   # NEW (mirror openwebui.container.tmpl; NO PublishPort)
    ├── qdrant.volume.tmpl      # NEW (mirror openwebui.volume.tmpl — plain named volume)
    └── embed.container.tmpl    # NEW (container w/ Exec=, Volume=:ro,z, NO PublishPort)

cmd/villa/
└── install.go              # EXTEND: pre-stage embed GGUF (ensureModel-style seam),
                            #         start villa-qdrant + villa-embed, run memory proof
```

### Pattern 1: Qdrant managed-service render (mirror `openwebui.go`) — INFRA-01
**What:** Add a digest-pinned `const qdrantImage` + exported `QdrantImage()` accessor, stable unit-name/volume consts, a `qdrantView` (ContainerName/Image/Network/Volume — **no PublishPort, no Env**) and a `qdrantVolumeView` (VolumeName), and `buildQdrantView()`/`buildQdrantVolumeView()` builders. This is byte-for-byte the `openwebui.go` shape minus the env block and minus the publish port.
**When to use:** The Qdrant unit (D-01/D-02/D-03).
**Example:**
```go
// Source: mirror internal/orchestrate/openwebui.go (openWebUIImage + OpenWebUIImage()).
// Resolved on the dev box <date> via `podman pull … && podman image inspect …
// --format '{{index .RepoDigests 0}}'` (the :unprivileged tag is silently rebuilt;
// the digest is not). Legitimacy: official qdrant/qdrant org, USER_ID=1000 variant.
const qdrantImage = "docker.io/qdrant/qdrant:v1.18.2-unprivileged@sha256:<dev-box-resolved-digest>"

func QdrantImage() string { return qdrantImage }

const (
    qdrantContainerUnitName = "villa-qdrant.container"
    qdrantVolumeUnitName    = "villa-qdrant.volume"
    qdrantContainerName     = "villa-qdrant"     // stable container-DNS name
    qdrantVolumeName        = "villa-qdrant"
    // Durable storage mount: dedicated named volume → /qdrant/storage with the :Z
    // PRIVATE SELinux label (Fedora target). See Pitfall 1 for the :U addition.
    qdrantVolumeMount = qdrantVolumeName + ".volume:/qdrant/storage:Z"
    // NO PublishPort const — Qdrant is container-DNS only (D-03/D-10, SC#4).
)
```

### Pattern 2: villa-embed render (container with Exec + read-only model mount) — INFRA-02
**What:** A second digest-pinned `const embedImage` (equal to the pinned `vulkanImage` literal — a deliberate independent const per D-04), an `embedView` carrying `Exec` (the `llama-server …` arg string) and a read-only `Volume` mount of `villa-models`, **no PublishPort, no Env**. The kyuz0 image entrypoint accepts `llama-server` as its first exec token (proven by `backend_vulkan.go` line 95). The container internal port is 8080 with `--host 0.0.0.0`; only `villa.network` peers reach it (no host publish).
**When to use:** The villa-embed unit (D-04/D-05).
**Example:**
```go
// Source: mirror openwebui.go const block + backend_vulkan.go Exec shape.
const embedImage = "docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…ac7aad" // == vulkanImage (D-04)

func EmbedImage() string { return embedImage }

const (
    embedContainerUnitName = "villa-embed.container"
    embedContainerName     = "villa-embed"          // stable container-DNS name (D-05)
    embedContainerPort     = 8080                    // container-internal only (D-05)
    embedContextLen        = 8192                    // D-05/D-08
    // Model store mounted READ-ONLY (the GGUF is pre-staged by install). :z (lowercase)
    // matches the inference path's shared-models convention (backend_vulkan.go modelBind).
    embedModelMount = "villa-models:/models:ro,z"
)

// Exec is assembled from fixed tokens — NO shell interpolation (T-03-01). The model
// filename is the catalog-resolved GGUF base name joined onto /models.
func buildEmbedExec(ggufFilename string) string {
    // llama-server -m /models/<gguf> --embeddings --pooling mean -c 8192
    //   --host 0.0.0.0 --port 8080
    // (compare backend_vulkan.go: same --host 0.0.0.0 container-internal pattern)
    return strings.Join([]string{
        "llama-server",
        "-m", "/models/" + ggufFilename,
        "--embeddings", "--pooling", "mean",
        "-c", strconv.Itoa(embedContextLen),
        "--host", "0.0.0.0",
        "--port", strconv.Itoa(embedContainerPort),
    }, " ")
}
```
**Critical nuance:** `--embeddings` has a trailing **s**, and `/v1/embeddings` requires a pooling mode **other than `none`** (`mean` chosen per D-05). The embed unit does NOT need the chat path's GPU residency flags for correctness, but reusing `--no-mmap -fa 1 -ngl 999` is reasonable for a GPU-resident embedder — **planner's call**; the load-bearing flags are `--embeddings --pooling mean -c 8192`. The GGUF filename must match the pre-staged file (Pattern 3) — derive both from one source so they cannot drift.

### Pattern 3: Embedding-model pre-staging via the existing download path — PRIV-04
**What:** Define the nomic GGUF as a `catalog.CatalogModel` with a single `Shard{URL, Filename, SHA256, SizeBytes}` (values verified below) and pull it into the existing models dir via `download.PullModel(ctx, model, modelsDir)` at install — only when absent (idempotent), exactly like the chat-model `ensureModel`/`modelDownloaded` seams in `install.go`. The file lands at `<modelsDir>/nomic-embed-text-v1.5.Q8_0.gguf`; `villa-embed` mounts `villa-models` read-only at `/models` and runs `-m /models/nomic-embed-text-v1.5.Q8_0.gguf`.
**When to use:** Install-time, before starting `villa-embed` (D-07/PRIV-04/SC#3). Mirrors install.go step (6) "ensure model present BEFORE starting the unit (F-1)".
**Example:**
```go
// Source: internal/catalog Shard format (seed.json) + verified HF HEAD 2026-06-09.
var nomicEmbedShard = catalog.Shard{
    URL:       "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q8_0.gguf",
    Filename:  "nomic-embed-text-v1.5.Q8_0.gguf",
    SHA256:    "3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7", // X-Linked-Etag (git-LFS oid)
    SizeBytes: 146146432,                                                          // X-Linked-Size
}
// download.headVerify confirms X-Linked-Size/X-Linked-Etag match BEFORE pulling, then
// streams+hashes, verifies SHA256+size, and atomically renames (D-06). Runtime is then
// fully offline — PRIV-04 forbids a RUNTIME pull, not this install-time controlled fetch.
```
**Where to record the GGUF identity (planner's call):** the embed GGUF (model id, filename, dim 768) could live (a) as a new `internal/orchestrate` (or `internal/memory`) const set, or (b) as a catalog entry. Keep the **filename a single source of truth** shared by the pre-stage Shard and `buildEmbedExec` so the mounted path and the served `-m` path cannot drift (Pitfall 3). Note: it is **NOT** currently in `internal/catalog/seed.json` (verified) — the planner adds it.

### Pattern 4: Conditional render keyed on `memory_enabled` (byte-identical when off) — D-11/INFRA-04
**What:** `Render()` appends the three new units (`villa-qdrant.container`, `villa-qdrant.volume`, `villa-embed.container`) to its returned `[]Unit` ONLY when `in.Cfg.MemoryEnabled` is true. When false, the returned slice is byte-for-byte the current five-unit v1.2 output. The new units consume the resolved `memory.RenderView` values (no image literal in the input).
**When to use:** Always — this is the v1.2 continuity guarantee (Phase-18 SC#1 extends here).
**Example:**
```go
// Source: extend internal/orchestrate/render.go Render() after the OWUI units.
units := []Unit{
    {Name: containerUnitName, Text: containerText},
    {Name: networkUnitName, Text: networkText},
    {Name: volumeUnitName, Text: volumeText},
    {Name: openWebUIContainerUnitName, Text: owuiContainerText},
    {Name: openWebUIVolumeUnitName, Text: owuiVolumeText},
}
if in.Cfg.MemoryEnabled {
    mv := memory.RenderView(in.Cfg) // resolved values only (Phase 18)
    // qInfo/qVol/embed rendered from mv + the orchestrate image consts.
    units = append(units,
        Unit{Name: qdrantContainerUnitName, Text: qdrantText},
        Unit{Name: qdrantVolumeUnitName, Text: qdrantVolumeText},
        Unit{Name: embedContainerUnitName, Text: embedText},
    )
}
return units, nil
```
**Critical nuance:** `RenderInput.Cfg` is ALREADY passed into `Render()` (`install.go` line 318–323 sets `Cfg: cfg`), so the gate value is already in hand. The planner should thread the resolved `memory.RenderView` either by computing it inside `Render()` from `in.Cfg` or by adding a `RenderInput.Memory memory.MemoryRenderInput` field. Computing inside `Render()` from `in.Cfg` keeps `RenderInput` unchanged and is the smaller diff; either honors D-11 (resolved values, no image literal in the input).

### Anti-Patterns to Avoid
- **Putting either image literal in `internal/inference` or any caller outside `internal/orchestrate`:** `TestSeamGrepGate`'s `kyuz0|docker\.io/` pattern fires on the kyuz0/qdrant literals anywhere outside the inference seam *and* outside `cmd/villa`'s allowlist — but the gate's allowlist is `inference/` + `detect/gpu_amd.go` for `internal/`. **The new consts MUST live in `internal/orchestrate` (a non-seam, non-cmd package), so the gate does NOT scan orchestrate.** Confirm: the gate walks `internal/` and skips only the seam; orchestrate is already where `openWebUIImage` lives and `TestSeamGrepGate` is green today — so adding `qdrantImage`/`embedImage` next to `openWebUIImage` is provably safe (the OWUI image already proves orchestrate is outside the gate's effective reach because the gate's `kyuz0|docker.io/` pattern would otherwise already fail on `openwebui.go`… it does not, because **`openwebui.go`'s image is `ghcr.io/…`, not `docker.io/`**). **THIS IS A REAL RISK:** `embedImage = "docker.io/kyuz0/…"` and `qdrantImage = "docker.io/qdrant/…"` BOTH match `kyuz0|docker\.io/`. See Pitfall 7 — the gate's `internal/` walk does NOT exclude `orchestrate/`, so these literals in `internal/orchestrate/memory.go` **WILL trip `TestSeamGrepGate`**. The planner MUST extend the gate's seam allowlist to include `orchestrate/memory.go` (or the managed-service image file), in the SAME commit, with an explanatory comment — mirroring how the ROCm tags were added to the gate in one commit (12-02).
- **Publishing a host port for either service:** breaks SC#4 and the privacy posture. Neither template emits `PublishPort=`. `villa-embed` uses `--host 0.0.0.0` (container-internal) with NO `-p` host publish — distinct from the chat path which DOES loopback-publish.
- **Mounting `villa-models` read-write into `villa-embed`:** the embedder only reads the GGUF; mount `:ro,z` (Pattern 2). A writable mount is an unnecessary tampering surface.
- **Hand-editing a Quadlet unit as the authority:** units regenerate from config (INFRA-04). The `# GENERATED — do not edit` header is already the template convention.
- **Truncating nomic via Matryoshka to <768:** dimension-skew hazard; pin 768 (D-08, Pitfall 8).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Model/GGUF download (resume, checksum, atomic) | A new fetcher for the embed GGUF | `internal/download.PullModel` + a `catalog.Shard` | Proven HEAD-verify/Range-resume/SHA256/atomic-rename; signed-URL-leak-safe (D-07). |
| Quadlet unit rendering | A bespoke string builder for the new units | `text/template` + `execTemplate` + the `openwebui.*.tmpl` precedent | The render pipeline + goldens already exist; copy the OWUI template shape. |
| Container image identity | Re-typing the digest in cmd/install or a caller | `orchestrate.QdrantImage()` / `EmbedImage()` accessors | Single source of truth behind the seam; mirrors `OpenWebUIImage()` (D-02/D-04). |
| systemd start/enable/is-active | A new exec wrapper | `orchestrate.Systemd` (`Start`/`Enable`/`IsActive`/`DaemonReload`) | Fixed-arg, typed `ErrToolNotFound`/`ErrCommandFailed`, injectable for tests. |
| Loopback/privacy audit | A new audit for the two services | `internal/status.publishedPorts` (UNCHANGED) | It reads only `PublishPort=` lines; no host port → vacuously loopback-only (SC#4). |
| Config write safety | A new writer for memory fields | `SaveVilla`/`marshalVilla` (already memory-aware, drop-when-off) | XDG-confined, 0600, traversal-guarded, byte-identical when off — already shipped in Phase 18. |
| Local vector store | An embedded/cgo vector store in Go | Qdrant managed Quadlet unit | Breaks the single static CGO-free binary; explicitly out of scope (PROJECT.md). |
| Local embeddings engine | A Go embeddings engine | `llama-server --embeddings` in the kyuz0 image | Go is control-plane only; reuse the pinned image (D-04/D-07). |

**Key insight:** Phase 19 is almost entirely *composition of existing, tested seams* — the only genuinely new code is two image consts + three small view builders + three templates + the conditional append + the pre-stage Shard + the install proof. Every "hard" part (download integrity, atomic unit writes, systemd lifecycle, privacy audit, byte-identical config) is already shipped and reused unchanged.

## Runtime State Inventory

> This is a service-introduction phase (not a rename/refactor), but it DOES create new durable runtime state — inventoried here so the planner and Phase 23 (backup/restore) account for it.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | NEW: the `villa-qdrant` named volume holds Qdrant collections/vectors at `/qdrant/storage` (created empty this phase; vectors land in Phase 20/21). NEW: the pre-staged `nomic-embed-text-v1.5.Q8_0.gguf` file in the `villa-models` volume. | Create the durable `villa-qdrant.volume`; pre-stage the GGUF. The **768 dim** is recorded on the rendered service for the Phase-23 backup manifest / swap guard (D-08). |
| Live service config | NEW: `villa-qdrant.container`, `villa-qdrant.volume`, `villa-embed.container` Quadlet units in `~/.config/containers/systemd/`. Regenerated from config (D-11), never hand-edited. | Render + reconcile + write via orchestrate. `QDRANT_API_KEY` env is NOT set this phase (empty on the private net; Phase-20 OWUI-facing choice). |
| OS-registered state | NEW: two systemd `--user` services (`villa-qdrant.service`, `villa-embed.service`) via Quadlet `.container`→`.service` mapping; `[Install] WantedBy=default.target` + existing user lingering give reboot survival (SC#2). | `systemctl --user enable` + `start` the two services (mirror the OWUI start); lingering already enabled by v1.0 install. |
| Secrets/env vars | None written this phase. `QDRANT_API_KEY` recorded as a future env key (empty on `villa.network`; A2 from Phase 18). No new SOPS/.env. | none |
| Build artifacts | New `internal/orchestrate/memory.go` + three `quadlet/*.tmpl` compile into the single binary; three new golden files added. | `make build` / `make check`; re-freeze goldens (see Validation Architecture). |

**Nothing requiring data migration** — verified: this phase creates fresh empty state (a new Qdrant volume + a pre-staged read-only GGUF); no existing record is renamed or re-keyed. Vector data does not exist until Phase 20/21.

## Common Pitfalls

### Pitfall 1: Qdrant write failure on the rootless named volume (SC#2 keystone)
**What goes wrong:** `villa-qdrant` starts but cannot write `/qdrant/storage` → crash-loop or read-only DB; SC#2 (writable + survives reboot) fails.
**Why it happens:** The unprivileged image runs as UID:GID **1000:1000** (`USER_ID=1000` build arg, verified in the qdrant release workflow). In rootless Podman, container UID 1000 maps to a *subuid* on the host (not the host user). A bind mount would appear root-owned inside the namespace; a **named volume** is the right choice because Podman initializes an empty named volume from the image directory's contents AND ownership on first mount — and the Qdrant Dockerfile `chown`s `/qdrant/storage` to 1000:1000 at build, so the fresh volume inherits 1000:1000 and the in-container UID-1000 process can write. SELinux on Fedora additionally requires a relabel.
**How to avoid:** (1) Use a **named volume** (D-03), never a bind mount, for `/qdrant/storage`. (2) Apply the **`:Z`** SELinux private-relabel (Fedora target; matches the OWUI `:Z` convention). (3) If a permission failure still surfaces on the dev box, add the **`:U`** flag (`…:Z,U` or `…:U`) so Podman recursively chowns the volume to the container user's mapped UID — this is the documented belt-and-suspenders fix for non-root container users on named volumes. (4) The install proof (D-09) MUST actively assert writability (e.g. create-collection / `PUT` a tiny collection, or check `/readyz` AND a write), not merely that the service is `active`.
**Warning signs:** Qdrant logs "permission denied" on `/qdrant/storage`; `villa-qdrant.service` flaps; `/readyz` 503s; collections vanish after restart.

### Pitfall 2: `/v1/embeddings` returns 501/400 on the pinned image (the #15406 carry-forward)
**What goes wrong:** `villa-embed` is `active` but `/v1/embeddings` returns `501 not supported` (or wrong-length/empty vectors) — a silent memory-stack break.
**Why it happens:** (a) wrong flag — it is `--embeddings` (trailing **s**), and `/v1/embeddings` requires `--pooling` ≠ `none`; (b) the kyuz0 image is auto-rebuilt from llama.cpp **master**, and a master regression (issue #15406, build range 5630–5686, now Closed) returned 501 on `/v1/embeddings` despite `--embeddings`.
**How to avoid:** (1) Use `--embeddings --pooling mean` explicitly (D-05). (2) **Curl `/v1/embeddings` against the PINNED kyuz0 digest** before freezing the unit (D-06) — the digest pin is what protects the user from a future regressed rebuild; the install proof's offline 768-dim smoke is the gate. (3) If the pinned digest is regressed, the planner pins a different (older/known-good) kyuz0 digest for the embed const — it need not equal the chat digest, but D-04 prefers reuse; reuse only if the curl passes.
**Warning signs:** `/v1/embeddings` 501/400; `data[0].embedding` length ≠ 768; embeddings work on `/embeddings` but not `/v1/embeddings`.

### Pitfall 3: GGUF path drift between the pre-stage file and the `-m` mount
**What goes wrong:** `villa-embed` starts with `-m /models/<wrong-name>.gguf` and crash-loops on a missing file, or the pre-stage writes a different filename than the unit expects.
**Why it happens:** The pre-stage Shard `Filename` and `buildEmbedExec`'s `-m` path are two separate literals that can diverge.
**How to avoid:** Make the GGUF filename a **single source of truth** consumed by both the pre-stage Shard and the Exec builder. Mount `villa-models` read-only at `/models` (matching the chat path's `/models` convention in `backend_vulkan.go`), and the Exec uses `/models/<that-filename>`. Ensure the pre-stage runs BEFORE `villa-embed` starts (mirror install.go step 6 "ensure model present BEFORE starting the unit").
**Warning signs:** `villa-embed.service` flaps; llama-server logs "failed to load model" / file-not-found.

### Pitfall 4: Embed unit started before the GGUF is staged → crash-loop + WARN
**What goes wrong:** Install renders/starts `villa-embed` but the GGUF was not yet pulled → the service crash-loops and install reports WARN/FAIL.
**Why it happens:** Order-of-operations: the pre-stage must precede the start (and precede the readiness proof).
**How to avoid:** Replicate the existing install ordering: ensure model present (step 6) → write units → daemon-reload → start. The embed pre-stage is a NEW "ensure present" step gated on `memory_enabled` and on the file's absence (idempotent, strictly-local — never re-pulled). Under `--dry-run`, pull nothing (the existing dry-run contract).
**Warning signs:** install prints a start failure for `villa-embed`; readiness proof times out.

### Pitfall 5: The offline proof doesn't actually prove "offline"
**What goes wrong:** The `/v1/embeddings` smoke passes but the embedder silently reached out to HuggingFace, so PRIV-04 is not actually proven.
**Why it happens:** llama-server with a local `-m` path does NOT fetch from HF (unlike OWUI's built-in embedder), so for `villa-embed` the risk is low — but the *proof* must be honest (offload-asserting discipline: a silent skip is a FAIL, not a false-green).
**How to avoid:** The proof asserts (a) a real `/v1/embeddings` request returns a **768-length** float vector, and (b) it succeeds with the GGUF already on disk (no runtime pull path is exercised). Because `villa-embed` reads a local file and the kyuz0 image is not given an HF token or network model id, a successful embedding inherently proves the local-only path. The stronger Phase-20 firewalled zero-outbound document-upload smoke (PRIV-05) is the runtime-network proof; Phase 19's proof is the *service-level* offline-answer proof. Keep the two distinct and do not over-claim.
**Warning signs:** the proof passes on a machine that secretly had network and would fail when firewalled — guard by asserting the vector length + a fixed deterministic input, and by NOT relying on any model-id-driven lazy load.

### Pitfall 6: SC#4 false alarm — the privacy audit and the new units
**What goes wrong:** A reviewer fears the two new services breach loopback-only (SC#4).
**Why it happens:** Misreading how the audit works.
**How to avoid:** `internal/status.publishedPorts` parses ONLY `PublishPort=` lines from the generated container units (it deliberately never reads `Exec=`). Neither new unit emits `PublishPort=`, so they contribute zero bindings; `allLoopback` is vacuously true for them and `LoopbackOnly` stays true. **No change to the audit is needed** (D-10). The embed unit's `--host 0.0.0.0` is on the `Exec=` line (container-internal only) and is correctly ignored by the audit. Add a test asserting the rendered memory units contain no `PublishPort=`.
**Warning signs:** none expected; a regression would be a stray `-p`/`PublishPort=` in a memory template.

### Pitfall 7: The new image literals trip `TestSeamGrepGate`
**What goes wrong:** Adding `qdrantImage = "docker.io/qdrant/…"` and `embedImage = "docker.io/kyuz0/…"` to `internal/orchestrate/memory.go` fails `TestSeamGrepGate` — its `container image literal` pattern is `kyuz0|docker\.io/|…`, and the `internal/` walk's seam allowlist is ONLY `inference/` + `detect/gpu_amd.go`. `internal/orchestrate` is NOT in the allowlist, so both `docker.io/` literals match outside the seam and FAIL.
**Why it happens:** The existing `openWebUIImage` does NOT trip the gate because it is `ghcr.io/…` (not `docker.io/`) and contains no `kyuz0`. The Qdrant (`docker.io/`) and embed (`docker.io/kyuz0/`) literals DO match the pattern.
**How to avoid:** In the SAME commit that adds the consts, extend `TestSeamGrepGate`'s `isSeam` allowlist (the `internal/` walk) to include the orchestrate managed-service image file (e.g. `orchestrate/memory.go`), with a comment mirroring the D-10 rationale ("managed-service image, not a GPU-backend token") — exactly how the ROCm tags were added to the gate in one commit (12-02). Confirm `TestROCmMarkerPresence` and the cmd-tier walk are unaffected (the cmd walk has no Qdrant/embed literal). **This is a required, non-obvious task** — do not assume orchestrate is already outside the gate.
**Warning signs:** `go test ./internal/inference/ -run TestSeamGrepGate` fails with "seam leak in orchestrate/memory.go".

### Pitfall 8: Embedding-dimension skew (carried from Phase 18)
**What goes wrong:** A later model/dim change against an existing Qdrant collection produces wrong-length vectors → broken retrieval, no error.
**Why it happens:** Qdrant collections are created with a fixed vector size; nomic-embed-text-v1.5 is Matryoshka (768/512/256/128/64).
**How to avoid:** Pin 768 (D-08); record it on the rendered service for the Phase-23 swap guard. Phase 19 does not create collections, but it freezes the served model+dim that Phase 20/21 will create collections against.
**Warning signs:** (future) retrieval collapse after a model/dim change; Qdrant vector-size-mismatch insert errors.

## Code Examples

### Qdrant container template (mirror `openwebui.container.tmpl`, NO PublishPort) — INFRA-01
```gotemplate
# ~/.config/containers/systemd/villa-qdrant.container  (GENERATED — do not edit; source: config.toml)
[Unit]
Description=VillaStraylight Qdrant vector store
After=villa-network.service

[Container]
ContainerName={{.ContainerName}}
Image={{.Image}}
Network={{.Network}}
Volume={{.Volume}}

[Service]
Restart=on-failure

[Install]
WantedBy=default.target
```
Notes: NO `PublishPort=` (container-DNS only, SC#4/D-10). `{{.Volume}}` = `villa-qdrant.volume:/qdrant/storage:Z` (add `,U` if the dev-box proof shows a write failure — Pitfall 1). `{{.Network}}` = `villa.network`. No `Env=` block (Qdrant runs its defaults; `QDRANT_API_KEY` is a Phase-20 choice).

### villa-embed container template (Exec + read-only model mount, NO PublishPort) — INFRA-02
```gotemplate
# ~/.config/containers/systemd/villa-embed.container  (GENERATED — do not edit; source: config.toml)
[Unit]
Description=VillaStraylight embeddings (llama-server /v1/embeddings)
After=villa-network.service

[Container]
ContainerName={{.ContainerName}}
Image={{.Image}}
Network={{.Network}}
Volume={{.Volume}}
Exec={{.Exec}}

[Service]
Restart=on-failure

[Install]
WantedBy=default.target
```
Notes: `{{.Volume}}` = `villa-models:/models:ro,z` (read-only). `{{.Exec}}` = `llama-server -m /models/nomic-embed-text-v1.5.Q8_0.gguf --embeddings --pooling mean -c 8192 --host 0.0.0.0 --port 8080`. NO `PublishPort=` — `--host 0.0.0.0` is container-internal; only `villa.network` peers reach `villa-embed:8080`.

### Qdrant durable named-volume template (mirror `openwebui.volume.tmpl`) — INFRA-01
```gotemplate
# ~/.config/containers/systemd/villa-qdrant.volume  (GENERATED — do not edit; source: config.toml)
[Volume]
VolumeName={{.VolumeName}}
Driver=local

[Install]
WantedBy=default.target
```
Notes: a plain podman-managed named volume (Driver=local; NO `Type=none`/`Device=`/`Options=bind` — that bind-mount form is only for the host-path `villa-models.volume`). Durability = named volume + user lingering (SC#2). `{{.VolumeName}}` = `villa-qdrant`.

### Pre-stage Shard (verified HF metadata) — PRIV-04
```go
// Source: HuggingFace HEAD 2026-06-09 (X-Linked-Size / X-Linked-Etag).
var nomicEmbedShard = catalog.Shard{
    URL:       "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q8_0.gguf",
    Filename:  "nomic-embed-text-v1.5.Q8_0.gguf",
    SHA256:    "3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7",
    SizeBytes: 146146432,
}
// Pull via download.PullModel(ctx, CatalogModel{Shards:[]Shard{nomicEmbedShard}}, modelsDir)
// only when the file is absent (idempotent; runtime stays offline — PRIV-04).
```

### Offline `/v1/embeddings` install proof (768-dim) — D-09 / SC#3
```bash
# The install proof issues this against villa-embed (container-DNS) after start, with
# the GGUF already on disk and no runtime pull. A 768-length float vector = PASS.
curl -sf http://villa-embed:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{"input":"villa memory readiness probe","model":"nomic-embed-text-v1.5","encoding_format":"float"}' \
  | jq '.data[0].embedding | length'   # MUST equal 768 (D-08)
```
Notes (Go-side): the proof is a fixed `net/http` POST mirroring the existing `pollReady`/`installReadiness` seam; it asserts `len(data[0].embedding) == cfg.EmbeddingDim` (768). A failure (non-200, wrong length, or unreachable) → refuse-with-remediation (reuse `ErrToolNotFound`/`ErrCommandFailed`-style typed degradation), never a silent skip (honesty-by-construction). The probe runs from the install host — if `villa-embed` is only reachable by container DNS, the proof can curl from inside the network (e.g. via a one-shot `podman run --network villa.network`) or assert through Qdrant/OWUI reachability; **planner's call** on the exact reachability mechanism, but the assertion (768-length vector, offline) is fixed.

### Qdrant writable/readiness probe — D-09 / SC#2
```bash
# Readiness:
curl -sf http://villa-qdrant:6333/readyz        # 200 = ready
# Writable assert (create + delete a probe collection, 768-dim to match the pin):
curl -sf -X PUT http://villa-qdrant:6333/collections/villa-probe \
  -H 'Content-Type: application/json' \
  -d '{"vectors":{"size":768,"distance":"Cosine"}}'   # 200 = writable (SC#2)
curl -sf -X DELETE http://villa-qdrant:6333/collections/villa-probe
```
Notes: a successful create proves the `:Z`(+`:U`) named volume is writable by UID 1000 (Pitfall 1). The probe collection is deleted so no stray state remains. **Planner's call** whether to use a collection round-trip or a lighter writable assertion, but `/readyz`-only is insufficient for SC#2 ("writable") — assert an actual write.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Default root `qdrant/qdrant` image | `…-unprivileged` (UID 1000) variant | maintained since v1.6-unprivileged | Non-root container; SC#2 chooses the unprivileged variant for least-privilege + clean named-volume ownership. |
| llama.cpp `/v1/embeddings` always-on | `--pooling` ≠ `none` required; #15406 master regression (5630–5686) | mid-2025 | Pin a known-good kyuz0 digest + curl `/v1/embeddings` before freezing (D-06). |
| Runtime model pull (`-hf` / OWUI HF lazy-download) | Install-time pre-stage of a local GGUF into `villa-models` | this milestone | PRIV-04: zero runtime download. |

**Deprecated/outdated:**
- `-hf` runtime model resolution for the embedder: violates PRIV-04; use the install-time `download` path.
- Matryoshka-truncating nomic below 768: dimension-skew hazard; pin 768.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | A fresh rootless named volume inherits the image dir's UID-1000 ownership, so Qdrant-unprivileged can write `/qdrant/storage` with `:Z` (no `:U` needed in the common case) | Pitfall 1 / Code Examples | Medium — if the dev box shows a permission failure, the documented fix (`:U` to recursively chown to the mapped user) resolves it; the install proof (D-09) catches it before declaring success, so it cannot silently ship broken. **Verify on the dev box.** |
| A2 | The pinned kyuz0 digest currently serves a working `/v1/embeddings` (not in the #15406 regressed range) | Pitfall 2 / D-06 | Medium — the curl-against-pinned-digest gate (D-06) is precisely the check; if regressed, pin a different known-good kyuz0 digest for the embed const. **Verify on the dev box before freezing.** |
| A3 | `QDRANT_API_KEY` can be empty on the private `villa.network` (no auth surface) | Runtime State / Security | Low — strictly-local single-user posture; a generated key can be added later without schema change (Phase-18 A2; Phase-20 OWUI-facing choice). |
| A4 | The `~512 MiB` embed footprint reservation is conservative (over-reserves) | (carried from Phase 18 D-08) | Low — over-reserves, never under-reserves. Phase 19 should MEASURE the resident GTT delta when `villa-embed` is up and feed the refined number to Phase 22 (CTRL-01). |
| A5 | The Qdrant `manifest-list` digest from the registry API will resolve to a matching linux/amd64 RepoDigest on the dev box | Standard Stack / Audit | Low — the repo convention pins the dev-box-resolved RepoDigest regardless; the API digest is documentation, not the frozen value. |

**Note:** A1 and A2 are the two on-hardware verifications that gate freezing the units — both are caught by the D-09 install proof, so neither can silently ship a broken stack. A `checkpoint:human-verify` (Audit section) covers both.

## Open Questions

1. **villa-embed reachability for the install proof**
   - What we know: `villa-embed` is container-DNS only (no host port); the existing `pollReady` polls a host-loopback endpoint (the chat path DOES publish loopback).
   - What's unclear: the cleanest way to reach `villa-embed:8080/v1/embeddings` from the install host to run the proof, given no host bind.
   - Recommendation: run the proof from inside `villa.network` (a one-shot `podman run --rm --network villa.network <small-image> curl …`), or assert the embed endpoint indirectly. Container-DNS-only is non-negotiable (D-05/D-10); do NOT add a host publish just to test. **Planner picks the mechanism; the assertion (768-length, offline) is fixed.**

2. **Where the embed GGUF identity lives (const set vs catalog entry)**
   - What we know: it is NOT in `internal/catalog/seed.json` today (verified); the Shard format is known and verified.
   - What's unclear: whether to add it as a catalog model or as an orchestrate/memory const set.
   - Recommendation: keep the **filename a single source of truth** shared by the pre-stage Shard and the embed Exec (Pitfall 3); the storage location (const vs catalog) is the planner's call. A non-catalog const avoids polluting the chat-model catalog/recommend math.

3. **Embed unit GPU residency flags**
   - What we know: `--embeddings --pooling mean -c 8192` are load-bearing; the chat path adds `--no-mmap -fa 1 -ngl 999 -lv 4 --metrics`.
   - What's unclear: whether the embedder needs GPU offload flags for the install proof / for Phase-22 footprint measurement.
   - Recommendation: planner's call; `-ngl 999 --no-mmap` make the embedder GPU-resident (matches the ~512 MiB GTT reservation in D-08) and let Phase 22 measure a real GTT delta. Not required for `/v1/embeddings` correctness.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | building orchestrate/memory + templates | ✓ (repo builds in CI) | 1.26.x | — |
| rootless Podman v5 + user manager | rendering/starting the two Quadlet services (runtime, on the Fedora dev box) | host-dependent (not on this researcher box) | — | — (install-time host requirement; preflight gating is Phase 22) |
| `qdrant/qdrant:v1.18.2-unprivileged` image | `villa-qdrant` | pullable from Docker Hub (verified tag/digest exists) | v1.18.2 | pin an earlier `-unprivileged` digest if v1.18.2 misbehaves |
| kyuz0 toolbox image (pinned) | `villa-embed` | already pinned in v1.0 | vulkan-radv@sha256:9a74e555… | pin a known-good kyuz0 digest if `/v1/embeddings` is regressed (D-06) |
| nomic Q8_0 GGUF | pre-stage / `villa-embed` | reachable on HuggingFace (HEAD verified) | Q8_0 (146146432 bytes) | F16 (274 MB) acceptable per D-08 if Q8_0 unavailable |
| `curl`/`jq` (or Go `net/http`/`encoding/json`) | install proof (D-09) | Go stdlib path preferred (no external tool) | — | Go `net/http` (no `curl` dependency) |

**Missing dependencies with no fallback:** none at code-build time. The Podman/image/GGUF availability is an install-time host concern verified on the Fedora dev box during execution (and gated by preflight in Phase 22).
**Missing dependencies with fallback:** F16 GGUF as a fallback quant; an earlier Qdrant unprivileged digest; an alternate known-good kyuz0 digest.

## Validation Architecture

> `nyquist_validation: true` — REQUIRED. The render is PURE and golden-frozen; the install proof and config gate are table-testable off-hardware via the existing injectable seams.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven; byte-for-byte golden fixtures; no third-party assert/mock) |
| Config file | none — `go test` convention |
| Quick run command | `go test ./internal/orchestrate/ ./internal/memory/ ./internal/config/ -count=1` |
| Full suite command | `make check` (vet + `go test ./...`, includes `TestSeamGrepGate`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| INFRA-01 | `Render()` with `memory_enabled=true` emits `villa-qdrant.container` (digest-pinned image, `:Z` named volume, NO `PublishPort=`) byte-for-byte matching a new golden | golden | `go test ./internal/orchestrate/ -run TestRenderQdrant -x` | ❌ Wave 0 (+ new `villa-qdrant.container.golden`, `villa-qdrant.volume.golden`) |
| INFRA-02 | `Render()` emits `villa-embed.container` with `Exec=llama-server … --embeddings --pooling mean -c 8192`, `:ro,z` model mount, NO `PublishPort=` | golden | `go test ./internal/orchestrate/ -run TestRenderEmbed -x` | ❌ Wave 0 (+ new `villa-embed.container.golden`) |
| INFRA-01/02 (D-11) | `Render()` with `memory_enabled=false` is BYTE-IDENTICAL to the current 5-unit v1.2 output (no memory units appended) | golden/regression | `go test ./internal/orchestrate/ -run TestRenderByteIdenticalWhenMemoryOff -x` | ❌ Wave 0 (existing 5 goldens UNCHANGED prove this) |
| INFRA-01 (SC#4) | The rendered memory units contain no `PublishPort=` line (privacy audit stays vacuously loopback-only) | unit | `go test ./internal/orchestrate/ -run TestMemoryUnitsNoPublishPort -x` | ❌ Wave 0 |
| INFRA-01 (constraint) | `TestSeamGrepGate` stays green AFTER the seam allowlist is extended for `orchestrate/memory.go` | unit (existing, extended) | `go test ./internal/inference/ -run TestSeamGrepGate -x` | ✅ exists (extend allowlist — Pitfall 7) |
| PRIV-04 | The pre-stage uses the verified nomic Shard (size 146146432, sha256 3e243421…); `download` HEAD-verify + SHA256 + atomic rename pass against an httptest fixture | unit | `go test ./internal/download/ -run TestPreStageNomic -x` (or reuse existing download tests with the new Shard) | ⚠️ existing download tests cover the mechanism; add a Shard-value test |
| INFRA-04 / D-11 | `Render()` consumes resolved `memory.RenderView` values (model id/dim/addr/port) with NO image literal in the input | unit | `go test ./internal/orchestrate/ -run TestRenderConsumesMemoryView -x` | ❌ Wave 0 |
| SC#2 / SC#3 (D-09) | install proof: a 768-length embedding response → PASS; a non-200 / wrong-length → refuse-with-remediation (table-driven via the injectable proof seam) | unit | `go test ./cmd/villa/ -run TestInstallMemoryProof -x` | ❌ Wave 0 (new install proof seam, stubbed like `pollReady`) |
| SC#3 (D-11) | install renders/starts the two new services ONLY when `memory_enabled=true` (stubbed start seam asserts call set) | unit | `go test ./cmd/villa/ -run TestInstallMemoryServices -x` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/orchestrate/ ./internal/memory/ ./cmd/villa/ -count=1`
- **Per wave merge:** `make check` (vet + full suite, includes `TestSeamGrepGate` + all goldens)
- **Phase gate:** Full suite green + the on-hardware D-09 install proof (offline 768-dim `/v1/embeddings` + Qdrant writable) PASS on the Fedora dev box before `/gsd-verify-work`. CGO-free static build stays green.

### Wave 0 Gaps
- [ ] `internal/orchestrate/testdata/villa-qdrant.container.golden` — new (INFRA-01)
- [ ] `internal/orchestrate/testdata/villa-qdrant.volume.golden` — new (INFRA-01)
- [ ] `internal/orchestrate/testdata/villa-embed.container.golden` — new (INFRA-02)
- [ ] `internal/orchestrate/*_test.go` — add TestRender{Qdrant,Embed}, ByteIdenticalWhenMemoryOff, MemoryUnitsNoPublishPort, ConsumesMemoryView
- [ ] Extend `internal/inference/seam_test.go` `isSeam` allowlist for `orchestrate/memory.go` (Pitfall 7) — SAME commit as the consts
- [ ] `cmd/villa/install_test.go` — add memory-proof + memory-services-conditional cases (new injectable proof seam, stubbed like `pollReady`/`ensureModel`)
- [ ] `internal/download` or `internal/catalog` — a test asserting the nomic Shard values (size/sha256) so a typo is caught
- [ ] Re-freeze: the existing 5 orchestrate goldens MUST remain UNCHANGED (they prove byte-identical-when-off) — confirm no incidental drift. Use `go test … -update` ONLY for the three NEW goldens, intentionally.

## Security Domain

> `security_enforcement: true` — included. ASVS L1; block on high. This phase's surface is two new container services (container-DNS only), a durable named volume, a one-time install-time model fetch, and an install-time HTTP proof.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | partial | No auth surface added; `villa-qdrant`/`villa-embed` are container-DNS only on the private `villa.network`. `QDRANT_API_KEY` may be empty on the private net (A3) or generated later (Phase 20) — no auth boundary is exposed to the host/LAN. |
| V3 Session Management | no | n/a |
| V4 Access Control | yes | Unprivileged Qdrant (UID 1000) — least privilege; model store mounted `:ro,z` into `villa-embed` (no write surface). Unit files written via `WriteUnits` (traversal-guarded, atomic). |
| V5 Input Validation | yes | Config gated by `memory.Decide` (fail-closed); Exec assembled from fixed tokens, NO shell interpolation (T-03-01); GGUF filename catalog/config-resolved, never interpolated. |
| V6 Cryptography | yes | Pre-stage integrity = SHA256 (git-LFS oid) + size verify + atomic rename (`download`); image identity = digest pin. No hand-rolled crypto. |
| V10 Malicious Code | yes | Digest-pinned images (Qdrant + kyuz0); manual image-provenance audit; pre-stage SHA256-verified against the catalog oid (re-upload caught by HEAD X-Linked-Etag check). |
| V12 File & Resources | yes | Quadlet units written traversal-guarded + atomic (`reconcile.go assertInsideDir`); durable named volume (not a host bind) confines Qdrant storage. |
| V14 Configuration | yes | Units regenerated from config (INFRA-04); byte-identical when memory off (D-11); loopback/container-DNS-only posture preserved; no telemetry from first-party components. |

### Known Threat Patterns for {Qdrant + llama-server + Quadlet + install-pull} stack
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Widening a service bind to a routable interface | Information Disclosure | NO `PublishPort=` on either unit; container-DNS only (D-03/D-05/D-10). `internal/status` audit stays green (Pitfall 6). |
| Shell interpolation in a rendered Exec/arg | Tampering / Injection | Exec built from fixed `[]string` tokens joined deterministically; model filename catalog/config-resolved; no `%s`-into-shell (T-03-01). |
| Malicious / re-uploaded embed GGUF | Tampering | `download.headVerify` (X-Linked-Etag/Size) + post-download SHA256+size against the pinned catalog oid; atomic rename — never a half-written/unverified model on disk. |
| Slopsquatted / drifted container image | Tampering / Spoofing | Digest-pinned consts behind the orchestrate seam; manual provenance audit (official `qdrant/qdrant`, pinned kyuz0); re-audit-on-bump enforced by golden re-freeze. |
| Qdrant write failure → false-green install | Denial of Service / integrity | Active writable probe (create+delete collection) in the install proof (D-09/SC#2) — a silent skip is a FAIL (honesty-by-construction). |
| `/v1/embeddings` regression shipping silently (#15406) | DoS / integrity | Digest pin + offline 768-dim curl smoke against the pinned digest before freezing (D-06). |
| Runtime model exfil/download (OWUI HF lazy path) | Information Disclosure | Dedicated local `villa-embed` reading a pre-staged local GGUF — no HF runtime fetch (PRIV-04). The OWUI offline-lockdown env is Phase 20. |
| Container image literal leaking into a backend-neutral caller | (architectural integrity) | Literals confined to `internal/orchestrate/memory.go`; `TestSeamGrepGate` extended to keep them in the managed-service seam (Pitfall 7). |

**Phase-19 security note for each PLAN's `<threat_model>` (carry forward from Phase 18):** (1) config write safety is reused unchanged (0600/0700/traversal/no-shell-interpolation, `marshalVilla` byte-identical when off); (2) all memory endpoints are container-DNS/loopback — **never widen a bind**, no `PublishPort=` on either new unit; (3) image identities are digest-pinned behind the orchestrate seam with a manual provenance audit; (4) the embed GGUF is SHA256+size verified at install and runtime is offline (PRIV-04); (5) `QDRANT_API_KEY` may be empty on the private net (A3). ASVS L1, block on high.

## Sources

### Primary (HIGH confidence)
- Codebase (verified this session): `internal/orchestrate/openwebui.go` (managed-service image const + accessor + view + `:Z` volume precedent), `render.go` (Render pipeline, conditional-append point, `parseContainerArgs`), `reconcile.go` (atomic traversal-guarded WriteUnits), `orchestrate.go` (RenderInput/Unit/Plan), `systemd.go` (Systemd seam + typed errors), `quadlet/*.tmpl` (all five templates), `internal/inference/backend_vulkan.go` (kyuz0 image, `llama-server` Exec shape, `--host 0.0.0.0` pattern, loopback publish), `internal/inference/seam_test.go` (`TestSeamGrepGate` pattern + seam allowlist), `internal/download/download.go` (PullModel HEAD-verify/resume/SHA256/atomic), `internal/catalog` Shard format + `seed.json` (nomic NOT present), `internal/memory/{memory,footprint}.go` (RenderView/MemoryRenderInput/Decide/Footprint), `internal/config/villaconfig.go` (memory fields, defaults, `marshalVilla` drop-when-off), `internal/status/status.go` (`publishedPorts`/`allLoopback` privacy audit reads only PublishPort=), `cmd/villa/install.go` (render→reconcile→ensureModel→saveConfig→start→pollReady flow + injectable seams).
- Docker Hub registry API (2026-06-09): `qdrant/qdrant` `v1.18.2-unprivileged` → manifest-list `sha256:b79aaa49ce…5962e5` (also v1.18/latest/v1-unprivileged).
- `github.com/qdrant/qdrant` `Dockerfile` (master) + release workflow: unprivileged built with `--build-arg USER_ID=1000`; `chown`s `/qdrant/storage` to that UID; `EXPOSE 6333/6334`; entrypoint `./entrypoint.sh`.
- HuggingFace HEAD (2026-06-09): `nomic-embed-text-v1.5.Q8_0.gguf` — `X-Linked-Size: 146146432`, `X-Linked-Etag: 3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7`.
- llama.cpp `tools/server/README.md` + `/v1/embeddings` usage (curl + response shape; `--embeddings` + `--pooling` ≠ `none`).
- Phase-18 `18-DECISIONS.md` / `18-RESEARCH.md` (PINNED D-07/D-08/D-09: runtime, model/dim/GGUF, OWUI env contract).

### Secondary (MEDIUM confidence)
- Rootless Podman named-volume / non-root-UID / `:U` vs `:Z` behavior (tutorialworks, oneuptime, Fedora Discussion) — named-volume ownership-init + `:U` recursive-chown guidance (A1).
- `github.com/ggml-org/llama.cpp/issues/15406` — `/v1/embeddings` 501 regression, build range 5630–5686, now Closed (digest-pin rationale, D-06).
- `qdrant.tech` security/config docs — default ports 6333/6334, `/qdrant/storage`, unprivileged `--user 1000:2000` guidance.

### Tertiary (LOW confidence)
- The `~512 MiB` embed footprint reservation (Phase-18 estimate; measure on-hardware in Phase 19 — A4).
- Qdrant manifest-list-vs-platform digest equivalence (pin the dev-box-resolved RepoDigest regardless — A5).

## Metadata

**Confidence breakdown:**
- Standard stack (images, GGUF, download path): HIGH — image tag/digest, GGUF size/sha256, and the reused seams are all verified this session against authoritative sources + the codebase.
- Architecture (render/template/conditional/pre-stage mirroring `openwebui.go`): HIGH — directly mirrors a shipped, tested pattern; every touchpoint read this session.
- Pitfalls: HIGH on the seam-gate trip (Pitfall 7, verified against the live regex) and the SC#4 audit (Pitfall 6, verified the audit reads only PublishPort=); MEDIUM on the SC#2 named-volume UID behavior (A1) and the `/v1/embeddings` digest health (A2) — both gated by the D-09 install proof + a `checkpoint:human-verify`.
- Security: HIGH — reuses the existing digest-pin/traversal-guard/no-shell-interpolation discipline; the new surface is container-DNS-only with verified integrity controls.

**Research date:** 2026-06-09
**Valid until:** 2026-07-09 for the Qdrant tag/digest + kyuz0 `/v1/embeddings` health (re-verify the dev-box-resolved digest at freeze time); 2026-09-09 for the in-repo pattern findings (stable).
</content>
</invoke>
