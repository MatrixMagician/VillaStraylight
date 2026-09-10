package taskstore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fullTask is a fully populated Task fixture: every field set, every nested
// slice non-empty, used by the round-trip and golden tests.
func fullTask(id string) Task {
	exit := 0
	return Task{
		ID:          id,
		Workspace:   "/home/dev/reports",
		Instruction: "Summarize Q3 numbers and cite every figure.",
		Mode:        "ask",
		State:       Done,
		Exit:        &exit,
		SubmittedAt: "2026-09-10T12:01:15Z",
		StartedAt:   "2026-09-10T12:01:16Z",
		FinishedAt:  "2026-09-10T12:04:02Z",
		Harness:     Harness{Name: "crush", Version: "0.9.1"},
		Files: []FileEvent{
			{Path: "summary.md", Action: "created"},
			{Path: "notes.txt", Action: "modified"},
		},
		Approvals: []Approval{
			{Seq: 1, Tool: "bash", Action: "execute", Path: "", Command: "ls -l reports", AskedAt: "2026-09-10T12:02:00Z", Answer: "allow", AnsweredAt: "2026-09-10T12:02:05Z", By: "prompt"},
			{Seq: 2, Tool: "edit", Action: "write", Path: "summary.md", AskedAt: "2026-09-10T12:03:00Z", Answer: "allow_session", AnsweredAt: "2026-09-10T12:03:02Z", By: "cli"},
		},
		Grounding: Grounding{
			Checked: true,
			Documents: []GroundingDocument{
				{
					Path:   "summary.md",
					Claims: 3,
					Unsupported: []Unsupported{
						{Claim: "revenue grew 40%", Reason: "no supporting figure in workspace files"},
					},
				},
			},
		},
	}
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return data
}

// TestNewIDDeterministic proves NewID is a pure function of (now, rand bytes):
// same inputs, same id, in the documented "<time>-<4 lowercase hex>" shape.
func TestNewIDDeterministic(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 1, 15, 0, time.UTC)
	id := NewID(now, bytes.NewReader([]byte{0x4f, 0x2a}))
	want := "20260910-120115-4f2a"
	if id != want {
		t.Errorf("NewID = %q, want %q", id, want)
	}
	// Same inputs again -> identical id.
	if again := NewID(now, bytes.NewReader([]byte{0x4f, 0x2a})); again != want {
		t.Errorf("NewID repeated = %q, want %q (deterministic)", again, want)
	}
}

// TestCreateLoadRoundTrip proves a fully populated Task saves and loads equal,
// and that the bytes on disk match testdata/task-show.golden.json exactly.
func TestCreateLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	id := "20260910-120115-4f2a"
	in := fullTask(id)
	// Create only accepts a Queued/Refused initial state (spec §3.2); reuse it
	// to reach Done the same way a real task would, through Transition, so the
	// on-disk bytes are produced by the same path production code takes.
	queued := in
	queued.State = Queued
	queued.Exit = nil
	queued.FinishedAt = ""
	queued.StartedAt = ""
	if err := s.Create(queued); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Transition(id, Running, func(tk *Task) { tk.StartedAt = in.StartedAt }); err != nil {
		t.Fatalf("Transition -> running: %v", err)
	}
	got, err := s.Transition(id, Done, func(tk *Task) {
		tk.Workspace = in.Workspace
		tk.Instruction = in.Instruction
		tk.Mode = in.Mode
		tk.FinishedAt = in.FinishedAt
		tk.Harness = in.Harness
		tk.Files = in.Files
		tk.Approvals = in.Approvals
		tk.Grounding = in.Grounding
	})
	if err != nil {
		t.Fatalf("Transition -> done: %v", err)
	}
	if got.ID != in.ID || got.Workspace != in.Workspace || got.State != Done {
		t.Fatalf("round-tripped task mismatch: %+v", got)
	}
	if got.Exit == nil || *got.Exit != 0 {
		t.Fatalf("Exit = %v, want pointer to 0 (done)", got.Exit)
	}

	loaded, err := s.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Instruction != in.Instruction || len(loaded.Files) != 2 || len(loaded.Approvals) != 2 {
		t.Fatalf("loaded task mismatch: %+v", loaded)
	}

	onDisk, err := os.ReadFile(filepath.Join(root, "tasks", id+".json"))
	if err != nil {
		t.Fatalf("read on-disk record: %v", err)
	}
	want := readGolden(t, "task-show.golden.json")
	if !bytes.Equal(onDisk, want) {
		t.Errorf("on-disk record != golden\n got: %s\nwant: %s", onDisk, want)
	}
}

// TestCreateRefusesBadInitialState proves Create only accepts queued or
// refused as an initial state, and refuses to overwrite an existing record.
func TestCreateRefusesBadInitialState(t *testing.T) {
	s := New(t.TempDir())
	bad := fullTask("20260910-120115-0001")
	bad.State = Running
	if err := s.Create(bad); err == nil {
		t.Error("Create(state=running) = nil error, want refused")
	}

	refused := fullTask("20260910-120115-0002")
	refused.State = Refused
	refused.Exit = nil
	if err := s.Create(refused); err != nil {
		t.Fatalf("Create(state=refused): %v", err)
	}
	loaded, err := s.Load(refused.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Exit == nil || *loaded.Exit != 1 {
		t.Errorf("refused Exit = %v, want pointer to 1", loaded.Exit)
	}

	if err := s.Create(refused); err == nil {
		t.Error("Create(duplicate id) = nil error, want refused (no overwrite)")
	}
}

// TestTransitionRefusesIllegalEdge proves an illegal edge names from/to in its
// error and leaves the stored record untouched.
func TestTransitionRefusesIllegalEdge(t *testing.T) {
	s := New(t.TempDir())
	id := "20260910-120115-0003"
	if err := s.Create(Task{ID: id, State: Queued, SubmittedAt: "2026-09-10T12:00:00Z"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(s.root, "tasks", id+".json"))
	if err != nil {
		t.Fatalf("read record: %v", err)
	}

	_, err = s.Transition(id, Done, nil)
	if err == nil {
		t.Fatal("Transition(queued -> done) = nil error, want refused")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("queued")) || !bytes.Contains([]byte(err.Error()), []byte("done")) {
		t.Errorf("error %q does not name both from and to", err)
	}

	after, err := os.ReadFile(filepath.Join(s.root, "tasks", id+".json"))
	if err != nil {
		t.Fatalf("read record after refused transition: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("record changed on disk after a refused transition")
	}
}

// TestTransitionRefusesFromTerminal proves Transition on an already-terminal
// record is refused, interrupted included.
func TestTransitionRefusesFromTerminal(t *testing.T) {
	s := New(t.TempDir())
	id := "20260910-120115-0004"
	if err := s.Create(Task{ID: id, State: Queued, SubmittedAt: "2026-09-10T12:00:00Z"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Transition(id, Cancelled, nil); err != nil {
		t.Fatalf("Transition -> cancelled: %v", err)
	}
	if _, err := s.Transition(id, Interrupted, nil); err == nil {
		t.Error("Transition off a terminal (cancelled) state = nil error, want refused")
	}
}

// TestListTimeOrder proves List returns records in time order (id order).
func TestListTimeOrder(t *testing.T) {
	s := New(t.TempDir())
	ids := []string{
		"20260910-120500-0001",
		"20260910-120115-0002",
		"20260910-130000-0003",
	}
	for _, id := range ids {
		if err := s.Create(Task{ID: id, State: Queued, SubmittedAt: "2026-09-10T12:00:00Z"}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{
		"20260910-120115-0002",
		"20260910-120500-0001",
		"20260910-130000-0003",
	}
	if len(got) != len(want) {
		t.Fatalf("List returned %d tasks, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("List()[%d].ID = %q, want %q (time order)", i, got[i].ID, id)
		}
	}
}

// TestListUnparsableIsError proves an unparsable record file is reported as an
// error, never silently skipped — a task an operator cannot see in `villa
// task list` is a worse failure than a list command that refuses and says why.
func TestListUnparsableIsError(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	if err := s.Create(Task{ID: "20260910-120115-0001", State: Queued, SubmittedAt: "2026-09-10T12:00:00Z"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	tasksDir := filepath.Join(root, "tasks")
	if err := os.WriteFile(filepath.Join(tasksDir, "20260910-120200-0002.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt record: %v", err)
	}
	if _, err := s.List(); err == nil {
		t.Error("List() with a corrupt record = nil error, want error")
	}
}

// TestListViewMatchesGolden builds a ListView from a fixed two-task List and
// checks it against testdata/task-list.golden.json byte for byte.
func TestListViewMatchesGolden(t *testing.T) {
	exitDone := 0
	tasks := []Task{
		{
			ID: "20260910-120115-0001", Workspace: "/home/dev/reports", State: Done,
			Exit: &exitDone, SubmittedAt: "2026-09-10T12:01:15Z", FinishedAt: "2026-09-10T12:04:02Z",
		},
		{
			ID: "20260910-121000-0002", Workspace: "/home/dev/ledger", State: Running,
			SubmittedAt: "2026-09-10T12:10:00Z",
		},
	}
	view := NewListView(tasks)
	got, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal ListView: %v", err)
	}
	want := readGolden(t, "task-list.golden.json")
	if !bytes.Equal(got, want) {
		t.Errorf("ListView != golden\n got: %s\nwant: %s", got, want)
	}
}

// TestRecoverIdempotent proves Recover marks exactly the non-terminal records
// Interrupted, returns their ids, and a second call is a byte-identical no-op.
func TestRecoverIdempotent(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	seed := map[string]State{
		"20260910-120100-0001": Queued,
		"20260910-120200-0002": Running,
		"20260910-120300-0003": AwaitingApproval,
		"20260910-120400-0004": Done,
		"20260910-120500-0005": Refused,
	}
	for id, state := range seed {
		if state == Queued || state == Refused {
			if err := s.Create(Task{ID: id, State: state, SubmittedAt: "2026-09-10T12:00:00Z"}); err != nil {
				t.Fatalf("Create %s: %v", id, err)
			}
			continue
		}
		if err := s.Create(Task{ID: id, State: Queued, SubmittedAt: "2026-09-10T12:00:00Z"}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
		if state == AwaitingApproval {
			if _, err := s.Transition(id, Running, nil); err != nil {
				t.Fatalf("Transition %s -> running: %v", id, err)
			}
			if _, err := s.Transition(id, state, nil); err != nil {
				t.Fatalf("Transition %s -> %s: %v", id, state, err)
			}
			continue
		}
		if state == Running {
			if _, err := s.Transition(id, Running, nil); err != nil {
				t.Fatalf("Transition %s -> running: %v", id, err)
			}
			continue
		}
		// Terminal seed states (done, refused via non-queued path) route through
		// running first, since queued -> done is not a legal edge.
		if _, err := s.Transition(id, Running, nil); err != nil {
			t.Fatalf("Transition %s -> running: %v", id, err)
		}
		if _, err := s.Transition(id, state, nil); err != nil {
			t.Fatalf("Transition %s -> %s: %v", id, state, err)
		}
	}

	got, err := s.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	wantRecovered := []string{"20260910-120100-0001", "20260910-120200-0002", "20260910-120300-0003"}
	if len(got) != len(wantRecovered) {
		t.Fatalf("Recover returned %v, want %v", got, wantRecovered)
	}
	for i, id := range wantRecovered {
		if got[i] != id {
			t.Errorf("Recover()[%d] = %q, want %q", i, got[i], id)
		}
	}
	for _, id := range wantRecovered {
		tk, err := s.Load(id)
		if err != nil {
			t.Fatalf("Load %s: %v", id, err)
		}
		if tk.State != Interrupted {
			t.Errorf("%s state = %q, want interrupted", id, tk.State)
		}
	}

	tasksDir := filepath.Join(root, "tasks")
	before := map[string][]byte{}
	for id := range seed {
		data, err := os.ReadFile(filepath.Join(tasksDir, id+".json"))
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		before[id] = data
	}

	second, err := s.Recover()
	if err != nil {
		t.Fatalf("second Recover: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second Recover returned %v, want none", second)
	}
	for id, data := range before {
		after, err := os.ReadFile(filepath.Join(tasksDir, id+".json"))
		if err != nil {
			t.Fatalf("re-read %s: %v", id, err)
		}
		if !bytes.Equal(data, after) {
			t.Errorf("%s changed on disk after a no-op Recover", id)
		}
	}
}
