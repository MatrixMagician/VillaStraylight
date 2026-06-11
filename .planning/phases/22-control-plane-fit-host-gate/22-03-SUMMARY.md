---
phase: 22-control-plane-fit-host-gate
plan: 03
subsystem: doctor
tags: [go, doctor, memory, residency, offload, down-rank, no-false-green]

# Dependency graph
requires:
  - phase: 22-control-plane-fit-host-gate
    plan: 01
    provides: "MemoryInputs/footprint contracts; liveWeightBytes zero-value-inputs invariance keeping status.json.golden frozen"
  - phase: 22-control-plane-fit-host-gate
    plan: 02
    provides: "preflight.RunMemory(p, MemoryGateInput) — the exact runner doctor folds via findingFromCheck (D-08)"
  - phase: 19-vector-store-embeddings
    provides: "orchestrate accessors (EmbedImage, QdrantContainerUnitName, EmbedContainerUnitName) + runProbeCurl villa.network probe pattern"
provides:
  - "doctor.Deps memory growth: MemoryEnabled, MemoryServices (.service names), RunMemoryChecks, ResidencyUnderLoad — all nil/zero-safe (memory-off byte-identical)"
  - "MEM-DOC-residency finding: chat-model residency under a REAL embedding workload — confident CPU fallback = BLOCK FAIL, unevaluable = typed-Unknown WARN, nil seam = no finding (never PASS-by-default)"
  - "Non-GPU memory-service offload down-rank: visible but non-rank-raising on Status==WARN only — doctor PASS reachable on a healthy memory-on stack (Pitfall 1 resolved)"
  - "runResidencyUnderLoad live seam: D-10 read-only precondition gate + bounded goroutine embed drive + mid-drive RunningOffloadVerdict sample"
affects: [22-04, 23-surfacing-backup-swap]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Down-rank-but-visible for non-GPU offload WARNs: the supersession-shaped (memory-on AND offload:<svc> in MemoryServices AND Status==WARN) conjunction — a confident FAIL is never suppressed"
    - "Concurrent drive + mid-load sample: buffered completion channel lets the consumer sample journal/GTT while the producer goroutine has the next request in flight; channel close is the join"

key-files:
  created:
    - cmd/villa/testdata/doctor-memory-pass.golden
    - cmd/villa/testdata/doctor-memory-residency-fail.golden
    - cmd/villa/testdata/doctor-memory.json.golden
  modified:
    - internal/doctor/doctor.go
    - internal/doctor/doctor_test.go
    - cmd/villa/doctor.go
    - cmd/villa/doctor_test.go

key-decisions:
  - "Down-rank predicate includes MemoryEnabled in the conjunction (belt-and-suspenders over MemoryServices-emptiness alone) — strictly more conservative, FAIL never suppressed either way"
  - "Drive errors degrade a PASS sample to WARN but never overwrite a confident residency FAIL — the FAIL signal is the chat model's residency, not the drive's success (D-09)"
  - "Memory-on doctor test fixtures build on rocmDoctorDeps: the only off-hardware path where host-prep PASS (and therefore Overall PASS) is constructible; the down-rank under test is backend-independent"

patterns-established:
  - "MEM-DOC-* doctor finding IDs for the opt-in memory subsystem (MEM-PRE-* preflight precedent)"

requirements-completed: [CTRL-03]

# Metrics
duration: 13min
completed: 2026-06-10
---

# Phase 22 Plan 03: Doctor Memory Fold + Under-Load Residency Proof Summary

**`villa doctor` now folds the memory host gate (preflight.RunMemory reused verbatim) and a chat-model residency-under-REAL-embedding-load proof into its PASS/WARN/FAIL exit contract, with the villa-qdrant/villa-embed typed-Unknown offload WARNs down-ranked-but-visible so PASS is finally reachable on a healthy memory-on stack — while a confident CPU fallback anywhere is never suppressed**

## Performance

- **Duration:** ~13 min
- **Started:** 2026-06-10T16:33:03Z
- **Completed:** 2026-06-10T16:46:00Z
- **Tasks:** 2 (Task 1 TDD: RED + GREEN commits)
- **Files modified:** 7

## Accomplishments

- D-08 composition: `doctor.Aggregate` folds `RunMemoryChecks` results through the existing `findingFromCheck` — zero new aggregation logic; MEM-PRE-disk/MEM-PRE-headroom rank worst-wins like every other check (a confident headroom FAIL raises Overall to FAIL)
- D-09 mapping: `residencyUnderLoadFinding` copies offloadFinding's opaque-Verdict switch — StatusFail → BLOCK FAIL with remediation, StatusWarn → typed-Unknown WARN, StatusPass → PASS; a nil seam emits NO finding at all (never PASS-by-default)
- Pitfall 1 resolved (Research Open Question 3 → down-rank-but-visible): `offload:<svc>` findings for Deps-supplied MemoryServices with Status==WARN are kept visible but contribute nothing to the worst-wins rank; the conjunction keeps Status==WARN explicit so a confident FAIL on the same service still folds to FAIL (DOCTOR-02, pinned by TestMemoryOffloadFailNotSuppressed)
- D-10 read-only live proof: precondition gate (memory.Decide valid + villa-llama/villa-qdrant/villa-embed active via the existing is-active seam) degrades to WARN naming the precondition — doctor never starts a service; only `podman run --rm` probe containers are used
- T-22-09/T-22-11 bounded drive: 12 sequential POSTs of a ~4.2 KiB JSON-marshaled body (model id never interpolated into a command string) to the config-resolved villa-embed `/v1/embeddings` via `runProbeCurl` over villa.network with `orchestrate.EmbedImage()`; 10 s per request, 60 s parent budget, completion channel drained before return so no probe container outlives the call
- Pitfall 6 closed: the residency sample fires after ≥2 completed requests WHILE the next request is in flight (buffered channel interleaving), evaluating `RunningOffloadVerdict` over exactly the liveStatusDeps input set (villa-llama ResidencyJournal + point-in-time GTTUsedBytes + liveWeightBytes + `BackendFor(cfg.Backend).ResidencyProof()`)
- Memory-off byte-identical: all four Deps fields nil/zero-safe; every pre-existing doctor test and golden passes unmodified; `reportSchemaVersion` stays 1 (findings are data, not schema); `git status --porcelain cmd/villa/testdata/` showed only the 3 NEW golden files
- Phase 23 boundary respected: zero changes to `internal/status/`, `status.json.golden`, or any existing doctor golden; `TestSeamGrepGate` green with zero allowlist changes; `make check` green (22 packages)

## Task Commits

Each task was committed atomically:

1. **Task 1: doctor core — Deps growth, memory fold, MEM-DOC-residency mapping, offload down-rank (TDD)**
   - RED: `ef52b70` (test) — failing tests for nil/zero-safety, check fold, D-09 mapping, healthy-memory-on PASS, FAIL-not-suppressed
   - GREEN: `60bb35d` (feat) — four Deps fields, fold steps 1b/2b, residencyUnderLoadFinding, down-rank predicate
2. **Task 2: live wiring — liveDoctorDeps growth + under-load proof seam** - `b9bb439` (feat)

## Files Created/Modified

- `internal/doctor/doctor.go` - Deps growth (4 nil/zero-safe fields with D-08/D-09 doc comments), memory-check fold (step 1b), MEM-DOC-residency emission (step 2b), `residencyUnderLoadFinding`, memory-offload down-rank predicate (step 4b) alongside `superseded`
- `internal/doctor/doctor_test.go` - memoryDoctorDeps/memoryOnStatusReport fixtures + 6 new invariant tests (memory-off identity, fold FAIL raise, proof FAIL/WARN mapping, healthy-on PASS with visible WARNs, FAIL-not-suppressed)
- `cmd/villa/doctor.go` - liveDoctorDeps memory-seam block (rocmImageGate conditional shape), `unitServiceName` (.container → .service derivation), `liveResidencyUnderLoad`/`runResidencyUnderLoad` + drive tuning consts
- `cmd/villa/doctor_test.go` - memoryHealthyReport/memoryResidencyFailReport fixtures, TestDoctorMemoryRender/TestDoctorMemoryJSON (new goldens), TestLiveDoctorDepsWiresMemorySeams (off → all nil/zero; on → bound + accessor-derived names)
- `cmd/villa/testdata/doctor-memory-pass.golden`, `doctor-memory-residency-fail.golden`, `doctor-memory.json.golden` - NEW additive render/JSON fixtures (existing goldens untouched)

## Decisions Made

- The down-rank predicate conjoins `MemoryEnabled` with the MemoryServices membership and `Status==statusWarn` checks — strictly more conservative than the plan's two-term predicate; suppression can only ever fire on the opted-in path
- A PASS residency sample under a faltering drive (any curl failures) degrades to WARN — the workload was not proven to exercise the embedder, so PASS would be a false-green; a confident FAIL or an already-WARN verdict stands unmodified
- `runResidencyUnderLoad` passes `ConfigModel: cfg.Model` / `ConfigContext: cfg.Ctx` and nil Props (per the plan's input list) — the /props drift overlay is identity corroboration the proof does not need

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Memory-on test fixtures rebased onto rocmDoctorDeps**
- **Found during:** Task 1 (GREEN run)
- **Issue:** The RED tests asserted Overall PASS on fixtures based on `newDoctorDeps()` (vulkan path), but off-hardware `preflight.Run` over the empty test HostProfile emits typed-Unknown WARNs by construction — host-prep PASS (and therefore Overall PASS) is only constructible via the ROCm fixture path, the same constraint `TestROCmResidencySupersedesHostPrepWARN` works under
- **Fix:** `memoryDoctorDeps` builds on `rocmDoctorDeps()` (documented in the fixture comment); the memory-off identity test asserts finding-absence rather than a profile-dependent Overall
- **Files modified:** internal/doctor/doctor_test.go
- **Commit:** 60bb35d

**2. [Rule 1 - Bug] Marker-literal fixture detail reworded; quoted service-name comment removed**
- **Found during:** Task 2 (acceptance-criteria audit)
- **Issue:** A cmd-tier fixture detail typed `Vulkan0` (a backend marker literal — gate skips _test.go but the project convention keeps markers out of test files too), and a doc comment in cmd/villa/doctor.go quoted `"villa-qdrant.service"`, tripping the acceptance grep for typed service literals
- **Fix:** Fixture detail reworded to a neutral "chat-model device buffer …"; comment reworded to "never a typed service-name literal"; goldens re-frozen (still NEW-only)
- **Files modified:** cmd/villa/doctor_test.go, cmd/villa/doctor.go, new goldens
- **Commit:** b9bb439

### Out-of-scope discoveries (not fixed)

- `go fmt` flagged pre-existing formatting drift in `cmd/villa/bench_compare.go` and `cmd/villa/verify_memory_test.go` (unrelated to this plan); the incidental reformat was reverted and the items logged to `deferred-items.md`

## Issues Encountered

None beyond the auto-fixed deviations above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- CTRL-03 is code-complete: doctor folds memory checks + the under-load residency finding into its exit contract; PASS reachable on a healthy memory-on stack; confident CPU fallback = FAIL, unevaluable = typed-Unknown WARN; doctor never mutates state
- Plan 22-04 (on-hardware UAT) proves the live path on gfx1151: the real embed drive, the mid-drive sample against the running chat model, and the healthy-memory-on `villa doctor` exit 0
- Phase 23 inherits the down-rank precedent: when the status-side non-GPU N/A-offload pattern lands (schema 2→3), the doctor down-rank predicate can be revisited (the WARN findings it suppresses will become N/A rows upstream)

## Self-Check: PASSED

All created/modified files exist on disk; commits ef52b70, 60bb35d, b9bb439 verified in git log.

---
*Phase: 22-control-plane-fit-host-gate*
*Completed: 2026-06-10*
