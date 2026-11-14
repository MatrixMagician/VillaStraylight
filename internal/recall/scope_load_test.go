package recall

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestLoadScopeFailsClosed: an absent, corrupt or other-version document is the zero Scope,
// which names no collection, and a written one reads back whole.
func TestLoadScopeFailsClosed(t *testing.T) {
	read := func(b []byte) func() ([]byte, error) { return func() ([]byte, error) { return b, nil } }
	for name, raw := range map[string][]byte{
		"absent":        nil,
		"corrupt":       []byte("{not json"),
		"other version": []byte(`{"schema_version":99,"knowledge_id":"kb","files":{"f":"u"}}`),
	} {
		got, err := LoadScope(Deps{ReadAll: read(raw)})
		if err != nil || got.KnowledgeID != "" || len(got.Files) != 0 {
			t.Errorf("%s: LoadScope = %+v, %v; want the zero scope", name, got, err)
		}
	}
	want := ScopeOf(State{KnowledgeID: "kb", Chats: map[string]ChatState{"c": {UserID: "u", FileID: "f"}}})
	b, _ := json.Marshal(want)
	if got, err := LoadScope(Deps{ReadAll: read(b)}); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %+v, %v; want %+v", got, err, want)
	}
}
