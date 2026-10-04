// Package evalstore persists eval baselines (ADR-0018): one schema-versioned JSON
// document, eval-baselines.json under villa's XDG data root, holding at most one
// eval baseline per key (model, quant, suite version).
//
// Persistence is internal/jsonstore, like verifystate and recall. Load keeps its
// fail-closed reading: an absent, corrupt or future-schema store is empty, which the
// comparison reports as "no eval baseline" (Reject), never as a pass.
//
// Put is stricter, because a baseline records a known-good state that cannot be
// re-recorded after the regression it exists to catch. A store this villa cannot
// read is refused, never overwritten: replacing it with one new baseline would
// silently drop every other model's.
package evalstore

import (
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/eval"
	"github.com/MatrixMagician/VillaStraylight/internal/jsonstore"
)

// schemaVersion is this store's own version, independent of every other store's and
// of the `villa eval --json` contract. Bump only on an incompatible document change.
const schemaVersion = 1

// filename is the document's base name under the data root.
const filename = "eval-baselines.json"

// Document is the whole eval-baselines.json document.
type Document struct {
	Baselines     []eval.Baseline `json:"baselines"`
	SchemaVersion int             `json:"schema_version"`
}

// GetSchemaVersion and SetSchemaVersion let the shared store stamp and check this
// document's version without knowing anything else about it.
func (d *Document) GetSchemaVersion() int  { return d.SchemaVersion }
func (d *Document) SetSchemaVersion(v int) { d.SchemaVersion = v }

// store binds the shared persistence layer to this document, filename and version.
var store = jsonstore.New[Document, *Document]("evalstore", filename, schemaVersion)

// Deps is the injectable byte-I/O seam.
type Deps = jsonstore.Deps

// SchemaVersion exposes this store's own schema version, for the backup manifest's
// readers and for tests.
func SchemaVersion() int { return schemaVersion }

// Load reads the document, failing closed to an empty one on an absent, corrupt or
// version-mismatched store. A real read error stays an error.
func Load(d Deps) (Document, error) { return store.Load(d) }

// Find returns the eval baseline recorded for k, or nil.
func (d Document) Find(k eval.Key) *eval.Baseline {
	for i := range d.Baselines {
		if d.Baselines[i].Key == k {
			return &d.Baselines[i]
		}
	}
	return nil
}

// Put records b as the eval baseline for its key and returns the one it replaced, or
// nil. It refuses, writing nothing, when the existing store cannot be read.
func Put(d Deps, b eval.Baseline) (*eval.Baseline, error) {
	doc, err := loadForWrite(d)
	if err != nil {
		return nil, err
	}
	replaced := doc.put(b)
	if err := store.Save(d, doc); err != nil {
		return nil, err
	}
	return replaced, nil
}

// put replaces the baseline under b's key, or appends b.
func (d *Document) put(b eval.Baseline) *eval.Baseline {
	if prior := d.Find(b.Key); prior != nil {
		replaced := *prior
		*prior = b
		return &replaced
	}
	d.Baselines = append(d.Baselines, b)
	return nil
}

// loadForWrite loads the document for a read-modify-write. Load cannot tell an
// absent store from one it could not read (both are empty), so an empty result is
// re-checked against the raw bytes: anything there, or a read that now fails, is a
// store this villa must not overwrite.
func loadForWrite(d Deps) (Document, error) {
	doc, err := store.Load(d)
	if err != nil || doc.SchemaVersion == schemaVersion {
		return doc, err
	}
	if unreadable(d) {
		return Document{}, fmt.Errorf("evalstore: %s holds a document this villa cannot read (corrupt, or a newer "+
			"schema); refusing to overwrite its eval baselines — move it aside, then record again", filename)
	}
	return doc, nil
}

// unreadable reports whether the store holds bytes, or can no longer be read.
func unreadable(d Deps) bool {
	raw, err := d.ReadAll()
	return err != nil || len(raw) > 0
}

// Path resolves eval-baselines.json under the villa data root.
func Path() string { return store.Path() }

// WriteFileAtomic is the live WriteAll seam the command tier wires: a
// traversal-guarded temp+rename write at 0600 under a 0700 directory.
func WriteFileAtomic(path string, data []byte) error { return store.WriteFileAtomic(path, data) }
