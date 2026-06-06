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
	specs        []BenchSpec // the spec each benchN side received (--ab identical-spec)

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
	d.OnSideStart = func(side string, spec BenchSpec) {
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
	res := Run(context.Background(), newBenchStub(rec), BenchSpec{Reps: 3, Warmup: 1, MinResident: 3})
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
	res := Run(context.Background(), newBenchStub(rec), BenchSpec{Reps: 3, Warmup: 0, MinResident: 3})
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
	res := Run(context.Background(), newBenchStub(rec), BenchSpec{Reps: 3, Warmup: 0, MinResident: 3})
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
	cap := 2*3 + 0
	if rec.measureCalls > cap {
		t.Errorf("measured runs %d exceeded the attempt cap %d (must be bounded)", rec.measureCalls, cap)
	}
}

// TestIdenticalSpecBothSides: in --ab mode the recorder captures the BenchSpec
// passed to each side; both sides receive a byte-identical spec.
func TestIdenticalSpecBothSides(t *testing.T) {
	rec := &benchRecorder{currentBE: "vulkan"}
	d := newBenchABStub(rec, nil)
	// record the spec each side received by wrapping Measure-free: capture in benchN
	// via a Switch boundary. Instead, assert through the recorder's specs slice,
	// which Run fills per side.
	spec := BenchSpec{Reps: 2, Warmup: 1, Prompt: "hello", NPredict: 16, Seed: 7, Temp: 0.0, MinResident: 1}
	res := Run(context.Background(), d, spec)
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

// TestBenchABRestoresOriginal: an --ab run that ERRORS mid-way (second-side
// Measure errors) still calls Restore(orig) exactly once as its FINAL backend op.
func TestBenchABRestoresOriginal(t *testing.T) {
	rec := &benchRecorder{currentBE: "vulkan", verdicts: []measureVerdict{
		// side A: one resident run, then side B errors
		{t: RunTimings{PromptPerSec: 100, PredictedPerSec: 10}, resident: true},
		{t: RunTimings{}, resident: false, err: context.DeadlineExceeded},
	}}
	d := newBenchABStub(rec, nil)
	res := Run(context.Background(), d, BenchSpec{Reps: 1, Warmup: 0, MinResident: 1})
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
	if switchIdx < 0 || restoreIdx < 0 || !(switchIdx < restoreIdx) {
		t.Errorf("Restore(orig) must follow the Switch(other), got order %v", rec.callOrder)
	}
}
