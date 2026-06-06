# Phase 9: `villa bench` (Honest A/B) - Pattern Map

**Mapped:** 2026-06-06
**Files analyzed:** 7 new/modified
**Analogs found:** 7 / 7 (every new file has a strong in-repo analog; this is a composition phase)

> Phase 9 is a **composition phase**, not a new-capability phase (09-RESEARCH.md §Summary). Every primitive exists behind a clean seam. The "new" code is: (1) a pure Deps-injected `internal/bench` state-machine cloned from `internal/backendswap`, (2) a `cmd/villa/bench.go` cobra noun cloned from `cmd/villa/backend.go` (esp. `liveProve`), and (3) a single `llm.OpenAIClient.Complete` method modeled on the existing `StreamChat`. All analogs below are verified against the live tree.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/bench/bench.go` | service (pure core) | transform / request-response | `internal/backendswap/backendswap.go` | exact (Deps/Result/Run idiom) |
| `internal/bench/stats.go` | utility | transform (batch arithmetic) | RESEARCH §Code Examples (no existing stats file) | role-match (stdlib `sort`/`math`) |
| `internal/bench/bench_test.go` | test | transform | `internal/backendswap/backendswap_test.go` | exact (fake-Deps recorder) |
| `cmd/villa/bench.go` | command (cobra noun) | request-response | `cmd/villa/backend.go` (`liveProve`, `liveBackendSwapDeps`, Result→exit map) | exact |
| `cmd/villa/bench_test.go` | test | request-response | `cmd/villa/status_test.go` (stubbed Deps + `-update` golden), `cmd/villa/backend_test.go` | exact |
| `cmd/villa/testdata/bench.json.golden` | config (test fixture) | — | `cmd/villa/testdata/status.json.golden` | exact |
| `internal/llm/openai.go` (+`Complete`) | service (HTTP client method) | request-response | `internal/llm/openai.go` `StreamChat` (same file, non-streaming sibling) | exact |

## Pattern Assignments

### `internal/bench/bench.go` (pure core; transform / request-response)

**Analog:** `internal/backendswap/backendswap.go` — the LOCKED transactional-core idiom. Bench clones its `Deps`/`Result`/`Run` shape but is simpler (no rollback frame; the `--ab` flip *delegates* to `backendswap.Run`, never re-implements it).

**Package-doc + literal-free discipline** (`backendswap.go:1-27`): mirror this header verbatim in intent — declare the package pure, list every Deps seam, and state it imports **neither `internal/inference` nor `internal/detect`** so it stays literal-free of backend markers (the `TestSeamGrepGate` invariant). Markers arrive only through the injected `Measure` seam's verdict.

**Deps seam pattern** (`backendswap.go:56-93`): every host-touching action is a struct field of `func` type so `bench_test.go` drives the whole flow without a live host:
```go
// internal/backendswap/backendswap.go:56
type Deps struct {
    LoadConfig func() (config.VillaConfig, error)
    // ... one func field per host action ...
    Prove func(ctx context.Context, target string) ProveVerdict
    InstallServiceName string
}
```
Bench's `Deps` (per RESEARCH Pattern 1) carries `Measure func(ctx) (RunTimings, resident bool, detail string, err error)`, plus `Switch`/`Restore` set **only** for `--ab` (both delegating to `backendswap.Run`).

**Typed Result, not an exit code** (`backendswap.go:96-127`): return a struct the cobra caller branches on (`Refused`/`Switched`/`RolledBack`/`NoOp`/`Reason`/`Err`/`FailedStep`). Bench returns per-metric `Stats{MedianPP, StddevPP, MedianTG, StddevTG, Kept, Void}` + an A/B delta + a void-exhaustion warn flag (RESEARCH Pattern 1, Pitfall 5).

**Run state-machine + always-restore on `--ab`** (`backendswap.go:145-253`): note the closure-based `rollback`/`rolledBack` helpers and the ordering contract. For bench, the load-bearing clone is the **defer-restore** (RESEARCH Pattern 4 / Pitfall 4):
```go
// internal/bench Run when Switch/Restore are set (RESEARCH Pattern 4):
orig := cfg.Backend
defer d.Restore(ctx, orig)          // ALWAYS restore — every exit path
statsA := benchN(ctx, spec)
if err := d.Switch(ctx, other(orig)); err != nil { /* surface; defer restores */ }
statsB := benchN(ctx, spec)
```
The `backendswap.Run` happy-path return (`backendswap.go:252`) and the no-op early return (`:153-155`) show the typed-Result construction to mirror.

---

### `internal/bench/stats.go` (utility; batch transform)

**Analog:** none in-repo (`go.mod` has zero stats libs — verified RESEARCH §Standard Stack). Use the RESEARCH §Code Examples excerpts verbatim — stdlib `sort.Float64s` + `math.Sqrt`, **pp and tg fed separately, never concatenated**:
```go
func median(xs []float64) float64 { s := append([]float64(nil), xs...); sort.Float64s(s); n := len(s)
    if n == 0 { return 0 }; if n%2 == 1 { return s[n/2] }; return (s[n/2-1] + s[n/2]) / 2 }
func stddev(xs []float64) float64 { n := len(xs); if n < 2 { return 0 }
    var m float64; for _, x := range xs { m += x }; m /= float64(n)
    var ss float64; for _, x := range xs { d := x - m; ss += d*d }; return math.Sqrt(ss / float64(n-1)) }
```
This may fold into `bench.go`. Error-handling style: pure functions return zero for n<2, never panic (mirrors the `detect`/`metrics` "typed-Unknown, never a fabricated value" discipline).

---

### `internal/bench/bench_test.go` (test; fake-Deps recorder)

**Analog:** `internal/backendswap/backendswap_test.go` (lines 1-120 read).

**Recorder + knobs pattern** (`backendswap_test.go:30-54`): a `swapRecorder` struct with a `callOrder []string` and per-knob fields drives every branch:
```go
// internal/backendswap/backendswap_test.go:30
type swapRecorder struct {
    callOrder []string
    // knobs:
    proveStatus string // Prove verdict Status
    // ...
}
```
**Stub builder** (`backendswap_test.go:58-120` `newSwapStub`): builds a `Deps` whose every closure appends to `rec.callOrder` and returns knob-driven values. Bench's recorder records each `Measure` call (so tests assert warmup-discard count and void-run exclusion) and each `Switch`/`Restore` (so `TestBenchABRestoresOriginal` asserts the final call restores `orig`). The `restartCalls` forward-vs-rollback discriminator (`:106-111`) is the exact pattern for "first N measures are warmup, the rest counted."

**Test targets** (RESEARCH §Phase Requirements→Test Map): `TestSeparatePPTG`, `TestVoidNonResident`, `TestWarmupDiscarded`, `TestStats`, `TestIdenticalSpecBothSides`, `TestVoidExhaustionWarn`, `TestBenchABRestoresOriginal`.

---

### `cmd/villa/bench.go` (cobra noun; request-response)

**Analog:** `cmd/villa/backend.go` — the single best analog in the repo. It holds `liveProve` (the residency-gate composition bench's `liveMeasure` clones), the cobra surface, the Result→exit mapping, and `liveBackendSwapDeps` (the seam-wiring idiom).

**Backend-marker discipline header** (`backend.go:23-33`): copy this warning verbatim in intent — `cmd/villa/bench.go` must stay LITERAL-FREE of backend markers; markers arrive ONLY via `inference.BackendFor(target).ResidencyProof()`. The `TestSeamGrepGate` walks `cmd/villa` and fails CI on any leak (confirmed `internal/inference/seam_test.go:34,107`).

**Bounded-timeout consts** (`backend.go:41,47`): clone `proveTimeout = 5 * time.Minute` (the load_tensors-hang guard, RESEARCH Pitfall 3) as bench's per-run `spec.Timeout`, and `busySampleInterval = 100 * time.Millisecond` for the during-decode gpu_busy sampling.

**`liveMeasure` = `liveProve` clone** (`backend.go:65-175`): this is the load-bearing reuse. The residency-gate composition to copy:
- resolve backend fail-closed: `backend, err := inference.BackendFor(target)` (`:68`)
- load config source-of-truth: `cfg, err := config.LoadVilla()` (`:75`)
- resolve endpoint the status-path way: `endpoint := inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint()` (`:90`)
- bound the run: `deadlineCtx, cancel := context.WithTimeout(ctx, proveTimeout)` (`:94`)
- **the goroutine+ticker during-decode gpu_busy sampler keeping `maxBusy`** (`:108-149`) — clone this loop, swapping `inference.GenerationProbe` for the new `llm.Complete` call that returns `timings`
- the residency verdict fold (`:158-167`):
```go
// cmd/villa/backend.go:159
v := inference.RunningOffloadVerdict(inference.RunningOffloadInput{
    JournalText:    journal,                                  // orchestrate.NewSystemd().ResidencyJournal(installServiceName)
    GTTUsedBytes:   detect.GTTUsedBytes(),
    GPUBusyPercent: maxBusy,
    WeightBytes:    liveWeightBytes(cfg),
    ConfigModel:    modelFile,                                // liveModelFile(cfg)
    ConfigContext:  cfg.Ctx,
    Markers:        backend.ResidencyProof(),
})
resident := v.Status == inference.StatusPass    // anything else ⇒ VOID this run (RESEARCH Pattern 3)
```
Reused live seams `liveModelFile(cfg)`/`liveWeightBytes(cfg)`/`installServiceName` live in `cmd/villa` (confirmed: `installServiceName = "villa-llama.service"` at `install.go:145`; `liveModelFile`/`liveWeightBytes` referenced by `backend.go` and `status.go`).

**Cobra noun + RunE→`os.Exit(code)`** (`backend.go:187-289`): `newBackend()` builds the noun; each subcommand's `RunE` calls `os.Exit(runX(...))` while the `runX` body **returns the int** so tests assert output+code without a subprocess. Clone for `newBench()` with flags `--ab`, `-n`/`--reps`, `--warmup`, `--n-predict`, `--json` (RESEARCH Open Questions 2). Register it in `cmd/villa/root.go:34-35` `newRoot().AddCommand(...)` alongside `newBackend()`.

**Result→exit mapping** (`backend.go:296-370` `runBackendSet`): the `switch { case res.Refused: ...; case res.RolledBack: ...; case res.NoOp: ...; default: }` shape returning `exitBlocked`/`exitPass`. Bench maps: no running endpoint → refuse-with-remediation (`exitBlocked`); insufficient resident runs → WARN (`exitWarn=2`, RESEARCH Pitfall 5); clean delta → `exitPass`.

**`--json` render** (`backend.go:232-262` `runBackendShow`): the `json.NewEncoder(out); enc.SetIndent("", "  "); enc.Encode(entry)` pattern with a typed `…Entry` struct. Bench's entry carries the separate pp/tg median+stddev per side + delta (Phase 10 reads tok/s from `bench.json.golden`).

**`--ab` Deps wiring** (`backend.go:378-474` `liveBackendSwapDeps`): the template for `liveBenchDeps()`. For `--ab`, `Switch`/`Restore` are wired to `func(ctx, target) error { res := backendswap.Run(*liveBackendSwapDeps(), target); /* map res to error */ }` — LOCKED composition (RESEARCH Pattern 4 / Anti-Patterns).

---

### `cmd/villa/bench_test.go` (test; request-response)

**Analog:** `cmd/villa/status_test.go` (stubbed-Deps + `-update` golden) and `cmd/villa/backend_test.go`.

**Stubbed Deps builder** (`status_test.go:49-83` `newStatusDeps`): builds a fully-stubbed `*Deps` with each seam a closure; knobs override per test. Bench clones this for a stubbed `bench.Deps` (a `Measure` returning canned `RunTimings`+resident, `Switch`/`Restore` recording calls).

**Golden test mechanism** (`status_test.go:203-244`): the exact pattern to copy:
```go
// cmd/villa/status_test.go:214
func TestStatusJSONGolden(t *testing.T) {
    d := newStatusDeps(t, loopbackUnits(t))
    cmd, out, _ := statusTestCmd()
    jsonOut = true; defer func(){ jsonOut = false }()
    code := runStatus(cmd, nil, d)
    // ...
    golden := filepath.Join("testdata", "status.json.golden")
    if *update { os.WriteFile(golden, out.Bytes(), 0o644); return }
    want, _ := os.ReadFile(golden)
    if !bytes.Equal(out.Bytes(), want) { t.Errorf(...) }
}
```
`statusTestCmd()` (`:203-209`) is the `cmd.SetOut/SetErr(&bytes.Buffer)` helper to clone. **Note:** the `*update` flag var is declared in `cmd/villa/detect_test.go` (package-shared) — `bench_test.go` reuses it, does **not** re-declare it.

---

### `cmd/villa/testdata/bench.json.golden` (test fixture)

**Analog:** `cmd/villa/testdata/status.json.golden` (2.4K, indented JSON). Sibling goldens confirm the convention: `inference-pass.json.golden`, `recommend.golden.json`, `status.json.golden`. Generate via `go test ./cmd/villa/ -run TestBenchJSON -update`. This is the frozen Phase-10 read contract — pp/tg tok/s must be **two separate keys per side**, never blended (RESEARCH Anti-Patterns).

---

### `internal/llm/openai.go` — NEW `Complete` method (HTTP client; request-response)

**Analog:** the existing `StreamChat` in the **same file** (`openai.go:55-95`) — `Complete` is its non-streaming sibling. Reuse the request-build/auth/error scaffolding verbatim; change `Stream:true`→`false`, `Accept` header, and parse a JSON body (not SSE) to capture the `timings` block `StreamChat` discards.

**Request build + auth + bounded error** (`openai.go:68-92`):
```go
// model fallback (openai.go:57-62), then:
body, _ := json.Marshal(wireRequest{Model: model, Messages: req.Messages, Stream: false})
httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
httpReq.Header.Set("Content-Type", "application/json")
if c.apiKey != "" { httpReq.Header.Set("Authorization", "Bearer "+c.apiKey) }
// ...
if resp.StatusCode != http.StatusOK {
    snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))   // bounded-body discipline
    return ..., fmt.Errorf("llm: upstream returned %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
}
```
**New wire type** (RESEARCH Pattern 2) — extend the `wireRequest` to carry `seed`/`temperature`/`max_tokens`, add a top-level `Timings` to the non-streaming response struct:
```go
type Timings struct {
    PromptN int `json:"prompt_n"`; PromptMS float64 `json:"prompt_ms"`; PromptPerSecond float64 `json:"prompt_per_second"`
    PredictedN int `json:"predicted_n"`; PredictedMS float64 `json:"predicted_ms"`; PredictedPerSec float64 `json:"predicted_per_second"`
}
```
**Backend-neutrality:** `internal/llm` is already inside the `TestSeamGrepGate` `internalRoot` walk — keep `Complete` literal-free of backend markers (it only knows `/v1`, no device/image tokens).
**Test extension** (`internal/llm/openai_test.go`): add a fixture-body case asserting `Complete` parses `timings` (mirror the existing `StreamChat` httptest pattern in that file).

## Shared Patterns

### Residency gate (offload asserted, not assumed)
**Source:** `cmd/villa/backend.go:65-175` (`liveProve`) → `inference.RunningOffloadVerdict` (`internal/inference/running_offload.go:305`).
**Apply to:** every counted bench run (`cmd/villa/bench.go` `liveMeasure`). A run whose verdict ≠ `inference.StatusPass` is **void**, excluded from stats — never a slow pass (governing invariant, RESEARCH Pattern 3 / Pitfall 1).

### Pure Deps-injected core
**Source:** `internal/backendswap/backendswap.go:56-93` (`Deps`), `:96-127` (`Result`), `:145-253` (`Run`).
**Apply to:** `internal/bench/bench.go`. Every host action is an injected `func` field; the core is print-free and exit-free; the cobra layer owns presentation.

### `--ab` composes the Phase-8 switch (LOCKED — never re-implement)
**Source:** `internal/backendswap.Run` (`backendswap.go:145`) + `cmd/villa/backend.go:333` (`backendswap.Run(*d, target)`).
**Apply to:** `cmd/villa/bench.go` `liveBenchDeps().Switch/Restore` only. Bench MUST NOT touch quadlet/systemd directly (RESEARCH Anti-Patterns / STATE.md LOCKED). `defer Restore(orig)` immediately after capturing the original backend (Pitfall 4).

### Bounded loopback HTTP (timeout + LimitReader)
**Source:** `internal/llm/openai.go:90` (`io.LimitReader(resp.Body, 2048)`), `internal/metrics/llamacpp.go:28,32,106-117` (`scrapeTimeout`, `maxScrapeBody`, bounded `ScrapeMetrics`).
**Apply to:** `llm.Complete` and any `/metrics` overlay scrape — every loopback request bounded by a timeout + `io.LimitReader`; a transport/non-200 degrades to a typed error, never a fabricated zero (RESEARCH §Security Domain V9).

### Result→exit-code mapping + RunE wrapper
**Source:** `cmd/villa/backend.go:281-289` (RunE → `os.Exit(runX(...))`), `:296-370` (typed `switch` → `exitPass`/`exitBlocked`).
**Apply to:** `cmd/villa/bench.go`. Exit codes (`cmd/villa/preflight.go:19-21`): `exitPass=0`, `exitWarn=2` (insufficient-residency WARN, Pitfall 5), `exitBlocked=1` (no endpoint / `--ab` flip failed).

### Golden-file `--json` contract
**Source:** `cmd/villa/status_test.go:203-244` + `cmd/villa/testdata/status.json.golden`; `*update` flag declared in `cmd/villa/detect_test.go`.
**Apply to:** `cmd/villa/bench_test.go` + `testdata/bench.json.golden`. pp/tg are two separate JSON keys per side (Phase-10 read contract).

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `internal/bench/stats.go` | utility | batch transform | No stats package exists in-repo by design (no `gonum` — single-static-binary constraint). Use the verified RESEARCH §Code Examples `median`/`stddev` stdlib excerpts. This is the only genuinely "new" code; keep it pure + golden/unit-tested. |

> Every other file has an exact or near-exact in-repo analog. The `timings` *response shape* is new to the repo but is a documented llama.cpp extension (RESEARCH Pattern 2, A1) — confirm presence on the `/v1` response at UAT; fall back to `/completion` if absent.

## Metadata

**Analog search scope:** `internal/backendswap/`, `internal/inference/` (`prove.go`, `probe.go`, `running_offload.go`, `seam_test.go`), `internal/llm/`, `internal/metrics/`, `internal/detect/` (memory/gpu_amd), `internal/status/`, `cmd/villa/` (`backend.go`, `status.go`, `root.go`, `install.go`, `preflight.go`, `*_test.go`, `testdata/`).
**Files scanned:** ~120 Go files enumerated; ~10 read in full/targeted for excerpts.
**Pattern extraction date:** 2026-06-06
