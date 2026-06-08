package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// tui_theme_test.go is the Wave-0 scaffold guarding the D-09/D-10 theme contract:
// the villa theme must render with color on a normal terminal and degrade to an
// attribute-only (bold/faint/underline) variant under NO_COLOR/TERM=dumb, while the
// status-glyph helper must fall back to [OK]/[WARN]/[BLOCK] ASCII when Unicode is off.
// No live terminal is needed — the theme builder is a pure func of a colorEnabled bool.

// isNoColor reports whether a lipgloss foreground is the absence-of-color marker
// (NoColor{}) — i.e. the terminal-default foreground, no accent applied. ThemeBase
// leaves Focused.Title at NoColor{}; villaTheme(true) overrides it with the accent.
func isNoColor(c lipgloss.TerminalColor) bool {
	_, ok := c.(lipgloss.NoColor)
	return ok || c == nil
}

// TestVillaThemeColorOn asserts the color-on variant applies the accent to the
// focused title (and keeps it bold+underline) — the styled path of D-10.
func TestVillaThemeColorOn(t *testing.T) {
	th := villaTheme(true)
	title := th.Focused.Title
	if isNoColor(title.GetForeground()) {
		t.Errorf("color-on: Focused.Title foreground = no-color, want accent applied")
	}
	if !title.GetBold() {
		t.Errorf("color-on: Focused.Title not bold, want bold (Display emphasis role)")
	}
	if !title.GetUnderline() {
		t.Errorf("color-on: Focused.Title not underlined, want underline (Display emphasis role)")
	}
	if isNoColor(th.Focused.SelectSelector.GetForeground()) {
		t.Errorf("color-on: Focused.SelectSelector foreground = no-color, want accent")
	}
}

// TestVillaThemeNoColor asserts the degraded variant strips the accent Foreground
// while the wizard chrome remains functional (D-09 — degrade the THEME, not the flow).
// The theme is still a usable *huh.Theme; only color is removed.
func TestVillaThemeNoColor(t *testing.T) {
	// Drive the env-gate too: NO_COLOR set must make colorEnabled() false regardless
	// of the (non-TTY in CI) stdout state.
	t.Setenv("NO_COLOR", "1")
	if colorEnabled() {
		t.Fatalf("colorEnabled() = true under NO_COLOR=1, want false")
	}

	th := villaTheme(false)
	if th == nil {
		t.Fatalf("villaTheme(false) returned nil — the wizard must still run degraded")
	}
	// In the degraded variant the accent is NOT applied: the focused title foreground
	// stays the terminal default (no-color), so meaning relies on bold/underline + glyphs.
	if !isNoColor(th.Focused.Title.GetForeground()) {
		t.Errorf("no-color: Focused.Title foreground = colored, want stripped (terminal default)")
	}
}

// TestVillaThemeDumbTerm asserts TERM=dumb also disables color (D-09).
func TestVillaThemeDumbTerm(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if colorEnabled() {
		t.Errorf("colorEnabled() = true under TERM=dumb, want false")
	}
}

// TestStatusGlyphUnicode asserts the UI-SPEC Unicode glyphs in color/Unicode mode.
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

// TestStatusGlyphASCIIFallback asserts the ASCII fallback (UI-SPEC Iconography)
// when Unicode is disabled — the meaning must survive a non-UTF-8 terminal.
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

// TestStepHeader asserts the Step N/M renderer produces a "Step 2/5"-style token.
func TestStepHeader(t *testing.T) {
	got := stepHeader(2, 5, true)
	if !strings.Contains(got, "Step") || !strings.Contains(got, "2/5") {
		t.Errorf("stepHeader(2,5,true) = %q, want a string containing \"Step\" and \"2/5\"", got)
	}
	// Degraded mode must still produce the same textual token (D-09 — styling only).
	if degraded := stepHeader(2, 5, false); !strings.Contains(degraded, "2/5") {
		t.Errorf("stepHeader(2,5,false) = %q, want \"2/5\" preserved under no-color", degraded)
	}
}
