package recall

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestScopeOf: the scope is the knowledge id plus a map from each indexed transcript's
// file id to its owner. A chat with no file id or no owner is in nobody's scope, because
// a scope that guessed would widen what a caller reads.
func TestScopeOf(t *testing.T) {
	st := State{
		KnowledgeID: "kb-1",
		Chats: map[string]ChatState{
			"chat-a": {UserID: "u1", FileID: "f-a"},
			"chat-b": {UserID: "u2", FileID: "f-b"},
			"chat-c": {UserID: "u1", FileID: ""},
			"chat-d": {UserID: "", FileID: "f-d"},
		},
	}
	got := ScopeOf(st)
	want := Scope{
		SchemaVersion: ScopeSchemaVersion,
		KnowledgeID:   "kb-1",
		Files:         map[string]string{"f-a": "u1", "f-b": "u2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ScopeOf = %+v, want %+v", got, want)
	}
	if got.FilesOf("u1")[0] != "f-a" || len(got.FilesOf("u1")) != 1 {
		t.Errorf("FilesOf(u1) = %v", got.FilesOf("u1"))
	}
	if len(got.FilesOf("nobody")) != 0 || len(got.FilesOf("")) != 0 {
		t.Error("an unknown or empty user owns no file")
	}
}

// TestScopeOfEmptyState: nothing indexed is a scope that names no collection, never a
// fabricated one.
func TestScopeOfEmptyState(t *testing.T) {
	got := ScopeOf(State{})
	if got.KnowledgeID != "" || len(got.Files) != 0 {
		t.Errorf("empty state produced %+v", got)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"schema_version":1,"knowledge_id":"","files":{}}` {
		t.Errorf("empty scope JSON = %s", b)
	}
}

// TestScopeOfIsAWireShape: the JSON field names are the contract the tool server reads.
func TestScopeOfIsAWireShape(t *testing.T) {
	b, err := json.Marshal(ScopeOf(State{KnowledgeID: "kb", Chats: map[string]ChatState{"c": {UserID: "u", FileID: "f"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"schema_version":1,"knowledge_id":"kb","files":{"f":"u"}}` {
		t.Errorf("scope JSON = %s", b)
	}
}

// TestChatIDOfInvertsTranscriptFilename: the tool server recovers a chat id from the
// uploaded file's name, and a name that is not a transcript name yields nothing.
func TestChatIDOfInvertsTranscriptFilename(t *testing.T) {
	for _, id := range []string{"abc-123", "x"} {
		got, ok := ChatIDOf(TranscriptFilename(id))
		if !ok || got != id {
			t.Errorf("ChatIDOf(TranscriptFilename(%q)) = %q, %v", id, got, ok)
		}
	}
	for _, bad := range []string{"", "notes.txt", "villa-recall-.txt", "villa-recall-abc", "other-villa-recall-abc.txt"} {
		if got, ok := ChatIDOf(bad); ok || got != "" {
			t.Errorf("ChatIDOf(%q) = %q, %v; want no chat id", bad, got, ok)
		}
	}
}
