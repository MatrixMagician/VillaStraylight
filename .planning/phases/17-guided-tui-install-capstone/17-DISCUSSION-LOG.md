# Phase 17: Guided TUI Install (Capstone) - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-08
**Phase:** 17-Guided TUI Install (Capstone)
**Areas discussed:** Invocation, Confirm/adjust depth, Preflight gap UX, Visual scope, Final apply, Fallback trigger, Auto-run scope

---

## Invocation

| Option | Description | Selected |
|--------|-------------|----------|
| TUI default on `villa install` | Bare `villa install` on a TTY launches the wizard; `--no-tui`/non-TTY/`--json` falls back to the flag path | ✓ |
| Separate `villa setup` command | New dedicated subcommand; `villa install` unchanged | |
| `villa install --tui` opt-in flag | Flag path default; `--tui` opts in | |

**User's choice:** TUI default on `villa install`
**Notes:** Matches the ROADMAP `--no-tui` wording — one command, progressively enhanced.

---

## Confirm/adjust depth

| Option | Description | Selected |
|--------|-------------|----------|
| Pick from memory-fitting alternatives | Show recommended pick + other catalog picks that fit the envelope, re-validated by `recommend.Pick` | ✓ |
| Accept-or-cancel only | Recommendation read-only; confirm or abort to flags | |
| Full override fields | Editable model/quant/ctx/backend, each re-validated | |

**User's choice:** Pick from memory-fitting alternatives
**Notes:** Backend stays Vulkan unless explicitly changed; wizard computes nothing itself.

---

## Preflight gap UX

| Option | Description | Selected |
|--------|-------------|----------|
| Gap screen + explicit per-fix consent | Per-item y-toggle before any fix; BLOCK needs --force | |
| Display-only, exit to shell | Show commands, never run privileged steps | |
| Auto-run safe fixes, prompt privileged | Non-privileged fixes run inline w/ notice; privileged still consent-gated | ✓ |

**User's choice:** Auto-run safe fixes, prompt privileged
**Notes:** Privileged commands never run silently — explicit consent preserved.

---

## Visual scope

| Option | Description | Selected |
|--------|-------------|----------|
| Themed: villa lipgloss theme + step progress | Accent color, BLOCK/WARN/PASS coloring, "Step 2/5" indicator | ✓ |
| Minimal: default huh styling | Stock huh forms, no theme | |
| Themed but monochrome | Structure/progress, no color | |

**User's choice:** Themed: villa lipgloss theme + step progress
**Notes:** Degrade theme (not the whole wizard) under NO_COLOR/TERM=dumb.

---

## Final apply

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — review screen + explicit confirm | Summary of choices + one "Install" confirm before any host mutation | ✓ |
| No — proceed after gap step | Install proceeds once gaps resolved | |

**User's choice:** Yes — review screen + explicit confirm
**Notes:** Matches villa's "never silently mutate" posture.

---

## Fallback trigger

| Option | Description | Selected |
|--------|-------------|----------|
| Non-TTY + `--json` + NO_COLOR-safe | Fall back on non-TTY/`--json`/`--no-tui`; honor NO_COLOR/TERM=dumb by degrading theme only | ✓ |
| Non-TTY + `--json` only | Fall back on non-TTY/`--json`/`--no-tui`; color handling deferred | |

**User's choice:** Non-TTY + `--json` + NO_COLOR-safe
**Notes:** CI-safe and accessible; theme degrades, guided flow stays.

---

## Auto-run scope

| Option | Description | Selected |
|--------|-------------|----------|
| TUI-mode only; flag path unchanged | Auto-run only inside wizard; flag path keeps y/N | |
| Both paths auto-run safe fixes | Unify behavior across TUI and flag path | ✓ |

**User's choice:** Both paths auto-run safe fixes
**Notes:** ⚠️ Changes existing flag-path behavior — planner must update `install_test.go` /
install goldens (append-only re-freeze) and confirm the 0/2/1 exit contract +
non-interactive/privileged-consent rules remain intact.

---

## Claude's Discretion

- Exact screen decomposition/order within `detect → recommend → confirm → gaps → review`.
- Whether `--dry-run` is exposed as a wizard preview screen or only via the flag/fallback path.
- Internal test-seam shape for the wizard (follow existing `installDeps` / `interactive`/`consent` pattern).

## Deferred Ideas

- Non-install TUI surfaces (dashboard TUI, model-management TUI) — own phase.
- Rich in-wizard `--dry-run` preview — possible follow-up.
