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
)

// Grader is a case's expectation. Which fields matter is the kind's: Want for exact
// and tool, Pattern for regex and no_tool, Keys for json and tool.
type Grader struct {
	Kind    Kind           `json:"kind"`
	Want    string         `json:"want,omitempty"`
	Pattern string         `json:"pattern,omitempty"`
	Keys    map[string]any `json:"keys,omitempty"`
}

// grader is one row of the table: the judgement and what a case must carry for it.
type grader struct {
	grade func(r llm.Reply, g Grader) bool
	// want, pattern, keys and tools say which of Want, a compilable Pattern, Keys
	// and the case's Tools a case of this kind must carry.
	want, pattern, keys, tools bool
}

// graders is the table. A new kind is a new row.
var graders = map[Kind]grader{
	KindExact:  {grade: gradeExact, want: true},
	KindRegex:  {grade: gradeRegex, pattern: true},
	KindJSON:   {grade: gradeJSON, keys: true},
	KindTool:   {grade: gradeTool, want: true, keys: true, tools: true},
	KindNoTool: {grade: gradeNoTool, pattern: true, tools: true},
}

// Grade judges a reply against a case's grader. A kind the table does not hold
// never passes; the loader refuses such a case before it can run.
func Grade(c Case, r llm.Reply) bool {
	row, ok := graders[c.Grader.Kind]
	return ok && row.grade(r, c.Grader)
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
