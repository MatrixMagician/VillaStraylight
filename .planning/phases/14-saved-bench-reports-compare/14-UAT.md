---
status: complete
phase: 14-saved-bench-reports-compare
source: [14-VERIFICATION.md]
started: 2026-06-07T00:00:00Z
updated: 2026-06-07T17:21:00Z
---

## Current Test

[testing complete]

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
result: pass
verified_on: 2026-06-07T17:21:00Z (on-hardware, gfx1151, qwen3.6-35b-a3b UD-Q4_K_M ctx=131072)
evidence: |
  Clean round-trip from an absent store. `villa bench` on rocm (pp 123.10±1.08,
  tg 50.26±0.09, kept=5 void=0, exit 0) then `villa backend set vulkan` (cutover
  proven) then `villa bench` on vulkan (pp 116.52±1.63, tg 60.65±0.10, kept=5
  void=0, exit 0). Store $XDG_DATA_HOME/villa/bench-reports.jsonl ended with 2
  JSONL lines, mode 0600, schema_version=1, each fingerprint.host_gfx_id="gfx1151"
  (real, from detect.Probe — never fabricated), kernel "7.0.11-200.fc44.x86_64",
  pp/tg as SEPARATE fields, void_exhausted=false, single.backend rocm/vulkan.
  `villa bench --compare` printed A(rocm)/B(vulkan) with pp and tg on separate
  lines and a real cross-backend delta Δpp -6.58, Δtg +10.39 tok/s, exit 0
  (same model/quant/ctx/host => comparable). `villa bench --list` enumerated
  both runs (#0 rocm, #1 vulkan) with separate pp/tg columns, exit 0. The
  measured Δtg +10.39 (vulkan over rocm) independently reproduces the Phase-12
  finding (~+11.68) with genuine timings. Original backend restored to rocm.

## Summary

total: 1
passed: 1
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none]
