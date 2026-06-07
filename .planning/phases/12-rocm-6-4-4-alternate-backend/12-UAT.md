---
status: complete
phase: 12-rocm-6-4-4-alternate-backend
source:
  - 12-01-SUMMARY.md
  - 12-02-SUMMARY.md
  - 12-03-SUMMARY.md
started: 2026-06-07T11:26:46Z
updated: 2026-06-07T11:27:30Z
note: >
  On-hardware UAT (Task 4) was performed by the operator on the live gfx1151 Strix
  Halo host on 2026-06-07 (model qwen3.6-35b-a3b) and is recorded verbatim in
  12-03-SUMMARY.md → "On-Hardware UAT Results". Results below transcribe that
  recorded run; the user is confirming the recorded verdict, not re-running pulls.
---

## Current Test

[testing complete]

## Tests

### 1. Switch to rocm-6.4.4 (transactional + residency proof, SC#1)
expected: `villa backend set rocm-6.4.4` switches transactionally with residency-proof PASS; `villa status` shows backend rocm-6.4.4, OFFLOAD PASS, image @sha256:c81f30a7…
result: pass
recorded: "On-hardware: switched + cutover proven (residency PASS); status = rocm-6.4.4, OFFLOAD PASS, @sha256:c81f30a7…. Proven twice (initial + post-rollback restore)."

### 2. rocWMMA residency FAIL → honest verbatim rollback (SC#1, offload-asserting)
expected: `villa backend set rocm-6.4.4-rocwmma` either comes up with residency PASS, or — if it cannot prove offload — FAILS honestly and rolls back verbatim to the prior backend (never a health-200 false-green).
result: pass
recorded: "On-hardware: -rocwmma residency FAILED ('not ready before timeout — possible load_tensors hang or CPU-fallback stall') and rolled back verbatim to rocm-6.4.4, which recovered to OFFLOAD PASS. Offload-asserting FAIL path + transactional rollback both worked as designed — a real honest FAIL, never a false-green. (Follow-up logged: investigate whether -rocwmma is a timeout-tuning issue or genuine gfx1151 incompat.)"

### 3. Fail-closed validation of an unknown --ab-target (SC#2 / D-03)
expected: `villa bench --ab --ab-target <bogus>` is rejected with an actionable error naming valid backends, BEFORE any switch — zero side effects.
result: pass
recorded: "On-hardware: --ab-target rocm-7.2.4 fail-closed rejected with named remediation (valid identifier is `rocm` = 7.2.4); zero side effects. D-03 validated."

### 4. Digest-pin survives rolling-tag drift (SC#2)
expected: A digest-pinned backend resolves and pulls the exact reproducible image even if the upstream rolling tag has been re-pushed; preflight digest-pin policy gate passes on --dry-run before any mutation.
result: pass
recorded: "On-hardware: --dry-run → fit PASS + preflight PASS before mutation. skopeo showed the live rocm-6.4.4 tag re-pushed to sha256:44f115e0… (≠ pinned c81f30a7…) same day; the pinned digest still resolved & pulled (content-addressed) — switch used the exact reproducible image. Pinning worked exactly as intended (-rocwmma tag 9a97129a… still matched)."

### 5. bench --ab arbitrary-pair, pp/tg reported separately (SC#3)
expected: `villa bench --ab --ab-target <backend>` benchmarks an arbitrary backend pair, prints prompt-processing (pp) and token-generation (tg) tok/s SEPARATELY (never blended), and residency-checks every run with auto-restore afterward.
result: pass
recorded: "On-hardware (warmup=1 reps=5 n_predict=128 seed=42 temp=0; kept=5 void=0; auto-restore after each A/B). rocm-6.4.4→rocm(7.2.4): Δpp +3.82, Δtg +1.08. rocm-6.4.4→vulkan: Δpp −1.72, Δtg +11.68. pp/tg kept separate throughout."

### 6. Δtg recovery hypothesis — honest A/B verdict (SC#3 / D-04)
expected: The A/B identifies whether rocm-6.4.4 (or -rocwmma) recovers the v1.1 Δtg −11.15 token-generation regression, and records the keep choice. Vulkan stays the default — never auto-switched.
result: pass
recorded: "VERDICT (bench-decided D-04): rocm-6.4.4 does NOT recover the regression. Vulkan still leads tg by ~11.68 tok/s over rocm-6.4.4 (≈ v1.1's rocm-7.2.4 Δtg −11.15). ROCm wins pp slightly; Vulkan remains the tg winner and correct default. The honest A/B did its job — prove, don't assume. Capability shipped correctly and safely; performance hypothesis disproven on this host/model."

### 7. Restore default backend (SC#1)
expected: `villa backend set vulkan` (and `villa backend set rocm`) switches cleanly back with cutover proven.
result: pass
recorded: "On-hardware: `villa backend set rocm` (restore) switched + cutover proven (4th proven cutover in the session); Vulkan re-established as default."

## Summary

total: 7
passed: 7
issues: 0
pending: 0
skipped: 0

## Gaps

[none]
