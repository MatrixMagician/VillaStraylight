package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/crushapi"
)

// fakeCrushServer stands in for `crush server`: the routes the bridge drives,
// an SSE stream the test feeds by hand, and a record of what was called.
type fakeCrushServer struct {
	mu sync.Mutex
	// version is what GET /v1/version reports.
	version string
	// events is written to by the test and drained by the SSE handler.
	events chan string
	// created counts workspace creations, so a refusal can be shown to happen
	// BEFORE any workspace exists.
	created  int
	prompts  []string
	grants   []map[string]any
	cancels  int
	srv      *httptest.Server
	streamed chan struct{}
}

func newFakeCrushServer(t *testing.T, version string) *fakeCrushServer {
	t.Helper()
	f := &fakeCrushServer{
		version:  version,
		events:   make(chan string, 8),
		streamed: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"` + f.version + `"}`))
	})
	mux.HandleFunc("POST /v1/workspaces", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.created++
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"id":"ws-1","path":"/workspace"}`))
	})
	mux.HandleFunc("POST /v1/workspaces/{id}/sessions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"sess-1"}`))
	})
	mux.HandleFunc("POST /v1/workspaces/{id}/agent", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.prompts = append(f.prompts, body.Prompt)
		f.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /v1/workspaces/{id}/permissions/grant", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.grants = append(f.grants, body)
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"resolved":true}`))
	})
	mux.HandleFunc("POST /v1/workspaces/{id}/agent/sessions/{sid}/cancel", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.cancels++
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/workspaces/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(f.streamed)
		for {
			select {
			case <-r.Context().Done():
				return
			case ev, ok := <-f.events:
				if !ok {
					return
				}
				_, _ = io.WriteString(w, "data: "+ev+"\n\n")
				w.(http.Flusher).Flush()
			}
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// fakeChild is a started `crush server` that never really ran.
type fakeChild struct {
	stopped chan struct{}
	once    sync.Once
}

func newFakeChild() *fakeChild { return &fakeChild{stopped: make(chan struct{})} }

func (c *fakeChild) Wait() error { <-c.stopped; return nil }
func (c *fakeChild) Stop() error { c.once.Do(func() { close(c.stopped) }); return nil }

// bridgeDepsFor wires the bridge core to a fake server and a fake child.
func bridgeDepsFor(f *fakeCrushServer, in io.Reader, out io.Writer, vetted string) (sandboxBridgeDeps, *fakeChild) {
	child := newFakeChild()
	return sandboxBridgeDeps{
		BaseURL:    f.srv.URL,
		HTTPClient: f.srv.Client(),
		Workspace:  "/workspace",
		Vetted:     func() string { return vetted },
		WriteConfig: func() error {
			return nil
		},
		StartCrush: func(context.Context) (sandboxChild, error) { return child, nil },
		WaitReady:  func(context.Context) error { return nil },
		Stdin:      in,
		Stdout:     out,
		Now:        func() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) },
	}, child
}

// decodeLines parses the bridge's stdout into typed lines.
func decodeLines(t *testing.T, b []byte) []crushapi.Line {
	t.Helper()
	var out []crushapi.Line
	dec := json.NewDecoder(bytes.NewReader(b))
	for {
		var l crushapi.Line
		if err := dec.Decode(&l); err != nil {
			return out
		}
		out = append(out, l)
	}
}

// TestBridgeRefusesADriftedCrushBeforeAnyWorkspaceExists guards the assertion the
// whole sandbox rests on: an unvetted harness must be a refusal, and the refusal
// must arrive before a workspace (and therefore before any task) could run.
func TestBridgeRefusesADriftedCrushBeforeAnyWorkspaceExists(t *testing.T) {
	f := newFakeCrushServer(t, "v0.77.1")
	var out syncBuffer
	d, child := bridgeDepsFor(f, strings.NewReader(""), &out, "v0.76.0")

	err := runSandboxBridge(t.Context(), d)
	if err == nil {
		t.Fatal("a drifted crush version must refuse, non-zero exit")
	}
	if f.created != 0 {
		t.Errorf("created %d workspaces before refusing; want 0", f.created)
	}

	lines := decodeLines(t, out.Bytes())
	if len(lines) != 1 || lines[0].Event == nil {
		t.Fatalf("want exactly one event line, got %q", out.String())
	}
	ev := lines[0].Event
	if ev.Kind != crushapi.KindBridgeError {
		t.Errorf("kind = %q, want %q", ev.Kind, crushapi.KindBridgeError)
	}
	if !strings.Contains(ev.Error, "v0.77.1") || !strings.Contains(ev.Error, "v0.76.0") {
		t.Errorf("refusal names neither version: %q", ev.Error)
	}
	select {
	case <-child.stopped:
	default:
		t.Error("the crush child was left running after a refusal")
	}
}

// TestBridgeRelaysAPromptAndItsEvents guards the happy path: bridge_ready first
// and carrying the asserted version, the prompt reaching the agent route, and
// every event relayed out in order until run_complete ends the run.
func TestBridgeRelaysAPromptAndItsEvents(t *testing.T) {
	f := newFakeCrushServer(t, "v0.76.0")
	var out syncBuffer

	in, inW := io.Pipe()
	d, _ := bridgeDepsFor(f, in, &out, "v0.76.0")

	done := make(chan error, 1)
	go func() { done <- runSandboxBridge(t.Context(), d) }()

	<-f.streamed
	mustWriteLine(t, inW, crushapi.Line{Command: &crushapi.Command{
		Kind: crushapi.CmdPrompt, Prompt: "find duplicates",
	}})
	waitFor(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.prompts) == 1 })

	f.events <- `{"type":"permission_request","payload":{"type":"created","payload":{"id":"perm-1","session_id":"sess-1","tool_call_id":"call-1","tool_name":"edit","action":"write","path":"/workspace/reports/duplicates.md"}}}`
	waitFor(t, func() bool { return bytes.Contains(out.Bytes(), []byte("perm-1")) })

	mustWriteLine(t, inW, crushapi.Line{Command: &crushapi.Command{
		Kind: crushapi.CmdGrant, PermissionID: "perm-1", Answer: crushapi.Allow,
	}})
	waitFor(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.grants) == 1 })

	f.events <- `{"type":"run_complete","payload":{"type":"created","payload":{"session_id":"sess-1","message_id":"msg-1","text":"done"}}}`

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runSandboxBridge: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run_complete did not end the bridge")
	}
	_ = inW.Close()

	lines := decodeLines(t, out.Bytes())
	if len(lines) < 3 {
		t.Fatalf("want at least ready + permission + complete, got %q", out.String())
	}
	if lines[0].Event == nil || lines[0].Event.Kind != crushapi.KindBridgeReady {
		t.Fatalf("first line = %+v, want bridge_ready", lines[0])
	}
	if lines[0].Event.Version != "v0.76.0" {
		t.Errorf("bridge_ready version = %q", lines[0].Event.Version)
	}
	if lines[1].Event == nil || lines[1].Event.Kind != crushapi.KindPermissionRequest {
		t.Fatalf("second line = %+v, want permission_request", lines[1])
	}
	last := lines[len(lines)-1].Event
	if last == nil || last.Kind != crushapi.KindRunComplete {
		t.Fatalf("last line = %+v, want run_complete", lines[len(lines)-1])
	}

	if f.prompts[0] != "find duplicates" {
		t.Errorf("prompt = %q", f.prompts[0])
	}
	if got := f.grants[0]["action"]; got != string(crushapi.Allow) {
		t.Errorf("grant action = %v", got)
	}
}

// TestBridgeCancelsOnCommand guards the cancel path: a cancel line calls Crush's
// cancel route, which is the only endpoint that may end a turn.
func TestBridgeCancelsOnCommand(t *testing.T) {
	f := newFakeCrushServer(t, "v0.76.0")
	var out syncBuffer
	in, inW := io.Pipe()
	d, _ := bridgeDepsFor(f, in, &out, "v0.76.0")

	done := make(chan error, 1)
	go func() { done <- runSandboxBridge(t.Context(), d) }()

	<-f.streamed
	mustWriteLine(t, inW, crushapi.Line{Command: &crushapi.Command{Kind: crushapi.CmdPrompt, Prompt: "go"}})
	waitFor(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.prompts) == 1 })

	mustWriteLine(t, inW, crushapi.Line{Command: &crushapi.Command{Kind: crushapi.CmdCancel}})
	waitFor(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.cancels == 1 })

	_ = inW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runSandboxBridge: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stdin EOF did not end the bridge")
	}
}

// TestBridgeRefusesWhenCrushWillNotStart guards the other pre-task refusal: a
// child that never answers is a bridge_error, not a silent hang.
func TestBridgeRefusesWhenCrushWillNotStart(t *testing.T) {
	f := newFakeCrushServer(t, "v0.76.0")
	var out syncBuffer
	d, _ := bridgeDepsFor(f, strings.NewReader(""), &out, "v0.76.0")
	d.WaitReady = func(context.Context) error { return context.DeadlineExceeded }

	if err := runSandboxBridge(t.Context(), d); err == nil {
		t.Fatal("a crush server that never answers must refuse")
	}
	lines := decodeLines(t, out.Bytes())
	if len(lines) != 1 || lines[0].Event == nil || lines[0].Event.Kind != crushapi.KindBridgeError {
		t.Fatalf("want one bridge_error line, got %q", out.String())
	}
	if f.created != 0 {
		t.Errorf("created %d workspaces; want 0", f.created)
	}
}

func mustWriteLine(t *testing.T, w io.Writer, l crushapi.Line) {
	t.Helper()
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// syncBuffer is a bytes.Buffer the bridge goroutine writes while the test reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.b.Bytes()...)
}

func (s *syncBuffer) String() string { return string(s.Bytes()) }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not reached within 5s")
}
