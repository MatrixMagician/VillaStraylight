package pathsafe

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyRejects is the containment predicate this package replaces, reproduced
// verbatim from the thirteen copies it was lifted from. It exists only so
// TestInsideMatchesLegacyPredicate can prove the filepath.IsLocal form agrees
// with it; nothing else may call it.
func legacyRejects(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

// TestInsideMatchesLegacyPredicate is the load-bearing test of this package: it
// asserts filepath.IsLocal reaches the same verdict as the hand-rolled predicate
// on every case the callers care about. If these ever diverge, thirteen
// traversal guards changed behaviour at once, so the divergence must fail here
// rather than in production.
func TestInsideMatchesLegacyPredicate(t *testing.T) {
	cases := []struct {
		name     string
		dir      string
		path     string
		wantRejt bool
	}{
		{"path equals dir", "/a/b", "/a/b", false},
		{"direct child", "/a/b", "/a/b/c.json", false},
		{"deep child", "/a/b", "/a/b/c/d/e.json", false},
		{"dotfile child", "/a/b", "/a/b/.hidden", false},
		{"dot segment collapses", "/a/b", "/a/b/./c", false},
		{"parent", "/a/b", "/a", true},
		{"sibling", "/a/b", "/a/c", true},
		{"filesystem root", "/a/b", "/", true},
		{"traversal escape", "/a/b", "/a/b/../../etc/passwd", true},
		{"traversal to parent", "/a/b", "/a/b/..", true},
		{"unrelated absolute", "/a/b", "/etc/passwd", true},
		{"shared prefix but not a child", "/a/b", "/a/bb/c", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotErr := Inside(tc.path, tc.dir)
			gotRejt := gotErr != nil

			if gotRejt != tc.wantRejt {
				t.Errorf("Inside(%q, %q) rejected=%v, want %v (err=%v)",
					tc.path, tc.dir, gotRejt, tc.wantRejt, gotErr)
			}
			if gotRejt && !errors.Is(gotErr, ErrEscapes) {
				t.Errorf("Inside(%q, %q) error %v does not wrap ErrEscapes", tc.path, tc.dir, gotErr)
			}

			// The parity assertion: derive the legacy verdict the same way the
			// old copies did and require it to agree.
			absDir, err := filepath.Abs(filepath.Clean(tc.dir))
			if err != nil {
				t.Fatalf("Abs(%q): %v", tc.dir, err)
			}
			absPath, err := filepath.Abs(filepath.Clean(tc.path))
			if err != nil {
				t.Fatalf("Abs(%q): %v", tc.path, err)
			}
			rel, err := filepath.Rel(absDir, absPath)
			if err != nil {
				t.Fatalf("Rel(%q, %q): %v", absDir, absPath, err)
			}
			if legacy := legacyRejects(rel); legacy != gotRejt {
				t.Errorf("DIVERGENCE for rel=%q: legacy rejected=%v, IsLocal-based rejected=%v",
					rel, legacy, gotRejt)
			}
		})
	}
}

// TestAssertRoot guards the two conditions a containment root must meet before
// it can bound anything. An empty root must not be silently read as "/", and a
// relative one must not be resolved against the process working directory --
// $XDG_DATA_HOME is attacker-controllable, so both are refusals.
func TestAssertRoot(t *testing.T) {
	if err := AssertRoot(""); !errors.Is(err, ErrRootEmpty) {
		t.Errorf("AssertRoot(\"\") = %v, want ErrRootEmpty", err)
	}
	if err := AssertRoot("relative/path"); !errors.Is(err, ErrRootNotAbsolute) {
		t.Errorf("AssertRoot(relative) = %v, want ErrRootNotAbsolute", err)
	}
	if err := AssertRoot("../escaping"); !errors.Is(err, ErrRootNotAbsolute) {
		t.Errorf("AssertRoot(..) = %v, want ErrRootNotAbsolute", err)
	}
	if err := AssertRoot("/absolute"); err != nil {
		t.Errorf("AssertRoot(/absolute) = %v, want nil", err)
	}
}

// TestDataRoot pins the three-step fallback chain the eight former copies
// implemented: $XDG_DATA_HOME wins when set, then the home directory, and the
// villa suffix is always appended. A relative XDG value must pass through
// unrewritten so the caller can refuse it and explain why.
func TestDataRoot(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg/data")
	if got, want := DataRoot(), filepath.Join("/xdg/data", "villa"); got != want {
		t.Errorf("DataRoot() with XDG set = %q, want %q", got, want)
	}

	t.Setenv("XDG_DATA_HOME", "relative")
	if got, want := DataRoot(), filepath.Join("relative", "villa"); got != want {
		t.Errorf("DataRoot() with relative XDG = %q, want %q (must pass through unrewritten)", got, want)
	}
	if err := AssertRoot(DataRoot()); !errors.Is(err, ErrRootNotAbsolute) {
		t.Errorf("a relative XDG_DATA_HOME must be refusable via AssertRoot, got %v", err)
	}

	t.Setenv("XDG_DATA_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	if got, want := DataRoot(), filepath.Join(home, ".local", "share", "villa"); got != want {
		t.Errorf("DataRoot() with XDG unset = %q, want %q", got, want)
	}
}

// TestWriteFileAtomicWrites covers the happy path: the bytes land, the mode is
// what was asked for, and no temp file survives.
func TestWriteFileAtomicWrites(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")

	if err := WriteFileAtomic(root, path, []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != `{"v":1}` {
		t.Errorf("contents = %q, want %q", got, `{"v":1}`)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	assertNoTmpRemnant(t, root)
}

// TestWriteFileAtomicTightensExistingMode guards the chmod-after-rename step:
// a file already on disk at a looser mode must not survive at that mode. The
// secret files this writer serves (0600 env files holding generated bearers)
// depend on it.
func TestWriteFileAtomicTightensExistingMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "secret.env")

	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed loose file: %v", err)
	}
	if err := WriteFileAtomic(root, path, []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 -- a pre-existing 0644 file survived at its old mode", fi.Mode().Perm())
	}
}

// TestWriteFileAtomicRefusesEscape is the fail-closed assertion: a path outside
// the supplied root is refused, and nothing is created on the way to finding
// out. This is why root is a required parameter rather than an optional
// pre-check a migrating caller could omit.
func TestWriteFileAtomicRefusesEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "elsewhere.json")

	err := WriteFileAtomic(root, outside, []byte("nope"), 0o600)
	if !errors.Is(err, ErrEscapes) {
		t.Fatalf("WriteFileAtomic outside root = %v, want ErrEscapes", err)
	}
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Errorf("the refused path was created anyway: %v", statErr)
	}
	assertNoTmpRemnant(t, root)
}

// TestWriteFileAtomicRefusesBadRoot: an unusable root is refused before any
// filesystem work, so a relative or empty $XDG_DATA_HOME cannot reach a write.
func TestWriteFileAtomicRefusesBadRoot(t *testing.T) {
	if err := WriteFileAtomic("", "/tmp/x.json", nil, 0o600); !errors.Is(err, ErrRootEmpty) {
		t.Errorf("empty root = %v, want ErrRootEmpty", err)
	}
	if err := WriteFileAtomic("relative", "relative/x.json", nil, 0o600); !errors.Is(err, ErrRootNotAbsolute) {
		t.Errorf("relative root = %v, want ErrRootNotAbsolute", err)
	}
}

// TestWriteFileAtomicMissingDirLeavesNothing: the writer does not create the
// parent directory (that mode is caller policy), so a missing one is an error --
// and it must not leave a remnant behind.
func TestWriteFileAtomicMissingDirLeavesNothing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "no-such-dir", "state.json")

	if err := WriteFileAtomic(root, path, []byte("x"), 0o600); err == nil {
		t.Fatal("WriteFileAtomic into a missing directory returned nil, want an error")
	}
	assertNoTmpRemnant(t, root)
}

// assertNoTmpRemnant fails if any *.tmp file survives under dir, mirroring the
// remnant assertions the orchestrate unit writers already carry.
func assertNoTmpRemnant(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp remnant left behind: %s", e.Name())
		}
	}
}
