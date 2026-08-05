// Package verifystate is the persisted store of the LAST real `villa verify search`
// outcome (SURF-04): verdict + timestamp ONLY, written after the heavy netns/nft
// bounded-outbound proof runs so status/doctor/dashboard can surface a host-readable,
// honest record of the result WITHOUT re-running the proof on every poll.
//
// PURE BY CONSTRUCTION: no os/exec, no network, no container-image or backend
// literal — all host I/O arrives via the injectable byte-I/O Deps seam; the only
// filesystem-touching functions (WriteFileAtomic and the path resolvers) exist so the
// cmd tier can wire the LIVE WriteAll/ReadAll seam, mirroring the recall store exactly.
//
// store.go persists verify-search-state.json — a verdict ("PASS"/"FAIL"/"REJECT", the
// verdictName vocabulary) and an RFC3339 UTC timestamp ONLY, never any query/URL or
// fetched web content (T-34-02; the file lives host-side and must not become a content
// leak). The store discipline is CLONED (not imported — the established
// clone-don't-import rule) from the recall store: an OWN schema_version with a
// fail-closed Load (absent/corrupt/future-schema ⇒ empty State = "no verified result",
// NEVER a fabricated PASS — T-34-01), a version-stamping Save, and the atomic 0600/0700
// temp+rename writer guarded against traversal OUT of the FIXED $XDG_DATA_HOME/villa
// root (T-34-04). The cached PASS is turned into the outbound-bounded indicator (with a
// freshness check) downstream in Plans 03/04/05 — it MUST NEVER be derived from a
// config bool.
package verifystate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
)

// verifyStateSchemaVersion is the verify-search store's OWN self-version, independent
// of status.Report's reportSchemaVersion and NOT golden-frozen. Bump only on an
// incompatible verify-search-state.json change.
const verifyStateSchemaVersion = 1

// SchemaVersion exposes the verify-search store's OWN schema version to downstream
// readers (Plans 03/04 status/doctor live deps). The const stays unexported; this
// accessor is the only way a consumer can track the store's actual schema without
// silently desyncing.
func SchemaVersion() int { return verifyStateSchemaVersion }

// storeFileMode / storeDirMode are the owner-only modes the atomic writer enforces on
// verify-search-state.json and its dir (T-34-02 info-disclosure mitigation), mirroring
// recall/usage/config.
const (
	storeFileMode os.FileMode = 0o600
	storeDirMode  os.FileMode = 0o700
)

// State is the whole verify-search-state.json document (schema v1, SURF-04): the LAST
// real proof verdict and WHEN it was checked. Verdict is the verdictName vocabulary
// ("PASS"/"FAIL"/"REJECT", cmd/villa/verify_search_json.go:31-38); CheckedAt is an
// RFC3339 UTC timestamp. Verdict+timestamp ONLY — never any query/URL or fetched
// content (T-34-02).
type State struct {
	SchemaVersion int    `json:"schema_version"`
	Verdict       string `json:"verdict"`
	CheckedAt     string `json:"checked_at"`
}

// Deps is the injectable byte-I/O seam (cloned from recall.Deps): the pure core
// marshals/parses; these funcs do the actual host I/O so the package is fully testable
// off-hardware with a buffer-backed Deps. The LIVE WriteAll (wired in Plan 01's cmd
// tier) calls WriteFileAtomic(VerifyStatePath(), …).
type Deps struct {
	// WriteAll writes the whole marshaled store, replacing any prior content.
	WriteAll func(data []byte) error
	// ReadAll returns the whole store's bytes, or (nil, nil) when no store exists yet.
	ReadAll func() ([]byte, error)
	// Now supplies a clock for callers that want a deterministic timestamp seam.
	Now func() time.Time
}

// Save stamps the store's own SchemaVersion, marshals the whole document, and writes
// it via the seam (full-file replace, not append). The schema_version is ALWAYS
// stamped here — a caller-supplied value is never trusted (T-34-01 tampering guard).
func Save(d Deps, s State) error {
	if d.WriteAll == nil {
		return fmt.Errorf("verifystate: Save: nil WriteAll seam")
	}
	s.SchemaVersion = verifyStateSchemaVersion
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("verifystate: marshal state: %w", err)
	}
	return d.WriteAll(data)
}

// Load reads the store via the seam and fails CLOSED to an empty State (no error, no
// panic) on an absent store (ReadAll ⇒ nil,nil), a corrupt/unparseable blob, or a
// schema_version mismatch — an empty state means "no verified result", NEVER a
// fabricated PASS (T-34-01; verbatim clone of recall.Load's fail-closed semantics). An
// unknown future schema is NEVER reinterpreted as the current version. A nil ReadAll
// seam or a real read error remain REAL errors.
func Load(d Deps) (State, error) {
	if d.ReadAll == nil {
		return State{}, fmt.Errorf("verifystate: Load: nil ReadAll seam")
	}
	data, err := d.ReadAll()
	if err != nil {
		return State{}, fmt.Errorf("verifystate: read state: %w", err)
	}
	if len(data) == 0 {
		return State{}, nil // absent store ⇒ empty state ("no verified result")
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, nil // corrupt ⇒ fail closed to empty, never a panic
	}
	if s.SchemaVersion != verifyStateSchemaVersion {
		// Unknown/future schema — fail closed (never reinterpret as the current
		// version, which could surface a mis-mapped fabricated PASS).
		return State{}, nil
	}
	return s, nil
}

// VerifyStatePath resolves the single mutable verify-search store:
// $XDG_DATA_HOME/villa/verify-search-state.json (with the recall-store fallbacks). It
// lives here so the resolver ships with the contract it serves.
func VerifyStatePath() string {
	return filepath.Join(pathsafe.DataRoot(), "verify-search-state.json")
}

// WriteFileAtomic writes data to path via a same-dir temp file + os.Rename, so a crash
// mid-write never leaves a torn verify-search-state.json (T-34-01). It guards path
// against traversal OUT of the fixed villa data-store root (WR-05), creates the dir
// 0700, writes the temp 0600, renames atomically, then tightens a pre-existing looser
// file to 0600 (T-34-02/T-34-04). The temp is cleaned up on every error branch before
// the rename. The cmd tier (Plan 01 Task 2) wires this as the live WriteAll seam; it
// writes the path resolved from storeRootDir, so a legitimate write is never rejected.
func WriteFileAtomic(path string, data []byte) error {
	root := pathsafe.DataRoot()
	// Guard BEFORE MkdirAll, not just inside the write. pathsafe.WriteFileAtomic
	// checks containment itself, but the directory creation below happens first, so
	// without this a `..`-bearing path would get a directory created for it outside
	// the root before the write refused it.
	if err := pathsafe.Inside(path, root); err != nil {
		return fmt.Errorf("verifystate: refusing to write outside the store root: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), storeDirMode); err != nil {
		return fmt.Errorf("verifystate: mkdir store dir: %w", err)
	}
	if err := pathsafe.WriteFileAtomic(root, path, data, storeFileMode); err != nil {
		return fmt.Errorf("verifystate: %w", err)
	}
	return nil
}
