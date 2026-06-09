# Phase 18: Memory Spine — config core + embeddings/wiring research spike - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-09
**Phase:** 18-memory-spine-config-core-embeddings-wiring-research-spike
**Mode:** `--auto` (single pass; recommended option auto-selected per area)
**Areas discussed:** memory-core API, config schema, embeddings runtime, embedding model + footprint, OWUI env contract, seam placement

---

## `internal/memory` pure core API

| Option | Description | Selected |
|--------|-------------|----------|
| Pure decision core (recommend.Pick idiom) | Typed inputs → typed decisions, no host I/O, no image literals; footprint fn + enablement gate + render-view struct | ✓ |
| Thin helpers on config | Add memory logic as methods on VillaConfig | |

**User's choice (auto):** Pure decision core mirroring `recommend.Pick`.
**Notes:** Keeps `TestSeamGrepGate` green; no `os/exec`, no image literal. (D-01/D-02/D-03)

---

## config.toml memory schema

| Option | Description | Selected |
|--------|-------------|----------|
| Flat self-healing fields | `memory_*` flat fields via existing `normalizeVilla`/`defaultConfig` pattern, default off, not emitted until opt-in | ✓ |
| New `[memory]` table / separate file | Grouped TOML sub-table or dedicated memory config file | |

**User's choice (auto):** Flat self-healing fields, `memory_enabled=false` default.
**Notes:** SC#1 byte-identical guarantee for existing v1.2 installs; reuse XDG/0600/traversal-guard Save discipline. (D-04/D-05/D-06)

---

## Embeddings runtime

| Option | Description | Selected |
|--------|-------------|----------|
| Dedicated `villa-embed` llama-server | Reuse pinned toolbox image, OpenAI `/v1/embeddings`, container-DNS only | ✓ |
| OWUI built-in embedder | Let Open WebUI run embeddings itself | |

**User's choice (auto):** Dedicated `villa-embed`. **Flagged for spike confirmation.**
**Notes:** OWUI built-in lazily downloads embed model from HF (privacy risk #1); dedicated server gives memory-fit control and reuses an already-pinned image. (D-07)

---

## Embedding model + footprint

| Option | Description | Selected |
|--------|-------------|----------|
| `nomic-embed-text-v1.5` (~768-dim GGUF) | Pin model + dimension; measure footprint in spike | ✓ |
| Larger/alternate embedder | Higher-dim model | |

**User's choice (auto):** `nomic-embed-text-v1.5`, ~768-dim; spike confirms exact GGUF source/quant + measured bytes.
**Notes:** Dimension is load-bearing/pinned (no auto-reindex on change). (D-08)

---

## Open WebUI env contract

| Option | Description | Selected |
|--------|-------------|----------|
| Re-verify keys against pinned digest, record only | Confirm exact RAG/Memory/offline keys now; freeze wiring in Phase 20 | ✓ |
| Assume documented keys | Trust prior notes without re-verification | |

**User's choice (auto):** Re-verify against the pinned OWUI digest; record keys; do not write any env block this phase. (D-09)

---

## Seam placement for new image literals

| Option | Description | Selected |
|--------|-------------|----------|
| Orchestrate managed-service render path | `openwebui.go` pattern; outside `inference.BackendFor`/`TestSeamGrepGate` | ✓ |
| inference.BackendFor | Treat embeddings as a GPU backend | |

**User's choice (auto, carried forward):** Orchestrate managed-service path. Phase 18 introduces no image literal but designs the core so the literal lands in `orchestrate` later. (D-10)

---

## Claude's Discretion
- Exact Go symbol names, struct field layout, TOML key spellings (planner's call within D-04/D-05).
- Whether spike confirmations live in RESEARCH.md or a decisions appendix (researcher's call; D-07/D-08/D-09 must be explicitly recorded with evidence).

## Deferred Ideas
- Render/start Qdrant + `villa-embed` units — Phase 19.
- OWUI env wiring + offline lockdown + zero-outbound test — Phase 20.
- chats→Knowledge recall indexer — Phase 21.
- recommend footprint reservation + preflight gating + doctor checks — Phase 22.
- status schema bump + dashboard rows + backup/swap coverage — Phase 23.
