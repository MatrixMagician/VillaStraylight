// Package grounding is villa's second-pass claim audit (spec §6): after a
// task creates or modifies a document, one chat completion asks the SAME
// model to list every claim in the document and mark each as supported
// (quoting a source) or UNSUPPORTED. Audit reports; it never edits the
// document. A clean audit is "the auditor found none", never "grounded". The
// auditor is the model that wrote the document, so it can share its blind
// spots, and the docs must say so the way websafe's do.
//
// A report that could not be produced (a Complete error, an empty model
// answer, or an answer with no recognisable claims) has Checked=false. A
// report that WAS produced but from a response that ended mid-claim keeps
// Checked=true with the claims parsed so far, and sets Err to note the
// truncation. The runner flags on either signal, never just on Checked.
package grounding

import (
	"context"

	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// Source is one file the task's session read, offered as evidence for the
// document's claims.
type Source struct {
	Path    string
	Content string
}

// Document is the file the task created or modified, being audited.
type Document struct {
	Path    string
	Content string
}

// Unsupported is one claim the audit found no supporting passage for.
type Unsupported struct {
	Claim  string
	Reason string
}

// DocumentReport is the audit outcome for one document.
type DocumentReport struct {
	Path        string
	Checked     bool
	Claims      int
	Unsupported []Unsupported
	// Err is set both when Checked is false (why no report exists) and when
	// Checked is true but the model's response was truncated (why Claims may
	// undercount). The runner must flag on either.
	Err string
}

// Deps is the one seam Audit needs: a chat completion call. The runner wires
// this to the chat unit over loopback; Prompt already sets temperature 0 and
// disables thinking, so Deps.Complete need only send the request and return
// the message content.
type Deps struct {
	Complete func(ctx context.Context, req llm.ChatRequest) (string, error)
}

// Audit runs the second-pass claim check for doc against sources. It never
// modifies doc.
func Audit(ctx context.Context, d Deps, doc Document, sources []Source) DocumentReport {
	out, err := d.Complete(ctx, Prompt(doc, sources))
	if err != nil {
		return DocumentReport{Path: doc.Path, Checked: false, Err: err.Error()}
	}
	rep := parseReport(out)
	rep.Path = doc.Path
	return rep
}

// CitationInstruction is appended to every task instruction: it requires the
// harness to cite the workspace source file for each factual claim it
// writes, which is what Audit checks after the fact.
func CitationInstruction() string {
	return "For every factual claim you write in a document, cite the workspace source file it comes from."
}
