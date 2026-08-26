// Package jsonstore is the persistence layer three stores used to implement
// separately: read and write one JSON document at one path under the user data
// root.
//
// Verify state, recall state and the store half of usage totals were line-for-line
// siblings — a schema-version accessor, a save, a load, a root resolver, a path
// builder, a containment guard and an atomic writer, in that order, three times
// over. Once the shared path and write helpers landed (internal/pathsafe), what was
// left of each was a type and a filename.
//
// # What is deliberately NOT flattened
//
// Each store keeps its OWN schema version, and they are not interchangeable. A
// store must refuse a payload from a version it does not understand rather than
// silently misreading it, so the version is a per-store parameter here, never a
// package-wide constant.
//
// The fail-closed Load semantics are the load-bearing part and are preserved
// exactly: an absent store, a corrupt blob, and a schema mismatch all yield the
// ZERO value with no error. That is what makes "no verified result" and "nothing
// indexed" impossible to confuse with a fabricated PASS or a fabricated index. An
// unknown future schema is never reinterpreted as the current one. A nil seam or a
// real read error stay real errors.
//
// Counter-folding, staleness classification and every other domain rule stay in
// their own packages. Only persistence moved.
package jsonstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
)

// FileMode / DirMode are the owner-only modes the atomic writer enforces on every
// store document and its directory (info-disclosure mitigation), matching config.
const (
	FileMode os.FileMode = 0o600
	DirMode  os.FileMode = 0o700
)

// Deps is the injectable byte-I/O seam. The core marshals and parses; these funcs
// do the host I/O, so a store is fully testable off-hardware with a buffer-backed
// Deps.
type Deps struct {
	// WriteAll writes the whole marshaled store, replacing any prior content.
	WriteAll func(data []byte) error
	// ReadAll returns the whole store's bytes, or (nil, nil) when no store exists yet.
	ReadAll func() ([]byte, error)
	// Now supplies a clock for callers that want a deterministic timestamp seam.
	Now func() time.Time
}

// Versioned is the one thing a stored document must be able to do: report and
// stamp its own schema version. Taking it as a constraint rather than embedding a
// shared struct keeps each document's JSON field order, and therefore its on-disk
// bytes, exactly as it was.
type Versioned interface {
	// GetSchemaVersion returns the version carried by this value.
	GetSchemaVersion() int
	// SetSchemaVersion stamps the store's version onto this value.
	SetSchemaVersion(v int)
}

// Store persists one JSON document of type T at one filename under the data root.
//
// T is constrained to a POINTER to a Versioned value (*State, not State) because
// SetSchemaVersion must mutate. Save and Load work in values of the pointed-to
// type, so callers keep passing and receiving plain structs.
type Store[T any, PT interface {
	*T
	Versioned
}] struct {
	// name is the store's package-facing name, used to prefix errors.
	name string
	// filename is the document's base name under the data root.
	filename string
	// schemaVersion is this store's OWN version, independent of every other store's.
	schemaVersion int
}

// New builds a store for one document type. name prefixes its errors, filename is
// its base name under the data root, and schemaVersion is its own self-version.
func New[T any, PT interface {
	*T
	Versioned
}](name, filename string, schemaVersion int) Store[T, PT] {
	return Store[T, PT]{name: name, filename: filename, schemaVersion: schemaVersion}
}

// Path resolves the single mutable document: <data root>/<filename>.
func (s Store[T, PT]) Path() string {
	return filepath.Join(pathsafe.DataRoot(), s.filename)
}

// Save stamps this store's schema version, marshals the whole document, and writes
// it through the seam (full-file replace, never an append). The version is ALWAYS
// stamped here — a caller-supplied value is never trusted.
func (s Store[T, PT]) Save(d Deps, v T) error {
	if d.WriteAll == nil {
		return fmt.Errorf("%s: Save: nil WriteAll seam", s.name)
	}
	PT(&v).SetSchemaVersion(s.schemaVersion)
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("%s: marshal state: %w", s.name, err)
	}
	return d.WriteAll(data)
}

// Load reads the document through the seam and fails CLOSED to the zero value (no
// error, no panic) on an absent store, a corrupt blob, or a schema mismatch. A nil
// ReadAll seam or a real read error remain real errors.
//
// Failing closed is the point: an empty value means "nothing recorded", which must
// never be confused with a recorded result, and an unknown future schema is never
// reinterpreted as the current version.
func (s Store[T, PT]) Load(d Deps) (T, error) {
	var zero T
	if d.ReadAll == nil {
		return zero, fmt.Errorf("%s: Load: nil ReadAll seam", s.name)
	}
	data, err := d.ReadAll()
	if err != nil {
		return zero, fmt.Errorf("%s: read state: %w", s.name, err)
	}
	if len(data) == 0 {
		return zero, nil // absent store ⇒ zero value
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return zero, nil // corrupt ⇒ fail closed, never a panic
	}
	if PT(&v).GetSchemaVersion() != s.schemaVersion {
		// Unknown/future schema — fail closed rather than reinterpret it as the
		// current version, which could surface a mis-mapped fabricated result.
		return zero, nil
	}
	return v, nil
}

// WriteFileAtomic writes data to path via a same-dir temp file and a rename, so a
// crash mid-write never leaves a torn document. It is the live WriteAll seam the
// cmd tier wires.
//
// BOTH guards run BEFORE the directory is created, and that ordering is the point.
// pathsafe.WriteFileAtomic performs the same two checks itself, but the MkdirAll
// below happens first, so a guard deferred to the write would fire only after the
// traversal had already created directories as a side effect:
//
//   - AssertRoot rejects an empty or RELATIVE $XDG_DATA_HOME. A relative root
//     resolves against whatever the process CWD happens to be, and — because the
//     store path is derived from that same relative root — the containment check
//     passes: both sides resolve under the CWD. So Inside alone lets a relative
//     $XDG_DATA_HOME through, and the MkdirAll silently scatters a `villa/` tree
//     into the working directory before pathsafe finally refuses the write.
//   - Inside rejects a path escaping the (now known-absolute) root.
func (s Store[T, PT]) WriteFileAtomic(path string, data []byte) error {
	root := pathsafe.DataRoot()
	if err := pathsafe.AssertRoot(root); err != nil {
		return fmt.Errorf("%s: refusing to write under an unusable data root "+
			"(set XDG_DATA_HOME to an absolute path): %w", s.name, err)
	}
	if err := pathsafe.Inside(path, root); err != nil {
		return fmt.Errorf("%s: refusing to write outside the store root: %w", s.name, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), DirMode); err != nil {
		return fmt.Errorf("%s: mkdir store dir: %w", s.name, err)
	}
	if err := pathsafe.WriteFileAtomic(root, path, data, FileMode); err != nil {
		return fmt.Errorf("%s: %w", s.name, err)
	}
	return nil
}
