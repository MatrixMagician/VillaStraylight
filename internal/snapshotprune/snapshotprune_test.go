// Package snapshotprune tests are about the one thing that can go badly wrong
// here: this is the only code in the project that deletes a data snapshot, so
// every test below is ultimately asking "could this delete the rollback target the
// stack would actually need?".
//
// The sharp case is the retained one. A snapshot the current tuple points at is the
// live rollback target, and removing it converts rollback protection into a claim
// villa cannot honour — silently, because nothing else on the host would notice.
package snapshotprune

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// snap builds a snapshot record.
func snap(vol, path string, bytes int64) pinstate.DataSnapshot {
	return pinstate.DataSnapshot{Volume: vol, Path: path, Bytes: bytes, TakenAt: "2026-08-26T12:00:00Z"}
}

// TestASupersededSnapshotIsRemovable is the base case: the committed update
// displaced it and nothing points at it, so the disk may come back.
func TestASupersededSnapshotIsRemovable(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Chat,
		State: pinstate.State{
			Previous: map[string]pinstate.Previous{
				"chat": {Data: snap("villa-openwebui", "/snap/chat-new.tar", 267_000_000)},
			},
		},
		Superseded: snap("villa-openwebui", "/snap/chat-old.tar", 260_000_000),
	})

	if got := plan.Removable(); len(got) != 1 || got[0] != "/snap/chat-old.tar" {
		t.Errorf("Removable = %v, want the displaced snapshot", got)
	}
	if plan.Blocked {
		t.Error("a readable store blocked the plan")
	}
	if plan.Decisions[0].Bytes == 0 {
		t.Error("the decision carries no size, so a caller cannot say what the removal reclaimed")
	}
}

// TestTheLiveRollbackTargetIsNeverRemoved is the safety property this package
// exists for.
//
// A snapshot the current retained tuple points at is the data villa would restore.
// Removing it leaves the record claiming a rollback target that is not there — the
// worst kind of failure, because it only shows up when it is needed.
func TestTheLiveRollbackTargetIsNeverRemoved(t *testing.T) {
	const live = "/snap/memory-live.tar"

	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Memory,
		State: pinstate.State{
			Previous: map[string]pinstate.Previous{
				"memory": {Data: snap("villa-qdrant", live, 2_800_000_000)},
			},
		},
		// The commit did not displace it after all.
		Superseded: snap("villa-qdrant", live, 2_800_000_000),
	})

	if got := plan.Removable(); len(got) != 0 {
		t.Fatalf("Removable = %v; the live rollback target was scheduled for deletion", got)
	}
	if len(plan.Decisions) == 0 || plan.Decisions[0].Action != Retain {
		t.Fatalf("decisions = %+v, want a retain", plan.Decisions)
	}
	if !strings.Contains(plan.Decisions[0].Reason, "rollback target") {
		t.Errorf("the retention gives no reason a user can act on: %q", plan.Decisions[0].Reason)
	}
}

// TestAnotherSubsystemsRollbackTargetIsNeverRemoved.
//
// A snapshot belongs to one subsystem today, so this cannot happen — but assuming
// it never will is the same assumption the image path had to unlearn when two
// components turned out to share a digest. The whole store is consulted, so the
// answer stays right if that changes.
func TestAnotherSubsystemsRollbackTargetIsNeverRemoved(t *testing.T) {
	const shared = "/snap/shared.tar"

	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Chat,
		State: pinstate.State{
			Previous: map[string]pinstate.Previous{
				"memory": {Data: snap("villa-qdrant", shared, 2_800_000_000)},
			},
		},
		Superseded: snap("villa-openwebui", shared, 2_800_000_000),
	})

	if got := plan.Removable(); len(got) != 0 {
		t.Errorf("Removable = %v; a snapshot another subsystem still points at was scheduled for deletion", got)
	}
}

// TestAnUnreadableStoreRemovesNothing is the unsafe direction, refused explicitly.
//
// An unreadable store yields an empty set of retained tuples, which reads as "no
// snapshot is a rollback target, delete freely" — and this is the only code in the
// project that would act on that reading.
func TestAnUnreadableStoreRemovesNothing(t *testing.T) {
	plan := Decide(Input{
		StateKnown: false,
		Subsystem:  subsystem.Chat,
		Superseded: snap("villa-openwebui", "/snap/chat-old.tar", 1),
	})

	if !plan.Blocked {
		t.Fatal("an unreadable store did not block the plan")
	}
	if got := plan.Removable(); len(got) != 0 {
		t.Errorf("Removable = %v; villa removed a snapshot it could not prove was unreferenced", got)
	}
	if !strings.Contains(plan.BlockedReason, "not the same as an empty one") {
		t.Errorf("the refusal does not distinguish unreadable from empty: %q", plan.BlockedReason)
	}
}

// TestNothingSupersededRemovesNothing: the first update of a stateful subsystem
// displaces no snapshot, because there was none.
func TestNothingSupersededRemovesNothing(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Chat,
		State: pinstate.State{
			Previous: map[string]pinstate.Previous{
				"chat": {Data: snap("villa-openwebui", "/snap/chat-new.tar", 1)},
			},
		},
	})

	if got := plan.Removable(); len(got) != 0 {
		t.Errorf("Removable = %v for an update that displaced nothing", got)
	}
}

// TestAStatelessSubsystemsUpdateRemovesNothing: inference has no snapshot to
// displace, so the plan is empty rather than pointing at something.
func TestAStatelessSubsystemsUpdateRemovesNothing(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Inference,
		State: pinstate.State{
			Previous: map[string]pinstate.Previous{
				"inference": {Refs: map[string]string{"backend-vulkan-radv": "old"}},
			},
		},
	})

	if len(plan.Decisions) != 0 {
		t.Errorf("decisions = %+v for a subsystem with no data", plan.Decisions)
	}
}

// TestAMissingSnapshotIsSurfaced.
//
// Someone clearing disk by hand has removed the rollback target. Villa proceeding
// as though it still had a safety net would be claiming a guarantee it cannot keep,
// so the absence is reported the same way a missing previous image is.
func TestAMissingSnapshotIsSurfaced(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Chat,
		State: pinstate.State{
			Previous: map[string]pinstate.Previous{
				"chat": {Data: snap("villa-openwebui", "/snap/chat-new.tar", 267_000_000)},
			},
		},
		Present: func(string) bool { return false },
	})

	var missing []Decision
	for _, d := range plan.Decisions {
		if d.Action == Missing {
			missing = append(missing, d)
		}
	}
	if len(missing) != 1 {
		t.Fatalf("decisions = %+v, want one missing-snapshot surface", plan.Decisions)
	}
	if !strings.Contains(missing[0].Reason, "no longer on disk") {
		t.Errorf("the surface does not say the snapshot is gone: %q", missing[0].Reason)
	}
	if missing[0].Subsystem != subsystem.Chat {
		t.Errorf("the surface names %v, want chat", missing[0].Subsystem)
	}
}

// TestEverySubsystemsMissingSnapshotIsSurfaced: a cleanup pass is the natural
// moment to notice that ANOTHER subsystem's rollback target went away, and the user
// is equally unable to see it either way.
func TestEverySubsystemsMissingSnapshotIsSurfaced(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Chat,
		State: pinstate.State{
			Previous: map[string]pinstate.Previous{
				"chat":   {Data: snap("villa-openwebui", "/snap/chat.tar", 1)},
				"memory": {Data: snap("villa-qdrant", "/snap/memory.tar", 1)},
			},
		},
		Present: func(p string) bool { return p == "/snap/chat.tar" },
	})

	var missing []Decision
	for _, d := range plan.Decisions {
		if d.Action == Missing {
			missing = append(missing, d)
		}
	}
	if len(missing) != 1 || missing[0].Subsystem != subsystem.Memory {
		t.Fatalf("decisions = %+v, want memory's missing snapshot surfaced", plan.Decisions)
	}
}

// TestAPresentSnapshotIsNotSurfacedAsMissing is the positive control: without it, a
// Present that always returned false would pass the test above.
func TestAPresentSnapshotIsNotSurfacedAsMissing(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Chat,
		State: pinstate.State{
			Previous: map[string]pinstate.Previous{
				"chat": {Data: snap("villa-openwebui", "/snap/chat.tar", 1)},
			},
		},
		Present: func(string) bool { return true },
	})

	for _, d := range plan.Decisions {
		if d.Action == Missing {
			t.Errorf("a present snapshot was surfaced as missing: %+v", d)
		}
	}
}

// TestNoPresentCheckSkipsRatherThanGuesses: a nil Present means villa cannot look,
// which is not the same as looking and finding nothing.
func TestNoPresentCheckSkipsRatherThanGuesses(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Chat,
		State: pinstate.State{
			Previous: map[string]pinstate.Previous{
				"chat": {Data: snap("villa-openwebui", "/snap/chat.tar", 1)},
			},
		},
	})

	for _, d := range plan.Decisions {
		if d.Action == Missing {
			t.Errorf("villa reported a snapshot missing without checking: %+v", d)
		}
	}
}

// TestTheDecisionIsDeterministic: a caller printing the plan twice prints the same
// thing, so the narration does not shuffle between runs.
func TestTheDecisionIsDeterministic(t *testing.T) {
	in := Input{
		StateKnown: true,
		Subsystem:  subsystem.Chat,
		State: pinstate.State{
			Previous: map[string]pinstate.Previous{
				"chat":   {Data: snap("villa-openwebui", "/snap/chat.tar", 1)},
				"memory": {Data: snap("villa-qdrant", "/snap/memory.tar", 1)},
			},
		},
		Present: func(string) bool { return false },
	}

	first := Decide(in)
	for i := 0; i < 20; i++ {
		next := Decide(in)
		if len(next.Decisions) != len(first.Decisions) {
			t.Fatalf("decision count varies between runs: %d then %d", len(first.Decisions), len(next.Decisions))
		}
		for j := range next.Decisions {
			if next.Decisions[j].Path != first.Decisions[j].Path {
				t.Fatalf("decision order varies between runs: %q then %q", first.Decisions[j].Path, next.Decisions[j].Path)
			}
		}
	}
}
