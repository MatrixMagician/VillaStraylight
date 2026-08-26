// Package pinresolve tests drive the three states a host can be in — fresh
// install, partial divergence, full divergence — plus the fault cases that must
// look like a fresh install rather than an error.
//
// The resolver is pure over two injected values, so there is no seam to fake and
// no host to stand in for: every case below is a literal table and a literal state.
package pinresolve

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// TestFreshInstallResolvesEverythingToVetted is the normal path, and the one that
// must not look like a fault. A machine that has never run `villa update` has no
// pin state at all, and that is not an error condition — it is the state every
// install starts in.
func TestFreshInstallResolvesEverythingToVetted(t *testing.T) {
	r := New(pinstate.State{})

	all := r.All()
	if len(all) != len(pins.Table()) {
		t.Fatalf("resolved %d components, want %d", len(all), len(pins.Table()))
	}
	for _, got := range all {
		if got.Current.Ref != got.Vetted.Ref {
			t.Errorf("%s: current %q != vetted %q on a fresh install", got.Component, got.Current.Ref, got.Vetted.Ref)
		}
		if got.FromStore {
			t.Errorf("%s: claims its pin came from the store, but the store is empty", got.Component)
		}
		if got.Diverged() {
			t.Errorf("%s: reads as diverged on a fresh install", got.Component)
		}
	}
	if d := r.Diverged(); len(d) != 0 {
		t.Errorf("a fresh install reports %d diverged components", len(d))
	}
}

// TestAnUnreadableStoreLooksLikeAFreshInstall: an absent or corrupt store loads as
// the zero State, and the resolver must turn that into vetted pins with no error
// surfaced. A user who deleted a state file, or never had one, has done nothing
// wrong and must not be shown a fault.
func TestAnUnreadableStoreLooksLikeAFreshInstall(t *testing.T) {
	// The zero State is exactly what pinstate.Load returns for absent, corrupt and
	// future-schema documents alike.
	r := New(pinstate.State{})
	got, ok := r.Resolve(pins.Qdrant)
	if !ok {
		t.Fatal("the table no longer names qdrant")
	}
	if got.Current.Ref != got.Vetted.Ref || got.FromStore {
		t.Errorf("an unreadable store did not resolve to the vetted pin: %+v", got)
	}
}

// TestPartialDivergenceIsRepresentable is the mixed state the vetted/effective
// split exists to make legible. One subsystem updated and the rest not is a
// combination nobody vetted as a whole, and the point is that villa can SAY so
// rather than have it lurk.
func TestPartialDivergenceIsRepresentable(t *testing.T) {
	updated := "example.invalid/qdrant@sha256:newer"
	r := New(pinstate.State{
		Pins: map[string]pinstate.Effective{
			string(pins.Qdrant): {Ref: updated},
		},
	})

	got, _ := r.Resolve(pins.Qdrant)
	if got.Current.Ref != updated {
		t.Errorf("current = %q, want the recorded effective pin %q", got.Current.Ref, updated)
	}
	if !got.FromStore {
		t.Error("the pin came from the store but FromStore is false")
	}
	if !got.Diverged() {
		t.Error("qdrant runs a pin villa did not vet, yet reads as not diverged")
	}
	if got.Vetted.Ref == updated {
		t.Error("the vetted pin was overwritten by the effective one; divergence would be unreportable")
	}

	// Its subsystem sibling was NOT updated, and must still read as vetted.
	embed, _ := r.Resolve(pins.Embedder)
	if embed.Diverged() || embed.FromStore {
		t.Errorf("the embedder diverged despite no record for it: %+v", embed)
	}

	if d := r.Diverged(); len(d) != 1 || d[0].Component != pins.Qdrant {
		t.Errorf("Diverged() = %v, want exactly qdrant", d)
	}
}

// TestFullDivergence: every component recorded as something else. This is the
// end state of a run that updated everything, and nothing may fall back.
func TestFullDivergence(t *testing.T) {
	state := pinstate.State{Pins: map[string]pinstate.Effective{}}
	for _, e := range pins.Table() {
		state.Pins[string(e.Component)] = pinstate.Effective{Ref: "example.invalid/" + string(e.Component) + "@sha256:new"}
	}
	r := New(state)

	for _, got := range r.All() {
		if !got.FromStore {
			t.Errorf("%s: fell back to the vetted pin despite a recorded effective one", got.Component)
		}
		if !got.Diverged() {
			t.Errorf("%s: reads as not diverged despite running a different pin", got.Component)
		}
	}
	if len(r.Diverged()) != len(pins.Table()) {
		t.Errorf("Diverged() returned %d of %d components", len(r.Diverged()), len(pins.Table()))
	}
}

// TestRecordedButEqualIsNotDivergence separates two facts that are easy to
// conflate. A host can record an effective pin EQUAL to the vetted one — a
// re-install, or an update that landed on the same digest — and "recorded" is not
// "different". Collapsing them would make doctor report a divergence that is not
// there.
func TestRecordedButEqualIsNotDivergence(t *testing.T) {
	entry, ok := pins.Lookup(pins.Qdrant)
	if !ok {
		t.Fatal("the table no longer names qdrant")
	}
	r := New(pinstate.State{
		Pins: map[string]pinstate.Effective{
			string(pins.Qdrant): {Ref: entry.Vetted().Ref},
		},
	})
	got, _ := r.Resolve(pins.Qdrant)
	if !got.FromStore {
		t.Error("FromStore is false despite a recorded pin")
	}
	if got.Diverged() {
		t.Error("a recorded pin equal to the vetted one reads as divergence")
	}
}

// TestABlankRecordedPinFallsBackRatherThanRunningNothing: a store carrying a
// component key with an empty ref is a half-written document, not a host declaring
// it runs nothing. Falling back is the only reading that leaves the stack runnable.
func TestABlankRecordedPinFallsBackRatherThanRunningNothing(t *testing.T) {
	r := New(pinstate.State{
		Pins: map[string]pinstate.Effective{string(pins.Qdrant): {Ref: ""}},
	})
	got, _ := r.Resolve(pins.Qdrant)
	if got.Current.Ref == "" {
		t.Error("resolved to an empty pin; the rendered unit would name no image")
	}
	if got.FromStore {
		t.Error("a blank record counted as a recorded pin")
	}
}

// TestResolveIsTheAllowlistForUnknownComponents: the bool is the same answer
// pins.Lookup gives, so a caller cannot resolve a component villa does not ship by
// going through the resolver instead of the table.
func TestResolveIsTheAllowlistForUnknownComponents(t *testing.T) {
	r := New(pinstate.State{
		Pins: map[string]pinstate.Effective{"a-component-villa-never-shipped": {Ref: "attacker.invalid/x"}},
	})
	if got, ok := r.Resolve("a-component-villa-never-shipped"); ok {
		t.Errorf("resolved a component the table does not name: %+v", got)
	}
	// And it must not appear in the walks either.
	for _, res := range r.All() {
		if res.Current.Ref == "attacker.invalid/x" {
			t.Error("a state entry for an unnamed component leaked into All()")
		}
	}
}

// TestForGroupsByProofUnit: the update flow walks subsystems because the proof unit
// is the verify verb's scope, so the resolver must group the same way the table
// does — both halves of the memory pairing, together.
func TestForGroupsByProofUnit(t *testing.T) {
	r := New(pinstate.State{})
	mem := r.For(subsystem.Memory)
	if len(mem) != 2 {
		t.Fatalf("memory resolved %d components, want Qdrant and the embedder", len(mem))
	}
	if mem[0].Component != pins.Qdrant || mem[1].Component != pins.Embedder {
		t.Errorf("memory resolved %v, want [qdrant embedder] in table order", []pins.ComponentID{mem[0].Component, mem[1].Component})
	}
	for _, res := range mem {
		if res.Subsystem != subsystem.Memory {
			t.Errorf("%s carries subsystem %v", res.Component, res.Subsystem)
		}
	}
}

// TestShapeIsCarriedThrough: shape is part of the --json contract, and a caller
// that had to re-open the table to find it would eventually forget to.
func TestShapeIsCarriedThrough(t *testing.T) {
	r := New(pinstate.State{})
	for _, res := range r.All() {
		entry, ok := pins.Lookup(res.Component)
		if !ok {
			t.Fatalf("%s is resolvable but not in the table", res.Component)
		}
		if res.Shape != entry.Shape {
			t.Errorf("%s: resolved shape %q, table shape %q", res.Component, res.Shape, entry.Shape)
		}
	}
}

// TestChecksumSurvivesForAChecksummedAsset: Crush is the one component whose pin is
// a version plus a checksum, and dropping the checksum on the way through would
// leave the install gate with nothing to verify against.
func TestChecksumSurvivesForAChecksummedAsset(t *testing.T) {
	r := New(pinstate.State{
		Pins: map[string]pinstate.Effective{
			string(pins.Crush): {Ref: "v0.99.0", Checksum: "deadbeef"},
		},
	})
	got, _ := r.Resolve(pins.Crush)
	if got.Current.Checksum != "deadbeef" {
		t.Errorf("checksum = %q, want the recorded one; the install gate would have nothing to verify", got.Current.Checksum)
	}
	if got.Vetted.Checksum == "" {
		t.Error("the vetted checksum was lost; a rollback could not verify what it restored")
	}
}

// TestNewWithTableIsPureOverItsInputs proves the resolver holds no hidden state: a
// caller-supplied table with a made-up component resolves exactly as given, with no
// reference to the compiled-in one.
func TestNewWithTableIsPureOverItsInputs(t *testing.T) {
	table := []pins.Entry{{
		Component: "test-component",
		Subsystem: subsystem.Chat,
		Shape:     pins.RollingDigest,
		Registry:  "example.invalid",
		Vetted:    func() pins.Pin { return pins.Pin{Ref: "example.invalid/x@sha256:vetted"} },
	}}
	r := NewWithTable(table, pinstate.State{
		Pins: map[string]pinstate.Effective{"test-component": {Ref: "example.invalid/x@sha256:effective"}},
	})

	all := r.All()
	if len(all) != 1 {
		t.Fatalf("resolved %d components from a one-entry table", len(all))
	}
	if all[0].Current.Ref != "example.invalid/x@sha256:effective" || all[0].Vetted.Ref != "example.invalid/x@sha256:vetted" {
		t.Errorf("resolved %+v, want the injected values", all[0])
	}
	// The real table's components must not leak in.
	if _, ok := r.Resolve(pins.Qdrant); ok {
		t.Error("a component from the compiled-in table resolved through an injected one")
	}
}
