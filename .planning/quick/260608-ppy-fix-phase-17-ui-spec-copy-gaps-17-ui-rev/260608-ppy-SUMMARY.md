---
phase: quick-260608-ppy
plan: 01
subsystem: cmd/villa (install command-tier presentation)
tags: [phase-17, ui-spec, install, wizard, typed-unknown, copywriting]
requires:
  - cmd/villa/install.go (no-fit branch)
  - cmd/villa/install_wizard.go (detectedHostSummary)
  - internal/detect typed-Unknown accessors (.Known)
provides:
  - Contracted empty-state no-fit copy (17-UI-SPEC.md:195)
  - Contracted typed-Unknown advisory note (17-UI-SPEC.md:196)
affects:
  - cmd/villa/install.go
  - cmd/villa/install_wizard.go
  - cmd/villa/tui_theme.go
  - cmd/villa/install_wizard_test.go
tech-stack:
  added: []
  patterns: [pure-presentation-copy, typed-unknown-degradation, command-tier-only]
key-files:
  created: []
  modified:
    - cmd/villa/install.go
    - cmd/villa/install_wizard.go
    - cmd/villa/tui_theme.go
    - cmd/villa/install_wizard_test.go
decisions:
  - "Empty-state GiB figure rendered via new gibUsableEnvelope (GiB-number + ' GiB'), distinct from gib() which renders 'X.XXX GiB (N bytes)' — the contract wants a bare '<N> GiB usable' figure."
  - "Typed-Unknown advisory uses mutedColor (help-tier, faint), NOT accent/status color — advisory augments, never replaces, the bare per-field 'unknown' tokens."
  - "Advisory predicate reuses strKnown (mirrors strOrUnknown's Known&&non-empty condition) so predicate and renderer can never disagree about 'unknown'."
metrics:
  duration: ~12 min
  completed: 2026-06-08
---

# Quick Task 260608-ppy: Fix Phase-17 UI-SPEC Install Copy Gaps Summary

Closed the two phase-17 UI-SPEC copy gaps from 17-UI-REVIEW.md — the missing empty-state "no fitting model" string (BLOCKER) and the missing typed-Unknown advisory note (WARNING) — as command-tier presentation copy in `cmd/villa/` only, byte-exact to 17-UI-SPEC.md:195/196, with table-driven tests asserting both contracted strings verbatim.

## What changed

### Task 1 — Contracted empty-state no-fit copy (TDD)
`cmd/villa/install.go`: the no-fit branch (recommend refused: empty Model / zero ctx / zero weight) now emits the EXACT 17-UI-SPEC.md:195 string:

> `No catalog model fits the detected memory envelope (<N> GiB usable). Free memory or supply a larger-envelope host, then re-run villa install. (--no-tui shows the same result.)`

- `<N> GiB` is substituted from `profile.UsableEnvelopeBytes` (the authoritative `detect.Bytes` typed-Unknown source — `rec.UsableEnvelopeBytes` may be zero on a refusal) via a new `gibUsableEnvelope` helper. A Known envelope renders the numeric figure (`8 GiB`); a typed-Unknown envelope renders `unknown GiB` (never a fabricated 0).
- The branch fires BEFORE the wizard is evaluated, so a single emission point covers both the flag and wizard paths — consistent with the `(--no-tui shows the same result.)` parity note.
- `exitBlocked` preserved.

### Task 2 — Typed-Unknown advisory in detectedHostSummary (TDD)
`cmd/villa/install_wizard.go`: `detectedHostSummary` now appends, as a trailing faint line, the EXACT 17-UI-SPEC.md:196 advisory:

> `Some host facts could not be probed; villa will pick conservatively. Run villa detect for detail.`

- Appended ONLY when at least one rendered fact (`CPUModel`, `UsableEnvelopeBytes`, `IGPUName`, `KernelVersion`) is not Known; all-Known omits it entirely (output unchanged from today's all-Known rendering).
- New `strKnown` helper mirrors `strOrUnknown`'s unknown-condition (`Known && Value != ""`) so the advisory predicate and the per-field renderer never disagree.
- Rendered via a new `mutedStyle()` help-tier (faint, `mutedColor`) renderer in `tui_theme.go` — advisory-tier, not status-tier (no accent/bold). The advisory augments, never replaces, the bare per-field `unknown` tokens.

### Task 3 — Refreeze check + full gates
- Searched `cmd/villa/` and `internal/` for the OLD generic install no-fit strings (`no fitting configuration for this host`, `memory envelope undeterminable`, `recommend refused`) and for screen-1 summary assertions lacking the advisory. The only remaining hit is `cmd/villa/inference.go:154` — a SEPARATE `villa inference` validate-command detail string, NOT the install no-fit branch and explicitly out of scope.
- No test or golden/`--json` fixture asserted the old install no-fit copy, so **no refreeze was required**. The new contracted strings are stderr human text (plain-string assertions), not frozen goldens; no `-update` was used.

## Deviations from Plan

None — plan executed exactly as written. The implementation chose a dedicated `gibUsableEnvelope` helper (rather than reusing `gib()`) because the contract wants a bare `<N> GiB` figure, not `gib()`'s `X.XXX GiB (N bytes)` fit-table format; the plan's action block explicitly permitted "emit just the GiB number followed by ' GiB usable'".

## Scope held
Fixed ONLY the two gaps. The other four 17-UI-REVIEW warnings (footer keymap, block indentation, step-2 "Use this model" CTA, BLOCK-declined copy) were left untouched. No decision logic added to any `internal/*` core; no backend/image literal added to `cmd/villa` (the wizard stays a pure collector).

## Gate results (all green)
- `go fmt ./cmd/villa/` — clean (no reformat)
- `CGO_ENABLED=0 go build ./cmd/villa` — exit 0
- `go test ./internal/inference/ -run TestSeamGrepGate` — PASS
- `make check` (vet + full suite) — green (`cmd/villa` 2.692s, all internal packages ok)
- New tests: `TestInstallNoFitEmitsContractedEmptyState` (Known + typed-Unknown), `TestDetectedHostSummaryTypedUnknownAdvisory` (all-known omits / typed-Unknown appends + trailing-line + token-preserved / each-fact-triggers) — all pass.

## Commits
- `583b1ee` fix(quick-260608-ppy): close two phase-17 UI-SPEC install copy gaps

## Self-Check: PASSED
- Files modified exist: cmd/villa/install.go, install_wizard.go, tui_theme.go, install_wizard_test.go (all in commit 583b1ee).
- Commit 583b1ee exists on branch gsd/phase-17-guided-tui-install-capstone; no file deletions; no unrelated files staged.
- Both contracted strings present verbatim in emitted output under their contracted conditions (asserted by passing tests).
