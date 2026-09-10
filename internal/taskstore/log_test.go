package taskstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAppendLogTwiceYieldsTwoLines proves AppendLog appends (never rewrites),
// ReadLog returns both events in order, and the sibling record file is
// untouched by a log write.
func TestAppendLogTwiceYieldsTwoLines(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	id := "20260910-120115-0001"
	if err := s.Create(Task{ID: id, State: Queued, SubmittedAt: "2026-09-10T12:00:00Z"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	recordBefore, err := os.ReadFile(filepath.Join(root, "tasks", id+".json"))
	if err != nil {
		t.Fatalf("read record: %v", err)
	}

	at := time.Date(2026, 9, 10, 12, 1, 0, 0, time.UTC)
	if err := s.AppendLog(id, LogEvent{At: at, Kind: "narration", Text: "starting"}); err != nil {
		t.Fatalf("AppendLog 1: %v", err)
	}
	if err := s.AppendLog(id, LogEvent{At: at.Add(time.Second), Kind: "harness", Raw: json.RawMessage(`{"tool":"bash"}`)}); err != nil {
		t.Fatalf("AppendLog 2: %v", err)
	}

	events, err := s.ReadLog(id)
	if err != nil {
		t.Fatalf("ReadLog: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ReadLog returned %d events, want 2", len(events))
	}
	if events[0].Kind != "narration" || events[0].Text != "starting" {
		t.Errorf("event 0 = %+v, want narration/starting", events[0])
	}
	if events[1].Kind != "harness" || string(events[1].Raw) != `{"tool":"bash"}` {
		t.Errorf("event 1 = %+v, want harness raw bash", events[1])
	}
	if !events[0].At.Equal(at) || !events[1].At.Equal(at.Add(time.Second)) {
		t.Errorf("event timestamps out of order: %v, %v", events[0].At, events[1].At)
	}

	recordAfter, err := os.ReadFile(filepath.Join(root, "tasks", id+".json"))
	if err != nil {
		t.Fatalf("re-read record: %v", err)
	}
	if string(recordBefore) != string(recordAfter) {
		t.Error("record file changed after AppendLog writes")
	}

	logData, err := os.ReadFile(filepath.Join(root, "tasks", id+".log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if n := len(splitLines(logData)); n != 2 {
		t.Errorf("log file has %d lines, want 2", n)
	}
}

// TestReadLogAbsentIsEmpty proves ReadLog on a task with no log yet returns
// (nil, nil) rather than an error.
func TestReadLogAbsentIsEmpty(t *testing.T) {
	s := New(t.TempDir())
	id := "20260910-120115-0002"
	if err := s.Create(Task{ID: id, State: Queued, SubmittedAt: "2026-09-10T12:00:00Z"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	events, err := s.ReadLog(id)
	if err != nil {
		t.Fatalf("ReadLog: %v", err)
	}
	if events != nil {
		t.Errorf("ReadLog(no log) = %v, want nil", events)
	}
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	return lines
}
