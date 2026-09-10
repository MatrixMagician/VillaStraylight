package taskrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/approval"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/crushapi"
	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
)

// fakeBridge is a scripted sandbox: the test feeds events, the runner's
// commands are recorded, Wait returns the scripted exit, Kill is observed.
type fakeBridge struct {
	events  chan crushapi.Event
	mu      sync.Mutex
	sent    []crushapi.Command
	waitErr error
	killed  bool
}

func newFakeBridge() *fakeBridge {
	return &fakeBridge{events: make(chan crushapi.Event, 64)}
}

func (f *fakeBridge) Events() <-chan crushapi.Event { return f.events }

func (f *fakeBridge) Send(c crushapi.Command) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, c)
	return nil
}

func (f *fakeBridge) Wait() error { return f.waitErr }

func (f *fakeBridge) Kill() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killed = true
	close(f.events)
	return nil
}

func (f *fakeBridge) commands() []crushapi.Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]crushapi.Command(nil), f.sent...)
}

func (f *fakeBridge) wasKilled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.killed
}

func (f *fakeBridge) feed(evs ...crushapi.Event) {
	for _, ev := range evs {
		f.events <- ev
	}
}

func (f *fakeBridge) end() { close(f.events) }

// clock hands out strictly increasing seconds so two submits never mint the
// same id.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(time.Second)
	return c.t
}

type harness struct {
	t       *testing.T
	r       *Runner
	store   taskstore.Store
	ws      string
	bridges chan *fakeBridge
	deps    Deps
	mu      sync.Mutex
	killed  []string
	audit   func(doc grounding.Document, sources []grounding.Source) grounding.DocumentReport
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o700); err != nil {
		t.Fatal(err)
	}
	h := &harness{
		t:       t,
		store:   taskstore.New(filepath.Join(root, "data")),
		ws:      ws,
		bridges: make(chan *fakeBridge, 4),
		audit: func(doc grounding.Document, _ []grounding.Source) grounding.DocumentReport {
			return grounding.DocumentReport{Path: doc.Path, Checked: true, Claims: 1}
		},
	}
	clk := &clock{t: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	h.deps = Deps{
		Store:      h.store,
		LoadConfig: func() (config.VillaConfig, error) { return config.VillaConfig{ToolsMode: true}, nil },
		Launch: func(_ context.Context, args []string) (Bridge, error) {
			select {
			case b := <-h.bridges:
				return b, nil
			default:
				return nil, errors.New("no scripted bridge for " + strings.Join(args, " "))
			}
		},
		KillByName: func(name string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.killed = append(h.killed, name)
			return nil
		},
		RenderArgs: func(_ config.VillaConfig, workspace, id string) ([]string, error) {
			return []string{"run", workspace, id}, nil
		},
		Registered: func(_ config.VillaConfig, path string) (string, bool) { return path, path == ws },
		ToolsOn:    func(config.VillaConfig) bool { return true },
		SandboxReady: func() (bool, string) {
			return true, ""
		},
		Audit: func(_ context.Context, doc grounding.Document, sources []grounding.Source) grounding.DocumentReport {
			return h.audit(doc, sources)
		},
		ReadFile: func(workspace, rel string) ([]byte, error) {
			return os.ReadFile(filepath.Join(workspace, rel))
		},
		Now:  clk.now,
		Rand: bytes.NewReader(bytes.Repeat([]byte{0x4f, 0x2a}, 64)),
	}
	return h
}

func (h *harness) start() *Runner {
	h.t.Helper()
	h.r = New(h.deps)
	return h.r
}

func (h *harness) submit(mode approval.Mode) taskstore.Task {
	h.t.Helper()
	task, err := h.r.Submit(context.Background(), SubmitRequest{Workspace: h.ws, Instruction: "write the memo", Mode: mode})
	if err != nil {
		h.t.Fatalf("Submit: %v", err)
	}
	return task
}

func (h *harness) waitState(id string, want taskstore.State) taskstore.Task {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last taskstore.Task
	for time.Now().Before(deadline) {
		t, err := h.store.Load(id)
		if err == nil && t.State == want {
			return t
		}
		last = t
		time.Sleep(2 * time.Millisecond)
	}
	h.t.Fatalf("task %s never reached %q; last state %q", id, want, last.State)
	return last
}

func (h *harness) waitCommands(b *fakeBridge, n int) []crushapi.Command {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cs := b.commands(); len(cs) >= n {
			return cs
		}
		time.Sleep(2 * time.Millisecond)
	}
	h.t.Fatalf("bridge saw %d commands, want %d", len(b.commands()), n)
	return nil
}

func (h *harness) logText(id string) string {
	h.t.Helper()
	evs, err := h.store.ReadLog(id)
	if err != nil {
		h.t.Fatalf("ReadLog: %v", err)
	}
	var sb strings.Builder
	for _, ev := range evs {
		sb.WriteString(ev.Kind + ": " + ev.Text + " " + string(ev.Raw) + "\n")
	}
	return sb.String()
}

func (h *harness) writeFile(rel, content string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.ws, rel), []byte(content), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func ready() crushapi.Event { return crushapi.Event{Kind: crushapi.KindBridgeReady, Version: "0.76.0"} }

func permission(id, tool, action, path string, params string) crushapi.Event {
	return crushapi.Event{Kind: crushapi.KindPermissionRequest, ID: id, Permission: &crushapi.PermissionRequest{
		ID: id, Tool: tool, Action: action, Path: path, Params: json.RawMessage(params),
	}}
}

func fileEvent(path string) crushapi.Event {
	return crushapi.Event{Kind: crushapi.KindFile, File: &crushapi.FileEvent{Path: path}}
}

func runComplete() crushapi.Event {
	return crushapi.Event{Kind: crushapi.KindRunComplete, RunComplete: &crushapi.RunComplete{SessionID: "s"}}
}

func filesRead(paths ...string) crushapi.Event {
	return crushapi.Event{Kind: crushapi.KindFilesRead, Files: paths}
}

// TestDoneCarriesTheWholeRecord guards the happy path end to end: queued →
// running → done with harness, files, grounding and timestamps on disk, and
// the prompt carrying the citation instruction.
func TestDoneCarriesTheWholeRecord(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.writeFile("memo.md", "Revenue grew.")
	h.writeFile("q3.csv", "revenue,100")
	var gotSources []grounding.Source
	h.audit = func(doc grounding.Document, sources []grounding.Source) grounding.DocumentReport {
		gotSources = sources
		return grounding.DocumentReport{Path: doc.Path, Checked: true, Claims: 2}
	}
	h.start()
	task := h.submit(approval.ModeAsk)
	if task.State != taskstore.Queued {
		t.Fatalf("submitted state = %q, want queued", task.State)
	}
	b.feed(ready(), fileEvent("/workspace/memo.md"), fileEvent("/workspace/memo.md"), runComplete(), filesRead("/workspace/q3.csv", "/workspace/memo.md"))
	b.end()

	got := h.waitState(task.ID, taskstore.Done)
	if *got.Exit != 0 || got.StartedAt == "" || got.FinishedAt == "" {
		t.Errorf("record = %+v", got)
	}
	if got.Harness != (taskstore.Harness{Name: "crush", Version: "0.76.0"}) {
		t.Errorf("harness = %+v", got.Harness)
	}
	wantFiles := []taskstore.FileEvent{{Path: "memo.md", Action: "created"}, {Path: "memo.md", Action: "modified"}}
	if len(got.Files) != 2 || got.Files[0] != wantFiles[0] || got.Files[1] != wantFiles[1] {
		t.Errorf("files = %+v, want %+v", got.Files, wantFiles)
	}
	if !got.Grounding.Checked || len(got.Grounding.Documents) != 1 || got.Grounding.Documents[0].Claims != 2 {
		t.Errorf("grounding = %+v; one document per distinct path", got.Grounding)
	}
	if len(gotSources) != 1 || gotSources[0].Path != "q3.csv" || gotSources[0].Content != "revenue,100" {
		t.Errorf("audit sources = %+v; the document itself must not be its own source", gotSources)
	}
	cmds := b.commands()
	if len(cmds) != 1 || cmds[0].Kind != crushapi.CmdPrompt || !strings.HasSuffix(cmds[0].Prompt, grounding.CitationInstruction()) || !strings.HasPrefix(cmds[0].Prompt, "write the memo") {
		t.Errorf("commands = %+v", cmds)
	}
	log := h.logText(task.ID)
	for _, want := range []string{"narration: queued", "harness:", `"kind":"run_complete"`, "narration: done"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

// TestFlaggedOnUnsupportedClaim guards exit 2: one unsupported claim flags the
// task and lands in the record.
func TestFlaggedOnUnsupportedClaim(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.writeFile("memo.md", "Revenue grew 40%.")
	h.audit = func(doc grounding.Document, _ []grounding.Source) grounding.DocumentReport {
		return grounding.DocumentReport{Path: doc.Path, Checked: true, Claims: 1, Unsupported: []grounding.Unsupported{{Claim: "revenue grew 40%", Reason: "no figure"}}}
	}
	h.start()
	task := h.submit(approval.ModeAsk)
	b.feed(ready(), fileEvent("/workspace/memo.md"), runComplete(), filesRead())
	b.end()

	got := h.waitState(task.ID, taskstore.Flagged)
	if *got.Exit != 2 || !got.Grounding.Checked || len(got.Grounding.Documents[0].Unsupported) != 1 {
		t.Errorf("record = %+v", got)
	}
}

// TestFlaggedWhenTheAuditCouldNotRun guards the honesty rule: an audit that
// did not run is never "grounded"; the task is flagged with checked:false.
func TestFlaggedWhenTheAuditCouldNotRun(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.writeFile("memo.md", "x")
	h.audit = func(doc grounding.Document, _ []grounding.Source) grounding.DocumentReport {
		return grounding.DocumentReport{Path: doc.Path, Checked: false, Err: "llama-server unreachable"}
	}
	h.start()
	task := h.submit(approval.ModeAsk)
	b.feed(ready(), fileEvent("/workspace/memo.md"), runComplete(), filesRead())
	b.end()

	got := h.waitState(task.ID, taskstore.Flagged)
	if got.Grounding.Checked {
		t.Errorf("grounding.checked = true after an audit that did not run")
	}
	if !strings.Contains(h.logText(task.ID), "llama-server unreachable") {
		t.Errorf("log lacks the audit error:\n%s", h.logText(task.ID))
	}
}

// TestFailedWhenTheBridgeDies guards the failure edge: a bridge that exits
// before run_complete fails the task with its error in the log.
func TestFailedWhenTheBridgeDies(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	b.waitErr = errors.New("exit status 137")
	h.bridges <- b
	h.start()
	task := h.submit(approval.ModeAsk)
	b.feed(ready())
	b.end()

	got := h.waitState(task.ID, taskstore.Failed)
	if *got.Exit != 1 {
		t.Errorf("exit = %d", *got.Exit)
	}
	if !strings.Contains(h.logText(task.ID), "exit status 137") {
		t.Errorf("log lacks the bridge error:\n%s", h.logText(task.ID))
	}
}

// TestRefusals guards the four refusals: each creates a refused record with the
// remediation in the log and never reaches running.
func TestRefusals(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(h *harness)
		want    string
	}{
		{"unregistered workspace", func(h *harness) {
			h.deps.Registered = func(config.VillaConfig, string) (string, bool) { return "", false }
		}, "villa workspace add"},
		{"tools mode off", func(h *harness) {
			h.deps.ToolsOn = func(config.VillaConfig) bool { return false }
		}, "villa tools-mode enter"},
		{"sandbox not ready", func(h *harness) {
			h.deps.SandboxReady = func() (bool, string) { return false, "Install the sandbox runtime packages" }
		}, "Install the sandbox runtime packages"},
		{"version drift", func(h *harness) {
			b := newFakeBridge()
			b.feed(crushapi.Event{Kind: crushapi.KindBridgeError, Error: "crush version drift: the sandbox runs v0.77.1"})
			b.end()
			h.bridges <- b
		}, "crush version drift"},
		{"launch failure", func(h *harness) {
			h.deps.Launch = func(context.Context, []string) (Bridge, error) {
				return nil, errors.New("podman: krun runtime not found")
			}
		}, "krun runtime not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tc.arrange(h)
			h.start()
			task := h.submit(approval.ModeAsk)
			got := h.waitState(task.ID, taskstore.Refused)
			if *got.Exit != 1 {
				t.Errorf("exit = %d", *got.Exit)
			}
			if !strings.Contains(h.logText(task.ID), tc.want) {
				t.Errorf("log lacks remediation %q:\n%s", tc.want, h.logText(task.ID))
			}
			if got.StartedAt != "" {
				t.Errorf("a refused task must never have started, got started_at %q", got.StartedAt)
			}
		})
	}
}

// TestCancelWhileQueued guards the drop edge: a queued task is cancelled
// without ever launching, and a terminal task refuses a second cancel.
func TestCancelWhileQueued(t *testing.T) {
	h := newHarness(t)
	first := newFakeBridge()
	h.bridges <- first
	h.start()
	running := h.submit(approval.ModeAsk)
	first.feed(ready())
	h.waitState(running.ID, taskstore.Running)
	queued := h.submit(approval.ModeAsk)
	if err := h.r.Cancel(queued.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got := h.waitState(queued.ID, taskstore.Cancelled)
	if got.StartedAt != "" {
		t.Errorf("a queued task was launched: %+v", got)
	}
	first.feed(runComplete(), filesRead())
	first.end()
	h.waitState(running.ID, taskstore.Done)
	if err := h.r.Cancel(queued.ID); !errors.Is(err, ErrTerminal) {
		t.Errorf("cancelling a terminal task = %v, want ErrTerminal", err)
	}
	if err := h.r.Cancel("nope"); !errors.Is(err, taskstore.ErrNotFound) {
		t.Errorf("cancelling an unknown id = %v, want ErrNotFound", err)
	}
}

// TestCancelWhileRunningKills guards the kill edge: cancel on a running task
// kills the sandbox and marks cancelled.
func TestCancelWhileRunningKills(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.start()
	task := h.submit(approval.ModeAsk)
	b.feed(ready())
	h.waitState(task.ID, taskstore.Running)
	if err := h.r.Cancel(task.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	h.waitState(task.ID, taskstore.Cancelled)
	if !b.wasKilled() {
		t.Error("Kill was not called")
	}
}

// TestCancelWhileAwaitingApproval guards the third cancel edge: a parked task
// is killed and cancelled, the pending approval left unanswered on the record.
func TestCancelWhileAwaitingApproval(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.start()
	task := h.submit(approval.ModeAsk)
	b.feed(ready(), permission("p1", "edit", "write", "/workspace/memo.md", `{}`))
	h.waitState(task.ID, taskstore.AwaitingApproval)
	if err := h.r.Cancel(task.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got := h.waitState(task.ID, taskstore.Cancelled)
	if !b.wasKilled() {
		t.Error("Kill was not called")
	}
	if len(got.Approvals) != 1 || got.Approvals[0].Answer != "" {
		t.Errorf("approvals = %+v, want one unanswered row", got.Approvals)
	}
}

// TestAskApproveDone guards the park-and-resume edge: a write in ask mode parks
// the task, approve sends allow and records who answered.
func TestAskApproveDone(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.start()
	task := h.submit(approval.ModeAsk)
	if err := h.r.Approve(task.ID, false); !errors.Is(err, ErrNotAwaiting) {
		t.Errorf("Approve before any request = %v, want ErrNotAwaiting", err)
	}
	b.feed(ready(), permission("p1", "edit", "write", "/workspace/memo.md", `{}`))
	got := h.waitState(task.ID, taskstore.AwaitingApproval)
	if len(got.Approvals) != 1 || got.Approvals[0].Seq != 1 || got.Approvals[0].AskedAt == "" || got.Approvals[0].Tool != "edit" || got.Approvals[0].Path != "memo.md" {
		t.Fatalf("approvals = %+v", got.Approvals)
	}
	if err := h.r.Approve(task.ID, false); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := h.r.Approve(task.ID, false); !errors.Is(err, ErrNotAwaiting) {
		t.Errorf("second Approve = %v, want ErrNotAwaiting", err)
	}
	cmds := h.waitCommands(b, 2)
	if cmds[1].Kind != crushapi.CmdGrant || cmds[1].PermissionID != "p1" || cmds[1].Answer != crushapi.Allow {
		t.Errorf("grant = %+v", cmds[1])
	}
	h.waitState(task.ID, taskstore.Running)
	b.feed(runComplete(), filesRead())
	b.end()
	got = h.waitState(task.ID, taskstore.Done)
	a := got.Approvals[0]
	if a.Answer != "allow" || a.AnsweredAt == "" || a.By != "dashboard" {
		t.Errorf("approval row = %+v", a)
	}
}

// TestAskDenyDone guards the denial rule (spec sec3.3): a denial is fed back,
// recorded, and does not change the exit code.
func TestAskDenyDone(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.start()
	task := h.submit(approval.ModeAsk)
	b.feed(ready(), permission("p1", "bash", "execute", "", `{"command":"python3 build.py"}`))
	h.waitState(task.ID, taskstore.AwaitingApproval)
	if err := h.r.Deny(task.ID); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	cmds := h.waitCommands(b, 2)
	if cmds[1].Answer != crushapi.Deny {
		t.Errorf("grant = %+v", cmds[1])
	}
	b.feed(runComplete(), filesRead())
	b.end()
	got := h.waitState(task.ID, taskstore.Done)
	if *got.Exit != 0 || got.Approvals[0].Answer != "deny" {
		t.Errorf("record = %+v", got)
	}
}

// TestApproveAllGrantsTheSession guards allow_session: after approve --all, a
// later ask is granted without parking and recorded as answered by the session.
func TestApproveAllGrantsTheSession(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.start()
	task := h.submit(approval.ModeAsk)
	b.feed(ready(), permission("p1", "edit", "write", "/workspace/a.md", `{}`))
	h.waitState(task.ID, taskstore.AwaitingApproval)
	if err := h.r.Approve(task.ID, true); err != nil {
		t.Fatalf("Approve --all: %v", err)
	}
	cmds := h.waitCommands(b, 2)
	if cmds[1].Answer != crushapi.AllowSession {
		t.Errorf("grant = %+v", cmds[1])
	}
	h.waitState(task.ID, taskstore.Running)
	b.feed(permission("p2", "edit", "write", "/workspace/b.md", `{}`))
	cmds = h.waitCommands(b, 3)
	if cmds[2].PermissionID != "p2" || cmds[2].Answer != crushapi.Allow {
		t.Errorf("auto-grant = %+v", cmds[2])
	}
	b.feed(runComplete(), filesRead())
	b.end()
	got := h.waitState(task.ID, taskstore.Done)
	if len(got.Approvals) != 2 || got.Approvals[0].Answer != "allow_session" || got.Approvals[1].Answer != "allow" || got.Approvals[1].By != "session" {
		t.Errorf("approvals = %+v", got.Approvals)
	}
}

// TestAutoModeAllowsAWriteWithoutAsking guards the auto column of the table.
func TestAutoModeAllowsAWriteWithoutAsking(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.start()
	task := h.submit(approval.ModeAuto)
	b.feed(ready(), permission("p1", "edit", "write", "/workspace/a.md", `{}`))
	cmds := h.waitCommands(b, 2)
	if cmds[1].Answer != crushapi.Allow {
		t.Errorf("grant = %+v", cmds[1])
	}
	b.feed(runComplete(), filesRead())
	b.end()
	got := h.waitState(task.ID, taskstore.Done)
	if len(got.Approvals) != 0 {
		t.Errorf("a table allow must not be an approval row: %+v", got.Approvals)
	}
}

// TestDeletionAsksInAutoMode guards the one rule that overrides every mode.
func TestDeletionAsksInAutoMode(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.start()
	task := h.submit(approval.ModeAuto)
	b.feed(ready(), permission("p1", "bash", "execute", "", `{"command":"rm -rf build"}`))
	got := h.waitState(task.ID, taskstore.AwaitingApproval)
	if got.Approvals[0].Action != "execute" || got.Approvals[0].Tool != "bash" {
		t.Errorf("approvals = %+v", got.Approvals)
	}
}

// TestFetchIsDeniedByPolicy guards the table's deny column: a fetch is denied
// without parking and without an approval row.
func TestFetchIsDeniedByPolicy(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.start()
	task := h.submit(approval.ModeAuto)
	b.feed(ready(), permission("p1", "fetch", "fetch", "", `{}`))
	cmds := h.waitCommands(b, 2)
	if cmds[1].Answer != crushapi.Deny {
		t.Errorf("grant = %+v", cmds[1])
	}
	b.feed(runComplete(), filesRead())
	b.end()
	if got := h.waitState(task.ID, taskstore.Done); len(got.Approvals) != 0 {
		t.Errorf("approvals = %+v", got.Approvals)
	}
}

// TestTwoSubmitsRunOneAfterTheOther guards the global one-at-a-time rule.
func TestTwoSubmitsRunOneAfterTheOther(t *testing.T) {
	h := newHarness(t)
	first, second := newFakeBridge(), newFakeBridge()
	h.bridges <- first
	h.bridges <- second
	h.start()
	a := h.submit(approval.ModeAsk)
	first.feed(ready())
	h.waitState(a.ID, taskstore.Running)
	b := h.submit(approval.ModeAsk)
	time.Sleep(20 * time.Millisecond)
	if got, _ := h.store.Load(b.ID); got.State != taskstore.Queued {
		t.Fatalf("second task state = %q while the first runs, want queued", got.State)
	}
	if len(second.commands()) != 0 {
		t.Fatal("the second bridge was prompted while the first task ran")
	}
	first.feed(runComplete(), filesRead())
	first.end()
	h.waitState(a.ID, taskstore.Done)
	second.feed(ready(), runComplete(), filesRead())
	second.end()
	h.waitState(b.ID, taskstore.Done)
}

// TestRecoverInterruptsAndKillsByName guards service-start recovery: a stale
// running record becomes interrupted and its container is killed by name.
func TestRecoverInterruptsAndKillsByName(t *testing.T) {
	h := newHarness(t)
	stale := taskstore.Task{ID: "20260910-110000-0000", Workspace: h.ws, Instruction: "x", Mode: "ask", State: taskstore.Queued, SubmittedAt: "2026-09-10T11:00:00Z"}
	if err := h.store.Create(stale); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.Transition(stale.ID, taskstore.Running, nil); err != nil {
		t.Fatal(err)
	}
	r := h.start()
	if err := r.Recover(); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	got, _ := h.store.Load(stale.ID)
	if got.State != taskstore.Interrupted {
		t.Errorf("state = %q, want interrupted", got.State)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.killed) != 1 || h.killed[0] != "villa-task-"+stale.ID {
		t.Errorf("killed = %v", h.killed)
	}
	if !strings.Contains(h.logText(stale.ID), "interrupted") {
		t.Errorf("log lacks the interruption:\n%s", h.logText(stale.ID))
	}
}

// TestSubscribeOnATerminalTaskClosesAfterOneSnapshot guards the late
// subscriber: one final snapshot, then close.
func TestSubscribeOnATerminalTaskClosesAfterOneSnapshot(t *testing.T) {
	h := newHarness(t)
	h.deps.ToolsOn = func(config.VillaConfig) bool { return false }
	r := h.start()
	task := h.submit(approval.ModeAsk)
	ch, stop := r.Subscribe(task.ID)
	defer stop()
	n, ok := <-ch
	if !ok || n.Kind != KindState || n.Task == nil || n.Task.State != taskstore.Refused {
		t.Fatalf("first narration = %+v, ok=%v", n, ok)
	}
	if _, ok := <-ch; ok {
		t.Error("channel still open after the final snapshot")
	}
	unknown, _ := r.Subscribe("nope")
	if _, ok := <-unknown; ok {
		t.Error("an unknown id must yield a closed channel")
	}
}

// TestSubscribeStreamsStateChangesAndNarration guards the live stream: every
// state change and narration line arrives with a snapshot, and the channel
// closes at the terminal state.
func TestSubscribeStreamsStateChangesAndNarration(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	r := h.start()
	task := h.submit(approval.ModeAsk)
	ch, stop := r.Subscribe(task.ID)
	defer stop()
	b.feed(ready(), fileEvent("/workspace/a.md"), runComplete(), filesRead())
	b.end()
	var states []taskstore.State
	var texts []string
	for n := range ch {
		if n.Task == nil {
			t.Fatal("narration without a snapshot")
		}
		if n.Kind == KindState {
			states = append(states, n.Task.State)
		}
		texts = append(texts, n.Text)
	}
	want := []taskstore.State{taskstore.Running, taskstore.Flagged}
	if len(states) != len(want) || states[0] != want[0] || states[1] != want[1] {
		t.Errorf("states = %v, want %v", states, want)
	}
	if !strings.Contains(strings.Join(texts, "\n"), "a.md") {
		t.Errorf("no file narration in %q", texts)
	}
}

// TestSubmitValidatesTheBoundary guards the parse-at-the-boundary rule: an
// unknown mode or an empty instruction is an error, never a record.
func TestSubmitValidatesTheBoundary(t *testing.T) {
	h := newHarness(t)
	r := h.start()
	if _, err := r.Submit(context.Background(), SubmitRequest{Workspace: h.ws, Instruction: "x", Mode: "yolo"}); err == nil {
		t.Error("unknown mode accepted")
	}
	if _, err := r.Submit(context.Background(), SubmitRequest{Workspace: h.ws, Mode: approval.ModeAsk}); err == nil {
		t.Error("empty instruction accepted")
	}
	h.deps.ToolsOn = func(config.VillaConfig) bool { return false }
	r = h.start()
	task, err := r.Submit(context.Background(), SubmitRequest{Workspace: h.ws, Instruction: "x"})
	if err != nil || task.Mode != string(approval.ModeAsk) {
		t.Errorf("empty mode should default to ask: %+v, %v", task, err)
	}
}
