---
phase: 06-rocm-backend-resolver-spine
plan: 01
subsystem: inference
tags: [go, residency-proof, offload-assert, rocm, vulkan, gpu, seam, backend-interface]

# Dependency graph
requires:
  - phase: 02-gpu-validated-inference-slice
    provides: "Backend/Runner/Verdict seam, scrapeOffloadLog + scrapeLoadTensorsVulkan offload-assert, combineOffload/gttFloor/parseBufferMiB combiners, backend_vulkan.go exemplar"
provides:
  - "ResidencyMarkers struct (DeviceToken/DeviceLabel/StartLogDevicePrefix/FaultString/RejectSoftwareRenderer) — the backend-owned, log-shape-only residency descriptor"
  - "ResidencyProof() ResidencyMarkers method on the Backend interface; backendVulkan implements it byte-identically (Vulkan0 / - Vulkan / ggml_vulkan:)"
  - "scrapeLoadTensorsResidency(journal, m) — descriptor-driven running scrape (renamed from scrapeLoadTensorsVulkan) + fault scan"
  - "scrapeOffloadLog(stderr, m) — descriptor-driven start-time scrape + 0<N<M partial-offload FAIL rule"
  - "gpuBusyFloor(busy detect.Int) helper folding the D-06 gpu_busy_percent signal through combineOffload (Known non-zero corroborates, Known-zero FAILs, absent/Unknown neutral-for-PASS)"
  - "RunningOffloadInput.Markers + .GPUBusyPercent fields; ValidateInput.Markers field"
affects: [06-02 (ROCm backend impl + BackendFor resolver), 06-03 (8-site re-route to BackendFor), phase-07 (ROCm quadlet render), phase-08 (live backend switch + decode-time gpu_busy read), phase-09 (bench), phase-10 (status/dashboard surfacing)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Backend-owned residency descriptor (ResidencyMarkers) behind the Backend seam — both scrape paths parameterized by it, no hardcoded device literals in the scrapers"
    - "Conditional combineOffload fold: an Unknown/absent corroborating signal is made combine-neutral by SKIPPING the fold (never a WARN), because combineOffload has no neutral state"
    - "Provenance strings embed the backend DeviceToken so the byte-frozen status --json golden stays identical for Vulkan"

key-files:
  created:
    - "internal/inference/testdata/radv_partial_fail.stderr — N<M (1/65) partial-offload fixture"
  modified:
    - "internal/inference/inference.go — ResidencyMarkers struct + ResidencyProof() on Backend"
    - "internal/inference/backend_vulkan.go — Vulkan ResidencyProof() (byte-identical literals)"
    - "internal/inference/running_offload.go — descriptor-driven scrape + fault scan + gpuBusyFloor fold"
    - "internal/inference/offload.go — descriptor-driven scrape + N<M partial-FAIL"
    - "internal/inference/validate.go — ValidateInput.Markers threaded to scrapeOffloadLog"
    - "internal/status/status.go — set RunningOffloadInput.Markers from backend.ResidencyProof()"
    - "cmd/villa/inference.go — set ValidateInput.Markers from backend.ResidencyProof()"

key-decisions:
  - "Provenance embeds m.DeviceToken to keep the status --json golden byte-frozen for Vulkan instead of changing the golden"
  - "Unknown/absent gpu_busy is made combine-neutral by skipping the fold (not by emitting WARN), preserving byte-identical Vulkan verdicts"
  - "Markers wired now in the two existing callers (inference.go, status.go) from VulkanBackend().ResidencyProof(); Plan 03 swaps the constructor for BackendFor(cfg.Backend)"

patterns-established:
  - "ResidencyMarkers descriptor: log-shape-only, owns no runtime signals (gpu_busy is a per-run input, not a per-backend literal)"
  - "gpuBusyFloor mirrors gttFloor's typed-Unknown discipline; reuses combineOffload, never re-rolls the offload math"

requirements-completed: [ROCM-02]

# Metrics
duration: 25min
completed: 2026-06-06
---

# Phase 6 Plan 01: Residency-Proof Spine Summary

**Backend-neutral residency-proof engine: `ResidencyMarkers` descriptor + `ResidencyProof()` on the Backend interface, both offload-assert scrapes parameterized by it, a journal fault scan, a 0<N<M partial-offload FAIL, and the D-06 gpu_busy_percent signal folded through `combineOffload` — Vulkan verdicts byte-identical.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-06-06
- **Completed:** 2026-06-06
- **Tasks:** 3
- **Files modified:** 11 (10 modified + 1 fixture created)

## Accomplishments
- Extended the `Backend` interface with `ResidencyProof() ResidencyMarkers`; Vulkan implements it reproducing today's exact literals (`Vulkan0` / `- Vulkan` / `ggml_vulkan:`) so the refactor is a behavior no-op.
- Parameterized BOTH scrape paths by the descriptor: `scrapeLoadTensorsResidency(journal, m)` (running) and `scrapeOffloadLog(stderr, m)` (start-time) — no hardcoded device literals remain in either scraper body.
- Added the journal fault scan (a non-empty `FaultString` found voids residency → FAIL before the buffer-line switch; empty Vulkan FaultString is a no-op) and the start-time N<M partial-offload FAIL (gated on an explicit `offloaded N/M` line so Vulkan auto-fit still PASSes).
- Wired the D-06 `gpu_busy_percent` signal as a `RunningOffloadInput.GPUBusyPercent` input folded through `combineOffload` via the new `gpuBusyFloor` helper: Known non-zero corroborates PASS, Known-zero FAILs a claimed-healthy decode, absent/Unknown is combine-neutral (the fold is skipped, never a WARN).
- Kept the whole `internal/inference` package green with all Vulkan cases byte-identical in verdict, and the byte-frozen `status --json` golden unchanged.

## Task Commits

Each task was committed atomically:

1. **Task 1: ResidencyMarkers + ResidencyProof() interface method + Vulkan impl** - `af1b129` (feat)
2. **Task 2: Parameterize running scrape + fault scan + gpu_busy_percent fold (D-06)** - `bdba511` (feat)
3. **Task 3: Parameterize start-time scrape + N<M partial-FAIL + thread ValidateInput** - `9aeaa0f` (feat)

_Note: each TDD task's RED test and GREEN implementation were committed together in the single task commit (the package would not compile with a renamed-signature test split out from its production change)._

## Files Created/Modified
- `internal/inference/inference.go` - `ResidencyMarkers` struct + `ResidencyProof()` on the `Backend` interface.
- `internal/inference/backend_vulkan.go` - `backendVulkan.ResidencyProof()` returning today's exact Vulkan literals.
- `internal/inference/running_offload.go` - renamed scrape → `scrapeLoadTensorsResidency(journal, m)`, fault scan, `gpuBusyFloor` helper, `verdictAsResult` collapse, conditional busy fold, `Markers`/`GPUBusyPercent` fields.
- `internal/inference/offload.go` - `scrapeOffloadLog(stderr, m)` descriptor-driven + 0<N<M partial-offload FAIL rule.
- `internal/inference/validate.go` - `ValidateInput.Markers` threaded to `scrapeOffloadLog`.
- `internal/status/status.go` - capture `backend` once; set `RunningOffloadInput.Markers` from `backend.ResidencyProof()`.
- `cmd/villa/inference.go` - set `ValidateInput.Markers` from `backend.ResidencyProof()`.
- `internal/inference/running_offload_test.go` - busy-fold cases (corroborate/FAIL/neutral), fault-scan test, Markers on existing Vulkan cases.
- `internal/inference/offload_test.go` - partial-FAIL case + auto-fit gating proof, Markers on existing cases.
- `internal/inference/validate_test.go` - Markers on the shared `baseInput` + the chat-fail construction.
- `internal/inference/testdata/radv_partial_fail.stderr` - new 1/65 partial-offload fixture.

## Decisions Made
- **Provenance embeds the DeviceToken** rather than dropping it: the descriptor refactor made the running provenance backend-neutral ("...load_tensors residency..."), which broke the byte-frozen `status --json` golden. Embedding `m.DeviceToken` restores the exact "...load_tensors Vulkan0 residency..." string for Vulkan, keeping the golden unchanged while staying backend-neutral.
- **Unknown/absent gpu_busy is neutral by SKIPPING the fold**, not by returning a WARN. `combineOffload` is a strict "any FAIL→FAIL; else any WARN→WARN; else PASS" combiner with no neutral state, so folding a WARN would downgrade every Vulkan PASS. The conditional fold (only when `GPUBusyPercent.Known`) is the load-bearing detail and is asserted explicitly in `TestRunningServerBusySignalFold`.
- **Markers wired in the existing callers now** (from `VulkanBackend().ResidencyProof()`); Plan 03 will replace the `VulkanBackend()` constructor with `BackendFor(cfg.Backend)` at all sites.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Threaded Markers through the two existing ValidateInput/RunningOffloadInput callers**
- **Found during:** Task 3 (threading ValidateInput)
- **Issue:** The renamed/re-signatured scrapes default an unset `Markers` to an all-empty descriptor (no device-token match), which made `internal/status` tests (`TestStatusExitCodes`, `TestAggregateWorstWins`) regress — a CPU-only journal returned WARN instead of FAIL because no device token matched. The plan named Plan 03 as the marker-wiring owner, but leaving the v1.0 callers unwired regresses current behavior.
- **Fix:** Set `Markers` from `backend.ResidencyProof()` in `cmd/villa/inference.go` and `internal/status/status.go` (capturing the already-resolved `VulkanBackend()` once). This preserves v1.0 behavior; Plan 03 still owns the `VulkanBackend()`→`BackendFor()` swap.
- **Files modified:** cmd/villa/inference.go, internal/status/status.go
- **Verification:** `go test ./...` fully green (status FAIL cases restored).
- **Committed in:** `9aeaa0f` (Task 3 commit)

**2. [Rule 1 - Bug] Provenance change broke the byte-frozen status --json golden**
- **Found during:** Task 3 (full-repo regression)
- **Issue:** Making the running provenance backend-neutral dropped "Vulkan0" → `TestStatusJSONGolden` failed (the golden is byte-frozen per CLAUDE.md).
- **Fix:** Embed `m.DeviceToken` in the provenance string so Vulkan renders the identical pre-refactor text.
- **Files modified:** internal/inference/running_offload.go
- **Verification:** `TestStatusJSONGolden` green; whole repo green.
- **Committed in:** `9aeaa0f` (Task 3 commit)

---

**Total deviations:** 2 auto-fixed (1 blocking, 1 bug)
**Impact on plan:** Both auto-fixes were necessary to keep the refactor a true no-op for Vulkan (the plan's #1 invariant, SC#4) — no scope added. The Markers-wiring overlaps slightly with Plan 03's re-route but only sets the field from the already-present `VulkanBackend()`; the constructor swap remains Plan 03's work.

## Issues Encountered
- The plan's `<verify>` block named tests (`TestImageDigestPinned`, `TestScrapeOffloadLog`) that do not exist under those names; the equivalent existing tests are `TestImageDigestPinned`→the `@sha256:` assertion in `inference_test.go`, and `TestScrapeOffloadLog`→`TestOffloadLogScrape`/`TestScrapeOffloadPartialGating`. Ran the actual tests plus the full package; all green.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- The residency spine is complete: a ROCm backend (Plan 02) can implement `ResidencyProof()` returning `ROCm0` markers and both scrapes will key off it with zero offload-math changes.
- `RunningOffloadInput.GPUBusyPercent` is wired as an input; Plan 02 can fixture ROCm busy-signal cases via `detect.GPUBusyPercentForTest`. The live decode-time read lands in Phase 8.
- No blockers.

## Self-Check: PASSED

- Created files verified on disk: 06-01-SUMMARY.md, testdata/radv_partial_fail.stderr (+ all modified source files present).
- Task commits verified in git history: af1b129, bdba511, 9aeaa0f.
- `go vet ./...` clean; `go test ./...` fully green (inference: 51 passed; status FAIL cases + status --json golden intact).

---
*Phase: 06-rocm-backend-resolver-spine*
*Completed: 2026-06-06*
