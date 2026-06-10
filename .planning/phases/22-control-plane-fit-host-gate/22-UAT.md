---
status: testing
phase: 22-control-plane-fit-host-gate
source: [22-VERIFICATION.md]
started: 2026-06-10T17:45:00Z
updated: 2026-06-10T17:45:00Z
---

## Current Test

number: 1
name: Operator sign-off on the on-hardware verification results
expected: |
  Operator reviews the 22-04 on-hardware evidence (recommend reservation row +
  schema-2 JSON, MEM-PRE gates PASS at the real volume root, post-fix doctor
  overall PASS / exit 0 with MEM-DOC-residency BLOCK PASS, D-05 measurement
  ~110.9 MiB peak vs 512 MiB reservation) and the optional embed-down negative
  control: `systemctl --user stop villa-embed` → `./villa doctor` shows
  MEM-DOC-residency WARN (never PASS, exit 2, unit NOT auto-started) →
  `systemctl --user start villa-embed`. Operator responds "approved" or lists issues.
awaiting: user response

## Tests

### 1. Operator sign-off on the on-hardware verification results
expected: Wave-4 checkpoint (22-04 Task 2) was auto-approved under the --auto chain (`human_verify_mode: end-of-phase`); the healthy-path evidence was re-confirmed live post-fix by the verifier and orchestrator. Remaining for the operator — formal sign-off, optionally re-running the embed-down negative control (mutates service state): stop villa-embed → doctor shows MEM-DOC-residency WARN never PASS, exit 2, doctor does not start the unit → restart villa-embed.
result: [pending]

## Summary

total: 1
passed: 0
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps
