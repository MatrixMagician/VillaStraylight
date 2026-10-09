package eval

// grade.go is the grader table (ADR-0018). Each row is one kind: how it judges a
// reply and which grader fields a capability case of that kind must carry, so the
// loader can refuse a malformed case before it is ever run. Graders are
// deterministic; a model never grades its own reply.

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// Kind names a grader.
type Kind string

const (
	// KindExact accepts content equal to Want after trimming and case-folding.
	KindExact Kind = "exact"
	// KindRegex accepts content that Pattern matches anywhere.
	KindRegex Kind = "regex"
	// KindJSON accepts content that is one JSON object, optionally inside one
	// ```json fence, holding every key in Keys with an equal value.
	KindJSON Kind = "json"
	// KindTool accepts exactly one tool call, of the function named Want, whose
	// arguments parse as a JSON object holding every key in Keys.
	KindTool Kind = "tool"
	// KindNoTool accepts a reply that calls no tool and whose content Pattern
	// matches: the model answered rather than reaching for a tool that does not fit.
	KindNoTool Kind = "no_tool"
	// KindRerank accepts a reranker scoring in which the document at Top scores
	// highest alone (ADR-0028). It is judged on scores, never on a reply.
	KindRerank Kind = "rerank"
	// KindExtract accepts an extraction whose text Pattern matches (ADR-0033). It
	// is judged on the extracted text, never on a reply.
	KindExtract Kind = "extract"
)

// Grader is a case's expectation. Which fields matter is the kind's: Want for exact
// and tool, Pattern for regex, no_tool and extract, Keys for json and tool, Top for
// rerank.
type Grader struct {
	Kind    Kind           `json:"kind"`
	Want    string         `json:"want,omitempty"`
	Pattern string         `json:"pattern,omitempty"`
	Keys    map[string]any `json:"keys,omitempty"`
	// Top is the index of the document a rerank case wants scored highest.
	Top int `json:"top,omitempty"`
}

// grader is one row of the table: the judgement, the channel a case of this kind
// is conducted through, and what it must carry.
type grader struct {
	// grade judges a reply; nil for a kind conducted through the reranker or the
	// extractor.
	grade func(r llm.Reply, g Grader) bool
	// rerank marks a kind conducted through Deps.Rerank with the case's documents
	// rather than through a completion.
	rerank bool
	// extract marks a kind conducted through Deps.Extract with the case's fixture
	// rather than through a completion.
	extract bool
	// want, pattern, keys and tools say which of Want, a compilable Pattern, Keys
	// and the case's Tools a case of this kind must carry; a completion kind also
	// needs its token bound.
	want, pattern, keys, tools bool
}

// graders is the table. A new kind is a new row.
var graders = map[Kind]grader{
	KindExact:   {grade: gradeExact, want: true},
	KindRegex:   {grade: gradeRegex, pattern: true},
	KindJSON:    {grade: gradeJSON, keys: true},
	KindTool:    {grade: gradeTool, want: true, keys: true, tools: true},
	KindNoTool:  {grade: gradeNoTool, pattern: true, tools: true},
	KindRerank:  {rerank: true},
	KindExtract: {extract: true, pattern: true},
}

// Grade judges a reply against a case's grader. A kind the table does not hold, or
// one judged on scores or extracted text rather than a reply, never passes here; the loader refuses an
// unknown kind before it can run.
func Grade(c Case, r llm.Reply) bool {
	row, ok := graders[c.Grader.Kind]
	return ok && row.grade != nil && row.grade(r, c.Grader)
}

// GradeRerank judges a rerank case's scores: the document at Top must hold the
// single highest score. A tie is a failure, because the reranker did not single
// the document out, and a scoring that does not cover every document never passes.
func GradeRerank(c Case, scores []float64) bool {
	if len(scores) != len(c.Documents) || c.Grader.Top < 0 || c.Grader.Top >= len(scores) {
		return false
	}
	top := scores[c.Grader.Top]
	for i, s := range scores {
		if i != c.Grader.Top && s >= top {
			return false
		}
	}
	return true
}

// GradeExtract judges an extract case's text: Pattern must match it anywhere.
func GradeExtract(c Case, text string) bool {
	re, err := regexp.Compile(c.Grader.Pattern)
	return err == nil && re.MatchString(text)
}

// argmax is the index of the highest score, the first on a tie, and 0 for none.
func argmax(scores []float64) int {
	best := 0
	for i, s := range scores {
		if s > scores[best] {
			best = i
		}
	}
	return best
}

func gradeExact(r llm.Reply, g Grader) bool {
	return strings.EqualFold(strings.TrimSpace(r.Content), strings.TrimSpace(g.Want))
}

func gradeRegex(r llm.Reply, g Grader) bool {
	re, err := regexp.Compile(g.Pattern)
	return err == nil && re.MatchString(r.Content)
}

func gradeJSON(r llm.Reply, g Grader) bool {
	obj, ok := object(unfence(r.Content))
	return ok && holds(obj, g.Keys)
}

func gradeTool(r llm.Reply, g Grader) bool {
	if len(r.ToolCalls) != 1 || r.ToolCalls[0].Name != g.Want {
		return false
	}
	args, ok := object(r.ToolCalls[0].Arguments)
	return ok && holds(args, g.Keys)
}

func gradeNoTool(r llm.Reply, g Grader) bool {
	return len(r.ToolCalls) == 0 && gradeRegex(r, g)
}

// unfence strips one enclosing ```json fence; anything else is returned trimmed and
// otherwise untouched, so an unterminated fence fails to parse rather than passing.
func unfence(s string) string {
	s = strings.TrimSpace(s)
	body, opened := strings.CutPrefix(s, "```json")
	body, closed := strings.CutSuffix(strings.TrimSpace(body), "```")
	if !opened || !closed {
		return s
	}
	return body
}

// object parses s as exactly one JSON object. An array, a scalar, or trailing data
// after the object is refused.
func object(s string) (map[string]any, bool) {
	var obj map[string]any
	err := json.Unmarshal([]byte(s), &obj)
	return obj, err == nil && obj != nil
}

// holds reports whether obj carries every key in want with an equal value. A key
// that is absent fails even when the wanted value is null.
func holds(obj, want map[string]any) bool {
	for k, v := range want {
		got, ok := obj[k]
		if !ok || !reflect.DeepEqual(got, v) {
			return false
		}
	}
	return true
}
