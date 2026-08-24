// Package verifystate is the persisted record of the LAST real
// `villa verify search` outcome: verdict and timestamp only, written
// after the heavy netns/nft bounded-outbound proof runs so status, doctor and the
// dashboard can surface an honest record of the result WITHOUT re-running the
// proof on every poll.
//
// Verdict and timestamp ONLY — never a query, a URL, or fetched web content
// The file lives host-side and must not become a content leak.
//
// Persistence is internal/jsonstore, shared with the recall store. What matters
// here and is NOT shared: this store's own schema version, and the fail-closed
// Load that turns an absent, corrupt or future-schema file into an EMPTY state.
// Empty means "no verified result" and must never be readable as a fabricated
// PASS. The cached PASS becomes the outbound-bounded indicator (with a
// freshness check) downstream; it must NEVER be derived from a config bool.
package verifystate

import (
	"github.com/MatrixMagician/VillaStraylight/internal/jsonstore"
)

// verifyStateSchemaVersion is this store's OWN self-version, independent of
// status.Report's reportSchemaVersion and of every other store's, and NOT
// golden-frozen. Bump only on an incompatible verify-search-state.json change.
const verifyStateSchemaVersion = 1

// State is the whole verify-search-state.json document (schema v1): the
// LAST real proof verdict and WHEN it was checked. Verdict is the verdictName
// vocabulary ("PASS"/"FAIL"/"REJECT"); CheckedAt is an RFC3339 UTC timestamp.
type State struct {
	SchemaVersion int    `json:"schema_version"`
	Verdict       string `json:"verdict"`
	CheckedAt     string `json:"checked_at"`
}

// GetSchemaVersion and SetSchemaVersion let the shared store stamp and check this
// document's version without knowing anything else about it.
func (s *State) GetSchemaVersion() int  { return s.SchemaVersion }
func (s *State) SetSchemaVersion(v int) { s.SchemaVersion = v }

// store binds the shared persistence layer to this document, filename and version.
var store = jsonstore.New[State, *State]("verifystate", "verify-search-state.json", verifyStateSchemaVersion)

// Deps is the injectable byte-I/O seam, so the package stays testable off-hardware
// with a buffer-backed Deps.
type Deps = jsonstore.Deps

// SchemaVersion exposes this store's OWN schema version to downstream readers, so
// a consumer can track it without silently desyncing.
func SchemaVersion() int { return verifyStateSchemaVersion }

// Save stamps the schema version and writes the whole document via the seam.
func Save(d Deps, s State) error { return store.Save(d, s) }

// Load reads the document via the seam, failing CLOSED to an empty State on an
// absent, corrupt or version-mismatched store — never a fabricated PASS.
func Load(d Deps) (State, error) { return store.Load(d) }

// Path resolves the single mutable verify-search store under the villa
// data root.
func Path() string { return store.Path() }

// WriteFileAtomic is the live WriteAll seam the cmd tier wires: a traversal-guarded
// temp+rename write at 0600 under a 0700 directory.
func WriteFileAtomic(path string, data []byte) error { return store.WriteFileAtomic(path, data) }
