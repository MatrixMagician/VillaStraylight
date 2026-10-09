package inference

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// completionServer answers /v1/chat/completions with one fixed non-streaming body
// and records the request it was sent.
func completionServer(t *testing.T, status int, body string) (*httptest.Server, *[]byte) {
	t.Helper()
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

// TestDecodeRate: the served model's decode rate is llama-server's own per-request
// timing of a bounded completion (#318). It is the number the agent proof budget is
// derived from, so a reply that carries no timings, too few tokens to be a rate, or
// no reply at all must be an error, never a fabricated rate.
func TestDecodeRate(t *testing.T) {
	const timed = `{"choices":[{"message":{"content":"x"}}],"timings":{"prompt_n":12,"prompt_ms":30,"predicted_n":64,"predicted_ms":6400,"predicted_per_second":10.0}}`

	t.Run("reads the server's timings", func(t *testing.T) {
		srv, _ := completionServer(t, http.StatusOK, timed)
		rate, err := NewClient(srv.URL, "").DecodeRate(t.Context(), "gemma-4-31b")
		if err != nil {
			t.Fatalf("DecodeRate: %v", err)
		}
		if rate != 10.0 {
			t.Errorf("DecodeRate = %v tok/s, want 10", rate)
		}
	})

	t.Run("the request bounds tokens, keeps thinking on and does not stream", func(t *testing.T) {
		srv, body := completionServer(t, http.StatusOK, timed)
		if _, err := NewClient(srv.URL, "").DecodeRate(t.Context(), "gemma-4-31b"); err != nil {
			t.Fatalf("DecodeRate: %v", err)
		}
		var req struct {
			Model     string         `json:"model"`
			MaxTokens int            `json:"max_tokens"`
			Stream    bool           `json:"stream"`
			Kwargs    map[string]any `json:"chat_template_kwargs"`
		}
		if err := json.Unmarshal(*body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "gemma-4-31b" {
			t.Errorf("model = %q, want gemma-4-31b", req.Model)
		}
		if req.MaxTokens != decodeProbeMaxTokens {
			t.Errorf("max_tokens = %d, want %d", req.MaxTokens, decodeProbeMaxTokens)
		}
		if req.Stream {
			t.Errorf("stream = true, want false (the server's timings ride on the final body)")
		}
		if _, off := req.Kwargs["enable_thinking"]; off {
			t.Errorf("chat_template_kwargs = %v, want thinking left on: the agent talks to the model as served", req.Kwargs)
		}
	})

	// llama-server's ngram-mod keeps one n-gram map across every request, so a probe
	// whose output repeats reads the draft's replay, not the model: measured on the
	// dev host 2026-10-09, an identical greedy probe read 51.4 then 196.6 tok/s (53 of
	// 53 drafted tokens accepted), and a nonce in the prompt or a seeded temperature-1
	// sample still read 93.5 to 259.7 and 70.8 tok/s. A flat sampler with a fresh seed
	// read 44.2 to 46.3 tok/s with 0 to 4 tokens accepted, back to back.
	t.Run("each probe samples flat with a fresh seed, so no draft can replay an earlier one", func(t *testing.T) {
		type sampled struct {
			Temperature float64  `json:"temperature"`
			TopK        *int     `json:"top_k"`
			TopP        *float64 `json:"top_p"`
			MinP        *float64 `json:"min_p"`
			Seed        int      `json:"seed"`
		}
		var reqs []sampled
		for range 2 {
			srv, body := completionServer(t, http.StatusOK, timed)
			if _, err := NewClient(srv.URL, "").DecodeRate(t.Context(), "m"); err != nil {
				t.Fatalf("DecodeRate: %v", err)
			}
			var req sampled
			if err := json.Unmarshal(*body, &req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			reqs = append(reqs, req)
		}
		for i, req := range reqs {
			if req.Temperature != 2 || req.TopK == nil || *req.TopK != 0 || req.TopP == nil || *req.TopP != 1 || req.MinP == nil || *req.MinP != 0 {
				t.Errorf("probe %d sampler = temperature %v top_k %v top_p %v min_p %v, want 2, 0, 1, 0", i+1, req.Temperature, deref(req.TopK), deref(req.TopP), deref(req.MinP))
			}
		}
		if reqs[0].Seed == reqs[1].Seed {
			t.Errorf("both probes sent seed %d, want a fresh seed per probe", reqs[0].Seed)
		}
	})

	t.Run("a reply without timings is an error", func(t *testing.T) {
		srv, _ := completionServer(t, http.StatusOK, `{"choices":[{"message":{"content":"x"}}]}`)
		if _, err := NewClient(srv.URL, "").DecodeRate(t.Context(), "m"); err == nil {
			t.Fatalf("DecodeRate: err = nil on a body without timings, want an error")
		}
	})

	t.Run("too few tokens are not a rate", func(t *testing.T) {
		srv, _ := completionServer(t, http.StatusOK, `{"choices":[{"message":{"content":"ok"}}],"timings":{"predicted_n":2,"predicted_ms":40,"predicted_per_second":50.0}}`)
		_, err := NewClient(srv.URL, "").DecodeRate(t.Context(), "m")
		if !errors.Is(err, errDecodeTooShort) {
			t.Fatalf("DecodeRate: err = %v, want errDecodeTooShort", err)
		}
	})

	t.Run("a non-200 is an error that names the status", func(t *testing.T) {
		srv, _ := completionServer(t, http.StatusUnauthorized, `{"error":"no key"}`)
		_, err := NewClient(srv.URL, "").DecodeRate(t.Context(), "m")
		if err == nil || !strings.Contains(err.Error(), "401") {
			t.Fatalf("DecodeRate: err = %v, want the 401 named", err)
		}
	})
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
