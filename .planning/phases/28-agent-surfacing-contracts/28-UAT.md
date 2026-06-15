---
status: testing
phase: 28-agent-surfacing-contracts
source: [28-VERIFICATION.md]
started: 2026-06-15T00:00:00Z
updated: 2026-06-15T00:00:00Z
---

## Current Test

number: 1
name: Agent residency-under-load samples while a tool-call round-trip is verifiably in flight (WR-03)
expected: |
  On the gfx1151 host with the coding agent enabled (`villa install --coding-agent` + coding mode)
  and the stack up (`villa up`), `villa doctor` reports the agent-residency-under-load proof from a
  genuinely IN-FLIGHT crush-run tool-call drive — not an idle sample. A forced coder CPU-fallback
  under load surfaces as a BLOCK FAIL (dominates the HTTP-200 health → overall exitBlocked), never a
  false-green PASS. An idle/fast-exiting probe degrades to typed-Unknown WARN (agentUnevaluable),
  never an idle-sampled PASS.
awaiting: user response

## Tests

### 1. Agent residency-under-load is sampled in-flight, CPU-fallback dominates (WR-03)
expected: agent-residency finding reports the under-load residency proof from a verifiably in-flight tool-call drive; a real coder CPU-fallback under load folds doctor to FAIL/exitBlocked; an un-sustainable drive degrades to typed-Unknown WARN — never an idle-sampled PASS.
result: [pending]

### 2. Residency reflects the post-reservation envelope + dashboard Agent panel renders correctly (WR-01)
expected: |
  On a memory-ENABLED, agent-ON host: `villa status --json` `coding.residency` reflects
  `recommend.Pick` against the REAL memory inputs (post-embedding-reservation envelope) — e.g. "swap"
  when the post-reservation reality is swap, never an optimistic "shared" from the un-reserved
  envelope. After `systemctl --user restart villa-dashboard.service`, the dashboard Agent panel
  appears (mirroring the Memory panel), is hidden when the agent is off, and shows
  version/pin/model/mode/residency rows, per-model coder usage, and the cache-effectiveness row
  (pct+raw, or a gray Unknown badge) — no fabricated 0/0%.
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
