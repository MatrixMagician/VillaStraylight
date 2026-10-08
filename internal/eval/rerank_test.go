package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// rerankCase is a two-document rerank case wanting the second document first.
func rerankCase(id string) Case {
	return Case{ID: id, Prompt: "q " + id, Documents: []string{"d0", "d1"}, Grader: Grader{Kind: KindRerank, Top: 1}}
}

// TestParseSuiteEnforcesTheRerankRules (ADR-0028): a rerank case needs a prompt
// (the query), at least two documents and a top index inside them, carries no
// tools and no token bound, while a completion case still needs its token bound.
func TestParseSuiteEnforcesTheRerankRules(t *testing.T) {
	for _, c := range []struct {
		name, json, wantErr string
	}{
		{"no documents", `[{"id":"a","prompt":"p","grader":{"kind":"rerank","top":0}}]`, "documents"},
		{"one document", `[{"id":"a","prompt":"p","documents":["x"],"grader":{"kind":"rerank","top":0}}]`, "documents"},
		{"top past the end", `[{"id":"a","prompt":"p","documents":["x","y"],"grader":{"kind":"rerank","top":2}}]`, "grader.top"},
		{"top negative", `[{"id":"a","prompt":"p","documents":["x","y"],"grader":{"kind":"rerank","top":-1}}]`, "grader.top"},
		{"no prompt", `[{"id":"a","documents":["x","y"],"grader":{"kind":"rerank","top":0}}]`, "a prompt"},
		{"with tools", `[{"id":"a","prompt":"p","documents":["x","y"],"tools":[{"type":"function","function":{"name":"f"}}],"grader":{"kind":"rerank","top":0}}]`, "no tools"},
		{"completion case still needs max_tokens", `[{"id":"a","prompt":"p","grader":{"kind":"exact","want":"x"}}]`, "max_tokens"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := parseSuite([]byte(c.json)); err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("parseSuite err = %v, want one containing %q", err, c.wantErr)
			}
		})
	}

	ok := `[{"id":"a","prompt":"p","documents":["x","y"],"grader":{"kind":"rerank","top":1}}]`
	cases, err := parseSuite([]byte(ok))
	if err != nil {
		t.Fatalf("a well-formed rerank case without max_tokens was refused: %v", err)
	}
	if cases[0].Grader.Top != 1 || len(cases[0].Documents) != 2 {
		t.Errorf("parsed case = %+v", cases[0])
	}
}

// TestEmbeddedSuiteCarriesTheRetrievalCases: the shipped suite holds the two
// rerank cases measured on the dev host (ADR-0028), each wanting a document the
// embedder ranks low.
func TestEmbeddedSuiteCarriesTheRetrievalCases(t *testing.T) {
	cases, err := Suite()
	if err != nil {
		t.Fatalf("Suite: %v", err)
	}
	got := map[string]Case{}
	for _, c := range cases {
		if c.Grader.Kind == KindRerank {
			got[c.ID] = c
		}
	}
	for _, id := range []string{"retrieval-bridge-opening", "retrieval-annual-plan"} {
		c, ok := got[id]
		if !ok {
			t.Errorf("suite lacks the rerank case %q (rerank cases: %v)", id, got)
			continue
		}
		if c.Grader.Top != 2 || len(c.Documents) < 4 {
			t.Errorf("%s = top %d of %d documents, want top 2 of at least 4", id, c.Grader.Top, len(c.Documents))
		}
	}
}

// TestExecuteConductsRerankCasesThroughTheRerankSeam (ADR-0028): a rerank case
// never reaches the completion seam. With the reranker off it is skipped with its
// own reason; a seam error is unconducted; otherwise it passes when the single
// highest score sits at the wanted index, and fails on a miss or a tie.
func TestExecuteConductsRerankCasesThroughTheRerankSeam(t *testing.T) {
	cases := []Case{rerankCase("pass"), rerankCase("miss"), rerankCase("tie"), rerankCase("down")}
	scores := map[string][]float64{
		"q pass": {-2.5, 0.75},
		"q miss": {0.1, -3},
		"q tie":  {1, 1},
	}
	var queries []string
	d := Deps{
		Complete: func(context.Context, llm.ChatRequest) (llm.Reply, error) {
			t.Error("a rerank case was sent as a completion")
			return llm.Reply{}, nil
		},
		Rerank: func(_ context.Context, query string, docs []string) ([]float64, error) {
			queries = append(queries, query)
			if len(docs) != 2 {
				t.Errorf("rerank got %d documents, want the case's 2", len(docs))
			}
			if s, ok := scores[query]; ok {
				return s, nil
			}
			return nil, errors.New("connection refused")
		},
	}

	off := Execute(t.Context(), cases, d)
	for i, r := range off {
		if r.Status != Skipped || r.Detail != "reranker off" {
			t.Errorf("reranker off: result %d = %+v, want skipped with \"reranker off\"", i, r)
		}
	}
	if len(queries) != 0 {
		t.Errorf("reranker off: %d queries were sent, want none", len(queries))
	}

	d.RerankOn = true
	got := Execute(t.Context(), cases, d)
	want := []Result{
		{CaseID: "pass", Status: Passed, Excerpt: "top 1 (scores -2.500 0.750)"},
		{CaseID: "miss", Status: Failed, Excerpt: "top 0 (scores 0.100 -3.000)"},
		{CaseID: "tie", Status: Failed, Excerpt: "top 0 (scores 1.000 1.000)"},
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
	if strings.Join(queries, ",") != "q pass,q miss,q tie,q down" {
		t.Errorf("queries sent = %v, want each case's prompt in order", queries)
	}
}

// TestGradeRerankWantsOneHighestScore: the judgement is the argmax, and a tie
// for the top is a failure because the reranker did not single the document out.
func TestGradeRerankWantsOneHighestScore(t *testing.T) {
	c := Case{Documents: []string{"a", "b", "c"}, Grader: Grader{Kind: KindRerank, Top: 2}}
	for _, tc := range []struct {
		scores []float64
		want   bool
	}{
		{[]float64{-1, -2, 0.5}, true},
		{[]float64{0.5, -2, -1}, false},
		{[]float64{0.5, -2, 0.5}, false},
		{[]float64{0.5, -2}, false},
	} {
		if got := GradeRerank(c, tc.scores); got != tc.want {
			t.Errorf("GradeRerank(%v) = %v, want %v", tc.scores, got, tc.want)
		}
	}
	if Grade(c, llm.Reply{Content: "c"}) {
		t.Error("Grade accepted a reply for a rerank case; a rerank case is judged on scores")
	}
}
