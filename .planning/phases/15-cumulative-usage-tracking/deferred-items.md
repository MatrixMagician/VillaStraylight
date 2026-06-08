# Phase 15 — Deferred Items (out-of-scope discoveries)

Logged during execution of plan 15-04. These are NOT fixed (out of scope per the
executor scope boundary: only auto-fix issues DIRECTLY caused by the current task).

- **`cmd/villa/bench_compare.go` is not gofmt-clean.** `go fmt ./cmd/villa/...` reformats
  this file. It is a Phase-14 artifact unrelated to Plan 15-04's files (dashboard usage
  writer + UI). Left untouched. Suggest a separate `style(chore)` commit or a
  `make fmt` sweep outside this phase.
