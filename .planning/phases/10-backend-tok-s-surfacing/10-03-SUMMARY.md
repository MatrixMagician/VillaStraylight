---
phase: 10-backend-tok-s-surfacing
plan: 03
subsystem: dashboard
tags: [dashboard, vanilla-js, backend-identity, rocm-readiness, tok-s, xss-safe, append-only]

# Dependency graph
requires:
  - phase: 10-backend-tok-s-surfacing (plan 01)
    provides: "Backend-aware status.Report (backend, image, gen_tokens_per_sec, rocm_readiness, schema_version) served verbatim by /api/status handleStatus"
  - phase: 05-dashboard
    provides: "vanilla-JS poll loop (renderHealth/renderPerformance/renderGPU), .badge-ready/warn/unknown + --status-* tokens, .health-row/.health-detail idioms"
provides:
  - "Dashboard Health panel: active backend + image rows (image omitted when unset; gray 'unavailable' badge when backend absent) + tri-state ROCm-readiness badge"
  - "Dashboard Performance panel: generation tok/s labeled by backend (number from /api/metrics, label from /api/status via lastBackend stash)"
  - "api_test assertions: /api/status carries backend/image/rocm_readiness; /api/metrics metricsView shape unchanged (no identity key)"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Compose identity (/api/status) + number (/api/metrics) in the JS, never duplicate identity into metricsView (D-01)"
    - "Tri-state badge reusing existing badge-ready/warn/unknown classes; unknown is the honest off-hardware default (no-false-green, D-04)"
    - "All server values via document.createElement + textContent (XSS-safe, no innerHTML interpolation) — mirrors renderHealth/renderGPU"

key-files:
  created: []
  modified:
    - internal/dashboard/assets/dashboard.js
    - internal/dashboard/api_test.go

key-decisions:
  - "Backend/image/readiness rows appended into the existing #health-rows after renderHealth — zero structural HTML change, zero new CSS (existing classes compose)"
  - "not-ready → badge-warn (amber), NOT badge-down (red) — red reserved for genuine failure (UI-SPEC color mapping)"
  - "(backend) suffix gated to the generating branch only; idle/activity-unknown/unavailable copy byte-unchanged (never label a fabricated 0)"
  - "/api/metrics test asserts an allowed-key allowlist so any future identity leak into metricsView fails the build (D-01 / T-10-10)"

patterns-established:
  - "readinessClass/readinessLabel tri-state mappers mirroring the existing healthClass/healthLabel idiom"
  - "renderBackend appends into #health-rows using the same .health-row build pattern as renderHealth"

requirements-completed: [DASH-06]

# Metrics
duration: 3min
completed: 2026-06-06
---

# Phase 10 Plan 03: Backend-aware control dashboard Summary

**The vanilla-JS control dashboard now renders the three approved UI-SPEC elements by composing the `Report` fields Plan 10-01 landed: the active backend + image tag and a tri-state ROCm-readiness badge in the Health panel, and the live generation tok/s labeled by the active backend in the Performance panel — all append-only, honest-Unknown, XSS-safe (textContent only), reusing existing badge/health-row idioms with zero new CSS, zero new endpoint, and no change to the `/api/metrics` shape.**

## Performance

- **Duration:** ~3 min
- **Started:** 2026-06-06T20:18Z
- **Completed:** 2026-06-06T20:21Z
- **Tasks:** 2 completed
- **Files modified:** 2

## Accomplishments
- Added a module-scoped `lastBackend` stashed from the `/api/status` poll (`report.backend`) — the SINGLE source of the Performance tok/s row's `(backend)` label, honoring D-01 (identity lives on `/api/status`, never `/api/metrics`).
- Implemented `renderBackend` (+ `readinessClass`/`readinessLabel` mappers) appending three rows into the existing `#health-rows` after `renderHealth`: a backend row (resolved name verbatim, or a gray `badge-unknown` "unavailable" when absent — never a fabricated default), an image row (omitted entirely when unset), and a tri-state ROCm-readiness badge (`ready`→`badge-ready` "ROCm ready", `not-ready`→`badge-warn` "ROCm not ready", `unknown`/absent→`badge-unknown` "ROCm readiness unknown" + a muted caption) — mirroring the GPU `busy_available` honest-Unknown badge precedent.
- Labeled the generation tok/s row by backend (`"12.3 tok/s (vulkan)"`) using `lastBackend`, gated to the generating branch ONLY; the idle, activity-unknown, and unavailable branches are byte-unchanged (never label a fabricated 0).
- Added `TestHandleStatusCarriesBackendIdentity` asserting `/api/status` carries `backend`/`image`/`rocm_readiness` as raw keys AND that they equal the shared core's values (handleStatus serves `Report` verbatim; readiness defaults to the honest `unknown` with no seam wired).
- Added `TestHandleMetricsShapeUnchanged` pinning the `metricsView` surface: it must carry NO `backend`/`image`/`rocm_readiness`/`schema_version` key, every present key is a metricsView field (allowlist), and the body still decodes into `metricsView` with `available=false` (D-01 / T-10-10 — identity never leaks into metrics).

## Task Commits

1. **Task 1: renderBackend + readiness badge + lastBackend stash** — `0510b32` (feat)
2. **Task 2: label tok/s by backend + assert status/metrics shapes** — `fe69300` (feat)

## Files Created/Modified
- `internal/dashboard/assets/dashboard.js` — Declared `lastBackend`; added `readinessClass`/`readinessLabel`/`renderBackend`; stashed `lastBackend` + called `renderBackend` in `poll()` after `renderHealth`; appended the `(backend)` suffix to the generation tok/s row (generating branch only).
- `internal/dashboard/api_test.go` — Added `TestHandleStatusCarriesBackendIdentity` and `TestHandleMetricsShapeUnchanged`.

## Deviations from Plan

None - plan executed exactly as written. (Zero structural HTML change and zero new CSS were required, as the plan anticipated — the existing `.health-row`/`.health-detail` and `.badge-ready/warn/unknown` classes compose the three additions; `internal/dashboard/assets/dashboard.html` and `dashboard.css` were not touched.)

## Threat Surface

All `<threat_model>` mitigations applied:
- **T-10-08 (XSS):** every server value (`backend`, `image`) set via `document.createElement` + `textContent`; no `innerHTML` interpolation of any report field. (The single pre-existing `innerHTML` at dashboard.js:117 is a static empty-state literal with no server value, untouched.)
- **T-10-09 (marker leak):** only the bare word "ROCm" appears in badge labels; `TestSeamGrepGate` green — no gated device/marker literal in any asset.
- **T-10-10 (identity into metrics):** `metricsView`/`api.go` untouched; `TestHandleMetricsShapeUnchanged` enforces the no-identity allowlist.
- **T-10-11 (telemetry):** no new endpoint, no external font/CDN/analytics; only the existing same-origin `/api/*` polls.
- **T-10-SC (installs):** no package installs; `git diff go.mod` empty.

## Verification Results

- `go build ./...` — Success
- `go vet ./...` — No issues
- `go test ./...` — 548 passed across 16 packages
- `go test ./internal/dashboard/` — 50 passed (incl. the 2 new tests)
- `go test ./internal/inference/ -run TestSeamGrepGate` — green (no backend-marker literal in any dashboard asset)
- `git diff --quiet cmd/villa/testdata/detect.golden.json` — byte-identical
- `git diff --quiet cmd/villa/testdata/status.json.golden` — byte-identical
- `git diff --quiet cmd/villa/testdata/recommend.golden.json` — byte-identical
- `git diff --quiet go.mod` — unchanged
- `git diff --quiet internal/dashboard/api.go` — unchanged (metricsView/handleMetrics untouched, D-01)
- No new HTTP endpoint, no new CSS color/size/weight/spacing token, no new dependency

## Manual-Only / Deferred (on-hardware UAT)

- Live ROCm-readiness badge rendering `ROCm ready`/`ROCm not ready` from a real gfx1151 readiness probe, and a non-zero gen tok/s labeled by the active backend under real generation — deferred to on-hardware UAT (same as Plan 10-01 and Phases 8/9). Off-hardware, the honest `unknown` badge + the typed-optional omit/label branches are unit-covered.

## Self-Check: PASSED

- Files verified present: `internal/dashboard/assets/dashboard.js`, `internal/dashboard/api_test.go`, `10-03-SUMMARY.md`
- Commits verified in git history: `0510b32`, `fe69300`
