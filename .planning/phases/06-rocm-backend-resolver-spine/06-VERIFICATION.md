---
phase: 06-rocm-backend-resolver-spine
verified: 2026-06-06T00:00:00Z
status: passed
score: 14/14 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  note: initial verification
---

# Phase 6: ROCm Backend + Resolver Spine Verification Report

**Phase Goal:** A ROCm/HIP backend exists behind the v1.0 `Backend` interface and is selected from config — the single resolver `BackendFor(cfg.Backend)` routes every inference call site, and the offload-assert proves ROCm residency (not just Vulkan). While Vulkan stays the only configured backend it is a behavior no-op, but the precondition for switching, benching, and surfacing.
**Verified:** 2026-06-06
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
| - | ----- | ------ | -------- |
| 1 | `BackendFor(name) (Backend, error)` resolver exists, fail-closed on unknown, empty/vulkan→Vulkan, rocm→ROCm | ✓ VERIFIED | `internal/inference/backend.go:21-30` — switch returns `(nil, error)` in default case; `TestBackendFor/unknown_fails_closed` PASS |
| 2 | `backend_rocm.go` exists with digest-pinned rocm-7.2.4 image (NEVER nightlies) | ✓ VERIFIED | `backend_rocm.go:26` const `...:rocm-7.2.4@sha256:2da150c1f025...`; `TestROCmImageDigestPinned` asserts `@sha256:[0-9a-f]{64}` PASS |
| 3 | ROCm ContainerArgs: /dev/kfd + /dev/dri + render group + ordered HSA_OVERRIDE_GFX_VERSION=11.5.1 then ROCBLAS_USE_HIPBLASLT=1 + -ngl 999 -fa 1 --no-mmap | ✓ VERIFIED | `backend_rocm.go:64-84` — exact arg order; flags via `llamaServerFlags` (`backend_vulkan.go:53`); `TestROCmContainerArgs` PASS |
| 4 | `backend = "vulkan"` yields unchanged Vulkan path (behavior no-op) | ✓ VERIFIED | `backend_vulkan.go:115-123` Vulkan0 markers unchanged; status --json golden green + unmodified in git; busy fold skipped for Vulkan |
| 5 | Offload-assert proves ROCm residency: ROCm0 device-buffer + offloaded N/N + gpu_busy signal (folded conditional-on-Known) + absence of "Memory access fault by GPU node" | ✓ VERIFIED | `TestRunningServerROCmResidency` (N/N→PASS, cpu→FAIL, fault→FAIL), `TestROCmOffloadLogScrape`, `TestRunningServerBusySignalFold` all PASS |
| 6 | CPU fallback is a FAIL (not silent pass) | ✓ VERIFIED | `TestRunningServerROCmResidency/rocm_cpu-only_journal_→_FAIL` PASS; running dual-assert is the authoritative FAIL path |
| 7 | Grep-gate fails if ROCm0/HIP marker strings dropped from backend_rocm.go (TestROCmMarkerPresence) | ✓ VERIFIED | `seam_test.go:96-107` checks ROCm0, HSA_OVERRIDE_GFX_VERSION, /dev/kfd; PASS |
| 8 | Negative seam gate covers the rocm image token (TestSeamGrepGate) | ✓ VERIFIED | `seam_test.go:42` pattern includes `rocm-7\.2\.4|rocm7-nightlies`; PASS |
| 9 | BOTH scrape paths parameterized by ResidencyMarkers (start-time offload.go AND running running_offload.go) | ✓ VERIFIED | `offload.go:74 scrapeOffloadLog(stderr, m ResidencyMarkers)` + `running_offload.go:110 scrapeLoadTensorsResidency(journal, m ResidencyMarkers)` — both key off `m.DeviceToken/DeviceLabel/StartLogDevicePrefix/FaultString/RejectSoftwareRenderer` |
| 10 | All physical `inference.VulkanBackend()` call sites resolve through `BackendFor(cfg.Backend)`; grep returns ZERO in non-test files (excluding definition) | ✓ VERIFIED | `grep -rn "inference\.VulkanBackend()" --include=*.go | grep -v _test.go` → 0 matches (exit 1); 7 `inference.BackendFor(` sites (8 occurrences, install.go ×2) |
| 11 | Each re-routed site surfaces BackendFor error (no silent swallow, no new panic) | ✓ VERIFIED | lifecycle.go:65-67, install.go:238-241/649-651, status.go:179-181, inference.go:129-137, model.go:360 — each returns/blocks on err |
| 12 | VulkanBackend() stays exported | ✓ VERIFIED | `backend_vulkan.go:63 func VulkanBackend() Backend` still present and exported |
| 13 | Start-time assert receives the resolved backend's markers (validate path no longer Vulkan-only) | ✓ VERIFIED | `cmd/villa/inference.go:178 Markers: backend.ResidencyProof()` from `BackendFor(cfg.Backend)` |
| 14 | CR-01 fix present: SysfsOffload/GTTDeltaBytes preserved on the Known-busy path; non-nested Detail | ✓ VERIFIED | `running_offload.go:320-338` status-only escalation, sysfs/delta NOT routed through combineOffload; `TestRunningServerBusyFoldPreservesContract` PASS (commit 499644e) |

**Score:** 14/14 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/inference/backend.go` | BackendFor resolver (single polymorphism point) | ✓ VERIFIED | Fail-closed switch; wired from 7 sites |
| `internal/inference/backend_rocm.go` | backendROCm + digest-pinned const + ContainerArgs delta + ROCm0 ResidencyProof | ✓ VERIFIED | All present, compile-time `var _ Backend` assertion |
| `internal/inference/backend_vulkan.go` | Vulkan ResidencyProof Vulkan0 markers + exported VulkanBackend() | ✓ VERIFIED | Byte-identical markers retained |
| `internal/inference/inference.go` | Backend interface extended with ResidencyProof() + ResidencyMarkers struct | ✓ VERIFIED | inference.go:71,80 |
| `internal/inference/offload.go` | scrapeOffloadLog parameterized + N<M partial-FAIL | ✓ VERIFIED | `TestScrapeOffloadPartialGating` PASS |
| `internal/inference/running_offload.go` | scrapeLoadTensorsResidency parameterized + fault scan + gpu_busy fold | ✓ VERIFIED | CR-01 fix at 320-338 |
| `internal/inference/seam_test.go` | TestROCmMarkerPresence + TestSeamGrepGate (rocm token) | ✓ VERIFIED | Both PASS |
| ROCm testdata fixtures (5) | PASS/CPU/fault/devinfo/partial | ✓ VERIFIED | All 5 present in testdata/ |
| `cmd/villa/{install,lifecycle,status,model,inference}.go`, `internal/status/status.go` | BackendFor re-routed sites | ✓ VERIFIED | 7 sites, all error-surfacing |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| backend.go | backendROCm{} | `case "rocm"` | ✓ WIRED | backend.go:26 |
| backend_rocm.go | rocmImage digest | `@sha256:2da150c1f025` | ✓ WIRED | backend_rocm.go:26 |
| cmd/villa/*.go + status.go | inference.BackendFor | replaces VulkanBackend() | ✓ WIRED | 7 sites |
| cmd/villa/inference.go | ValidateInput.Markers | backend.ResidencyProof() | ✓ WIRED | inference.go:178 |
| running_offload.go | detect.GPUBusyPercent (D-06) | gpuBusyFloor folded conditional-on-Known | ✓ WIRED | running_offload.go:320 |
| running_offload_test.go | detect.GPUBusyPercentForTest | temp drmRoot fixture | ✓ WIRED | helper at gpu_amd.go:369 |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Build clean | `go build ./...` | Success | ✓ PASS |
| Vet clean | `go vet ./...` | No issues | ✓ PASS |
| Full suite green | `go test ./...` | 461 passed, 17 packages | ✓ PASS |
| Named ROCm/seam/CR-01 tests | `go test -run 'TestSeamGrepGate|TestROCmMarkerPresence|TestBackendFor|TestROCmContainerArgs|TestROCmImageDigestPinned|TestRunningServerBusyFoldPreservesContract'` | all PASS | ✓ PASS |
| Status --json golden unchanged | golden test + `git status` | green, golden files unmodified | ✓ PASS |
| Zero VulkanBackend() non-test sites | grep | 0 matches | ✓ PASS |

### Probe Execution

Step 7c: SKIPPED — no `scripts/*/tests/probe-*.sh` declared; phase verification is via Go test suite (executed above), not shell probes.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| ROCM-01 | 06-02 | Opt-in ROCm/HIP backend behind Backend interface, selected by `backend` config; Vulkan default; no specifics leak | ✓ SATISFIED | backend_rocm.go + BackendFor; seam gate keeps literals in seam |
| ROCM-02 | 06-01, 06-02 | ROCm offload-asserting HIP residency proof (ROCm0 + N/N + gpu_busy + no fault); CPU fallback FAIL; grep-gate | ✓ SATISFIED | Dual-scrape parameterized; TestRunningServerROCmResidency; both grep-gates |
| ROCM-04 | 06-02, 06-03 | Single `BackendFor(cfg.Backend)` routes every site (replacing hardcoded VulkanBackend()) | ✓ SATISFIED | 7 sites re-routed; zero VulkanBackend() callers; suite green |

No orphaned requirements — REQUIREMENTS.md maps exactly ROCM-01/02/04 to Phase 6, all declared in plan frontmatter.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (none) | — | No TBD/FIXME/XXX in any phase-modified file | — | Clean |
| internal/status/status.go | 56 | word "placeholder" in a descriptive comment ("N/A placeholder a non-GPU service") | ℹ️ Info | Pre-existing comment, not a stub indicator |

### Human Verification Required

None. The phase goal is explicitly an off-hardware behavior no-op under the Vulkan default — every truth is programmatically verifiable via the Go test suite, grep gates, and golden freeze. On-hardware ROCm offload (real HSA-override behavior, live `gpu_busy_percent` read, `load_tensors` hang detection) is deferred to Phase 8 (the on-hardware switch verb), per the roadmap's spine-first ordering.

### Outstanding Advisory Notes (informational — not gaps)

- **REVIEW.md status stale:** `06-REVIEW.md` frontmatter still reads `status: issues_found` / `critical: 1`, but the one BLOCKER (CR-01) was fixed in commit `499644e` and is verified present + tested here (`TestRunningServerBusyFoldPreservesContract`). The review file predates the fix.
- **REQUIREMENTS.md line 88 stale:** the tracking table lists ROCM-02 as "In Progress (… markers + grep-gate land in 06-02)". The 06-02 work has landed and is verified; the checkbox at line 16 is `[x]`. Documentation lag only — code is complete.
- **4 advisory warnings (WR-01..WR-04) from 06-REVIEW.md remain open** by design (start-time ROCm CPU fixture is journald-shaped — verdict still correct via device-label-absence; running scrape relies on GTT floor for partial offload; seccomp=unconfined + /dev/kfd security note; runValidation second resolution point). None block the goal; all are tracked for follow-up. The authoritative CPU-fallback FAIL is proven on the running path.

### Gaps Summary

No gaps. All 14 must-haves verified against the codebase. ROCM-01/02/04 satisfied. The resolver spine (`BackendFor`), the ROCm backend behind the `Backend` interface, the parameterized dual offload-assert with ROCm0 residency proof + gpu_busy fold, both grep-gates, and the full re-route of all VulkanBackend() call sites are present, wired, and behavior-no-op under the Vulkan default (build/vet/test all green, golden byte-identical). The CR-01 fix is present and independently confirmed via its regression test.

---

_Verified: 2026-06-06_
_Verifier: Claude (gsd-verifier)_
