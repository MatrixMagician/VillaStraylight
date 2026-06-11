# Phase 23 — Deferred Items

Out-of-scope discoveries logged during execution (not fixed, per scope boundary).

## From Plan 23-01 (2026-06-10)

- **Pre-existing gofmt violations** in `cmd/villa/bench_compare.go` and
  `cmd/villa/verify_memory_test.go` (`gofmt -l` flags both on the pre-plan tree).
  Neither file is touched by Phase 23 plans so far. Fix opportunistically in a
  plan that touches them, or via a standalone `gofmt -w` chore commit.
