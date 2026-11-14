package taskstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/approval"
)

const researchID = "20260914-093000-7e1c"

// researchTask is the fixed research record the golden freezes: a finished
// task with two sources, one flagged by websafe, tainted by the web.
func researchTask() Task {
	exit := 2
	return Task{
		ID:          researchID,
		Instruction: "What changed in the ROCm 7.2 release?",
		State:       Flagged,
		Exit:        &exit,
		SubmittedAt: "2026-09-14T09:30:00Z",
		StartedAt:   "2026-09-14T09:30:01Z",
		FinishedAt:  "2026-09-14T09:36:40Z",
		Harness:     Harness{Name: "research", Version: "1"},
		Files:       []FileEvent{},
		Approvals:   []Approval{},
		Grounding: Grounding{Checked: true, Documents: []GroundingDocument{{
			Path: "tasks/" + researchID + ".report.md", Claims: 4,
			Unsupported: []Unsupported{{Claim: "ROCm 7.2 drops gfx1100", Reason: "no passage says so"}},
		}}},
		Taint: approval.TaintWeb,
		Kind:  KindResearch,
		Research: &Research{
			Budget: ResearchBudget{Rounds: 2, Minutes: 10},
			Report: "tasks/" + researchID + ".report.md",
			Sources: []ResearchSource{
				{N: 1, URL: "https://rocm.docs.amd.com/en/latest/release.html", Title: "ROCm release notes"},
				{N: 2, URL: "https://example.org/post", Title: "A post", Flagged: true, Rules: []string{"ignore-previous"}},
			},
		},
		Schema: schemaVersion,
	}
}

// TestResearchRecordMatchesGolden freezes a research record's bytes: kind and
// research sit above schema, and nothing about the workspace fields moved.
func TestResearchRecordMatchesGolden(t *testing.T) {
	got, err := json.Marshal(researchTask())
	if err != nil {
		t.Fatal(err)
	}
	if want := readGolden(t, "task-show-research.golden.json"); !bytes.Equal(got, want) {
		t.Errorf("research record != golden\n got: %s\nwant: %s", got, want)
	}
}

// TestWorkspaceRecordCarriesNoKind guards the append-only promise (ADR-0045):
// a record written before the field loads as a workspace task, and a workspace
// record serialises with no kind and no research key.
func TestWorkspaceRecordCarriesNoKind(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	old := readGolden(t, "task-show.golden.json")
	dir := filepath.Join(root, "tasks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20260910-120115-4f2a.json"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("20260910-120115-4f2a")
	if err != nil {
		t.Fatal(err)
	}
	if got.IsResearch() || got.Kind != "" || got.Research != nil {
		t.Errorf("old record loaded as kind %q research %v, want a workspace task", got.Kind, got.Research)
	}
	out, _ := json.Marshal(got)
	if bytes.Contains(out, []byte(`"kind"`)) || bytes.Contains(out, []byte(`"research"`)) {
		t.Errorf("a workspace record must carry neither key: %s", out)
	}
}

// TestCreateResearchRoundTrips guards that a research record survives the
// store with its sources as an array, never null.
func TestCreateResearchRoundTrips(t *testing.T) {
	s := New(t.TempDir())
	task := Task{
		ID: researchID, Instruction: "q", State: Queued, SubmittedAt: "2026-09-14T09:30:00Z",
		Kind: KindResearch, Research: &Research{Budget: ResearchBudget{Rounds: 2, Minutes: 10}},
	}
	if err := s.Create(task); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.root, "tasks", researchID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"sources":[]`)) {
		t.Errorf("a research record must write sources as [], got %s", raw)
	}
	got, err := s.Load(researchID)
	if err != nil || !got.IsResearch() || got.Research.Budget.Rounds != 2 {
		t.Errorf("Load = %+v, %v", got, err)
	}
}

// TestListViewCarriesKindForResearch freezes the list row: a research task
// names its kind, and a workspace row is byte-identical to before.
func TestListViewCarriesKindForResearch(t *testing.T) {
	exitDone := 0
	tasks := []Task{
		{ID: "20260910-120115-0001", Workspace: "/home/dev/reports", State: Done, Exit: &exitDone,
			SubmittedAt: "2026-09-10T12:01:15Z", FinishedAt: "2026-09-10T12:04:02Z"},
		researchTask(),
	}
	got, err := json.Marshal(NewListView(tasks))
	if err != nil {
		t.Fatal(err)
	}
	if want := readGolden(t, "task-list-research.golden.json"); !bytes.Equal(got, want) {
		t.Errorf("ListView != golden\n got: %s\nwant: %s", got, want)
	}
}

// TestReportRoundTrip guards the report artifact: written under the tasks
// directory by task id at mode 0600, read back verbatim, and never mistaken
// for a record by List or Recover.
func TestReportRoundTrip(t *testing.T) {
	s := New(t.TempDir())
	if err := s.Create(Task{ID: researchID, State: Queued, SubmittedAt: "2026-09-14T09:30:00Z", Kind: KindResearch}); err != nil {
		t.Fatal(err)
	}
	rel, err := s.WriteReport(researchID, []byte("# report\n"))
	if err != nil {
		t.Fatal(err)
	}
	if rel != "tasks/"+researchID+".report.md" {
		t.Errorf("relative path = %q", rel)
	}
	if abs := s.ReportPath(rel); !strings.HasSuffix(abs, rel) || !filepath.IsAbs(abs) {
		t.Errorf("ReportPath = %q", abs)
	}
	info, err := os.Stat(s.ReportPath(rel))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("report mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	got, err := s.ReadReport(researchID)
	if err != nil || string(got) != "# report\n" {
		t.Errorf("ReadReport = %q, %v", got, err)
	}
	tasks, err := s.List()
	if err != nil || len(tasks) != 1 {
		t.Errorf("List = %d tasks, %v; the report must not be listed as a record", len(tasks), err)
	}
	if ids, err := s.Recover(); err != nil || len(ids) != 1 {
		t.Errorf("Recover = %v, %v; want only the one queued record", ids, err)
	}
}

// TestReportRefusesAMalformedIDAndAnAbsentReport guards GHSA-9hj4's shape
// check for the new path, and the not-found answer for a task with no report.
func TestReportRefusesAMalformedIDAndAnAbsentReport(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.WriteReport("../x", []byte("x")); err == nil {
		t.Error("WriteReport with a traversal id must fail")
	}
	if _, err := s.ReadReport("../x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ReadReport traversal = %v, want ErrNotFound", err)
	}
	if _, err := s.ReadReport(researchID); !errors.Is(err, ErrNotFound) {
		t.Errorf("ReadReport absent = %v, want ErrNotFound", err)
	}
}

// TestTransitionKeepsTheResearchShadow guards that the runner can stamp the
// research data in the same write as a state change.
func TestTransitionKeepsTheResearchShadow(t *testing.T) {
	s := New(t.TempDir())
	if err := s.Create(Task{ID: researchID, State: Queued, SubmittedAt: "x", Kind: KindResearch, Research: &Research{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition(researchID, Running, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.Transition(researchID, Flagged, func(t *Task) {
		t.Research.Sources = []ResearchSource{{N: 1, URL: "https://a.example/"}}
	})
	if err != nil || got.Research == nil || len(got.Research.Sources) != 1 {
		t.Fatalf("Transition = %+v, %v", got, err)
	}
	if got.Exit == nil || *got.Exit != 2 {
		t.Errorf("exit = %v, want 2 from taskstore.Exit", got.Exit)
	}
}
