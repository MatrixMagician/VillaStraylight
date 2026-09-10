package crushapi

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// TestEveryLineRoundTrips guards the stdio contract the bridge and the runner
// share: one JSON object per line, and every Command and Event kind survives the
// crossing unchanged. A field that stops round-tripping is a command the runner
// silently cannot issue.
func TestEveryLineRoundTrips(t *testing.T) {
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	lines := []Line{
		{Command: &Command{Kind: CmdPrompt, Prompt: "find duplicate files"}},
		{Command: &Command{Kind: CmdGrant, PermissionID: "perm-1", Answer: Allow}},
		{Command: &Command{Kind: CmdGrant, PermissionID: "perm-2", Answer: AllowSession}},
		{Command: &Command{Kind: CmdGrant, PermissionID: "perm-3", Answer: Deny}},
		{Command: &Command{Kind: CmdCancel}},
		{Event: &Event{Kind: KindBridgeReady, Time: at, Version: "v0.76.0"}},
		{Event: &Event{Kind: KindBridgeError, Time: at, Error: "crush version drift"}},
		{Event: &Event{Kind: KindPermissionRequest, ID: "perm-1", Time: at, Permission: &PermissionRequest{
			ID: "perm-1", SessionID: "sess-1", ToolCallID: "call-1", Tool: "edit",
			Action: "write", Path: "/workspace/a.md", Params: json.RawMessage(`{"file_path":"/workspace/a.md"}`),
		}}},
		{Event: &Event{Kind: KindPermissionNotification, ID: "call-1", Time: at, Raw: json.RawMessage(`{"granted":true}`)}},
		{Event: &Event{Kind: KindMessage, ID: "msg-1", Time: at, Message: &Message{
			ID: "msg-1", SessionID: "sess-1", Role: "assistant", Text: "done",
			ToolCalls: []ToolCall{{ID: "call-1", Name: "edit", Input: "{}"}},
		}}},
		{Event: &Event{Kind: KindFile, ID: "file-1", Time: at, File: &FileEvent{
			ID: "file-1", SessionID: "sess-1", Path: "/workspace/a.md", Version: 3,
		}}},
		{Event: &Event{Kind: KindAgentEvent, ID: "sess-1", Time: at, Raw: json.RawMessage(`{"type":"response"}`)}},
		{Event: &Event{Kind: KindRunComplete, ID: "sess-1", Time: at, RunComplete: &RunComplete{
			SessionID: "sess-1", RunID: "run-1", MessageID: "msg-1", Text: "ok",
		}}},
		{Event: &Event{Kind: KindFilesRead, Time: at, Files: []string{"/workspace/q3.csv", "/workspace/notes.txt"}}},
		{Event: &Event{Kind: "a_kind_villa_has_never_seen", Time: at, Raw: json.RawMessage(`{"x":1}`)}},
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, l := range lines {
		if err := enc.Encode(l); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	if n := bytes.Count(buf.Bytes(), []byte("\n")); n != len(lines) {
		t.Fatalf("got %d newlines for %d lines: one JSON object per line is the contract", n, len(lines))
	}

	dec := json.NewDecoder(&buf)
	for i, want := range lines {
		var got Line
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("decode line %d: %v", i, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("line %d round-trip:\n got %+v\nwant %+v", i, got, want)
		}
	}
}

// TestALineIsEitherAnEventOrACommand guards the discriminator: a decoded line
// tells the reader which half it holds without a kind table.
func TestALineIsEitherAnEventOrACommand(t *testing.T) {
	var l Line
	if err := json.Unmarshal([]byte(`{"command":{"kind":"cancel"}}`), &l); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if l.Event != nil || l.Command == nil || l.Command.Kind != CmdCancel {
		t.Fatalf("line = %+v", l)
	}

	b, err := json.Marshal(Line{Event: &Event{Kind: KindBridgeReady}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte(`"command"`)) {
		t.Errorf("an event line carried an empty command key: %s", b)
	}
}
