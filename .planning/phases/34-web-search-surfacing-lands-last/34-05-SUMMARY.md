---
phase: 34-web-search-surfacing-lands-last
plan: 05
subsystem: dashboard
tags: [dashboard, web-search, SURF-05, hidden-until-data, xss-safe, tri-state, honesty-by-construction, no-build-ui]
status: complete

# Dependency graph
requires:
  - phase: 34-03
    provides: "status.Report schema 5 web_search block {enabled, outbound_bounded tri-state, verify_checked_at} — the contract this panel binds verbatim off the existing /api/status poll"
provides:
  - "#web-search-panel section in dashboard.html (after #agent-panel, ships hidden)"
  - "renderWebSearch(report) cloning renderAgent — reads report.web_search off the EXISTING /api/status poll (zero new fetch/endpoint/probe)"
  - "tri-state outbound indicator: bounded->green badge-ready, not-bounded->amber badge-warn, stale/absent->gray badge-unknown + remediation caption (no false-green)"
  - "egress-checked RFC3339 row (verbatim, omitted when absent)"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Hidden-until-data panel cloned verbatim from renderMemory/renderAgent (unhide on report.X present, re-hide when absent — pixel-identical when off)"
    - "Tri-state security indicator: green only for a real recent verify PASS; stale/absent -> gray Unknown, never false-green (T-34-17)"
    - "XSS-safe DOM: every server/web-derived value via createElement + textContent, never innerHTML (T-34-16)"
    - "Omit-when-absent: source-gap rows never fabricated (no zero / 'never' placeholder)"

key-files:
  created: []
  modified:
    - internal/dashboard/assets/dashboard.html
    - internal/dashboard/assets/dashboard.js

key-decisions:
  - "Rows 1-2 (villa-searxng / villa-websafe health) are NOT rendered inside the Web Search panel: per the frozen 34-03 contract these services surface as report.services[] rows owned by the Health panel, not as web_search sub-fields. Cloning them here would duplicate the Health panel and fabricate a second source — so they are omitted from the Web Search panel by design (omit-when-absent)."
  - "Rows 4 (verify freshness) renders as 'egress checked' ONLY when verify_checked_at is present (carried only for a non-stale result). Rows 5-7 (guard counters, last query, fetched URLs) are OMITTED entirely — the status core (Plan 03) ships no host source for them; the renderer never fabricates."
  - "Zero new CSS classes/tokens introduced (git diff dashboard.css is empty). The panel reuses memoryBadgeRow / metricRow / mutedP and the existing badge-ready/warn/unknown vocabulary verbatim."
  - "Outbound 'unknown' caption uses the UI-SPEC never-verified copy ('Outbound bound not verified — run villa verify search to confirm egress is bounded.'). The status core's 24h freshness gate collapses a stale prior PASS to 'unknown' with no verify_checked_at, so the never-run caption is the honest single message for every gray state; the stale-variant copy can be added later if the core ever distinguishes stale-vs-never at the JSON layer."

patterns-established:
  - "SURF-05 Web Search panel: read-only, hidden-until-data, tri-state outbound honesty, textContent-only — the most untrusted-data-bearing panel, so XSS-safety is non-negotiable."

requirements-completed: [SURF-05]

# Metrics
duration: ~10min (implementation; human-verify checkpoint pending)
completed: 2026-06-21
status: complete
---

# Phase 34 Plan 05: Dashboard Web Search panel (SURF-05) Summary

**A hidden-until-data Web Search panel cloned verbatim from `renderAgent`, reading `report.web_search` off the EXISTING `/api/status` poll (zero new fetch/endpoint/probe), rendering an honest tri-state outbound indicator (green only for a real recent verify PASS) with every value via `textContent` — web-search-off dashboards stay pixel-identical to v1.4.**

## Performance

- **Tasks:** 2 (Task 1 implementation complete; Task 2 is a blocking human-verify checkpoint — pending operator sign-off on the live dashboard)
- **Files modified:** 2
- **CSS:** zero new classes/tokens (dashboard.css unchanged)

## Accomplishments

- **HTML:** Added ONE `<section class="panel" id="web-search-panel" aria-labelledby="web-search-heading" hidden>` after `#agent-panel` (the last panel), mirroring the agent-panel chrome + the UI-SPEC comment block verbatim, with `#web-search-body`.
- **JS:** Declared `webSearchPanel` / `webSearchBody` beside the `memoryPanel`/`agentPanel` vars, and added `renderWebSearch(report)` cloning `renderAgent`'s shape exactly (guard → `var ws = report && report.web_search; if (!ws) { hidden = true; return; }` → unhide + `textContent=""` reset → build rows).
- **Wiring:** `renderWebSearch(report)` is called from `poll()`'s `.then` IMMEDIATELY AFTER `renderAgent(report)` (NOT from `.catch` — last-good stays under the global stale-dim).
- **Tri-state outbound (honesty-by-construction):** `bounded` → `memoryBadgeRow("outbound","bounded","ready")` (green); `not-bounded` → `("outbound","not bounded","warn")` (amber, never red); else → `("outbound","unavailable","unknown")` (gray) + the UI-SPEC remediation caption.
- **Freshness row:** `egress checked` → verbatim RFC3339 `verify_checked_at`, rendered ONLY when present (no fabricated relative time).
- **Omit-when-absent:** source-gap rows (guard counters, last query, fetched URLs) never render — no host source exists per Plan 03; the renderer fabricates nothing.
- **XSS:** every value via `createElement` + `textContent`; the renderer adds NO `innerHTML` assignment (T-34-16).

## Rendered Rows (as built, vs. UI-SPEC row order)

| # | UI-SPEC row | Rendered here? | Why |
|---|-------------|----------------|-----|
| 1 | villa-searxng health | NO (omitted) | Not a `web_search` sub-field — surfaces as a `report.services[]` row in the Health panel (frozen 34-03 contract) |
| 2 | villa-websafe health | NO (omitted) | Same as row 1 |
| 3 | Outbound bounded | YES | tri-state badge from `web_search.outbound_bounded` (+ caption on gray) |
| 4 | Verify freshness | YES (when present) | `egress checked` → verbatim `verify_checked_at`; omitted when absent |
| 5 | Guard verdicts | NO (omitted) | No host source (Plan 03 accepted scope limit) — never fabricated |
| 6 | Last query | NO (omitted) | No host source — never fabricated |
| 7 | Outbound visibility (query/URL) | NO (omitted) | No host source (ephemeral content excluded by design, SURF-07) — never fabricated |

## Tri-state Outbound Mapping (binding)

| `outbound_bounded` | Badge | Class | Caption |
|--------------------|-------|-------|---------|
| `"bounded"` | bounded | `badge-ready` (green) | none; `egress checked` row when `verify_checked_at` present |
| `"not-bounded"` | not bounded | `badge-warn` (amber) | none; `egress checked` row when present |
| `"unknown"` / absent / unexpected | unavailable | `badge-unknown` (gray) | `Outbound bound not verified — run villa verify search to confirm egress is bounded.` |

## Accepted Scope Limits (stated, not silent)

- **searxng/websafe health rows live in the Health panel**, not the Web Search panel (the 34-03 contract surfaces them as `report.services[]`, not `web_search` sub-fields). Rendering them here would duplicate the Health panel and require a second source — out of scope.
- **Guard counters, last_query, fetched URLs** have NO host source (Plan 03 accepted scope limits / SURF-07 ephemeral-content exclusion). The renderer OMITS these rows; it never fabricates a 0 / "never" / placeholder.
- **No new CSS class/token** was introduced (UI-SPEC goal met): `git diff internal/dashboard/assets/dashboard.css` is empty.

## Task Commits

1. **Task 1: hidden-until-data Web Search panel (HTML) + renderWebSearch (JS)** — `5af36d2` (feat)
2. **Task 2: human-verify checkpoint** — PENDING operator sign-off on the live dashboard (blocking, autonomous=false; not self-approved).

## Deviations from Plan

None — plan executed as written. The one reconciliation worth noting (not a deviation, an explicit honesty call already anticipated by the plan's ACCEPTED SCOPE LIMIT): the UI-SPEC's row-1/2 source field cited `web_search.services[]`, but the frozen 34-03 JSON contract carries searxng/websafe ONLY as `report.services[]` Health-panel rows. Per the plan's omit-when-absent directive ("rows 4-7 are absent today per Plan 03 — they simply don't render"), the same rule applies to any field `web_search` does not carry. The panel therefore renders the outbound tri-state (+ optional egress-checked row) and omits every source-gap row.

## Automated Verification

- `grep -n 'web-search-panel' internal/dashboard/assets/dashboard.html` → matches; section ships with `hidden`, placed AFTER `#agent-panel`. ✓
- `grep -n 'function renderWebSearch' internal/dashboard/assets/dashboard.js` → matches; called in `poll()`'s `.then` after `renderAgent(report)`. ✓
- `grep -c 'report.web_search\|report && report.web_search'` → 4 (>= 1; reads the existing poll, no new fetch). ✓
- No `innerHTML` assignment added by `renderWebSearch` (the only `innerHTML` in the file is the pre-existing static string in `renderHealth`, line 154). ✓
- `git diff internal/dashboard/assets/dashboard.css` empty (zero new classes). ✓
- `go test ./internal/dashboard/` — PASS.
- `make check` (vet + full test) — green across all packages.

## Pending Human-Verify Checkpoint (Task 2 — blocking, NOT self-approved)

The operator must verify on the LIVE dashboard (after `make build` + `systemctl --user restart villa-dashboard.service` — the dashboard service serves embedded assets from the running binary):

1. **WEB-OFF:** with web search disabled, the Web Search panel is ABSENT (pixel-identical to before); `villa status --json` shows NO `web_search` key (only `schema_version: 5`).
2. **WEB-ON:** with web search enabled + stack restarted, the panel appears with the `outbound` row.
3. **NO-FALSE-GREEN:** with no recent `villa verify search` PASS, `outbound` shows GRAY "unavailable" + the caption — NOT green. After a real `villa verify search` PASS (on-hardware), it shows GREEN "bounded" + the `egress checked` timestamp. A stale result (>24h) shows gray again, never green.
4. **OMIT-WHEN-ABSENT:** no guard-counter rows, no "last query", no fetched-URL rows, no fabricated zeros.
5. **XSS:** (DevTools) panel values are text nodes, not parsed HTML.

NOTE: searxng/websafe health rows appear in the HEALTH panel (per the 34-03 contract), not the Web Search panel — confirm that placement matches intent during sign-off.

## Known Stubs

None.

## Threat Flags

None — the panel introduces no new endpoint, fetch, or probe (reads `report.web_search` off the existing `/api/status` poll). All threats are covered by the plan's register: T-34-16 (XSS → textContent-only), T-34-17 (no false-green → tri-state gray Unknown), T-34-18 (no fabricated rows → omit-when-absent), T-34-19 (off pixel-identity → ships hidden, re-hidden when field disappears).

## Self-Check: PASSED

- Modified files verified present: internal/dashboard/assets/dashboard.html, internal/dashboard/assets/dashboard.js, this SUMMARY.
- Commit verified in history: `5af36d2` (Task 1).

---
*Phase: 34-web-search-surfacing-lands-last*
*Completed: 2026-06-21 (implementation; human-verify checkpoint pending)*
