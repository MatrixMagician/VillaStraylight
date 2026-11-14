package openwebui

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestToolServersReadsRegistrationAndNeverTheKey: the admin tool-server list carries each
// connection's bearer, and the proof needs only the id and the enabled flag. The result type
// has no field a key could land in, and a body that does not parse reports its shape.
func TestToolServersReadsRegistrationAndNeverTheKey(t *testing.T) {
	const key = "SQUIRREL-tool-bearer"
	body := `{"TOOL_SERVER_CONNECTIONS":[` +
		`{"type":"openapi","url":"http://villa-tools-web:8092","key":"` + key + `","config":{"enable":true},"info":{"id":"villa-web","name":"w"}},` +
		`{"type":"openapi","url":"http://villa-tools-recall:8093","key":"` + key + `","config":{"enable":false},"info":{"id":"villa-recall"}},` +
		`{"type":"openapi","url":"http://x","info":{}}]}`
	got, err := New(fakeJSON(map[string]string{pathToolServersConfig: body})).ToolServers(t.Context(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	want := []ToolServer{{ID: "villa-web", Enabled: true}, {ID: "villa-recall", Enabled: false}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %+v, want %+v (an entry with no id is not a registration)", got, want)
	}
	if b, _ := json.Marshal(got); strings.Contains(string(b), key) {
		t.Error("the result carries a bearer")
	}

	for name, bad := range map[string]string{
		"empty":         ``,
		"not json":      `<html>` + key,
		"wrong shape":   `{"detail":"` + key + `"}`,
		"null list":     `{"TOOL_SERVER_CONNECTIONS":null}`,
		"list of lists": `{"TOOL_SERVER_CONNECTIONS":["` + key + `"]}`,
	} {
		_, err := New(fakeJSON(map[string]string{pathToolServersConfig: bad})).ToolServers(t.Context(), "tok")
		if err == nil {
			t.Errorf("%s: an unreadable list must be an error, never an empty registration", name)
			continue
		}
		if strings.Contains(err.Error(), key) {
			t.Errorf("%s: the diagnostic reproduced the body: %v", name, err)
		}
	}
	if got, err := New(fakeJSON(map[string]string{pathToolServersConfig: `{"TOOL_SERVER_CONNECTIONS":[]}`})).ToolServers(t.Context(), "tok"); err != nil || len(got) != 0 {
		t.Errorf("an empty list is a real answer: %v %v", got, err)
	}
}

// TestToolCompletionBody pins the one request the chat leg sends. Open WebUI runs a tool
// server's loop only for a streaming request that has a chat's message ids (issue #363), so
// the body streams, starts a new chat with parent_id null, names the assistant message in
// `id`, carries the user's turn in `user_message`, and selects one registered tool server as
// `server:<id>` with native function calling. It never carries session_id: with one Open WebUI
// offers the model its built-in tools and runs the loop in the background.
func TestToolCompletionBody(t *testing.T) {
	raw, err := ToolCompletionBody("m.gguf", "villa-web", "search for x")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Model    string              `json:"model"`
		Stream   bool                `json:"stream"`
		ToolIDs  []string            `json:"tool_ids"`
		Messages []map[string]string `json:"messages"`
		Params   map[string]any      `json:"params"`
		ID       string              `json:"id"`
		UserMsg  struct {
			ID       string   `json:"id"`
			ParentID *string  `json:"parentId"`
			Children []string `json:"childrenIds"`
			Role     string   `json:"role"`
			Content  string   `json:"content"`
			Models   []string `json:"models"`
		} `json:"user_message"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "m.gguf" || !got.Stream || len(got.ToolIDs) != 1 || got.ToolIDs[0] != "server:villa-web" ||
		len(got.Messages) != 1 || got.Messages[0]["role"] != "user" || got.Messages[0]["content"] != "search for x" ||
		got.Params["function_calling"] != "native" {
		t.Errorf("body = %s", raw)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	if string(generic["parent_id"]) != "null" {
		t.Errorf("parent_id = %s, want an explicit null", generic["parent_id"])
	}
	if _, has := generic["session_id"]; has {
		t.Error("session_id must not be sent")
	}
	if got.ID == "" || got.UserMsg.ID == "" || got.ID == got.UserMsg.ID {
		t.Errorf("ids = %q and %q, want two distinct ids", got.ID, got.UserMsg.ID)
	}
	if got.UserMsg.ParentID != nil || got.UserMsg.Role != "user" || got.UserMsg.Content != "search for x" ||
		len(got.UserMsg.Children) != 0 || len(got.UserMsg.Models) != 1 || got.UserMsg.Models[0] != "m.gguf" {
		t.Errorf("user_message = %+v", got.UserMsg)
	}
	again, err := ToolCompletionBody("m.gguf", "villa-web", "search for x")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == string(again) {
		t.Error("two requests must not share message ids")
	}
}

// TestToolServersPathIsOwnedHere: the admin route is a const in paths.go, so the command
// tier's literal gate has nothing to flag.
func TestToolServersPathIsOwnedHere(t *testing.T) {
	if pathToolServersConfig != "/api/v1/configs/tool_servers" {
		t.Errorf("path = %q", pathToolServersConfig)
	}
}
