---
phase: 09-villa-bench-honest-a-b
reviewed: 2026-06-06T00:00:00Z
depth: deep
files_reviewed: 8
files_reviewed_list:
  - internal/llm/openai.go
  - internal/llm/openai_test.go
  - internal/bench/bench.go
  - internal/bench/stats.go
  - internal/bench/bench_test.go
  - cmd/villa/bench.go
  - cmd/villa/bench_test.go
  - cmd/villa/root.go
findings:
  critical: 0
  warning: 5
  info: 4
  total: 9
status: issues_found
fix_status: warnings_resolved
fix_summary: "WR-01..WR-05 all fixed and committed (info-level IN-01..IN-04 intentionally out of scope)."
fixed:
  - id: WR-01
    commit: c6bcb8f
    note: "Live --ab Restore closure now prints a LOUD stderr WARNING + recovery guidance and propagates the error; benchBackendSwap seam + cmd-level test added."
  - id: WR-02
    commit: 1cbb3a3
    note: "Complete returns ErrNoTimings when predicted_n==0 && predicted_per_second==0; liveMeasure voids such a run; llm-level tests added."
  - id: WR-03
    commit: 36b9cd6
    note: "bench.Run takes a ctx param; RunE passes cmd.Context() so SIGINT propagates; per-run hang-guard preserved; call sites + tests updated."
  - id: WR-04
    commit: 9a23fb5
    note: "RunE rejects --reps/--n-predict < 1 and --warmup < 0 before building the spec; table-driven cmd-level test added."
  - id: WR-05
    commit: 5225e03
    note: "gofmt -w on internal/bench/bench.go and cmd/villa/bench.go; gofmt -l clean in-scope (install.go pre-existing/out-of-scope)."
---

# Phase 9: Code Review Report

**Reviewed:** 2026-06-06
**Depth:** deep (cross-file analysis, import graphs, call chains)
**Files Reviewed:** 8
**Status:** issues_found

## Summary

Phase 9 (`villa bench` honest A/B) is, as the research framed it, a composition phase, and the
core methodology invariants are implemented correctly and well-tested. I verified each of the
eight focus areas:

- **Residency void-gate (correct):** `benchN` excludes a `resident==false`/error run from `kept`,
  increments `void`, and never substitutes it as a slow pass (`bench.go:210-217`); `liveMeasure`
  sets `resident` only on `inference.StatusPass` (`bench.go:184`). Void-exhaustion WARN is
  reachable and bounded by `cap := 2*spec.Reps` (`bench.go:209-210`). Verified by `TestVoidNonResident`,
  `TestVoidExhaustionWarn`.
- **pp/tg separation (correct):** pp and tg are carried in separate fields/slices end-to-end
  (`statsOf` gathers two slices, never concatenated; `abResult` deltas are per-metric). No blended
  figure exists anywhere. Verified by `TestSeparatePPTG` and the `--json` golden (3× each key, 0×
  blended).
- **Stats math (correct):** median handles odd/even/empty; stddev is sample (n−1) and returns 0
  for n<2 — no NaN, no divide-by-zero, no panic. Verified by `TestStats`.
- **`--ab` always-restore (mostly correct, one real gap — see WR-01):** `defer d.Restore(ctx, orig)`
  is registered BEFORE the flip, so success / Switch-error / void-exhaustion / panic-unwind all
  restore. Defer ordering is correct. The gap is that a *failed* restore is silently swallowed.
- **`Complete` HTTP path (one real gap — see WR-02):** context/timeout honored, body closed,
  error reads bounded by `io.LimitReader(2048)`. But an absent `timings` block decodes to a
  zero-valued struct returned WITHOUT error — silently polluting the band with 0 tok/s.
- **Goroutine sampler (correct):** the during-decode goroutine writes to a buffered (cap-1) channel,
  so it never leaks even when the deadline fires; the ticker is `defer`-stopped. No leak.
- **Seam purity (correct):** `internal/bench` imports neither `internal/inference` nor
  `internal/detect`; `TestSeamGrepGate` passes; `cmd/villa/bench.go` is literal-free of markers.
- **Context threading (see WR-03):** the bench core threads its own `context.Background()`, which
  preserves the per-run hang-guard but severs caller cancellation (Ctrl-C) of an in-flight bench.

No Critical/BLOCKER defects: the residency gate, pp/tg separation, stats, and defer-ordering are
all sound, and a failed `backendswap.Run` self-rolls-back so the stack is never left half-mutated.
The five Warnings are honesty/robustness gaps that degrade the phase's own "trustworthy number"
bar, plus an input-validation and a gofmt/CI hygiene defect.

## Warnings

### WR-01: Failed `--ab` restore is silently swallowed — user can be left on the wrong backend with zero indication

> **RESOLVED (commit c6bcb8f):** the live `Restore` closure now prints a LOUD stderr WARNING with `villa backend show` / `villa backend set` recovery guidance and propagates the error (via a `benchBackendSwap` seam); `TestBenchABFailedRestoreWarns` covers it. Always-restore defer ordering unchanged.


**File:** `internal/bench/bench.go:254` (with live wiring `cmd/villa/bench.go:219-221`, `230-242`)
**Issue:** The always-restore is `defer d.Restore(ctx, orig) //nolint:errcheck // best-effort restore; live layer logs`.
The error is discarded by the bare `defer`. The comment claims "live layer logs," but the live
`Restore` closure is `func(_ ctx, original) error { return runBackendSwap(original) }` and
`runBackendSwap` only *returns* an error — it prints/logs nothing. So if the restore-to-original
itself fails (e.g. the Vulkan bring-up after measuring ROCm errors, or rollback is incomplete),
the user is left on the non-default backend with NO message and exit code unaffected. This directly
defeats RESEARCH Pitfall 4 ("`--ab` leaving the user on the wrong backend"), whose whole guarantee
is that the user always *knows* they are back on the original. (The stack itself is not
half-mutated — `backendswap.Run` self-rolls-back a failed switch — but "silently on ROCm" is the
exact failure Pitfall 4 exists to prevent.)
**Fix:** Capture and surface the restore error. Since `bench.Run` is print-free, do it in the live
closure so the cmd layer reports it. Either log inside the closure, or have the cmd layer wrap
Restore and print on error, e.g.:
```go
d.Restore = func(_ context.Context, original string) error {
    if err := runBackendSwap(original); err != nil {
        // restore failed: the stack may still be on the other backend — make it LOUD.
        fmt.Fprintf(os.Stderr, "bench: WARNING — failed to restore original backend %q: %v\n"+
            "  run `villa backend show` and `villa backend set %s` to recover\n", original, err, original)
        return err
    }
    return nil
}
```
Add a cmd-level test that drives an `--ab` whose Restore errors and asserts the warning is printed.

### WR-02: Absent/empty `timings` block is folded into the band as a valid 0 tok/s sample instead of voiding the run

> **RESOLVED (commit 1cbb3a3):** `Complete` now returns a distinct `llm.ErrNoTimings` sentinel when `predicted_n==0 && predicted_per_second==0`, and `liveMeasure` treats it as a VOID measurement (resident=false) with an honest detail; `TestCompleteVoidsAbsentTimings` (absent/empty/all-zero) covers it.


**File:** `internal/llm/openai.go:178-182` and `cmd/villa/bench.go:176-184`
**Issue:** `Complete` JSON-decodes only `completeResponse{ Timings }`. If the running server's
`/v1` response omits the `timings` extension (RESEARCH Assumption A1 explicitly flags this as a
real per-build possibility — "if `/v1` omits it on a given build, fall back to `/completion`"),
the decode succeeds with a zero-valued `Timings` and `Complete` returns `Timings{}, nil` — no error.
`liveMeasure` then returns `RunTimings{PromptPerSec: 0, PredictedPerSec: 0}` with `resident`
decided purely by the offload verdict. A GPU-resident run with a missing timings block is therefore
KEPT and its 0 tok/s is folded into the median — silently corrupting the "honest" band that is the
entire point of the phase. There is no guard anywhere (`grep` confirms no `PredictedN==0` /
`timings` presence check).
**Fix:** Treat a missing/empty timings block as a measurement failure (void), not a 0-rate pass.
The cleanest place is the live layer, which already knows a real run produced tokens:
```go
// in liveMeasure, after reading `timings`:
if timings.PredictedN == 0 || timings.PredictedPerSec == 0 {
    return bench.RunTimings{}, false,
        "server returned no `timings` block (or zero predicted tokens) — " +
        "this build may not expose /v1 timings; cannot honestly measure tg",
        fmt.Errorf("missing timings in completion response")
}
```
(Per A1, the documented remediation is to fall back to `/completion`; at minimum the run must VOID
rather than report 0 tok/s as a measurement.)

### WR-03: Bench core hard-codes `context.Background()`, severing caller cancellation of a multi-minute run

> **RESOLVED (commit 36b9cd6):** `Run(ctx context.Context, d Deps, spec BenchSpec) Result` now threads the caller's context; `RunE` passes `cmd.Context()` (cobra's SIGINT-cancelled context). The per-run `spec.Timeout` hang-guard is preserved (derived from this ctx in `liveMeasure`). Call sites + tests updated.


**File:** `internal/bench/bench.go:230`
**Issue:** `Run` does `ctx := context.Background()` and threads that into every `Measure`. The only
cancellation source is the per-run `spec.Timeout` re-derived inside `liveMeasure`
(`context.WithTimeout(ctx, spec.Timeout)`). The per-run hang-guard is preserved, but there is no
way for the caller to cancel an in-progress bench: a `villa bench --ab -n 5` can run for many
minutes (two backend restarts + 10+ residency-checked 128-token completions), and a Ctrl-C cannot
propagate cancellation into the in-flight HTTP call or abort the loop. This is the bench-03 deviation
("pure core threads its own context.Background()"). It is not a hang-guard regression, but it makes
a long bench un-interruptible and forfeits standard CLI signal handling.
**Fix:** Thread a real context through the public API rather than synthesizing `Background()`:
`func Run(ctx context.Context, d Deps, spec BenchSpec) Result` and have `newBench`'s `RunE` pass
`cmd.Context()` (cobra installs a SIGINT-cancelled context). The live `Measure`/`Switch`/`Restore`
closures already accept a `ctx` parameter that is currently ignored — wiring the real one in is a
small change that restores Ctrl-C semantics and per-bench deadline composition.

### WR-04: No validation of `--reps` / `--warmup` / `--n-predict` — negative/zero values produce confusing or malformed behavior

> **RESOLVED (commit 9a23fb5):** `RunE` now rejects `--reps`/`--n-predict` `< 1` and `--warmup` `< 0` with a clear usage error before building the spec; `TestBenchFlagValidation` (table-driven over `-n 0`, `-n -5`, `--n-predict 0`, `--n-predict -1`, `--warmup -1`) covers it.


**File:** `cmd/villa/bench.go:276-296`, `internal/bench/bench.go:209`
**Issue:** The int flags are passed through unvalidated. `-n 0` → `cap := 2*0 == 0` → the measured
loop never runs → 0 kept → reported as "void-exhaustion WARN" rather than an obvious usage error.
`-n -5` → `cap == -10`, loop body skipped, same confusing WARN. `--n-predict -1` is sent verbatim
as `"max_tokens": -1` on the wire to the server (`completeRequest.MaxTokens`), an out-of-contract
request. `--warmup -1` is harmless (loop guard) but still nonsensical. None of these is rejected at
the cobra boundary.
**Fix:** Validate in `RunE` before building the spec (this is the V5 input-validation control the
RESEARCH Security Domain section claims as already covered — "`-n`/`--n-predict` are bounded ints"):
```go
if reps < 1 || warmup < 0 || nPredict < 1 {
    return fmt.Errorf("bench: --reps and --n-predict must be >= 1 and --warmup >= 0")
}
```

### WR-05: Phase source files are not gofmt-clean (CI hygiene; struct field misalignment)

> **RESOLVED (commit 5225e03):** `gofmt -w internal/bench/bench.go cmd/villa/bench.go` applied (struct-tag alignment); `gofmt -l` is now clean for the in-scope files. `cmd/villa/install.go` remains dirty but is pre-existing and out of this phase's scope. No `max()`/`min()` rewrite was flagged by gofmt, so behavior is unchanged.


**File:** `cmd/villa/bench.go:344-352` (`benchSide` / `benchAB` field alignment) and `internal/bench/bench.go`
**Issue:** Running `go fmt ./internal/bench/... ./cmd/villa/...` reformats both `internal/bench/bench.go`
and `cmd/villa/bench.go` (the `benchSide`/`benchAB` struct tags are misaligned, e.g.
`PredictedPerSec float64 \`json:...\`` not column-aligned with its siblings). Committed phase code
that is not gofmt-clean fails the standard `gofmt -l` CI gate and contradicts the project's
single-source-of-truth tidiness. (`cmd/villa/install.go` is also dirty but is outside this phase's
scope — pre-existing.)
**Fix:** Run `gofmt -w internal/bench/bench.go cmd/villa/bench.go` and commit; add `gofmt -l` to the
per-commit gate if not already enforced.

## Info

### IN-01: `cap` shadows the builtin in `benchN`

**File:** `internal/bench/bench.go:209`
**Issue:** `cap := 2 * spec.Reps` shadows the builtin `cap()`. It compiles and is correct, but
shadowing a builtin is a readability smell and would bite if `cap()` were later needed in scope.
**Fix:** Rename to `attemptCap` (or `maxAttempts`).

### IN-02: `--json` local flag silently shadows the inherited persistent `--json` (`jsonOut`)

**File:** `cmd/villa/bench.go:295` vs `cmd/villa/root.go:30`
**Issue:** `root` registers a *persistent* `--json` (`jsonOut`); `bench` registers a *local* `--json`
(`asJSON`). Cobra resolves the local flag for `bench`, so behavior is correct, but two distinct vars
back the same flag name depending on command — a latent footgun (a future reader wiring `jsonOut`
into bench would find it always false). `backend show` already does this, so it is an established
(if questionable) in-repo pattern, not a new defect.
**Fix:** Prefer reading the inherited persistent `--json` instead of re-declaring a local one, or
document the shadowing; at minimum keep the two patterns consistent across nouns.

### IN-03: Dead `abResult` computation on the Switch-error path

**File:** `internal/bench/bench.go:262`
**Issue:** On a Switch error, `abResult(orig, statsA, Stats{})` is built and stored in
`Result{AB: &ab, Err: err}`, but `runBench` returns at the `res.Err != nil` check before rendering,
so the `AB` block (with a zero-valued side B) is never used. Harmless, but it computes and attaches
a misleading zero-side-B result.
**Fix:** Return `Result{Err: err}` (no `AB`) on the Switch-error path, or document why `AB` is
attached for a caller that might inspect it.

### IN-04: No cmd-level test for the `--ab` Switch-failure exit mapping

**File:** `cmd/villa/bench_test.go` (gap), exercising `cmd/villa/bench.go:386-389`
**Issue:** `internal/bench/bench_test.go` covers a side-B *Measure* error, and the cmd tests cover
no-endpoint / void-exhaustion / clean / `--ab` happy-path. No test drives an `--ab` whose `Switch`
errors and asserts `runBench` maps `res.Err != nil` to `exitBlocked` and prints the failure. Given
WR-01 (failed-restore visibility), this path warrants explicit coverage.
**Fix:** Add a `newBenchStub` variant whose `Switch` returns an error and assert
`code == exitBlocked` + the `bench: <err>` message.

---

_Reviewed: 2026-06-06_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
