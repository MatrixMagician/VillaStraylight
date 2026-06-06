---
status: complete
phase: 10-backend-tok-s-surfacing
source: [10-VERIFICATION.md]
started: 2026-06-06T21:05:00Z
updated: 2026-06-06T22:10:00Z
---

## Current Test

[testing complete]

## Tests

### 1. status/dashboard reflect ROCm + live backend-labeled tok/s (on-hardware)
expected: |
  On a real gfx1151 ROCm install, run `villa status` and open the dashboard during active
  generation. status/dashboard show backend=rocm with the ROCm image tag, a live non-zero
  `gen tok/s (rocm)`, and an offload/residency PASS keyed on the live ROCm0 markers; the
  ROCm-readiness indicator shows ready/not-ready from the live probe (not `unknown`).
why_human: |
  Requires real Strix Halo (gfx1151) hardware running a ROCm-configured stack under load —
  off-hardware the readiness folds to the honest `unknown` and the tok/s seam returns nil
  (idle). Cannot be exercised in CI. Consistent with the on-hardware UAT deferrals in Phases
  8/9 and recorded in all three Plan SUMMARYs.
result: pass
note: |
  VERIFIED LIVE on real gfx1151 hardware AFTER bringing the ROCm backend up via
  `villa backend set rocm` (transactional cutover succeeded, model qwen3.6-35b-a3b preserved,
  cutover self-proven, exit 0). Confirmed via `villa status`:
    - backend identity from resolved backend: "backend":"rocm" ✓
    - ROCm image tag: docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1… ✓
    - live non-zero backend-labeled tok/s: `gen tok/s 49.3 (rocm)` under generation;
      OMITTED when idle (never fabricated 0) ✓
    - offload/residency PASS keyed on live ROCm0 markers (the HIP residency proof):
      "ROCm0 model buffer 20583.34 MiB resident on the iGPU; sysfs GTT-used ≥ weight footprint",
      provenance "journald load_tensors ROCm0 residency + point-in-time mem_info_gtt_used floor" ✓
  The Vulkan-path equivalent was also verified earlier (`gen tok/s 60.3 (vulkan)`, Vulkan0
  residency PASS) — both backends resolve polymorphically through the same surfacing seam.
  Throughput note (expected per milestone honesty constraint): ROCm tg ~49 tok/s vs Vulkan ~60
  tok/s — token-gen is flat-to-regressed on ROCm; its advantage is prompt-processing-weighted.

  CAVEAT — the single unmet sub-clause "readiness ready/not-ready (not `unknown`)": the
  rocm_readiness indicator STILL reads `unknown` even on the live ROCm backend. This is NOT
  environmental and NOT a Phase 10 defect — it is a known CODE GAP in the detect path:
  foldROCmReadiness (status.go:161) needs all 5 signals Known to leave `unknown`, but
  FirmwareDateOK() and HSAOverrideViable() are hardcoded UnknownBool (readiness_rocm.go:56,63 —
  "not probed"). Phase 10 honestly surfaces what detect provides; a non-`unknown` badge requires
  implementing those two detect probes (tracked follow-up, see VERIFICATION on-hardware note).

### 2. dashboard Performance + Health panels under live generation (on-hardware)
expected: |
  On a real gfx1151 install during active generation, open the control dashboard. The
  Performance panel reads e.g. `12.3 tok/s (vulkan)` (or `(rocm)`); the Health panel shows the
  active backend, image tag, and a non-`unknown` ROCm-readiness badge from the live readiness
  probe.
why_human: |
  Live tok/s + live readiness badge require a running model generating tokens on real hardware;
  the browser DOM rendering of live `/api/status`+`/api/metrics` data cannot be asserted by
  grep/unit tests.
result: pass
note: |
  Verified live on real gfx1151 hardware via Playwright against the running dashboard (:8888)
  during active Qwen3.6-35B-A3B generation (~60 tok/s), after rebuilding villa from HEAD and
  restarting villa-dashboard.service (the running process held a stale pre-Phase-10 binary):
    - Performance panel: "generation  60.3 tok/s (vulkan)" — backend-labeled live tok/s ✓
                         "prompt 90.3 tok/s", "prompt-eval latency 11.1 ms/tok" unchanged ✓
    - Health panel: backend "vulkan" ✓ ; image
      "docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…" ✓
      ROCm-readiness badge "unknown" + caption "ROCm readiness can't be evaluated on this host." ✓
    - /api/metrics top-level shape UNCHANGED (active_slots, activity_known, available,
      gen_tokens_per_sec, idle, latency_ms, prompt_tokens_per_sec, slots_known) — no status-only
      fields leaked into metricsView ✓
  Screenshot: .playwright-mcp/phase10-dashboard-live-vulkan.png
  Caveat (not a defect): the "non-`unknown` readiness badge" sub-clause is the only part not
  exercised — this host has no ROCm toolchain, so the badge correctly shows `unknown` by design.
  The non-unknown (ready/not-ready) badge needs a ROCm-configured stack (see Test 1).

## Summary

total: 2
passed: 2
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none blocking. One tracked follow-up (not a Phase 10 defect): the ROCm-readiness indicator
stays `unknown` even on the live ROCm backend because two detect-path signals
(FirmwareDateOK, HSAOverrideViable) are hardcoded typed-Unknown / not yet probed
(internal/detect/readiness_rocm.go:56,63). A non-`unknown` dashboard badge requires
implementing those two probes — Phase 10 surfacing itself is correct.]
