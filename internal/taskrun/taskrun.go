// Package taskrun is the workspace agent's runner (spec v1.11 §5): one task at
// a time globally, driven through the sandbox bridge's stdio, every permission
// answered by internal/approval or parked for the operator, every terminal
// state written to internal/taskstore and narrated to subscribers.
//
// The package holds no host I/O of its own. Launching the sandbox, killing it
// by name, rendering its arguments, reading workspace files and the audit call
// are all Deps; the runner composes them and owns only the lifecycle. The one
// worker goroutine is the only thing that talks to the bridge: Approve, Deny
// and Cancel hand their answer to it over a channel and never touch the bridge
// from the HTTP goroutine.
//
// taskstore.CanTransition is the arbiter of every state change. The sandbox is
// launched while the record is still queued and bridge_ready is what moves it
// to running, so anything that fails before the bridge is ready is a refusal
// (queued → refused, spec §3.2: "villa declining before the sandbox started")
// and anything after is a failure (running → failed). A transition the table
// refuses is a bug; the runner logs it and fails the task rather than guessing.
package taskrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/approval"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/crushapi"
	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
)

// Bridge is one launched sandbox seen from the host: the event stream off its
// stdout, commands into its stdin, its exit, and the kill switch.
type Bridge interface {
	Events() <-chan crushapi.Event
	Send(crushapi.Command) error
	Wait() error
	Kill() error
}

// Deps is the runner's host seam. cmd/villa's liveTaskRunDeps binds it to
// podman, the pin resolver, the chat unit and the filesystem; tests bind a
// scripted Bridge.
type Deps struct {
	Store      taskstore.Store
	LoadConfig func() (config.VillaConfig, error)
	// Launch starts one sandbox from the rendered podman arguments.
	Launch func(ctx context.Context, args []string) (Bridge, error)
	// KillByName kills a container recovery found still running.
	KillByName func(name string) error
	// RenderArgs renders the podman arguments for one task (orchestrate.RenderSandboxRun
	// behind the pin resolver).
	RenderArgs func(cfg config.VillaConfig, workspace, id string) ([]string, error)
	// Registered resolves a workspace path against the config's grant list.
	Registered func(cfg config.VillaConfig, path string) (string, bool)
	ToolsOn    func(cfg config.VillaConfig) bool
	// SandboxReady is PRE-09's verdict; the remediation is the refusal text.
	SandboxReady func() (ok bool, remediation string)
	Audit        func(ctx context.Context, doc grounding.Document, sources []grounding.Source) grounding.DocumentReport
	// ReadFile reads one workspace-relative file for the audit.
	ReadFile func(workspace, rel string) ([]byte, error)
	Now      func() time.Time
	Rand     io.Reader
}

// SubmitRequest is one `villa work` submission.
type SubmitRequest struct {
	Workspace   string
	Instruction string
	Mode        approval.Mode
}

// Narration kinds. A state narration is emitted on every record state change;
// a narration line is villa's own commentary between them.
const (
	KindNarration = "narration"
	KindState     = "state"
)

// Narration is one item of a task's live stream and the SSE payload
// (`event: <Kind>`, `data: <Narration JSON>`). Task is the runner's current
// snapshot, which may be ahead of the on-disk record between transitions.
type Narration struct {
	At   time.Time       `json:"at"`
	Kind string          `json:"kind"`
	Text string          `json:"text"`
	Task *taskstore.Task `json:"task"`
}

// The errors the HTTP layer maps to status codes.
var (
	ErrNotAwaiting = errors.New("taskrun: task is not awaiting approval")
	ErrTerminal    = errors.New("taskrun: task is already finished")
)

// guestWorkspace is where the grant is mounted inside the sandbox; every path
// the bridge reports is under it (orchestrate.RenderSandboxRun).
const guestWorkspace = "/workspace/"

// answer is what Approve or Deny hands the worker.
type answer struct {
	grant crushapi.GrantAnswer
	by    string
}

// active is the task the worker currently owns. awaiting is the claim flag:
// Approve/Deny clear it under the runner's mutex before sending, so exactly one
// answer reaches the worker per parked request. interrupted marks a cancel that
// came from Close rather than the operator, which ends the record interrupted.
// Every field but id and the channels is read and written under Runner.mu.
type active struct {
	id          string
	answers     chan answer
	cancel      chan struct{}
	awaiting    bool
	cancelled   bool
	interrupted bool
}

// Runner is the one-at-a-time task runner. New starts its worker goroutine and
// Close stops and joins it.
type Runner struct {
	d Deps

	mu      sync.Mutex
	queue   []string
	current *active
	subs    map[string][]chan Narration
	wake    chan struct{}

	// stop is closed by Close; wg counts the worker and every detached Wait,
	// so Close can join everything that still writes to the store.
	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// New builds a Runner and starts its worker. Call Recover before serving and
// Close when the service stops.
func New(d Deps) *Runner {
	r := &Runner{d: d, subs: map[string][]chan Narration{}, wake: make(chan struct{}, 1), stop: make(chan struct{})}
	r.track(r.work)
	return r
}

// Close stops the worker and joins it, so nothing writes to the store once
// Close returns. A task in flight is killed the way a cancel kills it, but its
// record ends interrupted: spec §3.2's word for a task the service stopped
// under, where cancelled is the operator's. Close is idempotent, and a second
// call returns as soon as the first has joined.
func (r *Runner) Close() {
	r.stopOnce.Do(func() {
		close(r.stop)
		r.mu.Lock()
		if a := r.current; a != nil && !a.cancelled {
			a.cancelled = true
			a.interrupted = true
			a.cancel <- struct{}{}
		}
		r.mu.Unlock()
	})
	r.wg.Wait()
}

// track runs fn in a goroutine Close joins. Every caller but New runs on the
// worker, which already holds a count, so the counter cannot reach zero
// between a concurrent Wait and this Add.
func (r *Runner) track(fn func()) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		fn()
	}()
}

// List is the store's time-ordered record list, for the read routes.
func (r *Runner) List() ([]taskstore.Task, error) { return r.d.Store.List() }

// Load is one record as it is on disk, for the read routes.
func (r *Runner) Load(id string) (taskstore.Task, error) { return r.d.Store.Load(id) }

// Recover marks every non-terminal record interrupted (taskstore.Recover) and
// kills each one's container by name, best effort. Spec §3.2: recovery never
// re-queues.
func (r *Runner) Recover() error {
	ids, err := r.d.Store.Recover()
	for _, id := range ids {
		_ = r.d.KillByName(orchestrate.SandboxContainerName(id))
		if t, loadErr := r.d.Store.Load(id); loadErr == nil {
			r.narrate(&t, KindState, "interrupted: the dashboard service restarted while this task was running")
			r.closeSubs(id)
		}
	}
	return err
}

// Submit refuses or queues one task. A refusal is a record in state refused
// with the remediation in its log, so the caller's exit-code table works from
// the record alone; only a malformed request or a store failure is an error.
func (r *Runner) Submit(_ context.Context, req SubmitRequest) (taskstore.Task, error) {
	mode := req.Mode
	if mode == "" {
		mode = approval.ModeAsk
	}
	if mode != approval.ModeAsk && mode != approval.ModeAuto {
		return taskstore.Task{}, fmt.Errorf("taskrun: mode %q is not %q or %q", req.Mode, approval.ModeAsk, approval.ModeAuto)
	}
	if req.Instruction == "" {
		return taskstore.Task{}, errors.New("taskrun: empty instruction")
	}
	cfg, err := r.d.LoadConfig()
	if err != nil {
		return taskstore.Task{}, fmt.Errorf("taskrun: load config: %w", err)
	}

	now := r.d.Now()
	t := taskstore.Task{
		ID:          taskstore.NewID(now, r.d.Rand),
		Workspace:   req.Workspace,
		Instruction: req.Instruction,
		Mode:        string(mode),
		State:       taskstore.Queued,
		SubmittedAt: stamp(now),
	}

	refusal := ""
	if ws, ok := r.d.Registered(cfg, req.Workspace); !ok {
		refusal = fmt.Sprintf("refused: %s is not a registered workspace; run `villa workspace add %s`", req.Workspace, req.Workspace)
	} else if t.Workspace = ws; !r.d.ToolsOn(cfg) {
		refusal = "refused: tools mode is off; run `villa tools-mode enter`"
	} else if ready, remediation := r.d.SandboxReady(); !ready {
		refusal = "refused: the sandbox cannot start: " + remediation
	}
	if refusal != "" {
		t.State = taskstore.Refused
		t.FinishedAt = stamp(now)
		if err := r.d.Store.Create(t); err != nil {
			return taskstore.Task{}, err
		}
		t, _ = r.d.Store.Load(t.ID)
		r.narrate(&t, KindState, refusal)
		return t, nil
	}

	if err := r.d.Store.Create(t); err != nil {
		return taskstore.Task{}, err
	}
	t, _ = r.d.Store.Load(t.ID)
	r.mu.Lock()
	r.queue = append(r.queue, t.ID)
	r.mu.Unlock()
	r.narrate(&t, KindNarration, "queued")
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return t, nil
}

// Approve answers the parked request with allow, or allow_session when all is
// set, which also auto-grants every later ask for this task.
func (r *Runner) Approve(id string, all bool) error {
	grant := crushapi.Allow
	if all {
		grant = crushapi.AllowSession
	}
	return r.answer(id, grant)
}

// Deny answers the parked request with deny. The denial is fed back to the
// model and recorded; it does not change the exit code (spec §3.3).
func (r *Runner) Deny(id string) error { return r.answer(id, crushapi.Deny) }

func (r *Runner) answer(id string, grant crushapi.GrantAnswer) error {
	r.mu.Lock()
	a := r.current
	if a == nil || a.id != id {
		r.mu.Unlock()
		return r.inactive(id)
	}
	if !a.awaiting {
		r.mu.Unlock()
		return ErrNotAwaiting
	}
	a.awaiting = false
	a.answers <- answer{grant: grant, by: "dashboard"}
	r.mu.Unlock()
	return nil
}

// Cancel drops a queued task or kills a running one by name (spec §5). The
// workspace keeps what was written.
func (r *Runner) Cancel(id string) error {
	r.mu.Lock()
	for i, queued := range r.queue {
		if queued != id {
			continue
		}
		r.queue = append(r.queue[:i], r.queue[i+1:]...)
		r.mu.Unlock()
		t, err := r.d.Store.Transition(id, taskstore.Cancelled, func(t *taskstore.Task) { t.FinishedAt = stamp(r.d.Now()) })
		if err != nil {
			return err
		}
		r.narrate(&t, KindState, "cancelled before it started")
		r.closeSubs(id)
		return nil
	}
	if a := r.current; a != nil && a.id == id {
		cancelled := a.cancelled
		if !cancelled {
			a.cancelled = true
			a.cancel <- struct{}{}
		}
		r.mu.Unlock()
		if cancelled {
			// The worker holds the job for a moment past the terminal write, so
			// current alone would answer "accepted" for a task that is already
			// finished. The record is the truth; a second cancel is idempotent
			// only while the first is still in flight.
			return r.terminal(id)
		}
		return nil
	}
	r.mu.Unlock()
	return r.inactive(id)
}

// terminal is ErrTerminal once the record has finished, nil while it has not.
// answer needs no equivalent: it clears awaiting before handing the worker the
// answer, so a second call is ErrNotAwaiting, which the HTTP layer maps to the
// same 409.
func (r *Runner) terminal(id string) error {
	t, err := r.d.Store.Load(id)
	if err == nil && taskstore.Terminal(t.State) {
		return ErrTerminal
	}
	return nil
}

// inactive explains why an id has no active task: unknown, or already finished.
func (r *Runner) inactive(id string) error {
	t, err := r.d.Store.Load(id)
	if err != nil {
		return err
	}
	if taskstore.Terminal(t.State) {
		return ErrTerminal
	}
	return ErrNotAwaiting
}

// Subscribe returns the task's live stream and a stop function. The channel
// closes when the task is terminal; a subscriber to an already-terminal task
// gets one final snapshot and close, and an unknown id a closed channel.
func (r *Runner) Subscribe(id string) (<-chan Narration, func()) {
	ch := make(chan Narration, 256)
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.d.Store.Load(id)
	if err != nil {
		close(ch)
		return ch, func() {}
	}
	if taskstore.Terminal(t.State) {
		ch <- Narration{At: r.d.Now(), Kind: KindState, Text: string(t.State), Task: &t}
		close(ch)
		return ch, func() {}
	}
	r.subs[id] = append(r.subs[id], ch)
	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		subs := r.subs[id]
		for i, s := range subs {
			if s == ch {
				r.subs[id] = append(subs[:i], subs[i+1:]...)
				return
			}
		}
	}
}

// narrate appends a narration line to the task's log and fans it out with a
// snapshot of t. A subscriber that cannot keep up loses lines rather than
// stalling the worker.
func (r *Runner) narrate(t *taskstore.Task, kind, text string) {
	snapshot := *t
	n := Narration{At: r.d.Now(), Kind: kind, Text: text, Task: &snapshot}
	_ = r.d.Store.AppendLog(t.ID, taskstore.LogEvent{At: n.At, Kind: "narration", Text: text})
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ch := range r.subs[t.ID] {
		select {
		case ch <- n:
		default:
		}
	}
}

func (r *Runner) closeSubs(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ch := range r.subs[id] {
		close(ch)
	}
	delete(r.subs, id)
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
