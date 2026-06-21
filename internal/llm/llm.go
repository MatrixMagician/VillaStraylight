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
	// Model is optional; the client falls back to its configured default.
	Model    string    `json:"model,omitempty"`
	Messages []Message `json:"messages"`
}

// StreamFunc receives incremental content deltas as they arrive from the model.
// Returning an error aborts the stream.
type StreamFunc func(delta string) error

// Options configures an OpenAI-compatible client.
type Options struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	Timeout      time.Duration
}
