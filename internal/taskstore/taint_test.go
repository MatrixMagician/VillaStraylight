package taskstore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/approval"
)

const taintID = "20260910-120115-0002"

func newTaintedStore(t *testing.T) (Store, string) {
	t.Helper()
	root := t.TempDir()
	s := New(root)
	if err := s.Create(Task{ID: taintID, State: Queued, SubmittedAt: "2026-09-10T12:00:00Z"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return s, root
}

// TestSetTaintRecordsSourceAndOneLogLine guards ADR-0040: the first SetTaint
// persists the source on the record and appends exactly one narration line
// naming it.
func TestSetTaintRecordsSourceAndOneLogLine(t *testing.T) {
	s, _ := newTaintedStore(t)
	at := time.Date(2026, 9, 10, 12, 1, 0, 0, time.UTC)
	got, changed, err := s.SetTaint(taintID, approval.TaintWeb, at)
	if err != nil || !changed {
		t.Fatalf("SetTaint = (%+v, %v, %v), want changed", got, changed, err)
	}
	loaded, err := s.Load(taintID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Taint != approval.TaintWeb {
		t.Errorf("loaded taint = %q, want web", loaded.Taint)
	}
	events, err := s.ReadLog(taintID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "narration" || !strings.Contains(events[0].Text, "web") || !events[0].At.Equal(at) {
		t.Errorf("log = %+v, want one narration line naming web", events)
	}
}

// TestSetTaintIsIdempotentAndNeverClears guards the one-way property: a second
// call, from the same or another source, changes neither the record bytes nor
// the log, and the first source stays.
func TestSetTaintIsIdempotentAndNeverClears(t *testing.T) {
	s, root := newTaintedStore(t)
	at := time.Date(2026, 9, 10, 12, 1, 0, 0, time.UTC)
	if _, _, err := s.SetTaint(taintID, approval.TaintWeb, at); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tasks", taintID+".json")
	before, _ := os.ReadFile(path)

	for _, src := range []approval.Taint{approval.TaintWeb, approval.Taint("email")} {
		got, changed, err := s.SetTaint(taintID, src, at.Add(time.Minute))
		if src == approval.TaintWeb && (err != nil || changed || got.Taint != approval.TaintWeb) {
			t.Errorf("repeat SetTaint(web) = (%+v, %v, %v), want unchanged no-op", got, changed, err)
		}
		if src != approval.TaintWeb && err == nil {
			t.Errorf("SetTaint(%q) must be refused", src)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Errorf("record changed on a repeat SetTaint")
	}
	if events, _ := s.ReadLog(taintID); len(events) != 1 {
		t.Errorf("log has %d lines after a repeat, want 1", len(events))
	}
}

// TestSetTaintRefusesAnIllegalSource guards the closed enumeration at the
// write boundary: the zero value and unknown sources are refused and leave the
// record untainted.
func TestSetTaintRefusesAnIllegalSource(t *testing.T) {
	s, _ := newTaintedStore(t)
	for _, bad := range []approval.Taint{approval.Untainted, "email"} {
		if _, _, err := s.SetTaint(taintID, bad, time.Now()); err == nil {
			t.Errorf("SetTaint(%q) = nil error, want refusal", bad)
		}
	}
	if loaded, _ := s.Load(taintID); loaded.Taint != approval.Untainted {
		t.Errorf("taint = %q after refused sets, want none", loaded.Taint)
	}
}

// TestSetTaintUnknownTask guards that a missing or malformed id is an error,
// not a fabricated record.
func TestSetTaintUnknownTask(t *testing.T) {
	s, _ := newTaintedStore(t)
	if _, _, err := s.SetTaint("20260910-120115-ffff", approval.TaintWeb, time.Now()); err == nil {
		t.Error("SetTaint on an unknown task must fail")
	}
	if _, _, err := s.SetTaint("../x", approval.TaintWeb, time.Now()); err == nil {
		t.Error("SetTaint on a malformed id must fail")
	}
}

// TestTransitionKeepsTheTaint guards that a later state change cannot drop a
// taint set in between (the runner transitions from its own copy).
func TestTransitionKeepsTheTaint(t *testing.T) {
	s, _ := newTaintedStore(t)
	if _, _, err := s.SetTaint(taintID, approval.TaintWeb, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Transition(taintID, Running, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Taint != approval.TaintWeb {
		t.Errorf("taint after Transition = %q, want web", got.Taint)
	}
}

// TestTaintedRecordMatchesGolden freezes the tainted record's bytes: the taint
// key sits above schema, and nothing else about the record moved.
func TestTaintedRecordMatchesGolden(t *testing.T) {
	task := fullTask("20260910-120115-4f2a")
	task.Taint = approval.TaintWeb
	task.Schema = schemaVersion
	got, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if want := readGolden(t, "task-show-tainted.golden.json"); !bytes.Equal(got, want) {
		t.Errorf("tainted record != golden\n got: %s\nwant: %s", got, want)
	}
}

// TestRecordWithoutTaintLoadsUntainted guards the stored-schema question: a
// record written before the field existed loads untainted, and an untainted
// record serialises with no taint key, so existing bytes are unchanged.
func TestRecordWithoutTaintLoadsUntainted(t *testing.T) {
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
	if got.Taint.Tainted() {
		t.Errorf("old record loaded tainted: %q", got.Taint)
	}
	if bytes.Contains(old, []byte(`"taint"`)) {
		t.Error("the untainted golden must carry no taint key")
	}
}
