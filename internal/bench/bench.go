// Package bench is the pure, Deps-injected benchmarking core for `villa bench`:
// the honest-methodology state-machine a throughput measurement must go through so
// a printed pp/tg delta is trustworthy. It clones the LOCKED `internal/backendswap`
// idiom (Deps/Result/Run) but is simpler — it has no rollback frame; the `--ab`
// flip DELEGATES to `backendswap.Run` through the injected Switch/Restore seams and
// never re-implements backend switching (STATE.md LOCKED).
//
// The methodology this core enforces:
//   - a Warmup run is measured then DISCARDED (never counted in stats);
//   - each measured run is residency-checked through the injected Measure verdict —
//     a run with resident==false is VOID (excluded from Kept, counted in Void),
//     never substituted as a slow pass (offload asserted, not assumed);
//   - the measured loop is bounded by an attempt cap so an all-void host can never
//     loop forever; if fewer than spec.MinResident resident runs are collected the
//     Result is an honest void-exhaustion WARN (VoidExhausted), not a confident band;
//   - prompt-processing (pp) and token-generation (tg) are carried SEPARATELY
//     end-to-end — two median+stddev figures, never blended into one slice or number;
//   - for `--ab`, the IDENTICAL BenchSpec is applied to both sides and the original
//     backend is ALWAYS restored on every exit path via a defer registered BEFORE
//     the flip (success / mid-AB error / panic-unwind all restore).
//
// Every host-touching action is an injected Deps field so the whole flow is driven
// from bench_test.go without a live host. The package is print-free and exit-free
// (the cobra layer in Plan 03 owns presentation), and deliberately imports NEITHER
// `internal/inference` NOR `internal/detect` so it stays literal-free of backend
// marker tokens — markers arrive ONLY through the injected Measure verdict (the
// TestSeamGrepGate invariant).
package bench

import (
	"context"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// BenchSpec is the identical, reproducible benchmark configuration applied to a
// single backend or — byte-for-byte — to BOTH sides of an --ab comparison.
type BenchSpec struct {
	// Reps is the number of RESIDENT (counted) runs to collect per side.
	Reps int
	// Warmup is the number of leading runs measured then discarded (caches/JIT
	// warm). Warmup runs are not residency-gated and never enter the stats.
	Warmup int
	// Prompt is the fixed prompt every run sends (reproducibility).
	Prompt string
	// NPredict is the fixed max_tokens every run requests (reproducibility).
	NPredict int
	// Seed is the fixed sampling seed (reproducibility).
	Seed int
	// Temp is the fixed sampling temperature (reproducibility; 0 = greedy).
	Temp float64
	// Timeout bounds a single Measure call (the load_tensors-hang guard lives in the
	// live Measure wiring, Plan 03; carried here so the spec is self-describing).
	Timeout time.Duration
	// MinResident is the floor of resident runs below which the Result is an honest
	// void-exhaustion WARN rather than a confident delta.
	MinResident int
}

// RunTimings is one run's server-computed throughput, pp and tg ALREADY separated
// (the llm.Timings shape the live Measure maps in). pp and tg never share a field.
type RunTimings struct {
	// PromptPerSec is the prompt-processing (pp) rate for this run.
	PromptPerSec float64
	// PredictedPerSec is the token-generation (tg) rate for this run.
	PredictedPerSec float64
	// PromptN / PredictedN are the token counts (carried for provenance, not folded
	// into the median — the rates are what the delta compares).
	PromptN   int
	PredictedN int
}

// Deps is the injectable seam set. Measure is always set; Switch/Restore/LoadConfig
// are set ONLY for the --ab path (nil for a single-backend run). Every field is a
// func so bench_test.go drives the whole flow without a live host.
type Deps struct {
	// Measure runs ONE bounded completion and returns its pp/tg timings plus the
	// residency verdict. resident==false marks a VOID run (excluded from stats); a
	// non-nil err is likewise void/error per the bounded live Measure. All backend
	// markers live behind this seam — never in this package.
	Measure func(ctx context.Context) (t RunTimings, resident bool, detail string, err error)
	// Switch flips the live stack to target by DELEGATING to backendswap.Run (live
	// wiring, Plan 03). nil for the single-backend path; set only for --ab.
	Switch func(ctx context.Context, target string) error
	// Restore flips the live stack back to the original backend (also via
	// backendswap.Run). nil for the single-backend path; set only for --ab.
	Restore func(ctx context.Context, original string) error
	// LoadConfig supplies the original backend (the restore target) for --ab. nil for
	// the single-backend path.
	LoadConfig func() (config.VillaConfig, error)
}

// Stats are the per-side, per-metric aggregates over the KEPT (resident) runs. pp
// and tg figures are computed from separate slices — there is deliberately NO
// blended tok/s field.
type Stats struct {
	// MedianPP / StddevPP are the prompt-processing median and sample stddev.
	MedianPP float64
	StddevPP float64
	// MedianTG / StddevTG are the token-generation median and sample stddev.
	MedianTG float64
	StddevTG float64
	// Kept is the number of resident runs that entered these figures; Void is the
	// number of non-resident/error runs excluded.
	Kept int
	Void int
}

// ABResult is the two-sided comparison. Each side keeps its own Kept/Void; the
// delta is computed per metric — never from a blended figure.
type ABResult struct {
	// From / To are the original and compared-against backends.
	From string
	To   string
	// A is the original-backend side; B is the To-backend side.
	A Stats
	B Stats
	// DeltaPP / DeltaTG are B minus A per metric (positive = To is faster).
	DeltaPP float64
	DeltaTG float64
}

// Result is the typed outcome of a bench Run (not an exit code) so the cobra caller
// (Plan 03) can branch on it. pp and tg are carried SEPARATELY end-to-end.
type Result struct {
	// Backend is the benchmarked backend (single-backend path); for --ab the sides'
	// backends live in AB.From / AB.To.
	Backend string
	// Single is the single-backend stats (zero value when AB is set).
	Single Stats
	// AB is non-nil only for an --ab comparison.
	AB *ABResult
	// VoidExhausted is true when fewer than spec.MinResident resident runs were
	// collected on a side — the band must NOT be presented as authoritative.
	VoidExhausted bool
	// Reason is the human explanation on a void-exhaustion WARN (empty on a clean run).
	Reason string
	// Err is a non-methodology failure (e.g. an --ab LoadConfig error). Distinct from
	// a void-exhaustion WARN (an honest low-confidence result, not an error).
	Err error
}

// statsOf folds a slice of kept (resident) RunTimings into per-metric Stats. pp and
// tg values are gathered into SEPARATE slices and never concatenated; Kept is the
// slice length. This is the only place the two metrics are aggregated, so the
// separation is structural.
func statsOf(kept []RunTimings) Stats {
	pp := make([]float64, 0, len(kept))
	tg := make([]float64, 0, len(kept))
	for _, r := range kept {
		pp = append(pp, r.PromptPerSec)
		tg = append(tg, r.PredictedPerSec)
	}
	return Stats{
		MedianPP: median(pp),
		StddevPP: stddev(pp),
		MedianTG: median(tg),
		StddevTG: stddev(tg),
		Kept:     len(kept),
	}
}

// other maps the two known backend tokens by string equality WITHOUT importing
// inference: it returns the opposite of orig in the local 2-value (vulkan<->rocm)
// swap. This is a string transform over values the caller already holds — no
// backend-marker literal and no allowlist — so the seam gate stays green.
func other(orig string) string {
	if orig == vulkan {
		return rocm
	}
	return vulkan
}

// vulkan / rocm are the two backend identifiers the --ab flip swaps between. They
// are configuration VALUES (the config.Backend string), not imperative backend
// assumptions — the seam gate matches GOOS/image/device/podman leaks, not these
// plain identifiers (mirrors recommend.go's "vulkan" default already in-tree).
const (
	vulkan = "vulkan"
	rocm   = "rocm"
)

// benchN runs the methodology for ONE side against the current backend: spec.Warmup
// discarded Measure calls, then measured runs until spec.Reps RESIDENT runs are
// collected OR a bounded attempt cap is hit (an all-void host can never loop
// forever). resident==false (or a Measure error) increments Void and is excluded;
// kept runs are folded into per-metric Stats. The boolean reports whether the floor
// (spec.MinResident) was met.
func benchN(ctx context.Context, d Deps, spec BenchSpec) (Stats, bool) {
	// Warmup: measure then discard (not residency-gated, never counted).
	for i := 0; i < spec.Warmup; i++ {
		_, _, _, _ = d.Measure(ctx)
	}

	kept := make([]RunTimings, 0, spec.Reps)
	void := 0
	// Bounded attempt budget so all-void runs terminate (Pitfall 5): allow up to one
	// retry per requested rep before declaring void-exhaustion.
	cap := 2 * spec.Reps
	for attempts := 0; len(kept) < spec.Reps && attempts < cap; attempts++ {
		t, resident, _, err := d.Measure(ctx)
		if err != nil || !resident {
			void++
			continue
		}
		kept = append(kept, t)
	}

	st := statsOf(kept)
	st.Void = void
	enough := len(kept) >= spec.MinResident
	return st, enough
}

// Run executes the honest benchmark and returns a typed Result. It is print-free
// and exit-free. When Switch/Restore are nil it benches a single backend; when they
// are set it runs the --ab comparison, applying the IDENTICAL spec to both sides and
// ALWAYS restoring the original backend on every exit path.
func Run(d Deps, spec BenchSpec) Result {
	ctx := context.Background()

	// Single-backend path.
	if d.Switch == nil || d.Restore == nil {
		st, enough := benchN(ctx, d, spec)
		res := Result{Single: st}
		if !enough {
			res.VoidExhausted = true
			res.Reason = "insufficient residency-checked runs " +
				"(collected fewer than the MinResident floor) — the throughput band is not authoritative"
		}
		return res
	}

	// --ab path. Load the original backend (the restore target) and IMMEDIATELY
	// register the restore so EVERY exit path — success, mid-AB error, void-exhaustion,
	// panic-unwind — flips the stack back (Pitfall 4 / Pattern 4). Switch/Restore
	// delegate (in the live layer, Plan 03) to backendswap.Run; this core never
	// touches quadlet/systemd directly (LOCKED).
	cfg, err := d.LoadConfig()
	if err != nil {
		return Result{Err: err}
	}
	orig := cfg.Backend
	defer d.Restore(ctx, orig) //nolint:errcheck // best-effort restore; live layer logs

	// Side A on the current (original) backend.
	statsA, enoughA := benchN(ctx, d, spec)

	// Flip to the other backend. A Switch error is surfaced; the deferred Restore
	// still fires (and the flip is then a backendswap no-op).
	if err := d.Switch(ctx, other(orig)); err != nil {
		ab := abResult(orig, statsA, Stats{})
		return Result{AB: &ab, Err: err}
	}

	// Side B on the other backend, with the SAME spec.
	statsB, enoughB := benchN(ctx, d, spec)

	ab := abResult(orig, statsA, statsB)
	res := Result{AB: &ab}
	if !enoughA || !enoughB {
		res.VoidExhausted = true
		res.Reason = "insufficient residency-checked runs on at least one --ab side " +
			"(collected fewer than the MinResident floor) — the throughput delta is not authoritative"
	}
	return res
}

// abResult builds the per-metric ABResult from the two sides' stats. Deltas are B
// minus A per metric — never derived from a blended figure.
func abResult(orig string, a, b Stats) ABResult {
	return ABResult{
		From:    orig,
		To:      other(orig),
		A:       a,
		B:       b,
		DeltaPP: b.MedianPP - a.MedianPP,
		DeltaTG: b.MedianTG - a.MedianTG,
	}
}
