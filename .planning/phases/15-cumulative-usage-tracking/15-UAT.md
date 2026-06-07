---
status: testing
phase: 15-cumulative-usage-tracking
source: [15-VERIFICATION.md]
started: 2026-06-07T00:00:00Z
updated: 2026-06-07T00:00:00Z
---

## Current Test

number: 1
name: A1 monotonic-growth of _total counters during generation
expected: |
  During a generation, scraping the loopback /metrics endpoint twice shows BOTH
  llamacpp:prompt_tokens_total and llamacpp:tokens_predicted_total INCREASE (they do
  not reset per scrape). If a counter is absent, the fold degrades to typed-Unknown
  (non-fatal) — note it.
awaiting: user response

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
result: [pending]

### 2. Reset-aware end-to-end persistence across an llama-server restart
expected: Note the cumulative total in `villa status`; restart `villa-llama` (the raw counter resets to 0); generate again; confirm `villa status` cumulative total CONTINUES from the prior value and does NOT drop to the new low raw count.
why_human: Requires a real container-restart counter reset against a live llama-server — the core fold is unit-proven (TestFoldResetAware) but the live reset-across-restart path is hardware-only (SC#1, USAGE-01, 15-VALIDATION.md Manual-Only).
result: [pending]

### 3. No-new-outbound / strictly-local posture at runtime
expected: With the dashboard running, confirm via `ss -tnp` / dashboard logs that NO new host/port appears — the scrape target is the SAME loopback /metrics endpoint already used for live tok/s. Confirm `villa status` still asserts no_telemetry.
why_human: Live socket observation cannot be done off-hardware; structurally the scrape reuses the existing bounded loopback endpoint and the no_telemetry assertion holds in tests, but the runtime no-new-socket confirmation is hardware-only (SC#2, USAGE-02/D-12).
result: [pending]

### 4. Dashboard Performance panel renders cumulative-total rows with honest empty/unavailable states
expected: After `make build` then `systemctl --user restart villa-dashboard.service`: open the dashboard; before any generation the cumulative block shows the muted "No usage recorded yet" copy; during/after generation it shows "prompt tokens (total)" and "generated tokens (total)" with thousands-grouped integer values, ALONGSIDE the unchanged live tok/s rows; a status-poll failure shows "Cumulative usage unavailable".
why_human: Visual rendering and the live /api/status-driven panel state can only be confirmed in a running browser against the long-lived dashboard service (SC#4, USAGE-02/D-10). Remember to restart villa-dashboard.service after make build — asset changes are not picked up otherwise.
result: [pending]

## Summary

total: 4
passed: 0
issues: 0
pending: 4
skipped: 0
blocked: 0

## Gaps
