package orchestrate

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// reconcile.go is the content-hash idempotency core (CLI-01 / D-06) plus the only
// filesystem writer. Reconcile is pure (sha256 render-vs-disk compare); WriteUnits
// is the impure half — it writes a sibling temp in the SAME dir then os.Rename
// (atomic, mirrors internal/download), and refuses any target resolving outside the
// unit dir (assertInsideDir, mirrors internal/config; threats T-03-02/T-03-03).

// unitFileMode is the mode for written unit files — non-secret (the secret config
// stays 0600 in internal/config), world-readable so systemd --user can read them.
const unitFileMode os.FileMode = 0o644

// Reconcile compares each rendered unit's content hash against the same-named file
// already on disk in unitDir. A unit whose on-disk file is absent or whose hash
// differs is Changed; a byte-identical one is Unchanged. It performs NO writes:
// identical config yields an empty Changed slice — a true no-op.
func Reconcile(units []Unit, unitDir string) (Plan, error) {
	var plan Plan
	for _, u := range units {
		path := filepath.Join(unitDir, u.Name)
		onDisk, err := os.ReadFile(path) //nolint:gosec // path = unitDir + validated unit name
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				plan.Changed = append(plan.Changed, u)
				continue
			}
			return Plan{}, fmt.Errorf("orchestrate: reconcile read %q: %w", path, err)
		}
		if sha256.Sum256(onDisk) == sha256.Sum256([]byte(u.Text)) {
			plan.Unchanged = append(plan.Unchanged, u)
		} else {
			plan.Changed = append(plan.Changed, u)
		}
	}
	return plan, nil
}

// WriteUnits writes every Changed unit atomically into unitDir: render to
// <name>.tmp in the SAME directory, fsync, then os.Rename to <name> so a half-
// written unit is never observable (T-03-03). Each target is traversal-guarded —
// a unit name resolving outside unitDir is refused before any write (T-03-02).
// Unchanged units are left untouched (no spurious daemon-reload/restart).
func WriteUnits(plan Plan, unitDir string) error {
	for _, u := range plan.Changed {
		target := filepath.Join(unitDir, u.Name)
		if err := assertInsideDir(target, unitDir); err != nil {
			return err
		}
		if err := atomicWrite(target, []byte(u.Text)); err != nil {
			return fmt.Errorf("orchestrate: write unit %q: %w", u.Name, err)
		}
	}
	return nil
}

// atomicWrite writes data to a sibling temp in target's directory, fsyncs it, and
// renames it over target (same filesystem ⇒ atomic). The temp is removed on any
// failure so no *.tmp is ever left behind.
func atomicWrite(target string, data []byte) error {
	dir := filepath.Dir(target)
	tmp := target + ".tmp"

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, unitFileMode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Best-effort dir fsync so the rename is durable; non-fatal if it fails.
	if df, derr := os.Open(dir); derr == nil {
		_ = df.Sync()
		_ = df.Close()
	}
	return nil
}

// assertInsideDir verifies target resolves within dir, rejecting traversal escapes
// (V12 / T-03-02). Mirrors internal/config/villaconfig.go:assertInsideDir.
func assertInsideDir(target, dir string) error {
	absDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("orchestrate: refusing to write %q outside unit dir %q", absPath, absDir)
	}
	return nil
}
