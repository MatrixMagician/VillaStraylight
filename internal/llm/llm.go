// Package llm is VillaStraylight's model gateway: an OpenAI-compatible client
// for talking to the local llama-server.
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
}

// StreamFunc receives incremental content deltas as they arrive from the model.
// Returning an error aborts the stream.
type StreamFunc func(delta string) error

// Options configures an OpenAI-compatible client.
type Options struct {
	BaseURL string
	Timeout time.Duration
}
