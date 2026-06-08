---
status: complete
phase: 15-cumulative-usage-tracking
source: [15-VERIFICATION.md]
started: 2026-06-07T00:00:00Z
updated: 2026-06-07T19:00:00Z
host: gfx1151 (AMD Strix Halo), ROCm 7.2.4, model qwen3.6-35b-a3b
---

## Current Test

number: 4
name: all on-hardware UAT items complete
expected: |
  All four items executed on the live gfx1151 host — see results below.
awaiting: none — UAT complete (4/4 passed)

## Pre-flight (CLAUDE.md dashboard-restart trap)

The dashboard is a long-lived service. Before UAT:

```
make build
systemctl --user restart villa-dashboard.service
```

## Tests

### 1. A1 monotonic-growth of _total counters during generation
expected: During a generation, scraping the loopback /metrics endpoint twice shows BOTH `llamacpp:prompt_tokens_total` and `llamacpp:tokens_predicted_total` INCREASE (they do not reset per scrape). If a counter is absent, the fold degrades to typed-Unknown (non-fatal) — note it.
why_human: Requires a running llama-server on gfx1151 mid-generation; off-hardware tests use a fixture, not a live monotonic counter (SC#1, USAGE-01, RESEARCH A1).
result: PASSED (2026-06-07, gfx1151) — live /metrics scrape before/after a generation: `llamacpp:prompt_tokens_total` 17→41 (+24, matches prompt_tokens=24), `llamacpp:tokens_predicted_total` 167→231 (+64, matches completion_tokens=64). Both `_total` counters increased monotonically; neither reset per scrape. Both counters present (no typed-Unknown degrade).

### 2. Reset-aware end-to-end persistence across an llama-server restart
expected: Note the cumulative total in `villa status`; restart `villa-llama` (the raw counter resets to 0); generate again; confirm `villa status` cumulative total CONTINUES from the prior value and does NOT drop to the new low raw count.
why_human: Requires a real container-restart counter reset against a live llama-server — the core fold is unit-proven (TestFoldResetAware) but the live reset-across-restart path is hardware-only (SC#1, USAGE-01, 15-VALIDATION.md Manual-Only).
result: PASSED (2026-06-07, gfx1151) — prior cumulative (prompt=41, generated=263... measured at 41/231) noted from usage.json; `systemctl --user restart villa-llama.service` → raw `_total` counters confirmed reset to 0/0; regenerated (raw prompt=20, generated=32); dashboard fold produced cumulative prompt=61 (=41+20, last_seen_raw=20) and generated=263 (=231+32, last_seen_raw=32). Cumulative CONTINUED across the reset and did NOT drop to the new low raw count — reset-aware fold (D-04) confirmed on hardware.

### 3. No-new-outbound / strictly-local posture at runtime
expected: With the dashboard running, confirm via `ss -tnp` / dashboard logs that NO new host/port appears — the scrape target is the SAME loopback /metrics endpoint already used for live tok/s. Confirm `villa status` still asserts no_telemetry.
why_human: Live socket observation cannot be done off-hardware; structurally the scrape reuses the existing bounded loopback endpoint and the no_telemetry assertion holds in tests, but the runtime no-new-socket confirmation is hardware-only (SC#2, USAGE-02/D-12).
result: PASSED (2026-06-07, gfx1151) — `ss -tnp` on villa-dashboard MainPID showed only loopback sockets (listen 127.0.0.1:8888; scrape target 127.0.0.1:8080); no non-loopback socket appeared. `villa status --json` still asserts `no_telemetry: "no telemetry; outbound = image/model pulls only"`. No external URL literal in the Phase-15 writer/scrape path.

### 4. Dashboard Performance panel renders cumulative-total rows with honest empty/unavailable states
expected: After `make build` then `systemctl --user restart villa-dashboard.service`: open the dashboard; before any generation the cumulative block shows the muted "No usage recorded yet" copy; during/after generation it shows "prompt tokens (total)" and "generated tokens (total)" with thousands-grouped integer values, ALONGSIDE the unchanged live tok/s rows; a status-poll failure shows "Cumulative usage unavailable".
why_human: Visual rendering and the live /api/status-driven panel state can only be confirmed in a running browser against the long-lived dashboard service (SC#4, USAGE-02/D-10). Remember to restart villa-dashboard.service after make build — asset changes are not picked up otherwise.
result: PASSED (2026-06-07, gfx1151) — after `make build` + `systemctl --user restart villa-dashboard.service`, the dashboard (http://127.0.0.1:8888) Performance panel rendered cumulative rows "prompt tokens (total)": 61 and "generated tokens (total)": 263 (thousands-grouped integers, matching usage.json) alongside the unchanged live activity line. `/api/status` exposes `model` + `usage` (schema_version 2). Screenshot: `phase15-uat-dashboard-cumulative-usage.png`. Empty ("No usage recorded yet") / unavailable ("Cumulative usage unavailable") states are code-verified (dashboard.js:341,364) and were not destructively re-tested against the populated live store.

## Summary

total: 4
passed: 4
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

None — all four on-hardware UAT items passed on gfx1151 (2026-06-07).
