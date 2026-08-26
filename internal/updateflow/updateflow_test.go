// Package updateflow tests drive every path — including each failure and each
// rollback — with no live host, because every host effect is an injected seam.
//
// The rows that matter are the ones where villa DECLINES: a subsystem that was
// already broken, a new image that cannot be proven, and a rollback that did not
// take. Each of those is a different sentence a user reads, and collapsing any two
// of them is how a user ends up believing something false about their stack.
package updateflow

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// recorder is a fake Deps that logs every step, so ORDERING is assertable and not
// merely assumed. Prove-current-before-capture is the whole point of the design, and
// a test that only checked outcomes would pass with the two swapped.
type recorder struct {
	steps []string

	proveCurrent  Proof
	proveNew      Proof
	proveRestored Proof

	captureErr     error
	pullErr        error
	mutateErr      error
	restoreErr     error
	commitErr      error
	stopErr        error
	snapshotErr    error
	startErr       error
	restoreDataErr error

	// restoredFrom is the snapshot the rollback imported, so a test can assert the
	// data went back to what was captured rather than merely that a call happened.
	restoredFrom pinstate.DataSnapshot

	snapshot  pinstate.DataSnapshot
	captured  Capture
	committed map[string]map[string]string
	previous  map[string]pinstate.Previous
}

func newRecorder() *recorder {
	return &recorder{
		proveCurrent:  Proof{Status: ProofPass},
		proveNew:      Proof{Status: ProofPass},
		proveRestored: Proof{Status: ProofPass},
		captured: Capture{
			Refs:   map[string]string{"qdrant": "old-qdrant"},
			Units:  map[string][]byte{"villa-qdrant.container": []byte("[Container]\nImage=old\n")},
			Config: "model = \"x\"\n",
		},
		snapshot: pinstate.DataSnapshot{
			Volume:  "villa-qdrant",
			Path:    "/data/villa/snapshots/memory-2026.tar",
			Bytes:   2_800_000_000,
			TakenAt: "2026-08-26T12:00:00Z",
		},
		committed: map[string]map[string]string{},
		previous:  map[string]pinstate.Previous{},
	}
}

func (r *recorder) log(step string) { r.steps = append(r.steps, step) }

func (r *recorder) deps() Deps {
	return Deps{
		ProveCurrent: func(context.Context, subsystem.Kind) Proof {
			r.log("prove-current")
			return r.proveCurrent
		},
		CaptureState: func(subsystem.Kind) (Capture, error) {
			r.log("capture")
			return r.captured, r.captureErr
		},
		Pull: func(context.Context, map[string]string) error {
			r.log("pull")
			return r.pullErr
		},
		Mutate: func(context.Context, subsystem.Kind, map[string]string) error {
			r.log("mutate")
			return r.mutateErr
		},
		Stop: func(context.Context, subsystem.Kind) error {
			r.log("stop")
			return r.stopErr
		},
		SnapshotData: func(context.Context, subsystem.Kind) (pinstate.DataSnapshot, error) {
			r.log("snapshot")
			return r.snapshot, r.snapshotErr
		},
		Start: func(context.Context, subsystem.Kind) error {
			r.log("start")
			return r.startErr
		},
		ProveNew: func(context.Context, subsystem.Kind) Proof {
			r.log("prove-new")
			return r.proveNew
		},
		Restore: func(context.Context, subsystem.Kind, Capture) error {
			r.log("restore")
			return r.restoreErr
		},
		RestoreData: func(_ context.Context, _ subsystem.Kind, snap pinstate.DataSnapshot) error {
			r.log("restore-data")
			r.restoredFrom = snap
			return r.restoreDataErr
		},
		ProveRestored: func(context.Context, subsystem.Kind) Proof {
			r.log("prove-restored")
			return r.proveRestored
		},
		Commit: func(k subsystem.Kind, refs map[string]string, prev pinstate.Previous) error {
			r.log("commit")
			r.committed[k.String()] = refs
			r.previous[k.String()] = prev
			return r.commitErr
		},
		Now: func() string { return "2026-08-26T12:00:00Z" },
	}
}

// memoryTarget is one subsystem's worth of work.
func memoryTarget() Target {
	return Target{
		Subsystem: subsystem.Memory,
		Pins:      map[string]string{"qdrant": "new-qdrant", "embedder": "new-embed"},
	}
}

// stepsEqual compares the recorded step sequence.
func stepsEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestHappyPathOrder pins the sequence, not just the outcome.
//
// PROVE-CURRENT COMES FIRST, before the capture. That ordering is the design: a
// pre-check after the capture would still work, but a pre-check after the MUTATION
// would be worthless, and only an explicit assertion stops the steps drifting.
//
// Memory owns persistent state, so its mutation is the stopped WINDOW: the snapshot
// falls between the stop and the mutate. A snapshot taken while the service runs is
// a torn copy, which is a safety net that only fails when used, so the position of
// that step inside the window is asserted rather than merely its presence.
func TestHappyPathOrder(t *testing.T) {
	r := newRecorder()
	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})

	want := []string{"prove-current", "capture", "pull", "stop", "snapshot", "mutate", "start", "prove-new", "commit"}
	if !stepsEqual(r.steps, want) {
		t.Errorf("steps = %v, want %v", r.steps, want)
	}
	if len(res.Subsystems) != 1 || res.Subsystems[0].Outcome != Committed {
		t.Fatalf("outcome = %+v, want committed", res.Subsystems)
	}
	if res.Halted {
		t.Error("a successful run reports halted")
	}
	if r.committed["memory"]["qdrant"] != "new-qdrant" {
		t.Errorf("the committed pins are %v", r.committed["memory"])
	}
}

// TestTheRetainedPreviousIsTheTupleNotABareDigest: a digest alone is not restorable
// weeks later, because the unit it ran under renders from a config that may have
// changed since. The captured tuple must reach the store intact.
func TestTheRetainedPreviousIsTheTupleNotABareDigest(t *testing.T) {
	r := newRecorder()
	Run(context.Background(), r.deps(), []Target{memoryTarget()})

	prev := r.previous["memory"]
	if len(prev.Refs) == 0 {
		t.Error("the retained previous carries no refs")
	}
	if string(prev.Units["villa-qdrant.container"]) != "[Container]\nImage=old\n" {
		t.Errorf("the verbatim prior unit bytes were lost: %q", prev.Units["villa-qdrant.container"])
	}
	if prev.Config == "" {
		t.Error("the config the pins were proven under was lost; a restore would reproduce today's render, not what was proven")
	}
	if prev.CapturedAt == "" {
		t.Error("the capture is undated")
	}
}

// TestAPreExistingFailureIsARefusalNotAnUpdateFailure is the distinction the
// pre-check exists to make.
//
// Without it villa would mutate an already-broken subsystem, fail, roll back, and
// report "the update failed" — blaming itself for someone else's outage. NOTHING
// may be mutated, and the outcome must be distinguishable from a post-mutation
// failure.
func TestAPreExistingFailureIsARefusalNotAnUpdateFailure(t *testing.T) {
	r := newRecorder()
	r.proveCurrent = Proof{Status: ProofFail, Detail: "verify memory: retrieval returned no citation"}

	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})

	if !stepsEqual(r.steps, []string{"prove-current"}) {
		t.Errorf("steps = %v; nothing beyond the pre-check may run", r.steps)
	}
	sr := res.Subsystems[0]
	if sr.Outcome != RefusedUnhealthy {
		t.Errorf("outcome = %q, want refused_unhealthy", sr.Outcome)
	}
	if sr.Outcome == RolledBackFail || sr.Outcome == RolledBackReject {
		t.Error("a pre-existing failure was reported as an update failure")
	}
	if sr.Proof.Detail == "" {
		t.Error("the refusal carries no detail, so the caller cannot say what was already wrong")
	}
	if !res.Halted {
		t.Error("a refusal did not halt the sequence")
	}
}

// TestAnUnprovableCurrentStateAlsoRefuses: a REJECT before the mutation refuses
// too. Villa cannot demonstrate the subsystem is healthy, and mutating something it
// cannot vouch for would make any later failure unattributable.
func TestAnUnprovableCurrentStateAlsoRefuses(t *testing.T) {
	r := newRecorder()
	r.proveCurrent = Proof{Status: ProofReject, Detail: "the proof timed out"}

	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})
	if res.Subsystems[0].Outcome != RefusedUnhealthy {
		t.Errorf("outcome = %q, want refused_unhealthy", res.Subsystems[0].Outcome)
	}
	if len(r.steps) != 1 {
		t.Errorf("steps = %v; nothing may run after an unprovable pre-check", r.steps)
	}
}

// TestAnUncapturableStateRefusesWithoutMutating: a subsystem whose prior state
// cannot be captured must not be touched, because there would be nothing to go
// back to.
func TestAnUncapturableStateRefusesWithoutMutating(t *testing.T) {
	r := newRecorder()
	r.captureErr = errors.New("unit file unreadable")

	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})
	if !stepsEqual(r.steps, []string{"prove-current", "capture"}) {
		t.Errorf("steps = %v; nothing may be mutated without a rollback point", r.steps)
	}
	sr := res.Subsystems[0]
	if sr.Outcome != RefusedUnhealthy || sr.FailedStep != "capture" {
		t.Errorf("outcome = %q / step = %q, want a capture refusal", sr.Outcome, sr.FailedStep)
	}
}

// TestAFailedPullRefusesWithoutRollback: a pull is not a mutation of the running
// stack. A pulled-but-unused image is inert, so a pull failure needs a refusal, not
// a rollback of something that never changed.
func TestAFailedPullRefusesWithoutRollback(t *testing.T) {
	r := newRecorder()
	r.pullErr = errors.New("network unreachable")

	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})
	if !stepsEqual(r.steps, []string{"prove-current", "capture", "pull"}) {
		t.Errorf("steps = %v; a failed pull must not mutate or roll back", r.steps)
	}
	if res.Subsystems[0].FailedStep != "pull" {
		t.Errorf("failed step = %q, want pull", res.Subsystems[0].FailedStep)
	}
}

// TestAPostMutationFailRollsBackAndIsDistinguishable: villa OBSERVED something
// broken. The wording a user reads is confident, and the outcome says so.
func TestAPostMutationFailRollsBackAndIsDistinguishable(t *testing.T) {
	r := newRecorder()
	r.proveNew = Proof{Status: ProofFail, Detail: "verify memory: upload succeeded, retrieval returned no citation"}

	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})

	// Memory owns persistent state, so the rollback restores the DATA as well as
	// the pin, and does it inside its own stopped window. The retained image alone
	// could not undo a schema the update migrated forward.
	want := []string{"prove-current", "capture", "pull", "stop", "snapshot", "mutate", "start", "prove-new",
		"stop", "restore-data", "restore", "start", "prove-restored"}
	if !stepsEqual(r.steps, want) {
		t.Errorf("steps = %v, want %v", r.steps, want)
	}
	sr := res.Subsystems[0]
	if sr.Outcome != RolledBackFail {
		t.Errorf("outcome = %q, want rolled_back_fail", sr.Outcome)
	}
	if sr.RollbackIncomplete {
		t.Error("a clean rollback was reported as incomplete")
	}
	if len(r.committed) != 0 {
		t.Error("a failed subsystem committed a pin")
	}
}

// TestAPostMutationRejectRollsBackAndIsNOTAFail is the marker-drift case ADR-0001
// predicted, and the reason Reject exists as a separate outcome.
//
// An image update is exactly when the log format drifts, so a new image may be
// PERFECTLY GOOD YET UNPROVABLE. Both outcomes roll back, so only the wording
// differs — and the difference is the whole point: villa must not tell a user that
// upstream shipped a broken image when what actually happened is that villa could
// not tell.
func TestAPostMutationRejectRollsBackAndIsNOTAFail(t *testing.T) {
	r := newRecorder()
	r.proveNew = Proof{Status: ProofReject, Detail: "residency markers not found in the startup log"}

	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})
	sr := res.Subsystems[0]

	if sr.Outcome != RolledBackReject {
		t.Errorf("outcome = %q, want rolled_back_reject", sr.Outcome)
	}
	if sr.Outcome == RolledBackFail {
		t.Error("an unprovable image was reported as a broken one; only one of those was observed")
	}
	// It still rolls back. A transaction cannot commit on evidence it does not have.
	// Memory owns persistent state, so its rollback closes with the start that
	// reopens the window's services before the re-proof observes them.
	if !stepsEqual(r.steps[len(r.steps)-2:], []string{"start", "prove-restored"}) {
		t.Errorf("a Reject did not roll back: %v", r.steps)
	}
	if len(r.committed) != 0 {
		t.Error("a Reject committed a pin")
	}
}

// TestATimeoutIsARejectNotAFail: "did not become healthy within 90s" is a different
// claim from "is broken", and only the first was observed. Both roll back, so the
// only thing at stake is what villa tells the user — which is the thing that
// matters.
func TestATimeoutIsARejectNotAFail(t *testing.T) {
	r := newRecorder()
	r.proveNew = Proof{Status: ProofReject, Detail: "did not become healthy within 90s"}

	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})
	if got := res.Subsystems[0].Outcome; got != RolledBackReject {
		t.Errorf("a timeout produced %q, want rolled_back_reject — villa observed slowness, not brokenness", got)
	}
}

// TestAMutateErrorRollsBack: any failure after the mutation puts things back.
func TestAMutateErrorRollsBack(t *testing.T) {
	r := newRecorder()
	r.mutateErr = errors.New("systemctl restart failed")

	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})
	sr := res.Subsystems[0]
	if sr.Outcome != RolledBackReject || sr.FailedStep != "mutate" {
		t.Errorf("outcome = %q / step = %q", sr.Outcome, sr.FailedStep)
	}
	if !stepsEqual(r.steps[len(r.steps)-3:], []string{"restore", "start", "prove-restored"}) {
		t.Errorf("a mutate failure did not roll back: %v", r.steps)
	}
}

// TestAFailedRestoreIsReportedAsIncomplete is ADR-0003's honesty requirement. The
// worst state in the whole flow must never be reported as a clean rollback.
func TestAFailedRestoreIsReportedAsIncomplete(t *testing.T) {
	r := newRecorder()
	r.proveNew = Proof{Status: ProofFail}
	r.restoreErr = errors.New("could not write the prior unit")

	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})
	sr := res.Subsystems[0]

	if !sr.RollbackIncomplete {
		t.Error("a failed restore was reported as a clean rollback")
	}
	if sr.Err == nil {
		t.Error("a failed restore carries no error")
	}
	// The re-proof is skipped: there is nothing restored to prove.
	for _, step := range r.steps {
		if step == "prove-restored" {
			t.Error("villa re-proved a restore that failed")
		}
	}
}

// TestAnUnprovableRestoreIsAlsoIncomplete: villa put the bytes back but cannot show
// the subsystem is working. Claiming a clean rollback would assert exactly the kind
// of thing this package refuses to assert without evidence.
func TestAnUnprovableRestoreIsAlsoIncomplete(t *testing.T) {
	r := newRecorder()
	r.proveNew = Proof{Status: ProofFail}
	r.proveRestored = Proof{Status: ProofReject, Detail: "the restored server did not come back within the budget"}

	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})
	if !res.Subsystems[0].RollbackIncomplete {
		t.Error("a restore that could not be proven was reported as a clean rollback")
	}
}

// TestTheFirstFailureHaltsAndTheRestAreReportedAsNotTried.
//
// "What state am I in?" is the question a user asks immediately after a halt, and
// it cannot be answered by a list that omits the untried. An absent subsystem reads
// as "fine", which is the one thing villa cannot say about one it never looked at.
func TestTheFirstFailureHaltsAndTheRestAreReportedAsNotTried(t *testing.T) {
	r := newRecorder()

	// Inference commits; chat fails; memory and agent are never tried.
	calls := 0
	d := r.deps()
	base := d.ProveNew
	d.ProveNew = func(ctx context.Context, k subsystem.Kind) Proof {
		calls++
		if k == subsystem.Chat {
			return Proof{Status: ProofFail, Detail: "the chat probes did not pass"}
		}
		return base(ctx, k)
	}

	targets := []Target{
		{Subsystem: subsystem.Inference, Pins: map[string]string{"backend-vulkan-radv": "new"}},
		{Subsystem: subsystem.Chat, Pins: map[string]string{"open-webui": "new"}},
		{Subsystem: subsystem.Memory, Pins: map[string]string{"qdrant": "new"}},
		{Subsystem: subsystem.Agent, Pins: map[string]string{"crush": "new"}},
	}
	res := Run(context.Background(), d, targets)

	if !res.Halted {
		t.Fatal("the run did not halt")
	}
	if len(res.Subsystems) != 4 {
		t.Fatalf("%d subsystems reported, want all 4 including the untried", len(res.Subsystems))
	}
	want := []Outcome{Committed, RolledBackFail, NotTried, NotTried}
	for i, w := range want {
		if res.Subsystems[i].Outcome != w {
			t.Errorf("subsystem %d (%v) = %q, want %q", i, res.Subsystems[i].Subsystem, res.Subsystems[i].Outcome, w)
		}
	}
	// The already-committed subsystem STAYS committed. Reverting four good updates
	// because a fifth failed is what per-subsystem sequencing exists to avoid.
	if res.CommittedCount() != 1 {
		t.Errorf("%d subsystems committed, want the one that passed before the halt", res.CommittedCount())
	}
	if r.committed["inference"] == nil {
		t.Error("the subsystem that passed before the halt lost its commit")
	}
}

// TestPerSubsystemBudgetsAreUsed: a total cap would make failures depend on
// ordering, so the last subsystem gets blamed for time the first four spent. Each
// subsystem must get its own context.
func TestPerSubsystemBudgetsAreUsed(t *testing.T) {
	r := newRecorder()
	d := r.deps()

	var budgeted []subsystem.Kind
	d.Budget = func(c context.Context, k subsystem.Kind) (context.Context, context.CancelFunc) {
		budgeted = append(budgeted, k)
		return context.WithCancel(c)
	}

	targets := []Target{
		{Subsystem: subsystem.Inference, Pins: map[string]string{"backend-vulkan-radv": "new"}},
		{Subsystem: subsystem.Chat, Pins: map[string]string{"open-webui": "new"}},
	}
	Run(context.Background(), d, targets)

	if len(budgeted) != 2 {
		t.Errorf("%d budgets were taken for 2 subsystems; each needs its own or the last is blamed for the first's time", len(budgeted))
	}
}

// TestASubsystemWithNothingToDoIsNotTouched: an already-current subsystem must not
// be proven, captured or restarted. Proving it would spend the expensive residency
// budget to learn nothing.
func TestASubsystemWithNothingToDoIsNotTouched(t *testing.T) {
	r := newRecorder()
	res := Run(context.Background(), r.deps(), []Target{{Subsystem: subsystem.Memory}})

	if len(r.steps) != 0 {
		t.Errorf("steps = %v; an already-current subsystem must not be touched", r.steps)
	}
	if res.Subsystems[0].Outcome != NothingToDo {
		t.Errorf("outcome = %q, want nothing_to_do", res.Subsystems[0].Outcome)
	}
	if res.Halted {
		t.Error("nothing-to-do halted the sequence")
	}
}

// TestACommitFailureDoesNotUndoAProvenGoodState.
//
// The stack is running the new pins and they were PROVEN. Rolling back over a
// bookkeeping failure would undo a demonstrably good state, leaving the host on
// something unproven so that villa's record could be tidy. Reporting instead leaves
// the host on something good and villa's record behind, which is the right way
// round.
func TestACommitFailureDoesNotUndoAProvenGoodState(t *testing.T) {
	r := newRecorder()
	r.commitErr = errors.New("pin state store unwritable")

	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})
	sr := res.Subsystems[0]

	if sr.Outcome != Committed {
		t.Errorf("outcome = %q; the proven state is running and must not be undone by a bookkeeping failure", sr.Outcome)
	}
	if sr.Err == nil || sr.FailedStep != "commit" {
		t.Errorf("the commit failure is not surfaced: err=%v step=%q", sr.Err, sr.FailedStep)
	}
	for _, step := range r.steps {
		if step == "restore" {
			t.Error("a commit failure rolled back a proven good state")
		}
	}
}

// TestTheCoreIsPure is a structural assertion. The whole value of this package is
// that every path is drivable from a test, and one os/exec call would end that.
func TestTheCoreIsPure(t *testing.T) {
	src := readSource(t, "updateflow.go")
	for _, forbidden := range []string{"os/exec", "net/http", "os.ReadFile", "os.WriteFile", "exec.Command"} {
		if containsCode(src, forbidden) {
			t.Errorf("updateflow.go references %q; every host effect must be an injected seam or the failure paths stop being testable", forbidden)
		}
	}
}

// readSource reads a file in this package.
func readSource(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

// containsCode reports whether a token appears outside a comment, so a doc comment
// may name a forbidden symbol freely.
func containsCode(src, token string) bool {
	for _, line := range strings.Split(src, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		if strings.Contains(line, token) {
			return true
		}
	}
	return false
}
