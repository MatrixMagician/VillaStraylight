package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// TestRequestIsGreedyAndBounded guards ADR-0018's completion rule: every capability
// case is sent at temperature 0 (an explicit 0, not an omitted field), with thinking
// disabled through chat_template_kwargs and the case's own token bound. A tool case
// also carries its tool definitions and tool_choice "auto"; a text case carries
// neither. The system prompt, when present, precedes the user prompt.
func TestRequestIsGreedyAndBounded(t *testing.T) {
	tools := []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "f"}}}
	req := Request(Case{ID: "a", System: "be terse", Prompt: "hi", MaxTokens: 32, Tools: tools})

	if req.Temperature == nil || *req.Temperature != 0 {
		t.Errorf("temperature = %v, want an explicit 0", req.Temperature)
	}
	if v, ok := req.ChatTemplateKwargs["enable_thinking"]; !ok || v != false {
		t.Errorf("chat_template_kwargs = %v, want enable_thinking=false", req.ChatTemplateKwargs)
	}
	if req.MaxTokens != 32 {
		t.Errorf("max_tokens = %d, want 32", req.MaxTokens)
	}
	if len(req.Tools) != 1 || req.ToolChoice != "auto" {
		t.Errorf("tools = %v, tool_choice = %q, want the case's tool and auto", req.Tools, req.ToolChoice)
	}
	want := []llm.Message{{Role: llm.RoleSystem, Content: "be terse"}, {Role: llm.RoleUser, Content: "hi"}}
	if len(req.Messages) != 2 || req.Messages[0] != want[0] || req.Messages[1] != want[1] {
		t.Errorf("messages = %v, want %v", req.Messages, want)
	}

	plain := Request(Case{ID: "b", Prompt: "hi", MaxTokens: 8})
	if len(plain.Messages) != 1 || plain.Messages[0].Role != llm.RoleUser {
		t.Errorf("a case without a system prompt sent %v", plain.Messages)
	}
	if plain.Tools != nil || plain.ToolChoice != "" {
		t.Errorf("a text case sent tools %v / tool_choice %q", plain.Tools, plain.ToolChoice)
	}
}

// TestExecuteRunsSequentiallyAndClassifiesEachCase guards ADR-0018's run rules:
// cases run one at a time in suite order; a graded reply is passed or failed with an
// excerpt; a completion that errors is unconducted (never failed, never passed); and
// with tools mode off a tool case is skipped without being sent at all.
func TestExecuteRunsSequentiallyAndClassifiesEachCase(t *testing.T) {
	cases := []Case{
		{ID: "pass", Prompt: "p1", MaxTokens: 8, Grader: Grader{Kind: KindExact, Want: "yes"}},
		{ID: "fail", Prompt: "p2", MaxTokens: 8, Grader: Grader{Kind: KindExact, Want: "yes"}},
		{ID: "down", Prompt: "p3", MaxTokens: 8, Grader: Grader{Kind: KindExact, Want: "yes"}},
		{ID: "tool", Prompt: "p4", MaxTokens: 8, Tools: []llm.Tool{{Type: "function"}},
			Grader: Grader{Kind: KindTool, Want: "f", Keys: map[string]any{"x": 1.0}}},
	}
	replies := map[string]llm.Reply{"p1": text("Yes"), "p2": text("no")}
	var sent []string
	inFlight := 0
	d := Deps{Complete: func(_ context.Context, req llm.ChatRequest) (llm.Reply, error) {
		inFlight++
		defer func() { inFlight-- }()
		if inFlight != 1 {
			t.Errorf("%d completions in flight, want 1", inFlight)
		}
		prompt := req.Messages[len(req.Messages)-1].Content
		sent = append(sent, prompt)
		if r, ok := replies[prompt]; ok {
			return r, nil
		}
		return llm.Reply{}, errors.New("connection refused")
	}}

	got := Execute(t.Context(), cases, d)

	want := []Result{
		{CaseID: "pass", Status: Passed, Excerpt: "Yes"},
		{CaseID: "fail", Status: Failed, Excerpt: "no"},
		{CaseID: "down", Status: Unconducted, Detail: "connection refused"},
		{CaseID: "tool", Status: Skipped, Detail: "tools mode off"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if strings.Join(sent, ",") != "p1,p2,p3" {
		t.Errorf("sent %v, want p1,p2,p3 in order and the tool case not sent", sent)
	}

	d.ToolsOn = true
	replies["p4"] = call(llm.ToolCall{Name: "f", Arguments: `{"x":1}`})
	if r := Execute(t.Context(), cases[3:], d); r[0].Status != Passed || r[0].Excerpt != `f({"x":1})` {
		t.Errorf("with tools mode on the tool case = %+v, want passed with the call as its excerpt", r[0])
	}
}

// TestExcerptIsOneBoundedLine: a reply excerpt collapses whitespace onto one line,
// names each tool call after any content, and is cut at excerptRunes runes on a rune
// boundary, so a regression report stays readable and never splits a character.
func TestExcerptIsOneBoundedLine(t *testing.T) {
	if got := excerpt(llm.Reply{Content: " a\n\n b\t", ToolCalls: []llm.ToolCall{{Name: "f", Arguments: "{}"}}}); got != "a b f({})" {
		t.Errorf("excerpt = %q", got)
	}
	long := strings.Repeat("é", excerptRunes+10)
	got := excerpt(text(long))
	if r := []rune(got); len(r) != excerptRunes+1 || r[len(r)-1] != '…' {
		t.Errorf("long excerpt has %d runes ending %q, want %d plus an ellipsis", len(r), r[len(r)-1], excerptRunes)
	}
}
