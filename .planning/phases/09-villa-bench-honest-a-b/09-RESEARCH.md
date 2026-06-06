# Phase 9: `villa bench` (Honest A/B) — Research

**Researched:** 2026-06-06
**Domain:** Go control-plane CLI — honest throughput benchmarking over a running llama.cpp `llama-server` OpenAI-compatible endpoint, composing the Phase-8 backend switch for `--ab`.
**Confidence:** HIGH (codebase patterns + llama.cpp `/v1` timings/metrics contract are verified; the per-model *magnitude* of the pp/tg delta is on-hardware-volatile by design — that is the success-criterion to validate live, not a research gap)

## Summary

Phase 9 is almost entirely a **composition phase**, not a new-capability phase. Every primitive it needs already exists in the repo behind a clean seam: the OpenAI-compatible client (`internal/llm`), the running-server probes (`inference.PollHealth` / `inference.GenerationProbe`), the residency proof (`inference.RunningOffloadVerdict` + `BackendFor(target).ResidencyProof()` + `detect.GPUBusyPercent()`), the live-during-decode gpu_busy sampling pattern (`cmd/villa/backend.go:liveProve`), and — critically — a stdlib Prometheus-text scraper plus the confirmed llama.cpp gauge names (`internal/metrics`). The Phase-8 `internal/backendswap.Run` transactional core is the *only* way `--ab` may flip backends; bench must never re-implement switching (locked decision, STATE.md).

The single load-bearing research finding is the **measurement source**: llama.cpp's per-request `timings` block (`prompt_per_second`, `predicted_per_second`, `prompt_n`, `predicted_n`, `prompt_ms`, `predicted_ms`) is the *correct* honest source — it gives a clean, per-request, already-separated pp-vs-tg figure for exactly the generation just run. The existing `internal/metrics` `/metrics` scrape (`llamacpp:prompt_tokens_seconds` / `llamacpp:predicted_tokens_seconds`) is a **last-window AVERAGE gauge** (documented in `internal/metrics/llamacpp.go`), NOT a per-request reading — it will smear warmup and prior runs into the number and must NOT be the bench's primary source. Bench should drive a controlled non-streaming `/v1/chat/completions` (or `/completion`) request and read `timings.prompt_per_second` / `timings.predicted_per_second` from the response body. This requires a small new client method (the existing `llm.OpenAIClient` only forwards content deltas and discards `timings`/`usage`).

The honest-methodology layer (warmup discard, N reps, median + stddev/noise band, identical params both sides, void non-resident runs) is pure Go stdlib arithmetic over a slice of per-run `timings` — no new dependency. Residency-gating reuses the Phase-8 `liveProve` composition verbatim: a run whose `RunningOffloadVerdict` is not `StatusPass` (CPU fallback, 0% gpu_busy during decode) is **void**, not a slow pass.

**Primary recommendation:** Build `internal/bench` as a pure, Deps-injected core (mirroring `internal/backendswap`): it takes a `Measure(ctx) (RunTimings, ResidencyVerdict)` seam + an N/warmup spec and returns separate pp/tg median+stddev statistics over residency-checked runs only. Drive measurement off the per-request `timings` block (add `llm.OpenAIClient.Complete` that does a non-streaming POST and returns the parsed `timings`). Wire `villa bench` (current backend) and `villa bench --ab` (compose `backendswap.Run` to flip Vulkan↔ROCm, bench each, restore the original backend) in `cmd/villa/bench.go`, returning a `--json` shape Phase 10 surfacing reads.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Drive a controlled generation (fixed prompt/n_predict/seed/temp) | API/Backend (llama-server `/v1`) | — | The running server owns inference; bench is a read-only client over the OpenAI contract. |
| Read per-request pp/tg throughput | API/Backend (`timings` in response) | — | `timings.prompt_per_second`/`predicted_per_second` are computed server-side per request — the honest, already-separated source. |
| Warmup discard + N-rep + median/stddev/noise band | Control plane (`internal/bench`, pure) | — | Pure arithmetic over collected `timings`; stdlib only; deterministic + unit-testable off-hardware. |
| Residency gate (void CPU-fallback runs) | Control plane (`inference.RunningOffloadVerdict`) | Host (`detect.GPUBusyPercent`, journald) | Reuse the Phase-6/8 offload-assert; a run that isn't GPU-resident is not a measurement. |
| `--ab` backend flip + restore | Control plane (`internal/backendswap.Run`) | Host (systemd/quadlet) | LOCKED: bench composes the Phase-8 switch verb, never re-implements switching. |
| Exit-code mapping + table/`--json` render | cmd/villa (`bench.go`) | — | CLI presentation + the frozen scriptable contract; the core stays print/exit-free. |

## Standard Stack

This phase adds **zero new external dependencies**. Everything is Go stdlib (`encoding/json`, `net/http`, `math`, `sort`, `time`, `context`) plus already-vendored in-repo packages.

### Core (all pre-existing, reused)
| Package / Symbol | Purpose | Why Standard (here) |
|------------------|---------|---------------------|
| `internal/llm` `OpenAIClient` | OpenAI-compatible HTTP client (`/v1/chat/completions`) | D-04 reuse mandate; the gateway is reused, never rebuilt. **Needs a non-streaming `Complete` method that returns the `timings` block** (current `StreamChat` discards it). |
| `internal/metrics` `parsePromText` / `ScrapeMetrics` | stdlib Prometheus-text parser + `/metrics` gauges | Already proves the `/metrics` contract; usable as a *corroborating* secondary signal (not the primary per-run source). |
| `internal/inference` `PollHealth`, `GenerationProbe`, `RunningOffloadVerdict`, `BackendFor`, `NewContainerRunner(...).Endpoint()` | readiness, residency proof, endpoint resolution | Exact primitives `liveProve` already composes (Phase 8); bench reuses them for the residency gate + endpoint. |
| `internal/backendswap` `Run`, `Deps`, `Result`, `ProveStatusPass` | transactional backend switch | LOCKED composition target for `--ab`. Bench calls `Run` to flip and again to restore. |
| `internal/detect` `GPUBusyPercent`, `GTTUsedBytes` | during-decode residency corroborator | D-07 read; sample DURING the bench generation, keep max (clone `liveProve`'s goroutine+ticker pattern). |
| `internal/config` `LoadVilla` | source of truth (backend, model, ctx) | Resolve endpoint/model/backend the same way every live-deps site does. |
| `github.com/spf13/cobra` v1.10.2 | command tree | Project standard; `bench` is a new noun on `newRoot()`. |

### Supporting (statistics — stdlib, hand-rolled, intentionally)
| Need | Approach | Why Standard |
|------|----------|--------------|
| Median | `sort.Float64s` + middle element (mean of two middles on even N) | Trivial, exact, no dependency; the project has zero stats libs (verified `go.mod`). |
| Stddev / noise band | `math.Sqrt` over sample variance of the N retained per-metric throughputs | "Don't hand-roll" does NOT apply to a 5-line sample-stddev — pulling `gonum` would violate the single-static-binary / no-heavy-deps constraint. |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Per-request `timings` block | Scraping `/metrics` `llamacpp:*_tokens_seconds` gauges | **REJECT as primary.** Those are last-window *averages* (see `internal/metrics/llamacpp.go` doc + verified upstream) — they smear warmup/prior requests and are not per-run. Fine as a sanity overlay only. |
| Driving `/v1/chat/completions` | Driving the lower-level `/completion` endpoint | `/completion` exposes `timings` + `n_predict` + `cache_prompt` more directly and avoids chat-template overhead; either works. `/v1` keeps the existing `llm` client path. Plan can pick; `/v1` is the lower-risk reuse. |
| Hand-rolled median/stddev | `gonum.org/v1/gonum/stat` | New heavy dependency — violates the single-static-binary constraint. Reject. |

**Installation:** none — `go.mod` unchanged. Confirm with `go build ./... && go vet ./...`.

**Version verification:** Go `go1.26.2` (verified `go version`). Deps unchanged from the existing pinned set (`spf13/cobra v1.10.2`, `jaypipes/ghw v0.24.0`).

## Package Legitimacy Audit

> This phase installs **no external packages**. All code is Go stdlib + already-vendored in-repo modules. slopcheck is N/A (no registry installs).

| Package | Registry | Disposition |
|---------|----------|-------------|
| *(none — stdlib + existing repo packages only)* | — | No new installs |

**Packages removed due to slopcheck [SLOP] verdict:** none.
**Packages flagged as suspicious [SUS]:** none.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| BENCH-01 | `villa bench` runs A/B (Vulkan vs ROCm) on the loaded model, reporting **pp and tg tok/s separately** (never blended), over residency-checked runs only | pp/tg come pre-separated from the `timings` block (`prompt_per_second` / `predicted_per_second`); residency gate = `RunningOffloadVerdict==StatusPass` per run (reuse Phase 8); `--ab` flips via `backendswap.Run` (Architecture Patterns §1–4). |
| BENCH-02 | Honest methodology — discarded warmup, N reps with median + stddev/noise band, identical model/quant/context/flags both sides, stated conditions; per-metric delta = proof-of-value | Pure-stdlib stats core over collected per-run `timings`; identical params enforced by a single fixed `BenchSpec` (prompt, n_predict, seed=fixed, temperature=0) used on BOTH sides; "model = config" so quant/ctx/flags are already identical across a switch (the unit only changes backend). (Architecture Patterns §5, Common Pitfalls). |

## Architecture Patterns

### System Architecture Diagram

```
                          villa bench [--ab] [-n N] [--json]
                                      │
                                      ▼
                        ┌──────────────────────────┐
                        │  cmd/villa/bench.go       │  (cobra noun, exit map,
                        │  - liveBenchDeps()        │   table/--json render)
                        │  - runBench()             │
                        └────────────┬─────────────┘
                                     │ injects Deps
                                     ▼
                        ┌──────────────────────────┐
                        │  internal/bench (PURE)    │
                        │  Run(Deps, BenchSpec)     │
                        │  - warmup discard         │
                        │  - N reps                 │
                        │  - per-run residency gate │ ◀── void non-resident runs
                        │  - median + stddev        │
                        └──────┬───────────────┬────┘
                               │ Measure()     │ Switch()/Restore()  (only when --ab)
                               ▼               ▼
        ┌───────────────────────────┐   ┌──────────────────────────────┐
        │ ONE residency-checked run │   │ internal/backendswap.Run      │ (Phase 8 — LOCKED,
        │ (a) GenerationProbe-style │   │  capture→mutate→prove→rollback│  never reimplemented)
        │     non-streaming POST    │   └───────────────┬──────────────┘
        │     /v1/chat/completions  │                   │ flips villa-llama unit
        │  ──► read timings{pp,tg}  │                   ▼
        │ (b) sample GPUBusyPercent │            systemd / quadlet
        │     DURING decode (D-07)  │            (rootless podman)
        │ (c) RunningOffloadVerdict │
        │     == StatusPass ? keep  │   ◀── llama-server /v1 + /metrics + /props
        │        : VOID this run    │       (loopback 127.0.0.1, --metrics already on)
        └───────────────────────────┘
```

A reader can trace the primary use case: `villa bench` → resolve endpoint+model+backend from config → warmup run (discarded) → N residency-checked timed runs reading `timings` → fold into median/stddev pp + median/stddev tg → render two separate figures. `--ab` wraps that loop: bench current → `backendswap.Run(other)` → bench other → `backendswap.Run(original)` to RESTORE → render the per-metric delta + noise band.

### Recommended Project Structure
```
internal/bench/
├── bench.go        # PURE Deps-injected core: BenchSpec, RunResult, Stats, Run(Deps,BenchSpec)
├── stats.go        # median/mean/stddev helpers (stdlib math/sort) — or fold into bench.go
└── bench_test.go   # Wave-0 fake-Deps recorder: warmup-discarded, void-run-excluded,
                    #   median/stddev correctness, identical-spec-both-sides, --ab restore
cmd/villa/
├── bench.go        # cobra `bench` noun + --ab/-n/--warmup flags; liveBenchDeps();
│                   #   runBench() Result→exit map; table + --json render
└── bench_test.go   # stubbed Deps: exit mapping, --json golden, --ab restores original
cmd/villa/testdata/
└── bench.json.golden  # frozen --json shape (Phase 10 reads tok/s from here)
```

### Pattern 1: Pure Deps-injected core (clone `internal/backendswap`)
**What:** `internal/bench` is a pure state-machine with every host-touching action as an injected `Deps` field, so `bench_test.go` drives the whole warmup→N-reps→void-gate→stats flow with no live host.
**When to use:** Always — this is the established repo idiom (`backendswap`, `status`, `modelswap`).
```go
// internal/bench/bench.go
type BenchSpec struct {
    Reps      int           // N residency-checked runs to keep (default e.g. 5)
    Warmup    int           // runs to discard first (default 1)
    Prompt    string        // FIXED both sides
    NPredict  int           // FIXED both sides (e.g. 128) — bounds tg sample size
    Seed      int           // FIXED both sides
    Temp      float64       // 0 for determinism
    Timeout   time.Duration // per-run bound (load_tensors-hang guard, mirror proveTimeout)
}

type RunTimings struct { PromptPerSec, PredictedPerSec float64; PromptN, PredictedN int }

type Deps struct {
    // Measure drives ONE controlled generation and returns its per-request timings
    // plus whether the run was GPU-resident. A non-resident run is VOID.
    Measure func(ctx context.Context) (t RunTimings, resident bool, detail string, err error)
    // Switch/Restore are set ONLY for --ab; both delegate to backendswap.Run (LOCKED).
    Switch  func(ctx context.Context, target string) error
    Restore func(ctx context.Context, original string) error
}

// Stats are computed SEPARATELY for pp and tg — never blended.
type Stats struct { MedianPP, StddevPP, MedianTG, StddevTG float64; Kept, Void int }
```

### Pattern 2: Per-request `timings` measurement (the honest source)
**What:** Drive a non-streaming completion and read the server-computed `timings` block — pp and tg arrive already separated and scoped to exactly this request.
**When to use:** This is the primary measurement. Add a `Complete` method to `llm.OpenAIClient` (the existing `StreamChat` discards `timings`).
```go
// internal/llm/openai.go — NEW non-streaming method (stream:false to get the timings block)
type Timings struct {
    PromptN         int     `json:"prompt_n"`
    PromptMS        float64 `json:"prompt_ms"`
    PromptPerSecond float64 `json:"prompt_per_second"`
    PredictedN      int     `json:"predicted_n"`
    PredictedMS     float64 `json:"predicted_ms"`
    PredictedPerSec float64 `json:"predicted_per_second"`
}
// POST /v1/chat/completions with {stream:false, seed, temperature, max_tokens:NPredict};
// the response JSON carries a top-level "timings" object (llama.cpp extension) — unmarshal it.
```
**Source:** [CITED: github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md] — `timings.{prompt_n,prompt_ms,prompt_per_second,predicted_n,predicted_ms,predicted_per_second}` and cross-confirmed by a second source.

### Pattern 3: Residency gate per run (clone `liveProve`)
**What:** Each timed run runs the generation while sampling `detect.GPUBusyPercent()` DURING the decode (goroutine + ticker, keep max), then folds the invocation-scoped journal + GTT floor + max gpu_busy + `BackendFor(target).ResidencyProof()` markers through `inference.RunningOffloadVerdict`. A verdict ≠ `StatusPass` ⇒ the run is **void** (excluded from stats), not a slow pass.
**When to use:** Every timed run. This is `cmd/villa/backend.go:liveProve` lines 108–167 almost verbatim — extract/reuse rather than copy if practical.

### Pattern 4: `--ab` composes the Phase-8 switch and RESTORES (LOCKED)
**What:** `--ab` benches the current backend, calls `backendswap.Run(deps, other)` to flip, benches the other, then calls `backendswap.Run(deps, original)` to **restore the original backend** before returning — regardless of pass/fail.
**When to use:** `--ab` only. Bench MUST NOT touch quadlet/systemd directly (anti-pattern below).
```go
// pseudocode in internal/bench.Run when Switch/Restore are set:
orig := cfg.Backend
statsA := benchN(ctx, spec)               // current backend
defer d.Restore(ctx, orig)                // ALWAYS restore, even on a mid-AB failure
if err := d.Switch(ctx, other(orig)); err != nil { return ...rollback-aware error }
statsB := benchN(ctx, spec)               // other backend (identical spec)
// delta is computed per metric: ΔmedianPP, ΔmedianTG, each with its own noise band
```
A failed flip is already a no-op to the stack (backendswap rolls back); bench surfaces that honestly ("could not bring up rocm — bench aborted; original backend intact").

### Anti-Patterns to Avoid
- **Re-implementing backend switching in bench.** LOCKED decision (STATE.md `[v1.1 Roadmap]`): "Bench COMPOSES the Phase-8 `backend set` verb — it must never re-implement backend switching." `--ab` calls `backendswap.Run`, period.
- **Reporting a single blended tok/s.** Explicitly out-of-scope (REQUIREMENTS.md): pp (compute-bound) and tg (memory-bound) must be two separate figures. The data model carries them separately end-to-end.
- **Using `/metrics` gauges as the per-run number.** They are last-window averages — they leak warmup and prior requests into the figure. Use per-request `timings`.
- **Counting a CPU-fallback run.** A non-resident run is void, never a degraded pass (governing invariant: offload is asserted, not assumed).
- **Mutating the running unit to "isolate" pp from tg.** Not needed and disruptive — `timings` already separates them. Bench is read-only over `/v1` (+ a backend flip via the proven switch on `--ab`); SC#1 requires "non-disruptive" for the single-backend path.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Computing pp/tg tok/s | A wall-clock timer around the HTTP call + a token counter | `timings.prompt_per_second` / `predicted_per_second` from the response | The server measures the actual prompt-eval vs decode phases; a client wall-clock conflates them and includes network/queue time. |
| OpenAI-compatible HTTP/SSE | A new HTTP client | `internal/llm.OpenAIClient` (extend with `Complete`) | D-04: reuse the gateway, never rebuild. |
| Prometheus text parsing (if used as overlay) | A regex/parser | `internal/metrics.parsePromText` | Already written, label-hardened, tested. |
| Residency proof | A new offload check | `inference.RunningOffloadVerdict` + `liveProve` composition | Phase-6/8 engine; the residency markers are per-backend and seam-clean. |
| Backend flip | systemctl/quadlet calls | `internal/backendswap.Run` | LOCKED; gives transactional rollback for free. |
| Median/stddev | A stats dependency (`gonum`) | `sort.Float64s` + `math.Sqrt` (≤15 lines) | Single-static-binary / no-heavy-deps constraint; the math is trivial and exact. |

**Key insight:** Phase 9's risk is almost entirely *composition correctness* (residency gating, identical-params-both-sides, always-restore on `--ab`) and *measurement-source honesty* (per-request timings, not averaged gauges) — not novel algorithms. Treat the stats as the only "new" code and keep it pure + golden-tested.

## Common Pitfalls

### Pitfall 1: Mistaking `/metrics` average gauges for per-run throughput
**What goes wrong:** Using `llamacpp:prompt_tokens_seconds` / `llamacpp:predicted_tokens_seconds` as the per-run number yields a figure that smears warmup and all prior requests together — the noise band collapses to meaninglessness and warmup discard does nothing.
**Why it happens:** The gauges *look* like a live rate; `internal/metrics` already scrapes them for the dashboard panel.
**How to avoid:** Read the per-request `timings` block from the completion response. The `internal/metrics` doc comment already warns these gauges are "last-window snapshots, NOT a live signal."
**Warning signs:** Identical pp/tg across runs; near-zero stddev; the figure unchanged when you vary `n_predict`.

### Pitfall 2: Non-identical params across the A/B sides
**What goes wrong:** Different prompt length, `n_predict`, seed, temperature, or (worse) a different model/quant/ctx makes the Vulkan-vs-ROCm delta a comparison of two different workloads — the proof-of-value is invalid.
**Why it happens:** `cache_prompt` reuse, a re-pick of the model, or letting each side use its own defaults.
**How to avoid:** One `BenchSpec` (fixed prompt, `n_predict`, seed, temperature=0) applied to BOTH sides; "model = config" guarantees quant/ctx/flags are identical across a switch (the unit delta is backend-only — verified: ROCm render is a pure additive delta over Vulkan, same model/ctx/flags). Disable prompt cache (or warm it identically) so prompt-processing is measured, not skipped.
**Warning signs:** `timings.prompt_n` differs between sides; one side shows implausibly high pp (prompt was cached → near-zero prompt work).

### Pitfall 3: A load_tensors hang or CPU-fallback bench-run hanging forever
**What goes wrong:** A wedged or CPU-fallen-back server makes a bench run never complete.
**Why it happens:** Same gfx1151/ROCm fragility Phase 8 guards (`load_tensors` hang, allocation cap, firmware fault).
**How to avoid:** Bound every run with a per-run timeout (mirror `proveTimeout`=5m as the load_tensors-hang guard); a timed-out run is void/error, never an infinite wait. On `--ab`, a failed ROCm bring-up is already rolled back by `backendswap.Run` — bench reports it and keeps the original backend.
**Warning signs:** A run exceeding the timeout; `RunningOffloadVerdict` FAIL with 0% gpu_busy.

### Pitfall 4: `--ab` leaving the user on the wrong backend
**What goes wrong:** Bench flips to ROCm to measure it, then errors or returns without flipping back — user is silently left on a non-default backend.
**Why it happens:** Restore on the happy path only, or restore skipped on a mid-AB error/panic.
**How to avoid:** `defer d.Restore(ctx, original)` immediately after capturing the original backend, before the flip — so EVERY exit path (success, error, void-exhaustion) restores. Assert this in a test (`--ab` that fails mid-way still ends on the original backend).
**Warning signs:** `villa backend show` reports a different backend after a `bench --ab`.

### Pitfall 5: Counting too few residency-checked runs (void-run exhaustion)
**What goes wrong:** If most runs are void (non-resident), N "kept" runs may never be reached and the stats are computed over 1–2 samples — a meaningless noise band.
**Why it happens:** A flaky residency signal or a genuine intermittent CPU fallback.
**How to avoid:** Cap total attempts (e.g. `2*N + warmup`); if fewer than a minimum (e.g. 3) resident runs are collected, report a WARN/void verdict ("insufficient residency-checked runs to compute an honest band") rather than a confident delta. Surface `Kept` vs `Void` counts in the output.
**Warning signs:** `Kept < Reps`; a stddev computed over <3 samples.

### Pitfall 6: `/metrics` 404 (flag absent) — but it's already on
**What goes wrong:** Assuming `/metrics` must be enabled.
**Status:** **Non-issue, verified.** `--metrics` is already a fixed exec arg in the rendered unit (`internal/inference/backend_vulkan.go:53` `llamaServerFlags = [..., "--metrics"]`), shared by the ROCm backend. If bench uses `/metrics` only as an overlay this is moot; the primary `timings` source needs no flag.

### Pitfall 7: SELinux `/dev/kfd` on the ROCm side of `--ab`
**What goes wrong:** A ROCm bring-up during `--ab` silently CPU-falls-back if the SELinux `container_use_devices`/`/dev/kfd` policy isn't right — bench then voids every ROCm run.
**Why it happens:** Phase 7/8 carry-over; flagged in ROADMAP + STATE.md as "Confirm SELinux `/dev/kfd` behavior here or in Phase 7."
**How to avoid:** The residency gate catches it (voids the runs); surface the honest "ROCm runs were not GPU-resident — check SELinux `/dev/kfd` / `container_use_devices`" remediation rather than a slow number. Validate on-hardware (success-criterion #3).

## Runtime State Inventory

> Phase 9 is a read-only/compose phase (one new pure package + one cmd noun + one `llm` method). It mutates no stored data, no OS-registered state, no secrets. The ONLY transient runtime mutation is the `--ab` backend flip — which is performed *and reverted* via the proven Phase-8 `backendswap.Run` transactional core.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — bench reads `/v1` responses + sysfs; writes no datastore. A saved report artifact is **deferred (BENCH-03, out of v1.1 scope)**. | none |
| Live service config | `--ab` restarts `villa-llama.service` twice (flip + restore) via `backendswap.Run`. This is the *intended* transient mutation; config is restored to the original backend. Open WebUI / dashboard untouched. | none beyond always-restore (Pitfall 4) |
| OS-registered state | None — no new units, timers, or scheduler entries. | none |
| Secrets/env vars | None — no new secrets; the `/v1` call uses the existing `APIKey:"local"` placeholder. | none |
| Build artifacts | New package `internal/bench` + `cmd/villa/bench.go` compile into the existing single `villa` binary; no separate artifact. | none |

**The canonical question — after every file is updated, what runtime systems still hold old state?** For the single-backend `villa bench`: nothing (read-only). For `villa bench --ab`: the inference unit is left on the **original** backend (config + quadlet restored by the final `backendswap.Run`). No residual state if Pitfall 4 (always-restore) is honored.

## Code Examples

### Driving one controlled run and reading separated pp/tg
```go
// internal/bench measurement seam (wired in cmd/villa/bench.go:liveBenchDeps).
// Drives a non-streaming /v1 completion with FIXED params, reads timings, AND
// samples gpu_busy during the decode for the residency gate (clone of liveProve).
func liveMeasure(ctx context.Context, endpoint, modelID string, spec bench.BenchSpec, backend inference.Backend) (bench.RunTimings, bool, string, error) {
    runCtx, cancel := context.WithTimeout(ctx, spec.Timeout) // load_tensors-hang guard
    defer cancel()

    // sample GPUBusyPercent DURING the call (goroutine + ticker, keep max) — D-07.
    // run the non-streaming completion; read response.timings.{prompt,predicted}_per_second.
    t, err := llmClient.Complete(runCtx, llm.ChatRequest{ /* fixed prompt */ }, spec.NPredict, spec.Seed, spec.Temp)
    if err != nil { return bench.RunTimings{}, false, err.Error(), err }

    v := inference.RunningOffloadVerdict(inference.RunningOffloadInput{
        JournalText: journal, GTTUsedBytes: detect.GTTUsedBytes(),
        GPUBusyPercent: maxBusy, WeightBytes: liveWeightBytes(cfg),
        ConfigModel: modelFile, ConfigContext: cfg.Ctx, Markers: backend.ResidencyProof(),
    })
    resident := v.Status == inference.StatusPass // anything else ⇒ VOID
    return bench.RunTimings{
        PromptPerSec: t.PromptPerSecond, PredictedPerSec: t.PredictedPerSec,
        PromptN: t.PromptN, PredictedN: t.PredictedN,
    }, resident, v.Detail, nil
}
```
**Source:** composed from `cmd/villa/backend.go:liveProve` (verified) + `tools/server/README.md` timings [CITED].

### Median + stddev (stdlib, separate per metric)
```go
// internal/bench/stats.go
func median(xs []float64) float64 {
    s := append([]float64(nil), xs...); sort.Float64s(s)
    n := len(s); if n == 0 { return 0 }
    if n%2 == 1 { return s[n/2] }
    return (s[n/2-1] + s[n/2]) / 2
}
func stddev(xs []float64) float64 { // sample stddev (n-1); 0 for n<2
    n := len(xs); if n < 2 { return 0 }
    var m float64; for _, x := range xs { m += x }; m /= float64(n)
    var ss float64; for _, x := range xs { d := x - m; ss += d * d }
    return math.Sqrt(ss / float64(n-1))
}
// pp and tg are fed SEPARATELY — never concatenated into one slice.
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| KV-cache-usage ratio gauge in `/metrics` | Removed upstream; KV/context derived from `/slots` | already handled in `internal/metrics` | Don't query it; not relevant to bench. |
| Blended tok/s headline | pp/tg reported separately | project charter | Bench data model carries both end-to-end. |
| Deprecated `-mtp` images | MTP merged to llama.cpp master (auto-rebuilt images) | per CLAUDE.md | Use the pinned `kyuz0:rocm-7.2.4` / `vulkan-radv` images already wired. |

**Deprecated/outdated:** none new for this phase.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The non-streaming `/v1/chat/completions` response carries the top-level `timings` object (llama.cpp extension), not only `/completion`. | Pattern 2 | LOW — if `/v1` omits it on a given build, fall back to driving `/completion` (which definitively returns `timings`). Both are on the same server; plan should probe once at build/UAT time. |
| A2 | `temperature:0` + a fixed `seed` gives stable-enough generation that tg token counts don't wildly vary run-to-run. | BENCH-02 / Pitfall 2 | LOW — exact bit-identity isn't required (upstream notes logits aren't bit-identical across batch sizes); the median+band absorbs minor variation. Honest methodology only needs *identical request params*, which is fully controllable. |
| A3 | A per-run timeout of ~5m (mirroring `proveTimeout`) is a sufficient load_tensors-hang guard for bench runs. | Pitfall 3 | LOW — Phase 8 validated this bound on-hardware for the same backend bring-up. |
| A4 | The magnitude/direction of the Vulkan-vs-ROCm pp/tg delta on the user's model. | Success Criterion 3 | **By design volatile** — this is the on-hardware UAT to *measure*, not assume (ROADMAP research flag: ROCm-7.x-vs-6.4.4 ordering, pp-weighted win with tg ~flat). Not a code risk; the bench must *report* whatever it measures honestly. |

## Open Questions

1. **`/v1/chat/completions` vs `/completion` as the drive endpoint.**
   - What we know: both return the `timings` block; `/v1` reuses the existing `llm` client path; `/completion` gives more direct control of `n_predict`/`cache_prompt`/`seed`.
   - What's unclear: whether the chat-template overhead on `/v1` materially skews `prompt_n` for a fixed prompt.
   - Recommendation: default to `/v1` (lowest-risk reuse of `internal/llm`); add a one-line UAT probe confirming `timings` is present in the `/v1` response on the pinned image. If absent, switch the measurement client to `/completion`.

2. **Default N, warmup, and `n_predict`.**
   - What we know: methodology needs ≥1 discarded warmup and enough reps for a meaningful band; larger `n_predict` gives a steadier tg figure but a longer bench.
   - What's unclear: the right defaults for a "just works" UX vs. statistical honesty.
   - Recommendation: seed defaults (warmup=1, N=5, n_predict=128, temp=0, fixed seed) as flags (`-n`, `--warmup`, `--n-predict`); document them in the "stated conditions" output. Tune on-hardware.

3. **Should `villa bench` (no `--ab`) ever flip backends?**
   - What we know: SC#1 says the single-backend path is "non-disruptive" (read-only over the running endpoint). SC#3 attaches the cross-backend delta to "flipping via the Phase-8 switch."
   - Recommendation: `villa bench` benches ONLY the currently-running backend (zero flips, fully non-disruptive); the Vulkan-vs-ROCm delta is the `--ab` path. This cleanly satisfies SC#1 (non-disruptive) and SC#3 (delta) without ambiguity.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build | ✓ | go1.26.2 | — |
| Running `villa-llama` (loaded model) | every bench run | host-dependent (UAT) | — | refuse-with-remediation: "no running inference endpoint — `villa up` first" |
| llama-server `--metrics` flag | `/metrics` overlay (optional) | ✓ (in rendered unit) | — | overlay only; primary source is `timings` (no flag) |
| ROCm-capable host (gfx1151) | `--ab` ROCm side | host-dependent (UAT) | — | residency gate voids ROCm runs + remediation; original backend restored |
| `spf13/cobra`, `jaypipes/ghw` | already vendored | ✓ | 1.10.2 / 0.24.0 | — |

**Missing dependencies with no fallback:** none at build time (pure stdlib + vendored). A *missing running endpoint* is a runtime refuse-with-remediation, not a build blocker.
**Missing dependencies with fallback:** ROCm hardware for `--ab` → residency gate + honest remediation.

## Validation Architecture

> `nyquist_validation: true` (config) — section included.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (table tests + golden files) |
| Config file | none — `go test` |
| Quick run command | `go test ./internal/bench/... ./cmd/villa/ -run Bench -count=1` |
| Full suite command | `go test ./... -count=1` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| BENCH-01 | pp/tg reported as TWO separate figures; never blended | unit | `go test ./internal/bench/ -run TestSeparatePPTG -count=1` | ❌ Wave 0 |
| BENCH-01 | non-resident (CPU-fallback) run is VOID, excluded from stats | unit | `go test ./internal/bench/ -run TestVoidNonResident -count=1` | ❌ Wave 0 |
| BENCH-01 | `--ab` composes `backendswap.Run` (never re-implements switching) + RESTORES original | unit | `go test ./cmd/villa/ -run TestBenchABRestoresOriginal -count=1` | ❌ Wave 0 |
| BENCH-02 | warmup run discarded; not counted in stats | unit | `go test ./internal/bench/ -run TestWarmupDiscarded -count=1` | ❌ Wave 0 |
| BENCH-02 | median + stddev correct over known inputs (per metric) | unit | `go test ./internal/bench/ -run TestStats -count=1` | ❌ Wave 0 |
| BENCH-02 | identical BenchSpec applied to both `--ab` sides | unit | `go test ./internal/bench/ -run TestIdenticalSpecBothSides -count=1` | ❌ Wave 0 |
| BENCH-02 | insufficient resident runs ⇒ honest WARN, not a confident delta | unit | `go test ./internal/bench/ -run TestVoidExhaustionWarn -count=1` | ❌ Wave 0 |
| BENCH-01/02 | `--json` shape frozen (Phase 10 reads tok/s from it) | golden | `go test ./cmd/villa/ -run TestBenchJSON -count=1` | ❌ Wave 0 |
| (seam) | `cmd/villa/bench.go` literal-free of backend markers | grep-gate | `go test ./internal/inference/ -run TestSeamGrepGate -count=1` (already walks cmd/villa) | ✅ extend coverage |

### Sampling Rate
- **Per task commit:** `go test ./internal/bench/... ./cmd/villa/ -run Bench -count=1 && go vet ./...`
- **Per wave merge:** `go test ./... -count=1`
- **Phase gate:** full suite green + on-hardware UAT (real `villa bench` + `villa bench --ab` on gfx1151, residency-checked delta) before `/gsd-verify-work 9`.

### Wave 0 Gaps
- [ ] `internal/bench/bench_test.go` — fake-`Deps` recorder; warmup-discard, void-gate, median/stddev, identical-spec, void-exhaustion (covers BENCH-01/02)
- [ ] `cmd/villa/bench_test.go` — stubbed Deps: exit mapping, `--ab` restores original, `--json` golden
- [ ] `cmd/villa/testdata/bench.json.golden` — frozen `--json` shape
- [ ] (if used) `internal/llm/openai_test.go` extension — `Complete` parses the `timings` block from a fixture response body

## Security Domain

> `security_enforcement: true`, ASVS L1. Bench adds **no new network surface, no auth, no new file access** — it is a loopback client over the already-audited `/v1`+`/metrics` (127.0.0.1) plus the existing `backendswap` mutation path.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | local loopback only; existing `APIKey:"local"` placeholder, no new auth path |
| V3 Session Management | no | stateless HTTP requests |
| V4 Access Control | no | no new privileged action; `--ab` reuses `backendswap` (already threat-reviewed, Phase 8 08-SECURITY.md "no new endpoints/auth/file access") |
| V5 Input Validation | yes | `<backend>` target for `--ab` crosses only into `inference.BackendFor` (fail-closed allowlist) — same as `backend set`; `-n`/`--n-predict` are bounded ints |
| V6 Cryptography | no | none |
| V9 Communications | yes | all scrapes/requests are loopback 127.0.0.1, bounded by `io.LimitReader` + timeouts (mirror `internal/metrics`); no outbound |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Prompt/params leakage into output (slots/timings) | Information disclosure | bench reads ONLY `timings` numeric fields + narrow residency signals — never the prompt or sampling params into the report (mirror `metrics.Slot` field-set discipline) |
| Unbounded response body from a wedged server | DoS | `io.LimitReader` + `scrapeTimeout`/per-run timeout on every request (existing pattern) |
| `--ab` leaving a non-default/insecure backend live | Tampering / availability | always-restore (Pitfall 4) + `backendswap` transactional rollback; loopback-only posture unchanged by a backend flip |
| Backend-marker literal leak in `cmd/villa/bench.go` | (project invariant) | the cmd/villa-walking `TestSeamGrepGate` already gates this — keep bench.go literal-free; markers arrive only via `ResidencyProof()` |

## Project Constraints (from CLAUDE.md)

- **Go only, single static binary, no heavy deps** → bench is stdlib + in-repo packages; **no new module** (hand-rolled median/stddev, not `gonum`).
- **llama.cpp `llama-server` OpenAI-compatible contract is the integration boundary** → bench drives `/v1` (or `/completion`) and reads server-side `timings`; never bypasses the contract.
- **Vulkan RADV default, ROCm opt-in** → `villa bench` benches the *current* backend; `--ab` is the only cross-backend path and restores the original (default) backend after.
- **Config is the single source of truth; units derived, never hand-edited** → bench reads backend/model/ctx from `config.LoadVilla`; `--ab` mutates only via `backendswap.Run` (which regenerates the unit from config).
- **Backend-neutrality seam / inference grep-gate** → `cmd/villa/bench.go` and `internal/bench` must stay literal-free of backend markers (`ROCm0`/HSA/fault/image/device); markers come only through `BackendFor(target).ResidencyProof()`. The existing `TestSeamGrepGate` (walks `cmd/villa`) enforces this.
- **Strictly local, zero telemetry; outbound limited to image/model pulls** → bench makes only loopback requests; NO bench-report upload (cross-host/leaderboard upload is explicitly out-of-scope, REQUIREMENTS.md).
- **Offload asserted, not assumed (governing invariant)** → every counted run is residency-checked; a CPU-fallback run is void, never a slow pass.
- **GSD workflow** → implement via the planned phase tasks, not ad-hoc edits.

## Sources

### Primary (HIGH confidence)
- Codebase (verified via graphmind + Read): `internal/metrics/llamacpp.go` (confirmed gauge names + the "averages, not per-run" warning + stdlib prom parser), `internal/backendswap/backendswap.go` (the LOCKED composition target — `Run`/`Deps`/`Result`/`ProveStatusPass`), `cmd/villa/backend.go:liveProve` (the residency-gate + during-decode gpu_busy pattern to clone), `internal/inference/probe.go` + `prove.go` (`PollHealth`/`GenerationProbe`/`ChatResult`), `internal/llm/openai.go` (the client to extend — currently discards `timings`), `cmd/villa/status.go` + `inference.go` (endpoint resolution + `--json`/exit-code + golden conventions), `internal/inference/backend_vulkan.go:53` (`--metrics` already in the rendered unit).
- Planning docs: `.planning/REQUIREMENTS.md` (BENCH-01/02 + out-of-scope blended-number + no-upload), `.planning/ROADMAP.md` (Phase 9 goal/SC/research flag), `.planning/STATE.md` (LOCKED "bench composes the switch, never re-implements" + SELinux `/dev/kfd` flag).
- [ggml-org/llama.cpp `tools/server/README.md`](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) — `timings.{prompt_n,prompt_ms,prompt_per_second,predicted_n,predicted_ms,predicted_per_second}`; `/metrics` counters vs gauges; `--metrics` required; determinism params (`seed`, `temperature`, `n_predict`, `cache_prompt`).

### Secondary (MEDIUM confidence)
- WebSearch cross-confirmation of the `/completion` non-streaming `timings` fields (`prompt_per_second`/`predicted_per_second` present in non-streaming JSON), corroborating the README. [github.com/ggml-org/llama.cpp issues/15443, /14685]

### Tertiary (LOW confidence)
- The per-model Vulkan-vs-ROCm pp/tg delta magnitude/direction — intentionally left to on-hardware UAT (success-criterion to measure, not a research claim).

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new deps; every primitive verified present in-repo.
- Architecture (compose backendswap + residency gate + per-request timings): HIGH — `liveProve` and `backendswap.Run` are read directly; the pattern is a near-verbatim reuse.
- Measurement source (timings vs metrics): HIGH — README + a second source + the in-repo `metrics` doc all agree the gauges are averages.
- Pitfalls: HIGH — derived from the verified `metrics` doc, Phase-8 rollback semantics, and ROADMAP/STATE on-hardware flags.
- Per-model delta magnitude: LOW/volatile — by design (the thing bench exists to measure).

**Research date:** 2026-06-06
**Valid until:** ~2026-07-06 (stable; the only fast-moving element is the pinned llama.cpp image — re-confirm `timings` presence on the resolved digest at plan/UAT time per the existing "resolve the rocm-7.2.4 digest" blocker in STATE.md).
