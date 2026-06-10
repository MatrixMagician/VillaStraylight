# Phase 21 — Deferred Items (out-of-scope discoveries)

- **Pre-existing gofmt drift** in `cmd/villa/bench_compare.go` and
  `cmd/villa/verify_memory_test.go` (discovered during 21-02 Task 1 when running
  `go fmt ./cmd/villa/`). Not caused by this plan's changes — reverted the
  formatter's rewrite to keep the commits scoped. A future `make fmt` sweep (or
  quick task) should normalize them.
