---
phase: 23-surfacing-backup-memory-aware-swap
plan: 03
subsystem: dashboard SPA / memory surfacing
tags: [dashboard, memory-panel, ctrl-02, xss-safe-dom, typed-unknown, no-new-endpoints]
requires:
  - Plan 23-01 (status.Report v3 — report.memory contract, per-service memory health rows)
  - 23-UI-SPEC.md (binding design contract — placement, badge/state table, copywriting)
provides:
  - dashboard memory-panel section (hidden by default, aria-labelledby, after Models)
  - renderMemory(report) — show/hide on report.memory presence, UI-SPEC badge mapping
  - memoryBadgeRow helper (renderGPU busy-row precedent, metric-row + badge)
  - TestHandleStatusMemoryPassthrough (memory-on presence / memory-off absence + v3 pin)
affects:
  - 23-05 (on-hardware visual verification — dashboard-restart gotcha applies there)
tech-stack:
  added: []
  patterns:
    - hidden-in-static-shell panel unhidden by poll-presence (connection-banner precedent)
    - createElement + textContent XSS-safe DOM (renderHealth idiom, zero innerHTML delta)
    - typed-Unknown gray badge / amber degraded / omit-row-when-unproven honesty mapping
key-files:
  created: []
  modified:
    - internal/dashboard/assets/dashboard.html (memory-panel section, hidden)
    - internal/dashboard/assets/dashboard.js (renderMemory + memoryBadgeRow + poll call site)
    - internal/dashboard/api_test.go (stubMemoryStatusDeps + TestHandleStatusMemoryPassthrough)
decisions:
  - "Badge rows reuse the renderGPU busy-row shape (.metric-row + .badge, no .metric-value) — zero new CSS, existing badge-ready/warn/unknown vocabulary only"
  - "Empty-state heading reuses .model-empty-heading verbatim (UI-SPEC: mirrors the Models empty state)"
  - "Count/last-indexed rows guard on BOTH state (indexed/incomplete) AND field presence — an incomplete run whose omitempty fields are absent renders no NaN/zero-fill"
  - "dashboard.css untouched — every required class already existed"
metrics:
  duration: ~10 min
  completed: 2026-06-10
  tasks: 2
  commits: 2
---

# Phase 23 Plan 03: Dashboard Memory Panel Summary

**One-liner:** Memory panel per the approved 23-UI-SPEC — ships `hidden` in the static shell, unhidden only when `report.memory` arrives on the existing /api/status poll (D-03: no new fetch/endpoint/probe), XSS-safe createElement+textContent rows with the honesty badge mapping (typed-Unknown gray, incomplete/skew amber never red, unproven rows omitted never zero-filled), plus the Go-side passthrough test pinning memory-on presence and memory-off absence at schema_version 3.

## What was built

### Task 1 — Memory panel markup + renderMemory (commit 775537a)
- `dashboard.html`: `<section class="panel" id="memory-panel" aria-labelledby="memory-heading" hidden>` appended after the Models panel (last in `main.content`), heading `Memory`, empty `#memory-body` filled by JS. Memory-off DOM renders pixel-identical to v1.2 (panel stays hidden).
- `dashboard.js`:
  - `memoryBadgeRow(label, text, cls)` — the renderGPU busy-row precedent (`.metric-row` whose value slot is a `.badge`).
  - `renderMemory(report)` — `report.memory` absent/null ⇒ `hidden = true` and return (re-hides if the field disappears); present ⇒ unhide + rebuild:
    - `embedding model` / `dimension` rows (mono verbatim via `metricRow`).
    - `indexed chats` (`groupThousands`) and `last indexed` (verbatim RFC3339 from `last_index_completed_at`) render ONLY for `recall_state` indexed/incomplete AND only when the field is actually present — never zero-filled.
    - `recall index` state badge: indexed ⇒ `indexed`/badge-ready; incomplete ⇒ `incomplete`/badge-warn + muted re-run copy; empty ⇒ no badge, `.model-empty-heading` "No recall index yet" + muted body; unknown/unexpected ⇒ `unavailable`/badge-unknown + muted copy (typed-Unknown gray, never green/red).
    - skew: `embedding_skew === "mismatch"` ⇒ `embedding config` row, `mismatch`/badge-warn (amber, NOT red) + muted remediation copy; otherwise the row is omitted entirely (never a green ok).
  - Call site added in the /api/status poll success handler after `renderCumulativeUsage`; NOT called from the catch path — a failed poll keeps last-good content under the existing `setConnected` stale dimming (no spinner, no animation).
  - All five UI-SPEC copy strings present verbatim; all DOM via `createElement` + `textContent`.
- `dashboard.css`: **zero changes** — `.metric-row`, `.badge`, `.badge-ready/-warn/-unknown`, `.muted`, `.model-empty-heading` all pre-existed; no `--accent` consumer added.

### Task 2 — Go-side passthrough test (commit f456e54)
- `stubMemoryStatusDeps(t)`: stubStatusDeps re-based on `config.DefaultVillaConfig()` with `MemoryEnabled=true`, render output containing the real villa-qdrant/villa-embed units (Pitfall 8 coherence, mirrors the 23-01 fixture), healthy `QdrantHealth`/`EmbedHealth` seams, complete-run `ReadRecallState`.
- `TestHandleStatusMemoryPassthrough`: memory-on body carries the `memory` object with `embedding_model`/`embedding_dim`/`recall_state` keys (the exact names renderMemory reads); memory-off body omits the `memory` key entirely (omitempty-nil, D-04) and pins `schema_version == 3`.
- No schema-pin "re-fixes" — per the 23-01 SUMMARY note, no v2 pins ever existed in this package; this plan only ADDED the passthrough cases.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug-prevention guard] count/timestamp rows additionally guard on field presence**
- **Found during:** Task 1
- **Issue:** the status core populates `indexed_chats`/`last_index_completed_at` ONLY for a complete run (omitempty) — for `recall_state: incomplete` the fields are absent, and a state-only gate would have rendered `groupThousands(undefined)` → "NaN".
- **Fix:** rows render only when state is indexed/incomplete AND the field is present (`typeof === "number"` / non-empty string) — consistent with the plan's "never zero-fill" directive.
- **Files modified:** internal/dashboard/assets/dashboard.js
- **Commit:** 775537a

**2. [Note] new comments avoid the literal "innerHTML"**
- The acceptance criterion pins `grep -c "innerHTML"` to the pre-task value (4). Two new doc comments initially said "never innerHTML"; rephrased to "never HTML interpolation" so the literal count stays at baseline while the code itself adds zero innerHTML uses (git diff confirmed: no `+` code line touches innerHTML).

## Known Stubs

None — the hidden-by-default panel is the spec'd memory-off state (D-03 opt-in discipline), not a stub; renderMemory is fully wired to the live /api/status poll.

## Threat Flags

None — no new endpoint, fetch, probe, port, or dependency. T-23-12 (XSS) mitigated by createElement+textContent (innerHTML count unchanged); T-23-13 (false assurance) mitigated by the typed-Unknown gray / mismatch-only amber mapping; T-23-14/T-23-SC accepted as pre-registered (loopback posture and zero-toolchain unchanged).

## Verification

- `go build ./...`, `go vet ./internal/dashboard`, `node --check dashboard.js` — all clean.
- `go test ./internal/dashboard` — 55 passed; `make check` green (vet + full suite).
- `grep -c innerHTML dashboard.js` = 4, identical to HEAD~ baseline; git diff adds no innerHTML code line.
- `git diff --stat internal/status cmd/villa` empty — frozen v3 contract untouched (Pitfall 1).
- Memory-off DOM: panel carries `hidden` in the static shell; renderMemory re-asserts `hidden=true` when `report.memory` is absent.
- On-box visual check (restart `villa-dashboard.service` after `make build`) deferred to Plan 23-05 per plan.

## Commits

| Commit | Type | Content |
|--------|------|---------|
| 775537a | feat | Memory panel markup + renderMemory + memoryBadgeRow + poll call site |
| f456e54 | test | stubMemoryStatusDeps + TestHandleStatusMemoryPassthrough (on/off + v3 pin) |

## Self-Check: PASSED

Both commits verified in git log; both modified asset files and the test file exist on disk (2026-06-10).
