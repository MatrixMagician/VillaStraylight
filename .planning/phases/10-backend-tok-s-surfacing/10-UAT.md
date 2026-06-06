---
status: testing
phase: 10-backend-tok-s-surfacing
source: [10-VERIFICATION.md]
started: 2026-06-06T21:05:00Z
updated: 2026-06-06T21:05:00Z
---

## Current Test

number: 1
name: villa status + dashboard reflect ROCm with live backend-labeled tok/s on a real gfx1151 ROCm install
expected: |
  On a real gfx1151 ROCm install, during active generation, `villa status` (and the dashboard)
  show backend=rocm with the ROCm image tag, a live non-zero `gen tok/s (rocm)`, and an
  offload/residency PASS keyed on the live ROCm0 markers; the ROCm-readiness indicator shows
  ready/not-ready from the live probe (not unknown).
awaiting: user response

## Tests

### 1. status/dashboard reflect ROCm + live backend-labeled tok/s (on-hardware)
expected: |
  On a real gfx1151 ROCm install, run `villa status` and open the dashboard during active
  generation. status/dashboard show backend=rocm with the ROCm image tag, a live non-zero
  `gen tok/s (rocm)`, and an offload/residency PASS keyed on the live ROCm0 markers; the
  ROCm-readiness indicator shows ready/not-ready from the live probe (not `unknown`).
why_human: |
  Requires real Strix Halo (gfx1151) hardware running a ROCm-configured stack under load —
  off-hardware the readiness folds to the honest `unknown` and the tok/s seam returns nil
  (idle). Cannot be exercised in CI. Consistent with the on-hardware UAT deferrals in Phases
  8/9 and recorded in all three Plan SUMMARYs.
result: [pending]

### 2. dashboard Performance + Health panels under live generation (on-hardware)
expected: |
  On a real gfx1151 install during active generation, open the control dashboard. The
  Performance panel reads e.g. `12.3 tok/s (vulkan)` (or `(rocm)`); the Health panel shows the
  active backend, image tag, and a non-`unknown` ROCm-readiness badge from the live readiness
  probe.
why_human: |
  Live tok/s + live readiness badge require a running model generating tokens on real hardware;
  the browser DOM rendering of live `/api/status`+`/api/metrics` data cannot be asserted by
  grep/unit tests.
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
