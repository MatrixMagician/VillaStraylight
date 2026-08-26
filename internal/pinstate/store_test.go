// Package pinstate tests guard the fail-closed reading AND the two places where
// that reading is the unsafe direction.
//
// The ordinary fail-closed cases are cloned from verifystate's discipline: absent,
// corrupt and future-schema all read as "no effective pin", which resolves to the
// vetted pin. The two inversions get their own tests, and they are the reason this
// store exists as a package rather than as a struct inside the resolver: each is
// easy to get wrong independently, and each is silent when it is wrong.
package pinstate

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// bufferDeps is a byte-backed seam: what the store writes is what the next read
// sees, mirroring internal/jsonstore's compat tests.
func bufferDeps(initial []byte) (*[]byte, Deps) {
	buf := initial
	d := Deps{
		WriteAll: func(data []byte) error { buf = append([]byte(nil), data...); return nil },
		ReadAll:  func() ([]byte, error) { return buf, nil },
	}
	return &buf, d
}

// TestRoundTrip proves the document carries everything the update flow records:
// effective pins, retained tuples, the serial, and the check timestamp.
func TestRoundTrip(t *testing.T) {
	buf, deps := bufferDeps(nil)

	want := State{
		Pins: map[string]Effective{
			string(pins.Qdrant): {Ref: "example.invalid/qdrant@sha256:aa", UpdatedAt: "2026-08-26T10:00:00Z"},
			string(pins.Crush):  {Ref: "v0.77.0", Checksum: "beef", UpdatedAt: "2026-08-26T10:05:00Z"},
		},
		Previous: map[string]Previous{
			subsystem.Memory.String(): {
				Refs: map[string]string{
					string(pins.Qdrant):   "example.invalid/qdrant@sha256:bb",
					string(pins.Embedder): "example.invalid/embed@sha256:cc",
				},
				Units:      map[string][]byte{"villa-qdrant.container": []byte("[Container]\nImage=old\n")},
				Config:     "model = \"x\"\n",
				CapturedAt: "2026-08-26T09:59:00Z",
			},
		},
		Serial:    7,
		CheckedAt: "2026-08-26T10:00:00Z",
	}

	if err := Save(deps, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(deps)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Serial != 7 || got.CheckedAt != want.CheckedAt {
		t.Errorf("serial/checked_at lost in the round trip: %+v", got)
	}
	if e, ok := got.EffectiveFor(pins.Qdrant); !ok || e.Ref != want.Pins[string(pins.Qdrant)].Ref {
		t.Errorf("qdrant effective pin lost: %+v (ok=%v)", e, ok)
	}
	if e, ok := got.EffectiveFor(pins.Crush); !ok || e.Checksum != "beef" {
		t.Errorf("a checksummed asset lost its checksum: %+v (ok=%v)", e, ok)
	}

	// The tuple, not a bare digest: verbatim unit bytes and the config must both
	// survive, or a rollback weeks later restores something nobody proved.
	prev, ok := got.PreviousFor(subsystem.Memory)
	if !ok {
		t.Fatal("the retained previous was lost")
	}
	// The proof unit is the subsystem, so BOTH halves of the memory pairing must be
	// in one tuple. Half a pairing is a rollback target nothing ever proved.
	if len(prev.Refs) != 2 {
		t.Errorf("the retained tuple holds %d refs, want both halves of the memory pairing", len(prev.Refs))
	}
	if string(prev.Units["villa-qdrant.container"]) != "[Container]\nImage=old\n" {
		t.Errorf("the verbatim prior unit bytes did not survive: %q", prev.Units["villa-qdrant.container"])
	}
	if prev.Config != "model = \"x\"\n" {
		t.Errorf("the prior config did not survive: %q", prev.Config)
	}

	// The schema version is stamped by the store, never carried from the caller.
	var raw map[string]any
	if err := json.Unmarshal(*buf, &raw); err != nil {
		t.Fatalf("unmarshal written bytes: %v", err)
	}
	if raw["schema_version"] != float64(SchemaVersion()) {
		t.Errorf("schema_version = %v, want %d", raw["schema_version"], SchemaVersion())
	}
}

// TestLoadFailsClosedToNoEffectivePin covers the reading that IS naive-correct.
// Absent, corrupt and future-schema must each yield no effective pin, which the
// resolver reads as "fall back to the vetted pin" — the fresh-install path, not a
// fault, and never an error surfaced to a user who has done nothing wrong.
func TestLoadFailsClosedToNoEffectivePin(t *testing.T) {
	cases := map[string]string{
		"absent":        "",
		"corrupt":       `{not json at all`,
		"future schema": `{"schema_version":99,"pins":{"qdrant":{"ref":"attacker/image"}}}`,
		"past schema":   `{"schema_version":0,"pins":{"qdrant":{"ref":"attacker/image"}}}`,
	}
	for name, onDisk := range cases {
		t.Run(name, func(t *testing.T) {
			_, deps := bufferDeps([]byte(onDisk))
			got, err := Load(deps)
			if err != nil {
				t.Fatalf("Load returned an error rather than failing closed: %v", err)
			}
			if e, ok := got.EffectiveFor(pins.Qdrant); ok {
				t.Errorf("a %s store yielded effective pin %+v; a document villa cannot read must never name what the host runs", name, e)
			}
		})
	}
}

// TestSchemaVersionIsStamped: a caller-supplied version is never trusted, so a
// document claiming another version is rewritten to this store's own on save.
func TestSchemaVersionIsStamped(t *testing.T) {
	buf, deps := bufferDeps(nil)
	if err := Save(deps, State{SchemaVersion: 99, Serial: 3}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var got State
	if err := json.Unmarshal(*buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SchemaVersion != SchemaVersion() {
		t.Errorf("schema_version = %d, want %d — a caller-supplied version must not survive", got.SchemaVersion, SchemaVersion())
	}
}

// TestNilSeamAndReadErrorAreRealErrors: the fail-closed reading applies to what the
// store CONTAINS, never to whether the seam works. A nil seam is a programming
// error and a read error is a real fault; silently reporting either as "no
// effective pin" would hide a broken host behind a normal-looking fresh install.
func TestNilSeamAndReadErrorAreRealErrors(t *testing.T) {
	if _, err := Load(Deps{}); err == nil {
		t.Error("Load with a nil ReadAll seam returned no error")
	}
	ioErr := Deps{ReadAll: func() ([]byte, error) { return nil, os.ErrPermission }}
	if _, err := Load(ioErr); err == nil {
		t.Error("Load with a failing ReadAll returned no error")
	}
	if err := Save(Deps{}, State{}); err == nil {
		t.Error("Save with a nil WriteAll seam returned no error")
	}
}

// ---------------------------------------------------------------------------
// The two unsafe-zero fallbacks. Each is tested explicitly because each is silent
// when it is wrong.
// ---------------------------------------------------------------------------

// TestAbsentStoreYieldsTheCompiledInSerialNeverZero is the first inversion.
//
// Zero means "no floor". If an absent store read as zero, `rm pin-state.json` would
// reset the anti-downgrade protection and re-open the replay attack the serial
// exists to close — a deleted file must not be an attack primitive.
func TestAbsentStoreYieldsTheCompiledInSerialNeverZero(t *testing.T) {
	for name, onDisk := range map[string]string{
		"absent":  "",
		"corrupt": `{not json`,
		"future":  `{"schema_version":99,"serial":9999}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, deps := bufferDeps([]byte(onDisk))
			s, err := Load(deps)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if s.Serial != 0 {
				t.Fatalf("precondition: a %s store should carry serial 0, got %d", name, s.Serial)
			}
			if got := s.SerialFloor(); got != pins.Serial() {
				t.Errorf("SerialFloor() = %d, want the compiled-in %d — a zero floor accepts any manifest", got, pins.Serial())
			}
			if s.SerialFloor() == 0 {
				t.Error("the floor is zero, which means 'accept anything'")
			}
		})
	}
}

// TestRecordedSerialRaisesTheFloorButNeverLowersIt: a host that has accepted a
// newer manifest must not be talked back down to the compiled-in serial, and a
// store predating the running binary must not lower the floor below what that
// binary shipped with.
func TestRecordedSerialRaisesTheFloorButNeverLowersIt(t *testing.T) {
	higher := State{Serial: pins.Serial() + 5}
	if got := higher.SerialFloor(); got != pins.Serial()+5 {
		t.Errorf("SerialFloor() = %d, want the recorded %d — an accepted manifest raises the floor", got, pins.Serial()+5)
	}

	// A store written by an older binary carries a serial below this binary's.
	stale := State{Serial: 0}
	if got := stale.SerialFloor(); got != pins.Serial() {
		t.Errorf("SerialFloor() = %d for a stale store, want the compiled-in %d — villa must not accept a manifest older than its own table", got, pins.Serial())
	}
}

// TestAbsentStoreMeansRetainEverythingNotDeleteFreely is the second inversion, and
// the one with the worst failure mode.
//
// Prune is the only code in this project that deletes a container image. An empty
// reference set reads to it as "nothing is referenced", so an absent or unreadable
// store would license removing an image a RUNNING backend depends on — not a lost
// rollback, a broken stack. The bool is what forces "nothing is referenced" to be
// distinguishable from "villa cannot tell" (ADR-0004).
func TestAbsentStoreMeansRetainEverythingNotDeleteFreely(t *testing.T) {
	for name, onDisk := range map[string]string{
		"absent":  "",
		"corrupt": `{not json`,
		"future":  `{"schema_version":99,"pins":{"qdrant":{"ref":"x"}}}`,
		"empty":   `{"schema_version":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, deps := bufferDeps([]byte(onDisk))
			refs, known, err := ReferencedRefs(deps)
			if err != nil {
				t.Fatalf("ReferencedRefs: %v", err)
			}
			if known {
				t.Errorf("a %s store reported a KNOWN reference set of %d entries; prune would treat that as permission to delete", name, len(refs))
			}
		})
	}

	// A real read error must also refuse to license a removal.
	ioErr := Deps{ReadAll: func() ([]byte, error) { return nil, os.ErrPermission }}
	if _, known, err := ReferencedRefs(ioErr); err == nil || known {
		t.Error("an unreadable store reported a known reference set; villa cannot tell what is referenced")
	}
}

// TestReferencesAreCountedOverRefValuesNotComponents is the shared-digest safety
// property. The embedder and the vulkan backend are the same image today, so a
// reference set keyed by component would let updating one release an image the
// other is still running.
func TestReferencesAreCountedOverRefValuesNotComponents(t *testing.T) {
	shared := "example.invalid/toolboxes@sha256:shared"
	buf, deps := bufferDeps(nil)
	if err := Save(deps, State{
		Pins: map[string]Effective{
			string(pins.Embedder):      {Ref: shared},
			string(pins.BackendVulkan): {Ref: shared},
		},
		Previous: map[string]Previous{
			subsystem.Memory.String(): {Refs: map[string]string{string(pins.Qdrant): "example.invalid/qdrant@sha256:old"}},
		},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	_ = buf

	refs, known, err := ReferencedRefs(deps)
	if err != nil {
		t.Fatalf("ReferencedRefs: %v", err)
	}
	if !known {
		t.Fatal("a populated store reported an unknown reference set")
	}
	if !refs[shared] {
		t.Error("the shared image is not in the reference set; prune could delete an image a running backend depends on")
	}
	// A retained previous is a reference too: it is the rollback target, and
	// removing it silently removes the safety net.
	if !refs["example.invalid/qdrant@sha256:old"] {
		t.Error("a retained previous is not referenced; the rollback target would be prunable")
	}
}

// TestStoreHasItsOwnPathAndVersion: this store shares a persistence layer with the
// others, not a schema or a filename. A payload from one must not load as another.
func TestStoreHasItsOwnPathAndVersion(t *testing.T) {
	if Path() == "" {
		t.Fatal("the store resolves to no path")
	}
	// A verify-state document must not load as pin state.
	_, deps := bufferDeps([]byte(`{"schema_version":1,"verdict":"PASS","checked_at":"2026-08-06T09:00:00Z"}`))
	got, err := Load(deps)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Pins) != 0 || got.Serial != 0 {
		t.Errorf("a verify-state document loaded as pin state: %+v", got)
	}
}

// TestFileModesMatchTheOtherStores: the document records what this host runs, so it
// is owner-only like every other store, and its directory is 0700.
func TestFileModesMatchTheOtherStores(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	path := Path()
	if err := WriteFileAtomic(path, []byte(`{"schema_version":1}`)); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 0600", got)
	}
	di, err := os.Stat(dirOf(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Errorf("dir mode = %o, want 0700", got)
	}
}

// dirOf is filepath.Dir without pulling the import in for one call.
func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}
