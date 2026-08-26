package updateflow

// snapshot_test.go drives the stopped window: the half of the transaction that
// exists only for a subsystem whose DATA, not whose image, is the state being
// changed.
//
// The rows that matter are ORDERING rows. A snapshot taken while the service is
// running is a torn copy, and a torn copy is a safety net that only fails when used
// — so "a snapshot happened" is not the assertion. "The snapshot happened between
// the stop and the mutate" is.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// chatTarget is the subsystem the incident happened to: Open WebUI, whose SQLite
// schema a real update migrated forward.
func chatTarget() Target {
	return Target{
		Subsystem: subsystem.Chat,
		Pins:      map[string]string{"open-webui": "new-owui"},
	}
}

// inferenceTarget is the stateless comparison: the backend's image IS the state
// being changed, so it must keep the single atomic restart.
func inferenceTarget() Target {
	return Target{
		Subsystem: subsystem.Inference,
		Pins:      map[string]string{"backend-vulkan-radv": "new-backend"},
	}
}

// indexOf reports where a step ran, or -1.
func indexOf(steps []string, step string) int {
	for i, s := range steps {
		if s == step {
			return i
		}
	}
	return -1
}

// TestTheSnapshotIsTakenInsideTheStoppedWindow is the ordering assertion this whole
// ticket exists for.
//
// `villa backup` already stops Open WebUI before exporting its volume, describing
// the result as "a clean SQLite copy". Exporting from under a running service
// produces a torn one, which restores into a database that opens and is wrong.
// Asserting only that a snapshot was taken would pass against exactly that bug.
func TestTheSnapshotIsTakenInsideTheStoppedWindow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target Target
	}{
		{"chat", chatTarget()},
		{"memory", memoryTarget()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRecorder()
			Run(context.Background(), r.deps(), []Target{tc.target})

			stop, snap := indexOf(r.steps, "stop"), indexOf(r.steps, "snapshot")
			mutate, start := indexOf(r.steps, "mutate"), indexOf(r.steps, "start")
			if stop < 0 || snap < 0 || mutate < 0 || start < 0 {
				t.Fatalf("steps = %v; the stopped window is missing a step", r.steps)
			}
			if !(stop < snap) {
				t.Errorf("steps = %v: the snapshot was taken before the stop, so it is a torn copy of a running service", r.steps)
			}
			if !(snap < mutate) {
				t.Errorf("steps = %v: the data was mutated before it was snapshotted — the rollback target is the migrated data, not the original", r.steps)
			}
			if !(mutate < start) {
				t.Errorf("steps = %v: the services were started before the mutation landed", r.steps)
			}
		})
	}
}

// TestAStatelessSubsystemKeepsTheAtomicRestart: inference gets no stopped window.
//
// It buys nothing there — the backend's image is the state being changed, so the
// retained digest is a complete rollback target — and it would double the most
// expensive restart in the stack.
func TestAStatelessSubsystemKeepsTheAtomicRestart(t *testing.T) {
	r := newRecorder()
	res := Run(context.Background(), r.deps(), []Target{inferenceTarget()})

	want := []string{"prove-current", "capture", "pull", "mutate", "prove-new", "commit"}
	if !stepsEqual(r.steps, want) {
		t.Errorf("steps = %v, want %v — a stateless subsystem must not gain a stopped window", r.steps, want)
	}
	if res.Subsystems[0].Outcome != Committed {
		t.Errorf("outcome = %q, want committed", res.Subsystems[0].Outcome)
	}
	if prev := r.previous["inference"]; prev.Data.Taken() {
		t.Errorf("a stateless subsystem recorded a data snapshot: %+v", prev.Data)
	}
}

// TestAFailedSnapshotRefusesWithNothingMutatedAndTheServiceRunning is the accepted
// cost, asserted so nobody softens it later.
//
// Mutating state villa could not snapshot is precisely what produced the incident,
// so a full disk or an unavailable Podman BLOCKS updating a stateful subsystem. The
// second half matters just as much: the stop was villa's, so the services must come
// back up or a refusal becomes an outage.
func TestAFailedSnapshotRefusesWithNothingMutatedAndTheServiceRunning(t *testing.T) {
	r := newRecorder()
	r.snapshotErr = errors.New("no space left on device")

	res := Run(context.Background(), r.deps(), []Target{chatTarget()})

	want := []string{"prove-current", "capture", "pull", "stop", "snapshot", "start"}
	if !stepsEqual(r.steps, want) {
		t.Errorf("steps = %v, want %v", r.steps, want)
	}
	if indexOf(r.steps, "mutate") >= 0 {
		t.Error("the subsystem was mutated despite an uncapturable data volume")
	}
	sr := res.Subsystems[0]
	if sr.Outcome != RefusedUnhealthy {
		t.Errorf("outcome = %q, want refused_unhealthy — a failed snapshot is a REFUSAL, not a warning", sr.Outcome)
	}
	if sr.FailedStep != "snapshot" {
		t.Errorf("failed step = %q, want snapshot", sr.FailedStep)
	}
	if sr.RollbackIncomplete {
		t.Error("a refusal whose services came back up was reported as incomplete")
	}
	if len(r.committed) != 0 {
		t.Error("a refused subsystem committed a pin")
	}
	if !strings.Contains(sr.Proof.Detail, "nothing was changed") {
		t.Errorf("the refusal does not say nothing was changed: %q", sr.Proof.Detail)
	}
}

// TestAFailedStartAfterAFailedSnapshotIsSurfaced: villa stopped the services, so a
// stop it cannot undo leaves the subsystem DOWN and villa put it there.
//
// That is worse than the refusal it accompanies, and it must not be swallowed by
// the refusal's wording.
func TestAFailedStartAfterAFailedSnapshotIsSurfaced(t *testing.T) {
	r := newRecorder()
	r.snapshotErr = errors.New("no space left on device")
	r.startErr = errors.New("systemctl: unit failed to start")

	res := Run(context.Background(), r.deps(), []Target{chatTarget()})
	sr := res.Subsystems[0]

	if !sr.RollbackIncomplete {
		t.Error("a subsystem villa stopped and could not start again was reported as cleanly refused")
	}
	if sr.Err == nil || !strings.Contains(sr.Err.Error(), "STILL STOPPED") {
		t.Errorf("the error does not say the services are still stopped: %v", sr.Err)
	}
	if sr.Err == nil || !strings.Contains(sr.Err.Error(), "no space left on device") {
		t.Errorf("the original cause was lost: %v", sr.Err)
	}
}

// TestAFailedStopRefusesWithoutSnapshottingOrMutating: no window, no snapshot, no
// mutation. Villa still calls start, because a partial stop may have taken some of
// the subsystem's services down.
func TestAFailedStopRefusesWithoutSnapshottingOrMutating(t *testing.T) {
	r := newRecorder()
	r.stopErr = errors.New("systemctl: job timed out")

	res := Run(context.Background(), r.deps(), []Target{memoryTarget()})

	want := []string{"prove-current", "capture", "pull", "stop", "start"}
	if !stepsEqual(r.steps, want) {
		t.Errorf("steps = %v, want %v", r.steps, want)
	}
	sr := res.Subsystems[0]
	if sr.Outcome != RefusedUnhealthy {
		t.Errorf("outcome = %q, want refused_unhealthy", sr.Outcome)
	}
	if sr.FailedStep != "stop" {
		t.Errorf("failed step = %q, want stop", sr.FailedStep)
	}
}

// TestASnapshotIsRecordedBesideTheDigestAndUnits: the retained tuple grows a fourth
// member for a stateful subsystem.
//
// A digest plus unit bytes is a complete rollback target only when the image is the
// state being changed. For chat it is not, and the snapshot is what makes the tuple
// restorable at all.
func TestASnapshotIsRecordedBesideTheDigestAndUnits(t *testing.T) {
	r := newRecorder()
	r.snapshot = pinstate.DataSnapshot{
		Volume:  "villa-openwebui",
		Path:    "/data/villa/snapshots/chat.tar",
		Bytes:   267_000_000,
		TakenAt: "2026-08-26T12:00:00Z",
	}

	Run(context.Background(), r.deps(), []Target{chatTarget()})

	prev := r.previous["chat"]
	if !prev.Data.Taken() {
		t.Fatal("the retained tuple carries no data snapshot; the retained image alone cannot roll back a migrated schema")
	}
	if prev.Data.Volume != "villa-openwebui" {
		t.Errorf("the snapshot names volume %q; a restore would import into the wrong volume", prev.Data.Volume)
	}
	if prev.Data.Path != "/data/villa/snapshots/chat.tar" {
		t.Errorf("the snapshot path was lost: %q", prev.Data.Path)
	}
	if prev.Data.Bytes == 0 {
		t.Error("the snapshot's disk cost was not recorded, so nobody can see what it spent")
	}
	// The rest of the tuple survives intact: the snapshot is an addition, not a
	// replacement. Restoring data without the pin and unit it was proven against
	// would restore a combination nobody proved.
	if len(prev.Refs) == 0 || len(prev.Units) == 0 || prev.Config == "" {
		t.Errorf("the snapshot displaced part of the tuple: %+v", prev)
	}
}

// TestAFailedMutationInsideTheWindowStartsTheServicesBeforeRollingBack.
//
// The rollback re-proves the restored state, and a proof cannot observe a stopped
// service. Leaving the subsystem down would turn every rollback inside the window
// into a rollback-incomplete for a reason that had nothing to do with the restore.
func TestAFailedMutationInsideTheWindowStartsTheServicesBeforeRollingBack(t *testing.T) {
	r := newRecorder()
	r.mutateErr = errors.New("render failed")

	res := Run(context.Background(), r.deps(), []Target{chatTarget()})

	want := []string{"prove-current", "capture", "pull", "stop", "snapshot", "mutate", "start", "restore", "prove-restored"}
	if !stepsEqual(r.steps, want) {
		t.Errorf("steps = %v, want %v", r.steps, want)
	}
	if res.Subsystems[0].Outcome != RolledBackReject {
		t.Errorf("outcome = %q, want rolled_back_reject", res.Subsystems[0].Outcome)
	}
}

// TestAFailedStartAfterTheMutationRollsBack: the mutation landed but the subsystem
// is down, so the post-mutation proof cannot run.
//
// That is a REJECT, not a Fail. Villa observed that it could not start the
// subsystem, which is not the same claim as "the new image is broken".
func TestAFailedStartAfterTheMutationRollsBack(t *testing.T) {
	r := newRecorder()
	r.startErr = errors.New("systemctl: unit failed to start")

	res := Run(context.Background(), r.deps(), []Target{chatTarget()})
	sr := res.Subsystems[0]

	if sr.Outcome != RolledBackReject {
		t.Errorf("outcome = %q, want rolled_back_reject", sr.Outcome)
	}
	if sr.FailedStep != "start" {
		t.Errorf("failed step = %q, want start", sr.FailedStep)
	}
	if indexOf(r.steps, "prove-new") >= 0 {
		t.Error("villa proved a subsystem it had just failed to start")
	}
	if indexOf(r.steps, "restore") < 0 {
		t.Errorf("steps = %v; a failed start after the mutation must roll back", r.steps)
	}
}

// TestAStatefulSubsystemWithNoWindowWiredRefuses is the fail-closed guard on the
// seam set itself.
//
// A stateful subsystem whose snapshot seam is absent would otherwise fall through
// to the stateless path and mutate data it never captured — the exact shape of the
// incident, reintroduced by an omission rather than a decision.
func TestAStatefulSubsystemWithNoWindowWiredRefuses(t *testing.T) {
	r := newRecorder()
	d := r.deps()
	d.SnapshotData = nil

	res := Run(context.Background(), d, []Target{chatTarget()})
	sr := res.Subsystems[0]

	if sr.Outcome != RefusedUnhealthy {
		t.Errorf("outcome = %q, want refused_unhealthy", sr.Outcome)
	}
	if indexOf(r.steps, "mutate") >= 0 {
		t.Error("a subsystem with no snapshot seam was mutated anyway")
	}
}

// TestEveryStatefulSubsystemGetsTheWindow walks the declaration rather than naming
// the pair, so a sixth subsystem that gains a volume cannot quietly skip the
// snapshot.
func TestEveryStatefulSubsystemGetsTheWindow(t *testing.T) {
	for _, k := range subsystem.Stateful() {
		t.Run(k.String(), func(t *testing.T) {
			r := newRecorder()
			Run(context.Background(), r.deps(), []Target{{Subsystem: k, Pins: map[string]string{"c": "new"}}})
			if indexOf(r.steps, "snapshot") < 0 {
				t.Errorf("%v owns persistent state but its update took no snapshot: %v", k, r.steps)
			}
		})
	}
	for _, k := range subsystem.Every {
		if k.OwnsPersistentState() {
			continue
		}
		r := newRecorder()
		Run(context.Background(), r.deps(), []Target{{Subsystem: k, Pins: map[string]string{"c": "new"}}})
		if indexOf(r.steps, "stop") >= 0 {
			t.Errorf("%v owns no persistent state but was stopped: %v", k, r.steps)
		}
	}
}
