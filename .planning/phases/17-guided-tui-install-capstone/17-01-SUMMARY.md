---
phase: 17-guided-tui-install-capstone
plan: 01
subsystem: ui
tags: [huh, lipgloss, termenv, bubbletea, tui, no-color, cgo-free, ci, go]

# Dependency graph
requires:
  - phase: 16-backup-restore
    provides: version-stamped Makefile build (LDFLAGS/BINARY) reused by build-static
provides:
  - "charmbracelet/huh v1.0.0 dependency (bubbletea v1.3.6 / lipgloss v1.1.0 transitively pinned, no v2 leak)"
  - "make build-static (CGO_ENABLED=0) target — SC#4 static-build gate"
  - ".github/workflows/ci.yml — CGO-free build + vet/test + go mod verify + bubbletea-v1 assertion"
  - "cmd/villa/tui_theme.go — shared command-tier huh/lipgloss theme, NO_COLOR-degradable (D-09/D-10)"
  - "villaTheme/colorEnabled/stdoutIsTTY/statusGlyph/statusStyle/stepHeader theme primitives for Plan 02"
affects: [17-02 install wizard, 17-03 install gate wiring]

# Tech tracking
tech-stack:
  added: [charmbracelet/huh v1.0.0, charmbracelet/lipgloss v1.1.0, muesli/termenv v0.16.0]
  patterns: [command-tier-only TUI styling (no internal/* import — D-11), NO_COLOR theme degradation via termenv.Ascii, stdout-TTY twin of stdinIsInteractive]

key-files:
  created: [cmd/villa/tui_theme.go, cmd/villa/tui_theme_test.go, .github/workflows/ci.yml]
  modified: [go.mod, go.sum, Makefile]

key-decisions:
  - "huh/lipgloss/termenv pinned DIRECT (imported by tui_theme.go); bubbletea v1.3.6 stays indirect — never go get bubbletea directly (Pitfall 4)"
  - "Deferred final go mod tidy to after Task 2 so huh was not pruned as unused"
  - "Bold applied unconditionally to status styles so the status word survives termenv.Ascii foreground stripping"

patterns-established:
  - "Pattern 1: command-tier TUI theme file is the single styling source; no internal/* core imports huh/lipgloss (D-11)"
  - "Pattern 2: NO_COLOR/TERM=dumb/non-TTY → colorEnabled() false → villaTheme strips Foreground via termenv.Ascii, retains bold/faint/underline + glyph column (D-09)"
  - "Pattern 3: CI bubbletea-v1 grep assertion guards against accidental v2 upgrade (D-11)"

requirements-completed: [INSTALL-02]

# Metrics
duration: 14min
completed: 2026-06-08
---

# Phase 17 Plan 01: Dependency + CGO-free Build + Shared TUI Theme Summary

**charmbracelet/huh v1.0.0 landed CGO-free (bubbletea v1.3.6 pinned, no v2 leak), with a `make build-static` + GitHub Actions gate and a NO_COLOR-degradable command-tier lipgloss/huh theme (`villaTheme`, status glyphs, Step N/M) — the styling + supply-chain foundation the Plan-02 wizard consumes.**

## Performance

- **Duration:** ~14 min
- **Started:** 2026-06-08
- **Completed:** 2026-06-08
- **Tasks:** 2 (Task 2 was TDD: RED → GREEN, no refactor needed)
- **Files modified:** 6 (3 created, 3 modified)

## Accomplishments
- Added the milestone's single new direct dependency `charmbracelet/huh v1.0.0`; verified `bubbletea v1.3.6` pinned transitively (NOT `charm.land/bubbletea/v2`) and `go mod verify` clean.
- `make build-static` (`CGO_ENABLED=0 go build`) and `.github/workflows/ci.yml` gate the SC#4 static build, mirror `make check`, run `go mod verify`, and assert `bubbletea v1` (D-11 no-v2-leak).
- Shipped `cmd/villa/tui_theme.go`: `villaTheme(colorEnabled)`, `colorEnabled()`, `stdoutIsTTY()`, `statusStyle`/`statusGlyph` (Unicode + `[OK]`/`[WARN]`/`[BLOCK]` ASCII fallback), and a `stepHeader` Step N/M renderer — all command-tier, no `internal/*` import (D-11), no backend/image literal (seam gate green).
- TDD test scaffold (`tui_theme_test.go`) proves color-on applies the accent and NO_COLOR/TERM=dumb strips it while bold/underline + glyph fallback survive (D-09).

## Task Commits

1. **Task 1: Add charmbracelet/huh v1.0.0 + CGO-free build/CI plumbing** - `da9ef42` (chore)
2. **Task 2 (TDD RED): failing theme tests** - `024d7b4` (test)
3. **Task 2 (TDD GREEN): shared TUI theme with NO_COLOR degradation** - `87e48ea` (feat)

_Task 2 needed no REFACTOR commit — the GREEN implementation was already clean._

## Files Created/Modified
- `go.mod` / `go.sum` - Added huh v1.0.0 (+ lipgloss v1.1.0, termenv v0.16.0 direct); bubbletea v1.3.6 indirect.
- `Makefile` - New `.PHONY: build-static` target (`CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" …`).
- `.github/workflows/ci.yml` - New CI: CGO-free `go build ./...`, `go vet`+`go test`, `go mod verify`, bubbletea-v1 grep assertion.
- `cmd/villa/tui_theme.go` - Shared command-tier huh/lipgloss theme + glyph/Step-N/M helpers; NO_COLOR-degradable (D-09/D-10).
- `cmd/villa/tui_theme_test.go` - Color-on vs NO_COLOR theme variant tests + glyph ASCII fallback + Step N/M token assertions.

## Decisions Made
- Pinned `lipgloss`/`termenv` direct (they are imported by `tui_theme.go`); let `bubbletea v1.3.6` stay indirect. Never `go get bubbletea` directly (would risk v2 — RESEARCH Pitfall 4).
- Ran final `go mod tidy` only AFTER Task 2 added the `huh` import, so `huh` was not pruned as an unused dependency between tasks (per plan Task 1 action note).
- Applied `Bold(true)` unconditionally in `statusStyle` so the bold status word remains scannable after `lipgloss.SetColorProfile(termenv.Ascii)` strips foreground colors (UI-SPEC: meaning must survive NO_COLOR).

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None. `huh` initially appeared as `// indirect` in `go.mod` after Task 1 (nothing imported it yet) — expected and resolved by the post-Task-2 `go mod tidy`, exactly as the plan sequenced it.

## Verification Results
- `CGO_ENABLED=0 go build ./cmd/villa` → exit 0 (SC#4).
- `go mod verify` → "all modules verified".
- `go.mod` → `github.com/charmbracelet/huh v1.0.0` and `bubbletea v1.3.6` (no v2).
- `go test ./cmd/villa/ -run 'TestVillaTheme|TestStatusGlyph|TestStepHeader'` → ok (D-09).
- `go test ./internal/inference/ -run TestSeamGrepGate` → ok (no leaked backend/image literal).
- Full `go vet ./...` + `go test ./...` → all packages ok (no regression).

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Theme primitives (`villaTheme`, `statusGlyph`, `stepHeader`, status styles) and the `stdoutIsTTY`/`colorEnabled` gate are ready for Plan 02's wizard (`install_wizard.go`) and the Plan-03 `runInstall` TTY-gate.
- CGO-free build + bubbletea-v1 CI gate is in place to catch any v2 regression introduced by the wizard.

## Self-Check: PASSED

All created files exist on disk and all three task commits (`da9ef42`, `024d7b4`, `87e48ea`) are present in git history.

---
*Phase: 17-guided-tui-install-capstone*
*Completed: 2026-06-08*
