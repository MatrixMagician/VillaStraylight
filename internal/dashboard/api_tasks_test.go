package dashboard

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/crushapi"
	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
	"github.com/MatrixMagician/VillaStraylight/internal/taskrun"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
)

var update = flag.Bool("update", false, "regenerate golden files")

// fakeBridge is a scripted sandbox for the route tests, a copy of the one in
// internal/taskrun's tests: the test feeds events, commands are recorded.
type fakeBridge struct {
	events chan crushapi.Event
	mu     sync.Mutex
	sent   []crushapi.Command
}

func newFakeBridge() *fakeBridge { return &fakeBridge{events: make(chan crushapi.Event, 64)} }

func (f *fakeBridge) Events() <-chan crushapi.Event { return f.events }
func (f *fakeBridge) Send(c crushapi.Command) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, c)
	return nil
}
func (f *fakeBridge) Wait() error { return nil }
func (f *fakeBridge) Kill() error { close(f.events); return nil }
func (f *fakeBridge) commands() []crushapi.Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]crushapi.Command(nil), f.sent...)
}

// taskFixture is a real runner over a real store on t.TempDir(), one scripted
// bridge per launch.
type taskFixture struct {
	ws      string
	store   taskstore.Store
	bridges chan *fakeBridge
	toolsOn bool
}

func newTaskFixture(t *testing.T) *taskFixture {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o700); err != nil {
		t.Fatal(err)
	}
	return &taskFixture{ws: ws, store: taskstore.New(filepath.Join(root, "data")), bridges: make(chan *fakeBridge, 4), toolsOn: true}
}

// newTestRunner builds a runner over fx (a fresh fixture when nil). It is also
// what the PRIV-01 test in server_test.go passes as Config.Tasks.
func newTestRunner(t *testing.T, fx *taskFixture) *taskrun.Runner {
	t.Helper()
	if fx == nil {
		fx = newTaskFixture(t)
	}
	var mu sync.Mutex
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	return taskrun.New(taskrun.Deps{
		Store:      fx.store,
		LoadConfig: func() (config.VillaConfig, error) { return config.VillaConfig{}, nil },
		Launch: func(context.Context, []string) (taskrun.Bridge, error) {
			select {
			case b := <-fx.bridges:
				return b, nil
			default:
				return nil, errors.New("no scripted bridge")
			}
		},
		KillByName: func(string) error { return nil },
		RenderArgs: func(_ config.VillaConfig, ws, id string) ([]string, error) { return []string{"run", ws, id}, nil },
		Registered: func(_ config.VillaConfig, path string) (string, bool) { return path, path == fx.ws },
		ToolsOn:    func(config.VillaConfig) bool { return fx.toolsOn },
		SandboxReady: func() (bool, string) {
			return true, ""
		},
		Audit: func(_ context.Context, doc grounding.Document, _ []grounding.Source) grounding.DocumentReport {
			return grounding.DocumentReport{Path: doc.Path, Checked: true, Claims: 1}
		},
		ReadFile: func(ws, rel string) ([]byte, error) { return os.ReadFile(filepath.Join(ws, rel)) },
		Now: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			now = now.Add(time.Second)
			return now
		},
		Rand: bytes.NewReader(bytes.Repeat([]byte{0x4f, 0x2a}, 64)),
	})
}

func taskServer(t *testing.T, r *taskrun.Runner) http.Handler {
	t.Helper()
	return mustNewServer(t, Config{StatusDeps: stubStatusDeps(t), ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888, Tasks: r}).Handler()
}

// do issues one request through the handler; a POST carries the headers the
// same-origin guard requires.
func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeTask(t *testing.T, rec *httptest.ResponseRecorder) taskstore.Task {
	t.Helper()
	var task taskstore.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatalf("decode task: %v\n%s", err, rec.Body.String())
	}
	return task
}

func submitBody(ws string) string {
	b, _ := json.Marshal(map[string]string{"workspace": ws, "instruction": "write the memo", "mode": "ask"})
	return string(b)
}

func waitState(t *testing.T, store taskstore.Store, id string, want taskstore.State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if task, err := store.Load(id); err == nil && task.State == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("task %s never reached %q", id, want)
}

var taskRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/tasks"},
	{http.MethodPost, "/api/tasks"},
	{http.MethodGet, "/api/tasks/x"},
	{http.MethodGet, "/api/tasks/x/events"},
	{http.MethodPost, "/api/tasks/x/approve"},
	{http.MethodPost, "/api/tasks/x/deny"},
	{http.MethodPost, "/api/tasks/x/cancel"},
}

// TestRoutesGolden freezes the whole API route table: a route added, dropped
// or renamed changes the golden and must be a deliberate refreeze.
func TestRoutesGolden(t *testing.T) {
	srv := mustNewServer(t, Config{StatusDeps: stubStatusDeps(t), ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888})
	var lines []string
	for _, rt := range srv.apiRoutes() {
		lines = append(lines, rt.method+" "+rt.pattern)
	}
	sort.Strings(lines)
	got := []byte(strings.Join(lines, "\n") + "\n")
	path := filepath.Join("testdata", "routes.golden")
	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("route table drifted from %s:\n got:\n%s\nwant:\n%s", path, got, want)
	}
}

// TestTaskRoutesAnswer503WhenNotEnabled guards the nil-runner posture: a
// dashboard without the workspace agent never accepts a task on any route.
func TestTaskRoutesAnswer503WhenNotEnabled(t *testing.T) {
	h := taskServer(t, nil)
	for _, rt := range taskRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := do(h, rt.method, rt.path, "{}")
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("code = %d, want 503; body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"error":"workspace agent is not enabled"`) {
				t.Errorf("body = %s", rec.Body.String())
			}
		})
	}
}

// TestTaskMutationsSitBehindTheGuard guards that requireSameOrigin covers the
// new routes: a bare POST is refused before any handler runs.
func TestTaskMutationsSitBehindTheGuard(t *testing.T) {
	h := taskServer(t, newTestRunner(t, nil))
	for _, path := range []string{"/api/tasks", "/api/tasks/x/approve", "/api/tasks/x/deny", "/api/tasks/x/cancel"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s without same-origin headers = %d, want 403", path, rec.Code)
		}
	}
}

// TestSubmitAnswers202ForQueuedAndRefused guards the CLI contract: both a
// queued and a refused submission are 202 with a record, since the exit-code
// table works from the record alone; only a malformed body is a 400.
func TestSubmitAnswers202ForQueuedAndRefused(t *testing.T) {
	fx := newTaskFixture(t)
	fx.bridges <- newFakeBridge()
	h := taskServer(t, newTestRunner(t, fx))

	rec := do(h, http.MethodPost, "/api/tasks", submitBody(fx.ws))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d; body=%s", rec.Code, rec.Body.String())
	}
	if task := decodeTask(t, rec); task.State != taskstore.Queued {
		t.Errorf("state = %q, want queued", task.State)
	}

	fx.toolsOn = false
	rec = do(h, http.MethodPost, "/api/tasks", submitBody(fx.ws))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("refusal code = %d; body=%s", rec.Code, rec.Body.String())
	}
	if task := decodeTask(t, rec); task.State != taskstore.Refused || task.Exit == nil || *task.Exit != 1 {
		t.Errorf("refused record = %+v", task)
	}

	for _, body := range []string{`{"workspace":`, `{"nope":1}`, `{"workspace":"/w","instruction":"x","mode":"yolo"}`} {
		if rec := do(h, http.MethodPost, "/api/tasks", body); rec.Code != http.StatusBadRequest {
			t.Errorf("body %q = %d, want 400", body, rec.Code)
		}
	}
}

// TestListAndShow guards the two read routes: the list is the frozen
// ListView shape, show is the record, and an unknown id is 404.
func TestListAndShow(t *testing.T) {
	fx := newTaskFixture(t)
	fx.toolsOn = false
	h := taskServer(t, newTestRunner(t, fx))
	id := decodeTask(t, do(h, http.MethodPost, "/api/tasks", submitBody(fx.ws))).ID

	rec := do(h, http.MethodGet, "/api/tasks", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list code = %d", rec.Code)
	}
	var view taskstore.ListView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if view.Schema != 1 || len(view.Tasks) != 1 || view.Tasks[0].ID != id || view.Tasks[0].State != taskstore.Refused {
		t.Errorf("list = %+v", view)
	}

	rec = do(h, http.MethodGet, "/api/tasks/"+id, "")
	if rec.Code != http.StatusOK || decodeTask(t, rec).ID != id {
		t.Errorf("show = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/api/tasks/nope", ""); rec.Code != http.StatusNotFound {
		t.Errorf("show unknown = %d, want 404", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/tasks/nope/events", ""); rec.Code != http.StatusNotFound {
		t.Errorf("events unknown = %d, want 404", rec.Code)
	}
}

// TestApproveDenyCancelStatusCodes guards the verb routes: 409 when the task
// is not awaiting, 200 with the record once it is (and the grant reaches the
// bridge), 404 on an unknown id.
func TestApproveDenyCancelStatusCodes(t *testing.T) {
	fx := newTaskFixture(t)
	b := newFakeBridge()
	fx.bridges <- b
	h := taskServer(t, newTestRunner(t, fx))
	id := decodeTask(t, do(h, http.MethodPost, "/api/tasks", submitBody(fx.ws))).ID

	for _, verb := range []string{"approve", "deny"} {
		if rec := do(h, http.MethodPost, "/api/tasks/nope/"+verb, "{}"); rec.Code != http.StatusNotFound {
			t.Errorf("%s unknown = %d, want 404", verb, rec.Code)
		}
	}
	b.events <- crushapi.Event{Kind: crushapi.KindBridgeReady, Version: "0.76.0"}
	waitState(t, fx.store, id, taskstore.Running)
	if rec := do(h, http.MethodPost, "/api/tasks/"+id+"/approve", "{}"); rec.Code != http.StatusConflict {
		t.Errorf("approve while running = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/api/tasks/"+id+"/deny", ""); rec.Code != http.StatusConflict {
		t.Errorf("deny while running = %d, want 409", rec.Code)
	}

	b.events <- crushapi.Event{Kind: crushapi.KindPermissionRequest, ID: "p1", Permission: &crushapi.PermissionRequest{ID: "p1", Tool: "edit", Action: "write", Path: "/workspace/memo.md"}}
	waitState(t, fx.store, id, taskstore.AwaitingApproval)
	rec := do(h, http.MethodPost, "/api/tasks/"+id+"/approve", `{"all":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve = %d; body=%s", rec.Code, rec.Body.String())
	}
	if decodeTask(t, rec).ID != id {
		t.Errorf("approve body = %s", rec.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(b.commands()) < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	cmds := b.commands()
	if len(cmds) != 2 || cmds[1].Kind != crushapi.CmdGrant || cmds[1].PermissionID != "p1" || cmds[1].Answer != crushapi.Allow {
		t.Errorf("bridge commands = %+v", cmds)
	}
	waitState(t, fx.store, id, taskstore.Running)

	if rec := do(h, http.MethodPost, "/api/tasks/"+id+"/cancel", ""); rec.Code != http.StatusOK {
		t.Errorf("cancel running = %d; body=%s", rec.Code, rec.Body.String())
	}
	waitState(t, fx.store, id, taskstore.Cancelled)
	if rec := do(h, http.MethodPost, "/api/tasks/"+id+"/cancel", ""); rec.Code != http.StatusConflict {
		t.Errorf("cancel terminal = %d, want 409", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/api/tasks/nope/cancel", ""); rec.Code != http.StatusNotFound {
		t.Errorf("cancel unknown = %d, want 404", rec.Code)
	}
}

// TestCancelQueued guards the drop path over HTTP: a queued task cancels with
// 200 and a second cancel is 409.
func TestCancelQueued(t *testing.T) {
	fx := newTaskFixture(t)
	first := newFakeBridge()
	fx.bridges <- first
	h := taskServer(t, newTestRunner(t, fx))
	running := decodeTask(t, do(h, http.MethodPost, "/api/tasks", submitBody(fx.ws))).ID
	first.events <- crushapi.Event{Kind: crushapi.KindBridgeReady}
	waitState(t, fx.store, running, taskstore.Running)
	queued := decodeTask(t, do(h, http.MethodPost, "/api/tasks", submitBody(fx.ws))).ID

	rec := do(h, http.MethodPost, "/api/tasks/"+queued+"/cancel", "")
	if rec.Code != http.StatusOK || decodeTask(t, rec).State != taskstore.Cancelled {
		t.Fatalf("cancel queued = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/api/tasks/"+queued+"/cancel", ""); rec.Code != http.StatusConflict {
		t.Errorf("second cancel = %d, want 409", rec.Code)
	}
}

// sseFrame is one parsed `event:`/`data:` pair.
type sseFrame struct {
	event string
	data  taskrun.Narration
}

// readSSE parses frames until EOF, which is the proof the server closed the
// stream on its own.
func readSSE(t *testing.T, body io.Reader) []sseFrame {
	t.Helper()
	var frames []sseFrame
	var cur sseFrame
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cur.data); err != nil {
				t.Fatalf("decode data frame: %v: %s", err, line)
			}
		case line == "":
			if cur.event != "" {
				frames = append(frames, cur)
			}
			cur = sseFrame{}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read stream: %v", err)
	}
	return frames
}

// TestEventsStreamsNarrationUntilTerminal guards the SSE contract: every
// frame is `event: <Kind>` + `data: <Narration>`, the running state is seen,
// the last frame carries the terminal record, and the stream ends by itself.
func TestEventsStreamsNarrationUntilTerminal(t *testing.T) {
	fx := newTaskFixture(t)
	b := newFakeBridge()
	fx.bridges <- b
	if err := os.WriteFile(filepath.Join(fx.ws, "memo.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := taskServer(t, newTestRunner(t, fx))
	ts := httptest.NewServer(h)
	defer ts.Close()

	id := decodeTask(t, do(h, http.MethodPost, "/api/tasks", submitBody(fx.ws))).ID
	rsp, err := http.Get(ts.URL + "/api/tasks/" + id + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK || rsp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d, content-type %q", rsp.StatusCode, rsp.Header.Get("Content-Type"))
	}

	b.events <- crushapi.Event{Kind: crushapi.KindBridgeReady, Version: "0.76.0"}
	b.events <- crushapi.Event{Kind: crushapi.KindFile, File: &crushapi.FileEvent{Path: "/workspace/memo.md"}}
	b.events <- crushapi.Event{Kind: crushapi.KindRunComplete, RunComplete: &crushapi.RunComplete{SessionID: "s"}}
	b.events <- crushapi.Event{Kind: crushapi.KindFilesRead}
	close(b.events)

	frames := readSSE(t, rsp.Body)
	if len(frames) == 0 {
		t.Fatal("no frames")
	}
	sawRunning := false
	for _, f := range frames {
		if f.data.Task == nil {
			t.Fatalf("frame without a task snapshot: %+v", f)
		}
		if f.event == taskrun.KindState && f.data.Task.State == taskstore.Running {
			sawRunning = true
		}
	}
	if !sawRunning {
		t.Errorf("no state frame with running: %+v", frames)
	}
	last := frames[len(frames)-1]
	if last.event != taskrun.KindState || last.data.Task.State != taskstore.Done {
		t.Errorf("last frame = %s %+v, want state/done", last.event, last.data.Task)
	}
}
