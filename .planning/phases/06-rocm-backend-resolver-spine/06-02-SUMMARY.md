---
phase: 06-rocm-backend-resolver-spine
plan: 02
subsystem: inference
tags: [go, rocm, backend-interface, resolver, seam, residency-proof, gpu-busy, fail-closed]

# Dependency graph
requires:
  - phase: 06-rocm-backend-resolver-spine
    plan: 01
    provides: "ResidencyMarkers descriptor + ResidencyProof() on the Backend interface; scrapeOffloadLog(stderr, m) / scrapeLoadTensorsResidency(journal, m) parameterized scrapers; RunningOffloadInput.Markers + .GPUBusyPercent; gpuBusyFloor fold; backend_vulkan.go exemplar"
provides:
  - "backendROCm — the second Backend (digest-pinned rocm-7.2.4 image, /dev/kfd+/dev/dri devices, render group, ordered HSA_OVERRIDE_GFX_VERSION→ROCBLAS_USE_HIPBLASLT env, shared mandatory llama-server flags, ROCm0 ResidencyProof markers)"
  - "BackendFor(name) (Backend, error) — the single polymorphism point: \"\"/vulkan → Vulkan, rocm → ROCm, unknown → actionable error (fail-closed, never a silent fallback)"
  - "TestROCmMarkerPresence positive grep-gate (ROCm0/HSA/kfd must stay in backend_rocm.go); the negative seam gate extended to explicitly bind rocm-7.2.4|rocm7-nightlies"
  - "5 ROCm testdata fixtures + start-time/running scrape table cases driven by backendROCm{}.ResidencyProof(), incl. the gpu_busy_percent corroborate-PASS + neutral-Unknown cases via detect.GPUBusyPercentForTest"
affects: [06-03 (8-site re-route to BackendFor), phase-07 (ROCm quadlet render + render/video group byte-golden), phase-08 (live backend switch + decode-time gpu_busy read), phase-09 (ROCm bench)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Backend sibling-file discipline: backend_rocm.go mirrors backend_vulkan.go (stateless struct + compile-time `var _ Backend` assertion + ContainerArgs/ResidencyProof) — a new backend is a new file, not a caller change"
    - "Fail-closed resolver: BackendFor returns (nil, error) on an unknown config string, never a silent default — untrusted hand-edited config must not silently select a privileged device path"
    - "Positive + negative grep-gate pair: the negative gate keeps imperative backend literals OUT of callers; TestROCmMarkerPresence keeps the ROCm privilege/residency literals IN the seam"

key-files:
  created:
    - "internal/inference/backend_rocm.go — backendROCm struct, rocmImage @sha256 const, kfd+dri/render/ordered-env ContainerArgs delta, ROCm0 ResidencyProof"
    - "internal/inference/backend.go — BackendFor(name) (Backend, error) resolver"
    - "internal/inference/backend_rocm_test.go — TestROCmContainerArgs, TestROCmImageDigestPinned, TestBackendFor"
    - "internal/inference/testdata/load_tensors_rocm.txt — ROCm0 N/N PASS (with ROCm_Host, Pitfall 2 lock)"
    - "internal/inference/testdata/load_tensors_rocm_cpu.txt — CPU-only FAIL fixture"
    - "internal/inference/testdata/load_tensors_rocm_fault.txt — GPU-fault FAIL fixture"
    - "internal/inference/testdata/rocm_devinfo_pass.stderr — start-time device_info + offloaded 65/65 PASS"
    - "internal/inference/testdata/rocm_offloaded_partial.stderr — partial 1/65 FAIL (Pitfall 3)"
  modified:
    - "internal/inference/offload_test.go — TestROCmOffloadLogScrape (start-time ROCm cases)"
    - "internal/inference/running_offload_test.go — TestRunningServerROCmResidency + TestRunningServerROCmBusySignal (D-06)"
    - "internal/inference/seam_test.go — TestROCmMarkerPresence + extended negative-gate image pattern"

key-decisions:
  - "ROCm CPU-fallback START-TIME fixture yields WARN (not FAIL): the start-time scrape has no offloaded line and no ROCm device line, so it correctly degrades to could-not-evaluate; the RUNNING-path CPU fixture (a CPU model buffer line) is the FAIL — both semantics preserved from Plan 01"
  - "TestROCmMarkerPresence gates on ROCm0 (NOT ggml_cuda): the cuda-init prefix is SHARED with the CUDA path and would not distinguish a dropped ROCm descriptor"
  - "Negative seam gate extended with rocm-7.2.4|rocm7-nightlies for explicit intent; kyuz0|docker.io/ already bind the rocm token (verified)"

patterns-established:
  - "Ordered-env assertion: TestROCmContainerArgs asserts HSA_OVERRIDE_GFX_VERSION precedes ROCBLAS_USE_HIPBLASLT by index, not just presence"
  - "Busy-signal fixtures route through the real detect.GPUBusyPercentForTest seam (temp drmRoot gpu_busy_percent file) — no new detect code, mirroring the mem_info_gtt_used fixture pattern"

requirements-completed: [ROCM-01, ROCM-02, ROCM-04]

# Metrics
duration: 5min
completed: 2026-06-06
---

# Phase 6 Plan 02: ROCm Backend + Resolver Spine Summary

**The second backend and the one place that picks it: `backend_rocm.go` (digest-pinned rocm-7.2.4, /dev/kfd+/dev/dri, render group, ordered HSA→HIPBLASLT env, ROCm0 residency markers) behind the seam, plus `BackendFor(name)` that fails closed on an unknown config string — proven end-to-end off-hardware by 5 ROCm fixtures including the D-06 gpu_busy_percent corroborator. Vulkan stays the default and byte-identical.**

## Performance
- **Duration:** ~5 min
- **Started:** 2026-06-05T23:12Z
- **Completed:** 2026-06-06
- **Tasks:** 3
- **Files:** 13 (8 created + 5 modified; of the created, 1 production source, 1 resolver, 1 test, 5 fixtures)

## Accomplishments
- Added `backendROCm` as the Vulkan sibling: stateless struct, compile-time `var _ Backend = backendROCm{}` assertion (proves it satisfies the interface incl. `ResidencyProof`), the digest-pinned `rocm-7.2.4@sha256:2da150c1f025…` image (never the nightlies tag, D-08), and a `ContainerArgs` delta over Vulkan — `/dev/kfd` + `/dev/dri`, `--group-add render`, ordered `HSA_OVERRIDE_GFX_VERSION=11.5.1` then `ROCBLAS_USE_HIPBLASLT=1`, reusing the shared loopback publish, read-only model bind, and mandatory `-ngl 999 -fa 1 --no-mmap` flags.
- `ResidencyProof()` returns the ROCm analog markers (`ROCm0` / `- ROCm` / `ggml_cuda_init:` / `Memory access fault by GPU node`, `RejectSoftwareRenderer=false`); the Plan-01 parameterized scrapers consume them with zero offload-math changes.
- Added `BackendFor(name) (Backend, error)` — the single polymorphism point — that resolves `""`/`vulkan`→Vulkan, `rocm`→ROCm, and FAILS CLOSED on any other value (nil Backend + actionable error naming the bad value), never a silent privileged fallback (D-01/D-02, T-6-03).
- Proved the ROCm residency verdict end-to-end off-hardware with 5 fixtures: ROCm0 N/N → PASS, CPU-only → FAIL, GPU-fault → FAIL (fault voids residency before any buffer-line PASS), partial 1/65 → FAIL, device_info + offloaded 65/65 → PASS. The PASS fixture carries BOTH a `ROCm0` and a `ROCm_Host` line (Pitfall 2: `ROCm0` must not match `ROCm_Host`).
- Exercised the D-06 `gpu_busy_percent` signal through the real `detect.GPUBusyPercentForTest` reader (temp drmRoot fixture): a Known 37% busy reading corroborates the ROCm0 N/N PASS, and an absent/Unknown reading is neutral-for-PASS (never a false-FAIL, D-07/Q2).
- Added the positive `TestROCmMarkerPresence` grep-gate (ROCm0/HSA/kfd must live in `backend_rocm.go`) and verified+extended the negative seam gate to bind the rocm image token. Vulkan verdicts and the byte-frozen `status --json` golden are unchanged; the whole `internal/inference` package is green (69 tests).

## Task Commits
1. **Task 1: backend_rocm.go (image, ContainerArgs delta, ResidencyProof ROCm markers)** — `225bd66` (feat)
2. **Task 2: BackendFor resolver (fail-closed) + ROCm backend tests** — `ac65ce8` (feat)
3. **Task 3: ROCm fixtures + scrape table cases (busy signal D-06) + positive marker grep-gate** — `9d468e7` (test)

_Note: Tasks 1 and 2 are tdd="true"; the Task-1 production file is exercised by the Task-2 tests (TDD RED/GREEN folded across the two commits because the resolver tests depend on the resolver+backend compiling together). Task 3 is the fixture/test task._

## Decisions Made
- **The CPU-fallback start-time fixture is WARN, not FAIL.** `scrapeOffloadLog` on a journal with no `offloaded` line and no ROCm device line correctly degrades to could-not-evaluate (WARN). The FAIL for a CPU fallback is the RUNNING-path scrape on a CPU model-buffer line. Both match the Plan-01 semantics exactly; the ROCm cases just re-prove them with the ROCm descriptor.
- **TestROCmMarkerPresence gates on ROCm0, not ggml_cuda.** The cuda-init prefix is shared with the CUDA backend path, so gating on it would not catch a dropped ROCm descriptor. `ROCm0` + `HSA_OVERRIDE_GFX_VERSION` + `/dev/kfd` are the imperative ROCm-only tokens.
- **The negative seam gate already bound the rocm token** (`kyuz0|docker.io/` match it); `rocm-7.2.4|rocm7-nightlies` was added for explicit reviewer intent, not because of a gap.

## Deviations from Plan
None — plan executed exactly as written. (Rules 1–3 were not triggered; the start-time CPU-fixture WARN-vs-FAIL is the plan's own semantics, documented above as a clarification, not a deviation.)

## Threat Surface
All three plan threat-register mitigations are implemented and test-covered: T-6-03 (fail-closed resolver — `TestBackendFor` asserts unknown → nil+error), T-6-04 (digest pin — `TestROCmImageDigestPinned` enforces `@sha256:` + 64 hex), T-6-06 (seam leak — positive + negative gates), T-6-09 (busy corroborator never false-FAILs — `TestRunningServerROCmBusySignal`). No new security surface introduced beyond the planned `/dev/kfd` token, which is ENCODED only (no unit rendered/run this phase — T-6-05 accept, forwarded to Phase 7/8). No package-manager installs (container image only, digest-pinned).

## Known Stubs
None. The `/dev/kfd` device arg and ROCm env are encoded behind the seam but not yet rendered to a quadlet unit or run — this is the intended Phase-6 boundary (render is Phase 7, live switch is Phase 8), explicitly scoped in the plan's threat register (T-6-05) and not a stub blocking this plan's goal.

## Next Phase Readiness
- Plan 03 can re-route the 8 existing `VulkanBackend()` call sites through `BackendFor(cfg.Backend)` so `backend = rocm` in config.toml flips the whole inference path.
- Phase 7 owns the ROCm rendered-unit byte-golden (and the render/video group decision A3); Phase 8 owns the live decode-time `gpu_busy_percent` read (the Known-zero→FAIL rule is already unit-proven). No blockers.

## Self-Check: PASSED
- Created files verified on disk: backend_rocm.go, backend.go, backend_rocm_test.go, 5 testdata fixtures, 06-02-SUMMARY.md.
- Task commits verified in git history: 225bd66, ac65ce8, 9d468e7.
- `go vet ./...` clean; `go test ./...` fully green (inference: 69 passed; Vulkan cases + status --json golden unchanged). TestROCmMarkerPresence proven real (deleting ROCm0 fails it); negative seam regex verified to match the rocm image token.

---
*Phase: 06-rocm-backend-resolver-spine*
*Completed: 2026-06-06*
