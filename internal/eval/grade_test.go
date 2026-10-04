package eval

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// text is a reply carrying content only.
func text(s string) llm.Reply { return llm.Reply{Content: s} }

// call is a reply carrying the given tool calls and no content.
func call(calls ...llm.ToolCall) llm.Reply { return llm.Reply{ToolCalls: calls} }

// TestGradersAreDeterministicAndStrict pins each grader kind's rule (ADR-0018:
// graders are deterministic, never the model judging itself). exact trims and
// case-folds and nothing more; regex matches anywhere; json reads one object,
// optionally inside one ```json fence, and checks each required key; tool wants
// exactly one call of the named function whose arguments hold the required keys;
// no_tool wants no call and an answer matching the pattern.
func TestGradersAreDeterministicAndStrict(t *testing.T) {
	readFile := llm.ToolCall{Name: "read_file", Arguments: `{"path":"notes.txt","lines":10}`}
	for _, c := range []struct {
		name  string
		g     Grader
		reply llm.Reply
		want  bool
	}{
		{"exact trims and case-folds", Grader{Kind: KindExact, Want: "no"}, text("  No\n"), true},
		{"exact refuses trailing punctuation", Grader{Kind: KindExact, Want: "no"}, text("No."), false},
		{"exact refuses extra words", Grader{Kind: KindExact, Want: "no"}, text("no, it is not"), false},

		{"regex matches anywhere", Grader{Kind: KindRegex, Pattern: `\b42\b`}, text("The answer is 42."), true},
		{"regex is case-sensitive unless asked", Grader{Kind: KindRegex, Pattern: `^\s*BLUE\s*$`}, text("blue"), false},
		{"regex refuses a miss", Grader{Kind: KindRegex, Pattern: `\b42\b`}, text("421"), false},
		{"regex that does not compile never passes", Grader{Kind: KindRegex, Pattern: `(`}, text("("), false},

		{"json object with the required keys", Grader{Kind: KindJSON, Keys: map[string]any{"name": "Ada", "age": 36.0}},
			text(`{"name":"Ada","age":36,"extra":true}`), true},
		{"json inside one json fence", Grader{Kind: KindJSON, Keys: map[string]any{"even": true}},
			text("```json\n{\"even\": true}\n```"), true},
		{"json with a wrong value", Grader{Kind: KindJSON, Keys: map[string]any{"age": 36.0}}, text(`{"age":"36"}`), false},
		{"json missing a key", Grader{Kind: KindJSON, Keys: map[string]any{"age": nil}}, text(`{"name":"Ada"}`), false},
		{"json array is not one object", Grader{Kind: KindJSON, Keys: map[string]any{"a": 1.0}}, text(`[{"a":1}]`), false},
		{"json with two objects", Grader{Kind: KindJSON, Keys: map[string]any{"a": 1.0}}, text(`{"a":1} {"a":1}`), false},
		{"json with prose around it", Grader{Kind: KindJSON, Keys: map[string]any{"a": 1.0}}, text(`Here: {"a":1}`), false},
		{"json with an unterminated fence", Grader{Kind: KindJSON, Keys: map[string]any{"a": 1.0}}, text("```json\n{\"a\":1}"), false},

		{"tool call with the required arguments", Grader{Kind: KindTool, Want: "read_file", Keys: map[string]any{"path": "notes.txt"}},
			call(readFile), true},
		{"tool call of the wrong function", Grader{Kind: KindTool, Want: "write_file", Keys: map[string]any{"path": "notes.txt"}},
			call(readFile), false},
		{"tool call with a wrong argument", Grader{Kind: KindTool, Want: "read_file", Keys: map[string]any{"lines": 20.0}},
			call(readFile), false},
		{"tool call with unparseable arguments", Grader{Kind: KindTool, Want: "read_file", Keys: map[string]any{"path": "notes.txt"}},
			call(llm.ToolCall{Name: "read_file", Arguments: `{"path":`}), false},
		{"two tool calls", Grader{Kind: KindTool, Want: "read_file", Keys: map[string]any{"path": "notes.txt"}},
			call(readFile, readFile), false},
		{"no tool call where one was wanted", Grader{Kind: KindTool, Want: "read_file", Keys: map[string]any{"path": "notes.txt"}},
			text("I would read notes.txt"), false},

		{"no tool call and the answer", Grader{Kind: KindNoTool, Pattern: `\b144\b`}, text("144"), true},
		{"a tool call where none was wanted", Grader{Kind: KindNoTool, Pattern: `\b144\b`}, call(readFile), false},
		{"no tool call but the wrong answer", Grader{Kind: KindNoTool, Pattern: `\b144\b`}, text("12"), false},

		{"an unknown kind never passes", Grader{Kind: "vibes"}, text("anything"), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := Grade(Case{ID: "c", Grader: c.g}, c.reply); got != c.want {
				t.Errorf("Grade = %v, want %v", got, c.want)
			}
		})
	}
}
