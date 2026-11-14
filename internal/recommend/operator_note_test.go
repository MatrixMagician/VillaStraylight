package recommend

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// TestOverrideOfAnOperatorEntryDoesNotClaimAKnownDefect (ADR-0041): an entry added with
// `villa catalog add` is written with unified_memory_safe false because nothing
// qualified it, not because anything is known to be wrong with it. The override note
// must say what is true: it is the operator's entry, it is not auto-selected, and
// setting the flag is how it becomes selectable.
func TestOverrideOfAnOperatorEntryDoesNotClaimAKnownDefect(t *testing.T) {
	seed, err := catalog.Seed()
	if err != nil {
		t.Fatal(err)
	}
	mine := seed.Models[2]
	mine.ID, mine.UnifiedMemorySafe = "my-model", false
	c := catalog.Catalog{SchemaVersion: catalog.SupportedSchema, Models: []catalog.Model{mine}}

	rec := Pick(profileWithEnvelope(120<<30), c, Overrides{Model: "my-model"}, nil)
	if rec.Model != "my-model" {
		t.Fatalf("the override was not honored: %q", rec.Model)
	}
	var note string
	for _, n := range rec.Notes {
		if strings.Contains(n, "unified_memory_safe:false") {
			note = n
		}
	}
	if note == "" {
		t.Fatalf("expected a note about the flag, got %v", rec.Notes)
	}
	for _, want := range []string{"operator-supplied", "never auto-selected", "set unified_memory_safe true"} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q: %s", want, note)
		}
	}
	if strings.Contains(note, "misbehave") || strings.Contains(note, "WARNING") {
		t.Errorf("an unqualified entry is not a known defect: %s", note)
	}
}

// TestOperatorEntryIsNotAutoSelectedUntilTheFlagIsSet: the flag is the whole mechanism,
// so the same entry with it true is picked and with it false is not.
func TestOperatorEntryIsNotAutoSelectedUntilTheFlagIsSet(t *testing.T) {
	seed, err := catalog.Seed()
	if err != nil {
		t.Fatal(err)
	}
	heavy := seed.Models[2]
	heavy.ID, heavy.WeightBytes, heavy.MinEnvelopeBytes = "my-model", 30<<30, 0

	for safe, want := range map[bool]bool{false: false, true: true} {
		heavy.UnifiedMemorySafe = safe
		c := catalog.Catalog{SchemaVersion: catalog.SupportedSchema, Models: []catalog.Model{seed.Models[0], heavy}}
		rec := Pick(profileWithEnvelope(120<<30), c, Overrides{}, nil)
		if got := rec.Model == "my-model"; got != want {
			t.Errorf("unified_memory_safe=%v: auto-selected=%v, want %v (picked %q)", safe, got, want, rec.Model)
		}
	}
}
