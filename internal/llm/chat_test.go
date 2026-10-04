package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// chatServer answers one non-streamed chat completion with body, recording the
// request's decoded JSON body and headers for the caller to assert on.
func chatServer(t *testing.T, status int, body string, gotBody *map[string]json.RawMessage, gotHeader *http.Header) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		if gotBody != nil {
			if err := json.NewDecoder(r.Body).Decode(gotBody); err != nil {
				t.Errorf("decode request body: %v", err)
			}
		}
		if gotHeader != nil {
			*gotHeader = r.Header.Clone()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// readFileTool is one function definition in the OpenAI wire shape.
var readFileTool = Tool{Type: "function", Function: ToolFunction{
	Name:        "read_file",
	Description: "Read a file",
	Parameters:  map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
}}

// TestChatReturnsContentAndToolCalls guards the non-streamed chat call ADR-0018's
// tool-call capability cases ride on: the request carries the tool definitions,
// tool_choice, greedy sampling and stream=false on the wire, with the key attached,
// and the reply hands back both the content and every tool call the model made,
// arguments verbatim.
func TestChatReturnsContentAndToolCalls(t *testing.T) {
	var got map[string]json.RawMessage
	var hdr http.Header
	srv := chatServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":null,`+
		`"tool_calls":[{"type":"function","function":{"name":"read_file","arguments":"{\"path\":\"notes.txt\"}"}}]}}]}`,
		&got, &hdr)

	zero := 0.0
	reply, err := NewOpenAIClient(Options{BaseURL: srv.URL, APIKey: "k"}).Chat(t.Context(), ChatRequest{
		Model:              "m",
		Messages:           []Message{{Role: RoleUser, Content: "show notes.txt"}},
		Temperature:        &zero,
		ChatTemplateKwargs: map[string]any{"enable_thinking": false},
		MaxTokens:          64,
		Tools:              []Tool{readFileTool},
		ToolChoice:         "auto",
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply.Content != "" || len(reply.ToolCalls) != 1 {
		t.Fatalf("reply = %+v, want one tool call and no content", reply)
	}
	if c := reply.ToolCalls[0]; c.Name != "read_file" || c.Arguments != `{"path":"notes.txt"}` {
		t.Errorf("tool call = %+v", c)
	}

	for k, want := range map[string]string{
		"stream":               "false",
		"temperature":          "0",
		"max_tokens":           "64",
		"tool_choice":          `"auto"`,
		"chat_template_kwargs": `{"enable_thinking":false}`,
	} {
		if string(got[k]) != want {
			t.Errorf("%s on the wire = %s, want %s", k, got[k], want)
		}
	}
	var tools []Tool
	if err := json.Unmarshal(got["tools"], &tools); err != nil || len(tools) != 1 || tools[0].Function.Name != "read_file" {
		t.Errorf("tools on the wire = %s (%v)", got["tools"], err)
	}
	if hdr.Get("Authorization") != "Bearer k" || hdr.Get("Accept") != "application/json" {
		t.Errorf("headers: Authorization=%q Accept=%q", hdr.Get("Authorization"), hdr.Get("Accept"))
	}
}

// TestChatReturnsPlainContent: a reply with no tool calls hands back its content
// and an empty call list, and a request without tools puts neither tools nor
// tool_choice on the wire, so a text capability case is sent exactly as before.
func TestChatReturnsPlainContent(t *testing.T) {
	var got map[string]json.RawMessage
	srv := chatServer(t, http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"42"}}]}`, &got, nil)

	reply, err := NewOpenAIClient(Options{BaseURL: srv.URL}).Chat(t.Context(), ChatRequest{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "6*7?"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply.Content != "42" || len(reply.ToolCalls) != 0 {
		t.Errorf("reply = %+v, want content 42 and no tool calls", reply)
	}
	for _, k := range []string{"tools", "tool_choice"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s emitted for a request without tools", k)
		}
	}
}

// TestChatRefusesWhatItCannotRead: a non-200, an undecodable body and a body with
// no choices are each an error, never an empty reply a grader would read as the
// model's answer; so is a request missing its model or messages, or one that
// cannot be marshalled.
func TestChatRefusesWhatItCannotRead(t *testing.T) {
	msgs := []Message{{Role: RoleUser, Content: "hi"}}
	for _, c := range []struct {
		name    string
		status  int
		body    string
		req     ChatRequest
		wantErr string
	}{
		{"upstream error", http.StatusUnauthorized, "bad key", ChatRequest{Model: "m", Messages: msgs}, "bad key"},
		{"undecodable body", http.StatusOK, "not json", ChatRequest{Model: "m", Messages: msgs}, "decode response"},
		{"no choices", http.StatusOK, `{"choices":[]}`, ChatRequest{Model: "m", Messages: msgs}, "no choices"},
		{"no model", http.StatusOK, "", ChatRequest{Messages: msgs}, "no model"},
		{"no messages", http.StatusOK, "", ChatRequest{Model: "m"}, "messages must not be empty"},
		{"unmarshallable", http.StatusOK, "", ChatRequest{Model: "m", Messages: msgs,
			ChatTemplateKwargs: map[string]any{"bad": make(chan int)}}, "marshal request"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := chatServer(t, c.status, c.body, nil, nil)
			_, err := NewOpenAIClient(Options{BaseURL: srv.URL}).Chat(t.Context(), c.req)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, c.wantErr)
			}
		})
	}
}

// TestChatReportsAnUnreachableServer: a transport failure is an error naming the
// base URL, so a capability case against a stopped inference unit is unconducted,
// never graded.
func TestChatReportsAnUnreachableServer(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := NewOpenAIClient(Options{BaseURL: url}).Chat(t.Context(), ChatRequest{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "request to "+url) {
		t.Fatalf("err = %v, want a transport error naming %s", err, url)
	}
}
