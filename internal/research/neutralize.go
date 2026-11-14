package research

// neutralize.go is the report body's guard. The body is model output written after
// reading untrusted pages, so it is the one place an injection can reach the
// operator's eyes. Two things are defended here, and neither claims the report is
// safe (ADR-0045):
//
//   - Active content. A Markdown image or link, an autolink, a bare URL or raw HTML
//     in the body can make the operator's viewer fetch an attacker's URL with data in
//     it (the markdown-image residual of ADR-0002) or show a link that is not what it
//     says. All of it is rendered inert, and each kind found is a finding that ends
//     the task flagged. A fetched source's own URL is not live either: it becomes
//     that source's [n] marker.
//   - The source list. villa's list is the only thing the operator should trust to
//     say what was fetched. A heading in the body that reads as the list, in any
//     heading style, and a code fence that could swallow the real list, are escaped,
//     so the finished report has one Sources heading and it is villa's, last.
//
// The citation markers [n] stay. Everything else in square brackets is escaped, so
// no link or reference syntax survives for a marker to be attached to.

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
)

// urlLike matches a scheme URL, a www. host and the script-ish schemes a renderer
// may act on. It stops at whitespace, quotes, angle brackets and closing brackets,
// so a URL inside (...) or [...] is taken without its wrapper.
var urlLike = regexp.MustCompile("(?i)\\b[a-z][a-z0-9+.-]{1,15}://[^\\s<>\"'`)\\]]*|\\bwww\\.[^\\s<>\"'`)\\]]*|\\b(?:javascript|data|vbscript|mailto):[^\\s<>\"'`)\\]]*")

var (
	imageSyntax = regexp.MustCompile(`!\[[^\]]*\]\(`)
	linkSyntax  = regexp.MustCompile(`\]\(`)
	refDef      = regexp.MustCompile(`(?m)^\s{0,3}\[[^\]]+\]:`)
	htmlTag     = regexp.MustCompile(`<[A-Za-z/!?]`)
	atxHeading  = regexp.MustCompile(`^(\s{0,3})#{1,6}\s*(.*?)\s*#*\s*$`)
	setextRule  = regexp.MustCompile(`^(\s{0,3})(?:=+|-+)\s*$`)
	fenceOpen   = regexp.MustCompile("^(\\s{0,3})(`{3,}|~{3,})")
	citationAt  = regexp.MustCompile(`^\[\d+\]`)
)

// sourceHeadings are the words a body heading may not use, however it is drawn.
var sourceHeadings = map[string]bool{"source": true, "sources": true, "reference": true, "references": true, "bibliography": true}

// neutralize returns the body with its active content and any forged source list
// made inert, and one finding per kind of active content it found.
func neutralize(body string, sources []Source) (string, []grounding.Unsupported) {
	body = stripInvisible(body)
	findings := activeFindings(body, sources)

	body = urlLike.ReplaceAllStringFunc(body, func(m string) string {
		core := strings.TrimRight(m, ".,;:!?")
		for _, s := range sources {
			if core == s.URL {
				return fmt.Sprintf("[%d]", s.N) + m[len(core):]
			}
		}
		return "(link removed)"
	})
	body = htmlTag.ReplaceAllStringFunc(body, func(m string) string { return "&lt;" + m[1:] })
	body = escapeBrackets(body)
	return escapeStructure(body), findings
}

// stripInvisible drops control and format runes (zero-width, bidi overrides) other
// than newline and tab, so a heading cannot be spelled to look like villa's while
// matching nothing, and normalises line endings.
func stripInvisible(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' || (!unicode.IsControl(r) && !unicode.Is(unicode.Cf, r)) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// activeFindings reports each kind of active content once, with the first
// occurrence as the claim. A source's own URL is not a finding.
func activeFindings(body string, sources []Source) []grounding.Unsupported {
	var out []grounding.Unsupported
	add := func(kind, snippet string) {
		out = append(out, grounding.Unsupported{
			Claim:  label(snippet),
			Reason: "the report carried " + kind + ", which villa removed",
		})
	}
	if imageSyntax.MatchString(body) {
		add("a Markdown image", at(body, imageSyntax))
	}
	if linkSyntax.MatchString(body) {
		add("a Markdown link", at(body, linkSyntax))
	}
	if refDef.MatchString(body) {
		add("a reference-style link definition", at(body, refDef))
	}
	if htmlTag.MatchString(body) {
		add("raw HTML", at(body, htmlTag))
	}
	for _, m := range urlLike.FindAllString(body, -1) {
		core := strings.TrimRight(m, ".,;:!?")
		own := false
		for _, s := range sources {
			own = own || core == s.URL
		}
		if !own {
			add("a URL that is not one of the fetched sources", m)
			break
		}
	}
	return out
}

// at returns a short window of body around the first match of re.
func at(body string, re *regexp.Regexp) string {
	loc := re.FindStringIndex(body)
	if loc == nil {
		return ""
	}
	end := min(len(body), loc[1]+40)
	return body[loc[0]:end]
}

// escapeBrackets escapes every square bracket except a citation marker [n] that is
// not followed by ( or :, which are the two things that would make it a link
// destination or a reference definition.
func escapeBrackets(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch s[i] {
		case '[':
			if m := citationAt.FindString(s[i:]); m != "" {
				next := byte(0)
				if i+len(m) < len(s) {
					next = s[i+len(m)]
				}
				if next != '(' && next != ':' {
					b.WriteString(m)
					i += len(m)
					continue
				}
			}
			b.WriteString(`\[`)
		case ']':
			b.WriteString(`\]`)
		default:
			b.WriteByte(s[i])
		}
		i++
	}
	return b.String()
}

// escapeStructure escapes, line by line, a heading that reads as a source list
// (ATX or setext) and any code-fence opener, by putting a backslash in front of
// the first marker character.
func escapeStructure(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if m := atxHeading.FindStringSubmatch(line); m != nil && strings.HasPrefix(strings.TrimLeft(line, " "), "#") &&
			sourceHeadings[strings.ToLower(strings.TrimRight(m[2], ":"))] {
			lines[i] = m[1] + `\` + strings.TrimLeft(line, " ")
			continue
		}
		if sourceHeadings[strings.ToLower(strings.TrimRight(strings.TrimSpace(line), ":"))] && i+1 < len(lines) {
			if m := setextRule.FindStringSubmatch(lines[i+1]); m != nil {
				lines[i+1] = m[1] + `\` + strings.TrimLeft(lines[i+1], " ")
			}
		}
		if m := fenceOpen.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + `\` + strings.TrimLeft(line, " ")
		}
	}
	return strings.Join(lines, "\n")
}
