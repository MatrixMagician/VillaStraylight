package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// newTaskFixture points the task verbs at a fake loopback API.
func newTaskFixture(t *testing.T) (*taskDeps, *fakeTaskAPI) {
	t.Helper()
	f := newFakeTaskAPI(t)
	return &taskDeps{
		load: func() (config.VillaConfig, error) { return config.VillaConfig{}, nil },
		api: func(config.VillaConfig) taskAPIDeps {
			return taskAPIDeps{base: f.srv.URL, client: f.srv.Client()}
		},
	}, f
}

func readTaskGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return data
}

// TestRunTaskListJSONGolden freezes `villa task list --json`: the CLI emits the
// API's ListView bytes verbatim, so the terminal contract and the store's are
// the same contract, not two that can drift.
func TestRunTaskListJSONGolden(t *testing.T) {
	d, f := newTaskFixture(t)
	golden := readTaskGolden(t, "task-list.golden.json")
	f.listRaw = golden

	cmd, out, errOut := lifecycleTestCmd()
	if code := runTaskList(cmd, true, d); code != exitPass {
		t.Fatalf("exit = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}
	if out.String() != string(golden) {
		t.Errorf("stdout != golden\n got: %s\nwant: %s", out.String(), golden)
	}
}

// TestRunTaskShowJSONGolden freezes `villa task show --json`: the record,
// verbatim.
func TestRunTaskShowJSONGolden(t *testing.T) {
	d, f := newTaskFixture(t)
	golden := readTaskGolden(t, "task-show.golden.json")
	f.showRaw = golden

	cmd, out, errOut := lifecycleTestCmd()
	if code := runTaskShow(cmd, "20260910-120115-4f2a", true, d); code != exitPass {
		t.Fatalf("exit = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}
	if out.String() != string(golden) {
		t.Errorf("stdout != golden\n got: %s\nwant: %s", out.String(), golden)
	}
}

// TestRunTaskListTable asserts the human form carries the columns an operator
// scans: id, workspace, state and the exit code.
func TestRunTaskListTable(t *testing.T) {
	d, f := newTaskFixture(t)
	f.listRaw = readTaskGolden(t, "task-list.golden.json")

	cmd, out, errOut := lifecycleTestCmd()
	if code := runTaskList(cmd, false, d); code != exitPass {
		t.Fatalf("exit = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}
	for _, want := range []string{"20260910-120115-0001", "/home/dev/reports", "done", "running"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output = %q, want it to contain %q", out.String(), want)
		}
	}
}

// TestRunTaskShowTable asserts the human record names the files the task wrote
// and the unsupported claim that flagged it.
func TestRunTaskShowTable(t *testing.T) {
	d, f := newTaskFixture(t)
	f.showRaw = readTaskGolden(t, "task-show.golden.json")

	cmd, out, errOut := lifecycleTestCmd()
	if code := runTaskShow(cmd, "20260910-120115-4f2a", false, d); code != exitPass {
		t.Fatalf("exit = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}
	for _, want := range []string{"summary.md", "created", "revenue grew 40%", "crush"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output = %q, want it to contain %q", out.String(), want)
		}
	}
}

// TestRunTaskShowTaint asserts a tainted record says so in both forms: the
// human view names the source and what it changed, --json carries the record
// verbatim, and an untainted record prints no taint row.
func TestRunTaskShowTaint(t *testing.T) {
	d, f := newTaskFixture(t)
	golden := readTaskGolden(t, "task-show-tainted.golden.json")
	f.showRaw = golden

	cmd, out, _ := lifecycleTestCmd()
	if code := runTaskShow(cmd, "20260910-120115-4f2a", true, d); code != exitPass || out.String() != string(golden) {
		t.Fatalf("--json = %d %q, want the tainted record verbatim", code, out.String())
	}

	cmd, out, _ = lifecycleTestCmd()
	if code := runTaskShow(cmd, "20260910-120115-4f2a", false, d); code != exitPass {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "taint") || !strings.Contains(out.String(), "web: write and execute ask") {
		t.Errorf("human view = %q, want a taint row naming web", out.String())
	}

	f.showRaw = readTaskGolden(t, "task-show.golden.json")
	cmd, out, _ = lifecycleTestCmd()
	runTaskShow(cmd, "20260910-120115-4f2a", false, d)
	if strings.Contains(out.String(), "taint") {
		t.Errorf("an untainted record must print no taint row: %q", out.String())
	}
}

// TestRunTaskShowPrintsTheObservationBesideEachFile guards that the operator
// reads what villa saw on disk next to what the harness reported: present with
// size and hash, or unobserved with the reason, on the file's own line.
func TestRunTaskShowPrintsTheObservationBesideEachFile(t *testing.T) {
	d, f := newTaskFixture(t)
	f.showRaw = readTaskGolden(t, "task-show.golden.json")

	cmd, out, errOut := lifecycleTestCmd()
	if code := runTaskShow(cmd, "20260910-120115-4f2a", false, d); code != exitPass {
		t.Fatalf("exit = %d, want %d; stderr=%q", code, exitPass, errOut.String())
	}
	lines := strings.Split(out.String(), "\n")
	wantLines := map[string]string{
		"created summary.md": "present, 412 bytes, sha256 9f2c4e0d8b7a6f5e4d3c2b1a0f9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f10",
		"modified notes.txt": "unobserved (permission denied)",
	}
	for event, observed := range wantLines {
		found := false
		for _, l := range lines {
			if strings.Contains(l, event) && strings.Contains(l, observed) {
				found = true
			}
		}
		if !found {
			t.Errorf("no line carries %q with %q:\n%s", event, observed, out.String())
		}
	}
}

// TestRunTaskAnswerVerbs asserts each answer verb hits its own route with the
// body the API expects.
func TestRunTaskAnswerVerbs(t *testing.T) {
	t.Run("approve", func(t *testing.T) {
		d, f := newTaskFixture(t)
		cmd, _, errOut := lifecycleTestCmd()
		if code := runTaskAnswer(cmd, "approve", "abc", false, d); code != exitPass {
			t.Fatalf("exit = %d, want %d; stderr=%q", code, exitPass, errOut.String())
		}
		if len(f.answers) != 1 || f.answers[0].verb != "approve" || f.answers[0].body != `{"all":false}` {
			t.Fatalf("answers = %v, want one approve with all:false", f.answers)
		}
		if f.answers[0].id != "abc" {
			t.Errorf("id = %q, want abc", f.answers[0].id)
		}
	})

	t.Run("approve --all", func(t *testing.T) {
		d, f := newTaskFixture(t)
		cmd, _, _ := lifecycleTestCmd()
		runTaskAnswer(cmd, "approve", "abc", true, d)
		if len(f.answers) != 1 || f.answers[0].body != `{"all":true}` {
			t.Fatalf("answers = %v, want one approve with all:true", f.answers)
		}
	})

	for _, verb := range []string{"deny", "cancel"} {
		t.Run(verb, func(t *testing.T) {
			d, f := newTaskFixture(t)
			cmd, _, errOut := lifecycleTestCmd()
			if code := runTaskAnswer(cmd, verb, "abc", false, d); code != exitPass {
				t.Fatalf("exit = %d, want %d; stderr=%q", code, exitPass, errOut.String())
			}
			if len(f.answers) != 1 || f.answers[0].verb != verb || f.answers[0].body != "" {
				t.Fatalf("answers = %v, want one %s with an empty body", f.answers, verb)
			}
		})
	}
}

// TestRunTaskUnknownIDExits1 asserts a 404 from the API is reported as the
// API's own message, not swallowed into a false success.
func TestRunTaskUnknownIDExits1(t *testing.T) {
	d, f := newTaskFixture(t)
	f.status = http.StatusNotFound

	cmd, out, errOut := lifecycleTestCmd()
	if code := runTaskShow(cmd, "nope", true, d); code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "task not found") {
		t.Errorf("stderr = %q, want the API's error", errOut.String())
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want nothing on a failed lookup", out.String())
	}
}

// TestRunTaskDashboardDownNamesRemediation asserts the task verbs refuse the
// same way villa work does when the service is not answering.
func TestRunTaskDashboardDownNamesRemediation(t *testing.T) {
	d, _ := newTaskFixture(t)
	d.api = func(config.VillaConfig) taskAPIDeps {
		return taskAPIDeps{base: "http://127.0.0.1:1", client: http.DefaultClient}
	}
	cmd, _, errOut := lifecycleTestCmd()
	if code := runTaskList(cmd, false, d); code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "systemctl --user start villa-dashboard.service") {
		t.Errorf("stderr = %q, want the dashboard start remediation", errOut.String())
	}
}
