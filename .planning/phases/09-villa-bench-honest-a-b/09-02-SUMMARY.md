---
phase: 09-villa-bench-honest-a-b
plan: 02
subsystem: bench
tags: [benchmark, methodology, residency-gate, ab-compare, pure-core, stats]
requires:
  - "internal/backendswap.Run (the LOCKED switch the --ab flip delegates to, never re-implements)"
  - "internal/llm.Timings (the pp/tg-separated per-run measurement source, Plan 09-01)"
  - "internal/config.VillaConfig (original-backend source for the --ab restore target)"
provides:
  - "internal/bench: BenchSpec, RunTimings, Deps, Stats, Result, ABResult, Run — the pure Deps-injected honest-benchmark core"
  - "internal/bench: median/stddev stdlib stats helpers (per-metric, panic-free)"
  - "internal/bench: OnSideStart observational seam (cobra progress + identical-spec assertion)"
affects:
  - "cmd/villa/bench.go (Plan 09-03): wires liveMeasure/liveBenchDeps into bench.Run and maps Result to exit codes + --json"
tech-stack:
  added: []
  patterns:
    - "Pure Deps-injected core cloned from internal/backendswap (Deps/Result/Run), print-free and exit-free"
    - "Residency void-gate: resident==false ⇒ excluded from Kept, counted in Void, never substituted as a slow pass"
    - "Bounded attempt cap (2*Reps) so an all-void host terminates into an honest void-exhaustion WARN"
    - "defer Restore(orig) registered BEFORE the --ab flip so every exit path restores the original backend"
    - "pp and tg carried as separate fields/slices end-to-end; no blended tok/s figure anywhere"
key-files:
  created:
    - "internal/bench/bench.go"
    - "internal/bench/stats.go"
    - "internal/bench/bench_test.go"
  modified: []
decisions:
  - "[09-02] internal/bench is the pure honest-benchmark core: warmup-discard → N residency-checked measured runs → separate pp/tg median+stddev over KEPT runs only; a non-resident run is VOID (excluded, counted), and < MinResident resident runs yields VoidExhausted WARN rather than a confident band. Print-free/exit-free; the cobra layer (09-03) owns presentation."
  - "[09-02] The --ab flip composes backendswap.Run via the injected Switch/Restore seams and NEVER re-implements backend switching (STATE.md LOCKED). defer d.Restore(ctx, orig) is registered BEFORE the flip so success / mid-AB error / void-exhaustion / panic-unwind ALL restore the original backend; TestBenchABRestoresOriginal asserts Restore(orig) is the final backend op, exactly once, after the Switch."
  - "[09-02] The measured loop is bounded by a 2*Reps attempt cap so an all-void host can never loop forever — it terminates into an honest void-exhaustion WARN (Pitfall 5)."
  - "[09-02] internal/bench imports neither internal/inference nor internal/detect; backend markers arrive ONLY via the injected Measure verdict. other() swaps vulkan<->rocm by config-VALUE equality (local 2-value, no allowlist, no marker literal) so TestSeamGrepGate stays green."
  - "[09-02] Added an OnSideStart(side, spec) observational Deps seam (nil-safe): the cobra layer uses it for per-side progress and the test uses it to assert both --ab sides receive a byte-identical BenchSpec. It fires no host action."
requirements-completed: [BENCH-02]
metrics:
  duration: 4 min
  completed: 2026-06-06
---

# Phase 9 Plan 2: internal/bench (Honest Benchmark Core) Summary

The pure, Deps-injected `internal/bench` core implements honest throughput methodology — warmup-discard, per-run residency void-gating, bounded void-exhaustion WARN, and separated pp/tg median+stddev — plus the LOCKED `--ab` always-restore composition that delegates backend switching to `backendswap.Run`, all exercised off-hardware by a fake-Deps recorder.

## What Was Built

**`internal/bench/bench.go`** — the pure core (cloned from the `internal/backendswap` Deps/Result/Run idiom):
- Types: `BenchSpec` (reproducible Reps/Warmup/Prompt/NPredict/Seed/Temp/Timeout/MinResident), `RunTimings` (pp/tg already separated), `Deps` (injected `Measure`/`Switch`/`Restore`/`LoadConfig` + an observational `OnSideStart`), `Stats` (`MedianPP`/`StddevPP`/`MedianTG`/`StddevTG`/`Kept`/`Void` — no blended field), `Result`, `ABResult` (per-metric `DeltaPP`/`DeltaTG`).
- `Run(d Deps, spec BenchSpec) Result` — single-backend when Switch/Restore are nil; `--ab` when set. `benchN` runs `Warmup` discarded measures then keeps measuring until `Reps` RESIDENT runs are collected or a `2*Reps` attempt cap is hit; non-resident/error runs are voided; kept runs fold into per-metric `Stats`; `Kept < MinResident` sets `VoidExhausted` + an honest Reason.
- `--ab`: `LoadConfig` → `orig` → `defer d.Restore(ctx, orig)` BEFORE the flip → bench side A → `d.Switch(ctx, other(orig))` → bench side B with the SAME spec → per-metric `ABResult`. A Switch error is surfaced; the deferred Restore still fires.

**`internal/bench/stats.go`** — stdlib-only `median` (odd/even; 0 for empty) and `stddev` (sample n−1; 0 for n<2). Pure and panic-free; pp/tg passed as SEPARATE slices via `statsOf`.

**`internal/bench/bench_test.go`** — a `benchRecorder` (callOrder + replayed `measureVerdict` queue) driving all seven invariants: `TestStats`, `TestSeparatePPTG`, `TestWarmupDiscarded`, `TestVoidNonResident`, `TestVoidExhaustionWarn`, `TestIdenticalSpecBothSides`, `TestBenchABRestoresOriginal`.

## Verification

- `go test ./internal/bench/ -count=1` → 7 passed.
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` → passed (internal/bench introduces no backend-marker literal).
- `grep -rn 'internal/inference\|internal/detect' internal/bench/` → matches in comments only (no import).
- `go build ./...` clean; `go vet ./...` clean; `go.mod`/`go.sum` byte-unchanged.

## Deviations from Plan

### Auto-added (Rule 2 — supporting seam for a required behavior)

**1. [Rule 2 - Missing seam] Added `Deps.OnSideStart(side, spec)` observational hook**
- **Found during:** Task 2 (TestIdenticalSpecBothSides).
- **Issue:** The plan's `TestIdenticalSpecBothSides` requires the recorder to "capture the BenchSpec passed to each side," but the fixed `Measure` signature carries no spec, so neither side's spec was observable through any seam — the test could not be satisfied without one.
- **Fix:** Added a nil-safe, purely observational `OnSideStart(side string, spec BenchSpec)` Deps field that `benchN` calls once at each side's start. It fires no host action; it serves both the identical-spec assertion and per-side progress reporting for the cobra layer (Plan 09-03). The published `Measure`/`Switch`/`Restore` signatures are unchanged, so the live wiring contract is intact.
- **Files modified:** internal/bench/bench.go, internal/bench/bench_test.go.
- **Commit:** 9a6b7e0.

## Threat Model Outcome

All three `mitigate` dispositions are implemented and test-asserted:
- **T-09-03** (--ab leaving a non-default backend): `defer d.Restore(ctx, orig)` registered before the flip; `TestBenchABRestoresOriginal` asserts the final backend op is `Restore(orig)`, exactly once, after the Switch.
- **T-09-04** (void counted as a slow pass): `resident==false` runs are excluded from `Kept` and counted in `Void`, never substituted; `< MinResident` ⇒ `VoidExhausted` WARN. `TestVoidNonResident` / `TestVoidExhaustionWarn` assert this.
- **T-09-05** (marker literal leaking into internal/bench): no inference/detect import; `other()` swaps by config-value equality with no marker literal; `TestSeamGrepGate` green.
- **T-09-SC** (package installs): none — stdlib + existing repo packages only; `go.mod` byte-unchanged.

No new security-relevant surface introduced (no network endpoints, no auth paths, no file access) — the core is pure and host-touching only through injected seams.

## Self-Check: PASSED

- Files: internal/bench/bench.go, internal/bench/stats.go, internal/bench/bench_test.go — all FOUND.
- Commits: 7f738e3 (Task 1), 9a6b7e0 (Task 2) — both FOUND.
