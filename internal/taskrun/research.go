package taskrun

// research.go is the runner's second kind of job (ADR-0045): a research task has
// no sandbox, no bridge and no approvals. The worker drives internal/research's
// round loop against the host's web reach (Deps.Web), taints the record before
// the first fetch, writes the report artifact, and ends the record under the same
// state machine and exit codes as any task.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/approval"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
	"github.com/MatrixMagician/VillaStraylight/internal/llm"
	"github.com/MatrixMagician/VillaStraylight/internal/research"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
)

// ResearchRequest is one `villa research` submission. A zero Rounds or Minutes is
// the default, a value over its cap is refused.
type ResearchRequest struct {
	Question string
	Rounds   int
	Minutes  int
}

// webSearchOffRemediation is the refusal text for a host without web search.
const webSearchOffRemediation = "refused: web search is off, so there is no SearXNG to search and no villa-websafe to read pages. Turn it on with: villa install --web-search"

// SubmitResearch refuses or queues one research task. A malformed request (an
// empty question, a budget over its cap) is an error and leaves no record, as an
// empty instruction does for Submit; a host with web search off is a refused
// record carrying its remediation, so the terminal's exit code comes from the
// record alone.
func (r *Runner) SubmitResearch(_ context.Context, req ResearchRequest) (taskstore.Task, error) {
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return taskstore.Task{}, errors.New("taskrun: empty question")
	}
	budget, err := research.Budget{Rounds: req.Rounds, Minutes: req.Minutes}.Resolve()
	if err != nil {
		return taskstore.Task{}, fmt.Errorf("taskrun: %w", err)
	}
	cfg, err := r.d.LoadConfig()
	if err != nil {
		return taskstore.Task{}, fmt.Errorf("taskrun: load config: %w", err)
	}

	now := r.d.Now()
	t := taskstore.Task{
		ID:          taskstore.NewID(now, r.d.Rand),
		Instruction: question,
		State:       taskstore.Queued,
		SubmittedAt: stamp(now),
		Harness:     taskstore.Harness{Name: "research", Version: research.Version},
		Kind:        taskstore.KindResearch,
		Research: &taskstore.Research{
			Budget:  taskstore.ResearchBudget{Rounds: budget.Rounds, Minutes: budget.Minutes},
			Sources: []taskstore.ResearchSource{},
		},
	}
	refusal := ""
	if !r.d.WebSearchOn(cfg) {
		refusal = webSearchOffRemediation
	}
	return r.admit(t, refusal, now)
}

// runResearch is the worker's whole job for a research task.
func (r *Runner) runResearch(a *active, t taskstore.Task) {
	j := &job{r: r, a: a, t: t, seen: map[string]bool{}, pending: -1}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.mu.Lock()
	a.ctxCancel = cancel
	// A cancel that arrived before the context existed had nothing to cancel.
	if a.cancelled {
		cancel()
	}
	r.mu.Unlock()

	cfg, err := r.d.LoadConfig()
	if err != nil {
		j.finish(taskstore.Refused, "refused: load config: "+err.Error())
		return
	}
	if !r.d.WebSearchOn(cfg) {
		j.finish(taskstore.Refused, webSearchOffRemediation)
		return
	}
	b := j.t.Research.Budget
	started := stamp(r.d.Now())
	j.transition(taskstore.Running, func(t *taskstore.Task) { t.StartedAt = started })
	j.narrate(fmt.Sprintf("budget: %d rounds, %d minutes (soft); up to %d queries and %d pages per round, %d KiB of text per page",
		b.Rounds, b.Minutes, research.QueriesPerRound, research.PagesPerRound, research.PageBytes>>10))

	res := research.Run(ctx, r.researchDeps(cfg, j), research.Request{
		Question: j.t.Instruction,
		Budget:   research.Budget{Rounds: b.Rounds, Minutes: b.Minutes},
	})
	if ctx.Err() != nil {
		j.finishAfterCancel()
		return
	}
	j.settleResearch(res)
}

// researchDeps binds the loop's seam: the host's web reach for this task's config,
// the runner's audit, the taint on this task's record, and this job's narration.
func (r *Runner) researchDeps(cfg config.VillaConfig, j *job) research.Deps {
	return research.Deps{
		Search: func(ctx context.Context, q string) ([]string, error) { return r.d.Web.Search(ctx, cfg, q) },
		Fetch: func(ctx context.Context, urls []string) ([]research.Page, error) {
			return r.d.Web.Fetch(ctx, cfg, urls)
		},
		Complete: func(ctx context.Context, req llm.ChatRequest) (string, error) {
			return r.d.Web.Complete(ctx, cfg, req)
		},
		Audit:   r.d.Audit,
		Token:   r.d.Token,
		Taint:   func() error { return r.Taint(j.t.ID, approval.TaintWeb) },
		Narrate: j.narrate,
		Now:     r.d.Now,
	}
}

// settleResearch writes the report, stamps the sources and the audit into the
// record, and ends the task on the state the loop's outcome names.
func (j *job) settleResearch(res research.Result) {
	rs := j.t.Research
	rs.Sources = make([]taskstore.ResearchSource, len(res.Sources))
	for i, s := range res.Sources {
		rs.Sources[i] = taskstore.ResearchSource{N: s.N, URL: s.URL, Title: s.Title, Flagged: s.Flagged, Rules: s.Rules}
	}
	if res.Report != "" {
		rel, err := j.r.d.Store.WriteReport(j.t.ID, []byte(res.Report))
		if err != nil {
			j.finish(taskstore.Failed, "failed: could not write the report: "+err.Error())
			return
		}
		rs.Report = rel
		j.t.Grounding = researchGrounding(rel, res.Grounding)
	}

	switch res.Outcome {
	case research.Done:
		j.finish(taskstore.Done, "done")
	case research.Flagged:
		j.finish(taskstore.Flagged, "flagged: "+res.Reason)
	default:
		j.finish(taskstore.Failed, "failed: "+res.Reason)
	}
}

// researchGrounding maps the loop's audit onto the record's grounding block, one
// document: the report.
func researchGrounding(path string, rep grounding.DocumentReport) taskstore.Grounding {
	doc := taskstore.GroundingDocument{Path: path, Claims: rep.Claims, Unsupported: []taskstore.Unsupported{}}
	for _, u := range rep.Unsupported {
		doc.Unsupported = append(doc.Unsupported, taskstore.Unsupported{Claim: u.Claim, Reason: u.Reason})
	}
	return taskstore.Grounding{Checked: rep.Checked, Documents: []taskstore.GroundingDocument{doc}}
}
