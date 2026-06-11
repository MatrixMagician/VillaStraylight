---
status: complete
phase: 22-control-plane-fit-host-gate
source: [22-VERIFICATION.md]
started: 2026-06-10T17:45:00Z
updated: 2026-06-10T19:35:00Z
---

## Current Test

[testing complete]

## Evidence (executed live, --auto run delegated by operator)

evidence: |
  Re-executed live 2026-06-10T19:28Z on the gfx1151 box at HEAD 70b83f8 (--auto run):
  - Healthy path: `villa recommend` shows "− embed reservation 0.500 GiB
    (536870912 bytes)" row, Fits: yes; `--json` has embedding_reservation_bytes,
    memory_considered: true, schema_version: 2. `villa doctor` overall PASS,
    exit 0; MEM-PRE-disk PASS (469.30 GiB free at the real volume root),
    MEM-PRE-headroom PASS (81.22 GiB ≥ 0.50 GiB); MEM-DOC-residency BLOCK PASS
    (log: ROCm0 model buffer 20583.34 MiB resident; sysfs GTT-used ≥ weight).
  - Negative control EXECUTED: stop villa-embed → container gone from podman ps;
    doctor → MEM-DOC-residency WARN (never PASS, honest "villa-embed.service is
    not active" + remediation), overall WARN, exit 2, unit NOT auto-started
    (still inactive after doctor). Restored: start villa-embed → doctor back to
    overall PASS exit 0, MEM-DOC-residency BLOCK PASS.
  - OBSERVATION (pre-existing, not a phase-22 regression): with embed down,
    doctor still showed health:villa-embed.service PASS "/health is ready (200)"
    — status.Collect probes the single llama endpoint for every container row
    (internal/status/status.go:376), so embed/qdrant health rows are llama's
    /health. Verdict-level honesty preserved (Active + MEM-DOC-residency catch
    it → exit 2). Proper per-service health belongs in Phase 23 (schema 2→3).
    Saved to graphmind memory 527de579.
  - D-05 live re-measure (2026-06-10T19:33Z, post-restart): villa-embed
    MemoryPeak=227123200 bytes (~216.6 MiB) ≤ 512 MiB reservation (~42%) —
    reservation adequate; higher than the earlier ~110.9 MiB sample (fresh
    restart conditions) but the invariant peak ≤ reservation holds.

## Tests

### 1. Operator sign-off on the on-hardware verification results
expected: Wave-4 checkpoint (22-04 Task 2) was auto-approved under the --auto chain (`human_verify_mode: end-of-phase`); the healthy-path evidence was re-confirmed live post-fix by the verifier and orchestrator. Remaining for the operator — formal sign-off, optionally re-running the embed-down negative control (mutates service state): stop villa-embed → doctor shows MEM-DOC-residency WARN never PASS, exit 2, doctor does not start the unit → restart villa-embed.
result: pass
note: Operator delegated execution to the agent on the live Strix Halo host ("Please do the tests as you are running on the Strix Halo host"). All evidence re-executed live at HEAD 70b83f8 — healthy path (recommend reservation row + schema-2 JSON, MEM-PRE PASS, doctor overall PASS exit 0, MEM-DOC-residency BLOCK PASS), embed-down negative control (WARN never PASS, exit 2, unit NOT auto-started, restored cleanly), and D-05 peak ≤ reservation. One pre-existing observation (misattributed per-service health rows, status.go:376) logged for Phase 23 — not a phase-22 regression, verdict-level honesty preserved.

## Summary

total: 1
passed: 1
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
