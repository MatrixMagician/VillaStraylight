---
phase: 15-cumulative-usage-tracking
verified: 2026-06-07T00:00:00Z
status: passed
score: 12/12 automated/code must-haves verified (4 SC; SC#1 live-reset, SC#2 runtime no-outbound, SC#4 visual render require on-hardware UAT)
overrides_applied: 0
human_verification:
  - test: "A1 monotonic-growth of _total counters during generation"
    expected: "During a generation, scraping the loopback /metrics endpoint twice shows BOTH llamacpp:prompt_tokens_total and llamacpp:tokens_predicted_total INCREASE (they do not reset per scrape). If a counter is absent, the fold degrades to typed-Unknown (non-fatal) — note it."
    why_human: "Requires a running llama-server on gfx1151 mid-generation; off-hardware tests use a fixture, not a live monotonic counter (SC#1, USAGE-01, RESEARCH A1)."
  - test: "Reset-aware end-to-end persistence across an llama-server restart"
    expected: "Note the cumulative total in `villa status`; restart `villa-llama` (the raw counter resets to 0); generate again; confirm `villa status` cumulative total CONTINUES from the prior value and does NOT drop to the new low raw count."
    why_human: "Requires a real container-restart counter reset against a live llama-server — the core fold is unit-proven (TestFoldResetAware) but the live reset-across-restart path is hardware-only (SC#1, USAGE-01, 15-VALIDATION.md Manual-Only)."
  - test: "No-new-outbound / strictly-local posture at runtime"
    expected: "With the dashboard running, confirm via `ss -tnp` / dashboard logs that NO new host/port appears — the scrape target is the SAME loopback /metrics endpoint already used for live tok/s. Confirm `villa status` still asserts no_telemetry."
    why_human: "Live socket observation cannot be done off-hardware; structurally the scrape reuses the existing bounded loopback endpoint and the no_telemetry assertion holds in tests, but the runtime no-new-socket confirmation is hardware-only (SC#2, USAGE-02/D-12)."
  - test: "Dashboard Performance panel renders cumulative-total rows with honest empty/unavailable states"
    expected: "After `make build` then (CLAUDE.md dashboard-restart trap) `systemctl --user restart villa-dashboard.service`: open the dashboard; before any generation the cumulative block shows the muted 'No usage recorded yet' copy; during/after generation it shows 'prompt tokens (total)' and 'generated tokens (total)' with thousands-grouped integer values, ALONGSIDE the unchanged live tok/s rows; a status-poll failure shows 'Cumulative usage unavailable'."
    why_human: "Visual rendering and the live /api/status-driven panel state can only be confirmed in a running browser against the long-lived dashboard service (SC#4, USAGE-02/D-10). Remember to restart villa-dashboard.service after make build — asset changes are not picked up otherwise."
---

# Phase 15: Cumulative Usage Tracking Verification Report

**Phase Goal:** villa accumulates cumulative prompt/generated token counts per model locally over time — reset-aware (surviving `llama-server` counter resets) and counts-only (no prompt/response content, no new outbound) — and surfaces those cumulative totals in `villa status` and the control dashboard alongside the existing live tok/s.
**Verified:** 2026-06-07
**Status:** passed
**Re-verification:** Yes — on-hardware UAT executed 2026-06-07 on gfx1151 (ROCm 7.2.4, qwen3.6-35b-a3b). All 4 human_verification items PASSED (see 15-UAT.md): (1) `_total` counters grew monotonically 17→41 / 167→231 across scrapes; (2) reset-aware persistence held — cumulative continued 41→61 / 231→263 after a `villa-llama` restart reset the raw counters to 0; (3) loopback-only sockets, `no_telemetry` intact; (4) dashboard Performance panel rendered "prompt tokens (total)" 61 / "generated tokens (total)" 263. 12/12 automated + 4/4 on-hardware = goal fully achieved.

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria — the binding contract)

| # | Truth (Success Criterion) | Status | Evidence |
| --- | --- | --- | --- |
| SC#1 | villa accumulates cumulative prompt + generated token counts per model and they persist across `llama-server` restarts / counter resets (reset-aware folding) | ✓ VERIFIED (core) / ? UNCERTAIN (live) | Pure reset-aware `foldCounter` (usage.go:100-111): `sampleRaw < LastSeenRaw ⇒ delta=sampleRaw` (whole-sample-new). `Fold` per-model keyed (usage.go:119-146). End-to-end through the handler proven by TestMetricsWritesUsage (2nd scrape with a LOWER raw → cumulative continues, not a drop). TestFoldResetAware/TestFoldPerModel/TestFoldTypedUnknown/TestStoreRoundTrip all PASS. **LIVE reset-across-restart on gfx1151 is human_verification (Plan 15-04 Task 3, not yet performed).** |
| SC#2 | Usage is counts-only (no prompt/response content) and tracking adds no new outbound (strictly-local posture asserted) | ✓ VERIFIED (structural) / ? UNCERTAIN (runtime socket) | TestUsageTotalsHasNoContentFields PASSES (reflect over UsageTotals/ModelUsage + JSON denylist). Counter NAME literals confined to internal/metrics (grep gate clean). `ScrapeCounters` reuses the same bounded shape (scrapeTimeout + maxScrapeBody LimitReader) over the SAME loopback `/metrics` endpoint — no new host/endpoint literal (llamacpp.go:182-211). no_telemetry assertion intact (status_test.go:156, golden:85). **Runtime no-new-socket confirmation is human_verification.** |
| SC#3 | `villa status` surfaces cumulative usage totals over time (live tok/s remains) | ✓ VERIFIED | `status.Report.Usage *usage.UsageTotals json:"usage,omitempty"` above SchemaVersion (status.go:138); `reportSchemaVersion = 2` (status.go:156); read-only `ReadUsage` seam (status.go:240) populated nil-guarded in Run (status.go:343-344); CLI wiring is read-only (sole-writer grep gate clean). GenTokensPerSec (live tok/s) untouched in golden. Golden re-frozen `"schema_version": 2` + `"usage"`/`"model"` keys (TestStatusJSONGolden PASS). TestUsageOmittedWhenAbsent/TestUsageSurfacedWhenPresent PASS. |
| SC#4 | The control dashboard surfaces cumulative usage totals | ✓ VERIFIED (code) / ? UNCERTAIN (visual) | `renderCumulativeUsage(report)` reads `report.usage` keyed by `report.model` from the existing /api/status poll — no new endpoint/fetch (dashboard.js:334-377). WR-01 FIX confirmed: `report.model` now resolves (status.go:129 serializes `model`), so the per-model lookup at dashboard.js:353 works exactly (multi-model false-empty defect resolved). Honest empty ("No usage recorded yet") / unavailable ("Cumulative usage unavailable") states present; counts-only integer rows (never tok/s). TestStatusUsageSurfaced PASS. **Visual render on the live dashboard is human_verification.** |

**Score:** 12/12 automated/code must-haves verified across the four plans; SC#1 (live reset), SC#2 (runtime no-outbound), SC#4 (visual) carry hardware-only residual items routed to human verification.

### Required Artifacts

| Artifact | Expected | Status | Details |
| --- | --- | --- | --- |
| `internal/usage/usage.go` | pure reset-aware Fold + counts-only types + XDG atomic store + traversal guard | ✓ VERIFIED | 288 lines; `Fold`/`foldCounter`/`Save`/`Load`/`UsagePath`/`assertInsideDir`/`WriteFileAtomic`; `os.Rename`+`os.CreateTemp` (atomic, no O_APPEND); WR-04 sampling-limitation documented (usage.go:89-99). |
| `internal/metrics/llamacpp.go` | CounterSample + ScrapeCounters surfacing the two _total counters typed-Unknown | ✓ VERIFIED | `CounterSample` struct; `ScrapeCounters` reuses bounded scrape; WR-02 FIX: `counterFromMap` rejects NaN/±Inf/negative/>2^53 (llamacpp.go:99-105). |
| `internal/status/status.go` | ONE omitempty usage field + Model field + reportSchemaVersion=2 + read-only ReadUsage seam | ✓ VERIFIED | Model (l.129) + Usage (l.138) above SchemaVersion (l.143); const=2 (l.156); `report.Model = cfg.Model` (l.327), nil-guarded `report.Usage = d.ReadUsage()` (l.343-344). |
| `cmd/villa/testdata/status.json.golden` | re-frozen golden, schema_version 2 + usage/model keys | ✓ VERIFIED | `"model": "qwen3"`, `"schema_version": 2`; existing keys (gen_tokens_per_sec, no_telemetry) unchanged. |
| `internal/dashboard/server.go` + `api.go` | usageMu sole-writer + fold+atomic-write, model captured in-section | ✓ VERIFIED | `usageMu sync.Mutex` (server.go:172); `foldUsage` under `usageMu.Lock()/defer Unlock()` with `model := s.modelID()` inside section (api.go:142-160); nil-default honest no-ops (server.go:249-250). WR-03 single-process precondition documented (server.go:163-168). |
| `cmd/villa/dashboard.go` | live WriteUsage (WriteFileAtomic) + ScrapeCounters + cfg.Model seams | ✓ VERIFIED | `liveUsageDeps`/`liveReadUsageTotals`/`liveWriteUsage` (usage.Save) / `liveModelID` (cfg.Model re-read) / `ScrapeCounters(endpoint)` over the same endpoint (dashboard.go:182-242). |
| `internal/dashboard/assets/dashboard.js` | cumulative rows from report.usage keyed by report.model, honest states | ✓ VERIFIED | `renderCumulativeUsage` reuses metricRow/mutedP, no `fetch(` for usage, exact UI-SPEC copy (dashboard.js:334-377). |

### Key Link Verification

| From | To | Via | Status | Details |
| --- | --- | --- | --- | --- |
| usage.Fold | foldCounter | per-model application to both counters | ✓ WIRED | usage.go:135-139 |
| usage store write | os.Rename | atomic temp+rename | ✓ WIRED | usage.go:256/277 (CreateTemp→Rename, no O_APPEND) |
| metrics CounterSample read | parsePromText map presence | ok=false ⇒ Known=false | ✓ WIRED | counterFromMap llamacpp.go:99-105 |
| counter read | existing bounded scrape | scrapeTimeout + maxScrapeBody LimitReader | ✓ WIRED | ScrapeCounters llamacpp.go:191-202 (same bound/timeout/endpoint as ScrapeMetrics) |
| status Report.Usage | d.ReadUsage() | nil-guarded Run population | ✓ WIRED | status.go:343-344 |
| status.go ReadUsage seam | usage.Load | read-only over UsagePath (never a write) | ✓ WIRED | cmd/villa read-only; sole-writer grep gate clean |
| dashboard handleMetrics | usage.Fold + WriteUsage | usageMu-guarded RMW keyed by in-section ModelID | ✓ WIRED | api.go:142-160 |
| dashboard.go WriteUsage | usage.WriteFileAtomic / usage.Save | atomic temp+rename over UsagePath | ✓ WIRED | dashboard.go:210/230 |
| dashboard.js cumulative rows | report.usage (/api/status poll) | render into #cumulative-usage, no new fetch | ✓ WIRED | dashboard.js:347-376; keyed by report.model (WR-01) |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Named usage-core tests | `go test ./internal/usage -run 'TestFold...'` | TestFoldResetAware/PerModel/TypedUnknown/UsageTotalsHasNoContentFields/StoreRoundTrip/UsagePathXDG all PASS | ✓ PASS |
| Counter feed | `go test ./internal/metrics -run TestScrapeCountersTotal` | PASS | ✓ PASS |
| Status surface | `go test ./internal/status -run TestUsage` | TestUsageOmittedWhenAbsent/SurfacedWhenPresent PASS | ✓ PASS |
| Dashboard sole-writer + same-field | `go test ./internal/dashboard -run 'TestMetricsWritesUsage\|TestStatusUsageSurfaced'` | both PASS | ✓ PASS |
| Frozen --json contract | `go test ./cmd/villa -run TestStatusJSONGolden` | PASS (schema_version 2) | ✓ PASS |
| Full gate | `rtk proxy make check` | vet + `go test ./...` all packages ok, exit 0 | ✓ PASS |
| Live monotonic growth / reset-across-restart / no-new-socket / visual render | requires gfx1151 host | not runnable off-hardware | ? SKIP → human verification |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| --- | --- | --- | --- | --- |
| USAGE-01 | 15-01, 15-02 | Reset-aware per-model cumulative token accumulation, counts-only, no new outbound | ✓ SATISFIED (automated) | Pure reset-aware Fold + XDG atomic store + counts-only structural test + _total counter feed; live reset-aware persistence is the on-hardware UAT residual (SC#1). |
| USAGE-02 | 15-03, 15-04 | `villa status` + dashboard surface cumulative usage as an append-only, schema-bumped contract change | ✓ SATISFIED (automated/code) | ONE append-only Report.Usage field + schema bump 1→2 + single golden re-freeze + dashboard sole-writer + UI render; visual render is the on-hardware UAT residual (SC#4). |

Both declared requirement IDs (USAGE-01, USAGE-02) appear in REQUIREMENTS.md (lines 33-34, 88-89, 107) mapped to Phase 15. No orphaned requirements: REQUIREMENTS.md maps exactly USAGE-01 + USAGE-02 to Phase 15, both claimed by the plans' `requirements:` frontmatter.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| --- | --- | --- | --- | --- |
| (none in phase-15 files) | — | TBD/FIXME/XXX/TODO/HACK/placeholder scan | — | Clean — no debt markers in any of the 8 modified files. |
| `cmd/villa/bench_compare.go` | — | not gofmt-clean | ℹ️ Info | A Phase-14 artifact, NOT a Phase-15 file (deferred-items.md). Does not affect Phase 15 goal; `make check` (vet+test) passes regardless. Out of scope. |

### Code-Review Warnings — Disposition

| Warning | Description | Status | Evidence |
| --- | --- | --- | --- |
| WR-01 | dashboard JS keyed on a non-existent `report.model` → multi-model false-empty | ✓ FIXED | commit 1cc1624; `Model` field on status.Report (status.go:129) + `report.Model=cfg.Model` (status.go:327); golden re-frozen with `"model"`; JS lookup now exact. |
| WR-02 | counterFromMap admitted NaN/+Inf → fabricated durable count (D-05 violation) | ✓ FIXED | commit 0297eac; `math.IsNaN/IsInf/>2^53` guards (llamacpp.go:101). |
| WR-03 | multi-process write race (sole-writer by convention) | ✓ ADDRESSED (documented limitation) | commit cfd7b7a; single-process precondition documented (server.go:163-168). Accepted: single-instance systemd service is the only writer. |
| WR-04 | reset-between-scrapes undercount | ✓ ADDRESSED (documented limitation) | commit cfd7b7a; sampling limitation documented honestly in foldCounter (usage.go:89-99) — biases DOWNWARD (safe direction). |

### Human Verification Required

The four Success Criteria are structurally/code-complete and all automated gates are green, but three live behaviors require the operator's gfx1151 on-hardware UAT (Plan 15-04 Task 3, a `gate="blocking"` checkpoint that was NOT performed off-hardware). See the `human_verification` frontmatter for the precise runbook:

1. **A1 monotonic-growth** — two /metrics scrapes mid-generation show both `_total` counters increase.
2. **Reset-aware persistence** — `villa status` cumulative total continues across a `villa-llama` restart (does not drop to the new low raw count).
3. **No-new-outbound** — `ss -tnp`/logs show no new host/port; `villa status` still asserts no_telemetry.
4. **Dashboard visual render** — Performance panel shows the cumulative rows with honest empty/unavailable states alongside unchanged live tok/s.

**Pre-step (CLAUDE.md dashboard-restart trap):** `make build` then `systemctl --user restart villa-dashboard.service` — the long-lived dashboard service will not pick up asset/code changes otherwise.

### Gaps Summary

No blockers. All four ROADMAP Success Criteria are implemented and proven to the limit of off-hardware verification: the pure reset-aware fold, per-model keying, typed-Unknown degradation, counts-only structural guarantee, atomic XDG store, the bounded same-endpoint counter feed, the append-only schema-bumped status surface, and the dashboard sole-writer + per-model UI render are all present, wired, and unit/golden-tested green (`make check` clean). All four code-review warnings (WR-01..WR-04) are fixed or documented with committed evidence.

The residual is intrinsic to the phase's own completion gate: SC#1's live reset-across-restart persistence, SC#2's runtime no-new-socket posture, and SC#4's visual rendering can only be proven against a running `llama-server` + dashboard on gfx1151. These are not deferrable to a later milestone phase (Phase 16/17 do not cover them) — they are the Plan 15-04 Task 3 on-hardware UAT. Status is therefore `human_needed`, not `passed`.

---

_Verified: 2026-06-07_
_Verifier: Claude (gsd-verifier)_
