# Phase 12 Deferred Items

- [gofmt drift, out of scope] `internal/inference/validate.go` and `validate_test.go` have pre-existing gofmt formatting drift (doc-comment bullet + one-line method bodies) unrelated to Phase 12. `go fmt ./internal/inference/` would reformat them; reverted to keep the 12-01 commit scoped to plan files. Discovered during 12-01 Task 2. Fix with a standalone `make fmt` cleanup commit.
