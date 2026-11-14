package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
)

type researchFixture struct {
	deps   *researchDeps
	api    *fakeTaskAPI
	report string
}

func newResearchFixture(t *testing.T, cfgMut func(*config.VillaConfig)) *researchFixture {
	t.Helper()
	cfg := config.VillaConfig{WebSearchEnabled: true}
	if cfgMut != nil {
		cfgMut(&cfg)
	}
	f := newFakeTaskAPI(t)
	fx := &researchFixture{api: f, report: "ROCm 7.2 added gfx1151 [1].\n\n----- villa sources tok -----\n1. https://a.example/x\n"}
	fx.deps = &researchDeps{
		load: func() (config.VillaConfig, error) { return cfg, nil },
		api: func(config.VillaConfig) taskAPIDeps {
			return taskAPIDeps{base: f.srv.URL, client: f.srv.Client()}
		},
		readReport: func(id string) ([]byte, error) { return []byte(fx.report), nil },
		reportPath: func(rel string) string { return "/data/villa/" + rel },
		readLog:    func(string) ([]taskstore.LogEvent, error) { return nil, nil },
	}
	return fx
}

func researchRecord(state taskstore.State) taskstore.Task {
	t := researchGoldenTask()
	t.State = state
	if taskstore.Terminal(state) {
		exit := taskstore.Exit(state)
		t.Exit = &exit
	} else {
		t.Exit = nil
		t.FinishedAt = ""
	}
	return t
}

// researchGoldenTask is a research record with a report and two sources.
func researchGoldenTask() taskstore.Task {
	return taskstore.Task{
		ID: "20260914-093000-7e1c", Instruction: "What changed in ROCm 7.2?", State: taskstore.Done,
		SubmittedAt: "2026-09-14T09:30:00Z", Harness: taskstore.Harness{Name: "research", Version: "1"},
		Kind: taskstore.KindResearch, Schema: 1,
		Research: &taskstore.Research{
			Budget:  taskstore.ResearchBudget{Rounds: 2, Minutes: 10},
			Report:  "tasks/20260914-093000-7e1c.report.txt",
			Sources: []taskstore.ResearchSource{{N: 1, URL: "https://a.example/x", Title: "A"}},
		},
	}
}

// TestRunResearchExitCodeTable guards that the record's terminal state decides the
// exit code through taskstore.Exit, as villa work does: done 0, flagged 2, the
// rest 1.
func TestRunResearchExitCodeTable(t *testing.T) {
	for state, want := range map[taskstore.State]int{
		taskstore.Done: exitPass, taskstore.Flagged: exitWarn, taskstore.Failed: exitBlocked,
		taskstore.Refused: exitBlocked, taskstore.Cancelled: exitBlocked, taskstore.Interrupted: exitBlocked,
	} {
		t.Run(string(state), func(t *testing.T) {
			fx := newResearchFixture(t, nil)
			queued := researchRecord(taskstore.Queued)
			final := researchRecord(state)
			fx.api.record = final
			fx.api.frames = []string{narrationFrame("round 1 of 2", queued), stateFrame(final)}
			cmd, _, errOut := lifecycleTestCmd()
			if code := runResearch(cmd, "What changed in ROCm 7.2?", researchOpts{}, fx.deps); code != want {
				t.Fatalf("exit = %d, want %d; stderr=%q", code, want, errOut.String())
			}
		})
	}
}

// TestRunResearchPrintsTheBudgetThenFollowsAndPrintsTheReport guards the start
// and the end: the budget (from the record the runner accepted) and the privacy
// line come before any narration, then the narration, then the report text and
// its path.
func TestRunResearchPrintsTheBudgetThenFollowsAndPrintsTheReport(t *testing.T) {
	fx := newResearchFixture(t, nil)
	queued := researchRecord(taskstore.Queued)
	fx.api.record = researchRecord(taskstore.Done)
	fx.api.frames = []string{narrationFrame("round 1 of 2: \"rocm\"", queued), stateFrame(researchRecord(taskstore.Done))}
	cmd, out, errOut := lifecycleTestCmd()
	if code := runResearch(cmd, "What changed in ROCm 7.2?", researchOpts{rounds: 2, minutes: 10}, fx.deps); code != exitPass {
		t.Fatalf("exit = %d; stderr=%q", code, errOut.String())
	}
	got := out.String()
	order := []string{"budget: 2 rounds, 10 minutes", "SearXNG", "round 1 of 2", "report: /data/villa/tasks/20260914-093000-7e1c.report.txt", "ROCm 7.2 added gfx1151 [1].", "done (exit 0)"}
	last := -1
	for _, want := range order {
		i := strings.Index(got, want)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of order in:\n%s", want, got)
		}
		last = i
	}
	if len(fx.api.researchSubmits) != 1 || fx.api.researchSubmits[0]["question"] != "What changed in ROCm 7.2?" ||
		fx.api.researchSubmits[0]["rounds"] != float64(2) || fx.api.researchSubmits[0]["minutes"] != float64(10) {
		t.Errorf("submits = %v", fx.api.researchSubmits)
	}
}

// TestRunResearchFlaggedPrintsWhatFlaggedIt guards that a flagged task shows the
// findings the audit and the report check recorded.
func TestRunResearchFlaggedPrintsWhatFlaggedIt(t *testing.T) {
	fx := newResearchFixture(t, nil)
	final := researchRecord(taskstore.Flagged)
	final.Grounding = taskstore.Grounding{Checked: true, Documents: []taskstore.GroundingDocument{{
		Path: final.Research.Report, Claims: 3, Unsupported: []taskstore.Unsupported{{Claim: "drops gfx1100", Reason: "no passage"}},
	}}}
	fx.api.record = final
	fx.api.frames = []string{stateFrame(final)}
	cmd, out, _ := lifecycleTestCmd()
	if code := runResearch(cmd, "q", researchOpts{}, fx.deps); code != exitWarn {
		t.Fatalf("exit = %d, want %d", code, exitWarn)
	}
	if !strings.Contains(out.String(), `flagged: "drops gfx1100" (no passage)`) {
		t.Errorf("output = %q", out.String())
	}
}

// TestRunResearchRefusals guards the refusals that happen before anything is
// submitted: each exits 1, names its remediation, and posts nothing.
func TestRunResearchRefusals(t *testing.T) {
	cases := map[string]struct {
		mut   func(*config.VillaConfig)
		opts  researchOpts
		want  string
		setup func(*researchFixture)
	}{
		"web search off":   {mut: func(c *config.VillaConfig) { c.WebSearchEnabled = false }, want: "villa install --web-search"},
		"rounds over cap":  {opts: researchOpts{rounds: 9}, want: "rounds must be between 1 and 4"},
		"minutes over cap": {opts: researchOpts{minutes: 99}, want: "minutes must be between 1 and 30"},
		"dashboard down": {want: "systemctl --user start villa-dashboard.service", setup: func(fx *researchFixture) {
			dead := httptest.NewServer(http.NewServeMux())
			base := dead.URL
			dead.Close()
			fx.deps.api = func(config.VillaConfig) taskAPIDeps { return taskAPIDeps{base: base, client: http.DefaultClient} }
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newResearchFixture(t, tc.mut)
			if tc.setup != nil {
				tc.setup(fx)
			}
			cmd, _, errOut := lifecycleTestCmd()
			if code := runResearch(cmd, "q", tc.opts, fx.deps); code != exitBlocked {
				t.Fatalf("exit = %d, want %d", code, exitBlocked)
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("stderr = %q, want %q", errOut.String(), tc.want)
			}
			if len(fx.api.researchSubmits) != 0 {
				t.Errorf("submits = %v, want none", fx.api.researchSubmits)
			}
		})
	}
}

// TestRunResearchDetachPrintsIDAndBudget guards --detach: the id alone on stdout
// (so a script can capture it), the budget on stderr, exit 0, no stream opened.
func TestRunResearchDetachPrintsIDAndBudget(t *testing.T) {
	fx := newResearchFixture(t, nil)
	fx.api.record = researchRecord(taskstore.Queued)
	cmd, out, errOut := lifecycleTestCmd()
	if code := runResearch(cmd, "q", researchOpts{detach: true}, fx.deps); code != exitPass {
		t.Fatalf("exit = %d", code)
	}
	if strings.TrimSpace(out.String()) != "20260914-093000-7e1c" || !strings.Contains(errOut.String(), "budget: 2 rounds, 10 minutes") {
		t.Errorf("stdout = %q stderr = %q", out.String(), errOut.String())
	}
}

// TestRunResearchReadsAQuestionFromStdin guards "-" as the question, so a long one
// need not be shell-quoted.
func TestRunResearchReadsAQuestionFromStdin(t *testing.T) {
	fx := newResearchFixture(t, nil)
	fx.api.record = researchRecord(taskstore.Queued)
	cmd, _, _ := lifecycleTestCmd()
	cmd.SetIn(strings.NewReader("  a long\nquestion \n"))
	if code := runResearch(cmd, "-", researchOpts{detach: true}, fx.deps); code != exitPass {
		t.Fatalf("exit = %d", code)
	}
	if got := fx.api.researchSubmits[0]["question"]; got != "a long\nquestion" {
		t.Errorf("question = %q", got)
	}
}

// TestTaskShowAndListForAResearchTask guards the task noun: --json is the record
// and the list verbatim (goldens), the human show names kind, question, budget,
// sources and the report path, and the list row says research where a workspace
// would be.
func TestTaskShowAndListForAResearchTask(t *testing.T) {
	d, f := newTaskFixture(t)
	d.reportPath = func(rel string) string { return "/data/villa/" + rel }

	show := readTaskGolden(t, "task-show-research.golden.json")
	f.showRaw = show
	cmd, out, _ := lifecycleTestCmd()
	if code := runTaskShow(cmd, "20260914-093000-7e1c", true, d); code != exitPass || out.String() != string(show) {
		t.Fatalf("show --json = %d %q, want the record verbatim", code, out.String())
	}
	cmd, out, _ = lifecycleTestCmd()
	runTaskShow(cmd, "20260914-093000-7e1c", false, d)
	for _, want := range []string{"kind", "research", "question", "What changed in the ROCm 7.2 release?", "budget", "2 rounds, 10 minutes",
		"source #1", "https://rocm.docs.amd.com/en/latest/release.html", "source #2", "flagged by websafe: ignore-previous",
		"report", "/data/villa/tasks/20260914-093000-7e1c.report.txt", "taint", "ROCm 7.2 drops gfx1100"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("human show lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "workspace") || strings.Contains(out.String(), "mode") {
		t.Errorf("a research record has no workspace or mode row:\n%s", out.String())
	}

	list := readTaskGolden(t, "task-list-research.golden.json")
	f.listRaw = list
	cmd, out, _ = lifecycleTestCmd()
	if code := runTaskList(cmd, true, d); code != exitPass || out.String() != string(list) {
		t.Fatalf("list --json = %d %q", code, out.String())
	}
	cmd, out, _ = lifecycleTestCmd()
	runTaskList(cmd, false, d)
	if !strings.Contains(out.String(), "research") || !strings.Contains(out.String(), "/home/dev/reports") {
		t.Errorf("list table = %q", out.String())
	}
}
