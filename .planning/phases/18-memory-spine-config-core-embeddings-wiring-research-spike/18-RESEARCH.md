# Phase 18: Memory Spine — config core + embeddings/wiring research spike - Research

**Researched:** 2026-06-09
**Domain:** Go control-plane config schema + pure-decision core design; local-RAG integration de-risking (Open WebUI env contract, local embeddings runtime, embedding-model footprint)
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** A NEW pure core `internal/memory` mirrors the `recommend.Pick` / `preflight` idiom: typed inputs → typed decision values, **zero host I/O**. Imports neither `os/exec` nor any container-image literal; `TestSeamGrepGate` stays green (the gate walks `internal/` + `cmd/villa`).
- **D-02:** The core exposes three responsibilities (exact names = planner's call): (a) an **embedding-footprint** function `model → Bytes` (typed-Unknown on miss, never bare 0); (b) an **enablement-and-fields-valid gate** returning a typed decision (memory on/off + all required fields present & valid) — refuse-with-reason, fail-closed on bad config; (c) a **render-view input struct** that `orchestrate` consumes later (the recommend→orchestrate handoff pattern), carrying no image literals — only resolved values (model id, dim, service addrs/ports).
- **D-03:** Footprint is reserved **before** chat-model fit on shared gfx1151 GTT (this phase provides the function; CTRL-01's call site is Phase 22). The embedding **dimension is a load-bearing pinned value** — changing it corrupts existing vectors with no auto-reindex (drives the Phase-23 memory-aware swap guard).
- **D-04:** Memory fields follow the **existing flat, self-healing pattern** (`dashboard_*`/`chat_*` precedent in `internal/config/villaconfig.go`), not a new config file. Default **`memory_enabled = false`**.
- **D-05:** **Byte-identical guarantee (SC#1):** an existing v1.2 install stays byte-identical until the user opts in. Memory fields are defaulted/self-healed on load (extend `normalizeVilla` / `defaultConfig` — single source of the default literals, never re-hard-coded) and are NOT emitted to disk for a non-opted-in install. `SaveVilla`'s XDG-confined, 0600, path-traversal-guarded discipline is reused unchanged; no shell interpolation.
- **D-06:** Fields cover: enable flag, pinned embedding model id, embedding dimension, and the in-network service endpoints (Qdrant + embeddings) used over `villa.network`. Endpoints are **loopback / container-DNS only** — no published host port for Qdrant (PRIV-01 continuity); never widen a bind.
- **D-07:** **Dedicated `villa-embed` llama-server** (reusing the pinned kyuz0 toolbox image, OpenAI `/v1/embeddings`, container-DNS only) — NOT Open WebUI's built-in embedder. Rationale: OWUI's built-in path lazily downloads the embed model from HuggingFace on first upload (the #1 runtime-privacy risk); a dedicated server gives memory-fit control and reuses an already-pinned image. **Flagged for spike confirmation** against the live image.
- **D-08:** Pin **`nomic-embed-text-v1.5`** (GGUF, ~768-dim), served by `villa-embed`. The spike confirms the exact GGUF source/quant and the **measured** footprint in bytes (feeds D-03). Dimension is pinned and recorded for skew warning.
- **D-09:** The spike **re-verifies against the pinned OWUI digest** the exact env keys the wiring phase will freeze: `VECTOR_DB=qdrant` (+ Qdrant URI/key), `RAG_EMBEDDING_ENGINE=openai` (+ `RAG_OPENAI_API_BASE_URL`/key + embedding model), `ENABLE_MEMORIES`, `ENABLE_PERSISTENT_CONFIG=false`, and the offline lockdown set. Phase 18 **records** the verified keys; it does NOT write any env block (Phase 20).
- **D-10:** The two new image literals (Qdrant + `villa-embed`) live behind the **`orchestrate` managed-service render path** (the `openwebui.go` pattern), NOT `inference.BackendFor` / `TestSeamGrepGate`. Phase 18 introduces no image literal at all — but the core must be designed so the literal lands in `orchestrate` later.

### Claude's Discretion
- Exact Go symbol names, struct field layout, and TOML key spellings (flat `memory_*` vs grouped) are the planner's call within D-04/D-05.
- Whether the spike confirmations live in RESEARCH.md or a short decisions appendix — researcher's call; the three decisions (D-07/D-08/D-09) must be explicitly recorded with evidence either way. **(Resolved: recorded here in `## Spike Decisions (PINNED with evidence)`.)**

### Deferred Ideas (OUT OF SCOPE)
- Rendering/starting Qdrant + `villa-embed` Quadlet units — **Phase 19**.
- OWUI env wiring + offline lockdown + zero-outbound runtime test — **Phase 20**.
- chats→Knowledge semantic recall indexer — **Phase 21** (largest phase).
- recommend footprint-reservation + preflight memory gating + doctor checks — **Phase 22** (CTRL-01/CTRL-03/CTRL-06).
- status schema bump (2→3) + dashboard memory rows + backup/swap coverage — **Phase 23** (CTRL-02/CTRL-04/CTRL-05).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| INFRA-04 | The memory stack is config-driven — new `config.toml` fields (enable flag, embedding model, service ports/addrs) regenerate the Quadlet units; units are never hand-edited as the authority | `## Standard Stack` (BurntSushi/toml self-heal pattern reused), `## Architecture Patterns` (config-extension + pure-core), `## Code Examples` (memory field defaulting + footprint + gate), `## Common Pitfalls` (byte-identical invariant), `## Validation Architecture` (round-trip + byte-identical + footprint + gate tests). The spike pins the three downstream-frozen decisions (embeddings runtime, OWUI env keys, embedding model+footprint) so the config field set is correct the first time. |
</phase_requirements>

## Summary

Phase 18 is a **spine + spike** phase with no new dependencies and no host I/O. It does two concrete code things — extend `internal/config/villaconfig.go` with default-off, self-healing memory fields, and add a new pure `internal/memory` core — and one research thing: pin the three version-sensitive integration decisions (embeddings runtime, the exact Open WebUI env-var contract, and the embedding model + its memory footprint) so Phases 19–23 freeze the right values.

All three spike decisions are now **verified and pinned** against authoritative sources:
- **Embeddings runtime (D-07):** `llama-server` supports OpenAI-compatible embeddings via `--embeddings` (note the trailing **s**) + `--pooling <mode>`; `/v1/embeddings` returns the OpenAI shape. The dedicated-`villa-embed` approach is **CONFIRMED feasible and is the correct choice** — it sidesteps OWUI's built-in SentenceTransformers downloader entirely, which is the #1 runtime-privacy risk.
- **OWUI env contract (D-09):** Every target key is verified against the live OWUI env-config reference. **Critical correction surfaced:** `RAG_EMBEDDING_ENGINE`, `RAG_EMBEDDING_MODEL`, `RAG_OPENAI_API_*`, `ENABLE_MEMORIES`, and `VECTOR_DB` are all **PersistentConfig (`ConfigVar`) variables** — they are read from the OWUI database, not the environment, unless `ENABLE_PERSISTENT_CONFIG=False`. This makes `ENABLE_PERSISTENT_CONFIG=False` **load-bearing**, not optional, for INFRA-03/INFRA-04 ("config is the single source of truth"). Also surfaced: `ENABLE_QDRANT_MULTITENANCY_MODE` now defaults to `True` — a decision Phase 20/21 must make explicitly.
- **Embedding model + footprint (D-08):** `nomic-embed-text-v1.5` confirmed at **768 dimensions** (native; Matryoshka-truncatable to 512/256/128/64) and **8192-token context**. GGUF available from `nomic-ai/nomic-embed-text-v1.5-GGUF` with measured file sizes: **Q8_0 = 146 MB, F16 = 274 MB, Q4_K_M = 84.1 MB**. A realistic resident-footprint reservation (weights + KV + runtime overhead) is derived below.

**Primary recommendation:** Extend `defaultConfig()`/`normalizeVilla` with default-off memory fields (flat `memory_*` keys) so a non-opted-in v1.2 install stays byte-identical, and build `internal/memory` as an exec-free, image-literal-free pure core with three functions mirroring `recommend.Pick`. Pin `nomic-embed-text-v1.5` @ **768 dim, Q8_0 GGUF**, reserve a **~512 MiB** embedding footprint for the fit math, and record the OWUI env-key set with `ENABLE_PERSISTENT_CONFIG=False` flagged as mandatory.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Memory enable flag + embedding model id + dim + service endpoints | config (`internal/config`) | — | Config is the single source of truth; units regenerate from it (INFRA-04). Same tier that owns `dashboard_*`/`chat_*`. |
| Embedding footprint (model → Bytes) | pure core (`internal/memory`) | recommend (Phase 22 call site) | Pure decision math, exhaustively table-testable off-hardware; `recommend.Pick` consumes it later to shrink the envelope first (CTRL-01). |
| Enablement-and-fields-valid gate | pure core (`internal/memory`) | preflight/orchestrate (later) | Fail-closed validation belongs in a pure core (mirrors `preflight` refuse-with-remediation), consumed by render + gate sites later. |
| Render-view input struct (model id, dim, addrs/ports) | pure core (`internal/memory`) → orchestrate (Phase 19) | — | The recommend→orchestrate handoff: core produces resolved values; `orchestrate` adds the image literal (D-10). |
| Qdrant + `villa-embed` image literals | orchestrate managed-service path (Phase 19) | — | Managed-service constants, same category as `openWebUIImage` — NOT inference-backend tokens, so outside `TestSeamGrepGate` scope (D-10). |
| Open WebUI RAG/Memory env block | orchestrate `openwebui.go` env slice (Phase 20) | — | Env-only wiring behind the orchestrate seam; ordered slice, golden-frozen. |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/BurntSushi/toml` | v1.6.0 (already in go.mod) | Marshal/unmarshal the extended `VillaConfig` | Already the config codec; no new dependency. Note its zero-value-vs-absent behaviour drives the self-heal pattern (see Pitfall 1). [VERIFIED: go.mod / existing `internal/config/villaconfig.go`] |
| Go standard `testing` | Go 1.26 | Table-driven tests for the pure core + config round-trip | The only test framework in this repo; no assertion/mock libs (seams are injected funcs). [VERIFIED: codebase] |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `internal/detect` typed-Unknown wrappers (`Bytes`/`Bool`) | in-repo | Footprint returns `detect.Bytes` (typed-Unknown on catalog miss, never bare 0) | The embedding-footprint function's return type (D-02a / honesty-by-construction). [VERIFIED: `internal/detect/value.go`] |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Flat `memory_*` keys (D-04) | A `[memory]` TOML table | Grouped table is cleaner but breaks the established flat precedent (`dashboard_*`/`chat_*`) and complicates the self-heal/byte-identical logic. Flat is the locked direction; planner picks exact spellings. |
| New `internal/memory` core (D-01) | Fold logic into `recommend` | Would entangle memory math with chat-model fit and pollute `recommend`'s frozen contract early. A dedicated pure core matches the v1.2 "one new pure core per feature" decision. |
| Dedicated `villa-embed` (D-07) | OWUI built-in SentenceTransformers | OWUI built-in lazily downloads the embed model from HuggingFace at runtime (privacy + offline-mode breakage). Dedicated server reuses an already-pinned image and gives footprint control. **CONFIRMED correct** (see Spike Decisions). |

**Installation:**
```
# No new packages. Phase 18 adds ZERO dependencies (go.mod unchanged).
```

**Version verification:** Not applicable — no new package. `BurntSushi/toml v1.6.0` already present and exercised.

## Package Legitimacy Audit

> **Not applicable to Phase 18.** This phase installs no external packages and `go.mod` is unchanged. It only *records* (does not introduce) the image identities later phases will pin behind the orchestrate seam. Those identities are documented below for traceability; their literal pinning + legitimacy audit belongs to **Phase 19**.

| Image (recorded for Phase 19, NOT introduced here) | Registry | Role | Disposition |
|---------|----------|------|-------------|
| `docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…ac7aad` | Docker Hub | Reused for `villa-embed` (already pinned in `internal/inference/backend_vulkan.go`) | Already approved + digest-pinned in v1.0 |
| `qdrant/qdrant` (digest TBD by Phase 19) | Docker Hub | Vector DB managed service | **Phase 19** pins digest + runs legitimacy audit |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram

This diagram shows where Phase 18's two artifacts sit and what consumes them in later phases (Phase 18 builds only the boxed-solid nodes; dashed = downstream consumers, out of scope here).

```
                 ┌──────────────────────────────────────────┐
   user edits    │  config.toml  ($XDG_CONFIG_HOME/villa/)   │
   memory_*  ───►│  [memory_enabled=false default]           │  ◄── SINGLE SOURCE OF TRUTH
                 └───────────────┬──────────────────────────┘
                                 │ LoadVilla → normalizeVilla (self-heal, default-off)
                                 ▼
                 ┌──────────────────────────────────────────┐
                 │  internal/memory  (NEW pure core)         │   ── Phase 18 ──
                 │   (a) Footprint(modelID) → detect.Bytes   │
                 │   (b) Decide(cfg) → Decision (gate)       │
                 │   (c) RenderView(cfg) → MemoryRenderInput │
                 │   NO os/exec · NO image literal           │
                 └───┬───────────────┬──────────────────┬────┘
                     │ (a)           │ (b)              │ (c) resolved values only
                     ▼               ▼                  ▼
        ┌────────────────┐  ┌────────────────┐  ┌──────────────────────────┐
        │ recommend.Pick │  │ preflight /    │  │ orchestrate managed-svc  │
        │ (CTRL-01, P22) │  │ doctor (P22)   │  │ render: Qdrant + embed   │
        │ reserve embed  │  │ gate host fit  │  │ + OWUI env (P19/P20)     │
        │ BEFORE chat fit│  │                │  │ ── adds the image literal │
        └────────────────┘  └────────────────┘  └──────────────────────────┘
                (all dashed = OUT OF SCOPE for Phase 18)

   Runtime data flow that the pinned decisions enable (Phase 19/20+, for context):
   Open WebUI ──(OpenAI /v1/embeddings over villa.network)──► villa-embed (llama-server, nomic-embed-text-v1.5)
   Open WebUI ──(QDRANT_URI over villa.network, no host port)──► villa-qdrant (vectors, 768-dim)
```

### Recommended Project Structure
```
internal/
├── config/
│   └── villaconfig.go      # EXTEND: add memory_* fields to VillaConfig,
│                           # defaultConfig(), normalizeVilla() (single source of defaults)
├── memory/                 # NEW pure core (this phase)
│   ├── memory.go           # Footprint / Decide / RenderView + types; package doc comment
│   ├── footprint.go        # (optional split) embedding-footprint catalog → detect.Bytes
│   └── memory_test.go      # table-driven: footprint, gate, render-view, byte-identical
└── detect/value.go         # REUSE: detect.Bytes typed-Unknown return type (unchanged)
```

### Pattern 1: Config field extension with single-source defaults + self-heal (D-04/D-05)
**What:** Add memory fields to `VillaConfig`, seed their defaults ONLY in `defaultConfig()`, and self-heal absent/zero values in `normalizeVilla()` deriving from `defaultConfig()` — never re-hard-coding literals.
**When to use:** Always for new config fields in this repo (the `dashboard_*`/`chat_*` precedent).
**Example:**
```go
// Source: existing internal/config/villaconfig.go (defaultConfig + normalizeVilla precedent)
// defaultConfig() is the SINGLE source of the default literals.
func defaultConfig() VillaConfig {
    return VillaConfig{
        Backend:       "vulkan",
        DashboardAddr: "127.0.0.1",
        DashboardPort: 8888,
        ChatPort:      3000,
        // NEW (default-OFF; planner picks exact key spellings):
        MemoryEnabled:    false,
        EmbeddingModel:   "nomic-embed-text-v1.5",
        EmbeddingDim:     768,
        QdrantAddr:       "villa-qdrant",  // container-DNS only, NEVER a host bind
        QdrantPort:       6333,
        EmbedAddr:        "villa-embed",   // container-DNS only
        EmbedPort:        8080,
    }
}
```
**Critical nuance:** memory fields must default to a *coherent off state*. `MemoryEnabled=false` is the gate; the other defaults are inert until opt-in. The byte-identical guarantee (SC#1) is about not *changing the on-disk file* of an existing install (see Pitfall 1), not about the in-memory struct.

### Pattern 2: Pure-core triad mirroring `recommend.Pick` (D-01/D-02)
**What:** Three pure functions — footprint, gate, render-view — with typed inputs → typed outputs, no I/O, no image literals.
**When to use:** This is the `internal/memory` core's entire shape.
**Example:**
```go
// Source: shape mirrors internal/recommend/recommend.go (pure Pick) + internal/detect/value.go
package memory

// (a) Footprint: model id → resident byte reservation, typed-Unknown on catalog miss.
func Footprint(modelID string) detect.Bytes { /* lookup table; UnknownBytes on miss */ }

// (b) Decide: enablement-and-fields-valid gate (fail-closed, refuse-with-reason).
type Decision struct {
    Enabled bool
    Valid   bool
    Reasons []string // populated when !Valid (e.g. "embedding_dim must be > 0")
}
func Decide(cfg config.VillaConfig) Decision { /* pure validation */ }

// (c) RenderView: resolved values orchestrate consumes later — NO image literal here.
type MemoryRenderInput struct {
    EmbeddingModel string
    EmbeddingDim   int
    QdrantAddr     string; QdrantPort int
    EmbedAddr      string; EmbedPort  int
}
func RenderView(cfg config.VillaConfig) MemoryRenderInput { /* map cfg → resolved struct */ }
```

### Anti-Patterns to Avoid
- **Importing `os/exec` or a container-image string in `internal/memory`:** fails `TestSeamGrepGate` (the gate matches `kyuz0|docker.io/` and `exec.Command("podman"` across all of `internal/`). The render-view must carry only resolved *values* (D-02c/D-10).
- **Re-hard-coding default literals in `normalizeVilla`:** the dashboard precedent derives every default from `defaultConfig()`. Duplicating `768`/`6333` in two places is a drift bug.
- **Emitting memory fields to disk when `memory_enabled=false`:** would break the byte-identical guarantee (SC#1). See Pitfall 1 for the BurntSushi/toml mechanics.
- **Treating the embedding dimension as a free runtime value:** it is a pinned, load-bearing constant — changing it silently corrupts existing Qdrant vectors (no auto-reindex). Record it for the Phase-23 swap guard.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| TOML (de)serialization of new fields | A custom parser/marshaler | `BurntSushi/toml` (already in use) | Already the codec; no string interpolation (injection-safe on write). |
| "Unknown vs zero" footprint result | A bare `uint64` with a sentinel | `detect.Bytes` (`KnownBytes`/`UnknownBytes`) | Repo-wide honesty-by-construction; `--json`/recommend already understand it. |
| Local embeddings server | A Go embeddings engine (out of scope per PROJECT.md) | `llama-server --embeddings` in the kyuz0 toolbox | Go is the control plane only; reuse the already-pinned image. |
| Vector store | Embedded SQLite/cgo vector store | Qdrant managed Quadlet (Phase 19) | Breaks the single static CGO-free binary; explicitly out of scope. |
| Config-file write safety | A new writer | `SaveVilla` (XDG-confined, 0600, traversal-guarded) unchanged | Reuse the proven discipline; D-05 mandates no change to it. |

**Key insight:** Phase 18 builds *decision logic and a schema*, not services. Every "hard" RAG problem (embeddings, vectors, retrieval) is deferred to OSS integrations in later phases — the spine's only job is to compute and validate the values those integrations need.

## Runtime State Inventory

> Not a rename/refactor/migration phase. This section is included only to confirm no hidden runtime state is touched.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — Phase 18 writes no vectors, no OWUI DB, no Qdrant collections. The pinned **embedding dimension (768)** is recorded for the *future* Phase-23 swap/restore guard, but no data exists yet. | none this phase |
| Live service config | None — renders/starts no Quadlet unit; writes no OWUI env block. | none this phase |
| OS-registered state | None — no systemd units added or changed. | none this phase |
| Secrets/env vars | None written. `QDRANT_API_KEY` is recorded as an env *key to set later* (Phase 20); for a strictly-local single-user container-DNS Qdrant it can be empty/unset (no auth surface on the private network) — a Phase-19/20 decision, not Phase 18. | none this phase |
| Build artifacts | New `internal/memory` package compiles into the single binary; no stale artifacts. | `make build` / `make check` |

**Nothing found in any category requiring migration** — verified: Phase 18 is config-schema + pure-core + recorded-decisions only.

## Common Pitfalls

### Pitfall 1: BurntSushi/toml writes a key even when its value is the type-zero → byte-identical break
**What goes wrong:** Adding fields to `VillaConfig` makes `SaveVilla` marshal them — so any future save of an existing v1.2 config could emit `memory_enabled = false`, `embedding_dim = 0`, etc., changing the on-disk bytes and breaking SC#1.
**Why it happens:** `toml.Marshal` serializes all exported struct fields; and on *load*, BurntSushi sets a key present in the file even when its value is the type zero (the exact reason `normalizeVilla` exists for `dashboard_port=0`).
**How to avoid:** Two complementary moves — (1) treat type-zero memory fields as "unset → default" in `normalizeVilla` (so a load self-heals, mirroring the dashboard fix), and (2) for the byte-identical guarantee, ensure Phase 18 **does not save** for a non-opted-in install (Phase 18 is read-only for memory; only a future opt-in write emits the fields). The test that proves SC#1: load an existing v1.2 `config.toml` (no memory keys), assert the in-memory struct is correctly defaulted-off, and assert that a non-opt-in round-trip does not introduce memory keys. Consider `omitempty`-style handling only if a write path is added — but Phase 18 adds none.
**Warning signs:** A golden/round-trip test shows new keys appearing in a previously-clean config file.

### Pitfall 2: OWUI RAG/embedding/memory keys are PersistentConfig — env is ignored without `ENABLE_PERSISTENT_CONFIG=False`
**What goes wrong:** Phase 20 sets `RAG_EMBEDDING_ENGINE=openai`, `VECTOR_DB=qdrant`, `ENABLE_MEMORIES`, etc., the stack appears wired, but after first boot OWUI reads these from its **database** and ignores the env — so a config.toml-driven change silently does nothing.
**Why it happens:** OWUI marks these as `ConfigVar`/PersistentConfig; with `ENABLE_PERSISTENT_CONFIG=True` (the default) the DB wins over env.
**How to avoid:** **Record now (D-09) that `ENABLE_PERSISTENT_CONFIG=False` is mandatory, not optional**, for INFRA-03/INFRA-04. This is the load-bearing key that makes "config is the single source of truth" hold for OWUI. Phase 18 only records it; Phase 20 sets it.
**Warning signs:** Settings changed in config don't take effect after a restart; the Admin UI lets you "change" them but they revert.

### Pitfall 3: `--embedding` vs `--embeddings`, and `/v1/embeddings` requires a non-`none` pooling mode
**What goes wrong:** A `villa-embed` server started without the correct flags returns errors on `/v1/embeddings` or produces no usable vectors.
**Why it happens:** The flag is `--embeddings` (trailing s) on current `llama-server`; `/v1/embeddings` (OpenAI shape) requires a pooling mode other than `none` (e.g. `mean` or `cls`). Older llama.cpp builds don't auto-read the pooling type from the model. A llama.cpp regression (issue #15406) broke `/v1/embeddings` in some master builds — and the kyuz0 image is built from llama.cpp **master, auto-rebuilt**, so the digest pin is what protects against a silently-broken rebuild.
**How to avoid:** Phase 19 must (1) use `--embeddings --pooling mean` (or `cls`) explicitly, (2) verify the *pinned* kyuz0 digest's `llama-server` actually serves `/v1/embeddings` before freezing the unit, and (3) keep the digest pin so an upstream regression can't reach the user. Phase 18 records these flag requirements; it freezes no unit.
**Warning signs:** `/v1/embeddings` 400/500s; vectors are all-zero or wrong-length; embeddings work on `/embeddings` but not `/v1/embeddings`.

### Pitfall 4: `ENABLE_QDRANT_MULTITENANCY_MODE` now defaults to `True` and `QDRANT_COLLECTION_PREFIX` defaults to `open-webui`
**What goes wrong:** The recall indexer (Phase 21) or backup/restore (Phase 23) assumes a flat per-knowledge collection layout, but OWUI multitenancy consolidates collections — and toggling it later "disconnects" existing collections (requires a manual reindex).
**Why it happens:** Recent OWUI defaults changed; multitenancy reduces RAM but changes collection structure.
**How to avoid:** Record both keys (`ENABLE_QDRANT_MULTITENANCY_MODE`, `QDRANT_COLLECTION_PREFIX=open-webui`) in the env contract now so Phase 20 makes an explicit, frozen choice and Phase 21/23 build against the chosen layout. Do not leave it implicit.
**Warning signs:** Phase-21 indexer can't find/address collections; Phase-23 restore lands vectors the running OWUI can't see.

### Pitfall 5: Embedding-dimension skew silently corrupts retrieval
**What goes wrong:** Swapping the embedding model (or changing the Matryoshka truncation dim) against an existing Qdrant collection produces wrong-length vectors → broken or garbage retrieval, no error.
**Why it happens:** Qdrant collections are created with a fixed vector size; nomic-embed-text-v1.5 is Matryoshka (768/512/256/128/64) so the *same model* can emit different dims if truncation is configured.
**How to avoid:** Pin the dimension as a load-bearing config value (D-03/D-08 → 768), record it for the Phase-23 manifest + memory-aware swap guard, and never treat it as adjustable at runtime. Phase 18's `EmbeddingDim` field is exactly this anchor.
**Warning signs:** Retrieval quality collapses after a model/dim change; Qdrant insert errors about vector size mismatch.

## Code Examples

### Embedding footprint with typed-Unknown (the D-02a function)
```go
// Source: pattern from internal/detect/value.go (KnownBytes/UnknownBytes) +
// measured GGUF sizes (see Spike Decisions). Footprint reserves resident bytes
// BEFORE chat-model fit (consumed by recommend.Pick in Phase 22 / CTRL-01).
package memory

import "github.com/MatrixMagician/VillaStraylight/internal/detect"

// embedFootprints maps a pinned embedding model id → a conservative RESIDENT
// reservation (weights + KV + llama-server runtime overhead), not just the GGUF
// file size. nomic-embed-text-v1.5 Q8_0 weights ≈ 146 MB on disk; resident with
// a modest embedding context + runtime overhead, reserve ~512 MiB to stay honest
// on the shared gfx1151 GTT (see Spike Decisions for the derivation).
var embedFootprints = map[string]uint64{
    "nomic-embed-text-v1.5": 512 << 20, // 512 MiB conservative reservation
}

func Footprint(modelID string) detect.Bytes {
    if b, ok := embedFootprints[modelID]; ok {
        return detect.KnownBytes(b, "memory: pinned embedding footprint reservation")
    }
    return detect.UnknownBytes(
        "memory: no footprint known for embedding model "+modelID, modelID)
}
```

### Enablement-and-fields-valid gate (the D-02b function, fail-closed)
```go
// Source: refuse-with-reason discipline from internal/preflight + internal/recommend.
// Pure; returns a typed decision. Never panics, never does I/O.
func Decide(cfg config.VillaConfig) Decision {
    if !cfg.MemoryEnabled {
        return Decision{Enabled: false, Valid: true} // off is a valid state
    }
    var reasons []string
    if cfg.EmbeddingModel == "" {
        reasons = append(reasons, "embedding_model is required when memory_enabled=true")
    }
    if cfg.EmbeddingDim <= 0 {
        reasons = append(reasons, "embedding_dim must be a positive pinned value (e.g. 768)")
    }
    if cfg.QdrantAddr == "" || cfg.QdrantPort <= 0 {
        reasons = append(reasons, "qdrant addr/port required (container-DNS only)")
    }
    if cfg.EmbedAddr == "" || cfg.EmbedPort <= 0 {
        reasons = append(reasons, "embeddings addr/port required (container-DNS only)")
    }
    return Decision{Enabled: true, Valid: len(reasons) == 0, Reasons: reasons}
}
```

## Spike Decisions (PINNED with evidence)

These are the three decisions later phases freeze. Each is recorded with its evidence and confidence.

### D-07 — Embeddings runtime: dedicated `villa-embed` llama-server  ✅ CONFIRMED
- **Decision:** Run a dedicated `villa-embed` llama-server (reusing the pinned kyuz0 toolbox image) serving OpenAI `/v1/embeddings` over `villa.network` (container-DNS only). Wire OWUI with `RAG_EMBEDDING_ENGINE=openai` pointed at it — NOT OWUI's built-in SentenceTransformers path.
- **Feasibility confirmed:** `llama-server` serves embeddings via `--embeddings` (+ `--pooling <mean|cls>`); `/v1/embeddings` returns the OpenAI-compatible shape and requires a pooling mode other than `none`. Example documented usage: `llama-server -m nomic-embed-text-v1.5.Q8_0.gguf --embeddings --pooling mean`. [CITED: github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md] [CITED: github.com/ggml-org/llama.cpp/discussions/7712]
- **Why dedicated wins:** OWUI's built-in embedder lazily downloads the SentenceTransformers model from HuggingFace at runtime (breaks under `OFFLINE_MODE`/`HF_HUB_OFFLINE` and is the #1 runtime-privacy risk). The `engine=openai` path delegates to our local server, sidestepping the downloader entirely; reuses an already-pinned image (no new embeddings image to legitimacy-audit). [CITED: docs.openwebui.com env-config — `OFFLINE_MODE`/`HF_HUB_OFFLINE` notes that RAG won't work offline without a pre-staged embed model]
- **Spike risk to carry to Phase 19:** the kyuz0 image is built from llama.cpp **master (auto-rebuilt)**, and a master regression once broke `/v1/embeddings` (issue #15406). The **digest pin** mitigates silent breakage; Phase 19 MUST curl `/v1/embeddings` against the pinned digest before freezing the unit. [CITED: github.com/ggml-org/llama.cpp/issues/15406]
- **Confidence:** HIGH.

### D-08 — Embedding model + footprint: `nomic-embed-text-v1.5`, 768-dim, Q8_0 GGUF  ✅ CONFIRMED
- **Decision:** Pin `nomic-embed-text-v1.5`. **Dimension = 768** (native; Matryoshka-truncatable to 512/256/128/64 — but pin 768, do not truncate). **Context = 8192 tokens.** GGUF source: `nomic-ai/nomic-embed-text-v1.5-GGUF`. Recommended quant: **Q8_0** (best quality/size balance for a tiny model; F16 acceptable). [CITED: huggingface.co/nomic-ai/nomic-embed-text-v1.5] [CITED: zilliz.com/ai-models/nomic-embed-text-v1.5] [CITED: huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF]
- **Measured GGUF file sizes** (from the GGUF repo): **Q4_K_M = 84.1 MB, Q8_0 = 146 MB, F16 = 274 MB, F32 = 548 MB.** [CITED: huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF]
- **Footprint reservation (feeds D-03 / CTRL-01):** the GGUF *file* size is a floor, not the resident footprint. Resident = weights + a small embedding-context KV + llama-server runtime overhead. For Q8_0 (146 MB weights) a **conservative ~512 MiB reservation** is recommended for the fit math (covers weights + modest context + overhead with margin on the shared gfx1151 GTT). This is an **estimate flagged for measurement** in Phase 19 on-hardware (measure resident GTT delta when `villa-embed` is up; refine the constant then). Recording a *conservative* number now is safe (it over-reserves, never under-reserves → never causes an OOM the fit math missed). [ASSUMED — derivation from measured file size + runtime overhead; pending on-hardware measurement in Phase 19]
- **Usage caveat for Phase 20/21:** nomic-embed-text-v1.5 requires task-instruction prefixes (`search_document:` / `search_query:`) for optimal retrieval. OWUI may or may not apply these via the openai engine — a Phase-20 verification item, not a Phase-18 blocker. [CITED: huggingface.co/nomic-ai/nomic-embed-text-v1.5]
- **Confidence:** HIGH on model/dim/context/GGUF; MEDIUM on the exact footprint constant (conservative estimate pending measurement).

### D-09 — Open WebUI env contract, re-verified against the live env reference  ✅ VERIFIED
> Verified against the current `open-webui/docs` env-configuration reference (`docs/reference/env-configuration.mdx`, fetched 2026-06-09). **Phase 18 records these; Phase 20 sets them.** Defaults below are OWUI's defaults.

| Env key | Target value (Phase 20) | OWUI default | PersistentConfig? | Notes |
|---------|------------------------|--------------|-------------------|-------|
| `VECTOR_DB` | `qdrant` | `chroma` | (selection var) | Selects Qdrant over telemetry-posting ChromaDB (PRIV-05). [CITED] |
| `QDRANT_URI` | `http://villa-qdrant:6333` (container-DNS) | (unset) | no | REST URI; container-DNS only, no host port. [CITED] |
| `QDRANT_API_KEY` | empty/unset (local private net) | (unset) | no | No auth surface on `villa.network`; Phase-19/20 decision. [CITED] |
| `ENABLE_QDRANT_MULTITENANCY_MODE` | **explicit choice required** | `True` | no | Changed default; affects collection layout for Phase 21/23. **Decide explicitly.** [CITED] |
| `QDRANT_COLLECTION_PREFIX` | `open-webui` (default) | `open-webui` | no | Namespacing for Phase-21 indexer. [CITED] |
| `RAG_EMBEDDING_ENGINE` | `openai` | empty (=SentenceTransformers) | **YES (`ConfigVar`)** | Points OWUI at `villa-embed`. **Requires `ENABLE_PERSISTENT_CONFIG=False`.** [CITED] |
| `RAG_OPENAI_API_BASE_URL` | `http://villa-embed:8080/v1` | `${OPENAI_API_BASE_URL}` | **YES (`ConfigVar`)** | Local embeddings endpoint, container-DNS. [CITED] |
| `RAG_OPENAI_API_KEY` | `sk-no-key-required` (sentinel) | `${OPENAI_API_KEY}` | **YES (`ConfigVar`)** | llama-server does no auth; non-empty sentinel like the existing chat path. [CITED] |
| `RAG_EMBEDDING_MODEL` | `nomic-embed-text-v1.5` | `sentence-transformers/all-MiniLM-L6-v2` | **YES (`ConfigVar`)** | Must match the model `villa-embed` serves. [CITED] |
| `RAG_EMBEDDING_MODEL_AUTO_UPDATE` | `False` | `True` | no | Offline lockdown — no runtime model update (PRIV-04). [CITED] |
| `ENABLE_MEMORIES` | `True` (memory feature) | `True` | **YES (`ConfigVar`)** | Personalized memory (MEM-01). [CITED] |
| `ENABLE_PERSISTENT_CONFIG` | **`False`** | `True` | n/a | **LOAD-BEARING** — makes env (config) authoritative over OWUI's DB; without it the `ConfigVar` keys above are ignored after first boot (INFRA-03/INFRA-04). [CITED] |
| `OFFLINE_MODE` | `True` | `False` | no | Disables OWUI network/update/model-download; auto-forces `ENABLE_VERSION_UPDATE_CHECK=false`. [CITED] |
| `HF_HUB_OFFLINE` | `1` | `0` | no | Blocks all HuggingFace downloads (PRIV-04). Already set in the existing OWUI render. [CITED] |
| `ANONYMIZED_TELEMETRY` | `False` | (telemetry on) | no | Telemetry kill (PRIV-05). Already in the existing OWUI render. [CITED] |
| `ENABLE_VERSION_UPDATE_CHECK` | `False` | `True` | no | Already in the existing OWUI render; auto-false under `OFFLINE_MODE`. [CITED] |

- **Net new keys vs the existing `openwebui.go` env block:** the existing render already sets `ANONYMIZED_TELEMETRY=False`, `DO_NOT_TRACK=True`, `SCARF_NO_ANALYTICS=True`, `OFFLINE_MODE=True`, `ENABLE_VERSION_UPDATE_CHECK=False`, `HF_HUB_OFFLINE=1`, `WEBUI_AUTH=True`. Phase 20 ADDS the RAG/Qdrant/memory block + `ENABLE_PERSISTENT_CONFIG=False` + `RAG_EMBEDDING_MODEL_AUTO_UPDATE=False`. That env-block change is a DELIBERATE golden re-freeze (the telemetry-frozen test forces a re-audit on any env change). [VERIFIED: `internal/orchestrate/openwebui.go`]
- **Deprecation/rename watch:** no key renames detected vs the target set; the notable *behaviour* change is `ENABLE_QDRANT_MULTITENANCY_MODE` defaulting to `True`. Re-verify the digest's actual behaviour in Phase 20 (env reference tracks `:main`, not necessarily the pinned digest).
- **Confidence:** HIGH (verified against live docs source 2026-06-09).

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| OWUI env-config doc at `/getting-started/env-configuration` | Now at `/reference/env-configuration` (`docs/reference/env-configuration.mdx`) | recent docs restructure | Old WebFetch URL 404s; use the `open-webui/docs` repo source. |
| ChromaDB default vector DB (telemetry via PostHog) | `VECTOR_DB=qdrant` (no telemetry) | this milestone's choice | PRIV-05 driver — Qdrant over ChromaDB. |
| OWUI Qdrant single-collection (non-multitenant) | `ENABLE_QDRANT_MULTITENANCY_MODE=True` default | recent OWUI | Phase 20/21/23 must account for multitenant collection layout. |

**Deprecated/outdated:**
- Truncating nomic-embed via Matryoshka to <768 to save space: tempting but creates a dimension-skew hazard; pin 768 (Pitfall 5).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | ~512 MiB is a safe conservative resident footprint reservation for `nomic-embed-text-v1.5` Q8_0 | Spike D-08 / Code Examples | Low — over-reserves (never under-reserves), so it can't cause a missed OOM. Refine with on-hardware measurement in Phase 19; if grossly oversized it merely wastes a little envelope. |
| A2 | `QDRANT_API_KEY` can be empty on the private `villa.network` (no auth needed) | Spike D-09 / Runtime State | Low — strictly-local single-user posture; Phase 19/20 can add a generated key if desired without schema change. |
| A3 | OWUI's `engine=openai` path will apply (or tolerate the absence of) nomic's `search_document:`/`search_query:` task prefixes acceptably | Spike D-08 | Medium — affects retrieval *quality*, not wiring; a Phase-20 verification item, not a Phase-18 blocker. |

**Note:** A1–A3 are quality/measurement refinements deferred to later phases by design. None blocks Phase 18's deliverables (schema + pure core + recorded decisions). The three core spike *decisions* (D-07/D-08/D-09) are VERIFIED/CITED, not assumed.

## Open Questions (RESOLVED)

> Both items are RESOLVED for Phase 18 as deferred to a later phase — neither blocks
> this phase's deliverables (schema + pure core + recorded decisions). The three core
> spike *decisions* D-07/D-08/D-09 are VERIFIED/CITED, not open.

1. **Exact resident footprint of `villa-embed`** — **RESOLVED: deferred to Phase 19.**
   - What we know: GGUF file sizes are measured (Q8_0 = 146 MB); resident > file size.
   - What's unclear: the precise GTT reservation under embedding load on gfx1151.
   - Recommendation: pin a conservative ~512 MiB now (over-reserve is safe); measure and refine the constant in Phase 19 on-hardware. The footprint function's *signature and call site* are what Phase 18 must get right, not the last megabyte of the constant.

2. **Multitenancy mode choice** — **RESOLVED: deferred to Phase 20 (choice pending, before any vectors exist).**
   - What we know: `ENABLE_QDRANT_MULTITENANCY_MODE` defaults `True`; toggling later disconnects existing collections.
   - What's unclear: whether the Phase-21 recall indexer is simpler against multitenant or flat collections.
   - Recommendation: record both keys in the contract now; make the explicit choice in Phase 20 before any vectors exist (free to choose then).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | building `internal/memory` + config changes | ✓ (assumed; repo builds in CI) | 1.26.x | — |
| `BurntSushi/toml` | config marshal | ✓ | v1.6.0 (go.mod) | — |
| Podman / Qdrant image / kyuz0 image | NONE in Phase 18 (renders/starts nothing) | n/a | — | — |

**Missing dependencies with no fallback:** none — Phase 18 is pure code (config schema + pure core) with no external runtime dependency. The image/service availability checks belong to Phase 19.

## Validation Architecture

> `nyquist_validation: true` (config.json) — this section is REQUIRED. The pure-core decisions (footprint math, enablement gate, byte-identical-config invariant) are highly testable off-hardware.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven; no third-party assert/mock) |
| Config file | none — `go test` convention |
| Quick run command | `go test ./internal/memory/ ./internal/config/ -count=1` |
| Full suite command | `make check` (vet + `go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| INFRA-04 | An existing v1.2 `config.toml` (no memory keys) loads with memory defaulted-OFF and correct inert defaults | unit | `go test ./internal/config/ -run TestLoad.*Memory -x` | ❌ Wave 0 |
| INFRA-04 | Byte-identical: a non-opted-in install is not rewritten with memory keys (no save path triggers; round-trip introduces no new keys) | unit | `go test ./internal/config/ -run TestMemoryByteIdentical -x` | ❌ Wave 0 |
| INFRA-04 | `normalizeVilla` self-heals zero/absent memory fields from `defaultConfig()` (single source; no re-hardcoded literals) | unit | `go test ./internal/config/ -run TestNormalizeMemory -x` | ❌ Wave 0 |
| INFRA-04 | `memory.Footprint` returns `KnownBytes` for the pinned model and `UnknownBytes` (never bare 0) on a miss | unit | `go test ./internal/memory/ -run TestFootprint -x` | ❌ Wave 0 |
| INFRA-04 | `memory.Decide` gate: off→valid; on+missing-field→invalid with reasons (fail-closed) | unit | `go test ./internal/memory/ -run TestDecide -x` | ❌ Wave 0 |
| INFRA-04 | `memory.RenderView` carries only resolved values (no image literal); container-DNS addrs | unit | `go test ./internal/memory/ -run TestRenderView -x` | ❌ Wave 0 |
| INFRA-04 (constraint) | `TestSeamGrepGate` stays green — `internal/memory` leaks no image/exec literal | unit (existing) | `go test ./internal/inference/ -run TestSeamGrepGate -x` | ✅ exists |

### Sampling Rate
- **Per task commit:** `go test ./internal/memory/ ./internal/config/ -count=1`
- **Per wave merge:** `make check` (vet + full suite, includes `TestSeamGrepGate`)
- **Phase gate:** Full suite green before `/gsd-verify-work`; CI's CGO-free static build stays green.

### Wave 0 Gaps
- [ ] `internal/memory/memory_test.go` — covers footprint / gate / render-view (INFRA-04)
- [ ] `internal/config/villaconfig_test.go` — ADD memory cases (default-off load, byte-identical, normalize) mirroring existing `TestLoadNormalizesZeroPorts` / `TestSaveLoadRoundTrip`
- [ ] Framework install: none — Go `testing` already in use

*(No new fixtures or golden files required: Phase 18 changes no byte-frozen `--json` contract. The `recommend`/`status` golden re-freezes happen in Phases 22/23, not here.)*

## Security Domain

> `security_enforcement: true` (config.json) — included. ASVS L1; this phase's surface is config-file write safety + endpoint binding posture + the runtime-privacy decisions it records.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | No auth surface added; `villa-embed`/Qdrant are container-DNS only on a private net (Phase 19/20). |
| V3 Session Management | no | n/a |
| V4 Access Control | partial | File-system: `SaveVilla` 0600 file / 0700 dir + XDG-confined path-traversal guard (reused unchanged, D-05). |
| V5 Input Validation | yes | `memory.Decide` fail-closed gate validates config fields; no shell interpolation; TOML codec (no string interpolation on write). |
| V6 Cryptography | no | No crypto introduced; `QDRANT_API_KEY` recorded as a future key, empty on the private net (A2). |
| V12 File & Resources | yes | Config write confined to `$XDG_CONFIG_HOME/villa`, traversal-guarded, 0600 (existing discipline, unchanged). |
| V14 Configuration | yes | The recorded offline-lockdown env set (`OFFLINE_MODE`/`HF_HUB_OFFLINE`/`*_AUTO_UPDATE=False`/`ANONYMIZED_TELEMETRY=False`) + `ENABLE_PERSISTENT_CONFIG=False` enforce the runtime-privacy posture (PRIV-04/05). |

### Known Threat Patterns for this phase's stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Hand-edited/malicious config.toml (bad memory fields) | Tampering | `memory.Decide` fail-closed validation; refuse-with-reason, never silent-accept. |
| Config write outside XDG dir (traversal) | Tampering / Elevation | `assertInsideDir` traversal guard + 0600/0700 modes (reused unchanged). |
| Widening a service bind to a routable interface | Information Disclosure | Endpoint fields are container-DNS/loopback only (D-06); `normalizeVilla` never widens a bind (the dashboard precedent's PRIV-01 rule extends to memory endpoints). |
| OWUI lazily downloading the embed model from HuggingFace at runtime | Information Disclosure (exfil of doc content to fetch model / outbound) | Dedicated `villa-embed` (`engine=openai`) + recorded `OFFLINE_MODE`/`HF_HUB_OFFLINE`/`RAG_EMBEDDING_MODEL_AUTO_UPDATE=False` — the #1 runtime-privacy risk, mitigated by the spike's runtime choice. |
| Vector store telemetry (ChromaDB PostHog) | Information Disclosure | `VECTOR_DB=qdrant` + `ANONYMIZED_TELEMETRY=False` (PRIV-05); verified by a runtime zero-outbound smoke test in Phase 20 (not this phase). |
| Image-literal leak into a backend-neutral caller | (architectural integrity) | `internal/memory` imports no image literal; `TestSeamGrepGate` enforces. |

**Phase-18 security note for each downstream PLAN's `<threat_model>`:** the load-bearing security facts to carry forward are (1) config write safety is reused unchanged (0600/0700/traversal/no-shell-interpolation), (2) all memory endpoints are container-DNS/loopback — never widen a bind, and (3) the runtime-privacy posture depends on the recorded offline/telemetry env set + `ENABLE_PERSISTENT_CONFIG=False`, with the dedicated local embeddings server eliminating the HF runtime-download vector.

## Sources

### Primary (HIGH confidence)
- `open-webui/docs` repo — `docs/reference/env-configuration.mdx` (fetched via `gh api` 2026-06-09) — exact env keys, defaults, PersistentConfig status for VECTOR_DB/QDRANT_*/RAG_*/ENABLE_MEMORIES/ENABLE_PERSISTENT_CONFIG/OFFLINE_MODE/HF_HUB_OFFLINE/ANONYMIZED_TELEMETRY/ENABLE_QDRANT_MULTITENANCY_MODE.
- Codebase: `internal/config/villaconfig.go` (defaultConfig/normalizeVilla/SaveVilla discipline), `internal/recommend/recommend.go` (pure Pick idiom), `internal/detect/value.go` (typed-Unknown wrappers), `internal/orchestrate/openwebui.go` (managed-service render path + existing env block), `internal/inference/seam_test.go` (TestSeamGrepGate scope), `internal/inference/backend_vulkan.go` (pinned kyuz0 image digest).
- `github.com/ggml-org/llama.cpp` server README + discussion #7712 — `--embeddings`/`--pooling`/`/v1/embeddings` contract.
- `huggingface.co/nomic-ai/nomic-embed-text-v1.5` + `-GGUF` repo — dimension (768), context (8192), GGUF quant file sizes.

### Secondary (MEDIUM confidence)
- `zilliz.com/ai-models/nomic-embed-text-v1.5`, `nomic.ai/news/nomic-embed-matryoshka` — Matryoshka dims + task-prefix usage.
- `github.com/ggml-org/llama.cpp/issues/15406` — `/v1/embeddings` master regression (digest-pin risk).
- `qdrant.tech` installation/configuration — default ports 6333/6334, `/qdrant/storage` (for Phase 19 context).
- `github.com/kyuz0/amd-strix-halo-toolboxes` + DeepWiki — image is auto-rebuilt from llama.cpp master (digest-pin rationale).

### Tertiary (LOW confidence)
- The ~512 MiB footprint constant (estimate; pending on-hardware measurement in Phase 19).

## Metadata

**Confidence breakdown:**
- Config schema + pure-core design: HIGH — directly mirrors shipped, tested patterns (`dashboard_*` self-heal, `recommend.Pick`, `detect.Bytes`).
- OWUI env contract (D-09): HIGH — verified against live docs source 2026-06-09; the PersistentConfig finding is the key correction.
- Embeddings runtime + model/dim (D-07/D-08): HIGH — feasibility and dimension/GGUF confirmed from authoritative sources.
- Footprint constant: MEDIUM — conservative estimate, refine on-hardware (Phase 19).

**Research date:** 2026-06-09
**Valid until:** 2026-07-09 for the OWUI env contract (fast-moving — re-verify against the pinned digest in Phase 20); 2026-09-09 for the in-repo pattern findings (stable).
