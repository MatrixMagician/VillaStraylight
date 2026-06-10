// Package recall store tests guard the recall-state.json persistence discipline
// cloned from internal/usage: fail-closed Load (D-05 — absent/corrupt/future-schema
// ⇒ empty state = "nothing indexed", never a fabricated index), version-stamping
// Save, the atomic 0600/0700 temp+rename writer with traversal guard (T-21-02),
// and the ids/timestamps-only content discipline (T-21-01 — no chat titles or
// content may ever enter a host-side file).
package recall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestStoreLoadFailsClosed proves Load degrades to an empty State (no error, no
// panic) on an absent store, a corrupt blob, and a future/unknown schema_version —
// an empty state means "nothing indexed", never a fabricated index (D-05, T-21-03).
// A nil ReadAll seam and a ReadAll I/O error are REAL errors (wrapped), not
// silently-empty states.
func TestStoreLoadFailsClosed(t *testing.T) {
	// Nil seam ⇒ error (a programming error, never silent).
	if _, err := Load(Deps{}); err == nil {
		t.Error("Load(nil ReadAll) = nil error, want error")
	}

	// ReadAll I/O error ⇒ wrapped error.
	ioErr := Deps{ReadAll: func() ([]byte, error) { return nil, os.ErrPermission }}
	if _, err := Load(ioErr); err == nil {
		t.Error("Load(ReadAll error) = nil error, want wrapped error")
	}

	// Absent store: ReadAll ⇒ (nil,nil) ⇒ empty State, no error.
	absent := Deps{ReadAll: func() ([]byte, error) { return nil, nil }}
	if got, err := Load(absent); err != nil || len(got.Chats) != 0 || got.KnowledgeID != "" {
		t.Errorf("Load(absent) = (%+v, %v), want empty, nil", got, err)
	}

	// Empty blob (zero-length) ⇒ empty State, no error.
	empty := Deps{ReadAll: func() ([]byte, error) { return []byte{}, nil }}
	if got, err := Load(empty); err != nil || len(got.Chats) != 0 {
		t.Errorf("Load(empty) = (%+v, %v), want empty, nil", got, err)
	}

	// Corrupt blob: not JSON ⇒ empty, no error, no panic.
	corrupt := Deps{ReadAll: func() ([]byte, error) { return []byte("{not json"), nil }}
	if got, err := Load(corrupt); err != nil || len(got.Chats) != 0 {
		t.Errorf("Load(corrupt) = (%+v, %v), want empty, nil", got, err)
	}

	// Future/unknown schema: valid JSON, wrong schema_version ⇒ empty, no error —
	// a future schema is NEVER reinterpreted as the current version.
	skew := Deps{ReadAll: func() ([]byte, error) {
		return []byte(`{"schema_version":999,"knowledge_id":"kb1","chats":{"c1":{"user_id":"u1"}}}`), nil
	}}
	if got, err := Load(skew); err != nil || len(got.Chats) != 0 || got.KnowledgeID != "" {
		t.Errorf("Load(skew) = (%+v, %v), want empty, nil", got, err)
	}
}

// TestStoreSaveLoadRoundTrip proves Save stamps SchemaVersion=1 before marshal and
// that Load(Save(s)) returns a state identical to the input (D-05). A nil WriteAll
// seam is a real error.
func TestStoreSaveLoadRoundTrip(t *testing.T) {
	if err := Save(Deps{}, State{}); err == nil {
		t.Error("Save(nil WriteAll) = nil error, want error")
	}

	var buf []byte
	d := Deps{
		WriteAll: func(b []byte) error { buf = append([]byte(nil), b...); return nil },
		ReadAll:  func() ([]byte, error) { return buf, nil },
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}

	in := State{
		KnowledgeID:          "kb-uuid-1",
		KnowledgeName:        "Villa Recall — Past Conversations",
		EmbeddingModel:       "nomic-embed-text-v1.5",
		EmbeddingDim:         768,
		LastIndexStartedAt:   "2026-06-10T12:00:00Z",
		LastIndexCompletedAt: "2026-06-10T12:03:21Z",
		Chats: map[string]ChatState{
			"chat-1": {UserID: "user-1", OWUIUpdatedAt: 1781041047, FileID: "file-1", IndexedAt: "2026-06-10T12:01:07Z"},
			"chat-2": {UserID: "user-1", OWUIUpdatedAt: 1781041100, FileID: "file-2", IndexedAt: "2026-06-10T12:02:00Z"},
		},
	}
	if err := Save(d, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(d)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.SchemaVersion != recallSchemaVersion {
		t.Errorf("loaded schema_version = %d, want %d (Save must stamp it)", out.SchemaVersion, recallSchemaVersion)
	}
	in.SchemaVersion = recallSchemaVersion
	if !reflect.DeepEqual(out, in) {
		t.Errorf("round-trip state = %+v, want %+v", out, in)
	}
}

// TestStoreSchemaVersionMirrorsConst guards that the exported SchemaVersion()
// accessor (the Phase-23 backup-manifest reader-of-record) returns EXACTLY the
// unexported recallSchemaVersion const, so a manifest field can never silently
// desync from the store's actual on-disk schema version.
func TestStoreSchemaVersionMirrorsConst(t *testing.T) {
	if got := SchemaVersion(); got != recallSchemaVersion {
		t.Fatalf("SchemaVersion() = %d, want %d (must mirror the const)", got, recallSchemaVersion)
	}
	if recallSchemaVersion != 1 {
		t.Fatalf("recallSchemaVersion = %d, want 1 (schema v1)", recallSchemaVersion)
	}
}

// TestStoreStateHasNoContentKeys is the ids/timestamps-only security test
// (T-21-01, D-05): it marshals a fully-populated State and walks EVERY JSON key
// (recursively), asserting none contains a content-bearing token — no chat title,
// no message content, may ever leak into the host-side recall-state.json.
func TestStoreStateHasNoContentKeys(t *testing.T) {
	populated := State{
		SchemaVersion:        recallSchemaVersion,
		KnowledgeID:          "kb-uuid-1",
		KnowledgeName:        "Villa Recall — Past Conversations",
		EmbeddingModel:       "nomic-embed-text-v1.5",
		EmbeddingDim:         768,
		LastIndexStartedAt:   "2026-06-10T12:00:00Z",
		LastIndexCompletedAt: "2026-06-10T12:03:21Z",
		Chats: map[string]ChatState{
			"chat-1": {UserID: "user-1", OWUIUpdatedAt: 1781041047, FileID: "file-1", IndexedAt: "2026-06-10T12:01:07Z"},
		},
	}
	blob, err := json.Marshal(populated)
	if err != nil {
		t.Fatalf("marshal populated State: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatalf("re-parse marshaled State: %v", err)
	}

	var keys []string
	var walk func(v any)
	walk = func(v any) {
		switch vv := v.(type) {
		case map[string]any:
			for k, child := range vv {
				keys = append(keys, k)
				walk(child)
			}
		case []any:
			for _, child := range vv {
				walk(child)
			}
		}
	}
	walk(doc)

	// chat ids are map KEYS of State.Chats — exclude that one level of data-keys
	// from the structural check by walking the chats map's values only.
	denied := []string{"title", "content", "message"}
	for _, k := range keys {
		lk := strings.ToLower(k)
		for _, banned := range denied {
			if strings.Contains(lk, banned) {
				t.Errorf("marshaled state JSON key %q contains banned content token %q — ids/timestamps/counts only (T-21-01)", k, banned)
			}
		}
	}
}

// TestStoreRecallStatePathXDG proves the path resolver honors $XDG_DATA_HOME
// (the fixed villa data-store root) and that assertInsideDir rejects a traversal
// escape measured against that FIXED root (T-21-02, WR-05 precedent).
func TestStoreRecallStatePathXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	want := filepath.Join(tmp, "villa", "recall-state.json")
	if got := RecallStatePath(); got != want {
		t.Errorf("RecallStatePath() = %q, want %q", got, want)
	}

	dir := filepath.Join(tmp, "villa")
	if err := assertInsideDir(filepath.Join(dir, "recall-state.json"), dir); err != nil {
		t.Errorf("assertInsideDir rejected an in-dir path: %v", err)
	}
	if err := assertInsideDir(filepath.Join(dir, "..", "escape.json"), dir); err == nil {
		t.Error("assertInsideDir accepted a traversal escape, want rejection")
	}
}

// TestStoreWriteFileAtomic proves the atomic writer creates the store dir 0700,
// writes the file 0600 with the exact bytes, leaves NO temp file behind on success
// OR on a failed rename, and refuses a path outside the fixed store root
// (T-21-02).
func TestStoreWriteFileAtomic(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	path := RecallStatePath()
	data := []byte(`{"schema_version":1}`)

	if err := WriteFileAtomic(path, data); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("read back = %q, want %q", got, data)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat store file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("store file mode = %o, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat store dir: %v", err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("store dir mode = %o, want 0700", di.Mode().Perm())
	}

	// No temp residue after a successful write.
	assertNoTempFiles(t, filepath.Dir(path))

	// Traversal: a path outside the fixed store root is refused before any write.
	escape := filepath.Join(filepath.Dir(path), "..", "escape.json")
	if err := WriteFileAtomic(escape, data); err == nil {
		t.Error("WriteFileAtomic accepted a traversal escape, want rejection")
	}
	if _, err := os.Stat(filepath.Join(tmp, "escape.json")); !os.IsNotExist(err) {
		t.Error("traversal escape produced a file outside the store root")
	}

	// Failed rename (target is an existing DIRECTORY) ⇒ error AND temp cleaned up.
	blockedPath := filepath.Join(filepath.Dir(path), "blocked.json")
	if err := os.MkdirAll(blockedPath, 0o700); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}
	if err := WriteFileAtomic(blockedPath, data); err == nil {
		t.Error("WriteFileAtomic over an existing directory = nil error, want error")
	}
	assertNoTempFiles(t, filepath.Dir(path))
}

// assertNoTempFiles fails the test if any recall-state temp file is left in dir —
// the writer must clean its temp up on every error branch and after every rename.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read store dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file %q left behind in store dir", e.Name())
		}
	}
}
