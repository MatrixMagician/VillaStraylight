---
phase: 15
slug: cumulative-usage-tracking
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-07
---

# Phase 15 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` (table-driven + `httptest` + golden fixtures) — no third-party assert lib |
| **Config file** | none — `go test` convention |
| **Quick run command** | `go test ./internal/usage ./internal/metrics ./internal/status` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~20 seconds (quick); ~60s full |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/usage ./internal/metrics ./internal/status`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** `make check` green + the single golden diff reviewed
- **Max feedback latency:** ~20 seconds

---

## Per-Task Verification Map

| Req ID | Behavior | Test Type | Automated Command | File Exists |
|--------|----------|-----------|-------------------|-------------|
| USAGE-01 | Reset-aware fold: monotonic delta + backward-step ⇒ whole-sample-as-new (D-04) | unit | `go test ./internal/usage -run TestFoldResetAware` | ❌ W0 |
| USAGE-01 | Per-model keying: two models accumulate independently (D-03) | unit | `go test ./internal/usage -run TestFoldPerModel` | ❌ W0 |
| USAGE-01 | Counter absent ⇒ no fold, no write (typed-Unknown, D-05) | unit | `go test ./internal/usage -run TestFoldTypedUnknown` | ❌ W0 |
| USAGE-01 | Metrics extension surfaces both `_total` counters from fixture (D-06) | unit | `go test ./internal/metrics -run TestScrapeCountersTotal` | ⚠️ extend |
| USAGE-01 | Persist round-trip: atomic write then read returns identical totals (D-02) | unit | `go test ./internal/usage -run TestStoreRoundTrip` | ❌ W0 |
| USAGE-01 | XDG path resolver honors `$XDG_DATA_HOME` + traversal guard (D-02) | unit | `go test ./internal/usage -run TestUsagePathXDG` | ❌ W0 |
| USAGE-02 | `status.Report` carries `usage` field; absent store ⇒ omitted (omitempty, D-09) | unit | `go test ./internal/status -run TestUsageOmittedWhenAbsent` | ❌ W0 |
| USAGE-02 | Byte-frozen `--json` golden: only `usage` + `schema_version 1→2` changed (D-09) | golden | `go test ./cmd/villa -run TestStatusJSONGolden` | ✅ re-freeze once |
| USAGE-02 | Dashboard reads SAME `Report` field, no new endpoint (D-10) | unit | `go test ./internal/dashboard -run TestStatusUsageSurfaced` | ❌ W0 |
| USAGE-02 (D-07) | Dashboard `/api/metrics` folds+writes under mutex (sole writer) | unit | `go test ./internal/dashboard -run TestMetricsWritesUsage` | ❌ W0 |
| USAGE-02 (D-11) | Counts-only: `UsageTotals`/`ModelUsage` have NO content fields | security | `go test ./internal/usage -run TestUsageTotalsHasNoContentFields` | ❌ W0 |
| USAGE-02 (D-12) | No-new-outbound: usage derives only from existing scrape; `no_telemetry` intact | structural | `go test ./internal/status -run TestNoTelemetry` + grep gate | ⚠️ extend |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/usage/usage.go` + `internal/usage/usage_test.go` — net-new pure core + tests (USAGE-01, D-01/D-03/D-04/D-05/D-11)
- [ ] `internal/metrics/llamacpp_test.go` + `testdata/metrics.txt` — add the two `_total` token counters (fixture already has `n_decode_total`)
- [ ] `internal/dashboard/*_test.go` — writer-hook + sole-writer mutex test (D-07)
- [ ] `internal/status/status_test.go` — usage-omitted-when-absent + surfaced-when-present
- [ ] Re-freeze `cmd/villa/testdata/status.json.golden` ONCE (`go test ./cmd/villa -run TestStatusJSONGolden -update`), review diff
- [ ] No framework install needed (stdlib `testing` already in use)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| A1 monotonic-growth of `_total` counters | USAGE-01 | needs a live `llama-server` mid-generation | During a generation, scrape `/metrics` twice; assert both `llamacpp:prompt_tokens_total` and `llamacpp:tokens_predicted_total` increase |
| Reset-aware end-to-end persistence | USAGE-01 | needs a real counter reset (container restart) | Note cumulative total in `villa status`; restart `villa-llama` (counter resets to 0); generate again; assert `villa status` cumulative **continues from prior value**, does not drop to the new low raw count |
| No-new-outbound | USAGE-02, D-12 | needs live socket observation | Confirm no new host/port appears (`ss -tnp` / dashboard logs) — scrape target is the SAME loopback endpoint already used for live tok/s |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 20s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
