package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/backendswap"
	"github.com/MatrixMagician/VillaStraylight/internal/bench"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/llm"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// bench.go is the cmd-tier `villa bench` noun: the live host wiring that drives the
// pure internal/bench honest-methodology core (Plan 09-02). It holds liveMeasure —
// a clone of backend.go:liveProve that swaps the GenerationProbe for the new
// llm.Complete per-request timings source while keeping the residency gate — plus the
// cobra surface (--ab / -n / --warmup / --n-predict / --json), the Result→exit
// mapping, and liveBenchDeps (the seam-wiring template, --ab Switch/Restore delegating
// to backendswap.Run, LOCKED — never re-implement switching).
//
// CRITICAL — backend-marker discipline (T-09-10): this file must stay LITERAL-FREE of
// backend marker tokens (the per-backend residency device token, the HSA override env
// var, the GPU-fault abort string, and any image/device literal). Every such marker
// arrives ONLY through inference.BackendFor(target).ResidencyProof(); the cmd/villa-
// walking TestSeamGrepGate (internal/inference/seam_test.go) fails CI on any leak.
// Do NOT paste markers here.

// benchProveTimeout bounds a single measured run (and, by the shared deadline context,
// the whole measure). It mirrors backend.go's proveTimeout (5m, seeded from inference's
// defaultReadyTimeout) and carries the same provenance: the load_tensors-hang guard
// (T-09-08 / Pitfall 3). A run that never completes before this deadline is a VOID run,
// NEVER an infinite wait.
const benchProveTimeout = 5 * time.Minute

// benchBusySampleInterval is how often liveMeasure re-reads detect.GPUBusyPercent()
// DURING the decode window (D-07): a single post-call read can miss a short decode's
// busy window, so the measure samples repeatedly while tokens stream and keeps the max.
const benchBusySampleInterval = 100 * time.Millisecond

// benchPrompt is the fixed prompt every measured run sends (reproducibility,
// BENCH-02). It is a neutral, deterministic instruction so pp/tg are comparable
// across runs and across --ab sides. It carries no backend marker.
const benchPrompt = "Summarize the water cycle in three concise sentences."

// liveMeasure is the injected per-run measurement (bench.Deps.Measure). It is a clone
// of backend.go:liveProve: it probes the ALREADY-running villa-llama.service (no --rm
// container) and returns the run's pp/tg timings plus a residency verdict the pure
// bench core gates on. It composes the same residency gate as liveProve, swapping the
// boolean GenerationProbe for the new llm.Complete call that returns the server-
// computed per-request Timings (pp/tg already separated):
//
//	(a) the run is bounded by spec.Timeout (the load_tensors-hang guard),
//	(b) the measurement is a real non-streaming completion via llm.Complete,
//	(c) residency is proven via inference.RunningOffloadVerdict over the target
//	    backend's BackendFor(target).ResidencyProof() markers + the SAME concrete
//	    liveWeightBytes / liveModelFile seams status.go uses, with
//	    detect.GPUBusyPercent() sampled DURING the decode (D-07).
//
// resident is reported true ONLY for inference.StatusPass; any other verdict (incl. a
// fast-but-CPU-fallback completion) marks the run VOID so the core excludes it from the
// band (offload asserted, not assumed). All backend markers stay behind ResidencyProof()
// — this function is literal-free of them.
func liveMeasure(ctx context.Context, target string, spec bench.BenchSpec) (bench.RunTimings, bool, string, error) {
	// Resolve the backend fail-closed (D-02): an unknown target is a measure failure,
	// never a silent fallback. backend.ResidencyProof() is the ONLY source of markers.
	backend, err := inference.BackendFor(target)
	if err != nil {
		return bench.RunTimings{}, false, "", fmt.Errorf("resolve backend %q: %w", target, err)
	}

	// Load the source of truth for the residency seams (ConfigModel/ConfigContext) and
	// the completion's model id.
	cfg, err := config.LoadVilla()
	if err != nil {
		return bench.RunTimings{}, false, "", fmt.Errorf("load config: %w", err)
	}

	// Resolve the catalog-resolved GGUF filename ONCE for ConfigModel — the SAME
	// concrete seam status.go/liveProve use (cmd/villa/lifecycle.go liveModelFile).
	modelFile, err := liveModelFile(cfg)
	if err != nil {
		return bench.RunTimings{}, false, "", fmt.Errorf("resolve model file: %w", err)
	}

	// Derive the inference endpoint the SAME way the status/prove path does: the
	// resolved backend's container runner, never a hand-rolled URL.
	endpoint := inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint()

	// Bound the WHOLE measure by spec.Timeout (T-09-08). A wedged/CPU-fallback server
	// that never returns trips this deadline and is a VOID run, not an infinite wait.
	deadlineCtx, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()

	// (b) REAL completion via llm.Complete, with the live gpu_busy_percent sampled
	// DURING the decode window (D-07). Start the completion in a goroutine and poll
	// detect.GPUBusyPercent() repeatedly while tokens stream, keeping the max — a single
	// post-call read can miss a short decode's busy window.
	client := llm.NewOpenAIClient(llm.Options{
		BaseURL:      endpoint,
		APIKey:       "local",
		DefaultModel: cfg.Model,
		Timeout:      spec.Timeout,
	})
	req := llm.ChatRequest{
		Model:    cfg.Model,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: spec.Prompt}},
	}

	type completeResult struct {
		t   llm.Timings
		err error
	}
	doneCh := make(chan completeResult, 1)
	go func() {
		t, err := client.Complete(deadlineCtx, req, spec.NPredict, spec.Seed, spec.Temp)
		doneCh <- completeResult{t: t, err: err}
	}()

	maxBusy := detect.UnknownInt("gpu_busy_percent not sampled during completion", "")
	ticker := time.NewTicker(benchBusySampleInterval)
	defer ticker.Stop()

	var timings llm.Timings
sampleLoop:
	for {
		// Sample once up front and on every tick so even a very short decode gets at
		// least one in-flight read.
		if b := detect.GPUBusyPercent(); b.Known && (!maxBusy.Known || b.Value > maxBusy.Value) {
			maxBusy = b
		}
		select {
		case res := <-doneCh:
			// One last read at completion, then stop sampling.
			if b := detect.GPUBusyPercent(); b.Known && (!maxBusy.Known || b.Value > maxBusy.Value) {
				maxBusy = b
			}
			if res.err != nil {
				// A missing/empty `timings` block is a measurement failure, NOT a 0 tok/s
				// pass: void the run so it never pollutes the honest band (WR-02 / A1).
				if errors.Is(res.err, llm.ErrNoTimings) {
					return bench.RunTimings{}, false,
						"server returned no `timings` block (or zero predicted tokens) — " +
							"this build may not expose /v1 timings; cannot honestly measure tg",
						res.err
				}
				return bench.RunTimings{}, false, "completion failed: " + res.err.Error(), res.err
			}
			timings = res.t
			break sampleLoop
		case <-ticker.C:
			// keep sampling
		case <-deadlineCtx.Done():
			return bench.RunTimings{}, false,
				"completion did not finish before timeout (possible load_tensors hang or CPU-fallback stall)",
				deadlineCtx.Err()
		}
	}

	// (c) Residency proof. Read the INVOCATION-scoped journal (F-3 — ResidencyJournal),
	// then fold journal + GTT floor + the DURING-decode gpu_busy reading + the target
	// backend's markers into one Verdict. WeightBytes/ConfigModel/ConfigContext mirror
	// status.go exactly via the concrete liveWeightBytes(cfg)/liveModelFile(cfg)/cfg.Ctx
	// seams. Markers come ONLY from backend.ResidencyProof() (literal-free, T-09-10).
	journal, _ := orchestrate.NewSystemd().ResidencyJournal(installServiceName)
	v := inference.RunningOffloadVerdict(inference.RunningOffloadInput{
		JournalText:    journal,
		GTTUsedBytes:   detect.GTTUsedBytes(),
		GPUBusyPercent: maxBusy,
		WeightBytes:    liveWeightBytes(cfg),
		ConfigModel:    modelFile,
		ConfigContext:  cfg.Ctx,
		Markers:        backend.ResidencyProof(),
	})

	rt := bench.RunTimings{
		PromptPerSec:    timings.PromptPerSecond,
		PredictedPerSec: timings.PredictedPerSec,
		PromptN:         timings.PromptN,
		PredictedN:      timings.PredictedN,
	}
	// resident ONLY for a true StatusPass; anything else (incl. ready+200-but-residency-
	// FAIL, the silent CPU fallback) marks this run VOID (offload asserted, not assumed).
	return rt, v.Status == inference.StatusPass, v.Detail, nil
}

// liveBenchDeps wires the pure bench core to the real host. Measure is always set
// (the single-backend, non-disruptive path benches ONLY the running backend, SC#1).
// The spec is captured in the Measure closure (the run's flags ride the closure); the
// caller's SIGINT-cancellable context is threaded through bench.Run into every
// Measure/Switch/Restore call so a Ctrl-C aborts an in-flight bench, while liveMeasure
// still derives the per-run load_tensors-hang timeout from it. For --ab ONLY,
// Switch/Restore/LoadConfig are wired so the flip DELEGATES to
// backendswap.Run (the LOCKED Phase-8 transactional core, never re-implemented) and
// the original backend is restored on every exit path. For plain `villa bench`,
// Switch/Restore/LoadConfig stay nil so the core takes the single-backend branch.
func liveBenchDeps(ab bool, spec bench.BenchSpec) *bench.Deps {
	d := &bench.Deps{
		// Measure benches the currently-configured (running) backend. liveMeasure
		// re-loads cfg each call so an --ab flip is observed on the next side.
		Measure: func(ctx context.Context) (bench.RunTimings, bool, string, error) {
			cfg, err := config.LoadVilla()
			if err != nil {
				return bench.RunTimings{}, false, "", fmt.Errorf("load config: %w", err)
			}
			return liveMeasure(ctx, cfg.Backend, spec)
		},
	}
	if !ab {
		return d
	}

	// --ab: the flip path. LoadConfig supplies the original backend (the restore
	// target); Switch/Restore delegate to backendswap.Run (LOCKED). A non-Switched/
	// non-NoOp Result is surfaced as an error so the core records the side as failed
	// and the deferred Restore still fires.
	d.LoadConfig = config.LoadVilla
	d.Switch = func(_ context.Context, target string) error {
		return benchBackendSwap(target)
	}
	d.Restore = func(_ context.Context, original string) error {
		// The pure core's deferred Restore is errcheck-suppressed (best-effort), so a
		// failed restore-to-original would otherwise be SILENT — leaving the user on the
		// non-default backend with no indication (RESEARCH Pitfall 4). Make it LOUD here,
		// the only layer that can print, then still return the error for the record.
		if err := benchBackendSwap(original); err != nil {
			fmt.Fprintf(os.Stderr,
				"bench: WARNING — failed to restore original backend %q: %v\n"+
					"  the stack may still be on the other backend — run `villa backend show` "+
					"and `villa backend set %s` to recover\n", original, err, original)
			return err
		}
		return nil
	}
	return d
}

// runBackendSwap delegates a single --ab flip to backendswap.Run via the SAME live
// seam wiring `villa backend set` uses (liveBackendSwapDeps), mapping the typed Result
// to an error. This is the LOCKED composition (RESEARCH Pattern 4) — bench MUST NOT
// touch quadlet/systemd directly. A clean NoOp (already on target) or a proven Switch
// is success; any Refused/RolledBack/Err is an error.
// benchBackendSwap is the package-level indirection the --ab Switch/Restore closures
// call so bench_test.go can drive the failed-restore WARNING path (WR-01) without a
// live host. The default is the real runBackendSwap.
var benchBackendSwap = runBackendSwap

func runBackendSwap(target string) error {
	res := backendswap.Run(*liveBackendSwapDeps(), target)
	switch {
	case res.NoOp, res.Switched:
		return nil
	case res.Reason != "":
		return fmt.Errorf("switch to %s failed at %q: %s", target, res.FailedStep, res.Reason)
	case res.Err != nil:
		return fmt.Errorf("switch to %s failed at %q: %w", target, res.FailedStep, res.Err)
	default:
		return fmt.Errorf("switch to %s did not complete", target)
	}
}

// ---------------------------------------------------------------------------
// bench noun (BENCH-01/02): `villa bench [--ab] [-n N] [--warmup W] [--n-predict M]
// [--json]`. Cloned from the backend noun: RunE returns the mapped exit code (body
// RETURNS the int so tests assert output+code without a subprocess), the Result→exit
// mapping mirrors runBackendSet, and the live Deps wire every host seam to the proven
// in-repo primitives.
// ---------------------------------------------------------------------------

// newBench builds the `villa bench` noun. Plain `villa bench` measures ONLY the running
// backend non-disruptively (SC#1); --ab is the only path that flips, via backendswap.Run,
// and restores the original (SC#3).
func newBench() *cobra.Command {
	var (
		ab       bool
		abTarget string
		reps     int
		warmup   int
		nPredict int
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Benchmark inference throughput (pp/tg) of the running backend, honestly",
		Long: "Measure prompt-processing (pp) and token-generation (tg) throughput of the " +
			"currently-running inference backend over N residency-checked runs (warmup discarded, " +
			"median ± stddev, identical reproducible spec). pp and tg are reported SEPARATELY — never " +
			"blended. A run whose GPU residency cannot be proven is VOID (excluded), never a slow pass; " +
			"too few resident runs is an honest WARN, not a confident band. Plain `bench` is non-disruptive " +
			"(zero backend flips). --ab additionally flips to the other backend (via the transactional " +
			"`backend set` core), benches it with the IDENTICAL spec, and ALWAYS restores the original for " +
			"a per-metric Vulkan-vs-ROCm delta. --json emits the machine-readable contract.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Validate the bounded-int flags at the cobra boundary (RESEARCH Security
			// Domain V5: "-n/--n-predict are bounded ints"). Reject nonsensical values
			// up front with a clear usage error rather than letting -n 0/-n -5 fall
			// through to a confusing void-exhaustion WARN (cap := 2*reps) or sending an
			// out-of-contract negative max_tokens on the wire.
			if reps < 1 || warmup < 0 || nPredict < 1 {
				return fmt.Errorf("bench: --reps and --n-predict must be >= 1 and --warmup must be >= 0 "+
					"(got --reps=%d --warmup=%d --n-predict=%d)", reps, warmup, nPredict)
			}
			// --ab-target plumbing (Option A, SC#3). The explicit target is only
			// meaningful for the --ab flip path: reject it without --ab as a usage error
			// (clearer UX than silently ignoring it).
			if abTarget != "" && !ab {
				return fmt.Errorf("bench: --ab-target requires --ab (the explicit comparison " +
					"backend is only used by the --ab flip)")
			}
			// Fail-closed validation (D-03 / T-12-07): resolve a non-empty --ab-target via
			// the SINGLE BackendFor resolver BEFORE any switch — a typo is an actionable
			// error, never a silent flip to a wrong/missing backend.
			if abTarget != "" {
				if _, err := inference.BackendFor(abTarget); err != nil {
					return fmt.Errorf("bench: invalid --ab-target %q: %w", abTarget, err)
				}
			}
			spec := bench.BenchSpec{
				Reps:        reps,
				Warmup:      warmup,
				Prompt:      benchPrompt,
				NPredict:    nPredict,
				Seed:        benchSeed,
				Temp:        benchTemp,
				Timeout:     benchProveTimeout,
				MinResident: benchMinResident(reps),
				ABTarget:    abTarget,
			}
			benchRun(cmd, spec, ab, asJSON, liveBenchDeps(ab, spec))
			return nil
		},
	}
	cmd.Flags().BoolVar(&ab, "ab", false, "also flip to the other backend (via the transactional backend-set core), bench it with the identical spec, and restore the original — for a per-metric A/B delta")
	cmd.Flags().StringVar(&abTarget, "ab-target", "", "explicit backend the --ab flip measures against (e.g. rocm-6.4.4, rocm-7.2.4, vulkan) for an arbitrary-pair A/B; requires --ab; empty uses the vulkan<->rocm default")
	cmd.Flags().IntVarP(&reps, "reps", "n", 5, "number of residency-checked (counted) runs per side")
	cmd.Flags().IntVar(&warmup, "warmup", 1, "number of leading runs measured then discarded (cache/JIT warm)")
	cmd.Flags().IntVar(&nPredict, "n-predict", 128, "fixed max_tokens every run requests (reproducibility)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the bench result as JSON (the frozen pp/tg-separate contract)")
	return cmd
}

// benchSeed / benchTemp are the fixed sampling params every run uses so the band is
// reproducible (BENCH-02 / RESEARCH Open Questions 2): a stable seed and greedy
// (temp 0) decoding. They are plain numeric constants, no backend marker.
const (
	benchSeed = 42
	benchTemp = 0.0
)

// benchMinResident is the residency floor below which the band is an honest void-
// exhaustion WARN (Pitfall 5). It is at least 1 and never more than the requested reps:
// require a clear majority of the requested runs to be resident before reporting a band.
func benchMinResident(reps int) int {
	if reps <= 1 {
		return 1
	}
	min := (reps + 1) / 2
	if min < 1 {
		min = 1
	}
	return min
}

// benchEntry is the `bench --json` shape — the FROZEN Phase-10 read contract. pp and tg
// tok/s are TWO SEPARATE keys per side (prompt_per_sec / predicted_per_sec with their
// stddevs), never a blended figure. The AB block (and its per-metric deltas) is present
// only for --ab. It carries ONLY numeric timings + Kept/Void + the stated conditions —
// never the prompt or sampling internals beyond the reproducible spec (T-09-07).
type benchEntry struct {
	Mode       string          `json:"mode"` // "single" or "ab"
	Conditions benchConditions `json:"conditions"`
	Single     *benchSide      `json:"single,omitempty"`
	AB         *benchAB        `json:"ab,omitempty"`
}

// benchConditions are the stated, reproducible run conditions (BENCH-02).
type benchConditions struct {
	Warmup   int     `json:"warmup"`
	Reps     int     `json:"reps"`
	NPredict int     `json:"n_predict"`
	Seed     int     `json:"seed"`
	Temp     float64 `json:"temp"`
}

// benchSide is one backend's per-metric band: pp and tg as separate median+stddev
// figures plus the resident/void run counts.
type benchSide struct {
	Backend         string  `json:"backend"`
	PromptPerSec    float64 `json:"prompt_per_sec"`
	PromptStddev    float64 `json:"prompt_per_sec_stddev"`
	PredictedPerSec float64 `json:"predicted_per_sec"`
	PredictedStddev float64 `json:"predicted_per_sec_stddev"`
	Kept            int     `json:"kept"`
	Void            int     `json:"void"`
}

// benchAB is the two-sided comparison: each side's band + the per-metric deltas (B − A,
// positive = the To backend is faster). pp and tg deltas are SEPARATE keys.
type benchAB struct {
	From                 string    `json:"from"`
	To                   string    `json:"to"`
	A                    benchSide `json:"a"`
	B                    benchSide `json:"b"`
	DeltaPromptPerSec    float64   `json:"delta_prompt_per_sec"`
	DeltaPredictedPerSec float64   `json:"delta_predicted_per_sec"`
}

// benchRun is the package-level indirection RunE calls so bench_test.go can capture the
// constructed BenchSpec (asserting --ab-target plumbing) and assert the fail-closed
// validation NEVER reaches the run — all without firing os.Exit or touching a live host.
// The default runs the real runBench and os.Exit(code)s (the bench noun maps its result to
// a process exit code); a test override returns the captured code without exiting.
var benchRun = func(cmd *cobra.Command, spec bench.BenchSpec, ab, asJSON bool, d *bench.Deps) int {
	code := runBench(cmd, spec, ab, asJSON, d)
	os.Exit(code)
	return code // unreachable in the default; satisfies the signature for test overrides
}

// runBench builds the BenchSpec from flags, pre-checks a reachable endpoint
// (refuse-with-remediation if none), runs the pure bench core, and maps the typed
// Result to an exit code + rendered output. The body RETURNS the int (no os.Exit) so
// tests assert output+code without a subprocess. Exit mapping: no running endpoint →
// exitBlocked (1); void-exhaustion → exitWarn (2); clean delta → exitPass (0); a
// non-methodology Err (e.g. an --ab LoadConfig/flip error) → exitBlocked.
func runBench(cmd *cobra.Command, spec bench.BenchSpec, ab, asJSON bool, d *bench.Deps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	// Pre-check a reachable inference endpoint: bench is a read-only client over the
	// already-running server. No endpoint → refuse-with-remediation (exitBlocked), never
	// a fabricated zero band.
	if !benchEndpointReachable() {
		fmt.Fprintln(errOut, "bench: no running inference endpoint — start the stack with `villa up` first")
		return exitBlocked
	}

	res := bench.Run(cmd.Context(), *d, spec)

	// A non-methodology failure (distinct from a void-exhaustion WARN): surface and block.
	if res.Err != nil {
		fmt.Fprintf(errOut, "bench: %v\n", res.Err)
		return exitBlocked
	}

	// Single-mode label: the pure core leaves res.Backend empty (it is config-unaware),
	// so the single-side header/`--json single.backend` would read `backend ():` / "".
	// Name the measured backend from the configured-backend seam here, in the cmd layer.
	// Single path ONLY (`!ab`): the --ab branch labels its sides from res.AB.From/To and
	// must not read res.Backend, so leave it untouched there.
	if !ab {
		res.Backend = benchConfiguredBackend()
	}

	entry := benchEntryFromResult(res, ab, spec)

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entry); err != nil {
			fmt.Fprintf(errOut, "bench: encode json: %v\n", err)
			return exitBlocked
		}
	} else {
		renderBench(out, entry)
	}

	// Void-exhaustion → honest WARN (exitWarn), not a confident band.
	if res.VoidExhausted {
		fmt.Fprintln(errOut, "bench: WARN — insufficient residency-checked runs to compute an honest band; "+
			"check SELinux /dev/kfd / container_use_devices on the ROCm side")
		return exitWarn
	}
	return exitPass
}

// benchConfiguredBackend names the currently-configured backend for the single-mode
// result label. It is a package-level indirection mirroring benchEndpointReachable so
// bench_test.go can drive the single-mode label without a live host (runBench stays
// drivable with a stubbed bench.Deps and no live config). The default reads
// config.LoadVilla().Backend, returning "" on a load error — it NEVER panics and NEVER
// fabricates a backend name. Single-mode-only: runBench guards the assignment with
// `if !ab` so the --ab branch (which labels sides from res.AB.From/To) is untouched.
var benchConfiguredBackend = func() string {
	cfg, err := config.LoadVilla()
	if err != nil {
		return ""
	}
	return cfg.Backend
}

// benchEndpointReachable is the read-only reachability pre-check. It is a package-level
// indirection (not a field on the LOCKED bench.Deps) so bench_test.go overrides it to
// drive the no-endpoint refusal without a live host; the default resolves the endpoint
// the status/prove path does and bounds a health poll (no --rm container).
var benchEndpointReachable = func() bool {
	cfg, err := config.LoadVilla()
	if err != nil {
		return false
	}
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return false
	}
	endpoint := inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := inference.PollHealth(ctx, endpoint, 5*time.Second)
	return ready.Known && ready.Value
}

// benchEntryFromResult maps the pure bench.Result into the typed --json/render entry,
// carrying pp and tg SEPARATELY end-to-end.
func benchEntryFromResult(res bench.Result, ab bool, spec bench.BenchSpec) benchEntry {
	conds := benchConditions{
		Warmup:   spec.Warmup,
		Reps:     spec.Reps,
		NPredict: spec.NPredict,
		Seed:     spec.Seed,
		Temp:     spec.Temp,
	}
	if ab && res.AB != nil {
		a := sideFromStats(res.AB.From, res.AB.A)
		b := sideFromStats(res.AB.To, res.AB.B)
		return benchEntry{
			Mode:       "ab",
			Conditions: conds,
			AB: &benchAB{
				From:                 res.AB.From,
				To:                   res.AB.To,
				A:                    a,
				B:                    b,
				DeltaPromptPerSec:    res.AB.DeltaPP,
				DeltaPredictedPerSec: res.AB.DeltaTG,
			},
		}
	}
	side := sideFromStats(res.Backend, res.Single)
	return benchEntry{
		Mode:       "single",
		Conditions: conds,
		Single:     &side,
	}
}

// sideFromStats folds one side's pure Stats into the typed benchSide (pp/tg separate).
func sideFromStats(backend string, s bench.Stats) benchSide {
	return benchSide{
		Backend:         backend,
		PromptPerSec:    s.MedianPP,
		PromptStddev:    s.StddevPP,
		PredictedPerSec: s.MedianTG,
		PredictedStddev: s.StddevTG,
		Kept:            s.Kept,
		Void:            s.Void,
	}
}

// renderBench writes the human table: per side, pp tok/s (median ± stddev) and tg tok/s
// (median ± stddev) as TWO SEPARATE figures (never one blended number), plus Kept/Void
// and the stated conditions. For --ab it also shows Δpp and Δtg, each its own figure.
func renderBench(w io.Writer, e benchEntry) {
	fmt.Fprintf(w, "conditions: warmup=%d reps=%d n_predict=%d seed=%d temp=%g\n\n",
		e.Conditions.Warmup, e.Conditions.Reps, e.Conditions.NPredict, e.Conditions.Seed, e.Conditions.Temp)

	writeSide := func(label string, s benchSide) {
		fmt.Fprintf(w, "%s (%s):\n", label, s.Backend)
		fmt.Fprintf(w, "  pp tok/s: %8.2f ± %.2f\n", s.PromptPerSec, s.PromptStddev)
		fmt.Fprintf(w, "  tg tok/s: %8.2f ± %.2f\n", s.PredictedPerSec, s.PredictedStddev)
		fmt.Fprintf(w, "  kept=%d void=%d\n", s.Kept, s.Void)
	}

	if e.Mode == "ab" && e.AB != nil {
		writeSide("A", e.AB.A)
		fmt.Fprintln(w)
		writeSide("B", e.AB.B)
		fmt.Fprintf(w, "\ndelta (%s -> %s):\n", e.AB.From, e.AB.To)
		fmt.Fprintf(w, "  Δpp tok/s: %+8.2f\n", e.AB.DeltaPromptPerSec)
		fmt.Fprintf(w, "  Δtg tok/s: %+8.2f\n", e.AB.DeltaPredictedPerSec)
		return
	}
	if e.Single != nil {
		writeSide("backend", *e.Single)
	}
}
