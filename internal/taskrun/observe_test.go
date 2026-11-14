package taskrun

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/approval"
	"github.com/MatrixMagician/VillaStraylight/internal/crushapi"
	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestAReportedFileThatExistsIsObservedWithItsSizeAndHash guards the present
// observation: one observation per distinct reported path, carrying the size
// and sha256 of the bytes villa read after the run, never Crush's word for it.
func TestAReportedFileThatExistsIsObservedWithItsSizeAndHash(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.writeFile("memo.md", "Revenue grew.")
	h.start()
	task := h.submit(approval.ModeAsk)
	b.feed(ready(), fileEvent("/workspace/memo.md"), fileEvent("/workspace/memo.md"), runComplete(), filesRead())
	b.end()

	got := h.waitState(task.ID, taskstore.Done)
	if len(got.Observations) != 1 {
		t.Fatalf("observations = %+v, want one per distinct path", got.Observations)
	}
	o := got.Observations[0]
	if o.Path != "memo.md" || o.State != taskstore.ObservedPresent {
		t.Errorf("observation = %+v, want memo.md present", o)
	}
	if o.Size == nil || *o.Size != int64(len("Revenue grew.")) {
		t.Errorf("size = %v, want %d", o.Size, len("Revenue grew."))
	}
	if o.SHA256 != sha256Hex("Revenue grew.") {
		t.Errorf("sha256 = %q, want %q", o.SHA256, sha256Hex("Revenue grew."))
	}
	if o.ObservedAt == "" || o.Error != "" {
		t.Errorf("observation = %+v, want a timestamp and no error", o)
	}
}

// TestAReportedFileAbsentAfterTheRunFlagsWithItsPath guards the confident
// negative: a path the harness reported written that is gone after the run
// flags the task, and the reason names the path.
func TestAReportedFileAbsentAfterTheRunFlagsWithItsPath(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.writeFile("memo.md", "Revenue grew.")
	h.start()
	task := h.submit(approval.ModeAsk)
	b.feed(ready(), fileEvent("/workspace/memo.md"), fileEvent("/workspace/actions.md"), runComplete(), filesRead())
	b.end()

	got := h.waitState(task.ID, taskstore.Flagged)
	if len(got.Observations) != 2 {
		t.Fatalf("observations = %+v, want two", got.Observations)
	}
	o := got.Observations[1]
	if o.Path != "actions.md" || o.State != taskstore.ObservedAbsent || o.Size != nil || o.SHA256 != "" {
		t.Errorf("observation = %+v, want actions.md absent with no size or hash", o)
	}
	var reason string
	for _, line := range strings.Split(h.logText(task.ID), "\n") {
		if strings.HasPrefix(line, "narration: flagged:") {
			reason = line
		}
	}
	if !strings.Contains(reason, "actions.md") {
		t.Errorf("flagged reason = %q, want it to name actions.md", reason)
	}
	if !got.Grounding.Checked || len(got.Grounding.Documents) != 1 || got.Grounding.Documents[0].Path != "memo.md" {
		t.Errorf("grounding = %+v; an absent path has no document to audit", got.Grounding)
	}
}

// TestAnUnreadableFileIsUnobservedNotAbsent guards typed-Unknown: a read that
// fails for any reason but absence is recorded as unobserved with the error,
// and never counts as absent.
func TestAnUnreadableFileIsUnobservedNotAbsent(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.deps.ReadFile = func(workspace, rel string) ([]byte, error) {
		if rel == "locked.md" {
			return nil, &os.PathError{Op: "open", Path: rel, Err: os.ErrPermission}
		}
		return os.ReadFile(filepath.Join(workspace, rel))
	}
	h.start()
	task := h.submit(approval.ModeAsk)
	b.feed(ready(), fileEvent("/workspace/locked.md"), runComplete(), filesRead())
	b.end()

	got := h.waitState(task.ID, taskstore.Flagged)
	if len(got.Observations) != 1 {
		t.Fatalf("observations = %+v, want one", got.Observations)
	}
	o := got.Observations[0]
	if o.State != taskstore.Unobserved || !strings.Contains(o.Error, "permission denied") || o.Size != nil || o.SHA256 != "" {
		t.Errorf("observation = %+v, want unobserved with the read error", o)
	}
	log := h.logText(task.ID)
	if strings.Contains(log, "absent") {
		t.Errorf("an unreadable file was narrated as absent:\n%s", log)
	}
	if !strings.Contains(log, "unobserved locked.md") {
		t.Errorf("log does not narrate the unobserved path:\n%s", log)
	}
}

// TestTheAuditReadsTheBytesTheObservationHashed guards that the audited
// document is the observed one: each path is read once, and the audit sees
// exactly the bytes whose hash the record carries.
func TestTheAuditReadsTheBytesTheObservationHashed(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	var mu sync.Mutex
	reads := 0
	h.deps.ReadFile = func(_, rel string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if rel != "memo.md" {
			return nil, os.ErrNotExist
		}
		reads++
		return []byte(strings.Repeat("v", reads)), nil
	}
	var audited string
	h.audit = func(doc grounding.Document, _ []grounding.Source) grounding.DocumentReport {
		audited = doc.Content
		return grounding.DocumentReport{Path: doc.Path, Checked: true, Claims: 1}
	}
	h.start()
	task := h.submit(approval.ModeAsk)
	b.feed(ready(), fileEvent("/workspace/memo.md"), runComplete(), filesRead())
	b.end()

	got := h.waitState(task.ID, taskstore.Done)
	if got.Observations[0].SHA256 != sha256Hex(audited) {
		t.Errorf("observed sha256 %s is not the hash of the audited bytes %q", got.Observations[0].SHA256, audited)
	}
	mu.Lock()
	defer mu.Unlock()
	if reads != 1 {
		t.Errorf("memo.md read %d times, want once", reads)
	}
}

// TestADeletedHistoryRecordIsNotAFileTheTaskWrote guards the verb a Crush file
// event carries: "deleted" means Crush dropped its history record, not that the
// task wrote the path, so it is neither a file event nor an absence to flag.
func TestADeletedHistoryRecordIsNotAFileTheTaskWrote(t *testing.T) {
	h := newHarness(t)
	b := newFakeBridge()
	h.bridges <- b
	h.start()
	task := h.submit(approval.ModeAsk)
	dropped := crushapi.Event{Kind: crushapi.KindFile, File: &crushapi.FileEvent{Path: "/workspace/old.md", Op: crushapi.FileOpDeleted}}
	b.feed(ready(), dropped, runComplete(), filesRead())
	b.end()

	got := h.waitState(task.ID, taskstore.Done)
	if len(got.Files) != 0 || len(got.Observations) != 0 {
		t.Errorf("files = %+v, observations = %+v; a dropped history record is not a write", got.Files, got.Observations)
	}
}
