package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIClient talks to any OpenAI-compatible /chat/completions endpoint using
// server-sent-event streaming. It works with Ollama, llama.cpp's server, vLLM,
// LM Studio, and the OpenAI API itself.
type OpenAIClient struct {
	baseURL      string
	apiKey       string
	defaultModel string
	httpClient   *http.Client
}

// NewOpenAIClient builds a client from Options.
func NewOpenAIClient(opts Options) *OpenAIClient {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &OpenAIClient{
		baseURL:      strings.TrimRight(opts.BaseURL, "/"),
		apiKey:       opts.APIKey,
		defaultModel: opts.DefaultModel,
		httpClient:   &http.Client{Timeout: timeout},
	}
}

// wire types mirror the OpenAI streaming schema (only the fields we consume).
type wireRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type wireChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// Timings is the llama.cpp /v1 per-request timings extension. The server computes
// these for exactly this completion, with prompt-processing (pp) and
// token-generation (tg) rates already separated — the honest throughput source
// the bench reads, never the /metrics last-window averages (which smear warmup).
type Timings struct {
	PromptN         int     `json:"prompt_n"`
	PromptMS        float64 `json:"prompt_ms"`
	PromptPerSecond float64 `json:"prompt_per_second"`
	PredictedN      int     `json:"predicted_n"`
	PredictedMS     float64 `json:"predicted_ms"`
	PredictedPerSec float64 `json:"predicted_per_second"`
}

// completeRequest is the non-streaming sibling of wireRequest: it carries the
// fixed (max_tokens, seed, temperature) params on the wire so every bench run is
// reproducible, and forces stream=false so the server returns the timings block.
type completeRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	MaxTokens   int       `json:"max_tokens"`
	Seed        int       `json:"seed"`
	Temperature float64   `json:"temperature"`
}

// completeResponse captures only the top-level timings block; the choices/content
// the bench does not need are intentionally not deserialized.
type completeResponse struct {
	Timings Timings `json:"timings"`
}

// StreamChat implements Client.
func (c *OpenAIClient) StreamChat(ctx context.Context, req ChatRequest, onDelta StreamFunc) error {
	model := req.Model
	if model == "" {
		model = c.defaultModel
	}
	if model == "" {
		return fmt.Errorf("llm: no model specified and no default configured")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("llm: messages must not be empty")
	}

	body, err := json.Marshal(wireRequest{Model: model, Messages: req.Messages, Stream: true})
	if err != nil {
		return fmt.Errorf("llm: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("llm: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("llm: request to %s failed: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("llm: upstream returned %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
	}

	return parseSSE(resp.Body, onDelta)
}

// Complete drives a non-streaming /v1 chat completion with fixed (max_tokens,
// seed, temperature) params and returns the server-computed per-request Timings.
// Unlike StreamChat (which forwards content deltas and discards the timings
// block), Complete's whole purpose is to capture that block as the honest,
// per-run, pp/tg-separated throughput source for the bench.
func (c *OpenAIClient) Complete(ctx context.Context, req ChatRequest, nPredict int, seed int, temp float64) (Timings, error) {
	model := req.Model
	if model == "" {
		model = c.defaultModel
	}
	if model == "" {
		return Timings{}, fmt.Errorf("llm: no model specified and no default configured")
	}
	if len(req.Messages) == 0 {
		return Timings{}, fmt.Errorf("llm: messages must not be empty")
	}

	body, err := json.Marshal(completeRequest{
		Model:       model,
		Messages:    req.Messages,
		Stream:      false,
		MaxTokens:   nPredict,
		Seed:        seed,
		Temperature: temp,
	})
	if err != nil {
		return Timings{}, fmt.Errorf("llm: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Timings{}, fmt.Errorf("llm: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Timings{}, fmt.Errorf("llm: request to %s failed: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return Timings{}, fmt.Errorf("llm: upstream returned %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
	}

	var parsed completeResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return Timings{}, fmt.Errorf("llm: decode response: %w", err)
	}
	return parsed.Timings, nil
}

// parseSSE reads an OpenAI-style SSE stream and forwards content deltas.
func parseSSE(r io.Reader, onDelta StreamFunc) error {
	scanner := bufio.NewScanner(r)
	// Allow long lines (large tokens / reasoning blocks).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}

		var chunk wireChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Skip keep-alives / non-JSON comments rather than failing the stream.
			continue
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content == "" {
				continue
			}
			if err := onDelta(ch.Delta.Content); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("llm: read stream: %w", err)
	}
	return nil
}
