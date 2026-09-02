package residency

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// passingSpec builds a ProofSpec that would sample and pass over the shared
// fakes (Settle == 0, one round); tests then break one piece at a time.
func passingSpec(drove *int) ProofSpec {
	return ProofSpec{
		Subject:  "residency under test load",
		IsActive: func(string) (string, error) { return "active", nil },
		Services: []string{"villa-llama.service"},
		ResolveTarget: func() (Target, *inference.Verdict) {
			return Target{Service: "villa-llama.service", ModelFile: "m.gguf", ContextLen: 4096, WeightBytes: 20 << 30}, nil
		},
		Load: Load{
			Drive: func(ctx context.Context) error {
				if drove != nil {
					*drove++
				}
				return nil
			},
			Rounds: 1,
			Budget: time.Second,
		},
		Unsampled: func(r LoadResult) inference.Verdict {
			return Unevaluable("could not evaluate residency under test load — no round sampled", "re-run `villa doctor`")
		},
	}
}

// TestProveUnderLoadGatesBeforeDriving proves the read-only precondition gate:
// an inactive service degrades to a typed-Unknown WARN naming the service, the
// target is never resolved and the drive never fires. The proof NEVER starts a
// service and NEVER fabricates a FAIL from a stack that is not running.
func TestProveUnderLoadGatesBeforeDriving(t *testing.T) {
	f := newFakeDeps()
	drove := 0
	resolved := false
	spec := passingSpec(&drove)
	spec.IsActive = func(string) (string, error) { return "inactive", nil }
	inner := spec.ResolveTarget
	spec.ResolveTarget = func() (Target, *inference.Verdict) {
		resolved = true
		return inner()
	}
	v := ProveUnderLoad(t.Context(), f.deps(), spec)
	if v.Status != inference.StatusWarn {
		t.Fatalf("inactive service must degrade to WARN, got %v", v.Status)
	}
	if !strings.Contains(v.Detail, "villa-llama.service is not active") {
		t.Errorf("the WARN must name the inactive service; got %q", v.Detail)
	}
	if resolved || drove != 0 {
		t.Errorf("gate must fire before target resolution (resolved=%v) and drive (drove=%d)", resolved, drove)
	}
}

// TestProveUnderLoadIsActiveErrorDegrades proves an unevaluable gate (IsActive
// errors — systemctl missing) degrades identically: typed-Unknown WARN, never a
// fabricated confident state.
func TestProveUnderLoadIsActiveErrorDegrades(t *testing.T) {
	f := newFakeDeps()
	spec := passingSpec(nil)
	spec.IsActive = func(string) (string, error) { return "", context.DeadlineExceeded }
	v := ProveUnderLoad(t.Context(), f.deps(), spec)
	if v.Status != inference.StatusWarn {
		t.Fatalf("an unevaluable gate must degrade to WARN, got %v", v.Status)
	}
}

// TestProveUnderLoadReturnsTheResolutionWarn proves a target-resolution failure
// surfaces the caller's typed-Unknown wording verbatim and never drives.
func TestProveUnderLoadReturnsTheResolutionWarn(t *testing.T) {
	f := newFakeDeps()
	drove := 0
	spec := passingSpec(&drove)
	spec.ResolveTarget = func() (Target, *inference.Verdict) {
		w := Unevaluable("could not evaluate residency under test load — the served model could not be resolved", "fix config")
		return Target{}, &w
	}
	v := ProveUnderLoad(t.Context(), f.deps(), spec)
	if v.Status != inference.StatusWarn || !strings.Contains(v.Detail, "could not be resolved") {
		t.Fatalf("resolution warn must surface verbatim, got %+v", v)
	}
	if drove != 0 {
		t.Errorf("a failed resolution must never drive; drove %d rounds", drove)
	}
}

// TestProveUnderLoadSamplesAndReturnsTheVerdict proves the happy path: gate
// passes, target resolves, the drive is sampled, and the fold's verdict comes
// back untouched.
func TestProveUnderLoadSamplesAndReturnsTheVerdict(t *testing.T) {
	f := newFakeDeps()
	drove := 0
	v := ProveUnderLoad(t.Context(), f.deps(), passingSpec(&drove))
	if v.Status != inference.StatusPass {
		t.Fatalf("sampled proof must return the fold verdict (PASS), got %+v", v)
	}
	if drove != 1 {
		t.Errorf("drive rounds = %d, want 1", drove)
	}
}

// TestProveUnderLoadUnsampledDegrades proves the honesty mapping: a proof where
// no round could be sampled under load returns the caller's Unsampled wording —
// the verdict is never reported, because "under load" was never met.
func TestProveUnderLoadUnsampledDegrades(t *testing.T) {
	f := newFakeDeps()
	spec := passingSpec(nil)
	// A settle discipline with rounds that finish instantly: never caught in flight.
	spec.Load.Settle = 50 * time.Millisecond
	v := ProveUnderLoad(t.Context(), f.deps(), spec)
	if v.Status != inference.StatusWarn || !strings.Contains(v.Detail, "no round sampled") {
		t.Fatalf("unsampled proof must degrade via the Unsampled wording, got %+v", v)
	}
}

// TestProveUnderLoadFalteredDegradesWhenWorded proves the second honesty rule:
// with a Faltered mapping supplied, a PASS sampled under an erroring drive is
// degraded; without one, the settle-based discipline stands on its sampled round.
func TestProveUnderLoadFalteredDegradesWhenWorded(t *testing.T) {
	f := newFakeDeps()
	spec := passingSpec(nil)
	spec.Load.Rounds = 2
	spec.Load.DriveAllRounds = true
	spec.Load.Drive = func(ctx context.Context) error { return context.DeadlineExceeded }
	spec.Faltered = func(r LoadResult) inference.Verdict {
		return Unevaluable("could not evaluate residency under test load — the drive faltered", "check `villa logs`")
	}
	v := ProveUnderLoad(t.Context(), f.deps(), spec)
	if v.Status != inference.StatusWarn || !strings.Contains(v.Detail, "faltered") {
		t.Fatalf("a PASS under a faltering drive must degrade via Faltered, got %+v", v)
	}

	spec.Faltered = nil
	v = ProveUnderLoad(t.Context(), f.deps(), spec)
	if v.Status != inference.StatusPass {
		t.Fatalf("without a Faltered mapping the sampled verdict stands, got %+v", v)
	}
}
