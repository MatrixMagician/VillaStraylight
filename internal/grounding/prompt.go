package grounding

import (
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// auditInstruction is the prompt from the prototype's grounding-audit.sh,
// carried verbatim: claims listed, each with a supporting passage or
// UNSUPPORTED, arithmetic from source numbers counting as supported when
// shown.
const auditInstruction = "You are auditing a document for unsupported claims. Below are the SOURCES the author had, then the DOCUMENT.\n" +
	"List every factual claim in the DOCUMENT as a numbered list. For each claim, either quote the exact passage from a SOURCE that supports it (with the source name), or write UNSUPPORTED. Arithmetic that follows from source numbers counts as supported if you show the numbers.\n" +
	"Finish with one line: 'TOTAL: <n> claims, <m> unsupported'.\n"

// Prompt builds the audit chat request. It leaves Model unset. The model to
// call is the runner's decision (the same chat unit that wrote the document),
// not the auditor's. Temperature is pinned to 0 and thinking is disabled via
// chat_template_kwargs (spec §6: with thinking on, the prototype's model
// spent its whole token budget reasoning and returned no content).
func Prompt(doc Document, sources []Source) llm.ChatRequest {
	var b strings.Builder
	b.WriteString(auditInstruction)
	for _, s := range sources {
		b.WriteString("\n\n===== SOURCE: ")
		b.WriteString(s.Path)
		b.WriteString("\n")
		b.WriteString(s.Content)
	}
	b.WriteString("\n\n===== DOCUMENT\n")
	b.WriteString(doc.Content)

	temperature := 0.0
	return llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: b.String()},
		},
		Temperature:        &temperature,
		ChatTemplateKwargs: map[string]any{"enable_thinking": false},
	}
}
