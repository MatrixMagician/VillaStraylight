// Package llm is VillaStraylight's model gateway. It defines a provider-agnostic
// Client interface plus an OpenAI-compatible implementation, so additional
// backends (native Ollama, Anthropic, etc.) can be added without touching the
// HTTP layer.
package llm

import (
	"context"
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
	// Model is optional; the client falls back to its configured default.
	Model    string    `json:"model,omitempty"`
	Messages []Message `json:"messages"`
}

// StreamFunc receives incremental content deltas as they arrive from the model.
// Returning an error aborts the stream.
type StreamFunc func(delta string) error

// Client is the gateway abstraction every model backend implements.
type Client interface {
	// StreamChat streams a chat completion, invoking onDelta for each content
	// chunk. It returns when the stream completes, the context is cancelled, or
	// an error occurs.
	StreamChat(ctx context.Context, req ChatRequest, onDelta StreamFunc) error
}

// Options configures an OpenAI-compatible client.
type Options struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	Timeout      time.Duration
}
