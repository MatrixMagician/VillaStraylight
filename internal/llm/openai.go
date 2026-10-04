package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNoTimings signals that an otherwise-successful completion response carried no
// usable per-request `timings` block (the llama.cpp extension was absent, or decoded
// to an all-zero struct — predicted_n==0 AND predicted_per_second==0). A bench MUST
// treat such a run as a measurement failure (VOID), never fold a 0 tok/s sample into
// the honest band (RESEARCH Assumption A1: some builds omit `timings` on /v1). Callers
// detect it with errors.Is(err, llm.ErrNoTimings).
var ErrNoTimings = errors.New("llm: response carried no usable timings block")

// OpenAIClient talks to any OpenAI-compatible /chat/completions endpoint using
// server-sent-event streaming. It works with Ollama, llama.cpp's server, vLLM,
// LM Studio, and the OpenAI API itself.
type OpenAIClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewOpenAIClient builds a client from Options.
func NewOpenAIClient(opts Options) *OpenAIClient {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &OpenAIClient{
		baseURL:    strings.TrimRight(opts.BaseURL, "/"),
		apiKey:     opts.APIKey,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// setAuth attaches the Bearer header when the client carries a key
// (GHSA-qxg9, ADR-0011). A client built with no APIKey sends the request
// exactly as before — unauthenticated, which llama-server now answers 401.
func (c *OpenAIClient) setAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

// wire types mirror the OpenAI streaming schema (only the fields we consume).
type wireRequest struct {
	Model              string         `json:"model"`
	Messages           []Message      `json:"messages"`
	Stream             bool           `json:"stream"`
	Temperature        *float64       `json:"temperature,omitempty"`
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
	MaxTokens          int            `json:"max_tokens,omitempty"`
	Tools              []Tool         `json:"tools,omitempty"`
	ToolChoice         string         `json:"tool_choice,omitempty"`
}

// Tool is one function the model may call, in the OpenAI wire shape
// ({"type":"function","function":{...}}).
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction names a callable function and its JSON-schema parameters.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ToolCall is one function call the model made. Arguments is the JSON text the
// model wrote, verbatim: whether it parses is the caller's question, not the
// transport's.
type ToolCall struct {
	Name      string
	Arguments string
}

// Reply is a non-streamed completion's message: its content and every tool call.
// A reply that only calls a tool has empty Content.
type Reply struct {
	Content   string
	ToolCalls []ToolCall
}

// wireReply is the non-streamed response: only the first choice's message is read.
// A tool-only reply carries "content": null, which decodes to "".
type wireReply struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
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
	// DraftN / DraftNAccepted are the pinned server's (b9536) speculative-decoding
	// counters: tokens the draft proposed and tokens the target accepted. Both are
	// absent from the wire (decode to 0) when the request drafted nothing, which is
	// distinct from a 0% acceptance and must never be folded in as one (#119).
	DraftN         int `json:"draft_n"`
	DraftNAccepted int `json:"draft_n_accepted"`
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

// StreamChat streams a chat completion, invoking onDelta for each content
// chunk. It returns when the stream completes, the context is cancelled, or an
// error occurs.
func (c *OpenAIClient) StreamChat(ctx context.Context, req ChatRequest, onDelta StreamFunc) error {
	body, err := encode(wireRequest{Model: req.Model, Messages: req.Messages, Stream: true,
		Temperature: req.Temperature, ChatTemplateKwargs: req.ChatTemplateKwargs, MaxTokens: req.MaxTokens})
	if err != nil {
		return err
	}
	resp, err := c.post(ctx, body, "text/event-stream")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return parseSSE(resp.Body, onDelta)
}

// Chat sends one non-streamed chat completion and returns its message: the content
// and every tool call. It is the only call that sends req.Tools, because a tool
// call arrives whole in the message rather than as content deltas.
func (c *OpenAIClient) Chat(ctx context.Context, req ChatRequest) (Reply, error) {
	body, err := encode(wireRequest{Model: req.Model, Messages: req.Messages, Stream: false,
		Temperature: req.Temperature, ChatTemplateKwargs: req.ChatTemplateKwargs, MaxTokens: req.MaxTokens,
		Tools: req.Tools, ToolChoice: req.ToolChoice})
	if err != nil {
		return Reply{}, err
	}
	resp, err := c.post(ctx, body, "application/json")
	if err != nil {
		return Reply{}, err
	}
	defer resp.Body.Close()
	return decodeReply(resp.Body)
}

// encode refuses a request with no model or no messages, then marshals it.
func encode(w wireRequest) ([]byte, error) {
	if w.Model == "" {
		return nil, fmt.Errorf("llm: no model specified")
	}
	if len(w.Messages) == 0 {
		return nil, fmt.Errorf("llm: messages must not be empty")
	}
	body, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}
	return body, nil
}

// post sends body to the chat route with the key attached and returns the response
// of a 200 only; any other status is an error carrying the start of its body. The
// caller closes the returned body.
func (c *OpenAIClient) post(ctx context.Context, body []byte, accept string) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", accept)
	c.setAuth(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: request to %s failed: %w", c.baseURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("llm: upstream returned %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
	}
	return resp, nil
}

// decodeReply reads a non-streamed response's first message. A body that does not
// decode, or that carries no choice, is an error rather than an empty reply.
func decodeReply(r io.Reader) (Reply, error) {
	var parsed wireReply
	if err := json.NewDecoder(r).Decode(&parsed); err != nil {
		return Reply{}, fmt.Errorf("llm: decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Reply{}, fmt.Errorf("llm: response carried no choices")
	}
	msg := parsed.Choices[0].Message
	reply := Reply{Content: msg.Content}
	for _, call := range msg.ToolCalls {
		reply.ToolCalls = append(reply.ToolCalls, ToolCall{Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	return reply, nil
}

// Complete drives a non-streaming /v1 chat completion with fixed (max_tokens,
// seed, temperature) params and returns the server-computed per-request Timings.
// Unlike StreamChat (which forwards content deltas and discards the timings
// block), Complete's whole purpose is to capture that block as the honest,
// per-run, pp/tg-separated throughput source for the bench.
func (c *OpenAIClient) Complete(ctx context.Context, req ChatRequest, nPredict int, seed int, temp float64) (Timings, error) {
	model := req.Model
	if model == "" {
		return Timings{}, fmt.Errorf("llm: no model specified")
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
	c.setAuth(httpReq)

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
	// An absent `timings` block JSON-decodes to a zero-valued struct that would silently
	// pollute the bench's honest band with a 0 tok/s sample (RESEARCH A1: some builds omit
	// it on /v1). Signal it distinctly so the bench voids the run rather than counting it:
	// predicted_n==0 AND predicted_per_second==0 means there is no usable tg measurement.
	if parsed.Timings.PredictedN == 0 && parsed.Timings.PredictedPerSec == 0 {
		return Timings{}, ErrNoTimings
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
