// Package verifystate store tests guard the verify-search-state.json persistence
// discipline cloned from internal/recall (SURF-04): fail-closed Load (absent /
// corrupt / future-schema ⇒ empty State = "no verified result", NEVER a fabricated
// PASS — T-34-01), version-stamping Save, the atomic 0600/0700 temp+rename writer
// with a traversal guard against the FIXED store root (T-34-04), and the
// verdict+timestamp-only content discipline (T-34-02 — no query/URL content may
// ever enter the host-side file).
package verifystate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestStore is the top-level table of behaviors required by 34-01-PLAN.md Task 1.
// Subtests cover fail-closed Load, version-stamping Save round-trip, the
// SchemaVersion() accessor mirroring the const, the no-content-key discipline, the
// XDG path resolver + traversal guard, and the atomic writer.
func TestStore(t *testing.T) {
	t.Run("LoadFailsClosed", testStoreLoadFailsClosed)
	t.Run("SaveLoadRoundTrip", testStoreSaveLoadRoundTrip)
	t.Run("SchemaVersionMirrorsConst", testStoreSchemaVersionMirrorsConst)
	t.Run("StateHasNoContentKeys", testStoreStateHasNoContentKeys)
	t.Run("VerifyStatePathXDG", testStoreVerifyStatePathXDG)
	t.Run("WriteFileAtomic", testStoreWriteFileAtomic)
}

// testStoreLoadFailsClosed proves Load degrades to an empty State (no error, no
// panic) on an absent store, a corrupt blob, and a future/unknown schema_version —
// an empty state means "no verified result", never a fabricated PASS (T-34-01). A
// nil ReadAll seam and a ReadAll I/O error are REAL errors (wrapped).
func testStoreLoadFailsClosed(t *testing.T) {
	// Nil seam ⇒ error (a programming error, never silent).
	if _, err := Load(Deps{}); err == nil {
		t.Error("Load(nil ReadAll) = nil error, want error")
	}

	// ReadAll I/O error ⇒ wrapped error.
	ioErr := Deps{ReadAll: func() ([]byte, error) { return nil, os.ErrPermission }}
	if _, err := Load(ioErr); err == nil {
		t.Error("Load(ReadAll error) = nil error, want wrapped error")
	}

	// Absent store: ReadAll ⇒ (nil,nil) ⇒ empty State, no error.
	absent := Deps{ReadAll: func() ([]byte, error) { return nil, nil }}
	if got, err := Load(absent); err != nil || got.Verdict != "" || got.CheckedAt != "" {
		t.Errorf("Load(absent) = (%+v, %v), want empty, nil", got, err)
	}

	// Empty blob (zero-length) ⇒ empty State, no error.
	empty := Deps{ReadAll: func() ([]byte, error) { return []byte{}, nil }}
	if got, err := Load(empty); err != nil || got.Verdict != "" {
		t.Errorf("Load(empty) = (%+v, %v), want empty, nil", got, err)
	}

	// Corrupt blob: not JSON ⇒ empty, no error, no panic.
	corrupt := Deps{ReadAll: func() ([]byte, error) { return []byte("{not json"), nil }}
	if got, err := Load(corrupt); err != nil || got.Verdict != "" {
		t.Errorf("Load(corrupt) = (%+v, %v), want empty, nil", got, err)
	}

	// Future/unknown schema: valid JSON, wrong schema_version ⇒ empty, no error —
	// a future schema is NEVER reinterpreted as the current version (so a forged
	// future-schema "PASS" can never surface as a real verdict).
	skew := Deps{ReadAll: func() ([]byte, error) {
		return []byte(`{"schema_version":999,"verdict":"PASS","checked_at":"2026-06-21T00:00:00Z"}`), nil
	}}
	if got, err := Load(skew); err != nil || got.Verdict != "" || got.CheckedAt != "" {
		t.Errorf("Load(skew) = (%+v, %v), want empty, nil", got, err)
	}
}

// testStoreSaveLoadRoundTrip proves Save stamps SchemaVersion=1 before marshal and
// that Load(Save(s)) returns a state identical to the input. A nil WriteAll seam is
// a real error.
func testStoreSaveLoadRoundTrip(t *testing.T) {
	if err := Save(Deps{}, State{}); err == nil {
		t.Error("Save(nil WriteAll) = nil error, want error")
	}

	var buf []byte
	d := Deps{
		WriteAll: func(b []byte) error { buf = append([]byte(nil), b...); return nil },
		ReadAll:  func() ([]byte, error) { return buf, nil },
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}

	in := State{
		Verdict:   "PASS",
		CheckedAt: "2026-06-21T19:00:00Z",
	}
	if err := Save(d, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(d)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.SchemaVersion != verifyStateSchemaVersion {
		t.Errorf("loaded schema_version = %d, want %d (Save must stamp it)", out.SchemaVersion, verifyStateSchemaVersion)
	}
	in.SchemaVersion = verifyStateSchemaVersion
	if !reflect.DeepEqual(out, in) {
		t.Errorf("round-trip state = %+v, want %+v", out, in)
	}

	// Save must STAMP the schema version itself — never trust a caller-supplied one.
	forged := State{SchemaVersion: 999, Verdict: "FAIL", CheckedAt: "2026-06-21T20:00:00Z"}
	if err := Save(d, forged); err != nil {
		t.Fatalf("Save(forged schema): %v", err)
	}
	back, err := Load(d)
	if err != nil {
		t.Fatalf("Load after forged save: %v", err)
	}
	if back.SchemaVersion != verifyStateSchemaVersion || back.Verdict != "FAIL" {
		t.Errorf("Save did not re-stamp schema_version: got %+v", back)
	}
}

// testStoreSchemaVersionMirrorsConst guards that the exported SchemaVersion()
// accessor (the downstream reader-of-record for Plans 03/04) returns EXACTLY the
// unexported verifyStateSchemaVersion const, so a consumer can never silently
// desync from the store's actual on-disk schema version.
func testStoreSchemaVersionMirrorsConst(t *testing.T) {
	if got := SchemaVersion(); got != verifyStateSchemaVersion {
		t.Fatalf("SchemaVersion() = %d, want %d (must mirror the const)", got, verifyStateSchemaVersion)
	}
	if verifyStateSchemaVersion != 1 {
		t.Fatalf("verifyStateSchemaVersion = %d, want 1 (schema v1)", verifyStateSchemaVersion)
	}
}

// testStoreStateHasNoContentKeys is the verdict+timestamp-only security test
// (T-34-02): it marshals a fully-populated State and walks EVERY JSON key,
// asserting none contains a content-bearing token — no query, no URL, no fetched
// content may ever leak into the host-side verify-search-state.json.
func testStoreStateHasNoContentKeys(t *testing.T) {
	populated := State{
		SchemaVersion: verifyStateSchemaVersion,
		Verdict:       "PASS",
		CheckedAt:     "2026-06-21T19:00:00Z",
	}
	blob, err := json.Marshal(populated)
	if err != nil {
		t.Fatalf("marshal populated State: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatalf("re-parse marshaled State: %v", err)
	}

	denied := []string{"query", "url", "content", "fetched", "page"}
	for k := range doc {
		lk := strings.ToLower(k)
		for _, banned := range denied {
			if strings.Contains(lk, banned) {
				t.Errorf("marshaled state JSON key %q contains banned content token %q — verdict/timestamp only (T-34-02)", k, banned)
			}
		}
	}
}

// testStoreVerifyStatePathXDG proves the path resolver honors $XDG_DATA_HOME (the
// fixed villa data-store root) and that assertInsideDir rejects a traversal escape
// measured against that FIXED root (T-34-04).
func testStoreVerifyStatePathXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	want := filepath.Join(tmp, "villa", "verify-search-state.json")
	if got := VerifyStatePath(); got != want {
		t.Errorf("VerifyStatePath() = %q, want %q", got, want)
	}

	// Exercise the guard through WriteFileAtomic (the production write path), so the
	// test still fails if a refactor drops it.
	if err := WriteFileAtomic(want, []byte("{}")); err != nil {
		t.Errorf("WriteFileAtomic rejected a path inside the store root: %v", err)
	}
	escape := filepath.Join(tmp, "villa", "..", "escape.json")
	if err := WriteFileAtomic(escape, []byte("{}")); err == nil {
		t.Error("WriteFileAtomic accepted a traversal escape, want rejection")
	}
	if _, err := os.Stat(escape); !os.IsNotExist(err) {
		t.Errorf("the refused path was created anyway: %v", err)
	}
}

// testStoreWriteFileAtomic proves the atomic writer creates the store dir 0700,
// writes the file 0600 with the exact bytes, leaves NO temp file behind on success
// OR on a failed rename, and refuses a path outside the fixed store root (T-34-04).
func testStoreWriteFileAtomic(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	path := VerifyStatePath()
	data := []byte(`{"schema_version":1,"verdict":"PASS","checked_at":"2026-06-21T19:00:00Z"}`)

	if err := WriteFileAtomic(path, data); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("read back = %q, want %q", got, data)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat store file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("store file mode = %o, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat store dir: %v", err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("store dir mode = %o, want 0700", di.Mode().Perm())
	}

	// No temp residue after a successful write.
	assertNoTempFiles(t, filepath.Dir(path))

	// Traversal: a path outside the fixed store root is refused before any write.
	escape := filepath.Join(filepath.Dir(path), "..", "escape.json")
	if err := WriteFileAtomic(escape, data); err == nil {
		t.Error("WriteFileAtomic accepted a traversal escape, want rejection")
	}
	if _, err := os.Stat(filepath.Join(tmp, "escape.json")); !os.IsNotExist(err) {
		t.Error("traversal escape produced a file outside the store root")
	}

	// Failed rename (target is an existing DIRECTORY) ⇒ error AND temp cleaned up.
	blockedPath := filepath.Join(filepath.Dir(path), "blocked.json")
	if err := os.MkdirAll(blockedPath, 0o700); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}
	if err := WriteFileAtomic(blockedPath, data); err == nil {
		t.Error("WriteFileAtomic over an existing directory = nil error, want error")
	}
	assertNoTempFiles(t, filepath.Dir(path))
}

// assertNoTempFiles fails the test if any verify-state temp file is left in dir —
// the writer must clean its temp up on every error branch and after every rename.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read store dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file %q left behind in store dir", e.Name())
		}
	}
}
