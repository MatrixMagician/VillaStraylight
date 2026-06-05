package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamChatAssemblesDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Two content chunks, a keep-alive, then DONE.
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n"))
		_, _ = w.Write([]byte(": keep-alive\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\", world\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := NewOpenAIClient(Options{BaseURL: srv.URL, APIKey: "secret", DefaultModel: "test-model"})

	var sb strings.Builder
	err := client.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(delta string) error {
		sb.WriteString(delta)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat returned error: %v", err)
	}
	if got := sb.String(); got != "Hello, world" {
		t.Errorf("assembled = %q, want %q", got, "Hello, world")
	}
}

func TestStreamChatUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewOpenAIClient(Options{BaseURL: srv.URL, DefaultModel: "test-model"})
	err := client.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error from non-200 upstream, got nil")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error %q should include upstream body", err)
	}
}

func TestStreamChatRequiresModelAndMessages(t *testing.T) {
	client := NewOpenAIClient(Options{BaseURL: "http://unused"})
	if err := client.StreamChat(context.Background(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, func(string) error { return nil }); err == nil {
		t.Error("expected error when no model is set, got nil")
	}
	if err := client.StreamChat(context.Background(), ChatRequest{Model: "m"}, func(string) error { return nil }); err == nil {
		t.Error("expected error when messages empty, got nil")
	}
}
