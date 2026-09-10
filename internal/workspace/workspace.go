// Package workspace is the pure core behind the registered grant list (spec
// §3.1): a workspace is a folder the operator has explicitly registered, and
// registration is where fail-closed lives. Register never accepts a path it
// cannot resolve and contain — a relative path, one outside the home
// directory, one overlapping villa's own config/data roots, or one nested
// with an existing grant all refuse with a typed Refusal naming why and what
// to do about it. `villa work` trusts the grant list precisely because
// Register is the only way onto it.
//
// os appears only as the FileInfo type in Deps; every actual filesystem call
// is injected, so the core is testable off-hardware with a fake Deps.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
)

// Deps are the injected filesystem seams. liveWorkspaceDeps in cmd/villa wires
// these to the real host; tests wire fakes (or real os calls against a
// t.TempDir(), which needs no faking at all).
type Deps struct {
	Home         func() (string, error)
	ConfigRoot   func() string
	DataRoot     func() string
	Stat         func(string) (os.FileInfo, error)
	EvalSymlinks func(string) (string, error)
}

// RefusalKind names why Register refused a path (spec §3.1).
type RefusalKind int

const (
	// Relative reports a path that was not given as absolute.
	Relative RefusalKind = iota
	// NotResolved reports a path Stat or EvalSymlinks could not resolve.
	NotResolved
	// Home reports the home directory itself.
	Home
	// OutsideHome reports a path outside the home directory.
	OutsideHome
	// VillaRoot reports a path overlapping villa's XDG config or data root.
	VillaRoot
	// Nested reports a path nested with an existing grant, either direction.
	Nested
	// Missing reports a path that does not exist.
	Missing
	// NotDir reports a path that exists but is not a directory.
	NotDir
)

// String names the refusal kind for error text and remediation.
func (k RefusalKind) String() string {
	switch k {
	case Relative:
		return "relative"
	case NotResolved:
		return "not-resolved"
	case Home:
		return "home"
	case OutsideHome:
		return "outside-home"
	case VillaRoot:
		return "villa-root"
	case Nested:
		return "nested"
	case Missing:
		return "missing"
	case NotDir:
		return "not-dir"
	default:
		return "unknown"
	}
}

// Refusal is the typed error Register returns for every non-success path. It
// carries a Remediation so the CLI can print what to do, not just what failed.
type Refusal struct {
	Kind        RefusalKind
	Path        string
	Remediation string
}

// Error satisfies the error interface.
func (r Refusal) Error() string {
	return fmt.Sprintf("workspace %q refused (%s): %s", r.Path, r.Kind, r.Remediation)
}

// refuse builds a Refusal for path with a kind-specific remediation.
func refuse(kind RefusalKind, path, remediation string) Refusal {
	return Refusal{Kind: kind, Path: path, Remediation: remediation}
}

// Register validates path against every spec §3.1 refusal and, on success,
// returns cfg with the resolved absolute path appended to cfg.Workspace.
// Re-registering an already-granted path is a no-op, not a duplicate.
func Register(cfg config.VillaConfig, path string, d Deps) (config.VillaConfig, error) {
	if !filepath.IsAbs(path) {
		return cfg, refuse(Relative, path, "pass an absolute path, e.g. /home/you/project")
	}

	info, err := d.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, refuse(Missing, path, "create the folder first, then register it")
		}
		return cfg, refuse(NotResolved, path, fmt.Sprintf("cannot resolve %q: %v", path, err))
	}
	if !info.IsDir() {
		return cfg, refuse(NotDir, path, "register a directory, not a file")
	}

	resolved, err := d.EvalSymlinks(path)
	if err != nil {
		return cfg, refuse(NotResolved, path, fmt.Sprintf("cannot resolve %q: %v", path, err))
	}
	resolved = filepath.Clean(resolved)

	home, err := d.Home()
	if err != nil {
		return cfg, fmt.Errorf("workspace: resolve home directory: %w", err)
	}
	home = filepath.Clean(home)

	if resolved == home {
		return cfg, refuse(Home, resolved, "register a subfolder of your home directory, not home itself")
	}
	if err := pathsafe.Inside(resolved, home); err != nil {
		return cfg, refuse(OutsideHome, resolved, "register a folder under your home directory")
	}

	if overlaps(resolved, d.ConfigRoot()) || overlaps(resolved, d.DataRoot()) {
		return cfg, refuse(VillaRoot, resolved, "choose a folder outside villa's config and data directories")
	}

	for _, existing := range cfg.Workspace {
		if resolved == existing {
			return cfg, nil // idempotent re-add: already granted, no duplicate
		}
		if overlaps(resolved, existing) {
			return cfg, refuse(Nested, resolved,
				fmt.Sprintf("%q overlaps the existing workspace %q — remove it first", resolved, existing))
		}
	}

	next := cfg
	next.Workspace = append(append([]string{}, cfg.Workspace...), resolved)
	return next, nil
}

// Remove forgets the grant at path (an exact match against the persisted,
// already-resolved entries) and touches no file. Callers resolve an operator-
// supplied path with Registered first and pass the resolved match here.
func Remove(cfg config.VillaConfig, path string) (config.VillaConfig, error) {
	idx := -1
	for i, existing := range cfg.Workspace {
		if existing == path {
			idx = i
			break
		}
	}
	if idx == -1 {
		return cfg, fmt.Errorf("workspace: %q is not a registered workspace", path)
	}
	next := cfg
	kept := append([]string{}, cfg.Workspace[:idx]...)
	next.Workspace = append(kept, cfg.Workspace[idx+1:]...)
	return next, nil
}

// Registered resolves path and reports whether it matches a registered grant,
// returning the resolved match. An unresolvable or unregistered path is false,
// with no error: this is a lookup, not a validation.
func Registered(cfg config.VillaConfig, path string, d Deps) (string, bool) {
	if !filepath.IsAbs(path) {
		return "", false
	}
	resolved, err := d.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	resolved = filepath.Clean(resolved)
	for _, existing := range cfg.Workspace {
		if resolved == existing {
			return resolved, true
		}
	}
	return "", false
}

// overlaps reports whether a and b share bytes: equal, or either contains the
// other. Reused for the villa-root check and the nested-either-direction
// check, both of which are exactly this predicate against a different root.
func overlaps(a, b string) bool {
	if b == "" {
		return false
	}
	return pathsafe.Inside(a, b) == nil || pathsafe.Inside(b, a) == nil
}
