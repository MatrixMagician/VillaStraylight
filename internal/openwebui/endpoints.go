package openwebui

// endpoints.go reconciles Open WebUI's OpenAI-compatible connection list through the
// admin API.
//
// OPENAI_API_BASE_URLS is a PersistentConfig variable: Open WebUI reads it from the
// environment on FIRST launch only, persists it to its own database, and reads the
// database on every boot after that. Editing the rendered Quadlet Environment= line on
// an EXISTING install therefore does nothing, with no error anywhere — the extra
// resident endpoints simply never appear in the chat UI. The rendered env stays correct
// for a fresh install; this file is what makes an existing one converge.
//
// The document is modelled as an ordered list of connections rather than as the wire
// shape's three parallel collections. The wire shape can express documents that mean
// nothing — three URLs against two keys, a per-connection config keyed "7" against four
// endpoints — and re-ordering those collections by hand is precisely how one endpoint
// silently inherits another's config. A connection owns its URL, key and config
// together, so the lists cannot disagree by construction and a config cannot be
// separated from the endpoint it configures.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// NoAuthAPIKey is the required-but-ignored placeholder Open WebUI needs before it will
// register a connection to a llama-server that wants no authentication. Open WebUI
// truncates or zero-pads the keys array to the URL count server-side, so an endpoint
// without one is not an error there — but it is unregistered, which is worse.
//
// internal/orchestrate holds the same literal for the rendered Environment= line. The
// two must agree, or a fresh install and a reconciled one would disagree about the key
// for the same endpoint; they are separate consts only because orchestrate's is
// unexported and this package must not import the orchestration layer.
const NoAuthAPIKey = "sk-no-key-required"

// Connection is one OpenAI-compatible endpoint Open WebUI serves models from: its base
// URL, its API key, and its opaque per-connection config (the prefix, enable flag and
// model filter the admin UI writes). Config is carried verbatim rather than modelled,
// because villa sets none of it and must lose none of it.
type Connection struct {
	URL    string
	Key    string
	Config json.RawMessage
}

// Config is the /openai/config document: whether the OpenAI-compatible API is on at
// all, and the ordered connection list. Order is load-bearing — the first endpoint is
// the primary, and the wire form keys per-connection configs by list index.
type Config struct {
	Enabled     bool
	Connections []Connection
}

// wireConfig is the on-the-wire shape: three parallel collections that only mean
// something read together. It exists only at the JSON boundary, so no code above the
// parse can index one collection with another's offset.
type wireConfig struct {
	Enabled bool                       `json:"ENABLE_OPENAI_API"`
	URLs    []string                   `json:"OPENAI_API_BASE_URLS"`
	Keys    []string                   `json:"OPENAI_API_KEYS"`
	Configs map[string]json.RawMessage `json:"OPENAI_API_CONFIGS"`
}

// UnmarshalJSON parses the wire document into connections, refusing any document it
// cannot attribute unambiguously. Guessing which key or config belongs to which
// endpoint is how a user's credential ends up pointed at the wrong server.
func (c *Config) UnmarshalJSON(data []byte) error {
	var w wireConfig
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	if len(w.URLs) != len(w.Keys) {
		return fmt.Errorf("openai config lists disagree: %d base URLs but %d keys — refusing to guess which key belongs to which endpoint; repair the connection list in Open WebUI's admin settings", len(w.URLs), len(w.Keys))
	}
	conns := make([]Connection, len(w.URLs))
	for i := range w.URLs {
		conns[i] = Connection{URL: w.URLs[i], Key: w.Keys[i]}
	}
	for k, v := range w.Configs {
		idx, err := strconv.Atoi(k)
		if err != nil || idx < 0 || idx >= len(conns) {
			return fmt.Errorf("openai config carries a per-connection config keyed %q, which indexes none of the %d configured endpoints — remove the stale entry in Open WebUI's admin connection settings", k, len(conns))
		}
		conns[idx].Config = v
	}
	*c = Config{Enabled: w.Enabled, Connections: conns}
	return nil
}

// MarshalJSON emits the wire document, re-keying every per-connection config to its
// endpoint's CURRENT index. This is the whole reason the reorder is safe.
func (c Config) MarshalJSON() ([]byte, error) {
	w := wireConfig{
		Enabled: c.Enabled,
		URLs:    make([]string, len(c.Connections)),
		Keys:    make([]string, len(c.Connections)),
		Configs: map[string]json.RawMessage{},
	}
	for i, conn := range c.Connections {
		w.URLs[i] = conn.URL
		w.Keys[i] = conn.Key
		if len(conn.Config) > 0 {
			w.Configs[strconv.Itoa(i)] = conn.Config
		}
	}
	return json.Marshal(w)
}

// equal reports whether two documents are the same connection list in the same order.
// It is what makes reconciliation idempotent: an equal document is not written back.
func (c Config) equal(other Config) bool {
	if c.Enabled != other.Enabled || len(c.Connections) != len(other.Connections) {
		return false
	}
	for i, a := range c.Connections {
		b := other.Connections[i]
		if a.URL != b.URL || a.Key != b.Key || !bytes.Equal(a.Config, b.Config) {
			return false
		}
	}
	return true
}

// ReconcileEndpoints computes the connection list villa wants, given the one Open WebUI
// currently holds. It is pure: the caller decides whether to write, and writes only when
// changed is true.
//
// want is the desired ordered endpoint list, primary first. Those endpoints are villa's
// and are placed in that order at the head of the list. Every other connection is the
// user's own and survives, appended after villa's in its existing relative order —
// reconciling this stack's endpoints must not delete a connection someone added to a
// machine down the hall.
//
// Enabled is carried through untouched. A user who turned the OpenAI-compatible API off
// did so deliberately, and silently switching it back on is not this function's call.
func ReconcileEndpoints(current Config, want []string) (Config, bool) {
	held := make(map[string]Connection, len(current.Connections))
	for _, conn := range current.Connections {
		if _, dup := held[conn.URL]; !dup {
			held[conn.URL] = conn
		}
	}

	next := Config{
		Enabled:     current.Enabled,
		Connections: make([]Connection, 0, len(current.Connections)+len(want)),
	}
	ours := make(map[string]bool, len(want))
	for _, url := range want {
		if url == "" || ours[url] {
			continue
		}
		ours[url] = true
		// The key is always the sentinel: these are in-network llama-servers that take
		// no auth. The config is whatever the user set for this endpoint in the admin
		// UI, which moves with it rather than staying at its old index.
		next.Connections = append(next.Connections, Connection{
			URL:    url,
			Key:    NoAuthAPIKey,
			Config: held[url].Config,
		})
	}
	for _, conn := range current.Connections {
		if ours[conn.URL] {
			continue
		}
		next.Connections = append(next.Connections, conn)
	}
	return next, !next.equal(current)
}

// EndpointSync is the verdict of one reconciliation: whether a write actually happened,
// and the endpoint list Open WebUI holds afterwards. Wrote is the honest answer to "did
// this run change anything", which a bare error cannot give.
type EndpointSync struct {
	Wrote     bool
	Endpoints []string
}

// SyncEndpoints reads Open WebUI's connection list, reconciles villa's endpoints into
// it, and writes the result back ONLY when it differs. Both calls need an admin session;
// the upstream handlers depend on get_admin_user.
//
// Failure is closed at every step: an unreachable or non-2xx endpoint, a body that does
// not parse, and a document whose URL and key counts disagree are all errors naming what
// to do, never a silently defaulted or half-applied list.
func (c *Client) SyncEndpoints(ctx context.Context, token string, want []string) (EndpointSync, error) {
	out, err := c.do(ctx, "openai/config", Request{Path: pathOpenAIConfig, Token: token})
	if err != nil {
		return EndpointSync{}, err
	}
	var current Config
	if jerr := jsonUnmarshal(out, &current); jerr != nil {
		// Deliberately not decode(): its diagnostic embeds the raw body, and this
		// body carries the user's API keys.
		return EndpointSync{}, fmt.Errorf("parse openai/config: %w (response body withheld: it carries API keys)", jerr)
	}

	next, changed := ReconcileEndpoints(current, want)
	result := EndpointSync{Endpoints: endpointURLs(next)}
	if !changed {
		return result, nil
	}

	body, err := jsonBody(next)
	if err != nil {
		return EndpointSync{}, err
	}
	if _, err := c.do(ctx, "openai/config/update", Request{
		Method: "POST", Path: pathOpenAIConfigUpdate, Token: token, Body: body,
	}); err != nil {
		return EndpointSync{}, err
	}
	result.Wrote = true
	return result, nil
}

// endpointURLs is the reportable view of a reconciled document: URLs only, never keys.
func endpointURLs(cfg Config) []string {
	urls := make([]string, len(cfg.Connections))
	for i, conn := range cfg.Connections {
		urls[i] = conn.URL
	}
	return urls
}
