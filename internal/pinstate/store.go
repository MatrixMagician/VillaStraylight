// Package pinstate is the host-side record of what THIS machine is actually
// running, as opposed to what villa shipped.
//
// Before it, a pin was a compile-time constant, so "what is installed" and "what
// villa vetted" could not differ and could not be distinguished. That is why
// `doctor` could never say "you are running a digest villa never vetted", why
// rollback had no target, and why an update had nowhere to record its result.
//
// # Vetted and effective
//
// A vetted pin is villa's maintainer's claim: "I proved this on gfx1151 hardware."
// It lives in the compiled-in pins.Table and cannot be absent — a malformed one is
// a build-time programming error. An effective pin is this host's fact: "I am
// running this." It lives here, is mutable, and is written ONLY after a proven
// update. They are equal on a fresh install and diverge only when an update
// commits.
//
// The two live in different packages precisely because they FAIL differently.
// Merging them would blur a thing that cannot fail with a thing that routinely
// does.
//
// # Two places where absent-means-empty is the UNSAFE direction
//
// jsonstore's fail-closed reading — absent, corrupt or future-schema yields the zero
// value — is right for effective pins: no record means "fall back to the vetted
// pin", which is the fresh-install path and not a fault. It is WRONG, dangerously,
// for two other things this document carries, because for both of them the zero
// value points the wrong way:
//
//   - The SERIAL FLOOR. Zero means "no floor", so an absent store would reset the
//     anti-downgrade protection and re-open the replay attack the serial exists to
//     close. Deleting a state file must not be an attack primitive. SerialFloor
//     therefore falls back to the COMPILED-IN vetted serial, never to zero.
//
//   - The REFERENCE SET for pruning. An empty set reads as "nothing is referenced,
//     delete freely" — and this project's only image-deleting code would act on it.
//     ReferencedRefs therefore reports whether the state was READABLE, so a caller
//     with an absent store retains everything rather than pruning everything
//     (ADR-0004).
//
// Both are handled at the accessor rather than left to each caller to remember,
// because "remember to invert the default here" is not a property a codebase keeps.
//
// PURE: no I/O of its own — every byte moves through the injected jsonstore seam.
package pinstate

import (
	"github.com/MatrixMagician/VillaStraylight/internal/jsonstore"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// pinStateSchemaVersion is this store's OWN self-version, independent of every
// other store's. Bump only on an incompatible pin-state.json change.
const pinStateSchemaVersion = 1

// DataSnapshot is the exported copy of a stateful subsystem's data volume, taken
// while its services were stopped.
//
// It is part of the rollback tuple for exactly the subsystems whose IMAGE is not
// the state being changed. Chat and memory own a mutable volume, and a forward data
// migration makes the retained image an unusable rollback target on its own: a real
// `villa update chat` migrated Open WebUI's SQLite schema forward, and restoring the
// old digest onto the migrated database crash-looped it.
//
// Path, not bytes. The tar is gigabytes (memory's measured 2.8 GB), and pin-state
// is a small JSON document read on nearly every command — inlining the data would
// make every status read pay for it.
type DataSnapshot struct {
	// Volume is the podman named volume the snapshot was exported from, recorded so
	// a restore imports into the volume it came from rather than one derived
	// afresh from a declaration that may have moved since.
	Volume string `json:"volume"`
	// Path is where the exported tar lives.
	Path string `json:"path"`
	// Bytes is the snapshot's size on disk. Recorded because it is a real disk cost
	// a user is entitled to see, and because a zero-byte snapshot is a broken
	// rollback target that should be visible rather than inferred.
	Bytes int64 `json:"bytes,omitempty"`
	// TakenAt is when the export completed, RFC3339 UTC.
	TakenAt string `json:"taken_at,omitempty"`
}

// Taken reports whether a snapshot was actually recorded. A stateless subsystem's
// tuple carries the zero value, which is the honest "there was no data to take".
func (d DataSnapshot) Taken() bool { return d.Path != "" }

// Previous is the known-good state a SUBSYSTEM can be rolled back to.
//
// Per subsystem, not per component, because the subsystem is the proof unit: one
// `verify memory` proves Qdrant and the embedder together, so "the state that was
// last proven" is a fact about the pair. A per-component previous would offer a
// rollback target for half a pairing, restoring a combination nothing ever proved.
//
// It is a TUPLE, not a bare digest, and that is load-bearing. A digest alone is not
// restorable weeks later: the unit that digest ran under renders from a config that
// may since have changed, so restoring the image without the unit and the config it
// was proven under restores something nobody proved. This is the same capture
// internal/backendswap already performs before a swap — "the verbatim prior
// villa-llama.container bytes and the prior VillaConfig".
type Previous struct {
	// Refs are the pins each of the subsystem's components was running, keyed by
	// component id. Several, because a subsystem can hold several pinned
	// components, and they move and roll back together.
	Refs map[string]string `json:"refs"`
	// Units are the verbatim prior unit bytes, keyed by unit filename. Verbatim
	// because a re-render is not a restore: it would reproduce today's template
	// against today's config, which is not what was proven.
	Units map[string][]byte `json:"units,omitempty"`
	// Config is the serialised config the pins were proven under.
	Config string `json:"config,omitempty"`
	// Data is the exported data volume for a subsystem that owns persistent state.
	// It is the ZERO VALUE for a stateless subsystem, which is correct rather than
	// missing: the backends, SearXNG and the websafe base have no data of their own,
	// so their image genuinely IS the state a rollback restores.
	Data DataSnapshot `json:"data,omitempty"`
	// CapturedAt is when the tuple was taken, RFC3339 UTC.
	CapturedAt string `json:"captured_at,omitempty"`
}

// Effective is one component's recorded state on this host.
type Effective struct {
	// Ref is the pin this host is running for the component.
	Ref string `json:"ref"`
	// Checksum is populated only for a checksummed asset, mirroring pins.Pin.
	Checksum string `json:"checksum,omitempty"`
	// UpdatedAt is when the pin was committed, RFC3339 UTC.
	UpdatedAt string `json:"updated_at,omitempty"`
}

// State is the whole pin-state.json document.
//
// The two maps are keyed differently ON PURPOSE. Pins is keyed by component,
// because a pin is a component's value and memory holds two of them. Previous is
// keyed by subsystem, because the proof unit is the subsystem and a rollback target
// is only meaningful for a whole proven pairing.
type State struct {
	SchemaVersion int `json:"schema_version"`
	// Pins is the effective pin per component. A component absent from this map has
	// no effective pin, which correctly resolves to its vetted one.
	Pins map[string]Effective `json:"pins,omitempty"`
	// Previous is the ONE retained known-good tuple per subsystem. One, not a
	// history: the retention rule is a single previous, and the one before it
	// becomes a prune candidate.
	Previous map[string]Previous `json:"previous,omitempty"`
	// Serial is the highest manifest serial this host has accepted. It is the
	// anti-downgrade floor — but read it through SerialFloor, never directly, or an
	// absent store reads as zero and the floor vanishes.
	Serial uint64 `json:"serial,omitempty"`
	// CheckedAt is when villa last completed an update check, RFC3339 UTC. It is
	// recorded so status and doctor can say "last checked 200 days ago" rather than
	// implying currency by saying nothing.
	CheckedAt string `json:"checked_at,omitempty"`
}

// GetSchemaVersion and SetSchemaVersion let the shared store stamp and check this
// document's version without knowing anything else about it.
func (s *State) GetSchemaVersion() int  { return s.SchemaVersion }
func (s *State) SetSchemaVersion(v int) { s.SchemaVersion = v }

// store binds the shared persistence layer to this document, filename and version.
var store = jsonstore.New[State, *State]("pinstate", "pin-state.json", pinStateSchemaVersion)

// Deps is the injectable byte-I/O seam, so the package stays testable off-hardware
// with a buffer-backed Deps.
type Deps = jsonstore.Deps

// SchemaVersion exposes this store's OWN schema version to downstream readers.
func SchemaVersion() int { return pinStateSchemaVersion }

// Save stamps the schema version and writes the whole document via the seam.
func Save(d Deps, s State) error { return store.Save(d, s) }

// Load reads the document via the seam, failing CLOSED to an empty State on an
// absent, corrupt or version-mismatched store.
//
// Empty is the correct and expected reading for the effective pins: it means "no
// effective pin recorded", which resolves to the vetted pin — the fresh-install
// path, not a fault. It is NOT the correct reading for the serial floor or the
// reference set; use SerialFloor and ReferencedRefs, which invert the default.
func Load(d Deps) (State, error) { return store.Load(d) }

// Path resolves the single mutable pin-state document under the villa data root.
func Path() string { return store.Path() }

// WriteFileAtomic is the live WriteAll seam the cmd tier wires: a traversal-guarded
// temp+rename write at 0600 under a 0700 directory.
func WriteFileAtomic(path string, data []byte) error { return store.WriteFileAtomic(path, data) }

// EffectiveFor returns the recorded pin for a component, and whether one exists.
//
// The bool is the whole answer a resolver needs: false means "this host has no
// opinion", which is what makes the vetted pin the fallback rather than an error.
func (s State) EffectiveFor(id pins.ComponentID) (Effective, bool) {
	e, ok := s.Pins[string(id)]
	return e, ok
}

// PreviousFor returns the retained known-good tuple for a subsystem.
func (s State) PreviousFor(k subsystem.Kind) (Previous, bool) {
	p, ok := s.Previous[k.String()]
	return p, ok
}

// SerialFloor is the anti-downgrade floor: the HIGHER of the recorded serial and
// the compiled-in vetted serial.
//
// The max, not the recorded value, and this is the first of the two non-naive
// fallbacks. An absent or corrupt store yields State{} with Serial == 0, and zero
// means "accept anything" — so reading Serial directly would make `rm pin-state.json`
// a downgrade primitive. Taking the max also handles the subtler case a plain
// absent-check misses: a store that EXISTS but predates the current binary carries a
// serial below the one this binary shipped with, and villa must not accept a
// manifest older than its own compiled-in table either.
func (s State) SerialFloor() uint64 {
	if s.Serial > pins.Serial() {
		return s.Serial
	}
	return pins.Serial()
}

// ReferencedRefs is the set of pin references that must NOT be removed: every
// current effective pin and every retained previous, across the whole store.
//
// The bool is the second non-naive fallback, and it is a separate return rather
// than an empty set for a reason. This project's only image-deleting code consumes
// this, and an empty set reads to it as "nothing is referenced, delete freely" — so
// an unreadable store would license deleting an image a RUNNING backend depends on.
// The bool forces the caller to distinguish "nothing is referenced" from "villa
// cannot tell what is referenced", and only the first permits a removal (ADR-0004).
//
// References are counted over resolved REF VALUES, not component ids, so two
// components that happen to share one image (the embedder and the vulkan backend do
// today) count as two references to it without either needing to know about the
// other.
func ReferencedRefs(d Deps) (map[string]bool, bool, error) {
	s, err := Load(d)
	if err != nil {
		// A real I/O error, not an absent store. Villa cannot tell what is
		// referenced, so nothing may be removed.
		return nil, false, err
	}
	refs := map[string]bool{}
	for _, e := range s.Pins {
		if e.Ref != "" {
			refs[e.Ref] = true
		}
	}
	for _, p := range s.Previous {
		for _, ref := range p.Refs {
			if ref != "" {
				refs[ref] = true
			}
		}
	}
	// An empty document is indistinguishable from a corrupt or future-schema one
	// here, because Load folds all three into State{} — deliberately, since for
	// every OTHER field that folding is correct. For pruning it is not: villa
	// cannot demonstrate that nothing is referenced, only that it read nothing.
	// Retain everything.
	if len(refs) == 0 {
		return refs, false, nil
	}
	return refs, true, nil
}
