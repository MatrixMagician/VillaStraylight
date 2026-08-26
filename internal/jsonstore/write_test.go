package jsonstore_test

// write_test.go covers the live write seam, jsonstore's Store.WriteFileAtomic —
// the function the cmd tier wires as WriteAll for every store (verify state,
// recall state, usage totals).
//
// It was entirely uncovered, despite being the one place in the persistence layer
// that touches the real filesystem AND the place a documented ordering constraint
// lives: the containment guard must run BEFORE the MkdirAll, or a `..`-bearing
// path gets a directory created for it OUTSIDE the data root before the write
// itself refuses. A guard that fires after the side effect it is guarding is not
// a guard, and nothing was asserting the order.
//
// The untrusted input here is $XDG_DATA_HOME, which pathsafe.DataRoot reads, so
// these tests set it to a temp dir and treat it as the attacker-controlled value
// it is.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/verifystate"
)

// TestWriteFileAtomicWritesUnderTheDataRoot is the happy path: the document lands
// at the store path with owner-only modes on both the file and its directory.
func TestWriteFileAtomicWritesUnderTheDataRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)

	path := verifystate.Path()
	if err := verifystate.WriteFileAtomic(path, []byte(`{"schema_version":1}`)); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != `{"schema_version":1}` {
		t.Errorf("round-tripped %q, want the written bytes", got)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// 0600: the store can hold verification verdicts and usage history, so it is
	// owner-only rather than world-readable.
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("store file mode = %o, want 600", perm)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("store dir mode = %o, want 700", perm)
	}
}

// TestWriteFileAtomicReplacesRatherThanAppends pins full-document replacement. The
// stores marshal the WHOLE document every save, so a second write of shorter
// content must not leave a tail of the first behind (which would then fail to
// parse and, by the fail-closed Load, silently read as "nothing recorded").
func TestWriteFileAtomicReplacesRatherThanAppends(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	path := verifystate.Path()

	if err := verifystate.WriteFileAtomic(path, []byte(`{"a":"a long first document"}`)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := verifystate.WriteFileAtomic(path, []byte(`{"b":1}`)); err != nil {
		t.Fatalf("second write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != `{"b":1}` {
		t.Fatalf("store = %q, want only the second document — a replace, not an append", got)
	}
	if !json.Valid(got) {
		t.Error("store is not valid JSON after a shorter rewrite")
	}
}

// TestWriteFileAtomicRefusesEscapeBeforeCreatingAnything is the ordering guard.
//
// A path escaping the data root must be refused, AND the refusal must happen
// before the MkdirAll: otherwise the traversal has already created a directory
// outside the root as a side effect, which is exactly the thing the guard exists
// to prevent.
func TestWriteFileAtomicRefusesEscapeBeforeCreatingAnything(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "data")
	t.Setenv("XDG_DATA_HOME", root)

	// Aim outside the villa data root, at a directory that does not yet exist, so
	// its creation is observable.
	outside := filepath.Join(base, "data", "villa", "..", "..", "escaped", "state.json")

	err := verifystate.WriteFileAtomic(outside, []byte(`{"pwned":true}`))
	if err == nil {
		t.Fatal("WriteFileAtomic accepted a path outside the data root")
	}
	if !strings.Contains(err.Error(), "refusing to write outside") {
		t.Errorf("error = %v, want a refuse-to-write-outside message", err)
	}
	if _, statErr := os.Stat(filepath.Join(base, "escaped")); !os.IsNotExist(statErr) {
		t.Fatal("the refused traversal still created its directory outside the data root — " +
			"the containment guard must run BEFORE the MkdirAll, not after")
	}
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Error("a refused write left a file behind")
	}
}

// TestWriteFileAtomicRefusesRelativeDataHome covers the other untrusted-env case: a
// RELATIVE $XDG_DATA_HOME cannot bound anything, because it resolves against
// whatever the process CWD happens to be. It must be refused rather than quietly
// resolved.
func TestWriteFileAtomicRefusesRelativeDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "relative-not-absolute")

	err := verifystate.WriteFileAtomic(verifystate.Path(), []byte(`{}`))
	if err == nil {
		t.Fatal("WriteFileAtomic accepted a relative $XDG_DATA_HOME")
	}
	if _, statErr := os.Stat("relative-not-absolute"); !os.IsNotExist(statErr) {
		t.Error("a refused relative data home still created a directory in the CWD")
	}
}

// TestWriteFileAtomicLeavesNoTempRemnant asserts the temp file used for the
// atomic rename does not survive a successful write. A leftover "villa-*.tmp"
// beside the real store would make the no-remnant assertions elsewhere meaningless.
func TestWriteFileAtomicLeavesNoTempRemnant(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	path := verifystate.Path()

	if err := verifystate.WriteFileAtomic(path, []byte(`{"ok":1}`)); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	remnants, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(remnants) != 0 {
		t.Errorf("temp remnants left after a successful write: %v", remnants)
	}
}

// TestReportedSchemaVersionMatchesWhatSaveStamps guards the accessor the backup
// manifest reads. Each store returns its own constant, so nothing structurally
// ties that constant to the value Save actually stamps onto the document — and a
// store whose reported version drifted from its stamped one would make a backup's
// skew comparison silently wrong.
func TestReportedSchemaVersionMatchesWhatSaveStamps(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)

	reported := verifystate.SchemaVersion()
	if reported <= 0 {
		t.Fatalf("SchemaVersion() = %d, want a positive version", reported)
	}

	// The reported version must match what a save actually stamps, or the backup
	// manifest records a version the store does not write.
	var stamped []byte
	deps := verifystate.Deps{
		WriteAll: func(data []byte) error { stamped = data; return nil },
		ReadAll:  func() ([]byte, error) { return stamped, nil },
	}
	if err := verifystate.Save(deps, verifystate.State{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var doc struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(stamped, &doc); err != nil {
		t.Fatalf("unmarshal stamped doc: %v", err)
	}
	if doc.SchemaVersion != reported {
		t.Errorf("SchemaVersion() reports %d but Save stamps %d", reported, doc.SchemaVersion)
	}
}
