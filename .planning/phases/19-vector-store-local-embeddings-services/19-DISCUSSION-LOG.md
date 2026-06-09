# Phase 19: Vector Store + Local Embeddings Services - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-09
**Phase:** 19-vector-store-local-embeddings-services
**Mode:** `--auto` (single pass — each decision auto-selected as the recommended option,
grounded in the PINNED Phase-18 spike decisions 18-DECISIONS.md D-07/D-08/D-09)
**Areas discussed:** Qdrant image/rootless-perms, villa-embed image + seam placement,
embed-model pre-staging, storage volume layout, embed server invocation, install proof, conditional render

---

## Qdrant image & rootless write-permission strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Unprivileged variant, digest-pinned | `qdrant/qdrant:v1.18.2-unprivileged` — runs non-root, avoids rootless UID/`:Z` write failure (SC#2) | ✓ |
| Standard image + explicit userns/`:Z` | `qdrant/qdrant` with manual permission handling | |

**Auto-selected:** Unprivileged variant (recommended) — directly satisfies SC#2 (Qdrant writable, no rootless permission failure).
**Notes:** Digest pin + Phase-19 legitimacy audit before freezing (D-01/D-03).

---

## villa-embed image source & seam placement

| Option | Description | Selected |
|--------|-------------|----------|
| Dedicated orchestrate const | New digest-pinned const + accessor mirroring `openWebUIImage` (managed-service path, D-10) | ✓ |
| Reference inference backend accessor | Pull the toolbox digest from `internal/inference` | |

**Auto-selected:** Dedicated orchestrate const (recommended) — matches D-10; managed-service image, not a GPU-backend token, keeps `TestSeamGrepGate` clean.
**Notes:** Reuses the pinned kyuz0 toolbox image per D-07 (no new image to audit).

---

## Embedding-model pre-staging (PRIV-04 / SC#3)

| Option | Description | Selected |
|--------|-------------|----------|
| Install-time one-time pull → villa-models volume | Reuse `internal/download`; runtime fully offline | ✓ |
| Init-container / image bake | Bake GGUF into an image layer | |

**Auto-selected:** Install-time pull into existing `villa-models` volume (recommended) — PRIV-04 forbids runtime download, not an install-time controlled fetch; reuses the single model store.
**Notes:** Pinned source `nomic-ai/nomic-embed-text-v1.5-GGUF` Q8_0 (~146 MB), mounted read-only (D-07).

---

## Storage volume layout

| Option | Description | Selected |
|--------|-------------|----------|
| Dedicated Qdrant volume + shared model store | `villa-qdrant.volume` → `/qdrant/storage:Z`; embed GGUF on existing `villa-models` | ✓ |
| Separate embed-models volume | New volume just for the embed GGUF | |

**Auto-selected:** Dedicated Qdrant volume; embed GGUF reuses `villa-models` (recommended) — durable named volume for vectors (SC#2), single model store for weights.

---

## villa-embed llama-server invocation

| Option | Description | Selected |
|--------|-------------|----------|
| Pinned spike values | `villa-embed:8080` `/v1/embeddings`, `--embeddings --pooling mean`, ctx 8192 | ✓ |

**Auto-selected:** As pinned by Phase-18 D-07/D-08 — container-DNS only, no host bind; pooling mode ≠ none required for the embeddings endpoint.

---

## Install-time proof (SC#3, SC#4)

| Option | Description | Selected |
|--------|-------------|----------|
| Offline embeddings smoke + Qdrant probe | Assert 768-dim vector returns with no network; assert Qdrant writable; reuse loopback audit | ✓ |
| Liveness/health-200 only | Treat container up as success | |

**Auto-selected:** Real offline `/v1/embeddings` smoke (768-length) + Qdrant writable probe (recommended) — guards the llama.cpp-master embeddings regression (#15406) behind the digest pin; a silent skip is a FAIL.

---

## Conditional rendering on `memory_enabled`

| Option | Description | Selected |
|--------|-------------|----------|
| Render units + Qdrant volume only when memory_enabled=true | Off → byte-identical to v1.2 install (Phase-18 SC#1 continuity) | ✓ |

**Auto-selected:** Conditional render (recommended) — continues the Phase-18 byte-identical guarantee; units regenerated from config, never hand-edited.

---

## Claude's Discretion

- Exact Go symbol names, Quadlet template filenames, unit names, accessor spellings.
- Whether the embed GGUF stage is a distinct install sub-step or folded into the existing weight-pull flow.
- Whether `QDRANT_API_KEY` stays empty for the private net or a generated key is added (no schema change; OWUI-facing choice deferred to Phase 20).

## Deferred Ideas

- OWUI env wiring + offline/telemetry lockdown + zero-outbound runtime smoke test — Phase 20.
- `ENABLE_QDRANT_MULTITENANCY_MODE` choice (before any vectors exist) — Phase 20.
- chats→Knowledge recall indexer — Phase 21.
- `recommend` footprint reservation + `preflight` gating + `doctor` checks — Phase 22.
- `status`/dashboard rows (schema 2→3), Qdrant volume backup/restore, memory-aware swap — Phase 23.
