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
	"strconv"
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
	Tools []llm.Tool `json:"tools,omitempty"`
	// Documents are the candidates of a rerank case (ADR-0028): the prompt is the
	// query, and the grader names the document that must score highest. The case is
	// conducted through Deps.Rerank, never as a completion, and is skipped when the
	// reranker is off.
	Documents []string `json:"documents,omitempty"`
	// Document names an extract case's fixture under the embedded docs/ (ADR-0029)
	// and Mime the Content-Type it is handed to the extractor with. The prompt is the
	// question the fixture answers: documentation, and the bench's query, never sent.
	// The case is conducted through Deps.Extract and skipped when the extractor is
	// off.
	Document  string `json:"document,omitempty"`
	Mime      string `json:"mime,omitempty"`
	Grader    Grader `json:"grader"`
	MaxTokens int    `json:"max_tokens,omitempty"`
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

// toolsModeOff is the reason a tool-call case is skipped, rerankerOff the reason a
// rerank case is, and extractorOff the reason an extract case is.
const (
	toolsModeOff = "tools mode off"
	rerankerOff  = "reranker off"
	extractorOff = "extractor off"
)

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

// Deps are the two seams a run needs. Complete sends the request Request built (the
// caller sets the model) and returns the reply, or an error when no reply was
// obtained. ToolsOn reports whether the served model was started with tool calling
// (subsystem.ToolsOn); when it is false a tool-call case is skipped unsent. Rerank
// scores each document against a query, one score per document in document order
// (ADR-0028); RerankOn reports whether the reranker is rendered (subsystem.RerankOn),
// and when it is false a rerank case is skipped unsent. Extract hands a fixture's
// bytes to the extractor under its name and mime and returns the extracted text
// (ADR-0029); ExtractOn reports whether the extractor is rendered
// (subsystem.ExtractOn), and when it is false an extract case is skipped unsent.
type Deps struct {
	Complete  func(ctx context.Context, req llm.ChatRequest) (llm.Reply, error)
	ToolsOn   bool
	Rerank    func(ctx context.Context, query string, docs []string) ([]float64, error)
	RerankOn  bool
	Extract   func(ctx context.Context, name, mime string, data []byte) (string, error)
	ExtractOn bool
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

// conduct runs one case through the channel its kind names: a rerank case through
// the reranker, an extract case through the extractor, every other through a
// completion. Each is skipped when the stack cannot honour it, unconducted when no
// answer came back, otherwise graded.
func conduct(ctx context.Context, c Case, d Deps) Result {
	switch row := graders[c.Grader.Kind]; {
	case row.rerank:
		return conductRerank(ctx, c, d)
	case row.extract:
		return conductExtract(ctx, c, d)
	}
	if len(c.Tools) > 0 && !d.ToolsOn {
		return Result{CaseID: c.ID, Status: Skipped, Detail: toolsModeOff}
	}
	reply, err := d.Complete(ctx, Request(c))
	if err != nil {
		return Result{CaseID: c.ID, Status: Unconducted, Detail: err.Error()}
	}
	return Result{CaseID: c.ID, Status: graded[Grade(c, reply)], Excerpt: excerpt(reply)}
}

// conductRerank scores the case's documents against its prompt and grades the
// order. A nil seam with the gate on is a wiring fault, reported as unconducted.
func conductRerank(ctx context.Context, c Case, d Deps) Result {
	if !d.RerankOn {
		return Result{CaseID: c.ID, Status: Skipped, Detail: rerankerOff}
	}
	if d.Rerank == nil {
		return Result{CaseID: c.ID, Status: Unconducted, Detail: "no reranker seam"}
	}
	scores, err := d.Rerank(ctx, c.Prompt, c.Documents)
	if err != nil {
		return Result{CaseID: c.ID, Status: Unconducted, Detail: err.Error()}
	}
	return Result{CaseID: c.ID, Status: graded[GradeRerank(c, scores)], Excerpt: rerankExcerpt(scores)}
}

// conductExtract hands the case's fixture to the extractor and grades the text. A
// nil seam with the gate on is a wiring fault, reported as unconducted.
func conductExtract(ctx context.Context, c Case, d Deps) Result {
	if !d.ExtractOn {
		return Result{CaseID: c.ID, Status: Skipped, Detail: extractorOff}
	}
	if d.Extract == nil {
		return Result{CaseID: c.ID, Status: Unconducted, Detail: "no extractor seam"}
	}
	data, err := Fixture(c.Document)
	if err != nil {
		return Result{CaseID: c.ID, Status: Unconducted, Detail: err.Error()}
	}
	text, err := d.Extract(ctx, c.Document, c.Mime, data)
	if err != nil {
		return Result{CaseID: c.ID, Status: Unconducted, Detail: err.Error()}
	}
	return Result{CaseID: c.ID, Status: graded[GradeExtract(c, text)], Excerpt: oneLine(text)}
}

// rerankExcerpt renders a rerank outcome as the index that scored highest and
// every score in document order, so a failure shows how far the wanted document
// was from the top.
func rerankExcerpt(scores []float64) string {
	parts := make([]string, 0, len(scores))
	for _, s := range scores {
		parts = append(parts, strconv.FormatFloat(s, 'f', 3, 64))
	}
	return "top " + strconv.Itoa(argmax(scores)) + " (scores " + strings.Join(parts, " ") + ")"
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
// name(arguments).
func excerpt(r llm.Reply) string {
	parts := []string{r.Content}
	for _, c := range r.ToolCalls {
		parts = append(parts, c.Name+"("+c.Arguments+")")
	}
	return oneLine(strings.Join(parts, " "))
}

// oneLine collapses s's whitespace and cuts it to excerptRunes on a rune boundary.
func oneLine(s string) string {
	line := []rune(strings.Join(strings.Fields(s), " "))
	if len(line) <= excerptRunes {
		return string(line)
	}
	return string(line[:excerptRunes]) + "…"
}
