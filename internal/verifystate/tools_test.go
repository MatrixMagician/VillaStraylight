package verifystate

import (
	"path/filepath"
	"testing"
)

// TestToolsDocumentIsSeparateFromSearch: `villa verify tools` records its verdict in a second
// document, so it can never overwrite the web-search egress verdict `search-egress` reads, nor
// be read back as it.
func TestToolsDocumentIsSeparateFromSearch(t *testing.T) {
	if filepath.Base(ToolsPath()) != "verify-tools-state.json" {
		t.Errorf("tools path = %s", ToolsPath())
	}
	if ToolsPath() == Path() {
		t.Fatal("the tools proof and the search proof share a file")
	}

	var written []byte
	d := Deps{
		WriteAll: func(b []byte) error { written = b; return nil },
		ReadAll:  func() ([]byte, error) { return written, nil },
	}
	if err := SaveTools(d, State{Verdict: "PASS", CheckedAt: "2026-11-14T10:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTools(d)
	if err != nil || got.Verdict != "PASS" || got.CheckedAt != "2026-11-14T10:00:00Z" || got.SchemaVersion != ToolsSchemaVersion() {
		t.Errorf("round trip = %+v, %v", got, err)
	}
}

// TestToolsLoadFailsClosed: absent, corrupt and future-schema documents read as no result,
// never as a PASS.
func TestToolsLoadFailsClosed(t *testing.T) {
	for name, raw := range map[string][]byte{
		"absent":  nil,
		"corrupt": []byte("{nope"),
		"future":  []byte(`{"schema_version":999,"verdict":"PASS","checked_at":"2026-11-14T10:00:00Z"}`),
	} {
		got, err := LoadTools(Deps{ReadAll: func() ([]byte, error) { return raw, nil }})
		if err != nil || got.Verdict != "" || got.CheckedAt != "" {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}
}
