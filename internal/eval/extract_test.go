package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// extractCase is an extract case over the embedded handbook fixture.
func extractCase(id, pattern string) Case {
	return Case{
		ID:       id,
		Prompt:   "q " + id,
		Document: "handbook.docx",
		Mime:     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Grader:   Grader{Kind: KindExtract, Pattern: pattern},
	}
}

// TestParseSuiteEnforcesTheExtractRules (ADR-0029): an extract case needs a
// fixture that exists under docs/, a mime and a compilable pattern, carries no
// tools and no rerank documents, and needs no token bound.
func TestParseSuiteEnforcesTheExtractRules(t *testing.T) {
	for _, c := range []struct {
		name, json, wantErr string
	}{
		{"no document", `[{"id":"a","prompt":"p","mime":"application/pdf","grader":{"kind":"extract","pattern":"x"}}]`, "a document"},
		{"absent document", `[{"id":"a","prompt":"p","document":"missing.pdf","mime":"application/pdf","grader":{"kind":"extract","pattern":"x"}}]`, "a document"},
		{"path outside docs", `[{"id":"a","prompt":"p","document":"../cases.json","mime":"application/json","grader":{"kind":"extract","pattern":"x"}}]`, "a document"},
		{"no mime", `[{"id":"a","prompt":"p","document":"handbook.docx","grader":{"kind":"extract","pattern":"x"}}]`, "a mime"},
		{"no pattern", `[{"id":"a","prompt":"p","document":"handbook.docx","mime":"application/pdf","grader":{"kind":"extract"}}]`, "grader.pattern"},
		{"bad pattern", `[{"id":"a","prompt":"p","document":"handbook.docx","mime":"application/pdf","grader":{"kind":"extract","pattern":"("}}]`, "grader.pattern"},
		{"with tools", `[{"id":"a","prompt":"p","document":"handbook.docx","mime":"application/pdf","tools":[{"type":"function","function":{"name":"f"}}],"grader":{"kind":"extract","pattern":"x"}}]`, "no tools"},
		{"with documents", `[{"id":"a","prompt":"p","document":"handbook.docx","mime":"application/pdf","documents":["x","y"],"grader":{"kind":"extract","pattern":"x"}}]`, "no documents"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := parseSuite([]byte(c.json)); err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("parseSuite err = %v, want one containing %q", err, c.wantErr)
			}
		})
	}

	ok := `[{"id":"a","prompt":"p","document":"handbook.docx","mime":"application/pdf","grader":{"kind":"extract","pattern":"6 minutes"}}]`
	cases, err := parseSuite([]byte(ok))
	if err != nil {
		t.Fatalf("a well-formed extract case without max_tokens was refused: %v", err)
	}
	if cases[0].Document != "handbook.docx" || cases[0].Mime != "application/pdf" {
		t.Errorf("parsed case = %+v", cases[0])
	}
}

// TestEmbeddedSuiteCarriesTheExtractionCases: the shipped suite holds the three
// extract cases (ADR-0029), a scanned letter that needs OCR, a table row and a
// .docx, each over a fixture the suite embeds.
func TestEmbeddedSuiteCarriesTheExtractionCases(t *testing.T) {
	cases, err := Suite()
	if err != nil {
		t.Fatalf("Suite: %v", err)
	}
	got := map[string]string{}
	for _, c := range cases {
		if c.Grader.Kind == KindExtract {
			got[c.ID] = c.Document + " " + c.Mime
		}
	}
	want := map[string]string{
		"extract-scanned-letter": "scanned-letter.pdf application/pdf",
		"extract-table-row":      "table-parts.pdf application/pdf",
		"extract-docx-handbook":  "handbook.docx application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %q, want %q (extract cases: %v)", id, got[id], w, got)
		}
	}
}

// TestFixtureReadsOnlyTheEmbeddedDocuments: a fixture name resolves inside the
// embedded docs/ and nowhere else, at its committed size.
func TestFixtureReadsOnlyTheEmbeddedDocuments(t *testing.T) {
	for name, size := range map[string]int{"scanned-letter.pdf": 120306, "table-parts.pdf": 134021, "handbook.docx": 5918} {
		data, err := Fixture(name)
		if err != nil || len(data) != size {
			t.Errorf("Fixture(%q) = %d bytes, %v; want %d bytes", name, len(data), err, size)
		}
	}
	for _, name := range []string{"", "missing.pdf", "../cases.json", "docs/handbook.docx"} {
		if _, err := Fixture(name); err == nil {
			t.Errorf("Fixture(%q) read something outside the embedded docs", name)
		}
	}
}

// TestExecuteConductsExtractCasesThroughTheExtractSeam (ADR-0029): an extract case
// never reaches the completion seam. With the extractor off it is skipped with its
// own reason; a seam error is unconducted; otherwise the fixture's bytes and mime
// are handed to the extractor and the text is graded against the pattern, with an
// excerpt that collapses the text's whitespace.
func TestExecuteConductsExtractCasesThroughTheExtractSeam(t *testing.T) {
	cases := []Case{extractCase("pass", "6 minutes"), extractCase("miss", "9 minutes"), extractCase("down", "x")}
	type call struct {
		name, mime string
		size       int
	}
	var calls []call
	d := Deps{
		Complete: func(context.Context, llm.ChatRequest) (llm.Reply, error) {
			t.Error("an extract case was sent as a completion")
			return llm.Reply{}, nil
		},
		Extract: func(_ context.Context, name, mime string, data []byte) (string, error) {
			calls = append(calls, call{name, mime, len(data)})
			if len(calls) == 3 {
				return "", errors.New("connection refused")
			}
			return "\n\nPre-heat:\n  run the timer for 6 minutes\tbelow -12 degrees.\n", nil
		},
	}

	off := Execute(t.Context(), cases, d)
	for i, r := range off {
		if r.Status != Skipped || r.Detail != "extractor off" {
			t.Errorf("extractor off: result %d = %+v, want skipped with \"extractor off\"", i, r)
		}
	}
	if len(calls) != 0 {
		t.Errorf("extractor off: %d extractions were sent, want none", len(calls))
	}

	d.ExtractOn = true
	got := Execute(t.Context(), cases, d)
	want := []Result{
		{CaseID: "pass", Status: Passed, Excerpt: "Pre-heat: run the timer for 6 minutes below -12 degrees."},
		{CaseID: "miss", Status: Failed, Excerpt: "Pre-heat: run the timer for 6 minutes below -12 degrees."},
		{CaseID: "down", Status: Unconducted, Detail: "connection refused"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	wantCall := call{"handbook.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", 5918}
	for i, c := range calls {
		if c != wantCall {
			t.Errorf("extraction %d = %+v, want %+v", i, c, wantCall)
		}
	}
}

// TestExtractWithoutASeamIsUnconducted: the gate on with no seam wired is a
// wiring fault, reported as unconducted rather than passed or skipped.
func TestExtractWithoutASeamIsUnconducted(t *testing.T) {
	got := Execute(t.Context(), []Case{extractCase("a", "x")}, Deps{ExtractOn: true})
	if got[0].Status != Unconducted || got[0].Detail != "no extractor seam" {
		t.Errorf("result = %+v, want unconducted with \"no extractor seam\"", got[0])
	}
}

// TestGradeExtractMatchesThePatternOnTheText: the judgement is the pattern on the
// extracted text, and a reply is never graded for an extract case.
func TestGradeExtractMatchesThePatternOnTheText(t *testing.T) {
	c := Case{Grader: Grader{Kind: KindExtract, Pattern: `VX-220[^\n]{0,80}41\.80`}}
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"VX-220 pump gasket set   4   41.80", true},
		{"VX-220 pump gasket set\n41.80", false},
		{"VX-210 pump gasket set 41.80", false},
	} {
		if got := GradeExtract(c, tc.text); got != tc.want {
			t.Errorf("GradeExtract(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
	if Grade(c, llm.Reply{Content: "VX-220 41.80"}) {
		t.Error("Grade accepted a reply for an extract case; an extract case is judged on extracted text")
	}
}
