// Package eval is the pure core of `villa eval` (ADR-0018): it runs the embedded
// suite of capability cases against the served model and compares each case's
// result with the eval baseline recorded for that model.
//
// A capability case is one prompt with a deterministic grader. The cases are data
// (cases.json, embedded) and the graders are a table keyed by kind, so a new case is
// a JSON entry and a new kind is a table row, never a branch at a call site.
//
// The honesty rules are the verify family's, carried per case. A case whose
// completion could not be made (transport error, timeout, inference down) is
// unconducted: it is neither passed nor failed, and any unconducted case makes the
// run a Reject. A tool-call case run while tools mode is off is skipped, which is a
// fourth outcome again: the server cannot honour tools there, so the case says
// nothing about the model, and a skipped case is never compared. A case that passed
// in the eval baseline and fails now is a regression, and a regression is Fail.
//
// PURE: no I/O. Every completion goes through Deps.Complete, which the command tier
// wires to the inference client (ADR-0014).
package eval

import (
	"context"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// Case is one capability case: a prompt, an optional system prompt and tool set,
// the grader its reply must satisfy, and the token bound its completion is sent with.
type Case struct {
	ID     string `json:"id"`
	System string `json:"system,omitempty"`
	Prompt string `json:"prompt"`
	// Tools makes this a tool-call case: the definitions are sent with the request,
	// and the case is skipped when tools mode is off.
	Tools     []llm.Tool `json:"tools,omitempty"`
	Grader    Grader     `json:"grader"`
	MaxTokens int        `json:"max_tokens"`
}

// CaseStatus is one capability case's outcome in a run.
type CaseStatus string

const (
	// Passed means the case was conducted and its grader accepted the reply.
	Passed CaseStatus = "passed"
	// Failed means the case was conducted and its grader refused the reply.
	Failed CaseStatus = "failed"
	// Unconducted means no reply was obtained, so nothing was graded.
	Unconducted CaseStatus = "unconducted"
	// Skipped means the case was not sent because the stack cannot honour it.
	Skipped CaseStatus = "skipped"
)

// toolsModeOff is the reason a tool-call case is skipped.
const toolsModeOff = "tools mode off"

// Result is one capability case's outcome. Excerpt is the reply, cut to one bounded
// line, for a conducted case; Detail says why a case was unconducted or skipped.
type Result struct {
	CaseID  string     `json:"case_id"`
	Status  CaseStatus `json:"status"`
	Excerpt string     `json:"excerpt,omitempty"`
	Detail  string     `json:"detail,omitempty"`
}

// Key is what an eval baseline belongs to: the served model, its quant, and the
// suite version. Stack settings are provenance, never part of the key, so a pin move
// still finds the baseline it is being checked against.
type Key struct {
	Model        string `json:"model"`
	Quant        string `json:"quant"`
	SuiteVersion int    `json:"suite_version"`
}

// Provenance is the stack a run was made on, recorded beside the results and
// reported when it differs from the eval baseline's.
type Provenance struct {
	Backend     string `json:"backend"`
	ImageDigest string `json:"image_digest"`
	Speculation string `json:"speculation"`
	Ctx         int    `json:"ctx"`
	ToolsMode   bool   `json:"tools_mode"`
}

// Run is one pass of the suite: its key, its provenance, and one result per case in
// suite order.
type Run struct {
	Key        Key        `json:"key"`
	Provenance Provenance `json:"provenance"`
	Results    []Result   `json:"results"`
}

// Baseline is an eval baseline: a run accepted as the model's known state. Record is
// the only way one is made from a run, and it refuses a run with holes.
type Baseline Run

// Deps is the one seam a run needs: a completion. Complete sends the request Request
// built (the caller sets the model) and returns the reply, or an error when no reply
// was obtained. ToolsOn reports whether the served model was started with tool
// calling (subsystem.ToolsOn); when it is false a tool-call case is skipped unsent.
type Deps struct {
	Complete func(ctx context.Context, req llm.ChatRequest) (llm.Reply, error)
	ToolsOn  bool
}

// graded maps a grader's verdict onto the case status it earns.
var graded = map[bool]CaseStatus{true: Passed, false: Failed}

// Execute runs every case in order, one at a time, so a parallel slot cannot change
// a reply's batching, and returns one result per case.
func Execute(ctx context.Context, cases []Case, d Deps) []Result {
	results := make([]Result, 0, len(cases))
	for _, c := range cases {
		results = append(results, conduct(ctx, c, d))
	}
	return results
}

// conduct runs one case: skipped when it needs tools the stack cannot honour,
// unconducted when no reply came back, otherwise graded.
func conduct(ctx context.Context, c Case, d Deps) Result {
	if len(c.Tools) > 0 && !d.ToolsOn {
		return Result{CaseID: c.ID, Status: Skipped, Detail: toolsModeOff}
	}
	reply, err := d.Complete(ctx, Request(c))
	if err != nil {
		return Result{CaseID: c.ID, Status: Unconducted, Detail: err.Error()}
	}
	return Result{CaseID: c.ID, Status: graded[Grade(c, reply)], Excerpt: excerpt(reply)}
}

// Request builds the completion request for one case. It is greedy by construction:
// temperature an explicit 0, thinking disabled (the grounding audit's setting), and
// the case's token bound. It leaves Model unset; which model is served is the
// caller's fact, not the suite's.
func Request(c Case) llm.ChatRequest {
	temperature := 0.0
	req := llm.ChatRequest{
		Messages:           messages(c),
		Temperature:        &temperature,
		ChatTemplateKwargs: map[string]any{"enable_thinking": false},
		MaxTokens:          c.MaxTokens,
		Tools:              c.Tools,
	}
	if len(c.Tools) > 0 {
		req.ToolChoice = "auto"
	}
	return req
}

// messages is the case's system prompt, when it has one, then its user prompt.
func messages(c Case) []llm.Message {
	user := llm.Message{Role: llm.RoleUser, Content: c.Prompt}
	if c.System == "" {
		return []llm.Message{user}
	}
	return []llm.Message{{Role: llm.RoleSystem, Content: c.System}, user}
}

// excerptRunes bounds a reply excerpt.
const excerptRunes = 160

// excerpt renders a reply as one bounded line: the content, then each tool call as
// name(arguments), whitespace collapsed, cut on a rune boundary.
func excerpt(r llm.Reply) string {
	parts := []string{r.Content}
	for _, c := range r.ToolCalls {
		parts = append(parts, c.Name+"("+c.Arguments+")")
	}
	line := []rune(strings.Join(strings.Fields(strings.Join(parts, " ")), " "))
	if len(line) <= excerptRunes {
		return string(line)
	}
	return string(line[:excerptRunes]) + "…"
}
