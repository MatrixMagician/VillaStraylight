---
phase: 10-backend-tok-s-surfacing
plan: 01
subsystem: api
tags: [status, read-model, rocm-readiness, tok-s, golden, typed-optional, seam]

# Dependency graph
requires:
  - phase: 06-backend-resolver-reroute
    provides: "inference.BackendFor(cfg.Backend) resolver + backend.ResidencyProof() markers feeding status.Run (SC#1 residency already wired)"
  - phase: 07-rocm-render-unit-preflight-detect
    provides: "detect.ROCmReadiness 5-Bool typed-Optional sub-tree (consumed, never recomputed)"
  - phase: 05-dashboard
    provides: "internal/metrics.ScrapeMetrics / IsGenerating / ScrapeSlots collector (reused verbatim for the CLI tok/s)"
provides:
  - "Backend-aware internal/status.Report: tail-appended Backend, Image, GenTokensPerSec (*float64,omitempty), ROCmReadiness tri-state, SchemaVersion"
  - "Pure foldROCmReadiness helper (unknown wins over not-ready, no-false-green)"
  - "GenTokensPerSec + ROCmReadiness Deps seams wired in liveStatusDeps (reuse metrics collector + detect.Probe)"
  - "villa status table rows: backend, image (-v), gen tok/s (labeled, omitted when idle), rocm-readiness"
  - "Re-frozen status.json.golden (pure-addition diff); SC#1 off-hardware ROCm-residency proof"
affects: [10-02-recommend, 10-03-dashboard]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Tail-append to a frozen --json struct + one-time golden re-freeze (Phase-7 discipline)"
    - "Typed-optional figure: *float64 + omitempty, never a fabricated 0 (mirrors metricsView.LatencyMS)"
    - "Consume-don't-recompute tri-state fold with Known-first short-circuit to unknown"
    - "Deps seam reuse of the bounded metrics collector (no new scraper / attack surface)"

key-files:
  created: []
  modified:
    - internal/status/status.go
    - internal/status/status_test.go
    - cmd/villa/status.go
    - cmd/villa/status_test.go
    - cmd/villa/testdata/status.json.golden

key-decisions:
  - "reportSchemaVersion = 1 (first versioned Report contract; tail-appended additive field, D-07)"
  - "Image tag rendered only under -v (compact default table); --json carries it unconditionally (Open Q1)"
  - "tok/s seam returns nil on both scrape-fail AND idle — one typed-Unknown path, never a 0 (D-03)"
  - "SC#1 proven off-hardware via a cfg.Backend=rocm fixture with a ROCm0-only journal asserting offload PASS"

patterns-established:
  - "foldROCmReadiness: order-independent worst-wins fold; any !Known short-circuits to unknown"
  - "Backend identity sourced only from resolved backend.Name()/Image() — no marker literal in surfacing code"

requirements-completed: [DASH-06]

# Metrics
duration: 18min
completed: 2026-06-06
---

# Phase 10 Plan 01: Backend-aware status read-model Summary

**`villa status` (table + `--json`) and the shared `internal/status.Report` now surface the resolved active backend + image tag, live token-generation tok/s (typed-optional, omitted when idle/unavailable), and a tri-state ROCm-readiness indicator folded from the detect sub-tree — all tail-appended with the status golden re-frozen exactly once as a pure-addition diff.**

## Performance

- **Duration:** ~18 min
- **Started:** 2026-06-06T20:02Z
- **Completed:** 2026-06-06T20:20Z
- **Tasks:** 3 completed
- **Files modified:** 5

## Accomplishments
- Tail-appended five fields to the frozen `Report` (`Backend`, `Image`, `GenTokensPerSec`, `ROCmReadiness`, `SchemaVersion`) honoring append-only discipline — nothing above moved; `SchemaVersion` is the last tagged field, the unexported `err` stays after it and never serializes.
- Added a pure, order-independent `foldROCmReadiness` that consumes (never recomputes) the detect `rocm_readiness` 5-Bool sub-tree, short-circuiting to `unknown` on any unevaluable signal (no-false-green) so off-hardware the honest answer is `unknown`.
- Wired the live tok/s seam to REUSE the dashboard-proven, bounded `metrics.ScrapeMetrics`/`ScrapeSlots`/`IsGenerating` collector — nil on scrape-fail and on idle, so the figure is omitted (typed-Unknown), never a fabricated 0.
- Proved SC#1 off-hardware: on a `cfg.Backend="rocm"` install the residency verdict keys on the resolved `backendROCm.ResidencyProof()` `ROCm0` markers (a ROCm0-only journal reaches PASS — a Vulkan default could not), confirming the already-wired correctness without touching the `Markers: backend.ResidencyProof()` wiring.
- Re-froze `status.json.golden` exactly once as a reviewed pure-addition diff; `detect.golden.json` and `recommend.golden.json` stayed byte-identical.

## Task Commits

Each task was committed atomically:

1. **Task 1: Tail-append Report fields + foldROCmReadiness + Deps seams** - `4617e2b` (feat)
2. **Task 2: Wire tok/s + readiness seams in liveStatusDeps + table rows + SC#1 proof** - `22a044c` (feat)
3. **Task 3: Re-freeze status.json.golden once (pure-addition diff)** - `d1bb386` (test)

_Note: Tasks 1 and 2 are `tdd="true"` struct-extension tasks; because the RED test references types that don't exist until the GREEN implementation (the package won't compile otherwise), each task's test + implementation landed as a single atomic feat commit rather than separate test→feat commits._

## Files Created/Modified
- `internal/status/status.go` — Appended 5 Report fields; added `ROCmReadinessIndicator` enum + consts, `reportSchemaVersion`, pure `foldROCmReadiness`, two `Deps` seam members; populated identity/tok-s/readiness/schema in `Run` (residency wiring untouched).
- `internal/status/status_test.go` — Readiness fold table (4 cases), backend-aware field population test, SC#1 `cfg.Backend="rocm"` residency proof keying on `ROCm0`.
- `cmd/villa/status.go` — Imported `internal/metrics`; added `liveGenTokensPerSec` (collector reuse, nil on idle/fail); wired both new seams in `liveStatusDeps`; added backend/image(-v)/tok-s/rocm-readiness rows to `renderStatusTable`.
- `cmd/villa/status_test.go` — Added `GenTokensPerSec`/`ROCmReadiness` stub knobs to `newStatusDeps`; tok/s typed-optional cases (generating→value+label, idle→omitted, unavailable→omitted).
- `cmd/villa/testdata/status.json.golden` — Re-frozen once; new keys `backend`, `image`, `rocm_readiness`, `schema_version` appended at the tail (`gen_tokens_per_sec` omitted under the idle fixture).

## Deviations from Plan

None - plan executed exactly as written.

## Verification Results

- `go build ./...` — Success
- `go vet ./...` — No issues
- `go test ./...` — 539 passed across 16 packages
- `go test ./internal/inference/ -run TestSeamGrepGate` — green (no backend-marker literal leaked into surfacing `.go`; the `ROCm0` token appears only in `_test.go`, which the gate excludes)
- `git diff --quiet cmd/villa/testdata/detect.golden.json` — byte-identical (DETECT_UNCHANGED)
- `git diff --quiet cmd/villa/testdata/recommend.golden.json` — byte-identical (owned by Plan 02)
- `git diff --quiet go.mod` — unchanged (no new deps, v1.1 constraint)
- status golden diff reviewed: pure tail-addition (only the trailing `,` after `"overall"` changed for JSON syntax; no existing key reordered/renamed/retyped)

## Manual-Only / Deferred (on-hardware UAT)

- Live ROCm residency surfaced in `villa status` on a real gfx1151 ROCm install (DASH-06 SC#1) — deferred to on-hardware UAT, same as Phases 8/9. Off-hardware coverage (the ROCm0-marker proof) is complete.
- Live token-generation tok/s rendered + labeled by backend under real generation — deferred to on-hardware UAT; off-hardware the typed-optional omit/render branches are unit-covered.

## Notes for Downstream Plans

- **Plan 10-02 (recommend)** and **Plan 10-03 (dashboard)** render these same `Report` fields; the golden re-froze HERE exactly once (D-06), so no later plan touches `status.json.golden`. Plan 02 owns `recommend.golden.json`.
- The dashboard `/api/status` poll picks up the new `Report` fields for free; Plan 03 composes backend identity (status) + tok/s number (metrics) per D-01 — do NOT add backend identity to `metricsView`.

## Self-Check: PASSED

- Files verified present: `internal/status/status.go`, `cmd/villa/status.go`, `cmd/villa/testdata/status.json.golden`, `10-01-SUMMARY.md`
- Commits verified in git history: `4617e2b`, `22a044c`, `d1bb386`
