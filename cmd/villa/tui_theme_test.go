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

// TestVillaKeyMap asserts the first-party footer key map carries the contracted
// 17-UI-SPEC.md Interaction Contract vocabulary (Pillar 2): navigation keys (↑/↓),
// the Tab/Shift+Tab field motion, the y/n Confirm answers, and a clean esc/ctrl+c
// abort. The KEYS stay at huh defaults; only the help LABELS are first-party owned.
// villaKeyMap is a pure func — callable in CI with no terminal.
func TestVillaKeyMap(t *testing.T) {
	km := villaKeyMap()
	if km == nil {
		t.Fatalf("villaKeyMap() = nil, want a *huh.KeyMap")
	}

	// Navigation: the Select up/down help glyphs carry the contracted ↑/↓ vocabulary.
	if got := km.Select.Up.Help().Key; !strings.Contains(got, "↑") {
		t.Errorf("Select.Up help key = %q, want it to contain ↑", got)
	}
	if got := km.Select.Down.Help().Key; !strings.Contains(got, "↓") {
		t.Errorf("Select.Down help key = %q, want it to contain ↓", got)
	}

	// Field motion: a Tab next + a Shift+Tab "back" prev (the contracted Tab/Shift+Tab).
	if got := km.Note.Prev.Help().Key; !strings.Contains(got, "shift+tab") {
		t.Errorf("Note.Prev help key = %q, want it to contain shift+tab", got)
	}
	if got := km.Note.Prev.Help().Desc; got != "back" {
		t.Errorf("Note.Prev help desc = %q, want %q", got, "back")
	}

	// Confirm answers: the contracted y/n.
	if got := km.Confirm.Accept.Help().Key; got != "y" {
		t.Errorf("Confirm.Accept help key = %q, want %q", got, "y")
	}
	if got := km.Confirm.Reject.Help().Key; got != "n" {
		t.Errorf("Confirm.Reject help key = %q, want %q", got, "n")
	}

	// Abort: the Quit (ctrl+c) help renders the contracted "cancel" affordance.
	if got := km.Quit.Help().Desc; got != "cancel" {
		t.Errorf("Quit help desc = %q, want %q", got, "cancel")
	}
}

// TestStepTwoCTA asserts the step-2 Select advance affordance surfaces the contracted
// "use this model" CTA in the footer help (17-UI-SPEC.md Copywriting "Use this model":
// Enter-to-advance IS the confirm). The Select Next/Submit help desc carries the CTA.
func TestStepTwoCTA(t *testing.T) {
	km := villaKeyMap()
	const cta = "use this model"
	if got := km.Select.Next.Help().Desc; got != cta {
		t.Errorf("Select.Next help desc = %q, want the contracted CTA %q", got, cta)
	}
	if got := km.Select.Submit.Help().Desc; got != cta {
		t.Errorf("Select.Submit help desc = %q, want the contracted CTA %q", got, cta)
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
