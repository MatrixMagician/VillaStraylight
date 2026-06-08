---
phase: 15-cumulative-usage-tracking
plan: 04
subsystem: dashboard
tags: [usage, dashboard, sole-writer, usageMu, atomic-write, ui, typed-unknown, no-new-endpoint]
status: executed-checkpoint-pending
requires:
  - "internal/usage core (Plan 15-01): Fold/UsageTotals/Sample/Deps/WriteFileAtomic/Save/Load/UsagePath"
  - "internal/metrics (Plan 15-02): CounterSample + ScrapeCounters(endpoint)"
  - "status.Report.Usage *usage.UsageTotals omitempty field (Plan 15-03)"
provides:
  - "dashboard Config+Server usage writer seams: usageMu + ReadUsage/WriteUsage/ModelID/CounterSample (nil-defaulted honest no-ops)"
  - "handleMetrics foldUsage: SOLE usageMu-guarded fold+atomic-write of usage.json (model identity captured in-section)"
  - "cmd/villa live wiring: liveUsageDeps/liveReadUsageTotals/liveWriteUsage/liveModelID + ScrapeCounters(same endpoint)"
  - "dashboard.js renderCumulativeUsage: read-only cumulative rows from report.usage inside #performance-body, honest empty/unavailable states"
  - "TestMetricsWritesUsage (sole-writer + reset-aware-through-handler + typed-Unknown skip + live-view byte-identical) + TestStatusUsageSurfaced (same field, no new endpoint)"
affects:
  - "Plan 15 wave merge / make check"
  - "On-hardware UAT (Task 3, this plan) — pending live gfx1151 verification"
tech-stack:
  added: []
  patterns:
    - "dedicated mutex sibling to swapMu (usageMu Lock/defer-Unlock — fold never silently skipped, unlike swapMu TryLock-409)"
    - "in-section model-identity capture (Pitfall 2 / T-15-14) so per-model fold key cannot drift mid-write"
    - "loud-but-non-fatal write (T-15-17): WriteUsage error never changes the live metricsView"
    - "stable child container (#cumulative-usage) re-appended after the /api/metrics clear so two polls coexist (D-10)"
    - "typed-Unknown UI render (D-05/D-09): muted copy when absent/unreadable, never a fabricated 0"
key-files:
  created: []
  modified:
    - internal/dashboard/server.go
    - internal/dashboard/api.go
    - internal/dashboard/api_test.go
    - cmd/villa/dashboard.go
    - internal/dashboard/assets/dashboard.js
decisions:
  - "Seams live as new Server fields (not encapsulated in the store seam) — sibling to the existing swapMu/swapDeps shape (CONTEXT.md D-12-area discretion)"
  - "foldUsage runs FIRST in handleMetrics, independent of the live-view scrape result, so cumulative counters accumulate regardless of rate-gauge idle/available state (counters are monotonic lifetime totals)"
  - "ModelID re-reads cfg.Model via config.LoadVilla at scrape time (config is single source of truth, Pitfall 2) rather than capturing a snapshot at wiring time"
  - "UI: report.model has no status.Report field, so renderCumulativeUsage falls back to the sole per-model entry — honest and correct since the dashboard writes only the current model's key"
metrics:
  duration: ~12 min
  completed: 2026-06-07
  tasks_automated: 2
  tasks_total: 3
  files: 5
---

# Phase 15 Plan 04: Dashboard Sole-Writer + Cumulative-Usage UI Summary

Made the dashboard `/api/metrics` handler the SOLE, `usageMu`-guarded writer of `usage.json`
(folds the two monotonic `_total` counters + the in-section `cfg.Model` into the per-model
store and atomically writes — D-07), and surfaced the cumulative totals read-only in the
dashboard Performance panel from the SAME `status.Report.usage` field (Plan 03, no new
endpoint — D-10), with honest typed-Unknown empty/unavailable states (never a fabricated 0).

**Status:** Both autonomous CODE tasks (1 & 2) are complete, committed, and green. Task 3 is
an on-hardware UAT (`gate="blocking"` human-verify on live gfx1151) — it CANNOT be run in
this environment (no AMD Strix Halo hardware) and is surfaced as a checkpoint, NOT fabricated.

## What Was Built

### Task 1 — Sole-writer fold+atomic-write (commit 9248de6)

- **`dashboard.Server` + `Config` seams:** `usageMu sync.Mutex` (sibling to `swapMu`) plus
  `ReadUsage func() usage.UsageTotals`, `WriteUsage func(usage.UsageTotals) error`,
  `ModelID func() string`, and `CounterSample func() (metrics.CounterSample, bool)`. All
  nil-defaulted in `NewServer` to honest no-ops (ReadUsage→empty store, WriteUsage→silent
  no-op that never writes, ModelID→"", CounterSample→typed-Unknown unavailable) so a
  partially-wired Server never writes `usage.json` and never nil-panics.
- **`handleMetrics` → `foldUsage`:** the sole-writer hook runs FIRST (independent of the
  live-view scrape result so the monotonic counters accumulate regardless of rate-gauge
  idle/available state). It scrapes the `CounterSample`, then under `usageMu.Lock()/defer
  Unlock()` captures `model := s.modelID()` (Pitfall 2 / T-15-14 — identity read INSIDE the
  section so the per-model key cannot drift), `prior := s.readUsage()`, builds a
  `usage.Sample` carrying each counter's `Known` flag through to the pure `usage.Fold`
  (a `Known=false` counter contributes NO fold — D-05, no fabricated 0), and
  `_ = s.writeUsage(next)` — loud-but-non-fatal: a write error never changes the live
  `metricsView` (T-15-17 / D-10). The live `metricsView` shape is UNCHANGED.
- **`cmd/villa` live wiring:** `liveUsageDeps` (ReadAll over `usage.UsagePath()` → (nil,nil)
  when absent; WriteAll = `usage.WriteFileAtomic`), `liveReadUsageTotals` (`usage.Load`,
  fail-closed to empty), `liveWriteUsage` (`usage.Save` atomic temp+rename), `liveModelID`
  (`cfg.Model` re-read at scrape time), and `CounterSample: ScrapeCounters(endpoint)` over
  the SAME loopback endpoint already scraped for live tok/s — no new outbound (D-12).
- **Tests:** `TestMetricsWritesUsage` proves end-to-end through the handler: the fold runs
  keyed by the in-section `"m1"`; a SECOND scrape whose raw counter dropped (reset)
  continues cumulative reset-aware (100→+30 = 130, not a drop to 30); a `Known=false`
  prompt counter is NOT folded (prompt unchanged, no LastSeenRaw mutation); the live
  `metricsView` JSON is byte-identical to the pre-change unavailable shape; and an
  unavailable counter scrape writes nothing.

### Task 2 — Cumulative-totals UI (commit 01bc9d8)

- **`renderCumulativeUsage(report)`** renders from `report.usage` (the `/api/status` poll,
  D-10) — NO new endpoint, NO new fetch — into a stable `#cumulative-usage` child of
  `#performance-body`. Because `renderPerformance` clears `#performance-body` on every
  `/api/metrics` poll, the live rows were split into `renderPerformanceLive` and the
  cumulative box is re-appended (`ensureCumulativeBox`) after each clear so the two polls
  coexist without clobbering.
- **Honest states (D-05/D-09):** status-poll failure → muted `Cumulative usage unavailable`;
  report present but no usage for the model → muted `No usage recorded yet` + the UI-SPEC
  body copy; totals present → two `metricRow` lines (`prompt tokens (total)` /
  `generated tokens (total)`) with thousands-grouped integer counts (`toLocaleString`),
  counts-only (never tok/s), `textContent` only (XSS-safe). Zero new CSS tokens/charts/nav.

## Tasks Completed

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Dashboard sole-writer (usageMu + fold+atomic-write + live wiring) | 9248de6 | internal/dashboard/server.go, internal/dashboard/api.go, internal/dashboard/api_test.go, cmd/villa/dashboard.go |
| 2 | Dashboard UI cumulative rows from report.usage | 01bc9d8 | internal/dashboard/assets/dashboard.js |
| 3 | On-hardware UAT (gfx1151) | — | CHECKPOINT (blocking human-verify) — pending live hardware |

## Verification

- `go test ./internal/dashboard -run 'TestMetricsWritesUsage'` — passed.
- `go test ./internal/dashboard -run 'TestStatusUsageSurfaced'` — passed.
- `go test ./internal/dashboard ./cmd/villa` — 262 passed.
- `go test ./...` — 659 passed across 19 packages (incl. `TestSeamGrepGate`).
- `go build ./...` — clean. `go vet ./internal/dashboard ./cmd/villa` — no issues.
- **Sole-writer gate (D-07):** `grep -rn 'usage.Save|usage.WriteFileAtomic|WriteUsage'
  cmd/villa/status.go internal/status` → CLEAN (no writer in the status read path).
- **Fabricated-0 gate (UI):** `grep -nE '0\.0|toFixed\(1\) \+ " tok/s".*total' dashboard.js`
  → CLEAN (cumulative rows are grouped integer counts, never tok/s, never a literal-0 fallback).
- **UI-SPEC copy present:** `report.usage`, `prompt tokens (total)`, `generated tokens (total)`,
  `No usage recorded yet`, `Cumulative usage unavailable` — all found; reuses `metricRow(`/
  `mutedP(`; adds no `fetch(` for usage.

## Threat Model Coverage

| Threat ID | Mitigation in this plan |
|-----------|-------------------------|
| T-15-13 (torn usage.json under concurrent scrapes) | `usageMu.Lock()/defer Unlock()` serializes the whole ReadUsage→Fold→WriteUsage; live WriteAll = `usage.WriteFileAtomic` temp+rename. |
| T-15-14 (model-identity drift mid-accumulation) | `model := s.modelID()` captured INSIDE the usageMu section; `liveModelID` re-reads `cfg.Model` at scrape time. |
| T-15-15 (new outbound exfil) | `CounterSample = ScrapeCounters(endpoint)` reuses the SAME loopback endpoint + bound + timeout as live tok/s; no new host/port literal. **Live no-new-socket proof deferred to Task 3 UAT.** |
| T-15-16 (fabricated 0 / content in UI) | typed-Unknown muted copy (`No usage recorded yet` / `Cumulative usage unavailable`) when absent/unreadable; counts-only rows, exact UI-SPEC copy, textContent-only. |
| T-15-17 (write error stalling live response) | fold+write is loud-but-non-fatal: `_ = s.writeUsage(next)`; live `metricsView` asserted byte-identical by `TestMetricsWritesUsage`. |
| T-15-SC (package installs) | accept — NO package installs this phase (stdlib + existing first-party only). |

## Deviations from Plan

None substantive — plan executed as written. Two implementation choices (documented in
frontmatter `decisions`): (1) `foldUsage` runs first in `handleMetrics`, independent of the
live-view scrape result, so cumulative counters accumulate even when the rate gauges are
idle/unavailable (counters are monotonic lifetime totals, semantically distinct from the
last-window rate gauges); (2) the UI falls back to the sole per-model store entry because
`status.Report` exposes no `model` field — honest and correct since the dashboard writes only
the current model's key.

## Deferred Issues / Out-of-Scope

- `cmd/villa/bench_compare.go` is not gofmt-clean (a Phase-14 artifact unrelated to this
  plan's files). Logged to `deferred-items.md`; left untouched per the executor scope
  boundary. Suggest a separate `make fmt` sweep.

## Known Stubs

None. The dashboard writer is fully wired live (ReadUsage/WriteUsage/ModelID/CounterSample
all bound to real host I/O in `liveDashboardDeps`); the UI reads the real `report.usage`
field. No placeholder/mock data flows to the UI.

## On-Hardware UAT (Task 3) — PENDING

Task 3 is a `gate="blocking"` human-verify checkpoint that can ONLY be proven on a live
gfx1151 host (monotonic counter growth during generation, reset-aware continuation across an
`llama-server` restart, and no-new-socket/no_telemetry posture). This executor has no AMD
Strix Halo hardware, so these live behaviors are NOT fabricated. The plan is therefore
`executed` with the UAT checkpoint pending — see the returned CHECKPOINT REACHED block for
the operator runbook (build → restart dashboard service → 4-step verification).

## Self-Check: PASSED

- FOUND: internal/dashboard/server.go (contains `usageMu`)
- FOUND: internal/dashboard/api.go (contains `usageMu` + `usage.Fold(` + in-section model capture)
- FOUND: internal/dashboard/api_test.go (contains `func TestMetricsWritesUsage`, `func TestStatusUsageSurfaced`)
- FOUND: cmd/villa/dashboard.go (references `usage.` + `cfg.Model`)
- FOUND: internal/dashboard/assets/dashboard.js (contains `report.usage` + UI-SPEC copy)
- FOUND commit: 9248de6
- FOUND commit: 01bc9d8
