package crushapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// fixtureServer stands in for `crush server`: it serves the recorded SSE stream on
// the events route and records every other request so the wire shapes can be
// asserted.
type fixtureServer struct {
	t *testing.T
	// lastBody is the decoded body of the most recent POST, by route path.
	lastBody map[string]map[string]any
	// paths is every path requested, in order.
	paths []string
}

func newFixtureServer(t *testing.T) (*fixtureServer, *httptest.Server) {
	t.Helper()
	f := &fixtureServer{t: t, lastBody: map[string]map[string]any{}}
	mux := http.NewServeMux()
	record := func(r *http.Request) {
		f.paths = append(f.paths, r.URL.Path)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			f.lastBody[r.URL.Path] = body
		}
	}
	mux.HandleFunc("GET /v1/version", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = w.Write([]byte(`{"version":"v0.76.0","commit":"abc","go_version":"go1.26.2","platform":"linux/amd64"}`))
	})
	mux.HandleFunc("POST /v1/workspaces", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = w.Write([]byte(`{"id":"ws-1","path":"/workspace"}`))
	})
	mux.HandleFunc("POST /v1/workspaces/{id}/sessions", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = w.Write([]byte(`{"id":"sess-1","title":"t"}`))
	})
	mux.HandleFunc("POST /v1/workspaces/{id}/agent", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /v1/workspaces/{id}/permissions/grant", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = w.Write([]byte(`{"resolved":true}`))
	})
	mux.HandleFunc("POST /v1/workspaces/{id}/agent/sessions/{sid}/cancel", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/workspaces/{id}/sessions/{sid}/filetracker/files", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = w.Write([]byte(`["/workspace/a.md","/workspace/b.md"]`))
	})
	mux.HandleFunc("GET /v1/workspaces/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		f.paths = append(f.paths, r.URL.Path+"?client_id="+r.URL.Query().Get("client_id"))
		b, err := os.ReadFile("testdata/events.sse")
		if err != nil {
			f.t.Fatalf("read fixture: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv
}

// TestVersionParsesThePinnedRelease guards the assertion the bridge makes before
// any task runs: a drifted Crush must be visible as a version string, not as a
// silent success.
func TestVersionParsesThePinnedRelease(t *testing.T) {
	_, srv := newFixtureServer(t)
	got, err := New(srv.URL, srv.Client()).Version(t.Context())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if got != "v0.76.0" {
		t.Fatalf("version = %q, want v0.76.0", got)
	}
}

// TestEventsDecodesEveryKindFromTheFixture guards the SSE contract: every event
// kind the spec names decodes into its typed field, an unrecognised type still
// reaches the caller with its payload in Raw, and the stream closes cleanly.
func TestEventsDecodesEveryKindFromTheFixture(t *testing.T) {
	_, srv := newFixtureServer(t)
	c := New(srv.URL, srv.Client())

	evs, errs := c.Events(t.Context(), "ws-1")
	var got []Event
	for ev := range evs {
		got = append(got, ev)
	}
	if err := <-errs; err != nil {
		t.Fatalf("Events: %v", err)
	}

	wantKinds := []EventKind{
		KindPermissionRequest, KindPermissionNotification, KindMessage,
		KindFile, KindAgentEvent, "session", KindRunComplete,
	}
	if len(got) != len(wantKinds) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(wantKinds), got)
	}
	for i, want := range wantKinds {
		if got[i].Kind != want {
			t.Errorf("event %d kind = %q, want %q", i, got[i].Kind, want)
		}
		if len(got[i].Raw) == 0 {
			t.Errorf("event %d has no Raw payload", i)
		}
	}

	perm := got[0].Permission
	if perm == nil {
		t.Fatal("permission_request carried no Permission")
	}
	if perm.ID != "perm-1" || perm.Tool != "edit" || perm.Action != "write" ||
		perm.Path != "/workspace/reports/duplicates.md" || perm.SessionID != "sess-1" {
		t.Errorf("permission = %+v", *perm)
	}
	if string(perm.Params) == "" {
		t.Error("permission params not preserved")
	}
	if got[0].ID != "perm-1" {
		t.Errorf("event id = %q, want perm-1", got[0].ID)
	}

	msg := got[2].Message
	if msg == nil {
		t.Fatal("message carried no Message")
	}
	if msg.Text != "Listing the folder. Done." {
		t.Errorf("message text = %q", msg.Text)
	}
	if msg.Role != "assistant" || msg.SessionID != "sess-1" {
		t.Errorf("message = %+v", *msg)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Name != "edit" {
		t.Errorf("tool calls = %+v", msg.ToolCalls)
	}

	file := got[3].File
	if file == nil || file.Path != "/workspace/reports/duplicates.md" {
		t.Errorf("file = %+v", file)
	}

	rc := got[6].RunComplete
	if rc == nil {
		t.Fatal("run_complete carried no RunComplete")
	}
	if rc.SessionID != "sess-1" || rc.Text != "Wrote reports/duplicates.md." || rc.Cancelled {
		t.Errorf("run complete = %+v", *rc)
	}

	if got[5].Message != nil || got[5].Permission != nil {
		t.Error("an unrecognised event type was decoded into a typed field")
	}
}

// TestGrantSendsTheWholePermission guards the grant body: Crush resolves the
// pending request by id but records a session-wide grant by tool/action/path, so
// a body carrying only the id would make `allow_session` silently forget itself.
func TestGrantSendsTheWholePermission(t *testing.T) {
	f, srv := newFixtureServer(t)
	c := New(srv.URL, srv.Client())

	evs, _ := c.Events(t.Context(), "ws-1")
	for range evs { //nolint:revive // drain so the client caches the pending request
	}

	if err := c.Grant(t.Context(), "ws-1", "perm-1", AllowSession); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	body := f.lastBody["/v1/workspaces/ws-1/permissions/grant"]
	if body["action"] != string(AllowSession) {
		t.Errorf("action = %v, want %s", body["action"], AllowSession)
	}
	perm, _ := body["permission"].(map[string]any)
	for k, want := range map[string]string{
		"id": "perm-1", "tool_name": "edit", "action": "write",
		"path": "/workspace/reports/duplicates.md", "tool_call_id": "call-1",
		"session_id": "sess-1",
	} {
		if perm[k] != want {
			t.Errorf("permission.%s = %v, want %q", k, perm[k], want)
		}
	}
}

// TestGrantRefusesAnUnknownPermission guards the fail-closed rule: villa never
// invents a permission body it did not see on the stream.
func TestGrantRefusesAnUnknownPermission(t *testing.T) {
	_, srv := newFixtureServer(t)
	if err := New(srv.URL, srv.Client()).Grant(t.Context(), "ws-1", "nope", Allow); err == nil {
		t.Fatal("granting an unseen permission id should refuse")
	}
}

// TestPromptCancelAndFilesHitTheDocumentedRoutes guards the route strings against
// a Crush upgrade that moves them.
func TestPromptCancelAndFilesHitTheDocumentedRoutes(t *testing.T) {
	f, srv := newFixtureServer(t)
	c := New(srv.URL, srv.Client())

	ws, err := c.CreateWorkspace(t.Context(), "/workspace")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if ws != "ws-1" {
		t.Fatalf("workspace = %q", ws)
	}
	if got := f.lastBody["/v1/workspaces"]["path"]; got != "/workspace" {
		t.Errorf("workspace path = %v", got)
	}
	if got, _ := f.lastBody["/v1/workspaces"]["client_id"].(string); len(got) != 36 {
		t.Errorf("client_id = %q, want a uuid", got)
	}

	sid, err := c.SendPrompt(t.Context(), ws, "find duplicates")
	if err != nil {
		t.Fatalf("SendPrompt: %v", err)
	}
	if sid != "sess-1" {
		t.Fatalf("session = %q", sid)
	}
	if got := f.lastBody["/v1/workspaces/ws-1/agent"]["prompt"]; got != "find duplicates" {
		t.Errorf("prompt = %v", got)
	}
	if got := f.lastBody["/v1/workspaces/ws-1/agent"]["session_id"]; got != "sess-1" {
		t.Errorf("session_id = %v", got)
	}

	if err := c.Cancel(t.Context(), ws, sid); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	files, err := c.FilesRead(t.Context(), ws, sid)
	if err != nil {
		t.Fatalf("FilesRead: %v", err)
	}
	if len(files) != 2 || files[0] != "/workspace/a.md" {
		t.Errorf("files = %v", files)
	}

	want := []string{
		"/v1/workspaces",
		"/v1/workspaces/ws-1/sessions",
		"/v1/workspaces/ws-1/agent",
		"/v1/workspaces/ws-1/agent/sessions/sess-1/cancel",
		"/v1/workspaces/ws-1/sessions/sess-1/filetracker/files",
	}
	for i, w := range want {
		if i >= len(f.paths) || f.paths[i] != w {
			t.Errorf("request %d = %q, want %q", i, f.paths[i], w)
		}
	}
}

// TestNonOKStatusIsAnError guards fail-closed: a refused create is never a silent
// empty workspace id.
func TestNonOKStatusIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"nope"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, srv.Client())
	if _, err := c.CreateWorkspace(t.Context(), "/workspace"); err == nil {
		t.Fatal("a 500 should be an error")
	}
	if _, err := c.Version(t.Context()); err == nil {
		t.Fatal("a 500 should be an error")
	}
}
