---
status: testing
phase: 13-villa-doctor-health-diagnosis
source: [13-VERIFICATION.md]
started: 2026-06-07T00:00:00Z
updated: 2026-06-07T00:00:00Z
---

## Current Test

number: 1
name: On a live, healthy gfx1151 install (stack up), run `villa doctor`
expected: |
  Exit 0; an all-healthy findings report; offload PASS over a real residency proof.
awaiting: user response

## Tests

### 1. On a live, healthy gfx1151 install (stack up), run `villa doctor`
expected: Exit 0; all-healthy findings report; offload PASS over a real residency proof.
result: [pending]

### 2. Induce a CPU-fallback backend on real hardware, run `villa doctor`
expected: Exit 1 (exitBlocked); a BLOCK-class residency-FAIL finding with actionable remediation; never a false-green over a health-200.
result: [pending]

### 3. Hand-touch a rendered Quadlet unit on disk, run `villa doctor`
expected: Exit 2 (exitWarn); a config-vs-disk drift WARN finding with reconcile remediation.
result: [pending]

## Summary

total: 3
passed: 0
issues: 0
pending: 3
skipped: 0
blocked: 0

## Gaps
