---
phase: 34-web-search-surfacing-lands-last
plan: 03
subsystem: api
tags: [status, read-model, schema-bump, golden, verifystate, web-search, SURF-04, honesty-by-construction]
status: complete

# Dependency graph
requires:
  - phase: 34-01
    provides: "internal/verifystate (State{Verdict,CheckedAt}, fail-closed Load, VerifyStatePath) — the cached verify-search result the outbound-bounded indicator derives from"
provides:
  - "status.Report schema_version 4→5 with ONE append-only web_search tail-field"
  - "status.WebSearchInfo{enabled, outbound_bounded, verify_checked_at} sidecar"
  - "status.verifyFreshnessWindow = 24h (single named freshness const; --json + dashboard inherit it)"
  - "outbound_bounded tri-state ('bounded'/'not-bounded'/'unknown') derived from cached verifystate.State with a freshness gate — NEVER from cfg.WebSearchEnabled (T-34-08)"
  - "Outbound* token consts (OutboundBounded/OutboundNotBounded/OutboundUnknown)"
  - "status.Deps additions: SearxngService, WebsafeService, SearxngHealth, WebsafeHealth, ReadVerifyState"
  - "dedicated villa-searxng / villa-websafe service rows (own in-network health seams, OffloadApplies=false) — never the generic chat d.Health probe (T-34-09)"
  - "cmd/villa liveStatusDeps wiring: liveSearxngHealth (/healthz), liveWebsafeHealth (/load via mapWebsafeProbe), liveReadVerifyState (fail-closed)"
  - "re-frozen status*.json.golden (3 files) at schema 5"
affects: ["34-04 (doctor web-search findings)", "34-05 (dashboard Web Search panel binds report.web_search verbatim)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Append-only + schema-bump + single golden re-freeze (status 4→5, SchemaVersion stays last)"
    - "Typed-Unknown tri-state derived from a cached security proof with a freshness gate (never green by default, never from a config bool)"
    - "Dedicated in-network health seam per non-GPU managed service (qdrant/embed precedent) — never the generic chat probe"
    - "Service-with-no-health-route liveness probe: any HTTP response = up (mapWebsafeProbe)"

key-files:
  created: []
  modified:
    - internal/status/status.go
    - internal/status/status_test.go
    - cmd/villa/status.go
    - cmd/villa/testdata/status.json.golden
    - cmd/villa/testdata/status-memory.json.golden
    - cmd/villa/testdata/status-coding.json.golden
    - internal/dashboard/api_test.go

key-decisions:
  - "verifyFreshnessWindow = 24h (Open Q3 recommendation) — a single named const in the status core so --json and the dashboard inherit one freshness gate."
  - "outbound_bounded JSON keys: enabled (bool), outbound_bounded (tri-state string, NOT omitempty), verify_checked_at (RFC3339, omitempty)."
  - "villa-websafe has no /healthz route (only POST /load); its liveness probe (mapWebsafeProbe) treats ANY HTTP response as 'ready' and only a curl/podman-level failure as down/unknown — never the searxng 200-only mapping that would false-negative every healthy websafe."
  - "Source-gap fields (guard strip/flag counters, last_query_at, outbound-visibility last_query/last_fetched[]) are OMITTED — no host-side source exists; building one is out of scope (T-34-10)."

patterns-established:
  - "Outbound-bounded honesty: 'bounded' ONLY for a real RECENT verify PASS; a real recent non-PASS → 'not-bounded'; stale/absent/nil → 'unknown'."
  - "Web-search panel data contract (web_search block) frozen once at schema 5 for Plan 05 to bind verbatim."

requirements-completed: [SURF-04]

# Metrics
duration: 18min
completed: 2026-06-21
status: complete
---

# Phase 34 Plan 03: Status web_search contract (schema 4→5) Summary

**status.Report bumped 4→5 with ONE append-only web_search block whose outbound-bounded indicator is an honest tri-state derived from the cached `villa verify search` result with a 24h freshness gate — plus dedicated villa-searxng/villa-websafe health rows and a single golden re-freeze.**

## Performance

- **Duration:** 18 min
- **Started:** 2026-06-21T21:08Z
- **Completed:** 2026-06-21T21:26Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments
- status.Report at schema 5 with an append-only `web_search` tail-field (above SchemaVersion); web-search-OFF output differs from the v4 contract ONLY in schema_version.
- `outbound_bounded` tri-state derived from the cached `verifystate.State` with a `verifyFreshnessWindow = 24h` gate: PASS+fresh → "bounded", real-recent-non-PASS → "not-bounded", stale/absent/nil → "unknown". NEVER from `cfg.WebSearchEnabled` (T-34-08, with an explicit no-false-green test).
- Dedicated villa-searxng / villa-websafe service rows via their own in-network health seams (nil → HealthUnknown, OffloadApplies=false) — never the generic chat `d.Health()` probe (T-34-09).
- Live wiring (liveStatusDeps) + single intentional re-freeze of the three status*.json.golden (schema 4→5 only; no field reorder, no web_search block since all three fixtures are web-off).

## Task Commits

1. **Task 1: status.Report 4→5 — web_search tail-field, outbound-bounded tri-state, searxng/websafe rows (TDD)** - `04316a0` (feat)
2. **Task 2: live-wire status Deps + single golden re-freeze (3 status goldens)** - `8afe3c7` (feat)

**Plan metadata:** (this commit) (docs: complete plan)

## Web-Search JSON Contract (for Plan 05 to bind verbatim)

`report.web_search` (present ONLY when `cfg.WebSearchEnabled`):

| Key | Type | Notes |
|-----|------|-------|
| `enabled` | bool | always `true` (section built only when web search on) |
| `outbound_bounded` | string | tri-state: `"bounded"` / `"not-bounded"` / `"unknown"` — NOT omitempty (always surfaces) |
| `verify_checked_at` | string (RFC3339) | omitempty — carried ONLY for a non-stale cached result |

- **Freshness constant:** `verifyFreshnessWindow = 24 * time.Hour` (status core; `--json` and dashboard inherit it).
- **Tri-state mapping:** cached `verifystate.State` with `Verdict=="PASS"` AND `CheckedAt` within 24h → `"bounded"`; a real recent non-PASS verdict (FAIL/REJECT, within window) → `"not-bounded"`; nil seam / absent store / unparseable timestamp / stale (>24h) → `"unknown"`. Derived ONLY in `webSearchInfo`, never from the config bool.
- **searxng/websafe health** surface as `Services[]` rows (existing `ServiceStatus` shape), NOT as `web_search` sub-fields. Service unit names: `villa-searxng.service`, `villa-websafe.service`.
- **Token consts** exported for downstream readers: `status.OutboundBounded`, `status.OutboundNotBounded`, `status.OutboundUnknown`.

## Accepted Scope Limits (stated, not silent)

The following fields have NO host-side persisted source today and are OMITTED (no fabricated 0 / "never") — surfacing them would require a counter/query-log pipeline = NEW behavior = OUT OF SCOPE (SURF-04 surfacing phase):

- **Guard counters** (strip/flag/quarantine) — `metadata.guard` is per-request/in-container only; no host aggregate. "quarantine" is not a shipped guard verdict state.
- **`last_query_at`** — searches run inside OWUI→searxng→websafe; no host-readable last-query artifact.
- **Outbound-visibility** (`last_query` / `last_fetched[]`) — logging them conflicts with "ephemeral content excluded by design" (SURF-07).

## Files Created/Modified
- `internal/status/status.go` - WebSearchInfo struct + `web_search` tail-field above SchemaVersion; reportSchemaVersion 4→5; verifyFreshnessWindow const; Outbound* token consts; Deps additions (Searxng/WebsafeService, Searxng/WebsafeHealth, ReadVerifyState); webSearchInfo helper; searxng/websafe dedicated row branches.
- `internal/status/status_test.go` - TestRunWebSearch, TestWebSearchOutboundBounded (incl. no-false-green + verify_checked_at freshness), TestRunSearxngWebsafeRows; updated memory/coding/usage schema asserts 4→5.
- `cmd/villa/status.go` - liveStatusDeps wiring for the 5 new seams; liveSearxngHealth/liveWebsafeHealth (TTL-cached pair, /healthz + /load) + probeWebsafeURL/mapWebsafeProbe; liveReadVerifyState (fail-closed over verifystate.Load).
- `cmd/villa/testdata/status*.json.golden` (3) - single re-freeze: schema_version 4→5 ONLY.
- `internal/dashboard/api_test.go` - schema passthrough assertion 4→5 (downstream contract bump).

## Decisions Made
See key-decisions frontmatter. Headline: 24h freshness window; websafe liveness probe treats any HTTP response as up (no health route); source-gap fields omitted.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Updated downstream schema_version assertions 4→5**
- **Found during:** Task 1 (status core) and Task 2 (dashboard passthrough)
- **Issue:** The intentional status schema bump 4→5 broke pre-existing tests that asserted the v4 contract: `internal/status` (TestUsageSurfacedWhenPresent, TestRunMemoryOffReport, TestRunCodingOffReport) and `internal/dashboard` (TestHandleStatusMemoryPassthrough, which serializes the same Report through the passthrough handler).
- **Fix:** Updated each assertion (and its doc comment where it cited the version) from 4 to 5. No logic changed — these are contract-version asserts directly tracking the planned bump.
- **Files modified:** internal/status/status_test.go, internal/dashboard/api_test.go
- **Verification:** `go test ./internal/status/ ./internal/dashboard/` green; `make check` green.
- **Committed in:** `04316a0` (status asserts, Task 1) and `8afe3c7` (dashboard assert, Task 2)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** The downstream assertion updates are a mechanical consequence of the planned 4→5 contract bump (the whole point of the plan). No scope creep; no new behavior.

## Issues Encountered
- The web-search-ON test fixture initially reused `loopbackUnits` (web-off render), so the searxng/websafe service rows never appeared in the report. Resolved by rendering the fixture units with the web-search-ON config (`orchestrate.Render` with `cfg.WebSearchEnabled=true`), matching the memory-on fixture pattern.

## Verification

- `go test ./internal/status/ -run 'TestRunWebSearch|TestWebSearchOutboundBounded|TestRunSearxngWebsafeRows'` — PASS
- `go test ./internal/status/` (full package) — PASS
- `go test ./cmd/villa/ -run 'TestStatusJSONGolden|TestStatus'` — PASS (against re-frozen goldens)
- `go test ./internal/inference/ -run TestSeamGrepGate` — PASS (no leaked backend/image literal in the new wiring; unit names via orchestrate accessors)
- `make check` (vet + full test) — green across all packages.

### Acceptance gates
- `grep -v '^//' ... | grep -c 'reportSchemaVersion = 5'` → 1; no non-comment `= 4`. ✓
- `web_search,omitempty` present and the WebSearch field is ABOVE schema_version. ✓
- TestWebSearchOutboundBounded includes the case proving `cfg.WebSearchEnabled=true` with NO cached PASS yields `outbound_bounded != "bounded"`. ✓
- `git diff cmd/villa/testdata/status*.json.golden` = schema_version 4→5 ONLY (no reorder, no web_search block — fixtures are web-off). ✓
- `grep -n 'SearXNGContainerUnitName\|WebsafeContainerUnitName' cmd/villa/status.go` matches (no typed unit literal). ✓

## Known Stubs
None.

## Threat Flags
None — the new surface (a read-only status field + two in-network health probes) is fully covered by the plan's threat register (T-34-08..11). No new endpoint, auth path, or trust boundary was introduced; the outbound-bounded indicator is honest-by-construction (re-proven, never trusted from a stale cache, never derived from a config bool).

## Next Phase Readiness
- Plan 04 (doctor web-search findings) can fold the same searxng/websafe rows and the cached verify result.
- Plan 05 (dashboard Web Search panel) binds `report.web_search` verbatim — the contract (keys, tri-state, freshness) is frozen at schema 5.

## Self-Check: PASSED

- Modified files verified present: internal/status/status.go, internal/status/status_test.go, cmd/villa/status.go, the 3 status*.json.golden, internal/dashboard/api_test.go, this SUMMARY.
- Commits verified in history: `04316a0` (Task 1), `8afe3c7` (Task 2).

---
*Phase: 34-web-search-surfacing-lands-last*
*Completed: 2026-06-21*
