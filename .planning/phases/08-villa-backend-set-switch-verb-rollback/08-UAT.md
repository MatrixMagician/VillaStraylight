---
status: testing
phase: 08-villa-backend-set-switch-verb-rollback
source: [08-VERIFICATION.md]
started: 2026-06-06T00:00:00Z
updated: 2026-06-06T00:00:00Z
---

## Current Test

number: 4
name: Live `--dry-run` preview and `backend show` against a real configured install
expected: |
  `villa backend set rocm --dry-run` prints {target, fit, preflight} and mutates
  nothing; `villa backend show` reports the real active backend + resolved image tag.
awaiting: in progress (Test 3 = real 5m never-ready timeout, run last)

## Tests

### 1. Real `villa backend set rocm` cutover on a running install
expected: Switch succeeds (exit 0); `villa backend show` reports rocm; only villa-llama.service restarted; model/quant/context unchanged. Real ROCm offload proves healthy (offloaded N/N, gpu_busy>0 during decode).
why_human: Requires real ROCm/HIP offload on gfx1151 with HSA_OVERRIDE — the live generation-probe + residency proof against a real llama-server cannot run off-host.
result: pass
fixed_by: "f3eaedb fix(08): drop illegal --group-add render from ROCm backend (CR-G1)"
first_run: "FAIL (blocker) — `./villa backend set rocm` EXIT=1, podman exit 125 `the '--group-add keep-groups' option is not allowed with any other --group-add options`; auto-rolled-back to vulkan (rollback verified healthy). Root cause: backend_rocm.go emitted keep-groups + render together (illegal); 3 golden tests had locked it in. Fix: keep-groups only."
retest: "PASS on-hardware 2026-06-06 (gfx1151, ROCm 7.2.4): `switched backend vulkan -> rocm — ... cutover proven`, EXIT=0. backend show=rocm + pinned rocm-7.2.4 digest. config model/quant/ctx preserved (only backend flipped). ONLY villa-llama restarted (openwebui ts unchanged). iGPU busy peaked 95% during the decode (37 non-zero samples). villa status OFFLOAD PASS, overall PASS, loopback-only, no telemetry."
note: "The first run's transactional rollback worked perfectly (verbatim unit + config restore, re-ready on vulkan) — that is the behavior Test 2 targets, giving early confidence in the rollback path."

### 2. Forced-bad ROCm bring-up auto-rolls back verbatim
expected: liveProve classifies a silent-CPU-fallback / load_tensors-hang as FAIL (gpu_busy 0% / not-ready-before-timeout / no tokens) within proveTimeout (5m); the switch auto-rolls back to the verbatim prior vulkan unit+config and re-readies villa-llama; exit 1 with a "rolled back; prior backend restored" message; the running stack is unchanged (a failed switch is a no-op).
why_human: Silent-CPU-fallback detection, the load_tensors-hang deadline, the allocation-cap / firmware-fault paths, and the live transactional restore all depend on real ROCm runtime behavior unavailable off-host.
result: pass
method: "Throwaway fault-injection build: appended `-ngl 0` to the ROCm ContainerArgs (last -ngl wins → CPU-only) to simulate a silent CPU fallback, then `villa backend set rocm`. Reverted via git checkout + rebuild immediately after; working tree clean."
retest: "PASS on-hardware 2026-06-06: liveProve returned FAIL at the prove step with honest multi-signal detail — `offload FAILED — log: only a CPU model buffer was loaded — server fell back to CPU; sysfs: GTT-used 2879131648 bytes < 22134528992 weight footprint — weights not resident; busy: gpu_busy_percent 10% during decode`. EXIT=1, message `backend set: switch to rocm failed at \"prove\" — rolled back; prior backend (vulkan) restored`. Verbatim vulkan unit+config restored, villa-llama re-readied on Vulkan0 (journal confirms), openwebui untouched, final `villa status overall PASS`. Prove completed in ~13s (well within the 5m bound — no hang)."
note: "Observation (minor, not a gap): `set` returns exit 1 immediately after issuing the rollback restart; the restored backend's llama-server is still loading the model for ~4s after return, so a `villa status` run in that window shows a TRANSIENT `overall FAIL` before settling to PASS. Inherent to Type=notify (podman notifies on container-start, not app-readiness). The stack does return to healthy on its own."

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
passed: 2
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps

- truth: "`villa backend set rocm` brings up a working ROCm llama-server and proves residency before cutover"
  status: resolved
  resolved_by: "f3eaedb (CR-G1) — keep-groups only; retested PASS on-hardware"
  reason: "ROCm quadlet emitted `--group-add keep-groups` + `--group-add render` together; podman rejects the combination (exit 125) → restart fails → auto-rollback to vulkan. The forward ROCm path never started."
  severity: blocker
  test: 1
  id: CR-G1
  artifacts:
    - internal/inference/backend_rocm.go:69-70   # remove the `--group-add render` arg (keep-groups already carries render)
    - internal/inference/backend_rocm_test.go:30 # drop the `--group-add render` assertion
    - internal/orchestrate/render_test.go:152    # wantGroups: drop GroupAdd=render
    - internal/orchestrate/parseargs_test.go:39  # wantGroup: re-scope (generic parser test — keep generic two-group case or update)
  missing:
    - "An on-hardware (or rendered-unit integration) check that the ROCm unit actually STARTS under podman, not just that the arg string matches a golden — the golden tests locked in the illegal combination."

- truth: "A cold cutover (ROCm image not yet cached) does not spuriously roll back because the 7GB pull exceeds the 45s TimeoutStartSec"
  status: investigated_cleared
  id: CR-G2
  severity: none
  reason: "Tested explicitly: removed the local ROCm image, ran `villa backend set rocm`. The ~62s pull exceeded the 45s TimeoutStartSec but the switch SUCCEEDED (exit 0, cutover proven). Journal shows podman quadlet detects it runs under systemd and `setting pull timeout to 5m0s`, extending the start watchdog past TimeoutStartSec. So the in-start image pull is correctly handled — NOT a bug, no fix needed."
  note: "Cleared on-hardware at this network speed; the mechanism (podman 5m pull-timeout + watchdog extension) is speed-independent, so the 45s start timeout is not a hard cap during pull."
