---
phase: quick-260608-pyp
plan: 01
type: execute
subsystem: cmd/villa (command-tier TUI presentation)
tags: [phase-17, ui-spec, install-wizard, huh, presentation]
requirements: [INSTALL-01]
key-files:
  modified:
    - cmd/villa/tui_theme.go
    - cmd/villa/tui_theme_test.go
    - cmd/villa/install_wizard.go
    - cmd/villa/install_wizard_test.go
    - cmd/villa/install.go
    - cmd/villa/install_test.go
commits:
  - f2f8da6
  - 1cce280
  - 77abb73
metrics:
  tasks: 3
  files: 6
  completed: 2026-06-08
---

# Phase 17 Quick 260608-pyp: Fix remaining Phase-17 UI-SPEC warnings — Summary

Closed the four remaining 17-UI-REVIEW.md warnings as command-tier presentation only:
a first-party `villaKeyMap()` footer (Pillar 2), the 2-cell `block` detail-line indent
(Pillar 5), the step-2 "use this model" advance CTA (Pillar 1/6), and the verbatim
BLOCK-gap-declined error copy (Pillar 1). Zero `internal/*` edits, zero new dependency,
zero backend/image literal — all three changes are byte-level conformance to the approved
17-UI-SPEC.md contract.

## Tasks

### Task 1 — First-party footer key map + step-2 CTA (commit f2f8da6)
- Added `villaKeyMap() *huh.KeyMap` to `tui_theme.go`: starts from `huh.NewDefaultKeyMap()`
  and rewrites only the help LABELS via `bubbles/key.WithKeys(...)+WithHelp(...)` (keys stay
  huh defaults). Contracted footer vocabulary: `↑ up` / `↓ down`, Tab `next` / Shift+Tab
  `back`, Confirm `y`/`n`, Quit (ctrl+c) `cancel`.
- The Select `Next` AND `Submit` help desc render the contracted CTA `use this model`
  (Enter-to-advance IS the confirm). Lowercase per the project's lowercase-command-aware
  faint-footer voice (the titlecase "Use this model" is the row-label register).
- Wired `buildWizardForm` to `.WithKeyMap(villaKeyMap())` (footer help left enabled).
- Used the already-transitive `github.com/charmbracelet/bubbles/key` — no `go.mod` change.

### Task 2 — 2-cell `block` indent for review + preflight detail lines (commit 1cce280)
- Added `const blockIndent = "  "` to `tui_theme.go` (the UI-SPEC `block` token = 2-cell
  left indent).
- `preflightSummary` prefixes each per-check row with `blockIndent`; `reviewBlock` prefixes
  each model/backend/will-pull/write/start line. Headings (Note titles) stay flush — only the
  detail lines indent, which IS the `block` hierarchy. All inner content (glyph/word/name,
  `backend.Image()` accessor) byte-identical apart from the leading 2 spaces.

### Task 3 — Contracted BLOCK-gap-declined copy (commit 77abb73)
- `resolveGap` wizard-decline branch now emits the verbatim contracted line BEFORE returning
  false; the terse "declined — run the command above…" hint is kept as the actionable next-step.
- Added `blockRemediation(c)`: returns `c.Remediation` when present, else the
  `remediationCommand` fallback (remediation-forward rule).
- Exit/mutation contract UNCHANGED: declined-without-`--force` stays `exitBlocked` with zero
  host mutation (seam never runs, nothing written/started/pulled/persisted); `--force` still
  degrades to `exitWarn`; `d.consent` is never re-prompted on the threaded path.

## Exact contracted strings emitted (verbatim from 17-UI-SPEC.md)
- Step-2 advance CTA (footer help): `use this model`
- BLOCK-gap-declined copy:
  `BLOCK: <check name>. <remediation>. Run the suggested command, or re-run with --no-tui --force to override (auditable).`
  (asserted with the seloffCheck name + remediation substituted; the contract template's
  `<remediation>.` after a remediation that itself ends in `.` yields a verbatim `..` — a
  faithful substitution, not a typo).
- Footer key vocabulary: `↑`/`↓`, `shift+tab`→`back`, Confirm `y`/`n`, ctrl+c→`cancel`.
- `block` spacing token: 2-cell left indent on review + preflight detail rows.

## Decision on warning #3
**IMPLEMENTED (not waived).** The footer-help CTA route was taken (the Select advance
help renders `use this model`); no 17-UI-SPEC.md amendment was made.

## Tests added (TDD, no live TTY)
- `TestVillaKeyMap`, `TestStepTwoCTA` (tui_theme_test.go) — footer label + CTA assertions.
- `TestPreflightSummaryBlockIndent`, `TestReviewBlockIndent` (install_wizard_test.go) —
  `HasPrefix("  ")` on every non-empty detail line; content/accessor preserved.
- `TestWizardBlockDeclinedCopy` (install_test.go) — verbatim contracted line + 0/2/1 exit /
  zero-mutation contract; `--force` degrade-to-WARN subtest.

## Verification gate (all green)
- `go fmt ./cmd/villa/` — clean on my 6 files (no diff).
- `CGO_ENABLED=0 go build ./cmd/villa` — exit 0 (static-binary constraint).
- `go test ./internal/inference/ -run TestSeamGrepGate` — PASS (no backend/image literal leaked).
- `make check` (vet + full suite) — green.

## Goldens
**None refrozen** (as expected). No golden/`--json`/byte-frozen fixture references the
footer/indent/copy; `make check` surfaced no golden diff, so no `-update` was run.

## Deviations from Plan
- **[Rule 3 - Blocking issue, out-of-scope guard] Reverted a stray gofmt reformat of an
  unrelated file.** Running `go fmt ./cmd/villa/` reformatted a pre-existing comment-alignment
  nit in `cmd/villa/bench_compare.go` (a list-bullet `//   - exit 1).`), which is NOT part of
  this task. Per the explicit constraint to stage ONLY my code changes by path, I reverted that
  file (`git checkout -- cmd/villa/bench_compare.go`) so my three task commits touch exactly the
  six intended files. The change was never staged into any commit; build/test stay green after
  revert. Logged here rather than fixed (out-of-scope of this quick task).

## Self-Check: PASSED
- cmd/villa/tui_theme.go — FOUND (villaKeyMap, blockIndent)
- cmd/villa/install_wizard.go — FOUND (WithKeyMap, blockIndent-prefixed rows)
- cmd/villa/install.go — FOUND (contracted BLOCK-gap-declined line, blockRemediation)
- Commit f2f8da6 — FOUND
- Commit 1cce280 — FOUND
- Commit 77abb73 — FOUND
