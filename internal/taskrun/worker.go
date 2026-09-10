package taskrun

// worker.go is the one goroutine that drives a task through the bridge. It
// keeps a shadow of the record (files, approvals, harness) ahead of disk,
// because the store's arbiter has no running → running edge for a mid-run
// append; the shadow is written into every Transition's mutate, so disk
// catches up at the next state change and subscribers see it live.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/approval"
	"github.com/MatrixMagician/VillaStraylight/internal/crushapi"
	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
)

func (r *Runner) work() {
	for {
		r.mu.Lock()
		if len(r.queue) == 0 {
			r.mu.Unlock()
			<-r.wake
			continue
		}
		a := &active{id: r.queue[0], answers: make(chan answer, 1), cancel: make(chan struct{}, 1)}
		r.queue = r.queue[1:]
		r.current = a
		r.mu.Unlock()

		r.run(a)

		r.mu.Lock()
		r.current = nil
		r.mu.Unlock()
	}
}

// job is the worker's per-task state.
type job struct {
	r            *Runner
	a            *active
	t            taskstore.Task
	sessionAllow bool
	pending      int
	pendingID    string
	runComplete  bool
	filesRead    []string
	seen         map[string]bool
}

func (r *Runner) run(a *active) {
	t, err := r.d.Store.Load(a.id)
	if err != nil || t.State != taskstore.Queued {
		return
	}
	j := &job{r: r, a: a, t: t, seen: map[string]bool{}, pending: -1}
	ctx := context.Background()

	cfg, err := r.d.LoadConfig()
	if err != nil {
		j.finish(taskstore.Refused, "refused: load config: "+err.Error())
		return
	}
	args, err := r.d.RenderArgs(cfg, j.t.Workspace, j.t.ID)
	if err != nil {
		j.finish(taskstore.Refused, "refused: "+err.Error())
		return
	}
	b, err := r.d.Launch(ctx, args)
	if err != nil {
		j.finish(taskstore.Refused, "refused: the sandbox could not start: "+err.Error())
		return
	}

	if !j.awaitReady(b) {
		return
	}

	if err := b.Send(crushapi.Command{Kind: crushapi.CmdPrompt, Prompt: j.t.Instruction + "\n\n" + grounding.CitationInstruction()}); err != nil {
		_ = b.Kill()
		j.finish(taskstore.Failed, "failed: could not send the instruction: "+err.Error())
		return
	}

	for {
		select {
		case <-a.cancel:
			j.killAndCancel(b)
			return
		case ans := <-a.answers:
			j.answerPending(b, ans)
		case ev, ok := <-b.Events():
			if !ok {
				j.settle(ctx, b)
				return
			}
			j.logHarness(ev)
			switch ev.Kind {
			case crushapi.KindPermissionRequest:
				if ev.Permission != nil {
					j.decide(b, *ev.Permission)
				}
			case crushapi.KindFile:
				if ev.File != nil {
					j.addFile(ev.File.Path)
				}
			case crushapi.KindRunComplete:
				j.runComplete = true
				j.narrate("run complete")
			case crushapi.KindFilesRead:
				j.filesRead = ev.Files
			case crushapi.KindBridgeError:
				j.narrate("bridge: " + ev.Error)
			}
		}
	}
}

// awaitReady reads until bridge_ready, moving the record to running. Anything
// before that is a refusal (spec §3.2), cancel included.
func (j *job) awaitReady(b Bridge) bool {
	for {
		select {
		case <-j.a.cancel:
			j.killAndCancel(b)
			return false
		case ev, ok := <-b.Events():
			if !ok {
				err := b.Wait()
				j.finish(taskstore.Refused, "refused: the bridge exited before it was ready: "+errText(err))
				return false
			}
			j.logHarness(ev)
			switch ev.Kind {
			case crushapi.KindBridgeReady:
				j.t.Harness = taskstore.Harness{Name: "crush", Version: ev.Version}
				started := stamp(j.r.d.Now())
				j.transition(taskstore.Running, func(t *taskstore.Task) { t.StartedAt = started })
				return true
			case crushapi.KindBridgeError:
				go func() { _ = b.Wait() }()
				j.finish(taskstore.Refused, "refused: "+ev.Error)
				return false
			}
		}
	}
}

// settle runs after the event stream ends: a run that never completed failed,
// a completed one is audited.
func (j *job) settle(ctx context.Context, b Bridge) {
	err := b.Wait()
	if !j.runComplete {
		j.finish(taskstore.Failed, "failed: the sandbox exited before the run completed: "+errText(err))
		return
	}
	if err != nil {
		j.narrate("the sandbox exited with an error after completing: " + err.Error())
	}
	j.audit(ctx)
}

func (j *job) killAndCancel(b Bridge) {
	if err := b.Kill(); err != nil {
		j.narrate("kill failed: " + err.Error())
	} else {
		go func() { _ = b.Wait() }()
	}
	j.finish(taskstore.Cancelled, "cancelled")
}

// decide answers one permission request from the table, the session grant, or
// by parking the task for the operator.
func (j *job) decide(b Bridge, p crushapi.PermissionRequest) {
	req := approval.Request{
		Action:    approval.Action(p.Action),
		Tool:      p.Tool,
		Path:      p.Path,
		Command:   commandParam(p.Params),
		Workspace: strings.TrimSuffix(guestWorkspace, "/"),
	}
	what := p.Action + " " + p.Tool
	if p.Path != "" {
		what += " " + guestRel(p.Path)
	}
	if req.Command != "" {
		what += ": " + req.Command
	}
	decision := approval.Decide(approval.Mode(j.t.Mode), req)
	if decision == approval.Ask && j.sessionAllow {
		now := stamp(j.r.d.Now())
		j.t.Approvals = append(j.t.Approvals, taskstore.Approval{
			Seq: len(j.t.Approvals) + 1, Tool: p.Tool, Action: p.Action, Path: guestRel(p.Path),
			AskedAt: now, Answer: string(crushapi.Allow), AnsweredAt: now, By: "session",
		})
		j.grant(b, p.ID, crushapi.Allow)
		j.narrate("allowed by the session grant: " + what)
		return
	}
	switch decision {
	case approval.Allow:
		j.grant(b, p.ID, crushapi.Allow)
		j.narrate("allowed: " + what)
	case approval.Deny:
		j.grant(b, p.ID, crushapi.Deny)
		j.narrate("denied by policy: " + what)
	case approval.Ask:
		j.t.Approvals = append(j.t.Approvals, taskstore.Approval{
			Seq: len(j.t.Approvals) + 1, Tool: p.Tool, Action: p.Action, Path: guestRel(p.Path),
			AskedAt: stamp(j.r.d.Now()),
		})
		j.pending = len(j.t.Approvals) - 1
		j.pendingID = p.ID
		j.transition(taskstore.AwaitingApproval, nil)
		j.narrate(fmt.Sprintf("awaiting approval #%d: %s", j.pending+1, what))
		j.r.mu.Lock()
		j.a.awaiting = true
		j.r.mu.Unlock()
	}
}

// answerPending records the operator's answer, forwards it, and resumes.
func (j *job) answerPending(b Bridge, ans answer) {
	if j.pending < 0 {
		return
	}
	row := &j.t.Approvals[j.pending]
	row.Answer = string(ans.grant)
	row.AnsweredAt = stamp(j.r.d.Now())
	row.By = ans.by
	if ans.grant == crushapi.AllowSession {
		j.sessionAllow = true
	}
	j.grant(b, j.pendingID, ans.grant)
	j.pending = -1
	j.transition(taskstore.Running, nil)
	j.narrate(fmt.Sprintf("approval #%d answered %s by %s", len(j.t.Approvals), ans.grant, ans.by))
}

func (j *job) grant(b Bridge, permissionID string, g crushapi.GrantAnswer) {
	if err := b.Send(crushapi.Command{Kind: crushapi.CmdGrant, PermissionID: permissionID, Answer: g}); err != nil {
		j.narrate("could not answer permission " + permissionID + ": " + err.Error())
	}
}

func (j *job) addFile(guestPath string) {
	rel := guestRel(guestPath)
	action := "modified"
	if !j.seen[rel] {
		j.seen[rel] = true
		action = "created"
	}
	j.t.Files = append(j.t.Files, taskstore.FileEvent{Path: rel, Action: action})
	j.narrate(action + " " + rel)
}

// audit runs the grounding second pass (spec §6) over every distinct file the
// task wrote, the sources being what the session read minus the document
// itself. checked:false, an audit error or any unsupported claim flags.
func (j *job) audit(ctx context.Context) {
	var sources []grounding.Source
	for _, p := range j.filesRead {
		rel := guestRel(p)
		if j.seen[rel] {
			continue
		}
		data, err := j.r.d.ReadFile(j.t.Workspace, rel)
		if err != nil {
			j.narrate("audit source unreadable: " + rel + ": " + err.Error())
			continue
		}
		sources = append(sources, grounding.Source{Path: rel, Content: string(data)})
	}

	g := taskstore.Grounding{Checked: true, Documents: []taskstore.GroundingDocument{}}
	flagged := false
	audited := map[string]bool{}
	for _, f := range j.t.Files {
		if audited[f.Path] {
			continue
		}
		audited[f.Path] = true
		var rep grounding.DocumentReport
		if data, err := j.r.d.ReadFile(j.t.Workspace, f.Path); err != nil {
			rep = grounding.DocumentReport{Path: f.Path, Err: err.Error()}
		} else {
			rep = j.r.d.Audit(ctx, grounding.Document{Path: f.Path, Content: string(data)}, sources)
		}
		doc := taskstore.GroundingDocument{Path: f.Path, Claims: rep.Claims, Unsupported: []taskstore.Unsupported{}}
		for _, u := range rep.Unsupported {
			doc.Unsupported = append(doc.Unsupported, taskstore.Unsupported{Claim: u.Claim, Reason: u.Reason})
		}
		g.Documents = append(g.Documents, doc)
		if !rep.Checked {
			g.Checked = false
			flagged = true
			j.narrate("audit could not run for " + f.Path + ": " + rep.Err)
			continue
		}
		if rep.Err != "" || len(rep.Unsupported) > 0 {
			flagged = true
		}
		line := fmt.Sprintf("audit %s: %d claims, %d unsupported", f.Path, rep.Claims, len(rep.Unsupported))
		if rep.Err != "" {
			line += " (" + rep.Err + ")"
		}
		j.narrate(line)
	}
	j.t.Grounding = g
	if flagged {
		j.finish(taskstore.Flagged, "flagged: the audit found claims it could not support; the document is left as written")
		return
	}
	j.finish(taskstore.Done, "done")
}

// transition writes the shadow into the record under a legal edge. A refused
// edge is a bug; it is logged and the task failed rather than left inconsistent.
func (j *job) transition(to taskstore.State, extra func(*taskstore.Task)) {
	t, err := j.r.d.Store.Transition(j.t.ID, to, func(t *taskstore.Task) {
		t.Harness = j.t.Harness
		t.Files = j.t.Files
		t.Approvals = j.t.Approvals
		t.Grounding = j.t.Grounding
		if extra != nil {
			extra(t)
		}
	})
	if err != nil {
		j.narrate("bug: " + err.Error())
		if to != taskstore.Failed {
			j.finish(taskstore.Failed, "failed: an illegal state transition was attempted")
		}
		return
	}
	j.t = t
	j.r.narrate(&j.t, KindState, string(to))
}

func (j *job) finish(to taskstore.State, text string) {
	finished := stamp(j.r.d.Now())
	t, err := j.r.d.Store.Transition(j.t.ID, to, func(t *taskstore.Task) {
		t.Harness = j.t.Harness
		t.Files = j.t.Files
		t.Approvals = j.t.Approvals
		t.Grounding = j.t.Grounding
		t.FinishedAt = finished
	})
	if err != nil {
		j.narrate("bug: " + err.Error())
		if to != taskstore.Failed {
			j.finish(taskstore.Failed, "failed: an illegal state transition was attempted")
			return
		}
		j.r.closeSubs(j.t.ID)
		return
	}
	j.t = t
	j.r.narrate(&j.t, KindState, text)
	j.r.closeSubs(j.t.ID)
}

func (j *job) narrate(text string) { j.r.narrate(&j.t, KindNarration, text) }

// logHarness appends the harness event verbatim so the log is the transcript
// that survives the sandbox (spec §3.2).
func (j *job) logHarness(ev crushapi.Event) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_ = j.r.d.Store.AppendLog(j.t.ID, taskstore.LogEvent{At: j.r.d.Now(), Kind: "harness", Raw: raw})
}

// commandParam lifts the bash tool's command out of the permission params.
func commandParam(params json.RawMessage) string {
	var p struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(params, &p)
	return p.Command
}

// guestRel turns a sandbox path into the workspace-relative path the record
// and the host-side read use.
func guestRel(guestPath string) string {
	return strings.TrimPrefix(guestPath, guestWorkspace)
}

func errText(err error) string {
	if err == nil {
		return "no error reported"
	}
	return err.Error()
}
