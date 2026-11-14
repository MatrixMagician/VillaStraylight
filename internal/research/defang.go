package research

// defang.go is the report body's guard. The body is model output written after
// reading untrusted pages, so it is the one place an injection can reach the
// operator's eyes. The control is not a Markdown sanitiser (ADR-0045 records why: a
// regex racing a Markdown parser lost three times in review, to an escaped bracket,
// to a container-marked heading and to a scheme glued to emphasis). It is two
// controls that do not depend on how any viewer parses:
//
//   - Flag. inspect looks at the body after the NFKC normalisation the page guard
//     uses, so a fullwidth letter or a zero-width character cannot hide a scheme, and
//     it looks for substrings, with no word boundary and no Markdown structure: any
//     "://", "www.", "//host", tag opener, link or image or definition shape, or
//     script scheme ends the task flagged, naming what was found.
//   - Defang. The report is a plain-text file, which nothing renders and nothing
//     auto-loads, and the body is written with those shapes broken anyway, as a belt
//     for a viewer that renders it. Every backslash is doubled in the same pass that
//     adds villa's own escapes, so a backslash the model wrote cannot cancel one of
//     villa's.
//
// Neither claims the report is safe. Prose can still mislead; this removes what a
// viewer would act on and says so when it had to.

import (
	"regexp"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/websafe"
)

// detector is one shape of active content and the words used to report it.
type detector struct {
	kind string
	re   *regexp.Regexp
}

// detectors run over the normalised body, as plain substrings and character
// classes: no \b, no line anchors, nothing that needs Markdown to have parsed.
var detectors = []detector{
	{"a URL (scheme://)", regexp.MustCompile(`://`)},
	{"a www. address", regexp.MustCompile(`(?i)www\.`)},
	{"a protocol-relative //host address", regexp.MustCompile(`(?:^|[^:])//[A-Za-z0-9]`)},
	{"an HTML tag or comment", regexp.MustCompile(`<[A-Za-z/!?]`)},
	{"a Markdown link or image", regexp.MustCompile(`\]\(|!\[`)},
	{"a Markdown reference definition", regexp.MustCompile(`\]:`)},
	{"a script or mail scheme", regexp.MustCompile(`(?i)(?:javascript|vbscript|mailto):`)},
}

// citationRe matches a source marker at the start of a string.
var citationRe = regexp.MustCompile(`^\[\d+\]`)

// inspect normalises the body and reports each kind of active content in it, once
// each. The normalised text is what is written: it is what the detectors saw.
func inspect(body string) (normalised string, kinds []string) {
	normalised = websafe.Normalize(body)
	for _, d := range detectors {
		if d.re.MatchString(normalised) {
			kinds = append(kinds, d.kind)
		}
	}
	return normalised, kinds
}

// snippet is a short, defanged window of the body around the first match of kind,
// for a finding's claim. It shows what was found without reprinting it live.
func snippet(normalised, kind string) string {
	for _, d := range detectors {
		if d.kind != kind {
			continue
		}
		loc := d.re.FindStringIndex(normalised)
		if loc == nil {
			return ""
		}
		start, end := max(0, loc[0]-20), min(len(normalised), loc[1]+40)
		for start > 0 && !isRuneStart(normalised[start]) {
			start--
		}
		for end < len(normalised) && !isRuneStart(normalised[end]) {
			end++
		}
		return clean(defang(normalised[start:end]))
	}
	return ""
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// defang writes s with every shape the detectors look for broken, in one pass.
// A backslash is doubled; "://" and any other "//" lose their second slash to an
// escape, "www." its dot, a "<" that opens a tag its meaning; a square bracket is
// escaped unless it is a citation marker [n] that is not followed by "(" or ":",
// the two things that would make it a link destination or a definition.
func defang(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		switch {
		case strings.HasPrefix(rest, "://"):
			b.WriteString(`:/\/`)
			i += 3
			continue
		case strings.HasPrefix(rest, "//"):
			b.WriteString(`/\/`)
			i += 2
			continue
		case len(rest) >= 4 && strings.EqualFold(rest[:4], "www."):
			b.WriteString(rest[:3] + `\.`)
			i += 4
			continue
		case rest[0] == '\\':
			b.WriteString(`\\`)
		case rest[0] == '<' && len(rest) > 1 && strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz/!?", rune(rest[1])):
			b.WriteString(`\<`)
		case rest[0] == '[':
			if m := citationRe.FindString(rest); m != "" {
				next := byte(0)
				if len(rest) > len(m) {
					next = rest[len(m)]
				}
				if next != '(' && next != ':' {
					b.WriteString(m)
					i += len(m)
					continue
				}
			}
			b.WriteString(`\[`)
		case rest[0] == ']':
			b.WriteString(`\]`)
		default:
			b.WriteByte(rest[0])
		}
		i++
	}
	return b.String()
}
