package residency

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// loadRecorder drives a fake workload whose per-round duration a test controls, and
// records whether each sample landed while a round was actually running. That
// recording is the point: the invariant under test is not "a verdict came back" but
// "the verdict was taken while the stack was under load".
type loadRecorder struct {
	mu       sync.Mutex
	inFlight int
	// sampledInFlight records, for each sample taken, how many rounds were running
	// at that instant. A zero entry is an idle sample — the false-green this
	// protocol exists to prevent.
	sampledInFlight []int
	launched        int

	durations []time.Duration // per round; the last value repeats
	errs      []error         // per round; the last value repeats
}

func (r *loadRecorder) drive(ctx context.Context) error {
	r.mu.Lock()
	i := r.launched
	r.launched++
	r.inFlight++
	r.mu.Unlock()

	d := pick(r.durations, i, time.Duration(0))
	if d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
		}
	}

	r.mu.Lock()
	r.inFlight--
	r.mu.Unlock()
	return pick(r.errs, i, nil)
}

func (r *loadRecorder) recordSample() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sampledInFlight = append(r.sampledInFlight, r.inFlight)
}

func pick[T any](s []T, i int, zero T) T {
	if len(s) == 0 {
		return zero
	}
	if i >= len(s) {
		i = len(s) - 1
	}
	return s[i]
}

// loadDeps builds seams whose Journal read doubles as the sample instant, so the
// recorder can observe how many rounds were in flight when the fold happened.
func loadDeps(r *loadRecorder, f *fakeDeps) Deps {
	d := f.deps()
	journal := d.Journal
	d.Journal = func(service string) (string, bool) {
		r.recordSample()
		return journal(service)
	}
	return d
}

// TestUnderLoadNeverSamplesIdle is the invariant the whole protocol exists for. A
// round that finishes before the settle deadline was too fast to have loaded the
// model under observation, so it must be joined and the next round driven — never
// sampled. A CPU fallback under load is indistinguishable from a healthy stack once
// the load stops, so an idle sample is a false-green.
func TestUnderLoadNeverSamplesIdle(t *testing.T) {
	t.Run("fast rounds are never sampled", func(t *testing.T) {
		r := &loadRecorder{durations: []time.Duration{0}} // every round returns instantly
		f := newFakeDeps()

		got := UnderLoad(context.Background(), loadDeps(r, f), target(), Load{
			Drive:  r.drive,
			Rounds: 3,
			Settle: 50 * time.Millisecond,
			Budget: 5 * time.Second,
		})

		if got.Sampled {
			t.Error("a round that finished before the settle deadline must never be sampled")
		}
		if len(r.sampledInFlight) != 0 {
			t.Errorf("the fold ran %d times on an idle stack", len(r.sampledInFlight))
		}
		if r.launched != 3 {
			t.Errorf("launched %d rounds, want all 3 attempted before degrading", r.launched)
		}
		if f.foldCalls != 0 {
			t.Error("an unsampled proof must not produce a verdict")
		}
	})

	t.Run("a slow round is sampled while it runs", func(t *testing.T) {
		r := &loadRecorder{durations: []time.Duration{200 * time.Millisecond}}
		f := newFakeDeps()

		got := UnderLoad(context.Background(), loadDeps(r, f), target(), Load{
			Drive:  r.drive,
			Rounds: 3,
			Settle: 20 * time.Millisecond,
			Budget: 5 * time.Second,
		})

		if !got.Sampled {
			t.Fatal("a round still running at the settle deadline must be sampled")
		}
		if len(r.sampledInFlight) != 1 {
			t.Fatalf("sampled %d times, want exactly 1", len(r.sampledInFlight))
		}
		if r.sampledInFlight[0] != 1 {
			t.Errorf("the sample saw %d rounds in flight, want 1 — it was taken on an idle stack", r.sampledInFlight[0])
		}
		if r.launched != 1 {
			t.Errorf("launched %d rounds, want 1 (driving stops once sampled)", r.launched)
		}
	})

	t.Run("the first slow round after fast ones is the one sampled", func(t *testing.T) {
		r := &loadRecorder{durations: []time.Duration{0, 0, 200 * time.Millisecond}}
		f := newFakeDeps()

		got := UnderLoad(context.Background(), loadDeps(r, f), target(), Load{
			Drive:  r.drive,
			Rounds: 4,
			Settle: 20 * time.Millisecond,
			Budget: 5 * time.Second,
		})

		if !got.Sampled {
			t.Fatal("the protocol must keep driving until a round stays in flight")
		}
		if len(r.sampledInFlight) != 1 || r.sampledInFlight[0] != 1 {
			t.Errorf("samples = %v, want exactly one taken with a round in flight", r.sampledInFlight)
		}
	})
}

// TestUnderLoadJoinsEveryRound: no probe process or --rm container may outlive the
// call. Every launched round is joined before UnderLoad returns, including the one
// that was sampled and one interrupted by the budget.
func TestUnderLoadJoinsEveryRound(t *testing.T) {
	t.Run("the sampled round is joined", func(t *testing.T) {
		r := &loadRecorder{durations: []time.Duration{100 * time.Millisecond}}
		f := newFakeDeps()

		UnderLoad(context.Background(), loadDeps(r, f), target(), Load{
			Drive:  r.drive,
			Rounds: 2,
			Settle: 10 * time.Millisecond,
			Budget: 5 * time.Second,
		})

		r.mu.Lock()
		defer r.mu.Unlock()
		if r.inFlight != 0 {
			t.Errorf("%d rounds still in flight after the call returned", r.inFlight)
		}
	})

	t.Run("a budget-exhausted round is joined", func(t *testing.T) {
		r := &loadRecorder{durations: []time.Duration{time.Second}}
		f := newFakeDeps()

		UnderLoad(context.Background(), loadDeps(r, f), target(), Load{
			Drive:  r.drive,
			Rounds: 2,
			Settle: 30 * time.Millisecond,
			Budget: 50 * time.Millisecond,
		})

		r.mu.Lock()
		defer r.mu.Unlock()
		if r.inFlight != 0 {
			t.Errorf("%d rounds still in flight after the budget cut the call short", r.inFlight)
		}
	})
}

// TestUnderLoadWarmupSamplesAfterRealWork covers the cheap-uniform-round discipline
// (Settle == 0): drive Warmup rounds to completion so the stack has demonstrably
// done real work, then launch the next round and sample.
//
// The load evidence here is the completed warmup, not a confirmed in-flight
// instant — a round this short cannot be caught by a settle deadline, so the
// assertion is on the warmup ordering rather than on inFlight. That is the weaker of
// the two disciplines, and the reason Settle > 0 is preferred wherever rounds are
// long enough to be caught.
func TestUnderLoadWarmupSamplesAfterRealWork(t *testing.T) {
	r := &loadRecorder{durations: []time.Duration{50 * time.Millisecond}}
	f := newFakeDeps()

	got := UnderLoad(context.Background(), loadDeps(r, f), target(), Load{
		Drive:          r.drive,
		Rounds:         5,
		Warmup:         2,
		Budget:         5 * time.Second,
		DriveAllRounds: true,
	})

	if !got.Sampled {
		t.Fatal("the warmup discipline must sample once the warmup rounds complete")
	}
	if len(r.sampledInFlight) != 1 {
		t.Fatalf("sampled %d times, want exactly 1", len(r.sampledInFlight))
	}
	if r.launched < 3 {
		t.Errorf("sampled after %d rounds launched, want the sample to follow the 2 warmup rounds", r.launched)
	}
	if got.Completed != 5 {
		t.Errorf("completed %d of 5 rounds; DriveAllRounds must finish the drive", got.Completed)
	}
}

// TestDriveFalteredRefusesAPassUnderABrokenDrive: a PASS sampled while the workload
// was erroring did not observe the stack under real load, so it is not a proven
// PASS. A confident FAIL and an unevaluable WARN both stand on their own.
func TestDriveFalteredRefusesAPassUnderABrokenDrive(t *testing.T) {
	cases := []struct {
		name   string
		status inference.Status
		errs   int
		want   bool
	}{
		{"pass under a faltering drive is not proven", inference.StatusPass, 1, true},
		{"pass under a clean drive stands", inference.StatusPass, 0, false},
		{"a confident FAIL stands even under a faltering drive", inference.StatusFail, 1, false},
		{"an unevaluable WARN stands even under a faltering drive", inference.StatusWarn, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := LoadResult{
				Verdict:   inference.Verdict{Status: tc.status},
				DriveErrs: tc.errs,
			}
			if got := r.DriveFaltered(); got != tc.want {
				t.Errorf("DriveFaltered() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestUnderLoadCountsDriveErrors: a round's error is counted but is not itself the
// failure signal. What is being proven is residency, not the round's success, so the
// drive continues and the sample is still taken.
func TestUnderLoadCountsDriveErrors(t *testing.T) {
	r := &loadRecorder{
		durations: []time.Duration{0, 100 * time.Millisecond},
		errs:      []error{errors.New("round 1 failed"), nil},
	}
	f := newFakeDeps()

	got := UnderLoad(context.Background(), loadDeps(r, f), target(), Load{
		Drive:  r.drive,
		Rounds: 3,
		Settle: 20 * time.Millisecond,
		Budget: 5 * time.Second,
	})

	if !got.Sampled {
		t.Fatal("a failing round must not stop the proof — residency is the signal, not the round")
	}
	if got.DriveErrs != 1 {
		t.Errorf("DriveErrs = %d, want 1", got.DriveErrs)
	}
}

// TestUnderLoadFoldsTheTargetAndProps proves the sample feeds the fold the same
// signals the status fold reads, keyed on the resolved GGUF filename.
func TestUnderLoadFoldsTheTargetAndProps(t *testing.T) {
	r := &loadRecorder{durations: []time.Duration{100 * time.Millisecond}}
	f := newFakeDeps()
	props := &inference.PropsInfo{ModelPath: "/models/qwen3-30b-Q4_K_M.gguf", NCtx: 8192}
	d := loadDeps(r, f)
	d.Props = func() *inference.PropsInfo { return props }

	UnderLoad(context.Background(), d, target(), Load{
		Drive:  r.drive,
		Rounds: 2,
		Settle: 10 * time.Millisecond,
		Budget: 5 * time.Second,
	})

	if f.foldCalls != 1 {
		t.Fatalf("fold called %d times, want 1", f.foldCalls)
	}
	if f.folded.Props != props {
		t.Error("the /props drift overlay must reach the fold")
	}
	if f.folded.ConfigModel != target().ModelFile {
		t.Errorf("ConfigModel = %q, want the resolved GGUF filename", f.folded.ConfigModel)
	}
	if f.folded.JournalText == "" {
		t.Error("the journal must reach the fold")
	}
}

// TestUnevaluableIsAWarnNeverAFail pins the shared constructor the three doctor
// proofs used to each declare: an unmet precondition warns, and is never a FAIL
// fabricated from a signal that could not be evaluated.
func TestUnevaluableIsAWarnNeverAFail(t *testing.T) {
	v := Unevaluable("villa-embed is not active", "run `villa up`, then re-run `villa doctor`")
	if v.Status != inference.StatusWarn {
		t.Errorf("Status = %v, want Warn — an unmet precondition is not a contradicted signal", v.Status)
	}
	if v.Remediation == "" {
		t.Error("an unevaluable verdict must carry remediation")
	}
}
