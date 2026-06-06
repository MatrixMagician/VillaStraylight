---
phase: 09-villa-bench-honest-a-b
plan: 03
subsystem: cmd/villa (bench noun)
tags: [bench, cobra, residency-gate, ab-delta, json-contract, seam-clean]
requires:
  - "09-01: llm.Complete (per-request pp/tg Timings)"
  - "09-02: internal/bench pure core (Deps/BenchSpec/Result/Run/Stats)"
  - "08-02: cmd/villa liveProve + liveBackendSwapDeps (cloned for liveMeasure; --ab delegate)"
provides:
  - "cmd/villa: `villa bench` / `villa bench --ab` cobra noun"
  - "cmd/villa: liveMeasure (liveProve clone, llm.Complete measurement + residency gate)"
  - "cmd/villa: liveBenchDeps (Measure + --ab Switch/Restore -> backendswap.Run, LOCKED)"
  - "cmd/villa: benchEntry --json contract (separate pp/tg per side + per-metric delta)"
  - "cmd/villa/testdata/bench.json.golden (frozen Phase-10 read contract)"
affects:
  - "Phase 10 (surfacing): reads tok/s from the frozen bench.json.golden shape"
tech-stack:
  added: []
  patterns:
    - "liveProve-clone residency gate per counted bench run (offload asserted, not assumed)"
    - "--ab flip composes backendswap.Run via injected Switch/Restore (LOCKED, never re-implemented)"
    - "spec captured in the live Measure closure (pure core threads its own context.Background())"
    - "reachability pre-check as a package-level indirection var (no field on the LOCKED bench.Deps)"
    - "frozen golden --json with separate pp/tg keys + -update regen (status.json.golden discipline)"
key-files:
  created:
    - cmd/villa/bench.go
    - cmd/villa/bench_test.go
    - cmd/villa/testdata/bench.json.golden
  modified:
    - cmd/villa/root.go
decisions:
  - "Spec rides the live Measure closure, not the context: the LOCKED bench core threads its own context.Background() through Measure, so liveBenchDeps(ab, spec) captures the run's flags in the closure rather than mutating the core to accept a spec-bearing context."
  - "Reachability pre-check is a package-level `var benchEndpointReachable = func() bool` indirection (overridable in tests), NOT a new field on bench.Deps — the Plan-02 core is LOCKED and a no-endpoint refusal is a cmd-tier concern, not a methodology seam."
  - "MinResident = ceil(reps/2) (clear majority) below which the band is an honest void-exhaustion WARN; fixed seed=42, temp=0 (greedy) for reproducibility (BENCH-02)."
  - "--json shape: separate prompt_per_sec/predicted_per_sec (+ *_stddev) per side and delta_prompt_per_sec/delta_predicted_per_sec — never a blended tok/s key (Phase-10 read contract)."
metrics:
  duration: 4 min
  completed: 2026-06-06
---

# Phase 9 Plan 3: villa bench (Honest A/B) Summary

`villa bench` wires the Plan-01 `llm.Complete` per-request timings and the Plan-02 pure `internal/bench` core into a residency-gated cobra noun: plain `villa bench` measures ONLY the running backend non-disruptively (separate pp/tg tok/s, median ± stddev), `--ab` flips via `backendswap.Run` and restores the original for a per-metric delta, and a frozen `--json` golden carries the separate pp/tg keys Phase 10 reads.

## What was built

- **`cmd/villa/bench.go`** — the `villa bench` noun and its wiring:
  - `liveMeasure(ctx, target, spec)` — a clone of `backend.go:liveProve`'s residency-gate composition, swapping the boolean `GenerationProbe` for the new `llm.Complete` (which returns the server-computed pp/tg `Timings`). It resolves the backend fail-closed via `inference.BackendFor`, runs the completion in a goroutine while the `100ms`-ticker `detect.GPUBusyPercent()` sampler keeps `maxBusy` DURING the decode, bounds the run with `context.WithTimeout(ctx, spec.Timeout)` (the load_tensors-hang guard), and folds `inference.RunningOffloadVerdict` over the target backend's `ResidencyProof()` markers. A run is `resident` ONLY for `inference.StatusPass`; anything else (incl. a fast CPU-fallback completion) marks it VOID.
  - `liveBenchDeps(ab, spec)` — wires `Measure` (spec captured in the closure); for `--ab` ONLY, `LoadConfig`/`Switch`/`Restore` delegate to `backendswap.Run` via `runBackendSwap` (the SAME `liveBackendSwapDeps` wiring `villa backend set` uses — LOCKED, never re-implemented).
  - `newBench()` cobra noun with `--ab`, `-n`/`--reps` (5), `--warmup` (1), `--n-predict` (128), `--json`; fixed `seed=42`, `temp=0`.
  - `runBench(...) int` — reachability pre-check (no endpoint → `exitBlocked` with `villa up` remediation) → `bench.Run` → exit map (`Err` → `exitBlocked`, `VoidExhausted` → `exitWarn`, clean → `exitPass`); renders a human table (separate pp/tg rows + Kept/Void + stated conditions; Δpp/Δtg for `--ab`) or the typed `benchEntry` `--json`.
- **`cmd/villa/root.go`** — `newBench()` registered on `newRoot().AddCommand(...)` alongside `newBackend()`.
- **`cmd/villa/bench_test.go`** — stubbed `bench.Deps` builder + `benchTestCmd()` helper + `withReachable` override of the reachability indirection: `TestBenchNoEndpoint`/`TestBenchVoidExhaustion`/`TestBenchCleanPass` (exit mapping), `TestBenchABRestoresOriginal` (--ab Switches then ends restoring the original), `TestBenchJSON` (frozen golden), `TestBenchRegistered`.
- **`cmd/villa/testdata/bench.json.golden`** — the frozen `--ab` `--json` contract: separate `prompt_per_sec`/`predicted_per_sec` (+ stddevs) per side + `delta_prompt_per_sec`/`delta_predicted_per_sec`; no blended key.

## How it was verified

- `go build ./...` succeeds; `villa bench --help` lists `--ab`, `-n`, `--warmup`, `--n-predict`, `--json`.
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` green — `cmd/villa/bench.go` is literal-free of backend markers (markers arrive only via `inference.BackendFor(target).ResidencyProof()`).
- `go test ./cmd/villa/ -run TestBench -count=1` green (6 tests): exit mapping, --ab original-restore, frozen --json golden.
- `go test ./... -count=1` full suite green (522 tests); `go vet ./...` clean; `go.mod`/`go.sum` unchanged.
- Golden key assertion: 3× `prompt_per_sec"`, 3× `predicted_per_sec"` (A/B/delta), 0× `tok_per_sec`.

## Deviations from Plan

None — plan executed exactly as written. Two implementation choices the plan left to the executor (recorded as decisions above): (1) the run spec rides the live `Measure` closure rather than the core's `context` because `internal/bench` (LOCKED) threads its own `context.Background()`; (2) the no-endpoint reachability pre-check is a package-level indirection `var` rather than a new `bench.Deps` field, since the Plan-02 core is LOCKED and the refusal is a cmd-tier concern. Neither modifies any Wave-1 file.

## Known Stubs

None. The single-backend and `--ab` paths are fully wired to `llm.Complete` + `inference.RunningOffloadVerdict` + `backendswap.Run`; the live `liveMeasure`/`liveBenchDeps` paths are exercised on-hardware (UAT, below). No placeholder data flows to render or `--json`.

## On-hardware UAT (manual, not a plan blocker)

Real `villa bench` + `villa bench --ab` on gfx1151: confirm pp/tg reported separately and residency-checked; confirm `/v1` `timings` present on the running server (fall back to `/completion` if absent, A1); confirm the ROCm `--ab` side reaches GPU residency (SELinux `/dev/kfd`); confirm the original backend is restored after `--ab`.

## Self-Check: PASSED

- FOUND: cmd/villa/bench.go
- FOUND: cmd/villa/bench_test.go
- FOUND: cmd/villa/testdata/bench.json.golden
- FOUND commit 7a5fb76 (feat — bench noun)
- FOUND commit fcb31ae (test — exit mapping + --ab restore + golden)
