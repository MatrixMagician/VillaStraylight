---
phase: 22-control-plane-fit-host-gate
plan: 01
subsystem: recommend
tags: [go, recommend, memory, fit-math, schema-bump, golden]

# Dependency graph
requires:
  - phase: 18-memory-spine
    provides: "internal/memory pure core (Footprint typed-Unknown, D-08 512 MiB pinned constant) + VillaConfig memory_* fields"
  - phase: 19-vector-store-embeddings
    provides: "villa-embed managed service whose resident footprint this reservation accounts for"
provides:
  - "Memory-aware recommend.Pick: embedding footprint reserved off the envelope BEFORE the chat-model fit (D-01, SC#1)"
  - "memory.ConservativeFootprintBytes() exported single-source 512 MiB conservative default (D-02)"
  - "recommend.MemoryInputs value struct (zero value = memory off = byte-identical math)"
  - "Recommendation schema 2: append-only embedding_reservation_bytes + memory_considered above SchemaVersion (D-03)"
  - "All 7 production Pick call sites thread explicit memory inputs; install MinMemBytes includes the reservation"
affects: [22-02-preflight-gate, 22-03-doctor, 23-surfacing-backup-swap, dashboard]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Reservation-before-fit: subtract subsystem reservations from the envelope BEFORE pick math so every downstream term sees the shrunken value"
    - "liveLoadedMemoryInputs() fail-soft cmd-tier helper (load error -> zero value, never an error path)"

key-files:
  created:
    - internal/memory/footprint_test.go
  modified:
    - internal/memory/footprint.go
    - internal/recommend/recommend.go
    - internal/recommend/recommend_test.go
    - cmd/villa/recommend.go
    - cmd/villa/install.go
    - cmd/villa/status.go
    - cmd/villa/model.go
    - cmd/villa/inference.go
    - cmd/villa/backend.go
    - cmd/villa/dashboard.go
    - cmd/villa/recommend_test.go
    - cmd/villa/testdata/recommend.golden.json

key-decisions:
  - "Memory-on refusal paths report the reservation as computed + MemoryConsidered=true (honest surface, asserted in TestPickRefusalStampsMemoryFields)"
  - "Refusal Notes prepend the D-02 conservative note (memNotes) so the conservative reservation is never silently dropped on a refusal"
  - "Call sites with a config already in scope (backend.go closure cfg, inference.go runValidation cfg, dashboard.go fail-soft cfg) build MemoryInputs from it directly instead of a redundant fresh load — same fail-soft semantics as the decision table"
  - "Install MinMemBytes includes rec.EmbeddingReservationBytes (Open Question 2 resolved YES); value flows from the pick so memory.Footprint stays the single source"

patterns-established:
  - "Reservation-before-fit: envelope shrinks first; uint64 clamp-to-0 (never wrap) falls into the existing no-fit refusal"
  - "Gated table rows on non-zero contract fields keep memory-off CLI output byte-identical (ROCmAdvice precedent)"

requirements-completed: [CTRL-01]

# Metrics
duration: 9min
completed: 2026-06-10
---

# Phase 22 Plan 01: Memory-Aware Fit Math + Schema-2 Recommend Contract Summary

**recommend.Pick now reserves the embedding-model footprint (pinned 512 MiB or honest conservative default) off the unified-memory envelope BEFORE the chat-model fit, surfaced as append-only schema-2 fields and threaded through all 7 production call sites including the install MinMemBytes gate**

## Performance

- **Duration:** 9 min
- **Started:** 2026-06-10T16:07:51Z
- **Completed:** 2026-06-10T16:16:59Z
- **Tasks:** 2 (Task 1 TDD: RED + GREEN commits)
- **Files modified:** 13

## Accomplishments

- D-01 envelope-shrinks-first: with `memory_enabled=true` the fit verdict, headroom, OOM guard and `UsableEnvelopeBytes` all compute against `envelope − reservation`; proven live on the gfx1151 host (`− embed reservation 0.500 GiB` row, shrunken envelope)
- D-02 honest conservative default: an unrecognized embedding model id reserves `memory.ConservativeFootprintBytes()` (512 MiB, single-source export) and appends a `RESERVED CONSERVATIVELY` note naming the model — never a silent 0
- D-03 append-only contract: `embedding_reservation_bytes` + `memory_considered` above `SchemaVersion`, `recommendSchemaVersion` 1→2, stamped on EVERY return path including the no-envelope refusal; recommend golden re-frozen exactly once (audit: only `recommend.golden.json` modified)
- Memory-off identity: zero-value `MemoryInputs` is provably byte-identical math — all 50 internal table tests + full `make check` (status/preflight/doctor/orchestrate goldens untouched) green; no-config CLI table contains no reservation row
- Pitfall 3 closed: install's pick seam threads persisted memory inputs and `MinMemBytes` adds the reservation; status's `liveWeightBytes` passes zero-value inputs on purpose (WeightBytes envelope-invariance guarded by `TestPickOverrideWeightInvariance`)

## Task Commits

Each task was committed atomically:

1. **Task 1: Memory-aware fit math in recommend.Pick + schema-2 contract (TDD)**
   - RED: `361f342` (test) — failing reservation-matrix, invariance, refusal-stamp and accessor-coherence tests
   - GREEN: `e24e168` (feat) — ConservativeFootprintBytes, MemoryInputs, reservation-before-fit, schema 2
2. **Task 2: Thread the 7 Pick call sites, gated reservation render, single golden re-freeze** - `dfc4f8c` (feat)

## Files Created/Modified

- `internal/memory/footprint.go` - exported `ConservativeFootprintBytes()` (unexported const + one-line accessor, orchestrate.EmbedImage shape)
- `internal/memory/footprint_test.go` - single-source coherence: accessor == pinned nomic-embed-text-v1.5 footprint, never zero
- `internal/recommend/recommend.go` - `MemoryInputs`, 4-arg `Pick`, `memoryReservation` helper, clamp-to-0 guard, schema-2 fields stamped in `finalizeRecommendation`
- `internal/recommend/recommend_test.go` - D-01/D-02/D-03 reservation matrix, weight-invariance guard, refusal-stamp tests
- `cmd/villa/recommend.go` - fail-soft memory-input sourcing, gated `− embed reservation` table row, `liveLoadedMemoryInputs()` helper
- `cmd/villa/install.go` - memory-aware pick seam + `MinMemBytes` includes `EmbeddingReservationBytes`
- `cmd/villa/status.go` - zero-value inputs in `liveWeightBytes` (frozen status.json.golden provably untouched)
- `cmd/villa/model.go`, `cmd/villa/inference.go`, `cmd/villa/backend.go`, `cmd/villa/dashboard.go` - one-line ripples threading persisted memory inputs fail-soft
- `cmd/villa/recommend_test.go` - fixture pinned at SchemaVersion 2
- `cmd/villa/testdata/recommend.golden.json` - the ONE intentional golden re-freeze (schema 2 + two new keys)

## Decisions Made

- Memory-on refusals report the reservation as computed (not zero) + `MemoryConsidered=true` — the honest surface the plan recommended; asserted in `TestPickRefusalStampsMemoryFields`
- Refusal `Notes` prepend the D-02 conservative note so a memory-on refusal with an unknown embed model still names the reservation it applied
- Sites with a config already in scope (backend-swap `FitsModel(cfg)` closure, inference `runValidation` cfg, dashboard fail-soft cfg) construct `MemoryInputs` from that config directly — same persisted/fail-soft semantics as `liveLoadedMemoryInputs()`, no redundant load

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Schema-2 recommend contract is frozen and threaded — Plan 22-02 (preflight vector-disk/headroom gate) can consume `memory.Footprint`/`ConservativeFootprintBytes` as the same single source
- Doctor (Plan 22-03) inherits the memory-aware fit surface; the residency-under-embed-load proof still needs the live seam composition (no analog — composed per 22-PATTERNS)
- On-hardware UAT for the full phase remains for the phase-close checkpoint (recommend table verified live on gfx1151 during execution)

## Self-Check: PASSED

All created/modified files exist on disk; commits 361f342, e24e168, dfc4f8c verified in git log.

---
*Phase: 22-control-plane-fit-host-gate*
*Completed: 2026-06-10*
