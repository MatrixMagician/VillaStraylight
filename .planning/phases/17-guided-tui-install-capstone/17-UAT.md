---
status: testing
phase: 17-guided-tui-install-capstone
source: [17-VERIFICATION.md]
started: 2026-06-08
updated: 2026-06-08
---

## Current Test

number: 1
name: Real-TTY guided wizard walk-through on a gfx1151 Fedora host
expected: |
  `villa install` in a real terminal walks all 5 screens (detect → model select →
  preflight gaps + per-item privileged consent → review → Install/Cancel confirm),
  the Step N/M progress and BLOCK=red/WARN=amber/PASS=green coloring render, keyboard
  nav works, Install confirm defaults focus to Cancel, and the resulting config.toml +
  install match the flag path.
awaiting: user response

## Tests

### 1. Real-TTY guided wizard walk-through on a gfx1151 Fedora host
expected: `villa install` in a real terminal walks all 5 screens (detect → model select → preflight gaps + per-item privileged consent → review → Install/Cancel confirm); Step N/M progress and BLOCK=red/WARN=amber/PASS=green coloring render; keyboard nav works; Install confirm defaults focus to Cancel; resulting config.toml + install match the flag path. (D-09/D-10, INSTALL-01/02)
result: [pending]

### 2. NO_COLOR=1 and TERM=dumb degraded-theme render on hardware
expected: Re-running `villa install` with `NO_COLOR=1`, and again with `TERM=dumb`, still presents the full guided flow, unstyled — Foreground stripped, bold/faint/underline + the [OK]/[WARN]/[BLOCK] ASCII glyph column retained; the flow completes. (D-09)
result: [pending]

### 3. `--no-tui` and piped-stdin fallback parity on hardware
expected: `villa install --no-tui` and `villa install </dev/null` both run the flag-driven path and produce a config.toml byte-identical to the wizard path for the same recommendation. (INSTALL-02; byte-identity proven off-hardware by TestWizardConfigMatchesFlagPath, live install side-effects confirmed here.)
result: [pending]

## Summary

total: 3
passed: 0
issues: 0
pending: 3
skipped: 0
blocked: 0

## Gaps
