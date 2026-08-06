// Package recall is the pure conversational-recall core for Phase 21 (D-08): the
// plan/diff algebra deciding what to add/update/delete in the Open WebUI recall
// knowledge base (D-05), the role-labeled per-chat transcript renderer (D-04), the
// typed-Unknown staleness classification (D-06), and the recall-state.json store.
//
// PURE BY CONSTRUCTION: no os/exec, no network, no container-image or backend
// literal — TestSeamGrepGate stays green over this package (D-08). All host I/O
// arrives via the injectable byte-I/O Deps seam; the only filesystem-touching
// functions (WriteFileAtomic and the path resolvers) exist so the cmd tier can
// wire the LIVE WriteAll/ReadAll seam, mirroring internal/usage exactly.
//
// store.go persists recall-state.json — ids/timestamps/counts ONLY, never chat
// titles or content (T-21-01; the state file lives host-side and must not become
// a content leak). The store discipline is CLONED (not imported — the established
// clone-don't-import rule, usage.go:243) from internal/usage: own schema_version
// with a fail-closed Load (absent/corrupt/future-schema ⇒ empty state = "nothing
// indexed", NEVER a fabricated index — D-05, T-21-03), version-stamping Save, and
// the atomic 0600/0700 temp+rename writer guarded against traversal OUT of the
// fixed $XDG_DATA_HOME/villa root (T-21-02, WR-05 precedent). RECALL-03.
package recall

import (
	"github.com/MatrixMagician/VillaStraylight/internal/jsonstore"
)

// recallSchemaVersion is the recall store's OWN self-version, independent of
// status.Report's reportSchemaVersion and of every other store's, and NOT
// golden-frozen. Bump only on an incompatible recall-state.json change.
const recallSchemaVersion = 1

// ChatState is the persisted per-chat index record: WHO the chat belongs to, the
// OWUI updated_at observed when it was last indexed (epoch SECONDS, as the list
// API returns), WHICH transcript file currently represents it in the knowledge
// base, and WHEN villa indexed it (RFC3339 UTC). Ids and timestamps only — no
// title, no content (T-21-01).
type ChatState struct {
	UserID        string `json:"user_id"`
	OWUIUpdatedAt int64  `json:"owui_updated_at"`
	FileID        string `json:"file_id"`
	IndexedAt     string `json:"indexed_at"`
}

// State is the whole recall-state.json document (schema v1, D-05): the recall
// knowledge-base identity, the embedding model/dim skew guards (Phase-23), the
// last index run stamps (LastIndexCompletedAt is ONLY stamped on a clean full
// pass — D-06 partial-run honesty), and the per-chat index records keyed by chat
// id. Ids/timestamps/counts only — never chat titles or content (T-21-01).
type State struct {
	SchemaVersion        int                  `json:"schema_version"`
	KnowledgeID          string               `json:"knowledge_id"`
	KnowledgeName        string               `json:"knowledge_name"`
	EmbeddingModel       string               `json:"embedding_model"`
	EmbeddingDim         int                  `json:"embedding_dim"`
	LastIndexStartedAt   string               `json:"last_index_started_at"`
	LastIndexCompletedAt string               `json:"last_index_completed_at"`
	Chats                map[string]ChatState `json:"chats,omitempty"`
}

// GetSchemaVersion and SetSchemaVersion let the shared store stamp and check this
// document's version without knowing anything else about it.
func (s *State) GetSchemaVersion() int  { return s.SchemaVersion }
func (s *State) SetSchemaVersion(v int) { s.SchemaVersion = v }

// store binds the shared persistence layer to this document, filename and version.
var store = jsonstore.New[State, *State]("recall", "recall-state.json", recallSchemaVersion)

// Deps is the injectable byte-I/O seam, so the package stays testable off-hardware
// with a buffer-backed Deps.
type Deps = jsonstore.Deps

// SchemaVersion exposes the recall store's OWN schema version to the backup
// manifest (the reader-of-record outside this package), so the manifest's field
// can never silently desync from the store's actual schema.
func SchemaVersion() int { return recallSchemaVersion }

// Save stamps the schema version and writes the whole document via the seam.
func Save(d Deps, s State) error { return store.Save(d, s) }

// Load reads the document via the seam, failing CLOSED to an empty State on an
// absent, corrupt or version-mismatched store — an empty state means "nothing
// indexed", never a fabricated index (D-05, T-21-03).
func Load(d Deps) (State, error) { return store.Load(d) }

// RecallStatePath resolves the single mutable recall store under the villa data root.
func RecallStatePath() string { return store.Path() }

// WriteFileAtomic is the live WriteAll seam the cmd tier wires: a traversal-guarded
// temp+rename write at 0600 under a 0700 directory, so a crash mid-write never
// leaves a torn recall-state.json.
func WriteFileAtomic(path string, data []byte) error { return store.WriteFileAtomic(path, data) }
