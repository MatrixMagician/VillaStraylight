package recall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
)

// TestWriteScopeIsOwnerOnlyAndAtomic: the scope document lands at
// <data root>/recall-scope/scope.json, directory 0700 and file 0600 (ids only, but
// still not for another local account), and a rewrite replaces it whole (ADR-0042).
func TestWriteScopeIsOwnerOnlyAndAtomic(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	first := ScopeOf(State{KnowledgeID: "kb-1", Chats: map[string]ChatState{"c1": {UserID: "u1", FileID: "f1"}}})
	if err := WriteScope(first); err != nil {
		t.Fatalf("WriteScope: %v", err)
	}
	if want := filepath.Join(pathsafe.DataRoot(), "recall-scope", "scope.json"); ScopePath() != want {
		t.Fatalf("ScopePath = %q, want %q", ScopePath(), want)
	}
	if got := filepath.Dir(ScopePath()); got != ScopeDir() {
		t.Errorf("ScopeDir = %q, want the scope file's directory %q", ScopeDir(), got)
	}
	if fi, err := os.Stat(ScopePath()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("scope file mode = %v, %v; want 0600", fi, err)
	}
	if fi, err := os.Stat(ScopeDir()); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("scope dir mode = %v, %v; want 0700", fi, err)
	}

	read := func() Scope {
		t.Helper()
		b, err := os.ReadFile(ScopePath())
		if err != nil {
			t.Fatal(err)
		}
		var sc Scope
		if err := json.Unmarshal(b, &sc); err != nil {
			t.Fatalf("scope file is not JSON: %v\n%s", err, b)
		}
		return sc
	}
	if got := read(); !reflect.DeepEqual(got, first) {
		t.Errorf("scope on disk = %+v, want %+v", got, first)
	}

	second := ScopeOf(State{KnowledgeID: "kb-1", Chats: map[string]ChatState{"c2": {UserID: "u2", FileID: "f2"}}})
	if err := WriteScope(second); err != nil {
		t.Fatalf("WriteScope (rewrite): %v", err)
	}
	if got := read(); !reflect.DeepEqual(got, second) {
		t.Errorf("a rewrite did not replace the scope: %+v, want %+v", got, second)
	}
}

// TestWriteScopeRefusesARelativeDataRoot: the shared guard stands, so a hand-set relative
// $XDG_DATA_HOME cannot scatter a villa tree into the working directory.
func TestWriteScopeRefusesARelativeDataRoot(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "relative/dir")
	if err := WriteScope(Scope{}); err == nil {
		t.Error("WriteScope wrote under a relative data root")
	}
}
