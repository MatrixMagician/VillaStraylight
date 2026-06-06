---
status: testing
phase: 08-villa-backend-set-switch-verb-rollback
source: [08-VERIFICATION.md]
started: 2026-06-06T00:00:00Z
updated: 2026-06-06T00:00:00Z
---

## Current Test

number: 1
name: Real `villa backend set rocm` cutover on a running install
expected: |
  Switch succeeds (exit 0); `villa backend show` reports rocm; only
  villa-llama.service was restarted; model/quant/context unchanged in config.toml.
  The real ROCm bring-up proves healthy (offloaded N/N layers, gpu_busy>0 during
  the decode) before cutover.
awaiting: user response

## Tests

### 1. Real `villa backend set rocm` cutover on a running install
expected: Switch succeeds (exit 0); `villa backend show` reports rocm; only villa-llama.service restarted; model/quant/context unchanged. Real ROCm offload proves healthy (offloaded N/N, gpu_busy>0 during decode).
why_human: Requires real ROCm/HIP offload on gfx1151 with HSA_OVERRIDE — the live generation-probe + residency proof against a real llama-server cannot run off-host.
result: [pending]

### 2. Forced-bad ROCm bring-up auto-rolls back verbatim
expected: liveProve classifies a silent-CPU-fallback / load_tensors-hang as FAIL (gpu_busy 0% / not-ready-before-timeout / no tokens) within proveTimeout (5m); the switch auto-rolls back to the verbatim prior vulkan unit+config and re-readies villa-llama; exit 1 with a "rolled back; prior backend restored" message; the running stack is unchanged (a failed switch is a no-op).
why_human: Silent-CPU-fallback detection, the load_tensors-hang deadline, the allocation-cap / firmware-fault paths, and the live transactional restore all depend on real ROCm runtime behavior unavailable off-host.
result: [pending]

### 3. Bounded proveTimeout (5m) fires on a never-ready ROCm server
expected: The cutover prove returns FAIL at the deadline (not an infinite wait) and rolls back to the prior backend.
why_human: Requires a real hung llama-server load on the target hardware; the deadline context is wired but its trip can only be observed live.
result: [pending]

### 4. Live `--dry-run` preview and `backend show` against a real configured install
expected: `villa backend set rocm --dry-run` prints {target, fit verdict, preflight verdict} and mutates nothing (config.toml + units byte-unchanged, service untouched); `villa backend show` reports the real active backend + resolved image tag.
why_human: Requires a real configured install with rendered units on the target host to confirm zero-mutation preview and an accurate active-backend report.
result: [pending]

## Summary

total: 4
passed: 0
issues: 0
pending: 4
skipped: 0
blocked: 0

## Gaps
