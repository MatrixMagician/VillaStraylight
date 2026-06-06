---
status: partial
phase: 10-backend-tok-s-surfacing
source: [10-VERIFICATION.md]
started: 2026-06-06T21:05:00Z
updated: 2026-06-06T21:50:00Z
---

## Current Test

[testing paused — 1 item outstanding: ROCm-backend on-hardware path]

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
result: blocked
blocked_by: third-party
reason: |
  Executed on real gfx1151 hardware (this host = AMD Ryzen AI Max 300 / RADV STRIX_HALO),
  but no ROCm toolchain is installed (`rocminfo` absent) and the live stack runs the VULKAN
  backend (villa-llama = kyuz0 vulkan-radv image). The ROCm-SPECIFIC assertions
  (backend=rocm, ROCm image tag, live `gen tok/s (rocm)`, ROCm0 residency markers, readiness
  ready/not-ready) therefore cannot be satisfied here — they need a ROCm-configured backend.

  The backend-AGNOSTIC surfacing mechanism (the actual Phase 10 deliverable) WAS verified
  live on this hardware against the Vulkan backend, which exercises the identical code paths:
    - backend identity sourced from the resolved backend (not a literal): `villa status --json`
      → "backend":"vulkan", "image":"docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…"
    - live backend-labeled tok/s: `gen tok/s 60.3 (vulkan)` during generation; OMITTED when idle
      (gen_tokens_per_sec absent — never fabricated 0)
    - residency PASS keyed on the RESOLVED backend's markers: villa-llama offload PASS on live
      Vulkan0 model-buffer residency + sysfs GTT floor
    - readiness tri-state renders honestly: rocm_readiness="unknown" (unevaluable on this host)
  Swapping the backend to a ROCm-7.2.4 stack is the only remaining step to turn the ROCm-labeled
  values green; the wiring is proven. NOT a defect — environmental (no ROCm install).

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
passed: 1
issues: 0
pending: 0
skipped: 0
blocked: 1

## Gaps

[none — no defects found; the one blocked item is environmental (no ROCm install), not a code gap]
