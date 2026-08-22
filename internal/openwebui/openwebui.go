// Package openwebui owns the Open WebUI HTTP protocol: sign-in, knowledge, files,
// chats, model discovery and health.
//
// The protocol had no module. The recall command injected seventeen dependencies,
// twelve of which were a one-to-one rename of twelve functions defined beside them —
// an interface that is a rename of its implementation, which is the definition of a
// shallow module. Endpoint literals appeared in more than one file with no shared
// client, and three different strategies for shelling out to curl existed across the
// memory verify path, the memory install path and the status probe.
//
// The seam is the TRANSPORT, one thing, not a dozen named functions. Two adapters
// justify it: the real loopback curl in production and a fake in tests. Everything
// above the transport — paths, pagination, parsing, the read-merge-write attach
// choreography — is ordinary code a test reaches through that one seam.
//
// # Endpoint paths live here and nowhere else
//
// Every path this package speaks is a const in paths.go. A path literal outside this
// package is a bug; there is a test in the recall package guarding the command tier
// against reintroducing them.
//
// # Honesty invariants carried from the original seam
//
//   - An empty id in a 200 body is an ERROR carrying the truncated raw body for
//     diagnosis, never a silent skip.
//   - Unknown is distinct from Missing. An unevaluable attachment state is never
//     reported as a confident absence.
//   - Indexing writes go ONLY through the knowledge/files pipeline; villa never
//     writes Qdrant directly.
package openwebui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Request is one protocol request, described in terms of HTTP rather than of curl
// flags. The transport adapter decides how to perform it.
type Request struct {
	// Method is the HTTP method. Empty means GET.
	Method string
	// Path is the URL path and query, relative to the client's base. It always comes
	// from a const in paths.go.
	Path string
	// Token is the bearer JWT, or empty for an unauthenticated request.
	Token string
	// Body is the JSON request body, or nil.
	Body []byte
	// Upload, when non-empty, makes this a multipart file upload whose content
	// travels via STDIN rather than argv or a temp file — transcripts can be large
	// and must never reach a command line.
	Upload *Upload
	// TimeoutSeconds bounds this single request, for the cheap probes that must not
	// hang. Zero leaves bounding to the context.
	TimeoutSeconds int
}

// Upload is a multipart file upload's content and filename.
type Upload struct {
	Filename string
	Content  string
}

// Transport performs one request and returns the response body. A non-2xx response
// is an error: the protocol treats HTTP failure as failure, never as an empty
// result. This is the ONE seam of this package.
type Transport func(ctx context.Context, req Request) ([]byte, error)

// Client speaks the Open WebUI protocol over an injected transport.
type Client struct {
	transport Transport
	// clock and sleep are seams for the ONE operation that waits: polling a file's
	// processing status. They exist so a test can prove the timeout is an error
	// rather than spending real seconds proving it.
	clock func() time.Time
	sleep func(time.Duration) <-chan time.Time
}

// New builds a client over the given transport, with the real clock.
func New(t Transport) *Client {
	return &Client{transport: t, clock: time.Now, sleep: time.After}
}

// WithClock returns a client whose waiting is driven by the supplied clock and
// sleep, for tests of the polling timeout.
func (c *Client) WithClock(now func() time.Time, after func(time.Duration) <-chan time.Time) *Client {
	clone := *c
	clone.clock = now
	clone.sleep = after
	return &clone
}

func (c *Client) now() time.Time                         { return c.clock() }
func (c *Client) after(d time.Duration) <-chan time.Time { return c.sleep(d) }

// jsonUnmarshal is json.Unmarshal, named locally so every decode in this package
// reads the same and no caller reaches for encoding/json directly.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// do performs a request, wrapping any failure with the endpoint name so an error
// always says which call failed.
func (c *Client) do(ctx context.Context, name string, req Request) ([]byte, error) {
	out, err := c.transport(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

// decode parses a JSON response, reporting a parse miss with the truncated raw body
// so a failure stays diagnosable without dumping hundreds of KiB to stderr.
func decode(name string, out []byte, v any) error {
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("parse %s (%v): %s", name, err, truncate(out))
	}
	return nil
}

// truncate bounds a raw response body embedded in an error detail.
func truncate(out []byte) string {
	const limit = 512
	if len(out) <= limit {
		return string(out)
	}
	return string(out[:limit]) + "…(truncated)"
}

// jsonBody marshals a request body. It exists so no caller hand-builds JSON.
func jsonBody(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}
	return b, nil
}

// Health is the cheap pre-mutation reachability gate. It runs BEFORE any mutating
// step so a down Open WebUI (or its embedder dependency, since creating a knowledge
// collection embeds its metadata) surfaces as a refusal naming the service rather
// than a confusing mid-create failure.
func (c *Client) Health(ctx context.Context) error {
	_, err := c.do(ctx, "health", Request{Path: pathHealth, TimeoutSeconds: healthTimeoutSeconds})
	return err
}

// SignIn mints the admin JWT for the villa service account, signing up first if the
// account does not exist yet (the first user on a fresh database becomes admin). The
// token is held in memory only and is never persisted.
func (c *Client) SignIn(ctx context.Context, email, password, name string) (string, error) {
	extract := func(out []byte) (string, bool) {
		var r struct {
			Token string `json:"token"`
		}
		if json.Unmarshal(out, &r) == nil && r.Token != "" {
			return r.Token, true
		}
		return "", false
	}

	cred, err := jsonBody(map[string]string{"email": email, "password": password})
	if err != nil {
		return "", err
	}
	if out, serr := c.transport(ctx, Request{Method: "POST", Path: pathSignIn, Body: cred}); serr == nil {
		if tok, ok := extract(out); ok {
			return tok, nil
		}
	}

	signup, err := jsonBody(map[string]string{"name": name, "email": email, "password": password})
	if err != nil {
		return "", err
	}
	out, err := c.transport(ctx, Request{Method: "POST", Path: pathSignUp, Body: signup})
	if err != nil {
		return "", fmt.Errorf("signin and signup both failed: %w", err)
	}
	tok, ok := extract(out)
	if !ok {
		return "", fmt.Errorf("signup returned no token: %s", truncate(out))
	}
	return tok, nil
}

// DiscoverModel resolves the SERVED model id — the GGUF filename the inference
// server reports, not the config slug.
func (c *Client) DiscoverModel(ctx context.Context, token string) (string, error) {
	out, err := c.do(ctx, "models", Request{Path: pathModels, Token: token})
	if err != nil {
		return "", err
	}
	var r struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if derr := decode("/api/models", out, &r); derr != nil {
		return "", derr
	}
	for _, m := range r.Data {
		if m.ID != "" {
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("no chat model available from /api/models: %s", truncate(out))
}

// ChatCompletion posts a chat completion and returns the raw response body for the
// caller to parse. It is the drive used by the RAG smoke proof and by the
// search-load residency drive, both of which read fields the protocol does not model.
func (c *Client) ChatCompletion(ctx context.Context, token string, body []byte) ([]byte, error) {
	return c.do(ctx, "chat/completions", Request{
		Method: "POST",
		Path:   pathChatCompletions,
		Token:  token,
		Body:   body,
	})
}

// isCompleteStatus reports whether a file-processing status means the
// chunk-embed-store pipeline finished successfully.
func isCompleteStatus(s string) bool {
	switch strings.ToLower(s) {
	case "completed", "done", "success", "processed":
		return true
	}
	return false
}

// isFailedStatus reports whether a file-processing status is a confident failure, as
// distinct from "not finished yet".
func isFailedStatus(s string) bool {
	switch strings.ToLower(s) {
	case "failed", "error":
		return true
	}
	return false
}
