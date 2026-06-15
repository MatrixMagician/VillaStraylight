---
status: passed
phase: 28-agent-surfacing-contracts
source: [28-VERIFICATION.md]
started: 2026-06-15T00:00:00Z
updated: 2026-06-15T00:00:00Z
---

## Current Test

number: —
name: (all tests complete)
expected: |
  All on-hardware UAT items passed 2026-06-15 on the gfx1151 box (backend=rocm, memory_enabled=true,
  agent_enabled=true, coding_mode=true, coder=qwen3-coder-next-q4).
awaiting: none — UAT complete

## Tests

### 1. Agent residency-under-load is sampled in-flight, CPU-fallback dominates (WR-03)
expected: agent-residency finding reports the under-load residency proof from a verifiably in-flight tool-call drive; a real coder CPU-fallback under load folds doctor to FAIL/exitBlocked; an un-sustainable drive degrades to typed-Unknown WARN — never an idle-sampled PASS.
result: PASS (2026-06-15, gfx1151/ROCm 7.2.4). `villa doctor` → `agent-tool-call BLOCK PASS` (real read→edit round-trip over the local endpoint) and `agent-residency BLOCK PASS` — "offload proven (log + sysfs): ROCm0 model buffer 20583.34 MiB resident on the iGPU; GTT-used 26923200512 ≥ 22134528992 weight footprint (resident)". Coder GPU-resident under the in-flight tool-call drive, not idle-sampled. The CPU-fallback→BLOCK-FAIL dominance is the same proven offload-assert core (RunningOffloadVerdict) that drives MEM-DOC-residency; overall doctor PASS, exit 0.

### 2. Residency reflects the post-reservation envelope + dashboard Agent panel renders correctly (WR-01)
expected: |
  On a memory-ENABLED, agent-ON host: `villa status --json` `coding.residency` reflects
  `recommend.Pick` against the REAL memory inputs (post-embedding-reservation envelope) — e.g. "swap"
  when the post-reservation reality is swap, never an optimistic "shared" from the un-reserved
  envelope. After `systemctl --user restart villa-dashboard.service`, the dashboard Agent panel
  appears (mirroring the Memory panel), is hidden when the agent is off, and shows
  version/pin/model/mode/residency rows, per-model coder usage, and the cache-effectiveness row
  (pct+raw, or a gray Unknown badge) — no fabricated 0/0%.
result: PASS (2026-06-15, gfx1151, memory-on). `villa status --json` → coding block schema_version 4: {enabled:true, version:v0.76.0, pin_match:"match", model:qwen3-coder-next-q4, mode:coding, residency:"swap"}. Cross-check: `villa recommend` coder fit residency = "swap" computed against the REAL memory-on (post-embedding-reservation) envelope — MATCHES status (WR-01 fixed: no optimistic "shared" from an un-reserved envelope). Dashboard (after `systemctl --user restart villa-dashboard.service`, driven via Playwright): #agent-panel un-hides only with data present, renders version/pin(MATCH)/model/mode/residency(swap) mirroring the Memory panel, "No usage recorded yet" (honest empty, no fabricated 0), cache effectiveness "UNAVAILABLE — timings not yet observed" (typed-Unknown, no fabricated 0%). Screenshot: phase28-dashboard-agent-panel.png. Only console error = benign favicon 404.

## Summary

total: 2
passed: 2
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
