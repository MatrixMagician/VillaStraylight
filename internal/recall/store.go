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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// recallSchemaVersion is the recall store's OWN self-version, independent of
// status.Report's reportSchemaVersion and NOT golden-frozen. Bump only on an
// incompatible recall-state.json change.
const recallSchemaVersion = 1

// SchemaVersion exposes the recall store's OWN schema version to the Phase-23
// backup manifest (the reader-of-record outside this package). The const stays
// unexported; this accessor is the only way the manifest's field can track the
// store's actual schema without silently desyncing.
func SchemaVersion() int { return recallSchemaVersion }

// storeFileMode / storeDirMode are the owner-only modes the atomic writer enforces
// on recall-state.json and its dir (T-21-01 info-disclosure mitigation), mirroring
// usage/config/benchstore.
const (
	storeFileMode os.FileMode = 0o600
	storeDirMode  os.FileMode = 0o700
)

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

// Deps is the injectable byte-I/O seam (cloned from usage.Deps): the pure core
// marshals/parses; these funcs do the actual host I/O so the package is fully
// testable off-hardware with a buffer-backed Deps. The LIVE WriteAll (wired in
// Plan 02's cmd tier) calls WriteFileAtomic(RecallStatePath(), …).
type Deps struct {
	// WriteAll writes the whole marshaled store, replacing any prior content.
	WriteAll func(data []byte) error
	// ReadAll returns the whole store's bytes, or (nil, nil) when no store exists yet.
	ReadAll func() ([]byte, error)
	// Now supplies a clock for callers that want a deterministic timestamp seam.
	Now func() time.Time
}

// Save stamps the store's own SchemaVersion, marshals the whole document, and
// writes it via the seam (full-file replace, not append). D-05.
func Save(d Deps, s State) error {
	if d.WriteAll == nil {
		return fmt.Errorf("recall: Save: nil WriteAll seam")
	}
	s.SchemaVersion = recallSchemaVersion
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("recall: marshal state: %w", err)
	}
	return d.WriteAll(data)
}

// Load reads the store via the seam and fails CLOSED to an empty State (no error,
// no panic) on an absent store (ReadAll ⇒ nil,nil), a corrupt/unparseable blob, or
// a schema_version mismatch — an empty state means "nothing indexed", NEVER a
// fabricated index (D-05, T-21-03; verbatim clone of usage.Load's fail-closed
// semantics). An unknown future schema is NEVER reinterpreted as the current
// version. A nil ReadAll seam or a real read error remain REAL errors.
func Load(d Deps) (State, error) {
	if d.ReadAll == nil {
		return State{}, fmt.Errorf("recall: Load: nil ReadAll seam")
	}
	data, err := d.ReadAll()
	if err != nil {
		return State{}, fmt.Errorf("recall: read state: %w", err)
	}
	if len(data) == 0 {
		return State{}, nil // absent store ⇒ empty state ("nothing indexed")
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, nil // corrupt ⇒ fail closed to empty, never a panic
	}
	if s.SchemaVersion != recallSchemaVersion {
		// Unknown/future schema — fail closed (never reinterpret as the current
		// version, which could surface a mis-mapped fabricated index).
		return State{}, nil
	}
	return s, nil
}

// storeRootDir resolves the fixed villa DATA-store root that every durable
// data-dir artifact (usage.json, bench-reports.jsonl, recall-state.json) lives
// directly under: $XDG_DATA_HOME/villa, falling back to ~/.local/share/villa then
// /var/tmp/villa. WriteFileAtomic guards every write path against THIS fixed root
// rather than against the path's own parent — a `..`-bearing path is only
// meaningfully rejected when measured against a root the caller does NOT control
// (WR-05 precedent).
func storeRootDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "villa")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "villa")
	}
	return filepath.Join("/var/tmp", "villa")
}

// RecallStatePath resolves the single mutable recall store:
// $XDG_DATA_HOME/villa/recall-state.json (with the usage-store fallbacks). It
// lives here so the resolver ships with the contract it serves.
func RecallStatePath() string {
	return filepath.Join(storeRootDir(), "recall-state.json")
}

// assertInsideDir verifies path resolves within dir, rejecting traversal escapes
// (T-21-02). This is a LOCAL copy of the usage/config/benchstore guard shape —
// the clone-don't-import rule: importing another store package solely for an
// unexported guard would widen this pure core's deps.
func assertInsideDir(path, dir string) error {
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
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("recall: refusing to write %q outside store dir %q", absPath, absDir)
	}
	return nil
}

// WriteFileAtomic writes data to path via a same-dir temp file + os.Rename, so a
// crash mid-write never leaves a torn recall-state.json. It guards path against
// traversal OUT of the fixed villa data-store root (WR-05), creates the dir 0700,
// writes the temp 0600, renames atomically, then tightens a pre-existing looser
// file to 0600 (T-21-01/T-21-02). The temp is cleaned up on every error branch
// before the rename. The cmd tier (Plan 02) wires this as the live WriteAll seam;
// it writes the path resolved from storeRootDir, so a legitimate write is never
// rejected.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	// Guard against the FIXED store root, not the path's own parent — only then
	// does a `..`-bearing path get measured against a root the caller does not
	// control (WR-05). recall-state.json lives directly under this root.
	if err := assertInsideDir(path, storeRootDir()); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, storeDirMode); err != nil {
		return fmt.Errorf("recall: mkdir store dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "recall-state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("recall: create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if err := tmp.Chmod(storeFileMode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("recall: chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("recall: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("recall: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("recall: rename temp into place: %w", err)
	}
	// Tighten a pre-existing looser file (rename preserves the temp's mode, but be
	// explicit to mirror the usage/config chmod-tighten discipline).
	if err := os.Chmod(path, storeFileMode); err != nil {
		return fmt.Errorf("recall: chmod store file: %w", err)
	}
	return nil
}
