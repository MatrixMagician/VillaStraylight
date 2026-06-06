package llm

import (
	"context"
	"encoding/json"
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

// TestCompleteParsesTimings proves Complete reads the server-computed per-request
// timings block (pp/tg separated) from a non-streaming /v1 response body.
func TestCompleteParsesTimings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","content":"hi"}}],
			"timings":{
				"prompt_n":42,"prompt_ms":210.5,"prompt_per_second":199.5,
				"predicted_n":128,"predicted_ms":3200.0,"predicted_per_second":40.0
			}
		}`))
	}))
	defer srv.Close()

	client := NewOpenAIClient(Options{BaseURL: srv.URL, DefaultModel: "test-model"})
	tm, err := client.Complete(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, 128, 7, 0.0)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if tm.PromptPerSecond != 199.5 {
		t.Errorf("PromptPerSecond = %v, want 199.5", tm.PromptPerSecond)
	}
	if tm.PredictedPerSec != 40.0 {
		t.Errorf("PredictedPerSec = %v, want 40.0", tm.PredictedPerSec)
	}
	if tm.PromptN != 42 {
		t.Errorf("PromptN = %d, want 42", tm.PromptN)
	}
	if tm.PredictedN != 128 {
		t.Errorf("PredictedN = %d, want 128", tm.PredictedN)
	}
}

// TestCompleteParamsOnWire proves Complete sends stream=false and the fixed
// (max_tokens, seed, temperature) params on the request body.
func TestCompleteParamsOnWire(t *testing.T) {
	type wire struct {
		Stream      bool    `json:"stream"`
		MaxTokens   int     `json:"max_tokens"`
		Seed        int     `json:"seed"`
		Temperature float64 `json:"temperature"`
	}
	got := make(chan wire, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body wire
		_ = json.NewDecoder(r.Body).Decode(&body)
		got <- body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"timings":{"predicted_per_second":1.0}}`))
	}))
	defer srv.Close()

	client := NewOpenAIClient(Options{BaseURL: srv.URL, DefaultModel: "test-model"})
	if _, err := client.Complete(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, 256, 99, 0.7); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	body := <-got
	if body.Stream {
		t.Error("stream = true, want false for a non-streaming completion")
	}
	if body.MaxTokens != 256 {
		t.Errorf("max_tokens = %d, want 256", body.MaxTokens)
	}
	if body.Seed != 99 {
		t.Errorf("seed = %d, want 99", body.Seed)
	}
	if body.Temperature != 0.7 {
		t.Errorf("temperature = %v, want 0.7", body.Temperature)
	}
}

// TestCompleteUpstreamError proves a non-200 yields a bounded-snippet error, no panic.
func TestCompleteUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, strings.Repeat("x", 8192)+" boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewOpenAIClient(Options{BaseURL: srv.URL, DefaultModel: "test-model"})
	_, err := client.Complete(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, 16, 1, 0.0)
	if err == nil {
		t.Fatal("expected error from non-200 upstream, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q should reference upstream status", err)
	}
	// Snippet must be bounded by io.LimitReader(2048); the 8192-byte body
	// (plus "boom") must never appear in full.
	if len(err.Error()) > 2048+128 {
		t.Errorf("error length %d exceeds bounded snippet", len(err.Error()))
	}
}

// TestCompleteRequiresModel proves the same no-model guard StreamChat enforces.
func TestCompleteRequiresModel(t *testing.T) {
	client := NewOpenAIClient(Options{BaseURL: "http://unused"})
	_, err := client.Complete(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, 16, 1, 0.0)
	if err == nil {
		t.Fatal("expected error when no model is set, got nil")
	}
	if !strings.Contains(err.Error(), "no model specified") {
		t.Errorf("error %q should match StreamChat's no-model message", err)
	}
}
