// Package prune tests are about the one thing that can go badly wrong here: this
// is the only code in the project that deletes a container image, so every test
// below is ultimately asking "could this delete something a running stack needs?".
//
// The shared-digest case is the sharp one. The embedder and the vulkan backend are
// byte-identical today, so a per-component prune would delete an image a RUNNING
// backend depends on — not a lost rollback, a broken stack.
package prune

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// TestAnUnreferencedImageIsRemovable is the base case: nothing points at it, so it
// may go.
func TestAnUnreferencedImageIsRemovable(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Memory,
		State: pinstate.State{
			Pins: map[string]pinstate.Effective{"qdrant": {Ref: "new-qdrant"}},
		},
		Superseded: map[string]string{"qdrant": "old-qdrant"},
	})

	if got := plan.Removable(); len(got) != 1 || got[0] != "old-qdrant" {
		t.Errorf("Removable = %v, want the superseded and unreferenced image", got)
	}
	if plan.Blocked {
		t.Error("a readable store blocked the plan")
	}
}

// TestASharedDigestIsNeverRemoved is the safety property this package exists for.
//
// Updating the embedder must not remove an image the inference backend still runs.
// The two are the same digest today, so a prune keyed by component would delete it
// and break a running stack.
func TestASharedDigestIsNeverRemoved(t *testing.T) {
	const shared = "docker.io/example/toolboxes@sha256:shared"

	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Memory,
		State: pinstate.State{
			Pins: map[string]pinstate.Effective{
				// Memory moved on...
				"embedder": {Ref: "docker.io/example/toolboxes@sha256:newembed"},
				// ...but the inference backend is STILL RUNNING the shared image.
				"backend-vulkan-radv": {Ref: shared},
			},
		},
		Superseded: map[string]string{"embedder": shared},
	})

	if got := plan.Removable(); len(got) != 0 {
		t.Fatalf("prune would remove %v, which the inference backend is still running — "+
			"that is not a lost rollback, it is a broken stack", got)
	}

	// And the retention is REPORTED, with the reason. A silent no-op leaves a user
	// asking why the old image is still on disk.
	if len(plan.Decisions) != 1 || plan.Decisions[0].Action != Retain {
		t.Fatalf("decisions = %+v, want a single reported retention", plan.Decisions)
	}
	if !strings.Contains(plan.Decisions[0].Reason, "still referenced by") {
		t.Errorf("the retention does not say what still references it: %q", plan.Decisions[0].Reason)
	}
	if !strings.Contains(plan.Decisions[0].Reason, "backend-vulkan-radv") {
		t.Errorf("the retention does not name the holder: %q", plan.Decisions[0].Reason)
	}
}

// TestARetainedPreviousIsAReference: a rollback target is referenced in exactly the
// sense that matters. Removing it would remove the safety net while leaving the
// record that claims one exists.
func TestARetainedPreviousIsAReference(t *testing.T) {
	const old = "old-searxng"

	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Memory,
		State: pinstate.State{
			Pins: map[string]pinstate.Effective{"qdrant": {Ref: "new-qdrant"}},
			Previous: map[string]pinstate.Previous{
				// Another subsystem's rollback target happens to be this image.
				"web search": {Refs: map[string]string{"searxng": old}},
			},
		},
		Superseded: map[string]string{"qdrant": old},
	})

	if got := plan.Removable(); len(got) != 0 {
		t.Errorf("prune would remove %v, which is another subsystem's rollback target", got)
	}
	if !strings.Contains(plan.Decisions[0].Reason, "retained previous") {
		t.Errorf("the retention does not name the rollback target: %q", plan.Decisions[0].Reason)
	}
}

// TestReferencesAreCountedAcrossTheWholeStore: a per-subsystem view could not see
// that another subsystem still holds the image, which is precisely the hazard
// reference counting closes.
func TestReferencesAreCountedAcrossTheWholeStore(t *testing.T) {
	const shared = "shared-image"

	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Memory, // memory's update...
		State: pinstate.State{
			// ...but the reference lives under a completely different subsystem.
			Pins: map[string]pinstate.Effective{"searxng": {Ref: shared}},
		},
		Superseded: map[string]string{"embedder": shared},
	})

	if len(plan.Removable()) != 0 {
		t.Error("a reference held by another subsystem did not prevent removal")
	}
}

// TestAnUnreadableStoreBlocksEveryRemoval is the second unsafe-zero fallback at the
// point where it would do damage.
//
// An empty reference set reads as "nothing is referenced, delete freely", and this
// is the only code in the project that would act on it. Villa cannot tell what is
// referenced, so it removes nothing (ADR-0004).
func TestAnUnreadableStoreBlocksEveryRemoval(t *testing.T) {
	plan := Decide(Input{
		StateKnown: false,
		Subsystem:  subsystem.Memory,
		Superseded: map[string]string{"qdrant": "old-qdrant"},
	})

	if !plan.Blocked {
		t.Fatal("an unreadable store did not block the plan")
	}
	if got := plan.Removable(); len(got) != 0 {
		t.Errorf("an unreadable store licensed removing %v", got)
	}
	if !strings.Contains(plan.BlockedReason, "not the same as an empty one") {
		t.Errorf("the refusal does not distinguish unreadable from empty: %q", plan.BlockedReason)
	}
}

// TestAnEmptyStoreDoesNotLicenseRemovalEither: reached through StateKnown=false in
// practice, since pinstate.ReferencedRefs reports an empty document as unknown.
// Asserted here so the two packages' readings cannot drift apart.
func TestAnEmptyStoreDoesNotLicenseRemovalEither(t *testing.T) {
	// pinstate.ReferencedRefs returns known=false for an empty document precisely
	// so this never reaches Decide with StateKnown=true. This asserts the contract
	// the two packages share.
	_, known, err := pinstate.ReferencedRefs(pinstate.Deps{
		ReadAll: func() ([]byte, error) { return []byte(`{"schema_version":1}`), nil },
	})
	if err != nil {
		t.Fatalf("ReferencedRefs: %v", err)
	}
	if known {
		t.Error("pinstate reports an empty store as a KNOWN reference set; prune would then treat it as permission to delete")
	}
}

// TestAMissingRecordedPreviousIsSurfaced.
//
// Someone running `podman image prune` by hand loses rollback protection. Villa
// proceeding as though it still had a safety net would be claiming a guarantee it
// cannot honour, so it says so instead — the image store is the operator's, and
// villa's claim is limited to what it can see.
func TestAMissingRecordedPreviousIsSurfaced(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Memory,
		State: pinstate.State{
			Pins: map[string]pinstate.Effective{"qdrant": {Ref: "new-qdrant"}},
			Previous: map[string]pinstate.Previous{
				"memory": {Refs: map[string]string{"qdrant": "vanished-qdrant"}},
			},
		},
		Superseded: map[string]string{},
		Present:    func(ref string) bool { return ref != "vanished-qdrant" },
	})

	var found *Decision
	for i := range plan.Decisions {
		if plan.Decisions[i].Action == MissingPrevious {
			found = &plan.Decisions[i]
		}
	}
	if found == nil {
		t.Fatalf("a missing recorded previous was not surfaced: %+v", plan.Decisions)
	}
	if !strings.Contains(found.Reason, "outside villa") {
		t.Errorf("the report does not say something removed it outside villa: %q", found.Reason)
	}
	if !strings.Contains(found.Reason, "rollback protection") {
		t.Errorf("the report does not say what was lost: %q", found.Reason)
	}
}

// TestAPresentPreviousIsNotSurfaced: the missing-previous report must not fire on
// the normal case, or it becomes noise a user learns to ignore.
func TestAPresentPreviousIsNotSurfaced(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Memory,
		State: pinstate.State{
			Previous: map[string]pinstate.Previous{
				"memory": {Refs: map[string]string{"qdrant": "old-qdrant"}},
			},
		},
		Present: func(string) bool { return true },
	})
	for _, d := range plan.Decisions {
		if d.Action == MissingPrevious {
			t.Errorf("a present previous was reported as missing: %+v", d)
		}
	}
}

// TestNoPresenceSeamSkipsTheCheckRatherThanGuessing: villa cannot see the image
// store, so it says nothing rather than reporting a loss it has not observed.
func TestNoPresenceSeamSkipsTheCheckRatherThanGuessing(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Memory,
		State: pinstate.State{
			Previous: map[string]pinstate.Previous{
				"memory": {Refs: map[string]string{"qdrant": "old-qdrant"}},
			},
		},
		Present: nil,
	})
	for _, d := range plan.Decisions {
		if d.Action == MissingPrevious {
			t.Error("villa reported a missing previous without being able to check")
		}
	}
}

// TestThePlanIsDeterministic: a caller printing it twice must print the same thing,
// and a map-ordered plan would randomise the output between runs.
func TestThePlanIsDeterministic(t *testing.T) {
	in := Input{
		StateKnown: true,
		Subsystem:  subsystem.Memory,
		State:      pinstate.State{Pins: map[string]pinstate.Effective{"qdrant": {Ref: "new"}}},
		Superseded: map[string]string{"a": "img-c", "b": "img-a", "c": "img-b"},
	}
	first := Decide(in)
	for i := 0; i < 20; i++ {
		got := Decide(in)
		if len(got.Decisions) != len(first.Decisions) {
			t.Fatalf("decision count varies between runs")
		}
		for j := range got.Decisions {
			if got.Decisions[j].Ref != first.Decisions[j].Ref {
				t.Fatalf("decision order varies between runs: %q then %q", first.Decisions[j].Ref, got.Decisions[j].Ref)
			}
		}
	}
}

// TestDuplicateSupersededRefsAreConsideredOnce: two components moving off the same
// image must not produce two removal decisions for one reference, which would make
// the second removal fail confusingly.
func TestDuplicateSupersededRefsAreConsideredOnce(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Memory,
		State:      pinstate.State{Pins: map[string]pinstate.Effective{"qdrant": {Ref: "new"}}},
		Superseded: map[string]string{"qdrant": "old-shared", "embedder": "old-shared"},
	})
	if len(plan.Removable()) != 1 {
		t.Errorf("Removable = %v, want one decision per distinct reference", plan.Removable())
	}
}

// TestEveryDecisionCarriesAReason: the no-op will look like a bug, so no decision
// may reach the user unexplained.
func TestEveryDecisionCarriesAReason(t *testing.T) {
	plan := Decide(Input{
		StateKnown: true,
		Subsystem:  subsystem.Memory,
		State: pinstate.State{
			Pins:     map[string]pinstate.Effective{"backend-vulkan-radv": {Ref: "shared"}},
			Previous: map[string]pinstate.Previous{"memory": {Refs: map[string]string{"qdrant": "gone"}}},
		},
		Superseded: map[string]string{"embedder": "shared", "qdrant": "unreferenced"},
		Present:    func(ref string) bool { return ref != "gone" },
	})

	if len(plan.Decisions) == 0 {
		t.Fatal("no decisions")
	}
	for _, d := range plan.Decisions {
		if strings.TrimSpace(d.Reason) == "" {
			t.Errorf("decision %+v carries no reason; a silent retention reads as a bug", d)
		}
	}
}
