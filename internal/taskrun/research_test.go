package taskrun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/approval"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
	"github.com/MatrixMagician/VillaStraylight/internal/llm"
	"github.com/MatrixMagician/VillaStraylight/internal/research"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
	"github.com/MatrixMagician/VillaStraylight/internal/websafe"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// webPage is a page as villa-websafe would return it: sanitized, normalized and
// fenced by the real loader.
func webPage(t *testing.T, url, body string) research.Page {
	t.Helper()
	client := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("<title>" + url + "</title>" + body))}, nil
	})}
	pages := websafe.NewLoader(websafe.Deps{Client: client}, websafe.DefaultBounds()).Load(t.Context(), []string{url})
	if len(pages) != 1 {
		t.Fatalf("websafe produced %d pages", len(pages))
	}
	p := pages[0]
	return research.Page{URL: p.Source, Title: p.Title, Content: p.Content, Flagged: p.Verdict.Detected, Rules: p.Verdict.Rules}
}

// web is the scripted host side of a research task. Each knob can be replaced
// per test; the defaults answer one clean question in one round.
type web struct {
	mu       sync.Mutex
	search   func(ctx context.Context, q string) ([]string, error)
	fetch    func(ctx context.Context, urls []string) ([]research.Page, error)
	complete func(ctx context.Context, req llm.ChatRequest) (string, error)
	seen     []llm.ChatRequest
}

func newWeb(t *testing.T) *web {
	w := &web{}
	w.search = func(context.Context, string) ([]string, error) { return []string{"https://a.example/x"}, nil }
	w.fetch = func(_ context.Context, urls []string) ([]research.Page, error) {
		var out []research.Page
		for _, u := range urls {
			out = append(out, webPage(t, u, "ROCm 7.2 added gfx1151."))
		}
		return out, nil
	}
	w.complete = func(_ context.Context, req llm.ChatRequest) (string, error) {
		switch sys := req.Messages[0].Content; {
		case strings.Contains(sys, "plan web searches"):
			return "QUERY: rocm 7.2", nil
		case strings.Contains(sys, "reading web pages"):
			return "It added gfx1151 [1].", nil
		default:
			return "ROCm 7.2 added gfx1151 [1].", nil
		}
	}
	return w
}

func (w *web) deps() WebDeps {
	return WebDeps{
		Search: func(ctx context.Context, _ config.VillaConfig, q string) ([]string, error) { return w.search(ctx, q) },
		Fetch: func(ctx context.Context, _ config.VillaConfig, urls []string) ([]research.Page, error) {
			return w.fetch(ctx, urls)
		},
		Complete: func(ctx context.Context, _ config.VillaConfig, req llm.ChatRequest) (string, error) {
			w.mu.Lock()
			w.seen = append(w.seen, req)
			w.mu.Unlock()
			return w.complete(ctx, req)
		},
	}
}

// researchHarness is the workspace harness with web search switched on and a
// scripted web; the sandbox pieces stay unused.
func researchHarness(t *testing.T) (*harness, *web) {
	t.Helper()
	h := newHarness(t)
	w := newWeb(t)
	h.deps.WebSearchOn = func(config.VillaConfig) bool { return true }
	h.deps.Web = w.deps()
	h.start()
	return h, w
}

func (h *harness) submitResearch(req ResearchRequest) taskstore.Task {
	h.t.Helper()
	task, err := h.r.SubmitResearch(context.Background(), req)
	if err != nil {
		h.t.Fatalf("SubmitResearch: %v", err)
	}
	return task
}

// TestResearchTaskRunsToDone guards the whole lifecycle: a queued research
// record with its budget, then running, then done with the sources, the report
// file, the audit and the taint on the record.
func TestResearchTaskRunsToDone(t *testing.T) {
	h, _ := researchHarness(t)
	queued := h.submitResearch(ResearchRequest{Question: "What changed in ROCm 7.2?"})
	if queued.State != taskstore.Queued || !queued.IsResearch() || queued.Workspace != "" || queued.Instruction != "What changed in ROCm 7.2?" {
		t.Fatalf("submitted record = %+v", queued)
	}
	if got := queued.Research.Budget; got.Rounds != research.DefaultRounds || got.Minutes != research.DefaultMinutes {
		t.Errorf("budget = %+v, want the defaults", got)
	}
	if queued.Harness.Name != "research" || queued.Harness.Version != research.Version {
		t.Errorf("harness = %+v", queued.Harness)
	}

	done := h.waitState(queued.ID, taskstore.Done)
	if done.Exit == nil || *done.Exit != 0 || done.Taint != approval.TaintWeb {
		t.Errorf("exit = %v taint = %q, want 0 and web", done.Exit, done.Taint)
	}
	if len(done.Research.Sources) != 1 || done.Research.Sources[0].URL != "https://a.example/x" || done.Research.Sources[0].N != 1 {
		t.Errorf("sources = %+v", done.Research.Sources)
	}
	if !done.Grounding.Checked || len(done.Grounding.Documents) != 1 || done.Grounding.Documents[0].Path != done.Research.Report {
		t.Errorf("grounding = %+v report = %q", done.Grounding, done.Research.Report)
	}
	report, err := h.store.ReadReport(done.ID)
	if err != nil || !strings.Contains(string(report), "## Sources") || !strings.Contains(string(report), "https://a.example/x") {
		t.Errorf("report = %q, %v", report, err)
	}
	log := h.logText(done.ID)
	for _, want := range []string{"budget: 2 rounds", "tainted by web", "source [1]"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

// TestResearchTaintLandsBeforeAPageReachesTheModel guards ADR-0040's ordering at
// the runner: when the model is first asked to read a page, the record on disk
// already says web.
func TestResearchTaintLandsBeforeAPageReachesTheModel(t *testing.T) {
	h, w := researchHarness(t)
	var mu sync.Mutex
	var taintAtRead approval.Taint
	inner := w.complete
	w.complete = func(ctx context.Context, req llm.ChatRequest) (string, error) {
		if strings.Contains(req.Messages[0].Content, "reading web pages") {
			tasks, _ := h.store.List()
			mu.Lock()
			if len(tasks) == 1 {
				taintAtRead = tasks[0].Taint
			}
			mu.Unlock()
		}
		return inner(ctx, req)
	}
	task := h.submitResearch(ResearchRequest{Question: "q"})
	h.waitState(task.ID, taskstore.Done)
	mu.Lock()
	defer mu.Unlock()
	if taintAtRead != approval.TaintWeb {
		t.Errorf("taint when the first page was read = %q, want web", taintAtRead)
	}
}

// TestResearchWithNoResultsEndsFlaggedWithExit2 guards the decision: zero
// sources is flagged, never done, and the exit code is taskstore.Exit's.
func TestResearchWithNoResultsEndsFlaggedWithExit2(t *testing.T) {
	h, w := researchHarness(t)
	w.search = func(context.Context, string) ([]string, error) { return nil, nil }
	task := h.submitResearch(ResearchRequest{Question: "q"})
	got := h.waitState(task.ID, taskstore.Flagged)
	if got.Exit == nil || *got.Exit != taskstore.Exit(taskstore.Flagged) || *got.Exit != 2 {
		t.Errorf("exit = %v, want 2", got.Exit)
	}
	if got.Research.Report != "" {
		t.Errorf("a run with no sources has no report, got %q", got.Research.Report)
	}
	if _, err := h.store.ReadReport(got.ID); !errors.Is(err, taskstore.ErrNotFound) {
		t.Errorf("ReadReport = %v, want not found", err)
	}
	if !strings.Contains(h.logText(got.ID), "no results") {
		t.Errorf("the reason must be in the log:\n%s", h.logText(got.ID))
	}
}

// TestResearchWithAnUnreachableSearchEndsFailed guards the infrastructure
// verdict and its exit code.
func TestResearchWithAnUnreachableSearchEndsFailed(t *testing.T) {
	h, w := researchHarness(t)
	w.search = func(context.Context, string) ([]string, error) { return nil, errors.New("curl exit 7") }
	task := h.submitResearch(ResearchRequest{Question: "q"})
	got := h.waitState(task.ID, taskstore.Failed)
	if got.Exit == nil || *got.Exit != 1 || !strings.Contains(h.logText(got.ID), "villa doctor") {
		t.Errorf("exit = %v log:\n%s", got.Exit, h.logText(got.ID))
	}
}

// TestResearchUnsupportedClaimsEndFlagged guards that the audit's findings reach
// the record's grounding and flag the task.
func TestResearchUnsupportedClaimsEndFlagged(t *testing.T) {
	h, _ := researchHarness(t)
	h.audit = func(doc grounding.Document, _ []grounding.Source) grounding.DocumentReport {
		return grounding.DocumentReport{Path: doc.Path, Checked: true, Claims: 2,
			Unsupported: []grounding.Unsupported{{Claim: "drops gfx1100", Reason: "no passage"}}}
	}
	task := h.submitResearch(ResearchRequest{Question: "q"})
	got := h.waitState(task.ID, taskstore.Flagged)
	docs := got.Grounding.Documents
	if len(docs) != 1 || docs[0].Claims != 2 || len(docs[0].Unsupported) != 1 {
		t.Errorf("grounding = %+v", got.Grounding)
	}
	if got.Research.Report == "" {
		t.Error("a flagged report is still written")
	}
}

// TestSubmitResearchRefusesWhenWebSearchIsOff guards the gate: a refused record
// with the remediation in its log, exit 1, nothing queued.
func TestSubmitResearchRefusesWhenWebSearchIsOff(t *testing.T) {
	h := newHarness(t)
	h.deps.WebSearchOn = func(config.VillaConfig) bool { return false }
	h.deps.Web = newWeb(t).deps()
	h.start()
	task := h.submitResearch(ResearchRequest{Question: "q"})
	if task.State != taskstore.Refused || task.Exit == nil || *task.Exit != 1 {
		t.Fatalf("record = %+v, want refused with exit 1", task)
	}
	if !strings.Contains(h.logText(task.ID), "villa install --web-search") {
		t.Errorf("the refusal must carry its remediation:\n%s", h.logText(task.ID))
	}
}

// TestSubmitResearchRefusesAMalformedRequest guards the two errors that are not
// records: an empty question and a budget over its cap.
func TestSubmitResearchRefusesAMalformedRequest(t *testing.T) {
	h, _ := researchHarness(t)
	for name, req := range map[string]ResearchRequest{
		"empty question":  {Question: "  "},
		"too many rounds": {Question: "q", Rounds: research.MaxRounds + 1},
		"too long":        {Question: "q", Minutes: research.MaxMinutes + 1},
	} {
		if _, err := h.r.SubmitResearch(context.Background(), req); err == nil {
			t.Errorf("%s: SubmitResearch must refuse", name)
		}
	}
	if tasks, _ := h.store.List(); len(tasks) != 0 {
		t.Errorf("a malformed request must leave no record, got %d", len(tasks))
	}
}

// TestResearchBudgetFlagsReachTheRecord guards that a raised budget is what the
// record, and so the terminal, reports.
func TestResearchBudgetFlagsReachTheRecord(t *testing.T) {
	h, _ := researchHarness(t)
	task := h.submitResearch(ResearchRequest{Question: "q", Rounds: 3, Minutes: 20})
	if got := task.Research.Budget; got.Rounds != 3 || got.Minutes != 20 {
		t.Errorf("budget = %+v", got)
	}
	h.waitState(task.ID, taskstore.Done)
}

// TestCancelStopsARunningResearchTask guards that the existing cancel reaches a
// task with no sandbox: the in-flight call is cancelled and the record ends
// cancelled.
func TestCancelStopsARunningResearchTask(t *testing.T) {
	h, w := researchHarness(t)
	started := make(chan struct{})
	w.complete = func(ctx context.Context, _ llm.ChatRequest) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	task := h.submitResearch(ResearchRequest{Question: "q"})
	<-started
	if err := h.r.Cancel(task.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got := h.waitState(task.ID, taskstore.Cancelled)
	if got.Exit == nil || *got.Exit != 1 {
		t.Errorf("exit = %v", got.Exit)
	}
}

// TestCloseInterruptsARunningResearchTask guards the service stopping under a
// task: it ends interrupted, not failed and not done.
func TestCloseInterruptsARunningResearchTask(t *testing.T) {
	h, w := researchHarness(t)
	started := make(chan struct{})
	w.complete = func(ctx context.Context, _ llm.ChatRequest) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	task := h.submitResearch(ResearchRequest{Question: "q"})
	<-started
	h.r.Close()
	if got, _ := h.store.Load(task.ID); got.State != taskstore.Interrupted {
		t.Errorf("state = %q, want interrupted", got.State)
	}
}

// TestRecoverInterruptsAResearchTaskWithoutKillingAContainer guards ADR-0045
// §7: recovery marks it interrupted like any task, never resumes it, and does
// not go looking for a sandbox container it never had.
func TestRecoverInterruptsAResearchTaskWithoutKillingAContainer(t *testing.T) {
	h := newHarness(t)
	rec := taskstore.Task{
		ID: "20260914-093000-7e1c", Instruction: "q", State: taskstore.Queued, SubmittedAt: "2026-09-14T09:30:00Z",
		Kind: taskstore.KindResearch, Research: &taskstore.Research{},
	}
	if err := h.store.Create(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.Transition(rec.ID, taskstore.Running, nil); err != nil {
		t.Fatal(err)
	}
	h.start()
	if err := h.r.Recover(); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.store.Load(rec.ID); got.State != taskstore.Interrupted {
		t.Errorf("state = %q, want interrupted", got.State)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.killed) != 0 {
		t.Errorf("recovery killed %v for a research task", h.killed)
	}
}

// TestResearchTasksRunOneAtATime guards the global one-at-a-time rule for the
// new kind: a second submission stays queued until the first finishes.
func TestResearchTasksRunOneAtATime(t *testing.T) {
	h, w := researchHarness(t)
	release := make(chan struct{})
	started := make(chan struct{}, 16)
	inner := w.complete
	w.complete = func(ctx context.Context, req llm.ChatRequest) (string, error) {
		started <- struct{}{}
		<-release
		return inner(ctx, req)
	}
	first := h.submitResearch(ResearchRequest{Question: "one"})
	second := h.submitResearch(ResearchRequest{Question: "two"})
	<-started
	time.Sleep(20 * time.Millisecond)
	if got, _ := h.store.Load(second.ID); got.State != taskstore.Queued {
		t.Errorf("second task state = %q while the first runs, want queued", got.State)
	}
	close(release)
	h.waitState(first.ID, taskstore.Done)
	h.waitState(second.ID, taskstore.Done)
}
