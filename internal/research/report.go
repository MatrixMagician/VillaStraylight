package research

// report.go assembles the report file and runs the deterministic citation
// check that sits beside the grounding audit. The source table is villa's own:
// numbers, URLs, titles and websafe flags come from what was fetched, never from
// the model, so a model steered by a page cannot invent or rewrite a source.

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
)

// maxLabelRunes bounds a title or URL in the source table.
const maxLabelRunes = 160

// citation matches a source number in square brackets, the form the report
// prompt asks for.
var citation = regexp.MustCompile(`\[(\d+)\]`)

// label makes web-supplied text safe to print in a Markdown line: whitespace
// collapsed, control and format runes dropped, bounded, and the characters that
// would turn it into a link or an image or end the code span removed.
func label(s string) string {
	s = clean(s)
	s = strings.NewReplacer("`", "'", "[", "(", "]", ")", "<", "(", ">", ")", "!", "").Replace(s)
	if r := []rune(s); len(r) > maxLabelRunes {
		s = string(r[:maxLabelRunes]) + "..."
	}
	return s
}

// render is the report file: the question, the model's body and the source table.
func render(question, body string, sources []Source) string {
	var b strings.Builder
	b.WriteString("# " + label(question) + "\n\n")
	b.WriteString(strings.TrimSpace(body) + "\n\n## Sources\n\n")
	for _, s := range sources {
		fmt.Fprintf(&b, "%d. `%s`", s.N, label(s.URL))
		if t := label(s.Title); t != "" {
			b.WriteString(" " + t)
		}
		if s.Flagged {
			b.WriteString(" (flagged by websafe")
			if len(s.Rules) > 0 {
				b.WriteString(": " + label(strings.Join(s.Rules, ", ")))
			}
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// citationFindings is the deterministic half of the audit. A report that cites
// nothing has made claims with no source, and a number that names no fetched
// source points at nothing. Each is reported in the audit's own shape, so the
// record and the terminal treat them like any unsupported claim.
func citationFindings(body string, sources int) []grounding.Unsupported {
	cited := map[int]bool{}
	for _, m := range citation.FindAllStringSubmatch(body, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			n = -1
		}
		cited[n] = true
	}
	if len(cited) == 0 {
		return []grounding.Unsupported{{Claim: "(the whole report)", Reason: "the report cites no source"}}
	}
	var bad []int
	for n := range cited {
		if n < 1 || n > sources {
			bad = append(bad, n)
		}
	}
	sort.Ints(bad)
	var out []grounding.Unsupported
	for _, n := range bad {
		out = append(out, grounding.Unsupported{
			Claim:  fmt.Sprintf("[%d]", n),
			Reason: fmt.Sprintf("the report cites source %d, and only %d were fetched", n, sources),
		})
	}
	return out
}
