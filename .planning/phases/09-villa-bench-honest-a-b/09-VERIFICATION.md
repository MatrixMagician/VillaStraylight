---
phase: 09-villa-bench-honest-a-b
verified: 2026-06-06T00:00:00Z
status: human_needed
score: 7/7 must-haves verified (off-hardware); proof-of-value delta routed to on-hardware UAT
overrides_applied: 0
human_verification:
  - test: "Run `villa bench` against a real loaded model on gfx1151"
    expected: "pp and tg tok/s reported as two separate figures (median ± stddev), Kept>0 / Void counts shown, exit 0; the running llama-server `/v1` response actually carries the `timings` block (else the run VOIDs honestly via ErrNoTimings — fall back to /completion per A1)"
    why_human: "Requires the AMD Strix Halo GPU and a loaded model; off-hardware the /v1 timings presence and live throughput cannot be observed. ROADMAP on-hardware research flag."
  - test: "Run `villa bench --ab` flipping Vulkan<->ROCm on the live host"
    expected: "Identical spec both sides; the ROCm side reaches GPU residency (SELinux /dev/kfd / container_use_devices correct) so its runs are KEPT not VOID; a per-metric Δpp and Δtg delta with noise band is produced; the original backend is restored afterward (`villa backend show` confirms)"
    why_human: "Needs the live backend switch + ROCm container device access (SELinux /dev/kfd) which only the GPU host can exercise; this delta magnitude IS the milestone proof-of-value and is the ROADMAP-flagged on-hardware item."
  - test: "Confirm the ROCm `--ab` side residency on the real host"
    expected: "RunningOffloadVerdict returns StatusPass for ROCm runs (markers via ResidencyProof, gpu_busy sampled during decode); a CPU-fallback run is correctly VOIDed, not folded as a slow pass"
    why_human: "The residency verdict over real journal/GTT/gpu_busy signals can only be exercised against a live ROCm container on gfx1151."
---

# Phase 9: `villa bench` (Honest A/B) Verification Report

**Phase Goal:** A user can prove, on their own loaded model, whether ROCm is actually faster than Vulkan — `villa bench` runs an honest A/B over the running endpoint, reporting prompt-processing (pp) and token-generation (tg) throughput SEPARATELY (never a single blended number), over residency-checked runs only. The per-metric throughput delta is the milestone's proof-of-value.
**Verified:** 2026-06-06
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

The honest-methodology machinery is fully delivered and green off-hardware. Per the ROADMAP on-hardware research flag, the live pp/tg delta magnitude, SELinux `/dev/kfd` on the ROCm `--ab` side, and real `/v1` `timings` presence are deliberate UAT items — routed to `human_needed`, not treated as off-hardware blockers. No CODE gap was found.

### Observable Truths

| # | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1 (SC#1) | `villa bench` measures the running backend non-disruptively; pp/tg reported as two separate figures, never blended | ✓ VERIFIED | `cmd/villa/bench.go:282` `newBench` registered (`root.go:35`); plain path leaves Switch/Restore nil → single-backend branch (`bench.go:218-220`, core `bench.go:237`). `benchSide` carries `prompt_per_sec` + `predicted_per_sec` as separate keys (`bench.go:380-388`); `renderBench` prints pp and tg on separate rows (`bench.go:525-526`). No blended field anywhere (golden: 0× `tok_per_sec`). |
| 2 (SC#2) | Honest methodology: warmup-discard, N reps + median+stddev, identical spec both sides, residency-checked-only (CPU fallback VOID) | ✓ VERIFIED | Warmup discarded (`bench.go:201-203`, `TestWarmupDiscarded`); separate median+stddev (`stats.go:21-53`, `statsOf` `bench.go:154-168`, `TestStats`/`TestSeparatePPTG`); residency void-gate excludes `resident==false` (`bench.go:212-214`, `TestVoidNonResident`); void-exhaustion WARN below `MinResident` (`bench.go:240-244`, `TestVoidExhaustionWarn`); identical spec both `--ab` sides (`OnSideStart`, `TestIdenticalSpecBothSides`); live residency gate folds `RunningOffloadVerdict` + during-decode `GPUBusyPercent`, `resident` only on `StatusPass` (`cmd/villa/bench.go:175-193`). |
| 3 (SC#3) | `--ab` flips via Phase-8 switch and yields a per-metric Vulkan-vs-ROCm delta, always restoring the original | ✓ VERIFIED (machinery) | `--ab` wires Switch/Restore → `backendswap.Run` (`bench.go:258`, LOCKED, never re-implemented); `defer d.Restore(ctx, orig)` registered BEFORE the flip (`bench.go:258` core) so every exit path restores; per-metric `DeltaPP`/`DeltaTG` (`bench.go:285-294`); `TestBenchABRestoresOriginal` (core + cmd). **Delta magnitude is on-hardware UAT.** |
| 4 | Each run reads per-request `timings` via `llm.Complete` (pp/tg separated, fixed params) | ✓ VERIFIED | `Complete` (`internal/llm/openai.go:142-199`) POSTs `stream:false` with fixed `max_tokens`/`seed`/`temperature` and parses the top-level `timings` block; `TestCompleteParsesTimings`/`TestCompleteParamsOnWire`. |
| 5 | An absent/empty `timings` block VOIDs the run (never folded as 0 tok/s) — WR-02 | ✓ VERIFIED | `ErrNoTimings` sentinel returned when `PredictedN==0 && PredictedPerSec==0` (`openai.go:195-196`); `liveMeasure` treats it as VOID (`cmd/villa/bench.go:150-155`); `TestCompleteVoidsAbsentTimings`. |
| 6 | A failed `--ab` restore is surfaced loudly, not silently swallowed — WR-01 | ✓ VERIFIED | Live Restore closure prints a LOUD stderr WARNING + `villa backend show`/`set` recovery and propagates the error (`cmd/villa/bench.go:230-243`); `TestBenchABFailedRestoreWarns`. |
| 7 | Caller context (Ctrl-C) threads through `bench.Run` — WR-03; bounded flag validation — WR-04 | ✓ VERIFIED | `Run(ctx, d, spec)` threads caller ctx (`bench.go:235`); `RunE` passes `cmd.Context()` (`cmd/villa/bench.go:419`). Flags rejected `<1`/`<0` before spec build (`bench.go:308-311`); `TestBenchFlagValidation`. |

**Score:** 7/7 truths verified off-hardware. SC#3's live delta magnitude is the proof-of-value item explicitly routed to on-hardware UAT by the ROADMAP research flag.

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/llm/openai.go` | `Complete` + `Timings` + `ErrNoTimings` | ✓ VERIFIED | `Complete` (142), `Timings` 6 fields (68-75), `ErrNoTimings` (22). Substantive, wired into `liveMeasure`. |
| `internal/bench/bench.go` | Pure Deps-injected core: BenchSpec/RunTimings/Stats/Deps/Result/Run | ✓ VERIFIED | All types + `Run(ctx,…)` state-machine present; imports neither inference nor detect. |
| `internal/bench/stats.go` | median/stddev (sort/math), per-metric | ✓ VERIFIED | `median` (21), `stddev` sample n-1 (37); panic-free degenerate guards. |
| `cmd/villa/bench.go` | bench noun + liveMeasure + liveBenchDeps + runBench + render/--json | ✓ VERIFIED | `newBench` (282), `liveMeasure` (73), `liveBenchDeps` (206), `runBench` (407), `benchEntry` (362). |
| `cmd/villa/root.go` | `newBench()` registered | ✓ VERIFIED | `root.go:35` AddCommand alongside `newBackend()`. |
| `cmd/villa/testdata/bench.json.golden` | separate pp/tg per side + delta, no blended key | ✓ VERIFIED | 3× `prompt_per_sec"`, 3× `predicted_per_sec"`, 0× blended key. |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | --- | --- | ------ | ------- |
| `liveMeasure` | `llm.Complete` + `detect.GPUBusyPercent` + `inference.RunningOffloadVerdict` | non-streaming completion, during-decode sampling, residency fold | ✓ WIRED | `cmd/villa/bench.go:125,138,175` |
| `liveBenchDeps` (--ab) | `backendswap.Run` | Switch/Restore delegate to backendswap.Run (LOCKED) | ✓ WIRED | `cmd/villa/bench.go:258` |
| `root.go` | `newBench()` | AddCommand | ✓ WIRED | `cmd/villa/root.go:35` |
| `bench.Run` | `Deps.Measure` | warmup-discard then N residency-checked runs | ✓ WIRED | `internal/bench/bench.go:202,211` |
| `bench.Run` (--ab) | `Deps.Restore` | defer Restore(orig) before flip | ✓ WIRED | `internal/bench/bench.go:258` |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
| -------- | ------------- | ------ | ------------------ | ------ |
| `benchEntry`/render | pp/tg medians | `bench.Stats` ← `statsOf(kept RunTimings)` ← `liveMeasure` ← `llm.Complete` server `timings` | Yes (off-hardware: real server timings on the live host) | ⚠️ HOLLOW off-hardware by design — real numbers require the GPU host (UAT). The dataflow path is fully wired; no hardcoded/empty render path. |

The render reads `res.AB`/`res.Single` from the live `Measure` verdict — not a hardcoded fixture. The only "static" data is the test golden (a fixture, not a code path). Per the ROADMAP flag, real numbers are an on-hardware UAT, not a code gap.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| `go build ./...` | build all | Success | ✓ PASS |
| Phase test suites | `go test ./internal/bench/ ./internal/llm/ ./cmd/villa/ -count=1` | 180 passed | ✓ PASS |
| Seam gate (no marker leak) | `go test ./internal/inference/ -run TestSeamGrepGate` | ok | ✓ PASS |
| `villa bench --help` lists flags | `go run ./cmd/villa bench --help` | `--ab`, `-n/--reps`, `--warmup`, `--n-predict`, `--json` all present | ✓ PASS |
| bench imports no inference/detect | grep | none | ✓ PASS |
| gofmt clean (WR-05) | `go fmt ./internal/bench/... ./cmd/villa/...` (bench) | no changes | ✓ PASS |
| Live A/B throughput delta | (requires gfx1151) | — | ? SKIP → UAT |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| BENCH-01 | 09-01, 09-02, 09-03 | A/B pp/tg tok/s separately, residency-checked only | ✓ SATISFIED (code) / ? on-hardware delta | Separate pp/tg end-to-end; residency void-gate; `--ab` via backendswap.Run. Live delta → UAT. |
| BENCH-02 | 09-02, 09-03 | Honest methodology: warmup, N reps median+stddev, identical spec, stated conditions | ✓ SATISFIED | Warmup-discard, median+stddev, identical spec, stated `conditions{warmup,reps,n_predict,seed,temp}` in render + --json. |

Both PLAN-declared requirement IDs (BENCH-01, BENCH-02) are accounted for and map to verified machinery. REQUIREMENTS.md (lines 37-38, 96-97) lists only these two for Phase 9 — no orphaned requirements.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (none) | — | No TODO/FIXME/XXX/TBD/HACK/PLACEHOLDER in any phase non-test file | ℹ️ Info | Clean — completion is auditable. |

`abResult` on the Switch-error path attaches a zero-side-B `AB` block (REVIEW IN-03), but `runBench` returns at the `res.Err != nil` check before rendering it (`bench.go:422-425`), so it is never surfaced — harmless, info-level only, intentionally out of scope per REVIEW.

### Human Verification Required

#### 1. `villa bench` on a real loaded model (gfx1151)

**Test:** Run `villa bench` against the running stack with a loaded model.
**Expected:** pp and tg tok/s as two separate figures (median ± stddev), Kept>0/Void counts, exit 0; confirm the live llama-server `/v1` response carries the `timings` block (else the run VOIDs honestly via `ErrNoTimings` — fall back to `/completion` per Assumption A1).
**Why human:** Requires the GPU + a loaded model; `/v1` timings presence and live throughput are unobservable off-hardware. ROADMAP on-hardware research flag.

#### 2. `villa bench --ab` Vulkan↔ROCm delta (the proof-of-value)

**Test:** Run `villa bench --ab` on the live host.
**Expected:** Identical spec both sides; the ROCm side reaches GPU residency (SELinux `/dev/kfd` / `container_use_devices` correct) so its runs are KEPT; a per-metric Δpp and Δtg delta with noise band is produced; the original backend is restored (`villa backend show`).
**Why human:** Needs the live backend switch + ROCm device access; this delta IS the milestone proof-of-value and is the ROADMAP-flagged on-hardware item.

#### 3. ROCm residency verdict on the real host

**Test:** Confirm `RunningOffloadVerdict` returns `StatusPass` for ROCm runs and a CPU-fallback run is correctly VOIDed.
**Expected:** Real journal/GTT/gpu_busy signals fold to StatusPass for a genuine GPU run; a CPU-fallback is excluded, not folded as a slow pass.
**Why human:** The residency verdict over live signals can only be exercised against a real ROCm container on gfx1151.

### Gaps Summary

No CODE gaps. The honest-methodology machinery the phase goal requires — separate pp/tg, residency void-gate, warmup-discard, median+stddev, `--ab` composing `backendswap.Run` with always-restore, zero new deps — is fully implemented, substantively wired, and green (180 tests + seam gate). All five REVIEW Warning honesty fixes landed in source (WR-01 loud failed-restore `bench.go:230-243`; WR-02 `ErrNoTimings` void `openai.go:195`/`bench.go:150`; WR-03 ctx threading `bench.go:235`/`bench.go:419`; WR-04 flag validation `bench.go:308`; WR-05 gofmt clean). go.mod unchanged — zero new dependencies.

The single remaining item is the live per-metric throughput delta (the milestone's proof-of-value), which by the ROADMAP on-hardware research flag is a deliberate UAT, not an off-hardware blocker. Status is therefore `human_needed`: automated verification is exhausted and green; the GPU-only proof awaits on-hardware UAT.

---

_Verified: 2026-06-06_
_Verifier: Claude (gsd-verifier)_
