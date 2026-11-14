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

// TestToolCompletionBody pins the one request the chat leg sends: a non-streaming
// completion that selects one registered tool server by its id, as `server:<id>`, and asks
// for native function calling.
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
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "m.gguf" || got.Stream || len(got.ToolIDs) != 1 || got.ToolIDs[0] != "server:villa-web" ||
		len(got.Messages) != 1 || got.Messages[0]["role"] != "user" || got.Messages[0]["content"] != "search for x" ||
		got.Params["function_calling"] != "native" {
		t.Errorf("body = %s", raw)
	}
}

// TestToolServersPathIsOwnedHere: the admin route is a const in paths.go, so the command
// tier's literal gate has nothing to flag.
func TestToolServersPathIsOwnedHere(t *testing.T) {
	if pathToolServersConfig != "/api/v1/configs/tool_servers" {
		t.Errorf("path = %q", pathToolServersConfig)
	}
}
