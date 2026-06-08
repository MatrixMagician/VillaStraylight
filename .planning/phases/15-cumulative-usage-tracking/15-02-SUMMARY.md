---
phase: 15-cumulative-usage-tracking
plan: 02
subsystem: internal/metrics
tags: [metrics, prometheus-scrape, typed-unknown, usage, counters]
requires:
  - "internal/metrics bounded /metrics scrape (ScrapeMetrics + parsePromText, Phase 5)"
provides:
  - "metrics.CounterSample (typed-Unknown cumulative-counter reading)"
  - "metrics.ScrapeCounters(endpoint) (CounterSample, bool) — counter feed for USAGE-01 fold"
  - "metric NAME consts mPromptTokensTotal / mPredictedTokensTotal (confined to internal/metrics)"
affects:
  - "internal/usage Fold (Plan 01) consumes CounterSample as its accumulation source (downstream)"
tech-stack:
  added: []
  patterns:
    - "typed-Unknown per-field Known bool (D-05) mirroring the existing ok=false gauge discipline"
    - "sibling accessor reusing the SAME bounded request shape (no second HTTP request, D-12)"
key-files:
  created: []
  modified:
    - internal/metrics/llamacpp.go
    - internal/metrics/llamacpp_test.go
    - internal/metrics/testdata/metrics.txt
decisions:
  - "Sibling CounterSample struct + ScrapeCounters accessor (not a widened PerfSnapshot) — counters are a different category from rate gauges (RESEARCH Open Question 2; Claude's discretion)"
  - "uint64 typed read via counterFromMap with negative/absence guard (Pitfall 3 belt-and-suspenders; lossless < 2^53)"
metrics:
  duration: ~2m
  completed: 2026-06-07
  tasks: 1
  files: 3
---

# Phase 15 Plan 02: _total counter feed (CounterSample) Summary

ScrapeCounters surfaces `llamacpp:prompt_tokens_total` and `llamacpp:tokens_predicted_total` as a typed-Unknown `CounterSample` from the existing bounded `/metrics` scrape — the cumulative-counter feed the Plan 01 usage fold accumulates from (USAGE-01, D-06).

## What was built

- **`metrics.CounterSample`** — sibling struct to `PerfSnapshot` carrying the two monotonic cumulative totals as `uint64`, each with its own `Known` typed-Unknown bool. Counters are deliberately a separate struct from the rate gauges (those are last-window snapshots; counters are monotonic lifetime totals).
- **`metrics.ScrapeCounters(endpoint string) (CounterSample, bool)`** — reuses the exact bounded request shape of `ScrapeMetrics`: `&http.Client{Timeout: scrapeTimeout}`, the same `endpoint + "/metrics"` GET, `io.LimitReader(resp.Body, maxScrapeBody)` (64 KiB), and `parsePromText`. No second HTTP request, no new endpoint/host literal (D-12, T-15-06/T-15-08). A transport error / non-200 → `(zero, false)` (whole scrape unavailable); on a 200 each counter's `Known` reflects its presence in the parsed map.
- **`counterFromMap`** helper — reads one counter via the `parsePromText` presence signal (`v, ok := m[name]`): `ok=false` (absent/unparseable) OR a negative value ⇒ `Known=false` with a zero total, never a fabricated 0 (D-05, T-15-07). float64→uint64 narrowing is lossless < 2^53 (Pitfall 3).
- **Name consts** `mPromptTokensTotal` / `mPredictedTokensTotal` — the ONLY new metric literals; confined to `internal/metrics` per the D-06 single-home discipline.
- **Fixture + tests** — two `# HELP`/`# TYPE … counter`/value triples appended to `testdata/metrics.txt` (values 130572 / 48913); `TestParsePromTextExtractsGauges` want-map extended; `TestScrapeCountersTotal` added covering present (both counters read as uint64 with Known=true), absent (Known=false, zero totals, no fabricated 0), and 404 (availability bool false) cases.

## Verification

- `go test ./internal/metrics` — 9 passed (targeted run: 6 passed for the three test groups).
- `go test ./...` — 653 passed across 19 packages (includes `TestSeamGrepGate`).
- `go vet ./internal/metrics/...` — no issues. `go fmt` — no changes.
- **D-06 grep gate** — `grep -rn 'llamacpp:prompt_tokens_total\|llamacpp:tokens_predicted_total' internal/usage internal/dashboard internal/status cmd/villa` returns nothing (literals confined to internal/metrics).
- **No-new-outbound (D-12)** — the counter read hits the SAME `/metrics` endpoint with the SAME `scrapeTimeout` + 64 KiB `LimitReader` bound; no new endpoint/host literal.

## Deviations from Plan

### Acceptance-criterion wording vs. sanctioned sibling pattern (non-substantive)

- **Found during:** Task 1 acceptance check.
- **Issue:** The final acceptance-criterion bullet asserts the diff "introduces no new `http.Client{`" and that `grep -c 'http.Client{'` is "unchanged from baseline". The chosen `ScrapeCounters` sibling necessarily constructs `&http.Client{Timeout: scrapeTimeout}`, raising the in-file count from 2 → 3.
- **Resolution:** This is permitted by the plan's own `<action>` and `<artifacts_this_phase_produces>`, which explicitly sanction "a sibling that mirrors its bounded shape" as Claude's discretion (RESEARCH recommends exactly this). The *threat-model intent* of that criterion — T-15-06 (no new/unbounded body read) and T-15-08/D-12 (no new outbound channel) — is fully satisfied: same `/metrics` endpoint, same 2s timeout, same 64 KiB `LimitReader`, no new host literal. The literal `http.Client{` count delta is an unavoidable, intended consequence of the sanctioned sibling accessor, not a new network channel. No code change made; substance of D-12 holds.
- **Files modified:** none (documentation only).

No other deviations — the plan executed as written.

## Known Stubs

None. Both counters are wired to the live bounded scrape; no placeholder/mock data.

## Threat Flags

None — no new security surface beyond the existing loopback `/metrics` scrape already covered by the plan's threat register (T-15-06/07/08). Same endpoint, same bound, same timeout.

## Self-Check: PASSED

- FOUND: internal/metrics/llamacpp.go (contains `CounterSample`, `llamacpp:prompt_tokens_total`, `llamacpp:tokens_predicted_total`)
- FOUND: internal/metrics/llamacpp_test.go (contains `func TestScrapeCountersTotal`)
- FOUND: internal/metrics/testdata/metrics.txt (both counter blocks, counter type)
- FOUND commit 977344b (test/RED), 3354233 (feat/GREEN)

## TDD Gate Compliance

- RED: `977344b test(15-02): add failing test …` — build failed (undefined ScrapeCounters), confirming RED before implementation.
- GREEN: `3354233 feat(15-02): surface _total counters …` — full metrics suite green after.
- REFACTOR: none needed (implementation mirrors existing ScrapeMetrics/parsePromText patterns; no cleanup commit).
