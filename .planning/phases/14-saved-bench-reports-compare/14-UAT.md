---
status: testing
phase: 14-saved-bench-reports-compare
source: [14-VERIFICATION.md]
started: 2026-06-07T00:00:00Z
updated: 2026-06-07T00:00:00Z
---

## Current Test

number: 1
name: On-hardware (gfx1151) cross-backend saved-report round-trip + compare
expected: |
  On a real AMD Strix Halo (gfx1151) host with rootless Podman + llama-server:
  run `villa bench` once on the vulkan backend and once on rocm (same
  model/quant/ctx/host), then `villa bench --compare`. Two saved JSONL records
  persist under `$XDG_DATA_HOME/villa/bench-reports.jsonl`, each with a
  non-fabricated `host_gfx_id` captured from `detect.Probe()`; `--compare`
  prints a real cross-backend Δpp/Δtg (exit 0) keeping pp and tg on separate
  lines; `villa bench --list` shows both runs. This proves the Phase-12 Δtg
  recovery with genuine timings.
awaiting: user response

## Tests

### 1. On-hardware cross-backend saved-report round-trip + compare
expected: |
  Run `villa bench` on vulkan, then on rocm (same model/quant/ctx/host), then
  `villa bench --compare` and `villa bench --list`. Two records persist under
  `$XDG_DATA_HOME/villa/bench-reports.jsonl` with non-empty `host_gfx_id`;
  `--compare` emits a real cross-backend Δpp/Δtg on separate lines, exit 0;
  `--list` enumerates both runs. (BENCH-03 + BENCH-04)
why_human: |
  Requires a live AMD Strix Halo GPU + rootless Podman llama-server to produce
  genuine pp/tg timings and a real (non-empty) host_gfx_id fingerprint; cannot
  be measured off-hardware. All deterministic logic (persistence, schema freeze,
  fail-closed Load, comparability guard, void flagging, exit mapping) is already
  verified via injected-Deps tests + binary spot-checks with synthetic records.
result: [pending]

## Summary

total: 1
passed: 0
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps
