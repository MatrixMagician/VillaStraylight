// Package llm is VillaStraylight's OpenAI wire protocol: an OpenAI-compatible client
// for the local llama-server's chat route. The control plane reaches it only
// through inference.Client, which owns the address and the api key (ADR-0014).
package llm

import (
	"time"
)

// Role identifies the author of a chat message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single turn in a conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is a provider-agnostic chat completion request.
type ChatRequest struct {
	// Model names the model to run; it is required.
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	// Temperature is the sampling temperature; nil omits it from the wire
	// request and leaves the server default in effect. A pointer distinguishes
	// "unset" from an explicit 0 (internal/grounding's audit call needs the
	// latter).
	Temperature *float64 `json:"temperature,omitempty"`
	// ChatTemplateKwargs passes template-level flags straight through to the
	// server (e.g. {"enable_thinking": false}). internal/grounding's audit
	// call needs thinking disabled, or the model spends its whole token
	// budget reasoning and returns no content.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
	// MaxTokens bounds the completion; zero omits it and leaves the server
	// default in effect.
	MaxTokens int `json:"max_tokens,omitempty"`
	// Tools are the functions the model may call, in the OpenAI wire shape; nil
	// omits them. Only Chat sends them: StreamChat's parser reads content deltas
	// only, so a streamed tool call would be lost rather than returned. llama-server
	// honours them only when it was started with --jinja (tools mode).
	Tools []Tool `json:"tools,omitempty"`
	// ToolChoice is the OpenAI tool_choice string ("auto", "none" or "required");
	// empty omits it and leaves the server default in effect.
	ToolChoice string `json:"tool_choice,omitempty"`
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

// StreamFunc receives incremental content deltas as they arrive from the model.
// Returning an error aborts the stream.
type StreamFunc func(delta string) error

// Options configures an OpenAI-compatible client.
type Options struct {
	BaseURL string
	Timeout time.Duration
	// APIKey is sent as `Authorization: Bearer <APIKey>` on every request when
	// non-empty (GHSA-qxg9, ADR-0011). Empty omits the header, which a keyed
	// llama-server answers with a 401, never a client-side panic. The control plane
	// builds this client only through inference.Client.Chat, which sets it
	// (ADR-0014).
	APIKey string
}
