// Package crushapi is villa's client for Crush v0.76.0's `crush server` HTTP+SSE
// surface, plus the typed stdio line protocol the bridge and the runner share.
//
// Every route and body was derived from the Crush source at tag v0.76.0:
//
//   - the route table: internal/server/server.go (mux.HandleFunc lines 120-179)
//   - the handlers and their status codes: internal/server/proto.go
//     (handleGetVersion, handlePostWorkspaces, handleGetWorkspaceEvents,
//     handlePostWorkspaceAgent, handlePostWorkspacePermissionsGrant,
//     handlePostWorkspaceAgentSessionCancel,
//     handleGetWorkspaceSessionFileTrackerFiles)
//   - the bodies: internal/proto/proto.go (Workspace, AgentMessage,
//     PermissionGrant, PermissionGrantResponse), internal/proto/version.go
//     (VersionInfo), internal/proto/permission.go (PermissionRequest)
//   - the /v1 prefix and the client_id query parameter: internal/client/client.go
//     and internal/client/proto.go (SubscribeEvents), matching the server's
//     requireClientID, which rejects anything that is not a UUID
//   - the grant semantics: internal/backend/permission.go +
//     internal/permission/permission.go — resolve() takes the pending request by
//     ID, but GrantPersistent keys the session-wide entry by
//     session/tool/action/path. A grant body carrying only an id would therefore
//     resolve the prompt and silently forget the "allow for the rest of this
//     task" half, so this client caches each pending request off the stream and
//     sends it back whole.
//
// The client is a thin, honest read of that surface: no retries, no reconnects,
// no polling. A dropped stream is an error the runner sees, not a gap it never
// hears about.
package crushapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// WorkspaceID and SessionID are Crush's two opaque handles. They are named types
// so a caller cannot pass one where the other belongs.
type (
	WorkspaceID string
	SessionID   string
)

// Client talks to one `crush server`.
type Client struct {
	base string
	hc   *http.Client
	// clientID is the UUID the server requires on the events route and records
	// as the workspace's owning client.
	clientID string
	// DataDir, when set, is sent as the workspace's data_dir. Crush resolves a
	// workspace's data directory from the CREATE body (internal/backend
	// CreateWorkspace -> config.Init(path, dataDir)), NOT from CRUSH_GLOBAL_DATA
	// or the server's --data-dir, so leaving it empty puts a .crush/ database
	// inside the operator's workspace grant. Measured on hardware.
	DataDir string
	// now is the receipt clock, seamed so a test can freeze it.
	now func() time.Time

	mu sync.Mutex
	// pending holds every unresolved permission request seen on the stream, so
	// Grant can send the whole body back. An entry is dropped when granted.
	pending map[string]PermissionRequest
}

// New returns a client for a `crush server` reachable at baseURL. hc may carry a
// DialContext for a guest-local unix socket, in which case baseURL is any host
// the transport ignores. A nil hc means http.DefaultClient.
func New(baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{
		base:     strings.TrimSuffix(baseURL, "/"),
		hc:       hc,
		clientID: newUUID(),
		now:      time.Now,
		pending:  map[string]PermissionRequest{},
	}
}

// newUUID returns a random RFC 4122 version-4 UUID. Crush's requireClientID
// parses it, so any other string is a 400. Generated from crypto/rand rather
// than by adding a dependency for sixteen bytes.
func newUUID() string {
	var b [16]byte
	// crypto/rand.Read never returns an error as of Go 1.24; it panics instead.
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Version returns the running Crush's version string, for the bridge's assertion
// against the pin.
func (c *Client) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/version", nil, &v); err != nil {
		return "", err
	}
	return v.Version, nil
}

// CreateWorkspace registers cwd as a Crush workspace and returns its id.
func (c *Client) CreateWorkspace(ctx context.Context, cwd string) (WorkspaceID, error) {
	body := map[string]string{"path": cwd, "client_id": c.clientID}
	if c.DataDir != "" {
		body["data_dir"] = c.DataDir
	}
	var ws struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/workspaces", body, &ws); err != nil {
		return "", err
	}
	if ws.ID == "" {
		return "", fmt.Errorf("crushapi: workspace create returned no id")
	}
	return WorkspaceID(ws.ID), nil
}

// SendPrompt opens a session and submits the instruction, returning the session
// id the run's events will carry. The agent route answers 202: the run is
// detached from this request, so completion arrives on the stream and nowhere
// else.
func (c *Client) SendPrompt(ctx context.Context, ws WorkspaceID, prompt string) (SessionID, error) {
	var sess struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/workspaces/"+string(ws)+"/sessions",
		map[string]string{"title": "villa workspace task"}, &sess); err != nil {
		return "", err
	}
	if sess.ID == "" {
		return "", fmt.Errorf("crushapi: session create returned no id")
	}
	body := map[string]string{"session_id": sess.ID, "prompt": prompt}
	if err := c.do(ctx, http.MethodPost, "/v1/workspaces/"+string(ws)+"/agent", body, nil); err != nil {
		return "", err
	}
	return SessionID(sess.ID), nil
}

// Grant answers one pending permission request. It refuses an id the stream never
// carried rather than inventing a body: a fabricated tool/action/path would
// resolve the prompt while recording the wrong session-wide grant.
func (c *Client) Grant(ctx context.Context, ws WorkspaceID, permissionID string, answer GrantAnswer) error {
	c.mu.Lock()
	req, ok := c.pending[permissionID]
	c.mu.Unlock()
	if !ok {
		return fmt.Errorf("crushapi: no pending permission %q", permissionID)
	}

	body := map[string]any{
		"permission": map[string]any{
			"id":           req.ID,
			"session_id":   req.SessionID,
			"tool_call_id": req.ToolCallID,
			"tool_name":    req.Tool,
			"description":  req.Description,
			"action":       req.Action,
			"path":         req.Path,
			"params":       req.Params,
		},
		"action": string(answer),
	}
	if err := c.do(ctx, http.MethodPost, "/v1/workspaces/"+string(ws)+"/permissions/grant", body, nil); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.pending, permissionID)
	c.mu.Unlock()
	return nil
}

// Cancel ends an in-flight run. It is the only endpoint that may end a turn;
// dropping the events connection does not (internal/server/proto.go,
// handlePostWorkspaceAgent).
func (c *Client) Cancel(ctx context.Context, ws WorkspaceID, sid SessionID) error {
	return c.do(ctx, http.MethodPost,
		"/v1/workspaces/"+string(ws)+"/agent/sessions/"+string(sid)+"/cancel", nil, nil)
}

// FilesRead lists the files the session read, from Crush's file tracker. It is
// what the grounding audit's source set is drawn from.
func (c *Client) FilesRead(ctx context.Context, ws WorkspaceID, sid SessionID) ([]string, error) {
	var files []string
	err := c.do(ctx, http.MethodGet,
		"/v1/workspaces/"+string(ws)+"/sessions/"+string(sid)+"/filetracker/files", nil, &files)
	return files, err
}

// Events subscribes to the workspace's SSE stream. The event channel closes when
// the stream ends; the error channel then yields the reason (nil on a clean end)
// exactly once. Every permission request seen is cached so Grant can answer it.
func (c *Client) Events(ctx context.Context, ws WorkspaceID) (<-chan Event, <-chan error) {
	events := make(chan Event, 64)
	errs := make(chan error, 1)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base+"/v1/workspaces/"+string(ws)+"/events?client_id="+c.clientID, nil)
	if err != nil {
		close(events)
		errs <- err
		return events, errs
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	rsp, err := c.hc.Do(req)
	if err != nil {
		close(events)
		errs <- fmt.Errorf("crushapi: subscribe: %w", err)
		return events, errs
	}
	if rsp.StatusCode != http.StatusOK {
		rsp.Body.Close()
		close(events)
		errs <- fmt.Errorf("crushapi: subscribe: status %d", rsp.StatusCode)
		return events, errs
	}

	go func() {
		defer close(events)
		defer rsp.Body.Close()
		errs <- c.stream(ctx, rsp.Body, events)
	}()
	return events, errs
}

// stream reads `data: <json>` frames until EOF. A frame that will not parse is
// skipped rather than fatal: one malformed event must not silence the rest of a
// task's narration.
func (c *Client) stream(ctx context.Context, body io.Reader, out chan<- Event) error {
	br := bufio.NewReader(body)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if data, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:")); ok {
				if ev, derr := decodeEvent(bytes.TrimSpace(data), c.now()); derr == nil {
					if ev.Permission != nil {
						c.mu.Lock()
						c.pending[ev.Permission.ID] = *ev.Permission
						c.mu.Unlock()
					}
					select {
					case out <- ev:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

// do performs one JSON request. A non-2xx is an error carrying the server's
// message when it sent one: fail closed, never a zero value that reads as success.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("crushapi: marshal %s: %w", path, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return fmt.Errorf("crushapi: %s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rsp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("crushapi: %s %s: %w", method, path, err)
	}
	defer rsp.Body.Close()

	if rsp.StatusCode < 200 || rsp.StatusCode > 299 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(rsp.Body).Decode(&e)
		if e.Message != "" {
			return fmt.Errorf("crushapi: %s %s: status %d: %s", method, path, rsp.StatusCode, e.Message)
		}
		return fmt.Errorf("crushapi: %s %s: status %d", method, path, rsp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(rsp.Body).Decode(out); err != nil {
		return fmt.Errorf("crushapi: decode %s: %w", path, err)
	}
	return nil
}
