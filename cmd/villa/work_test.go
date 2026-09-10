package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
	"github.com/MatrixMagician/VillaStraylight/internal/workspace"
)

// answerCall is one approve/deny/cancel POST the fake API recorded.
type answerCall struct {
	verb string
	id   string
	body string
}

// fakeTaskAPI stands in for villa-dashboard.service's loopback task API: the
// seven routes of spec §5 over a scripted SSE stream, recording every request
// so a refusal test can assert that NO submission was made.
type fakeTaskAPI struct {
	srv     *httptest.Server
	frames  []string
	record  taskstore.Task
	listRaw []byte
	showRaw []byte
	status  int

	submits []map[string]string
	answers []answerCall
}

func newFakeTaskAPI(t *testing.T) *fakeTaskAPI {
	t.Helper()
	f := &fakeTaskAPI{}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.submits = append(f.submits, body)
		writeFakeJSON(w, http.StatusAccepted, f.record)
	})
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if f.status != 0 {
			w.WriteHeader(f.status)
		}
		_, _ = w.Write(f.listRaw)
	})
	mux.HandleFunc("GET /api/tasks/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"error":"taskstore: task not found"}`))
			return
		}
		if f.showRaw != nil {
			_, _ = w.Write(f.showRaw)
			return
		}
		_ = json.NewEncoder(w).Encode(f.record)
	})
	mux.HandleFunc("GET /api/tasks/{id}/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, frame := range f.frames {
			_, _ = w.Write([]byte(frame))
			if flusher != nil {
				flusher.Flush()
			}
		}
	})
	for _, verb := range []string{"approve", "deny", "cancel"} {
		mux.HandleFunc("POST /api/tasks/{id}/"+verb, func(w http.ResponseWriter, r *http.Request) {
			body := make([]byte, 256)
			n, _ := r.Body.Read(body)
			f.answers = append(f.answers, answerCall{verb: verb, id: r.PathValue("id"), body: string(body[:n])})
			writeFakeJSON(w, http.StatusOK, f.record)
		})
	}

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func writeFakeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// stateFrame renders one `event: state` SSE frame carrying t as its snapshot.
func stateFrame(t taskstore.Task) string {
	data, _ := json.Marshal(workEvent{Kind: "state", Text: string(t.State), Task: &t})
	return "event: state\ndata: " + string(data) + "\n\n"
}

// narrationFrame renders one `event: narration` SSE frame.
func narrationFrame(text string, t taskstore.Task) string {
	data, _ := json.Marshal(workEvent{Kind: "narration", Text: text, Task: &t})
	return "event: narration\ndata: " + string(data) + "\n\n"
}

// workFixture is a registered workspace plus the deps villa work runs against.
type workFixture struct {
	deps      *workDeps
	api       *fakeTaskAPI
	workspace string
	answers   []string
}

func newWorkFixture(t *testing.T, cfgMut func(*config.VillaConfig)) *workFixture {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "reports")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	cfg := config.VillaConfig{ToolsMode: true, Workspace: []string{resolved}}
	if cfgMut != nil {
		cfgMut(&cfg)
	}

	f := newFakeTaskAPI(t)
	fx := &workFixture{api: f, workspace: dir}
	fx.deps = &workDeps{
		load: func() (config.VillaConfig, error) { return cfg, nil },
		wd: workspace.Deps{
			Home:         func() (string, error) { return home, nil },
			ConfigRoot:   func() string { return filepath.Join(home, ".config", "villa") },
			DataRoot:     func() string { return filepath.Join(home, ".local", "share", "villa") },
			Stat:         os.Stat,
			EvalSymlinks: filepath.EvalSymlinks,
		},
		api: func(config.VillaConfig) taskAPIDeps {
			return taskAPIDeps{base: f.srv.URL, client: f.srv.Client()}
		},
		prompt: func() (string, error) {
			if len(fx.answers) == 0 {
				return "", nil
			}
			line := fx.answers[0]
			fx.answers = fx.answers[1:]
			return line, nil
		},
		readLog: func(string) ([]taskstore.LogEvent, error) { return nil, nil },
	}
	return fx
}

func terminalRecord(state taskstore.State) taskstore.Task {
	exit := taskstore.Exit(state)
	return taskstore.Task{
		ID: "20260910-120115-4f2a", Workspace: "/home/dev/reports", Instruction: "tidy",
		Mode: "ask", State: state, Exit: &exit, SubmittedAt: "2026-09-10T12:01:15Z", Schema: 1,
	}
}

// TestRunWorkExitCodeTable guards the promise the whole verb rests on: the
// terminal state of the record decides the process exit code (spec §3.2), and
// villa work never invents one of its own.
func TestRunWorkExitCodeTable(t *testing.T) {
	cases := []struct {
		state taskstore.State
		want  int
	}{
		{taskstore.Done, exitPass},
		{taskstore.Flagged, exitWarn},
		{taskstore.Failed, exitBlocked},
		{taskstore.Refused, exitBlocked},
		{taskstore.Cancelled, exitBlocked},
		{taskstore.Interrupted, exitBlocked},
	}
	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			fx := newWorkFixture(t, nil)
			queued := terminalRecord(taskstore.Queued)
			queued.Exit = nil
			final := terminalRecord(tc.state)
			fx.api.record = final
			fx.api.frames = []string{
				narrationFrame("queued", queued),
				stateFrame(final),
			}

			cmd, _, errOut := lifecycleTestCmd()
			code := runWork(cmd, fx.workspace, "tidy", workOpts{}, fx.deps)
			if code != tc.want {
				t.Fatalf("exit = %d, want %d; stderr=%q", code, tc.want, errOut.String())
			}
		})
	}
}

// TestRunWorkPromptAnswers guards the approval prompt's contract: y approves
// once, a approves the rest of the task, and everything else (N, n, empty)
// denies, because the default is deny (spec §3.3).
func TestRunWorkPromptAnswers(t *testing.T) {
	cases := []struct {
		answer   string
		wantVerb string
		wantBody string
	}{
		{"y\n", "approve", `{"all":false}`},
		{"a\n", "approve", `{"all":true}`},
		{"N\n", "deny", ""},
		{"n\n", "deny", ""},
		{"\n", "deny", ""},
	}
	for _, tc := range cases {
		name := strings.TrimSpace(tc.answer)
		if name == "" {
			name = "empty line"
		}
		t.Run(name, func(t *testing.T) {
			fx := newWorkFixture(t, nil)
			fx.answers = []string{tc.answer}

			awaiting := terminalRecord(taskstore.AwaitingApproval)
			awaiting.Exit = nil
			awaiting.Approvals = []taskstore.Approval{{
				Seq: 1, Tool: "edit", Action: "write", Path: "summary.md",
				AskedAt: "2026-09-10T12:02:00Z",
			}}
			done := terminalRecord(taskstore.Done)
			fx.api.record = done
			fx.api.frames = []string{stateFrame(awaiting), stateFrame(done)}

			cmd, out, errOut := lifecycleTestCmd()
			if code := runWork(cmd, fx.workspace, "tidy", workOpts{}, fx.deps); code != exitPass {
				t.Fatalf("exit = %d, want %d; stderr=%q", code, exitPass, errOut.String())
			}
			if len(fx.api.answers) != 1 {
				t.Fatalf("answers = %v, want exactly one", fx.api.answers)
			}
			got := fx.api.answers[0]
			if got.verb != tc.wantVerb {
				t.Errorf("verb = %q, want %q", got.verb, tc.wantVerb)
			}
			if tc.wantBody != "" && got.body != tc.wantBody {
				t.Errorf("body = %q, want %q", got.body, tc.wantBody)
			}
			if !strings.Contains(out.String(), "summary.md") || !strings.Contains(out.String(), "write") {
				t.Errorf("output = %q, want the pending approval's action and path", out.String())
			}
		})
	}
}

// TestRunWorkRefusals guards the three refusals that happen BEFORE anything is
// submitted: each exits 1, names its remediation, and posts nothing.
func TestRunWorkRefusals(t *testing.T) {
	t.Run("unregistered workspace", func(t *testing.T) {
		fx := newWorkFixture(t, func(c *config.VillaConfig) { c.Workspace = nil })
		cmd, _, errOut := lifecycleTestCmd()
		code := runWork(cmd, fx.workspace, "tidy", workOpts{}, fx.deps)
		if code != exitBlocked {
			t.Fatalf("exit = %d, want %d", code, exitBlocked)
		}
		if !strings.Contains(errOut.String(), "villa workspace add") {
			t.Errorf("stderr = %q, want the workspace add remediation", errOut.String())
		}
		if len(fx.api.submits) != 0 {
			t.Errorf("submits = %v, want none", fx.api.submits)
		}
	})

	t.Run("tools mode off", func(t *testing.T) {
		fx := newWorkFixture(t, func(c *config.VillaConfig) { c.ToolsMode = false; c.CodingMode = false })
		cmd, _, errOut := lifecycleTestCmd()
		code := runWork(cmd, fx.workspace, "tidy", workOpts{}, fx.deps)
		if code != exitBlocked {
			t.Fatalf("exit = %d, want %d", code, exitBlocked)
		}
		if !strings.Contains(errOut.String(), "villa tools-mode enter") {
			t.Errorf("stderr = %q, want the tools-mode remediation", errOut.String())
		}
		if len(fx.api.submits) != 0 {
			t.Errorf("submits = %v, want none", fx.api.submits)
		}
	})

	t.Run("dashboard down", func(t *testing.T) {
		fx := newWorkFixture(t, nil)
		dead := httptest.NewServer(http.NewServeMux())
		base := dead.URL
		dead.Close()
		fx.deps.api = func(config.VillaConfig) taskAPIDeps {
			return taskAPIDeps{base: base, client: http.DefaultClient}
		}
		cmd, _, errOut := lifecycleTestCmd()
		code := runWork(cmd, fx.workspace, "tidy", workOpts{}, fx.deps)
		if code != exitBlocked {
			t.Fatalf("exit = %d, want %d", code, exitBlocked)
		}
		if !strings.Contains(errOut.String(), "systemctl --user start villa-dashboard.service") {
			t.Errorf("stderr = %q, want the dashboard start remediation", errOut.String())
		}
		if len(fx.api.submits) != 0 {
			t.Errorf("submits = %v, want none", fx.api.submits)
		}
	})
}

// TestRunWorkDetachPrintsIDOnly asserts --detach returns as soon as the record
// exists: the id on stdout, exit 0, and no event stream opened.
func TestRunWorkDetachPrintsIDOnly(t *testing.T) {
	fx := newWorkFixture(t, nil)
	fx.api.record = terminalRecord(taskstore.Queued)
	fx.api.record.Exit = nil

	cmd, out, errOut := lifecycleTestCmd()
	code := runWork(cmd, fx.workspace, "tidy", workOpts{detach: true}, fx.deps)
	if code != exitPass {
		t.Fatalf("exit = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}
	if out.String() != "20260910-120115-4f2a\n" {
		t.Errorf("output = %q, want the task id alone", out.String())
	}
}

// TestRunWorkAutoSendsAutoMode asserts --auto is carried on the submission, and
// the default submission is ask mode (spec §3.3).
func TestRunWorkAutoSendsAutoMode(t *testing.T) {
	for _, tc := range []struct {
		auto bool
		want string
	}{{false, "ask"}, {true, "auto"}} {
		fx := newWorkFixture(t, nil)
		fx.api.record = terminalRecord(taskstore.Queued)
		fx.api.record.Exit = nil

		cmd, _, _ := lifecycleTestCmd()
		runWork(cmd, fx.workspace, "tidy", workOpts{detach: true, auto: tc.auto}, fx.deps)
		if len(fx.api.submits) != 1 {
			t.Fatalf("submits = %v, want one", fx.api.submits)
		}
		if got := fx.api.submits[0]["mode"]; got != tc.want {
			t.Errorf("mode = %q, want %q", got, tc.want)
		}
	}
}

// TestRunWorkStdinInstruction asserts a positional "-" reads the instruction
// from stdin, so a long prompt need not be shell-quoted.
func TestRunWorkStdinInstruction(t *testing.T) {
	fx := newWorkFixture(t, nil)
	fx.api.record = terminalRecord(taskstore.Queued)
	fx.api.record.Exit = nil

	cmd, _, _ := lifecycleTestCmd()
	cmd.SetIn(strings.NewReader("reconcile the ledger\n"))
	runWork(cmd, fx.workspace, "-", workOpts{detach: true}, fx.deps)
	if len(fx.api.submits) != 1 {
		t.Fatalf("submits = %v, want one", fx.api.submits)
	}
	if got := fx.api.submits[0]["instruction"]; got != "reconcile the ledger" {
		t.Errorf("instruction = %q, want the stdin text", got)
	}
}

// TestRunWorkSubmitsResolvedWorkspace asserts the grant villa posts is the
// RESOLVED registered path, never the argument as typed.
func TestRunWorkSubmitsResolvedWorkspace(t *testing.T) {
	fx := newWorkFixture(t, nil)
	fx.api.record = terminalRecord(taskstore.Queued)
	fx.api.record.Exit = nil
	resolved, err := filepath.EvalSymlinks(fx.workspace)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	cmd, _, _ := lifecycleTestCmd()
	runWork(cmd, filepath.Join(fx.workspace, "."), "tidy", workOpts{detach: true}, fx.deps)
	if len(fx.api.submits) != 1 {
		t.Fatalf("submits = %v, want one", fx.api.submits)
	}
	if got := fx.api.submits[0]["workspace"]; got != resolved {
		t.Errorf("workspace = %q, want the resolved grant %q", got, resolved)
	}
}

// TestRunWorkVerboseReplaysLog asserts --verbose prints the task's transcript
// after the summary. The log is read locally, from this host's villa data root:
// there is no log route, so a detached task's transcript is readable only on
// the machine that ran it.
func TestRunWorkVerboseReplaysLog(t *testing.T) {
	fx := newWorkFixture(t, nil)
	final := terminalRecord(taskstore.Done)
	fx.api.record = final
	fx.api.frames = []string{stateFrame(final)}
	fx.deps.readLog = func(string) ([]taskstore.LogEvent, error) {
		return []taskstore.LogEvent{
			{At: time.Date(2026, 9, 10, 12, 1, 15, 0, time.UTC), Kind: "narration", Text: "queued"},
			{At: time.Date(2026, 9, 10, 12, 1, 16, 0, time.UTC), Kind: "harness", Raw: []byte(`{"kind":"message"}`)},
		}, nil
	}

	cmd, out, errOut := lifecycleTestCmd()
	if code := runWork(cmd, fx.workspace, "tidy", workOpts{verbose: true}, fx.deps); code != exitPass {
		t.Fatalf("exit = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}
	for _, want := range []string{"2026-09-10T12:01:15Z narration queued", `harness {"kind":"message"}`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output = %q, want it to contain %q", out.String(), want)
		}
	}
}
