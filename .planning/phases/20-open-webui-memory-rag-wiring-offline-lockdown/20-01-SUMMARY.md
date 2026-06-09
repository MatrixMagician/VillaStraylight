---
phase: 20-open-webui-memory-rag-wiring-offline-lockdown
plan: 01
subsystem: orchestrate
tags: [openwebui, rag, qdrant, memory, env-wiring, golden, telemetry, INFRA-03]
requires:
  - "internal/memory.RenderView (Phase-18 resolved-values handoff)"
  - "internal/config memory fields (MemoryEnabled/EmbeddingModel/QdrantAddr/QdrantPort/EmbedAddr/EmbedPort)"
  - "villa-qdrant + villa-embed services (Phase-19, container-DNS on villa.network)"
provides:
  - "buildOpenWebUIView(mv memory.MemoryRenderInput, memoryEnabled bool) — parameterized, memory-aware OWUI render view"
  - "memory-ON OWUI container unit carrying the full D-09 RAG/Qdrant/memory env block (24 Environment= lines)"
  - "villa-openwebui.container.memory.golden (byte-frozen memory-ON contract)"
  - "memory-aware TestRenderOpenWebUITelemetryFrozen (per-view env-line freeze + telemetry-kill re-audit)"
affects:
  - "internal/orchestrate (render path now memory-aware for OWUI)"
  - "Phase-20 Plan 02/03 (runtime RAG smoke proof consumes the wired env)"
tech-stack:
  added: []
  patterns:
    - "config-sourced env composition via fmt (no re-typed host literals; seam gate green)"
    - "append-only byte-frozen render contract + single deliberate golden re-freeze"
    - "conditional env block gated on memory_enabled (byte-identical when off)"
key-files:
  created:
    - "internal/orchestrate/testdata/villa-openwebui.container.memory.golden"
  modified:
    - "internal/orchestrate/openwebui.go"
    - "internal/orchestrate/render.go"
    - "internal/orchestrate/render_test.go"
decisions:
  - "D-01 ENABLE_QDRANT_MULTITENANCY_MODE=True locked before any vector exists"
  - "D-02 D-09 keys appended as one ordered group after existing entries"
  - "D-03 ENABLE_PERSISTENT_CONFIG=False mandatory (kept last in the block)"
  - "D-04 conditional on memory_enabled; memory-off byte-identical; config-sourced values"
  - "D-05 single deliberate golden re-freeze (new memory golden only)"
metrics:
  duration: "~4 min"
  completed: 2026-06-09
  tasks: 3
  files: 4
requirements: [INFRA-03]
---

# Phase 20 Plan 01: Open WebUI Memory/RAG Env Wiring Summary

Parameterized `buildOpenWebUIView` so the Open WebUI container unit appends the full D-09
RAG/Qdrant/memory env block (config-sourced, 24 `Environment=` lines incl. the mandatory
`ENABLE_PERSISTENT_CONFIG=False`) only when `memory_enabled=true`, leaving a memory-off
render byte-identical to the v1.2 golden — the env-wiring half of INFRA-03, frozen by a new
memory golden + a memory-aware telemetry test.

## What Was Built

- **Task 1 — `buildOpenWebUIView` parameterized** (`internal/orchestrate/openwebui.go`,
  commit `4090c94`): signature changed to `(mv memory.MemoryRenderInput, memoryEnabled bool)`.
  The existing 11 env entries stay byte-identical and in order; when `memoryEnabled` is true a
  single ordered group of 13 D-09 keys is appended (24 total). `QDRANT_URI` and
  `RAG_OPENAI_API_BASE_URL` are composed from `mv` via `fmt.Sprintf` (no re-typed
  `villa-qdrant`/`villa-embed`/port literals). `ENABLE_PERSISTENT_CONFIG=False` is kept last as
  the load-bearing switch (D-03/T-20-01). `QDRANT_API_KEY` and `RAG_EMBEDDING_PREFIX_FIELD_NAME`
  intentionally omitted. Added `fmt` + `internal/memory` imports and a package/function doc
  block citing D-01..D-09.
- **Task 2 — render.go call site** (`internal/orchestrate/render.go`, commit `795dd5a`):
  `mv := memory.RenderView(in.Cfg)` is hoisted once above the OWUI render call; the call passes
  `buildOpenWebUIView(mv, in.Cfg.MemoryEnabled)`. The existing `if in.Cfg.MemoryEnabled` branch
  reuses the hoisted `mv` (no redundant `RenderView`). Template unchanged.
- **Task 3 — memory golden + memory-aware tests** (`internal/orchestrate/render_test.go` +
  `testdata/villa-openwebui.container.memory.golden`, commit `df41e47`): new
  `TestRenderOpenWebUIMemoryContainerGolden` asserts the memory-ON unit against the new golden;
  `TestRenderOpenWebUITelemetryFrozen` is now table-driven over memory-off/memory-on, freezing the
  exact `Environment=` line count per view (11 off / 24 on) from the same `buildOpenWebUIView`
  source of truth and re-auditing the telemetry-kill set in both views. Reused the pre-existing
  `memoryFixtureInput()` from `memory_test.go`.

## Verification Results

- `go test ./internal/orchestrate/... ./internal/inference/... -run 'OpenWebUI|Telemetry|SeamGrepGate'` — 10 passed.
- `git diff --exit-code internal/orchestrate/testdata/villa-openwebui.container.golden` — clean (memory-off byte-identical).
- New golden re-run without `-update` — byte-frozen (passes).
- Zero re-typed `villa-qdrant`/`villa-embed` literals in code (all 4 occurrences are comments).
- `make check` (vet + `go test ./...`) — fully green.

The new memory golden carries exactly 24 `Environment=` lines including `ENABLE_PERSISTENT_CONFIG=False`,
`VECTOR_DB=qdrant`, `ENABLE_QDRANT_MULTITENANCY_MODE=True`, `RAG_EMBEDDING_ENGINE=openai`,
`ENABLE_MEMORIES=True`, and `RAG_EMBEDDING_QUERY_PREFIX=search_query:`.

## Critical Invariants Honored

- Memory-OFF render byte-identical to the existing golden (guarded by the in-test `git diff --exit-code` criterion).
- `ENABLE_PERSISTENT_CONFIG=False` present in the memory-on block (D-03).
- Env values composed from `mv.*` via `fmt` — `TestSeamGrepGate` green, no allowlist edit.
- Golden contract evolved append-only: a NEW `villa-openwebui.container.memory.golden`; only it
  was re-frozen with `-update`.
- `make check` green.

## Deviations from Plan

None — plan executed exactly as written.

One small efficiency note (not a deviation): the plan's Task 3 action described adding a
`memoryFixtureInput()` helper, but an identical helper already existed in `memory_test.go`
(same package). Reused it rather than redeclaring (a redeclaration would not compile). The
existing helper already sets `MemoryEnabled:true` plus the exact default memory fields the
golden requires, so the memory-ON fixture is stable in CI as intended.

## Known Stubs

None. The memory-on env block is fully wired from config; no placeholder/empty-value paths.

## Self-Check: PASSED

- internal/orchestrate/openwebui.go — FOUND
- internal/orchestrate/render.go — FOUND
- internal/orchestrate/render_test.go — FOUND
- internal/orchestrate/testdata/villa-openwebui.container.memory.golden — FOUND
- commit 4090c94 — FOUND
- commit 795dd5a — FOUND
- commit df41e47 — FOUND
