package orchestrate

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
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
