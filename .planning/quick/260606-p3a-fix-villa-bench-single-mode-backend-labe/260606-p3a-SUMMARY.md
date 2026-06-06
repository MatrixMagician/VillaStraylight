---
phase: quick-260606-p3a
plan: 01
subsystem: cmd/villa bench
tags: [bench, honest-ab, cmd-layer, backend-label, BENCH-01, BENCH-02]
dependency_graph:
  requires:
    - "internal/bench (pure honest core, Result.Backend field — 09-02)"
    - "cmd/villa/bench.go runBench + benchEntryFromResult/sideFromStats/renderBench (09-03)"
  provides:
    - "single-mode bench output that names the measured backend (human header + --json single.backend)"
    - "benchConfiguredBackend package seam (testable single-mode backend source)"
    - "TestBenchJSONNoBlendedKey invariant guard (pp/tg-separate, no blended key)"
  affects:
    - "Phase 10 surfacing (the --json single.backend field is now populated for the dashboard read contract)"
tech_stack:
  added: []
  patterns:
    - "package-level seam indirection (benchConfiguredBackend) mirroring benchEndpointReachable — cmd-layer config source overridable in tests, no live config in CI"
key_files:
  created: []
  modified:
    - "cmd/villa/bench.go"
    - "cmd/villa/bench_test.go"
decisions:
  - "Single-mode backend label is set in the cmd layer (runBench sets res.Backend from a configured-backend seam in the !ab path) — the pure internal/bench core stays config-unaware (LOCKED). No config plumbed into internal/bench."
  - "The seam (benchConfiguredBackend) mirrors the existing benchEndpointReachable indirection so runBench stays drivable with a stubbed bench.Deps and no live config; default reads config.LoadVilla().Backend, returns \"\" on load error (never panics, never fabricates a name)."
  - "The --ab branch is guarded with `if !ab` and left untouched — it labels sides from res.AB.From/To, never res.Backend; the byte-frozen --ab golden matches without -update."
metrics:
  duration: 2 min
  completed: 2026-06-06
---

# Phase quick-260606-p3a Plan 01: Fix villa bench single-mode backend label Summary

Single-mode `villa bench` now names the measured backend in both the human header
(`backend (vulkan):` instead of `backend ():`) and the `--json` `single.backend` field,
via a one-line cmd-layer wiring (`runBench` sets `res.Backend` from a testable
configured-backend seam in the non-`--ab` path) — the pure `internal/bench` core stays
config-unaware and the byte-frozen `--ab` contract is unchanged.

## What Was Built

**Task 1 (TDD) — thread the configured backend into the single-mode result:**
- Added a package-level `benchConfiguredBackend` seam in `cmd/villa/bench.go`, mirroring the
  existing `benchEndpointReachable` indirection. Default returns `config.LoadVilla().Backend`,
  `""` on a load error — never panics, never fabricates a backend name.
- In `runBench`, after the `res.Err != nil` block and before `benchEntryFromResult`, set
  `res.Backend = benchConfiguredBackend()` guarded by `if !ab` (single path only). The `--ab`
  branch reads `res.AB.From/To` and is untouched.
- Added the `withConfiguredBackend(t, ...)` test helper (save/restore via `t.Cleanup`,
  mirroring `withReachable`) and `TestBenchSingleNamesBackend`, which runs `runBench` in
  single mode twice: human mode asserts the header contains `backend (vulkan):` and NOT the
  empty `backend ():` form; `--json` mode decodes `single.backend` and asserts it equals
  `"vulkan"`.
- RED → GREEN verified: the test failed (header `backend ():`, `single.backend == ""`) before
  the `runBench` fix and passed after it. No refactor needed (the fix is a single line).

**Task 2 — guard the no-blended-key invariant:**
- Added `TestBenchJSONNoBlendedKey`, which reads `testdata/bench.json.golden` and asserts ZERO
  occurrences of `tok_per_sec` / `tokens_per_sec`. Locks the milestone honesty invariant
  (pp/tg stay SEPARATE) against future drift. The `--ab` golden matched byte-for-byte without
  `-update` (the fix did not touch the `--ab` branch).

## Verification Results

- `go build ./...` — Success.
- `go test ./internal/bench/ ./cmd/villa/ -count=1` — 171 passed (incl. the new
  `TestBenchSingleNamesBackend` + `TestBenchJSONNoBlendedKey`; existing `TestBenchJSON` golden
  and `TestBenchABRestoresOriginal` still pass).
- `go vet ./cmd/villa/ ./internal/bench/` — no issues.
- `grep -c 'tok_per_sec\|tokens_per_sec' cmd/villa/testdata/bench.json.golden` → `0`
  (`NO_BLENDED_KEY_OK`).
- `internal/bench/bench.go` unmodified (pure core stays config-unaware; confirmed via
  `git diff --name-only` — no `internal/bench/` change).
- `--ab` golden unchanged; pp/tg-SEPARATE contract preserved.

## Deviations from Plan

None - plan executed exactly as written.

## Commits

- `a210a7e` test(quick-260606-p3a): add failing test for single-mode backend label (RED)
- `8aa9c90` fix(quick-260606-p3a): name the measured backend in single-mode bench output (GREEN)
- `cb25a32` test(quick-260606-p3a): guard --json against blended tok/s keys

## TDD Gate Compliance

- RED: `a210a7e` `test(...)` — failing test for the single-mode label.
- GREEN: `8aa9c90` `feat/fix(...)` — minimal `runBench` wiring; test passes.
- REFACTOR: none required (one-line wiring, nothing to clean up).

## Self-Check: PASSED

- FOUND: `.planning/quick/260606-p3a-fix-villa-bench-single-mode-backend-labe/260606-p3a-SUMMARY.md`
- FOUND: commit `a210a7e` (RED test)
- FOUND: commit `8aa9c90` (GREEN fix)
- FOUND: commit `cb25a32` (blended-key guard)
- FOUND: `res.Backend = benchConfiguredBackend()` wiring in `cmd/villa/bench.go`
