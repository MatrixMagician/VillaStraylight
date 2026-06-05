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
