package orchestrate

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// reconcile.go is the content-hash idempotency core plus the only
// filesystem writer. Reconcile is pure (sha256 render-vs-disk compare); WriteUnits
// is the impure half — it writes a sibling temp in the SAME dir then os.Rename
// (atomic, mirrors internal/download), and refuses any target resolving outside the
// unit dir (assertInsideDir, mirrors internal/config; threats).

// unitFileMode is the mode for written unit files — non-secret (the secret config
// stays 0600 in internal/config), world-readable so systemd --user can read them.
const unitFileMode os.FileMode = 0o644

// Reconcile compares each rendered unit's content hash against the same-named file
// already on disk in unitDir. A unit whose on-disk file is absent or whose hash
// differs is Changed; a byte-identical one is Unchanged; a registry unit on disk that
// is not rendered is Removed (Orphans). It performs NO writes: identical config
// yields an empty Changed and Removed — a true no-op.
func Reconcile(units []Unit, unitDir string) (Plan, error) {
	removed, err := Orphans(units, func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(unitDir, name)) //nolint:gosec // unitDir + a registry unit name
	})
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Removed: removed}
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

// Orphans returns every unit the subsystem registry declares (subsystem.Every ×
// EveryUnit) that read finds on disk and rendered does not name, in registry order,
// each carrying its on-disk bytes. read reports an absent unit as fs.ErrNotExist.
// A unit outside the registry (the operator's own file, a resident slot) is never an
// orphan, so nothing villa did not declare is ever removed (ADR-0035).
func Orphans(rendered []Unit, read func(name string) ([]byte, error)) ([]Unit, error) {
	names := make(map[string]bool, len(rendered))
	for _, u := range rendered {
		names[u.Name] = true
	}
	var orphans []Unit
	for _, k := range subsystem.Every {
		declared, _ := k.EveryUnit()
		for _, name := range declared {
			if names[name] {
				continue
			}
			onDisk, err := read(name)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("orchestrate: read unit %q: %w", name, err)
			}
			orphans = append(orphans, Unit{Name: name, Text: string(onDisk)})
		}
	}
	return orphans, nil
}

// RemoveUnits removes every Removed unit's file from unitDir, traversal-guarded like
// WriteUnits; an already absent file is the goal state. It never touches Changed.
func RemoveUnits(plan Plan, unitDir string) error {
	for _, u := range plan.Removed {
		target := filepath.Join(unitDir, u.Name)
		if err := pathsafe.Inside(target, unitDir); err != nil {
			return fmt.Errorf("orchestrate: remove unit %q: %w", u.Name, err)
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("orchestrate: remove unit %q: %w", u.Name, err)
		}
	}
	return nil
}

// WriteUnits writes every Changed unit atomically into unitDir: render to
// <name>.tmp in the SAME directory, fsync, then os.Rename to <name> so a half-
// written unit is never observable. Each target is traversal-guarded
// a unit name resolving outside unitDir is refused before any write.
// Unchanged units are left untouched (no spurious daemon-reload/restart).
func WriteUnits(plan Plan, unitDir string) error {
	for _, u := range plan.Changed {
		target := filepath.Join(unitDir, u.Name)
		// The containment guard is part of the write call, not a separate step
		// before it, so a unit name resolving outside unitDir cannot be written
		// even if a future caller forgets to check first.
		if err := pathsafe.WriteFileAtomic(unitDir, target, []byte(u.Text), unitFileMode); err != nil {
			return fmt.Errorf("orchestrate: write unit %q: %w", u.Name, err)
		}
	}
	return nil
}
