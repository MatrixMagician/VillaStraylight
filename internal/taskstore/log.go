package taskstore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// LogEvent is one line of a task's tasks/<id>.log transcript: villa's own
// narration, or a harness message event carried verbatim in Raw. Crush's own
// SQLite lives on the sandbox tmpfs and dies with the VM, so this log is the
// only transcript that survives (spec §3.2).
type LogEvent struct {
	At   time.Time       `json:"at"`
	Kind string          `json:"kind"` // "narration" | "harness"
	Text string          `json:"text,omitempty"`
	Raw  json.RawMessage `json:"raw,omitempty"`
}

func (s Store) logPath(id string) string { return s.tasksDir() + "/" + id + ".log" }

// AppendLog appends one JSON-lines event to the task's log. The record file
// is never touched by a log write — narration and harness messages accumulate
// independently of the task's state transitions.
func (s Store) AppendLog(id string, ev LogEvent) error {
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("taskstore: AppendLog %s: marshal: %w", id, err)
	}
	line = append(line, '\n')
	if err := s.fs.appendLine(s.logPath(id), line); err != nil {
		return fmt.Errorf("taskstore: AppendLog %s: %w", id, err)
	}
	return nil
}

// ReadLog returns every event in a task's log, in append order. A task with
// no log yet returns (nil, nil), mirroring jsonstore's absent-store handling.
func (s Store) ReadLog(id string) ([]LogEvent, error) {
	data, err := s.fs.readFile(s.logPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("taskstore: ReadLog %s: %w", id, err)
	}
	var events []LogEvent
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev LogEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("taskstore: ReadLog %s: corrupt line: %w", id, err)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("taskstore: ReadLog %s: %w", id, err)
	}
	return events, nil
}
