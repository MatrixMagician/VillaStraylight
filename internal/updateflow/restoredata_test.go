package updateflow

// restoredata_test.go drives the half of the lifecycle that would have prevented
// the incident: a rollback that puts the DATA back, not only the pin.
//
// On hardware villa got the hard part right. It refused to commit on unprovable
// evidence, rolled back the pin, re-proved, and honestly reported ROLLBACK
// INCOMPLETE. What it could not do was undo the data migration, because it had
// never captured the data. These rows are that rollback completing.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// TestARollbackRestoresTheDataThePinWasProvenAgainst.
//
// The pin, the unit bytes and the config were already restored. Restoring them
// onto MIGRATED data restores a combination nobody ever proved — the old digest on
// a database it can no longer read.
func TestARollbackRestoresTheDataThePinWasProvenAgainst(t *testing.T) {
	r := newRecorder()
	r.snapshot = pinstate.DataSnapshot{
		Volume: "villa-openwebui",
		Path:   "/snap/chat.tar",
		Bytes:  267_000_000,
	}
	r.proveNew = Proof{Status: ProofFail, Detail: "the chat probes did not pass"}

	res := Run(context.Background(), r.deps(), []Target{chatTarget()})
	sr := res.Subsystems[0]

	if indexOf(r.steps, "restore-data") < 0 {
		t.Fatalf("steps = %v; the rollback left the migrated data in place", r.steps)
	}
	if r.restoredFrom.Path != "/snap/chat.tar" {
		t.Errorf("the rollback imported %+v, not the snapshot the capture took", r.restoredFrom)
	}
	if r.restoredFrom.Volume != "villa-openwebui" {
		t.Errorf("the rollback imported into %q rather than the volume the snapshot came from", r.restoredFrom.Volume)
	}
	if sr.RollbackIncomplete {
		t.Errorf("a complete rollback was reported as incomplete: %v", sr.Err)
	}
	if sr.Outcome != RolledBackFail {
		t.Errorf("outcome = %q, want rolled_back_fail", sr.Outcome)
	}
}

// TestTheDataIsRestoredWhileTheSubsystemIsStopped is the ordering assertion.
//
// Importing into a volume a running service holds open gives a half-restored
// database — the exact state this lifecycle exists to avoid. The restore mirrors
// the capture for the same reason the capture stops first.
func TestTheDataIsRestoredWhileTheSubsystemIsStopped(t *testing.T) {
	r := newRecorder()
	r.proveNew = Proof{Status: ProofFail, Detail: "probe failed"}

	Run(context.Background(), r.deps(), []Target{chatTarget()})

	// The rollback's own window: the LAST stop, the data restore, the pin restore,
	// then the start that closes it before the re-proof.
	tail := r.steps[len(r.steps)-5:]
	want := []string{"stop", "restore-data", "restore", "start", "prove-restored"}
	if !stepsEqual(tail, want) {
		t.Errorf("the rollback tail = %v, want %v", tail, want)
	}

	restoreData := indexOf(r.steps, "restore-data")
	lastStop := -1
	lastStart := -1
	for i, s := range r.steps {
		if s == "stop" && i < restoreData {
			lastStop = i
		}
		if s == "start" && i > restoreData {
			lastStart = i
			break
		}
	}
	if lastStop < 0 || lastStop >= restoreData {
		t.Errorf("steps = %v: the data was imported into a running service, which half-restores it", r.steps)
	}
	if lastStart < 0 || restoreData >= lastStart {
		t.Errorf("steps = %v: the services were started before the data was restored", r.steps)
	}
}

// TestTheDataIsRestoredBeforeTheReProof.
//
// The re-proof is what makes "rolled back" a demonstrated claim rather than an
// assumption. Proving before the data went back would prove the migrated data,
// which is the claim the incident's output made and could not support.
func TestTheDataIsRestoredBeforeTheReProof(t *testing.T) {
	r := newRecorder()
	r.proveNew = Proof{Status: ProofReject, Detail: "the probes could not be evaluated"}

	Run(context.Background(), r.deps(), []Target{memoryTarget()})

	restoreData, reProof := indexOf(r.steps, "restore-data"), indexOf(r.steps, "prove-restored")
	if restoreData < 0 || reProof < 0 {
		t.Fatalf("steps = %v", r.steps)
	}
	if restoreData >= reProof {
		t.Errorf("steps = %v: the restored state was proven before its data went back, so the proof observed the migrated data", r.steps)
	}
}

// TestARestoredVolumeThatStillFailsItsProofIsRollbackIncomplete.
//
// A data rollback nobody proved is the same unfounded claim the incident produced.
// Putting the bytes back is not the same as showing the subsystem works.
func TestARestoredVolumeThatStillFailsItsProofIsRollbackIncomplete(t *testing.T) {
	r := newRecorder()
	r.proveNew = Proof{Status: ProofFail, Detail: "probe failed"}
	r.proveRestored = Proof{Status: ProofFail, Detail: "the restored chat still fails its probes"}

	res := Run(context.Background(), r.deps(), []Target{chatTarget()})
	sr := res.Subsystems[0]

	if !sr.RollbackIncomplete {
		t.Error("a restored state that could not be proven was reported as a clean rollback")
	}
	if sr.Err == nil || !strings.Contains(sr.Err.Error(), "could not be proven") {
		t.Errorf("the incomplete rollback does not say the restored state was unprovable: %v", sr.Err)
	}
}

// TestAFailedVolumeRestoreIsTheWorstStateAndSaysSo.
//
// The data is whatever the failed import left and villa cannot say what that is.
// It must never read as a clean rollback, and villa must not claim the stack is
// untouched (ADR-0003).
func TestAFailedVolumeRestoreIsTheWorstStateAndSaysSo(t *testing.T) {
	r := newRecorder()
	r.proveNew = Proof{Status: ProofFail, Detail: "probe failed"}
	r.restoreDataErr = errors.New("podman volume import: no such volume")

	res := Run(context.Background(), r.deps(), []Target{chatTarget()})
	sr := res.Subsystems[0]

	if !sr.RollbackIncomplete {
		t.Fatal("a failed volume restore was reported as a clean rollback")
	}
	if sr.Err == nil || !strings.Contains(sr.Err.Error(), "data volume could not be restored") {
		t.Errorf("the error does not name the failed data restore: %v", sr.Err)
	}
	// The pin restore is not attempted after the data restore failed: putting the
	// old digest back onto unknown data is what produced the crash loop.
	if indexOf(r.steps, "restore") >= 0 {
		t.Errorf("steps = %v: the pin was restored onto data villa could not put back", r.steps)
	}
	// Villa opened the window, so it closes it even on this path.
	if r.steps[len(r.steps)-1] != "start" {
		t.Errorf("steps = %v: the subsystem was left stopped after a failed data restore", r.steps)
	}
}

// TestAFailedStopBeforeTheDataRestoreRefusesToImport.
//
// A merge into a live volume is worse than the migrated volume it would merge
// into: the migrated one is at least self-consistent. So villa declines the import
// and says the data was left as the update made it.
func TestAFailedStopBeforeTheDataRestoreRefusesToImport(t *testing.T) {
	r := newRecorder()
	r.proveNew = Proof{Status: ProofFail, Detail: "probe failed"}

	// The window's first stop succeeds; the rollback's stop fails.
	stops := 0
	d := r.deps()
	d.Stop = func(context.Context, subsystem.Kind) error {
		stops++
		r.log("stop")
		if stops > 1 {
			return errors.New("systemctl: job timed out")
		}
		return nil
	}

	res := Run(context.Background(), d, []Target{chatTarget()})
	sr := res.Subsystems[0]

	if indexOf(r.steps, "restore-data") >= 0 {
		t.Error("the data was imported into a service villa could not stop")
	}
	if !sr.RollbackIncomplete {
		t.Error("a rollback that could not restore the data was reported as complete")
	}
	if sr.Err == nil || !strings.Contains(sr.Err.Error(), "left as the update made it") {
		t.Errorf("the error does not say what state the data is in: %v", sr.Err)
	}
}

// TestAStatelessSubsystemsRollbackIsUnchanged: no data window, no import.
//
// Inference's image IS the state being changed, so the retained digest and unit
// bytes are a complete rollback target on their own.
func TestAStatelessSubsystemsRollbackIsUnchanged(t *testing.T) {
	r := newRecorder()
	r.proveNew = Proof{Status: ProofFail, Detail: "the residency proof failed"}

	res := Run(context.Background(), r.deps(), []Target{inferenceTarget()})

	want := []string{"prove-current", "capture", "pull", "mutate", "prove-new", "restore", "prove-restored"}
	if !stepsEqual(r.steps, want) {
		t.Errorf("steps = %v, want %v — a stateless rollback must not gain a data restore", r.steps, want)
	}
	if res.Subsystems[0].Outcome != RolledBackFail {
		t.Errorf("outcome = %q, want rolled_back_fail", res.Subsystems[0].Outcome)
	}
}

// TestTheIncidentReproduced is the regression test for what actually happened on
// gfx1151, driven end to end with no live host.
//
// A `villa update chat` migrates Open WebUI's SQLite config table from
// id/data/version to key/value. The new image cannot be proven. Villa rolls back —
// and BEFORE this ticket the old digest went back onto a migrated database, which
// crash-looped 24 times.
//
// The fake volume here is a single string standing in for the schema, which is
// enough to distinguish the three outcomes that matter: reverted, still migrated,
// or a blend of both.
func TestTheIncidentReproduced(t *testing.T) {
	const (
		oldSchema = "config(id,data,version)"
		newSchema = "config(key,value)"
	)

	// volume is the data on disk. The update migrates it forward; only a genuine
	// restore puts it back.
	volume := oldSchema
	// snapshotted is what the export captured, taken while the service was stopped.
	var snapshotted string
	running := true

	r := newRecorder()
	d := r.deps()

	d.Stop = func(context.Context, subsystem.Kind) error {
		running = false
		r.log("stop")
		return nil
	}
	d.Start = func(context.Context, subsystem.Kind) error {
		running = true
		r.log("start")
		return nil
	}
	d.SnapshotData = func(context.Context, subsystem.Kind) (pinstate.DataSnapshot, error) {
		r.log("snapshot")
		if running {
			// A torn copy. The export must happen inside the stopped window.
			return pinstate.DataSnapshot{}, errors.New("exported while the service was running")
		}
		snapshotted = volume
		return pinstate.DataSnapshot{Volume: "villa-openwebui", Path: "/snap/chat.tar", Bytes: 267_000_000}, nil
	}
	d.Mutate = func(context.Context, subsystem.Kind, map[string]string) error {
		r.log("mutate")
		// The new image migrates the schema forward on first start. This is the
		// step that makes the retained image useless as a rollback target.
		volume = newSchema
		return nil
	}
	d.ProveNew = func(context.Context, subsystem.Kind) Proof {
		r.log("prove-new")
		return Proof{Status: ProofReject, Detail: "the chat probes could not be evaluated"}
	}
	d.RestoreData = func(_ context.Context, _ subsystem.Kind, snap pinstate.DataSnapshot) error {
		r.log("restore-data")
		if running {
			return errors.New("imported into a running service")
		}
		if snap.Path != "/snap/chat.tar" {
			return errors.New("imported the wrong snapshot")
		}
		// REPLACE, not merge. `podman volume import` merges into existing
		// contents, so the live seam clean-recreates the volume first; the fake
		// models that by assigning rather than appending. A merge here would leave
		// oldSchema+newSchema, which is worse than either.
		volume = snapshotted
		return nil
	}
	d.ProveRestored = func(context.Context, subsystem.Kind) Proof {
		r.log("prove-restored")
		if volume != oldSchema {
			// The proof observes the DATA, which is the whole point: a rollback
			// that left the schema migrated cannot pass.
			return Proof{Status: ProofFail, Detail: "the restored chat cannot read its database"}
		}
		return Proof{Status: ProofPass}
	}

	res := Run(context.Background(), d, []Target{chatTarget()})
	sr := res.Subsystems[0]

	if volume != oldSchema {
		t.Errorf("the data is %q after the rollback, want %q — the migration was not undone", volume, oldSchema)
	}
	if strings.Contains(volume, "key,value") && strings.Contains(volume, "id,data") {
		t.Errorf("the volume holds a blend of both schemas: %q", volume)
	}
	if !running {
		t.Error("the subsystem was left stopped")
	}
	if sr.Outcome != RolledBackReject {
		t.Errorf("outcome = %q, want rolled_back_reject — the image may be fine and villa cannot show it", sr.Outcome)
	}
	if sr.RollbackIncomplete {
		t.Errorf("the rollback that recovered the data reported itself incomplete: %v", sr.Err)
	}
	if len(r.committed) != 0 {
		t.Error("an unprovable update committed a pin")
	}
}

// TestEveryStatefulSubsystemRestoresItsData walks the declaration, so a sixth
// subsystem that gains a volume cannot quietly roll back the pin alone.
func TestEveryStatefulSubsystemRestoresItsData(t *testing.T) {
	for _, k := range subsystem.Stateful() {
		t.Run(k.String(), func(t *testing.T) {
			r := newRecorder()
			r.proveNew = Proof{Status: ProofFail, Detail: "probe failed"}
			Run(context.Background(), r.deps(), []Target{{Subsystem: k, Pins: map[string]string{"c": "new"}}})
			if indexOf(r.steps, "restore-data") < 0 {
				t.Errorf("%v owns persistent state but its rollback restored only the pin: %v", k, r.steps)
			}
		})
	}
}
