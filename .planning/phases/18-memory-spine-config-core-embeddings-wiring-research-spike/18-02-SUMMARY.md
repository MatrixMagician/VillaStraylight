---
phase: 18-memory-spine-config-core-embeddings-wiring-research-spike
plan: 02
subsystem: memory
tags: [go, pure-core, memory, embeddings, typed-unknown, fail-closed, seam-gate, renderview]

# Dependency graph
requires:
  - phase: 18-01
    provides: "VillaConfig memory_* fields (MemoryEnabled, EmbeddingModel, EmbeddingDim, QdrantAddr/Port, EmbedAddr/Port) — the typed input this core consumes"
  - phase: v1.2 (shipped)
    provides: "internal/detect.Bytes typed-Unknown wrapper; recommend.Pick pure-core idiom; TestSeamGrepGate"
provides:
  - "internal/memory pure decision core (zero host I/O, no os/exec, no image literal)"
  - "memory.Footprint(modelID) -> detect.Bytes (KnownBytes for pinned model, typed-Unknown on miss)"
  - "memory.Decide(cfg) Decision — fail-closed enablement-and-fields-valid gate"
  - "memory.RenderView(cfg) MemoryRenderInput — resolved-values-only orchestrate handoff struct"
affects: [phase-19, phase-20, phase-22, phase-23]

# Tech tracking
tech-stack:
  added: []  # ZERO new dependencies; go.mod / go.sum unchanged
  patterns:
    - "Pure decision core mirroring recommend.Pick: typed input -> typed decision, no I/O"
    - "Typed-Unknown footprint (detect.Bytes Known=false) instead of bare 0 on a catalog miss"
    - "Fail-closed gate accumulating refuse-with-reason strings (all problems surfaced in one pass)"
    - "Resolved-values-only render handoff (addr/port pieces, no composed URL, no image literal) per D-10"

key-files:
  created:
    - internal/memory/footprint.go
    - internal/memory/memory.go
    - internal/memory/memory_test.go
  modified: []

key-decisions:
  - "Exported symbols: Footprint, Decide, RenderView, Decision, MemoryRenderInput (exactly as planned)"
  - "Footprint constant: nomic-embed-text-v1.5 -> 512<<20 (512 MiB) in the single embedFootprints map; flagged for on-hardware refinement in Phase 19 (D-08)"
  - "Decision struct layout {Enabled bool; Valid bool; Reasons []string} as planned"
  - "MemoryRenderInput carries 6 resolved fields (EmbeddingModel, EmbeddingDim, QdrantAddr, QdrantPort, EmbedAddr, EmbedPort) — no URL, no image literal (D-02c/D-10)"
  - "EmbeddingDim<=0 and ports<=0 treated as invalid (not just ==0) for defensive fail-closed validation"

patterns-established:
  - "internal/memory is a NEW pure core; orchestrate (later) owns the image identity and URL composition (D-10)"
  - "Footprint is offload-honest: never bare 0 — callers test .Known to distinguish 'no footprint known' from a real zero"

requirements: [INFRA-04]

metrics:
  duration: "~7 minutes"
  completed: 2026-06-09
  tasks_completed: 3
  files_created: 3
  files_modified: 0
  commits: 2
---

# Phase 18 Plan 02: internal/memory pure decision core Summary

Added the pure `internal/memory` decision-core triad the v1.3 memory stack composes — `Footprint` (embedding model → `detect.Bytes`, typed-Unknown on miss), `Decide` (fail-closed enablement-and-fields-valid gate), and `RenderView` (a resolved-values-only orchestrate handoff struct) — with zero host I/O and no container-image literal, keeping `TestSeamGrepGate` green (SC#2).

## What Was Built

- **`internal/memory/footprint.go`** — `Footprint(modelID string) detect.Bytes`. The `embedFootprints` map is the single home of the constant: `nomic-embed-text-v1.5 → 512<<20` (512 MiB conservative resident reservation, D-08). A hit returns `detect.KnownBytes(b, "memory: pinned embedding footprint reservation")`; any miss (unknown id OR empty string) returns `detect.UnknownBytes(reason, modelID)` — Known=false, never a bare-zero sentinel. Imports only `internal/detect`.
- **`internal/memory/memory.go`** — `Decide(cfg config.VillaConfig) Decision` and `RenderView(cfg config.VillaConfig) MemoryRenderInput`. `Decide` is fail-closed: memory off → `{Enabled:false, Valid:true}`; memory on validates every required field (embedding model non-empty; embedding dim > 0; qdrant addr non-empty + port > 0; embed addr non-empty + port > 0), accumulating one user-facing `Reasons` entry per offending field. It does no I/O and never panics. `RenderView` maps the cfg memory fields one-for-one into `MemoryRenderInput` — resolved values only (addr/port pieces, no composed URL, no image literal; orchestrate adds those later per D-10). Imports only `internal/config`.
- **`internal/memory/memory_test.go`** — table-driven `TestFootprint`, `TestDecide`, `TestDecideAccumulatesReasons`, `TestRenderView` (15 assertions/subtests total).

## Exported Symbols (as planned, no deviation)

| Symbol | Kind | File |
|--------|------|------|
| `Footprint(modelID string) detect.Bytes` | func | footprint.go |
| `Decide(cfg config.VillaConfig) Decision` | func | memory.go |
| `RenderView(cfg config.VillaConfig) MemoryRenderInput` | func | memory.go |
| `Decision{Enabled bool; Valid bool; Reasons []string}` | struct | memory.go |
| `MemoryRenderInput{EmbeddingModel string; EmbeddingDim int; QdrantAddr string; QdrantPort int; EmbedAddr string; EmbedPort int}` | struct | memory.go |

Footprint constant value: **`512 << 20` (512 MiB)**, in exactly one place (the unexported `embedFootprints` map).

## How It Was Verified

- `go test ./internal/memory/ -count=1` — Footprint / Decide / RenderView all green (15 subtests).
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` — **seam gate green with internal/memory present** (no image/exec literal leaked; `internal/memory` is not in the seam allowlist, so the gate actively covers it). `seam_test.go` was NOT modified.
- `make check` (vet + `go test ./...`) — full suite green, including the new `internal/memory` package.
- `go.mod` / `go.sum` unchanged — zero new dependencies (T-18-SC: no supply-chain checkpoint required).
- Manual audit: neither non-test file imports `os/exec` (the only occurrences are doc-comment prose stating the invariant), and neither contains any container-image token (`kyuz0`, `docker.io/`, `server-vulkan`, `:rocm-*`).

## TDD Gate Compliance

Tasks 1 and 2 followed RED → GREEN:
- Task 1: `TestFootprint` written first → build failed (`undefined: Footprint`) → `footprint.go` implemented → 4 passed. Committed as `feat(18-02)` (3ae6771).
- Task 2: `TestDecide`/`TestRenderView` written first → build failed (`undefined: Decide`, `undefined: RenderView`) → `memory.go` implemented → 11 passed. Committed as `feat(18-02)` (129fd13).

No REFACTOR step was needed (implementations were minimal and clean on first GREEN). Both RED and GREEN gates are represented; the plan-level pattern combines the test and implementation of each pure unit into a single `feat` commit per the project's table-driven convention (tests live beside source in the same package).

## Deviations from Plan

None — plan executed exactly as written. The planner's expected symbol names, struct layouts, footprint constant, and config field bindings (`MemoryEnabled`, `EmbeddingModel`, `EmbeddingDim`, `QdrantAddr`, `QdrantPort`, `EmbedAddr`, `EmbedPort` — confirmed authoritative in 18-01-SUMMARY.md) all matched; nothing required adjustment. One defensive refinement within plan discretion: ports and dim are validated as `<= 0` (not merely `== 0`) so a negative hand-edited value is also refused (strengthens the T-18-03 fail-closed control).

## Threat Mitigations Applied

- **T-18-03 (Tampering, Decide gate):** fail-closed validation — memory-on with any missing/invalid field returns `Valid:false` with reasons; never silent-accept, never panic. Proven by `TestDecide` + `TestDecideAccumulatesReasons`.
- **T-18-04 (Information Disclosure, RenderView):** `MemoryRenderInput` carries only container-DNS addr/port pieces from config; it composes no routable URL and widens no bind. Proven by `TestRenderView` (asserts addrs are bare DNS names, not host:port/URL).
- **T-18-ARCH (architectural integrity):** `internal/memory` imports no container-image literal and no `os/exec`; `TestSeamGrepGate` enforces it. The image identity lands in `orchestrate` later (D-10).

## Notes for Later Phases

- Phase 19 (`orchestrate`): consume `RenderView` → compose `http://villa-embed:8080/v1` and `http://villa-qdrant:6333` and own the Qdrant + villa-embed image literals (D-10). The 512 MiB footprint constant is flagged for on-hardware measurement/refinement here.
- Phase 22 (`recommend.Pick`): call `Footprint` to reserve embedding memory BEFORE chat-model fit on shared gfx1151 GTT (D-03/CTRL-01).
- Phase 23: `EmbeddingDim` is the load-bearing anchor for the memory-aware model-swap guard / backup manifest.
- This plan added NO call site — it only PROVIDES the functions.

## Self-Check: PASSED

- Files: `internal/memory/footprint.go`, `internal/memory/memory.go`, `internal/memory/memory_test.go` — all present.
- Commits: 3ae6771, 129fd13 — both present in git history.
