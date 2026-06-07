---
status: diagnosed
phase: 13-villa-doctor-health-diagnosis
source: [13-VERIFICATION.md]
started: 2026-06-07T00:00:00Z
updated: 2026-06-07T16:12:00Z
---

## Current Test

[testing complete]

## Tests

### 1. On a live, healthy gfx1151 install (stack up), run `villa doctor`
expected: Exit 0; all-healthy findings report; offload PASS over a real residency proof.
result: issue
reported: "On this host (backend=rocm, qwen3.6-35b-a3b live), `villa doctor` returned exit 2 (overall WARN), not exit 0. Substantive diagnosis was fully correct: offload PASS over a real residency proof (log ROCm0 20583.34 MiB resident + sysfs GTT-used 26.4 GB >= 22.1 GB weight footprint), all services /health 200, drift PASS. The exit-2 came solely from three typed-Unknown ROCm preflight WARNs — ROCM-PRE-firmware (firmware version not probed), ROCM-PRE-hsa (could not verify HSA_OVERRIDE_GFX_VERSION), ROCM-PRE-image (no ROCm image requested; standalone gate). A fully-working ROCm install can therefore never reach exit 0."
severity: major

### 2. Induce a CPU-fallback backend on real hardware, run `villa doctor`
expected: Exit 1 (exitBlocked); a BLOCK-class residency-FAIL finding with actionable remediation; never a false-green over a health-200.
result: pass

### 3. Hand-touch a rendered Quadlet unit on disk, run `villa doctor`
expected: Exit 2 (exitWarn); a config-vs-disk drift WARN finding with reconcile remediation.
result: pass

## Summary

total: 3
passed: 2
issues: 1
pending: 0
skipped: 0
blocked: 0

## Gaps

- truth: "On a live, healthy install `villa doctor` exits 0 with an all-healthy report (DOCTOR-01 'exit 0 = healthy' contract)"
  status: failed
  reason: "User reported: a fully-healthy ROCm install (offload proven, all /health 200, no drift) returns exit 2 (WARN), not exit 0, because doctor re-runs the pre-install ROCm preflight gate (preflight.RunROCm), whose host-prep checks (firmware floor, HSA_OVERRIDE_GFX_VERSION, image pin) are un-evaluable in a running-stack context and degrade to typed-Unknown WARN. Result: exit 0 is unreachable on the recommended opt-in ROCm backend even when residency is proven."
  severity: major
  test: 1
  root_cause: ""     # Filled by diagnosis
  artifacts: []      # Filled by diagnosis
  missing: []        # Filled by diagnosis
  debug_session: ""  # Filled by diagnosis

## On-Hardware Test Evidence

Executed live on the gfx1151 host (kernel 7.0.11-200.fc44, backend=rocm, model qwen3.6-35b-a3b UD-Q4_K_M, ctx 131072), 2026-06-07. Stack was up and in use (villa-llama/openwebui/dashboard active). Tests 2 and 3 mutated the live stack reversibly and were restored verbatim (offload re-proven post-restore; drift returned to PASS).

**Test 1 (healthy, read-only):** `overall WARN`, exit 2. offload:villa-llama PASS (ROCm0 20583.34 MiB resident; sysfs GTT 26.4 GB >= 22.1 GB). health llama/openwebui/dashboard PASS (200). drift PASS. Three ROCm preflight typed-Unknown WARNs (firmware/hsa/image) drove overall to WARN. → Issue (see Gaps).

**Test 2 (induced CPU fallback):** changed `-ngl 999`→`-ngl 0` on villa-llama.container, restarted (health 200 on CPU), ran doctor: `overall FAIL`, exit 1. health:villa-llama PASS (200) but offload:villa-llama BLOCK FAIL DOMINATES — "offload FAILED — log: only a CPU model buffer was loaded — server fell back to CPU; sysfs: GTT-used 1.9 GB < 22.1 GB weight footprint — weights not resident on GPU — GPU offload did not engage — check /dev/dri passthrough, keep-groups, and that the RADV ICD is present (not llvmpipe)". No false-green over the health-200. Backend restored to ROCm; offload re-proven. → PASS (exact match).

**Test 3 (hand-touched unit):** appended a comment line to villa-llama.container, ran doctor: drift finding flipped to WARN — "on-disk Quadlet units no longer match the rendered-from-config units — re-run `villa install` to reconcile config-vs-disk drift", exit 2. Restored original; drift returned to PASS. → PASS (exact match).
