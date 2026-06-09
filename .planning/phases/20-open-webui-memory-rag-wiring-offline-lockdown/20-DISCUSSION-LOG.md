# Phase 20: Open WebUI Memory/RAG Wiring + Offline Lockdown - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-09
**Phase:** 20-open-webui-memory-rag-wiring-offline-lockdown
**Mode:** `--auto` (single pass; recommended option auto-selected per area)
**Areas discussed:** Qdrant multitenancy mode, OWUI env-block re-freeze, Auto memory extraction default, Document KB + retrieval-prefix quality, Runtime zero-outbound proof

---

## Qdrant multitenancy mode (the load-bearing pending choice)

| Option | Description | Selected |
|--------|-------------|----------|
| `ENABLE_QDRANT_MULTITENANCY_MODE=True` | OWUI default + Qdrant-recommended single-collection, tenant-partitioned layout; lock before any vectors exist | ✓ |
| `ENABLE_QDRANT_MULTITENANCY_MODE=False` | Collection-per-knowledge; simpler conceptually for a single user but wastes RAM and diverges from upstream default | |

**Auto-selection:** `True` (recommended). Matches OWUI default (integrate-not-rebuild),
is Qdrant's own recommended layout, and is the forward-compatible substrate for the
Phase-21 indexer + Phase-23 backup. Flagged: MUST be re-verified against the pinned
OWUI digest and locked before the first vector is written (flipping later disconnects
existing collections).
**Notes:** This was the one decision D-09 explicitly left PENDING — now resolved (D-01).

---

## OWUI env-block evolution + golden re-freeze

| Option | Description | Selected |
|--------|-------------|----------|
| Append ordered group, gated on `memory_enabled`, single deliberate golden re-freeze | Preserve existing env order; append the D-09 RAG/Qdrant/memory keys; byte-identical when memory off | ✓ |
| Interleave / restructure the env slice | Reorder for grouping — breaks the byte-frozen golden unnecessarily | |

**Auto-selection:** Append-only ordered group (recommended). `ENABLE_PERSISTENT_CONFIG=False`
is mandatory (without it the new keys are silently ignored after first boot). One
intentional golden re-freeze + telemetry-test re-audit. (D-02..D-05)
**Notes:** Env values sourced from resolved config / Phase-18 render-view — no re-typed literals.

---

## Automatic memory extraction default (MEM-03 / SC#2)

| Option | Description | Selected |
|--------|-------------|----------|
| Default OFF, user-toggleable | Opt-in given the local-model extraction-quality caveat; not silently default-on | ✓ |
| Default ON | Silently enables LLM-assisted extraction — violates SC#2's opt-in requirement | |

**Auto-selection:** Default OFF, toggleable (recommended; SC#2 mandates opt-in). Research
must identify the exact OWUI mechanism (env / ConfigVar / function-filter) and confirm
it under `ENABLE_PERSISTENT_CONFIG=False`. (D-07)
**Notes:** `ENABLE_MEMORIES=True` enables the manual memory feature (MEM-01/02/04); auto-extraction is the separate opt-in.

---

## Document KB + retrieval-prefix quality (KB-01/02/03, D-08 caveat)

| Option | Description | Selected |
|--------|-------------|----------|
| Verify OWUI prefix support; set if exposed, else record limitation, don't block | nomic wants `search_document:`/`search_query:` prefixes; functional retrieval + citations is the bar, not maximal recall | ✓ |
| Block phase on optimal-recall prefix wiring | Over-scopes the phase on retrieval tuning | |

**Auto-selection:** Verify + set-if-exposed, else record limitation (recommended). Local
chunk/embed/retrieve through `villa-embed` + Qdrant; citations native. (D-08/D-09)

---

## Runtime zero-outbound proof (PRIV-05 / SC#4) — headline new gate

| Option | Description | Selected |
|--------|-------------|----------|
| Extend the Phase-19 proof seam: firewalled upload→retrieve smoke, asserts no external host | Pure core + injected probes; FIXED-arg podman over `villa.network`; silent skip = FAIL | ✓ |
| Install-time green only | Insufficient — SC#4 explicitly demands a runtime firewalled test | |

**Auto-selection:** Extend the proof seam (recommended). Drives the real RAG path under
an egress-blocked network; honesty-by-construction (no false-green). (D-10/D-11)

---

## Claude's Discretion

- Exact Go symbol/spelling and env-pair ordering within the appended group.
- Whether the smoke test is wired into `install` readiness vs a dedicated verify
  subcommand vs an on-hardware verification-wave step.
- The egress-blocking mechanism for the runtime test (`--network none` for the offline
  embedding leg vs firewalled `villa`-only network with an outbound sentinel).
- Whether `QDRANT_API_KEY` stays empty or a generated key is added (no schema change).

## Deferred Ideas

- chats→Knowledge semantic recall indexer — Phase 21.
- `recommend` footprint reservation + `preflight` gating + `doctor` health — Phase 22.
- `status`/dashboard memory rows (schema 2→3), Qdrant backup/restore, memory-aware swap — Phase 23.
