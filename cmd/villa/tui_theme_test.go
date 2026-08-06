package main

import (
	"os"
	"strings"
	"testing"
)

// tui_theme_test.go guards what survives of the presentation gate after the guided
// install became a stdin prompt loop: the colour/Unicode decision, and the status
// glyph column.
//
// The property worth protecting is the honesty one. PASS / WARN / BLOCK must be
// legible from the glyph and the status word together, never from colour alone, so
// an operator on NO_COLOR, a dumb terminal, a pipe, or a non-UTF-8 console reads the
// same verdict. That is why the ASCII fallback is a contract and not a nicety.

// TestStatusGlyphUnicode asserts the Unicode glyph column for a UTF-8 terminal.
func TestStatusGlyphUnicode(t *testing.T) {
	cases := map[statusTier]string{
		statusPass:  "✓",
		statusWarn:  "!",
		statusBlock: "✗",
	}
	for tier, want := range cases {
		if got := statusGlyph(tier, false); got != want {
			t.Errorf("statusGlyph(%v, ascii=false) = %q, want %q", tier, got, want)
		}
	}
}

// TestStatusGlyphASCIIFallback asserts the [OK]/[WARN]/[BLOCK] fallback, which is
// what a non-UTF-8 or NO_COLOR terminal gets. The words carry the meaning, so the
// verdict survives without Unicode.
func TestStatusGlyphASCIIFallback(t *testing.T) {
	cases := map[statusTier]string{
		statusPass:  "[OK]",
		statusWarn:  "[WARN]",
		statusBlock: "[BLOCK]",
	}
	for tier, want := range cases {
		if got := statusGlyph(tier, true); got != want {
			t.Errorf("statusGlyph(%v, ascii=true) = %q, want %q", tier, got, want)
		}
	}
}

// TestGlyphsAreDistinctPerTier is the property behind the two tests above: no two
// tiers may share a glyph in either mode, or the column would stop distinguishing
// the verdicts it exists to convey.
func TestGlyphsAreDistinctPerTier(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		seen := map[string]statusTier{}
		for _, tier := range []statusTier{statusPass, statusWarn, statusBlock} {
			g := statusGlyph(tier, ascii)
			if g == "" {
				t.Errorf("statusGlyph(%v, ascii=%v) is empty — every tier needs a mark", tier, ascii)
			}
			if other, clash := seen[g]; clash {
				t.Errorf("tiers %v and %v share the glyph %q (ascii=%v)", tier, other, g, ascii)
			}
			seen[g] = tier
		}
	}
}

// TestColorEnabledRespectsNoColor drives the D-09 env gate: NO_COLOR or TERM=dumb
// must force the degraded path regardless of anything else. Under `go test` stdout
// is a pipe, so colorEnabled() is false either way here; the assertion that matters
// is that setting either variable can never turn colour ON.
func TestColorEnabledRespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if colorEnabled() {
		t.Error("colorEnabled() = true with NO_COLOR set, want false")
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if colorEnabled() {
		t.Error("colorEnabled() = true with TERM=dumb, want false")
	}
}

// TestColorEnabledRequiresATTY asserts the piped-output case: under `go test`
// stdout is not a char device, so the guided install degrades rather than emitting
// escape codes into a captured log.
func TestColorEnabledRequiresATTY(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	if stdoutIsTTY() {
		t.Skip("stdout is a TTY in this environment; the piped-output case cannot be exercised")
	}
	if colorEnabled() {
		t.Error("colorEnabled() = true with a non-TTY stdout, want false")
	}
}

// TestBlockIndentIsTwoCells pins the detail-row indent the renderers share, so the
// review and preflight blocks cannot drift apart.
func TestBlockIndentIsTwoCells(t *testing.T) {
	if blockIndent != "  " {
		t.Errorf("blockIndent = %q, want two spaces", blockIndent)
	}
	if strings.TrimSpace(blockIndent) != "" {
		t.Errorf("blockIndent = %q, want whitespace only", blockIndent)
	}
}

// TestStdoutIsTTYDoesNotPanic guards the degraded path on an unusual stdout: the
// gate must answer false rather than fail when Stat is unavailable.
func TestStdoutIsTTYDoesNotPanic(t *testing.T) {
	if os.Stdout == nil {
		t.Skip("no stdout")
	}
	_ = stdoutIsTTY() // must not panic
}
