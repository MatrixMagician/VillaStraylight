// Package pathsafe is the shared filesystem seam: path containment, the XDG
// data-root resolution, and atomic file writes.
//
// Before this package these three answers were re-derived per package — the
// containment predicate in thirteen places, the data-root chain in eight, an
// atomic writer in five. They are here so there is one of each to audit.
//
// Containment is fail-closed by construction: WriteFileAtomic takes the root it
// must stay inside as a required argument, so a caller cannot write first and
// check later. That matters because the untrusted input is $XDG_DATA_HOME
// itself: an attacker-controlled env var can hold `..` or a relative value, so
// a resolved path must be measured against a root the caller chose, never
// against its own parent — which by construction can never escape.
package pathsafe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Sentinel errors, so callers can add their own message prefix while tests
// still match the condition with errors.Is.
var (
	// ErrEscapes reports a path that resolves outside the directory it must stay within.
	ErrEscapes = errors.New("path escapes its parent directory")
	// ErrRootEmpty reports an empty containment root — refused rather than treated as "/".
	ErrRootEmpty = errors.New("containment root is empty")
	// ErrRootNotAbsolute reports a relative containment root, which cannot bound anything.
	ErrRootNotAbsolute = errors.New("containment root is not absolute")
)

// Inside reports whether path resolves within dir, returning ErrEscapes if not.
//
// Both arguments are cleaned and made absolute before comparison, then measured
// with filepath.Rel + filepath.IsLocal. IsLocal is exactly the predicate the
// hand-rolled checks spelled out by hand (reject "..", a "../"-prefix, or an
// absolute result); pathsafe_test.go asserts the two agree case for case.
//
// A path equal to dir is inside it. Symlinks are not resolved: this is a lexical
// check, matching the behaviour it replaces.
func Inside(path, dir string) error {
	absDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return err
	}
	if !filepath.IsLocal(rel) {
		return fmt.Errorf("%q outside %q: %w", absPath, absDir, ErrEscapes)
	}
	return nil
}

// AssertRoot rejects a containment root that cannot bound anything: empty, or
// relative. Call it on a root derived from the environment before trusting it.
func AssertRoot(root string) error {
	if root == "" {
		return ErrRootEmpty
	}
	if !filepath.IsAbs(root) {
		return fmt.Errorf("%q: %w", root, ErrRootNotAbsolute)
	}
	return nil
}

// DataRoot resolves villa's user data root: $XDG_DATA_HOME/villa, falling back
// to ~/.local/share/villa, then /var/tmp/villa when the home directory is
// unreadable.
//
// The result is not validated — a relative $XDG_DATA_HOME propagates through so
// the caller can refuse it with AssertRoot and say why, rather than having it
// silently rewritten here.
func DataRoot() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "villa")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "villa")
	}
	return filepath.Join("/var/tmp", "villa")
}

// WriteFileAtomic writes data to path via a temp file in the same directory,
// then renames it into place. The file ends up at mode whether or not it already
// existed at a looser one.
//
// path must resolve inside root, which is checked before anything is created
// root is a parameter rather than an internal default so the guard cannot be
// forgotten at a call site.
//
// The parent directory must already exist. Creating it is left to the caller
// because the right directory mode is caller policy (0700 for a private store,
// 0755 for a unit directory), and inventing a parameter for it would hide that
// choice.
//
// Durability: contents are fsynced before the rename and the parent directory is
// fsynced after it, so a crash leaves either the old file or the new one, never
// a truncated one. The directory fsync is best-effort.
func WriteFileAtomic(root, path string, data []byte, mode os.FileMode) error {
	if err := AssertRoot(root); err != nil {
		return err
	}
	if err := Inside(path, root); err != nil {
		return err
	}

	dir := filepath.Dir(path)
	// A "*.tmp" pattern keeps the no-remnant assertions in the orchestrate tests
	// meaningful: any leftover is still discoverable by suffix.
	tmp, err := os.CreateTemp(dir, "villa-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// Best-effort: on every failure path below the temp is removed, so a failed
	// write never leaves a partial file beside the real one.
	cleanup := func() { _ = os.Remove(tmpName) }

	// Chmod before the write so the contents never exist at the default 0600
	// CreateTemp mode when the caller asked for something tighter.
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp into place: %w", err)
	}
	// Rename preserves the temp's mode, but tighten explicitly so a pre-existing
	// looser file does not survive at its old mode.
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	if df, derr := os.Open(dir); derr == nil {
		_ = df.Sync()
		_ = df.Close()
	}
	return nil
}
