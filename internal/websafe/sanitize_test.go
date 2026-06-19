// sanitize_test.go guards the GUARD-02 markup-sanitization invariants: StrictPolicy
// strips ALL tags/scripts, the output is entity-DECODED plain text (Pitfall 1), and an
// all-markup input trims to empty without panicking (CR-01/CR-02 robustness).
package websafe

import (
	"strings"
	"testing"
)

// TestSanitizeStripsMarkup asserts every tag/script is removed and HTML entities are
// decoded to literal characters (entity-decode is mandatory for model-readable text).
func TestSanitizeStripsMarkup(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		absent  []string // substrings that MUST NOT appear in the output
	}{
		{
			// StrictPolicy strips tags WITHOUT inserting separators, so adjacent
			// text across a removed tag joins directly ("goodbye"+"x" -> "goodbyex").
			// The invariants under test are: script tag AND its body removed, and the
			// &amp; entity decoded to a literal &.
			name:   "script-and-entities",
			in:     `<script>alert(1)</script>Hello &amp; goodbye<b>x</b>`,
			want:   "Hello & goodbyex",
			absent: []string{"<script>", "<b>", "alert(1)", "&amp;"},
		},
		{
			name:   "entity-decoded-lt-gt",
			in:     `if (a &lt; b)`,
			want:   "if (a < b)",
			absent: []string{"&lt;"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitize(tc.in)
			if got != tc.want {
				t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
			for _, a := range tc.absent {
				if strings.Contains(got, a) {
					t.Errorf("sanitize(%q) = %q, must not contain %q", tc.in, got, a)
				}
			}
		})
	}
}

// TestSanitizeAllMarkupTrimsEmpty asserts an input that is purely markup yields an
// empty (whitespace-trimmed) string and never panics — there was no visible text to
// preserve, which is distinct from blackholing real content (CR-02).
func TestSanitizeAllMarkupTrimsEmpty(t *testing.T) {
	if got := sanitize(`<div></div>`); got != "" {
		t.Errorf("sanitize(all-markup) = %q, want \"\"", got)
	}
}

// TestSanitizePreservesVisibleText asserts visible text content survives sanitization
// (defang-not-destroy: we strip markup, we do not blackhole legitimate content).
func TestSanitizePreservesVisibleText(t *testing.T) {
	in := `<p>The quick brown fox.</p>`
	if got := sanitize(in); got != "The quick brown fox." {
		t.Errorf("sanitize(%q) = %q, want preserved text", in, got)
	}
}
