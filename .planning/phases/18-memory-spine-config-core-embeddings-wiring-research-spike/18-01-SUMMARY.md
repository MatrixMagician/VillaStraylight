---
phase: 18-memory-spine-config-core-embeddings-wiring-research-spike
plan: 01
subsystem: config
tags: [go, toml, villaconfig, memory, embeddings, qdrant, open-webui, config-schema]

# Dependency graph
requires:
  - phase: v1.2 (shipped)
    provides: "internal/config/villaconfig.go dashboard_*/chat_* self-healing flat-field precedent + SaveVilla XDG/0600/traversal discipline"
provides:
  - "VillaConfig memory_* fields (memory_enabled, embedding_model, embedding_dim, qdrant_addr/port, embed_addr/port) — default-OFF, self-healing"
  - "Byte-identical guarantee proven for non-opted-in v1.2 installs (no memory save path added)"
  - "18-DECISIONS.md — canonical record of spike decisions D-07/D-08/D-09"
affects: [phase-18-plan-02, phase-19, phase-20, phase-21, phase-22, phase-23]

# Tech tracking
tech-stack:
  added: []  # ZERO new dependencies; go.mod unchanged
  patterns:
    - "Config field extension with single-source defaults (defaultConfig) + self-heal (normalizeVilla)"
    - "Memory endpoint addr fields are container-DNS only; normalizeVilla never widens a bind (PRIV-01)"
    - "Byte-identical guarantee = absence of a save path, not omitempty marshalling"

key-files:
  created:
    - .planning/phases/18-memory-spine-config-core-embeddings-wiring-research-spike/18-DECISIONS.md
  modified:
    - internal/config/villaconfig.go
    - internal/config/villaconfig_test.go

key-decisions:
  - "TOML key spellings: flat memory_enabled / embedding_model / embedding_dim / qdrant_addr / qdrant_port / embed_addr / embed_port (mirrors dashboard_*/chat_* precedent, D-04)"
  - "MemoryEnabled is NOT self-healed (false is a valid explicit default/choice); all other memory fields self-heal from defaultConfig()"
  - "No memory save path added — the absence of a save IS the SC#1 byte-identical guarantee"

patterns-established:
  - "Memory config fields default-OFF + self-heal from the single defaultConfig() source"
  - "Container-DNS-only endpoint defaults filled by normalizeVilla; never a routable bind"

requirements-completed: [INFRA-04]

# Metrics
duration: 4min
completed: 2026-06-09
---

# Phase 18 Plan 01: Memory config core + spike decisions Summary

**Default-OFF, self-healing `memory_*` fields added to `VillaConfig` (Go struct, TOML, single-source defaults) so a v1.2 install stays byte-identical until opt-in, plus a canonical 18-DECISIONS.md recording the pinned spike decisions D-07/D-08/D-09.**

## Performance

- **Duration:** ~4 min
- **Started:** 2026-06-09T15:56:14Z
- **Completed:** 2026-06-09T16:00:18Z
- **Tasks:** 3
- **Files modified:** 2 (1 source, 1 test) + 1 doc created

## Accomplishments
- Extended `VillaConfig` with seven memory fields, defaults seeded ONLY in `defaultConfig()` (the single home of the literals), self-healed in `normalizeVilla` deriving every fill from `defaultConfig()`.
- Proved the SC#1 byte-identical guarantee with `TestMemoryByteIdentical`: loading a memory-key-free v1.2 config does not mutate the on-disk file and leaks no memory key.
- Recorded the three pinned spike decisions (D-07/D-08/D-09) as the canonical `18-DECISIONS.md`, with `ENABLE_PERSISTENT_CONFIG=False` flagged MANDATORY and `ENABLE_QDRANT_MULTITENANCY_MODE` marked "CHOICE PENDING — Phase 20".
- `make check` green end-to-end, including `TestSeamGrepGate` (this plan introduces no image/exec literal).

## Memory field names + TOML key spellings (Plan 02 and downstream phases bind these)

| Go field | TOML key | Default | Notes |
|----------|----------|---------|-------|
| `MemoryEnabled bool` | `memory_enabled` | `false` | Gate; NOT self-healed (false is a valid choice) |
| `EmbeddingModel string` | `embedding_model` | `nomic-embed-text-v1.5` | Self-heals empty → default |
| `EmbeddingDim int` | `embedding_dim` | `768` | Load-bearing pinned dim; self-heals 0 → default |
| `QdrantAddr string` | `qdrant_addr` | `villa-qdrant` | Container-DNS only; never widened |
| `QdrantPort int` | `qdrant_port` | `6333` | Self-heals 0 → default |
| `EmbedAddr string` | `embed_addr` | `villa-embed` | Container-DNS only; never widened |
| `EmbedPort int` | `embed_port` | `8080` | Self-heals 0 → default |

**No memory save path was added.** `SaveVilla`/`SaveVillaTo`/`assertInsideDir` bodies are untouched; the byte-identical guarantee is the absence of a memory write in Phase 18.

## Task Commits

Each task committed atomically (TDD tasks have a test→feat pair):

1. **Task 1: memory fields (RED)** - `98133f6` (test)
2. **Task 1: memory fields (GREEN)** - `7b93ad3` (feat)
3. **Task 2: byte-identical guarantee** - `4330a13` (test)
4. **Task 3: spike decisions doc** - `b693970` (docs)

**Plan metadata:** committed separately (docs: complete plan)

## Files Created/Modified
- `internal/config/villaconfig.go` - Added 7 memory fields to `VillaConfig`; seeded defaults in `defaultConfig()`; extended `normalizeVilla` to self-heal zero/empty memory fields without widening a bind.
- `internal/config/villaconfig_test.go` - Added `TestDefaultConfigMemoryFields`, `TestLoadMemoryDefaultsOff`, `TestLoadMissingReturnsMemoryDefaults`, `TestNormalizeMemorySelfHeal`, `TestMemoryNeverWidensBind`, `TestMemoryPreservesExplicitNonDefault`, `TestMemoryByteIdentical`; updated `TestSaveLoadRoundTrip` literal for the extended struct.
- `.planning/.../18-DECISIONS.md` - Canonical record of D-07/D-08/D-09 with pinned values, rationale, downstream consumer phase, and evidence pointers to 18-RESEARCH.md.

## Decisions Made
- **TOML key spellings (D-04 discretion):** flat `memory_*`/`embedding_*`/`qdrant_*`/`embed_*` keys, mirroring the `dashboard_*`/`chat_*` precedent. These are the names Plan 02 and Phases 19–23 bind.
- **`MemoryEnabled` is not self-healed:** a bool's `false` is both its valid default and a meaningful explicit choice, so `normalizeVilla` leaves it exactly as parsed (only the inert string/int fields self-heal).

## Deviations from Plan
None - plan executed exactly as written.

## Issues Encountered
- An `Edit` against the `normalizeVilla` doc comment failed to match (em-dash / unicode in the existing comment). Resolved by anchoring the edit on the function body signature instead and writing the new doc comment with ASCII dashes. No behavior impact.

## Known Stubs
None - the memory fields are fully wired into load/normalize. Plan 02 (`internal/memory`) consumes them; the footprint constant (~512 MiB) is a recorded conservative estimate flagged for on-hardware refinement in Phase 19 (documented in 18-DECISIONS.md, not a stub).

## User Setup Required
None - no external service configuration required (Phase 18 renders/starts nothing).

## Next Phase Readiness
- Plan 02 (`internal/memory` pure core) can now import the `VillaConfig` memory fields — Wave 1 dependency satisfied.
- Phases 19/20 have the canonical env contract + runtime/model decisions to freeze against (18-DECISIONS.md).
- Open item carried forward (recorded, not a blocker): `ENABLE_QDRANT_MULTITENANCY_MODE` choice is pending for Phase 20 (must be decided before any vectors exist); the ~512 MiB embedding footprint is a conservative estimate pending on-hardware measurement in Phase 19.

## Self-Check: PASSED
- `internal/config/villaconfig.go` — FOUND (modified)
- `internal/config/villaconfig_test.go` — FOUND (modified)
- `.planning/.../18-DECISIONS.md` — FOUND (created)
- Commits `98133f6`, `7b93ad3`, `4330a13`, `b693970` — all FOUND in git log

---
*Phase: 18-memory-spine-config-core-embeddings-wiring-research-spike*
*Completed: 2026-06-09*
