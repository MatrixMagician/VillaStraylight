---
status: complete
phase: 13-villa-doctor-health-diagnosis
source: [13-VERIFICATION.md]
started: 2026-06-07T00:00:00Z
updated: 2026-06-07T16:55:00Z
---

## Current Test

[testing complete]

## Tests

### 1. On a live, healthy gfx1151 install (stack up), run `villa doctor`
expected: Exit 0; all-healthy findings report; offload PASS over a real residency proof.
result: pass
note: "Initially reported as an issue (exit 2 not 0 on a healthy ROCm install — typed-Unknown firmware/HSA/image preflight WARNs forced overall WARN). Closed by the 13-03 residency-supersession gap-closure (commits c1b9f6c/1e4e765/02265f3, review fix 00cb7e8). Re-validated LIVE on the gfx1151 host after the fix: `villa doctor` now returns overall PASS / EXIT 0, with offload proven (ROCm0 20583.34 MiB resident + sysfs GTT >= weight footprint), all /health 200, drift PASS. ROCM-PRE-image flipped to PASS (Option B now evaluates the running image via the seam); firmware/HSA stay VISIBLE as WARN but no longer raise the rank under proven residency. No-false-green preserved (TestConfidentROCmFAILStillDominatesResidency)."

### 2. Induce a CPU-fallback backend on real hardware, run `villa doctor`
expected: Exit 1 (exitBlocked); a BLOCK-class residency-FAIL finding with actionable remediation; never a false-green over a health-200.
result: pass

### 3. Hand-touch a rendered Quadlet unit on disk, run `villa doctor`
expected: Exit 2 (exitWarn); a config-vs-disk drift WARN finding with reconcile remediation.
result: pass

## Summary

total: 3
passed: 3
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

- truth: "On a live, healthy install `villa doctor` exits 0 with an all-healthy report (DOCTOR-01 'exit 0 = healthy' contract)"
  status: resolved
  reason: "User reported: a fully-healthy ROCm install (offload proven, all /health 200, no drift) returns exit 2 (WARN), not exit 0, because doctor re-runs the pre-install ROCm preflight gate (preflight.RunROCm), whose host-prep checks (firmware floor, HSA_OVERRIDE_GFX_VERSION, image pin) are un-evaluable in a running-stack context and degrade to typed-Unknown WARN. Result: exit 0 is unreachable on the recommended opt-in ROCm backend even when residency is proven."
  severity: major
  test: 1
  root_cause: "doctor.Aggregate re-runs the STANDALONE pre-install ROCm host-prep gate (preflight.RunROCm) against a POST-install running stack. RunROCm/RunROCmWithPolicy hardcode firmware and HSA as typed-Unknown (internal/preflight/checks_rocm.go:66-67) and pass an empty requested image (RunROCm, checks_rocm.go:39), so ROCM-PRE-firmware/-hsa/-image degrade to WARN by construction — independent of the real healthy host. The worst-wins fold (internal/doctor/doctor.go:202-215 + statusRank 114-123) rolls any single WARN up to Overall=WARN, and renderDoctor (cmd/villa/doctor.go:105-106) correctly maps WARN->exit 2. There is NO residency-supersession: a proven ROCm offload PASS (doctor.go:282-284, real log marker + GTT delta) does not down-rank the un-evaluable host-prep WARNs, so a residency-proven ROCm install can never reach exit 0. (Vulkan installs skip these checks — the 'exit 0 healthy' expectation was implicitly Vulkan-centric.)"
  artifacts:
    - path: "internal/doctor/doctor.go"
      issue: "Aggregate calls standalone RunROCm (lines 132-141) and runs the worst-wins fold (202-215) with no residency-supersession of typed-Unknown ROCm host-prep findings"
    - path: "internal/preflight/checks_rocm.go"
      issue: "RunROCm passes no image (line 39); RunROCmWithPolicy hardcodes firmware/HSA as typed-Unknown (66-67) — correct for pre-install, structurally WARN post-install. A thread-through variant RunROCmForImage already exists (48-50)"
    - path: "cmd/villa/doctor.go"
      issue: "liveDoctorDeps (155-211) never threads the running image/firmware/HSA into the gate; renderDoctor (80-110) maps correctly and needs no change"
  missing:
    - "Residency-supersession rule in pure doctor.Aggregate: when a ROCm-family service's offload Verdict is inference.StatusPass (residency proven), the typed-Unknown ROCm host-prep findings (idROCmFirmware/idROCmHSA/idROCmImage AND Status==WARN) must NOT raise the worst-wins rank — kept visible but non-rank-raising — ONLY under proven residency"
    - "Preserve no-false-green: a Known deny-listed firmware (checks_rocm.go:140-146), Known-wrong HSA (189-194), or denied image (219-225) is a confident FAIL and must still BLOCK -> exit 1; only un-evaluable host-prep superseded by proven residency is downgraded"
    - "Optional Option B: thread BackendFor(cfg.Backend).Image() via the existing RunROCmForImage so a denied RUNNING image is actively caught rather than merely un-evaluated"
    - "Add a supersession test case to internal/doctor/doctor_test.go + cmd/villa/doctor_test.go and refreeze the doctor golden"
  debug_session: .planning/debug/doctor-healthy-rocm-exits-2.md

## On-Hardware Test Evidence

Executed live on the gfx1151 host (kernel 7.0.11-200.fc44, backend=rocm, model qwen3.6-35b-a3b UD-Q4_K_M, ctx 131072), 2026-06-07. Stack was up and in use (villa-llama/openwebui/dashboard active). Tests 2 and 3 mutated the live stack reversibly and were restored verbatim (offload re-proven post-restore; drift returned to PASS).

**Test 1 (healthy, read-only):** `overall WARN`, exit 2. offload:villa-llama PASS (ROCm0 20583.34 MiB resident; sysfs GTT 26.4 GB >= 22.1 GB). health llama/openwebui/dashboard PASS (200). drift PASS. Three ROCm preflight typed-Unknown WARNs (firmware/hsa/image) drove overall to WARN. → Issue (see Gaps).

**Test 2 (induced CPU fallback):** changed `-ngl 999`→`-ngl 0` on villa-llama.container, restarted (health 200 on CPU), ran doctor: `overall FAIL`, exit 1. health:villa-llama PASS (200) but offload:villa-llama BLOCK FAIL DOMINATES — "offload FAILED — log: only a CPU model buffer was loaded — server fell back to CPU; sysfs: GTT-used 1.9 GB < 22.1 GB weight footprint — weights not resident on GPU — GPU offload did not engage — check /dev/dri passthrough, keep-groups, and that the RADV ICD is present (not llvmpipe)". No false-green over the health-200. Backend restored to ROCm; offload re-proven. → PASS (exact match).

**Test 3 (hand-touched unit):** appended a comment line to villa-llama.container, ran doctor: drift finding flipped to WARN — "on-disk Quadlet units no longer match the rendered-from-config units — re-run `villa install` to reconcile config-vs-disk drift", exit 2. Restored original; drift returned to PASS. → PASS (exact match).
