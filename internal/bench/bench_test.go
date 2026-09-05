package bench

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// bench_test.go drives the pure benchmarking core through a fake Deps recorder
// with no live host. It mirrors the backendswap swapRecorder + callOrder
// discipline ([08-01]): every host-touching seam (Measure / Switch / Restore)
// is a closure that appends to callOrder and returns knob-driven values, so the
// methodology invariants (warmup discard, residency void-gate, void-exhaustion
// WARN, identical --ab spec, always-restore) are asserted off-hardware.

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// --- Task 1: pure stats helpers + per-metric separation -----------------------

// TestStats covers median (odd/even), the n<2 stddev guard, and the empty/single
// degenerate inputs — every path returns a finite number, never a panic.
func TestStats(t *testing.T) {
	// median over a known odd-length slice returns the middle element.
	if got := median([]float64{3, 1, 2}); !approx(got, 2) {
		t.Errorf("median odd = %v, want 2", got)
	}
	// median over an even-length slice returns the mean of the two middles.
	if got := median([]float64{4, 1, 3, 2}); !approx(got, 2.5) {
		t.Errorf("median even = %v, want 2.5", got)
	}
	// median over empty / single-element input is 0 (never panic).
	if got := median(nil); got != 0 {
		t.Errorf("median(nil) = %v, want 0", got)
	}
	if got := median([]float64{42}); !approx(got, 42) {
		t.Errorf("median single = %v, want 42", got)
	}
	// stddev is sample (n-1); 0 for n<2.
	if got := stddev([]float64{2, 4, 4, 4, 5, 5, 7, 9}); !approx(got, 2.138089935299395) {
		t.Errorf("stddev sample = %v, want ~2.138089935", got)
	}
	if got := stddev([]float64{5}); got != 0 {
		t.Errorf("stddev(n=1) = %v, want 0", got)
	}
	if got := stddev(nil); got != 0 {
		t.Errorf("stddev(nil) = %v, want 0", got)
	}
}

// TestSeparatePPTG proves pp and tg are computed from SEPARATE per-metric slices:
// feeding distinct pp and tg values yields independent median/stddev figures, and
// changing one never affects the other.
func TestSeparatePPTG(t *testing.T) {
	runs := []RunTimings{
		{PromptPerSec: 100, PredictedPerSec: 10},
		{PromptPerSec: 200, PredictedPerSec: 20},
		{PromptPerSec: 300, PredictedPerSec: 30},
	}
	st := statsOf(runs)
	if !approx(st.MedianPP, 200) {
		t.Errorf("MedianPP = %v, want 200", st.MedianPP)
	}
	if !approx(st.MedianTG, 20) {
		t.Errorf("MedianTG = %v, want 20", st.MedianTG)
	}
	// pp stddev (sample) over {100,200,300} = 100; tg over {10,20,30} = 10.
	if !approx(st.StddevPP, 100) {
		t.Errorf("StddevPP = %v, want 100", st.StddevPP)
	}
	if !approx(st.StddevTG, 10) {
		t.Errorf("StddevTG = %v, want 10", st.StddevTG)
	}
	// Mutating tg values leaves pp figures byte-identical (no blended slice).
	runs2 := []RunTimings{
		{PromptPerSec: 100, PredictedPerSec: 999},
		{PromptPerSec: 200, PredictedPerSec: 1},
		{PromptPerSec: 300, PredictedPerSec: 500},
	}
	st2 := statsOf(runs2)
	if !approx(st2.MedianPP, st.MedianPP) || !approx(st2.StddevPP, st.StddevPP) {
		t.Errorf("changing tg values must not affect pp figures: got pp median=%v stddev=%v", st2.MedianPP, st2.StddevPP)
	}
	if st.Kept != 3 {
		t.Errorf("Kept = %d, want 3", st.Kept)
	}
}

// TestAcceptanceStats proves statsOf computes MedianAcceptance and Drafted from
// runs that actually drafted (DraftN > 0): acceptance is accepted/drafted per run,
// median over ONLY the drafted runs — a run the ngram cache never fired on
// (DraftN==0) must not enter the acceptance set as a fabricated 0% (#119).
func TestAcceptanceStats(t *testing.T) {
	cases := []struct {
		name             string
		runs             []RunTimings
		wantDrafted      int
		wantAcceptance   float64
		wantAcceptanceOK bool // sanity: MedianAcceptance is meaningful only if wantDrafted > 0
	}{
		{
			name: "all runs drafted, uniform acceptance",
			runs: []RunTimings{
				{DraftN: 100, DraftAccepted: 80},
				{DraftN: 200, DraftAccepted: 160},
			},
			wantDrafted:      2,
			wantAcceptance:   0.8,
			wantAcceptanceOK: true,
		},
		{
			name: "mixed: one run never drafted",
			runs: []RunTimings{
				{DraftN: 100, DraftAccepted: 90}, // 0.9
				{DraftN: 0, DraftAccepted: 0},    // never drafted — excluded, not a 0%
				{DraftN: 100, DraftAccepted: 70}, // 0.7
			},
			wantDrafted:      2,
			wantAcceptance:   0.8, // median(0.9, 0.7)
			wantAcceptanceOK: true,
		},
		{
			name: "no run drafted",
			runs: []RunTimings{
				{DraftN: 0, DraftAccepted: 0},
				{DraftN: 0, DraftAccepted: 0},
			},
			wantDrafted:      0,
			wantAcceptance:   0,
			wantAcceptanceOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := statsOf(tc.runs)
			if st.Drafted != tc.wantDrafted {
				t.Errorf("Drafted = %d, want %d", st.Drafted, tc.wantDrafted)
			}
			if !approx(st.MedianAcceptance, tc.wantAcceptance) {
				t.Errorf("MedianAcceptance = %v, want %v", st.MedianAcceptance, tc.wantAcceptance)
			}
		})
	}
}

// TestABDeltaAcceptance proves ABResult.DeltaAcceptance is computed ONLY when BOTH
// sides drafted at least one run; a side that never drafted makes the comparison
// meaningless, so AcceptanceComparable is false and the delta stays zero rather
// than fabricating a number from an undrafted side (#119).
func TestABDeltaAcceptance(t *testing.T) {
	drafted := Stats{MedianAcceptance: 0.9, Drafted: 3}
	otherDrafted := Stats{MedianAcceptance: 0.6, Drafted: 5}
	neverDrafted := Stats{MedianAcceptance: 0, Drafted: 0}

	both := abResult("vulkan", "rocm", drafted, otherDrafted)
	if !both.AcceptanceComparable {
		t.Fatalf("both sides drafted: AcceptanceComparable = false, want true")
	}
	if !approx(both.DeltaAcceptance, 0.6-0.9) {
		t.Errorf("DeltaAcceptance = %v, want %v (B-A)", both.DeltaAcceptance, 0.6-0.9)
	}

	oneSide := abResult("vulkan", "rocm", drafted, neverDrafted)
	if oneSide.AcceptanceComparable {
		t.Errorf("one side never drafted: AcceptanceComparable = true, want false")
	}
	if oneSide.DeltaAcceptance != 0 {
		t.Errorf("one side never drafted: DeltaAcceptance = %v, want 0 (never fabricated)", oneSide.DeltaAcceptance)
	}
}

// --- Task 2: the Run state-machine via a fake-Deps recorder -------------------

// measureVerdict is one canned Measure outcome the recorder replays in order.
type measureVerdict struct {
	t        RunTimings
	resident bool
	err      error
}

// benchRecorder records each side-effecting seam call so the tests can assert
// warmup-discard counts, void-run exclusion, and the --ab always-restore final
// op. It clones the backendswap swapRecorder/callOrder discipline.
type benchRecorder struct {
	callOrder    []string
	measureCalls int
	specs        []Spec // the spec each benchN side received (--ab identical-spec)

	verdicts []measureVerdict // replayed in order across ALL measured (non-warmup) runs
	vIdx     int

	currentBE string // backend in the loaded config (orig for --ab restore)
}

// next pops the next canned verdict; once exhausted it repeats the last one so a
// test need only enumerate the interesting prefix.
func (r *benchRecorder) next() measureVerdict {
	if len(r.verdicts) == 0 {
		return measureVerdict{t: RunTimings{PromptPerSec: 1, PredictedPerSec: 1}, resident: true}
	}
	v := r.verdicts[r.vIdx]
	if r.vIdx < len(r.verdicts)-1 {
		r.vIdx++
	}
	return v
}

// newBenchStub builds a single-backend Deps (Switch/Restore nil) wired to rec.
func newBenchStub(rec *benchRecorder) Deps {
	if rec.currentBE == "" {
		rec.currentBE = "vulkan"
	}
	return Deps{
		Measure: func(_ context.Context) (RunTimings, bool, string, error) {
			rec.measureCalls++
			rec.callOrder = append(rec.callOrder, "measure")
			v := rec.next()
			return v.t, v.resident, "", v.err
		},
		LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: "preserved-model", Backend: rec.currentBE}, nil
		},
	}
}

// newBenchABStub builds an --ab Deps: Switch/Restore record their target onto
// callOrder so TestBenchABRestoresOriginal can assert the final op restores orig.
func newBenchABStub(rec *benchRecorder, switchErr error) Deps {
	d := newBenchStub(rec)
	d.Measure = func(_ context.Context) (RunTimings, bool, string, error) {
		rec.measureCalls++
		rec.callOrder = append(rec.callOrder, "measure")
		v := rec.next()
		return v.t, v.resident, "", v.err
	}
	d.OnSideStart = func(side string, spec Spec) {
		rec.callOrder = append(rec.callOrder, "side:"+side)
		rec.specs = append(rec.specs, spec)
	}
	d.Switch = func(_ context.Context, target string) error {
		rec.callOrder = append(rec.callOrder, "switch:"+target)
		return switchErr
	}
	d.Restore = func(_ context.Context, original string) error {
		rec.callOrder = append(rec.callOrder, "restore:"+original)
		return nil
	}
	return d
}

func lastOp(order []string, prefix string) string {
	last := ""
	for _, c := range order {
		if strings.HasPrefix(c, prefix) {
			last = c
		}
	}
	return last
}

func countOp(order []string, op string) int {
	n := 0
	for _, c := range order {
		if c == op {
			n++
		}
	}
	return n
}

// TestWarmupDiscarded: with Warmup=1, Reps=3 (all resident), the recorder shows
// 1+3 Measure calls but Stats.Kept==3, and the warmup run's timing is NOT in the
// computed median.
func TestWarmupDiscarded(t *testing.T) {
	rec := &benchRecorder{verdicts: []measureVerdict{
		{t: RunTimings{PromptPerSec: 9999, PredictedPerSec: 9999}, resident: true}, // warmup (discarded)
		{t: RunTimings{PromptPerSec: 100, PredictedPerSec: 10}, resident: true},
		{t: RunTimings{PromptPerSec: 200, PredictedPerSec: 20}, resident: true},
		{t: RunTimings{PromptPerSec: 300, PredictedPerSec: 30}, resident: true},
	}}
	res := Run(t.Context(), newBenchStub(rec), Spec{Reps: 3, Warmup: 1, MinResident: 3})
	if rec.measureCalls != 4 {
		t.Errorf("expected 1 warmup + 3 measured = 4 Measure calls, got %d", rec.measureCalls)
	}
	if res.Single.Kept != 3 {
		t.Errorf("Kept = %d, want 3", res.Single.Kept)
	}
	// median of {100,200,300} is 200 — the 9999 warmup must not be in the set.
	if !approx(res.Single.MedianPP, 200) {
		t.Errorf("warmup leaked into stats: MedianPP = %v, want 200", res.Single.MedianPP)
	}
	if res.VoidExhausted {
		t.Errorf("3 resident runs >= MinResident must NOT be void-exhausted")
	}
}

// TestVoidNonResident: a Measure returning resident=false is excluded from Kept
// (counted as Void) and never substituted as a slow pass.
func TestVoidNonResident(t *testing.T) {
	rec := &benchRecorder{verdicts: []measureVerdict{
		{t: RunTimings{PromptPerSec: 100, PredictedPerSec: 10}, resident: true},
		{t: RunTimings{PromptPerSec: 5, PredictedPerSec: 1}, resident: false}, // VOID — must not count
		{t: RunTimings{PromptPerSec: 200, PredictedPerSec: 20}, resident: true},
		{t: RunTimings{PromptPerSec: 300, PredictedPerSec: 30}, resident: true},
	}}
	res := Run(t.Context(), newBenchStub(rec), Spec{Reps: 3, Warmup: 0, MinResident: 3})
	if res.Single.Kept != 3 {
		t.Errorf("Kept = %d, want 3 (only resident runs)", res.Single.Kept)
	}
	if res.Single.Void != 1 {
		t.Errorf("Void = %d, want 1 (the non-resident run)", res.Single.Void)
	}
	// The void run's slow 5 t/s pp must not be in the median.
	if !approx(res.Single.MedianPP, 200) {
		t.Errorf("void run substituted as a slow pass: MedianPP = %v, want 200", res.Single.MedianPP)
	}
}

// TestVoidExhaustionWarn: when resident runs collected < MinResident after the
// capped attempt budget, VoidExhausted==true with an honest Reason; no confident
// band is presented as authoritative.
func TestVoidExhaustionWarn(t *testing.T) {
	rec := &benchRecorder{verdicts: []measureVerdict{
		{t: RunTimings{PromptPerSec: 1, PredictedPerSec: 1}, resident: false}, // every run voids
	}}
	res := Run(t.Context(), newBenchStub(rec), Spec{Reps: 3, Warmup: 0, MinResident: 3})
	if !res.VoidExhausted {
		t.Fatalf("all-void runs must set VoidExhausted=true, got %+v", res)
	}
	if !strings.Contains(res.Reason, "insufficient residency-checked runs") {
		t.Errorf("VoidExhausted Reason must be honest, got %q", res.Reason)
	}
	if res.Single.Kept >= res.Single.Void+1 && res.Single.Kept >= 3 {
		t.Errorf("a void-exhausted result must not report MinResident kept runs, got Kept=%d", res.Single.Kept)
	}
	// The attempt budget is bounded — it must not loop forever on all-void.
	attemptBudget := 2*3 + 0
	if rec.measureCalls > attemptBudget {
		t.Errorf("measured runs %d exceeded the attempt cap %d (must be bounded)", rec.measureCalls, attemptBudget)
	}
}

// TestIdenticalSpecBothSides: in --ab mode the recorder captures the Spec
// passed to each side; both sides receive a byte-identical spec.
func TestIdenticalSpecBothSides(t *testing.T) {
	rec := &benchRecorder{currentBE: "vulkan"}
	d := newBenchABStub(rec, nil)
	// record the spec each side received by wrapping Measure-free: capture in benchN
	// via a Switch boundary. Instead, assert through the recorder's specs slice,
	// which Run fills per side.
	spec := Spec{Reps: 2, Warmup: 1, Prompt: "hello", NPredict: 16, Seed: 7, Temp: 0.0, MinResident: 1}
	res := Run(t.Context(), d, spec)
	if res.AB == nil {
		t.Fatalf("--ab Run must produce an ABResult, got %+v", res)
	}
	if len(rec.specs) != 2 {
		t.Fatalf("expected both sides to record their spec, got %d", len(rec.specs))
	}
	if rec.specs[0] != rec.specs[1] {
		t.Errorf("both --ab sides must receive an identical spec: A=%+v B=%+v", rec.specs[0], rec.specs[1])
	}
}

// TestBenchABUnsetTargetPreservesOther: with spec.ABTarget == "" and an original
// backend "vulkan", the --ab flip is the v1.1 other() 2-value swap — Run flips
// d.Switch to "rocm" and ABResult.To == "rocm" (back-compat, the existing golden
// semantics). This is the unset default that MUST NOT change.
func TestBenchABUnsetTargetPreservesOther(t *testing.T) {
	rec := &benchRecorder{currentBE: "vulkan", verdicts: []measureVerdict{
		{t: RunTimings{PromptPerSec: 100, PredictedPerSec: 10}, resident: true},
	}}
	d := newBenchABStub(rec, nil)
	res := Run(t.Context(), d, Spec{Reps: 1, Warmup: 0, MinResident: 1}) // ABTarget unset
	if res.AB == nil {
		t.Fatalf("--ab Run must produce an ABResult, got %+v", res)
	}
	if res.AB.From != "vulkan" {
		t.Errorf("ABResult.From = %q, want %q (the loaded original)", res.AB.From, "vulkan")
	}
	if res.AB.To != "rocm" {
		t.Errorf("unset ABTarget: ABResult.To = %q, want %q (other(orig) default)", res.AB.To, "rocm")
	}
	if got := lastOp(rec.callOrder, "switch:"); got != "switch:rocm" {
		t.Errorf("unset ABTarget must flip to other(orig)=rocm, got switch op %q (order=%v)", got, rec.callOrder)
	}
}

// TestBenchABExplicitTarget: with spec.ABTarget set to an arbitrary named backend
// and an original that is NOT its other() opposite, Run flips d.Switch to the NAMED
// target and ABResult.To equals that target (arbitrary-pair A/B — the capability
// the v1.1 other() 2-value swap cannot express). From always equals the loaded original.
func TestBenchABExplicitTarget(t *testing.T) {
	rec := &benchRecorder{currentBE: "rocm-6.4.4", verdicts: []measureVerdict{
		{t: RunTimings{PromptPerSec: 100, PredictedPerSec: 10}, resident: true},
	}}
	d := newBenchABStub(rec, nil)
	res := Run(t.Context(), d, Spec{Reps: 1, Warmup: 0, MinResident: 1, ABTarget: "rocm-7.2.4"})
	if res.AB == nil {
		t.Fatalf("--ab Run must produce an ABResult, got %+v", res)
	}
	if res.AB.From != "rocm-6.4.4" {
		t.Errorf("ABResult.From = %q, want %q (the loaded original)", res.AB.From, "rocm-6.4.4")
	}
	if res.AB.To != "rocm-7.2.4" {
		t.Errorf("explicit ABTarget: ABResult.To = %q, want %q (the named target, NOT other(orig))", res.AB.To, "rocm-7.2.4")
	}
	if got := lastOp(rec.callOrder, "switch:"); got != "switch:rocm-7.2.4" {
		t.Errorf("explicit ABTarget must flip to the NAMED target, got switch op %q (order=%v)", got, rec.callOrder)
	}
}

// TestBenchABDeltasNeverBlended: with an explicit target, DeltaPP/DeltaTG are still
// computed B−A per metric (never blended), and From is always the loaded original.
func TestBenchABDeltasNeverBlended(t *testing.T) {
	rec := &benchRecorder{currentBE: "rocm-6.4.4", verdicts: []measureVerdict{
		{t: RunTimings{PromptPerSec: 100, PredictedPerSec: 10}, resident: true}, // side A
		{t: RunTimings{PromptPerSec: 150, PredictedPerSec: 25}, resident: true}, // side B (after switch)
	}}
	d := newBenchABStub(rec, nil)
	res := Run(t.Context(), d, Spec{Reps: 1, Warmup: 0, MinResident: 1, ABTarget: "rocm-7.2.4"})
	if res.AB == nil {
		t.Fatalf("--ab Run must produce an ABResult, got %+v", res)
	}
	if !approx(res.AB.DeltaPP, 50) {
		t.Errorf("DeltaPP = %v, want 50 (B−A pp, never blended)", res.AB.DeltaPP)
	}
	if !approx(res.AB.DeltaTG, 15) {
		t.Errorf("DeltaTG = %v, want 15 (B−A tg, never blended)", res.AB.DeltaTG)
	}
}

// TestBenchABRestoresOriginal: an --ab run that ERRORS mid-way (second-side
// Measure errors) still calls Restore(orig) exactly once as its FINAL backend op.
func TestBenchABRestoresOriginal(t *testing.T) {
	rec := &benchRecorder{currentBE: "vulkan", verdicts: []measureVerdict{
		// side A: one resident run, then side B errors
		{t: RunTimings{PromptPerSec: 100, PredictedPerSec: 10}, resident: true},
		{t: RunTimings{}, resident: false, err: context.DeadlineExceeded},
	}}
	d := newBenchABStub(rec, nil)
	res := Run(t.Context(), d, Spec{Reps: 1, Warmup: 0, MinResident: 1})
	_ = res
	last := lastOp(rec.callOrder, "restore:")
	if last != "restore:vulkan" {
		t.Fatalf("final backend op must be Restore(orig=vulkan), got order %v", rec.callOrder)
	}
	if countOp(rec.callOrder, "restore:vulkan") != 1 {
		t.Errorf("Restore(orig) must fire exactly once, got order %v", rec.callOrder)
	}
	// And it must come AFTER the switch to the other backend.
	switchIdx, restoreIdx := -1, -1
	for i, c := range rec.callOrder {
		if strings.HasPrefix(c, "switch:") {
			switchIdx = i
		}
		if c == "restore:vulkan" {
			restoreIdx = i
		}
	}
	restoreAfterSwitch := switchIdx >= 0 && restoreIdx >= 0 && switchIdx < restoreIdx
	if !restoreAfterSwitch {
		t.Errorf("Restore(orig) must follow the Switch(other), got order %v", rec.callOrder)
	}
}

// TestCancelStopsMeasuringPromptly pins the Ctrl-C path in the measurement loop.
//
// The attempt budget is 2*Reps, and a cancelled Measure fails fast and counts as
// VOID. Before benchN checked the context, an interrupt therefore did not stop the
// bench at all: it spun through the entire remaining budget calling Measure on a
// dead context, so a Ctrl-C on a `--reps 20` run still issued 40 doomed attempts
// before reporting the interrupt as a residency problem.
//
// Cancelling after the first run must leave the loop immediately: exactly one
// Measure call, no budget burn.
func TestCancelStopsMeasuringPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	rec := &benchRecorder{verdicts: []measureVerdict{
		{t: RunTimings{PromptPerSec: 100, PredictedPerSec: 10}, resident: true},
	}}
	d := newBenchStub(rec)
	inner := d.Measure
	d.Measure = func(c context.Context) (RunTimings, bool, string, error) {
		// Interrupt arrives while the first run is in flight.
		cancel()
		return inner(c)
	}

	Run(ctx, d, Spec{Reps: 20, Warmup: 0, MinResident: 20})

	if rec.measureCalls != 1 {
		t.Fatalf("a cancelled bench made %d Measure calls, want 1 — the loop must abandon "+
			"the attempt budget on Ctrl-C rather than burning it on a dead context",
			rec.measureCalls)
	}
}

// TestCancelStopsWarmupPromptly is the warmup-phase sibling: a Ctrl-C during the
// discarded warmup runs must abandon them too, not run every one against a dead
// context before reaching the measured loop.
func TestCancelStopsWarmupPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // already interrupted before the bench starts

	rec := &benchRecorder{verdicts: []measureVerdict{
		{t: RunTimings{PromptPerSec: 100, PredictedPerSec: 10}, resident: true},
	}}
	Run(ctx, newBenchStub(rec), Spec{Reps: 5, Warmup: 5, MinResident: 5})

	if rec.measureCalls != 0 {
		t.Fatalf("a pre-cancelled bench made %d Measure calls, want 0", rec.measureCalls)
	}
}

// TestRunToleratesNilContext pins the boundary normalization in Run.
//
// cobra's cmd.Context() returns nil for a Command that was never Execute()d, which
// is how the cmd-tier tests construct one. Once the measurement loop started
// consulting ctx.Err(), a nil context turned that into a nil-pointer panic — a
// crash reachable from a legitimate caller. Run normalizes nil to Background so the
// bench still runs to completion.
func TestRunToleratesNilContext(t *testing.T) {
	rec := &benchRecorder{verdicts: []measureVerdict{
		{t: RunTimings{PromptPerSec: 100, PredictedPerSec: 10}, resident: true},
	}}
	//nolint:staticcheck // SA1012 is the point: a nil ctx must not panic here.
	res := Run(nil, newBenchStub(rec), Spec{Reps: 1, Warmup: 1, MinResident: 1})

	if res.Single.Kept != 1 {
		t.Fatalf("nil-context run kept %d runs, want 1", res.Single.Kept)
	}
	if res.VoidExhausted {
		t.Error("a resident run under a nil context must not be void-exhausted")
	}
}
