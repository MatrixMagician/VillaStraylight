package grounding

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// TestAuditCompleteErrors guards that a model-call failure never falls
// through to a passing report: Checked is false and Err carries the error.
func TestAuditCompleteErrors(t *testing.T) {
	wantErr := errors.New("connection refused")
	d := Deps{Complete: func(ctx context.Context, req llm.ChatRequest) (string, error) {
		return "", wantErr
	}}
	doc := Document{Path: "workspace/memo.md", Content: "the memo body"}

	rep := Audit(context.Background(), d, doc, nil)

	if rep.Checked {
		t.Fatal("Checked = true, want false on a Complete error")
	}
	if rep.Path != doc.Path {
		t.Errorf("Path = %q, want %q", rep.Path, doc.Path)
	}
	if rep.Err != wantErr.Error() {
		t.Errorf("Err = %q, want %q", rep.Err, wantErr.Error())
	}
}

// TestAuditCannedSuccess guards the happy path end to end: Audit hands the
// canned model output to the parser and stamps the document's path onto the
// result, and it never mutates the document it was given.
func TestAuditCannedSuccess(t *testing.T) {
	canned := readFixture(t, "testdata/audit-T4.txt")
	d := Deps{Complete: func(ctx context.Context, req llm.ChatRequest) (string, error) {
		return canned, nil
	}}
	original := "the memo body, unchanged"
	doc := Document{Path: "workspace/memo.md", Content: original}

	rep := Audit(context.Background(), d, doc, []Source{{Path: "workspace/notes/a.md", Content: "note"}})

	if !rep.Checked {
		t.Fatalf("Checked = false, want true (Err=%q)", rep.Err)
	}
	if rep.Path != doc.Path {
		t.Errorf("Path = %q, want %q", rep.Path, doc.Path)
	}
	if rep.Claims != 12 || len(rep.Unsupported) != 0 {
		t.Errorf("Claims=%d Unsupported=%d, want 12/0", rep.Claims, len(rep.Unsupported))
	}
	if doc.Content != original {
		t.Errorf("doc.Content mutated to %q, want unchanged %q", doc.Content, original)
	}
}

// TestCitationInstruction guards that the citation requirement text is
// present and names the workspace-file-per-claim obligation the audit checks.
func TestCitationInstruction(t *testing.T) {
	got := CitationInstruction()
	if got == "" {
		t.Fatal("CitationInstruction() is empty")
	}
	if !strings.Contains(got, "cite") {
		t.Errorf("CitationInstruction() = %q, want it to mention citing", got)
	}
	if !strings.Contains(got, "workspace") {
		t.Errorf("CitationInstruction() = %q, want it to mention the workspace file", got)
	}
	if !strings.Contains(got, "claim") {
		t.Errorf("CitationInstruction() = %q, want it to mention factual claims", got)
	}
}
