package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// tui_theme.go is the SINGLE command-tier source of villa's TUI styling (D-10):
// the shared lipgloss/huh theme, the "Step N/M" header renderer, and the status
// glyph helpers consumed by the guided `villa install` wizard (Plan 02).
//
// It is NO_COLOR-degradable (D-09): under NO_COLOR / TERM=dumb / a non-color stdout
// the theme strips Foreground/accent while RETAINING bold/faint/underline and the
// glyph column, so the wizard still runs end-to-end — only styling is removed.
//
// It lives in cmd/villa/ by constraint: huh/lipgloss/termenv are a presentation
// concern and NO internal/* core may import them (D-11). It renders no backend or
// image literal (TestSeamGrepGate walks cmd/villa); backend names reach the wizard
// via inference.Backend accessors, never re-typed here.

// villa theme color tokens (UI-SPEC "Color" table). AdaptiveColor carries a Light
// and Dark ANSI/256 value so the same theme reads on light and dark terminals.
var (
	// accentColor is the scarce ~10% accent (indigo/violet) reserved for the
	// focused selector caret, the screen Display title, and the Step N/M current step.
	accentColor = lipgloss.AdaptiveColor{Light: "63", Dark: "105"}
	// mutedColor is the faint help/chrome grey ("Step N/M", inline help, secondary detail).
	mutedColor = lipgloss.AdaptiveColor{Light: "244", Dark: "245"}
	// passColor / warnColor / blockColor are the three status semantics (D-10):
	// PASS=green, WARN=amber, BLOCK=red. Reserved EXCLUSIVELY for preflight/result
	// status and the terminal success/abort lines, always co-occurring with a bold
	// status word and a glyph so meaning survives NO_COLOR.
	passColor  = lipgloss.AdaptiveColor{Light: "34", Dark: "42"}
	warnColor  = lipgloss.AdaptiveColor{Light: "130", Dark: "214"}
	blockColor = lipgloss.AdaptiveColor{Light: "160", Dark: "196"}
)

// statusTier is the preflight/result status a glyph + color + bold word convey.
type statusTier int

const (
	statusPass statusTier = iota
	statusWarn
	statusBlock
)

// stdoutIsTTY reports whether os.Stdout is a char device (a real terminal). It is
// the stdout twin of stdinIsInteractive (install_hostprep.go): huh renders to
// stdout/stderr, so BOTH must be a TTY for the styled wizard to make sense. A piped
// / redirected stdout → non-color → the degraded theme (and the flag-path gate).
func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// colorEnabled is the explicit D-09 gate for the theme builder: color is on only
// when NO_COLOR is unset, TERM is not "dumb", and stdout is a TTY. termenv/lipgloss
// auto-detect too, but this makes the decision explicit and testable off-hardware.
func colorEnabled() bool {
	return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && stdoutIsTTY()
}

// villaTheme returns the shared huh theme (D-10). When colorEnabled is false it
// strips color globally (lipgloss.SetColorProfile(termenv.Ascii)) and returns the
// base theme unchanged — bold/faint/underline survive, the accent does not (D-09).
// When true it applies the accent to the focused title (bold+underline) and the
// select caret, and tints the faint help grey.
func villaTheme(colorEnabled bool) *huh.Theme {
	t := huh.ThemeBase()
	if !colorEnabled {
		// Belt-and-braces: globally drop color so any lipgloss render degrades too.
		// ThemeBase keeps bold/underline/faint attributes — the wizard stays legible.
		lipgloss.SetColorProfile(termenv.Ascii)
		return t
	}

	t.Focused.Title = t.Focused.Title.Foreground(accentColor).Bold(true).Underline(true)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(accentColor)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(accentColor).Bold(true).Underline(true)
	t.Help.ShortDesc = t.Help.ShortDesc.Foreground(mutedColor)
	t.Help.FullDesc = t.Help.FullDesc.Foreground(mutedColor)
	return t
}

// mutedStyle returns the help-tier (faint/muted) lipgloss style used for advisory
// help text — the same mutedColor the "Step N/M" label uses. It is help-tier, NOT
// status-tier: no bold, no accent. Under SetColorProfile(termenv.Ascii) the
// foreground is stripped and the text renders plain (the words still carry the
// meaning), so the advisory degrades gracefully on no-color terminals.
func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(mutedColor)
}

// statusStyle returns the named lipgloss style for a status tier — bold + the tier
// color. The preflight rows and the final result line reuse these so PASS/WARN/BLOCK
// render consistently. Bold is applied unconditionally so the status word stays
// scannable even when SetColorProfile(termenv.Ascii) has stripped the foreground.
func statusStyle(tier statusTier) lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true)
	switch tier {
	case statusPass:
		return s.Foreground(passColor)
	case statusWarn:
		return s.Foreground(warnColor)
	case statusBlock:
		return s.Foreground(blockColor)
	default:
		return s
	}
}

// statusGlyph returns the UI-SPEC glyph for a status tier. With ascii=true it
// returns the [OK]/[WARN]/[BLOCK] fallback for non-UTF-8 terminals; otherwise the
// Unicode ✓/!/✗. Glyph + bold status word carry meaning; color is additive only.
func statusGlyph(tier statusTier, ascii bool) string {
	switch tier {
	case statusPass:
		if ascii {
			return "[OK]"
		}
		return "✓"
	case statusWarn:
		if ascii {
			return "[WARN]"
		}
		return "!"
	case statusBlock:
		if ascii {
			return "[BLOCK]"
		}
		return "✗"
	default:
		return ""
	}
}

// stepHeader renders the "Step N/M" progress token for the persistent wizard header
// (UI-SPEC Layout). The "N/M" token is preserved verbatim regardless of color so the
// progress text survives NO_COLOR (D-09); when colorEnabled the chrome is faint with
// the current step accented.
func stepHeader(current, total int, colorEnabled bool) string {
	token := fmt.Sprintf("Step %d/%d", current, total)
	if !colorEnabled {
		return token
	}
	label := lipgloss.NewStyle().Foreground(mutedColor).Render("Step ")
	pos := lipgloss.NewStyle().Foreground(accentColor).Render(fmt.Sprintf("%d", current))
	rest := lipgloss.NewStyle().Foreground(mutedColor).Render(fmt.Sprintf("/%d", total))
	return label + pos + rest
}
