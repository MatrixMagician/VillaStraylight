package main

import (
	"os"
)

// tui_theme.go is the command-tier presentation gate for the guided `villa install`
// prompt loop: whether to use colour/Unicode at all, and the status glyph column.
//
// It was a full lipgloss/huh/termenv theme — adaptive colour tokens, a huh theme
// builder, a footer keymap and per-tier styles — serving one interactive flow. The
// flow is now a stdin prompt loop, so what survives is the part that carried
// meaning rather than decoration.
//
// The honesty property is unchanged and is why the glyph column exists: PASS / WARN
// / BLOCK is conveyed by a glyph AND a status word, never by colour alone. Colour
// was always additive, which is what made NO_COLOR a supported mode rather than a
// degradation — and is why dropping it costs nothing a reader depended on.

// blockIndent is the 2-cell left indent for detail rows under a heading, so the
// detail column sits two cells under its (flush) heading. Every review and
// preflight-gap row is prefixed with it.
const blockIndent = "  "

// statusTier is the three-way preflight semantic: pass, advisory, blocking.
type statusTier int

const (
	statusPass statusTier = iota
	statusWarn
	statusBlock
)

// stdoutIsTTY reports whether os.Stdout is a char device (a real terminal). It is
// the stdout twin of stdinIsInteractive (install_hostprep.go): the guided install
// reads stdin and writes stdout, so BOTH must be a TTY for it to make sense. A piped
// or redirected stdout means the flag path, not the prompt loop.
func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// colorEnabled is the explicit gate: colour (and the Unicode glyph column) is
// on only when NO_COLOR is unset, TERM is not "dumb", and stdout is a TTY.
//
// The guided install threads this into the glyph choice, so an operator who sets
// NO_COLOR, or who pipes the output, gets the [OK]/[WARN]/[BLOCK] fallback that
// survives a non-UTF-8 terminal.
func colorEnabled() bool {
	return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && stdoutIsTTY()
}

// statusGlyph returns the glyph for a status tier. With ascii=true it returns the
// [OK]/[WARN]/[BLOCK] fallback for non-UTF-8 terminals; otherwise the Unicode
// ✓/!/✗. The glyph and the status word beside it carry the meaning between them, so
// neither the colour nor the Unicode form is load-bearing.
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
