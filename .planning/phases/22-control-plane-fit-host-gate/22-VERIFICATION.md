---
phase: 22-control-plane-fit-host-gate
verified: 2026-06-10T19:35:00Z
status: passed
score: 22/22 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Operator sign-off on the on-hardware results (deferred blocking gate from 22-04 Task 2, auto-approved under human_verify_mode: end-of-phase). Review: (1) recommend reservation row + schema-2 JSON, (2) preflight MEM-PRE rows, (3) doctor overall PASS on the healthy stack + the honest WARN with villa-embed stopped (re-run optional: `systemctl --user stop villa-embed.service; ./villa doctor; systemctl --user start villa-embed.service`), (4) D-05 MemoryPeak 116,240,384 B vs the 512 MiB reservation and the recorded 'constant stands' decision, (5) box state: backend rocm, all villa-* units active."
    expected: "Operator types 'approved' (or itemizes failing observations). Items 1, 2 and 5 were re-confirmed live by this verification on the post-fix binary; item 3 healthy-path PASS was witnessed live post-fix by the orchestrator; the embed-down negative control was executed in 22-04 (pre-fix) and its code path is pinned by post-fix unit tests."
    why_human: "The 22-04 plan declared this a blocking checkpoint:human-verify gate; it was auto-approved by configuration, never by the operator. The negative control mutates service state (stop/start villa-embed) which a read-only verifier must not do, and the D-05 measurement decision is an operator judgment call recorded for Phase 23."
---

# Phase 22: Control-Plane Fit + Host Gate Verification Report

**Phase Goal:** The recommended configuration accounts for the embedding model so the memory stack fits the unified-memory envelope, and `villa` refuses or warns up front when the host can't host the memory stack — preserving the "runs healthy after install" bar with no OOM or silent CPU fallback under embedding load.
**Verified:** 2026-06-10T19:35:00Z
**Status:** passed (operator sign-off completed 2026-06-10 via /gsd-verify-work 22 — operator delegated live execution on the Strix Halo host; all evidence re-executed at HEAD 70b83f8 incl. the embed-down negative control and D-05 re-measure 227123200 B ≤ 512 MiB. See 22-UAT.md.)
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC#1: recommend reserves the embedding footprint BEFORE the chat-model fit (envelope shrinks first), append-only field + schema bump | ✓ VERIFIED | `internal/recommend/recommend.go:152-173` — `memoryReservation(mem)` resolved first, `envelope -= reservation` (clamped at 0, no uint64 wrap) before pickOverride/pickBest; `recommendSchemaVersion = 2` (:32); `embedding_reservation_bytes`/`memory_considered` JSON tags above `schema_version` (:106-115). Live: `./villa recommend --json` → `schema_version: 2`, `embedding_reservation_bytes: 536870912`, `memory_considered: true`, exit 0 |
| 2 | SC#2: preflight gates host fitness (vector-index disk + embedder headroom) with refuse-with-remediation | ✓ VERIFIED | `internal/preflight/checks_memory.go` — MEM-PRE-disk (:86) + MEM-PRE-headroom (:126), both TierBlock, every non-PASS carries remediation (pinned by `TestCheckVectorDisk`/`TestCheckEmbedHeadroom`). Live: both rows PASS against the real volume root `~/.local/share/containers/storage/volumes` |
| 3 | SC#3: doctor folds memory-stack health into PASS/WARN/FAIL with an offload-asserting residency proof; silent CPU fallback = FAIL, never false-green | ✓ VERIFIED | `internal/doctor/doctor.go:227-228` (RunMemoryChecks fold), `:292-293` + `:496` (MEM-DOC-residency); `cmd/villa/doctor.go:382-510` (drive + mid-flight sample). Live post-fix doctor run witnessed: overall PASS / exit 0, MEM-DOC-residency BLOCK PASS |
| 4 | memory_enabled=false: recommend math/table/exit byte-identical except 2 new JSON keys + schema 2 | ✓ VERIFIED | Gated table row `if rec.EmbeddingReservationBytes > 0` (`cmd/villa/recommend.go:160-162`); zero-value MemoryInputs identity pinned by recommend table tests; golden shows reservation 0 / considered false at schema 2 |
| 5 | Unrecognized embedding model reserves the 512 MiB conservative default with an honest note, never silent 0 | ✓ VERIFIED | `memoryReservation` (`recommend.go:195-207`): `!fp.Known` → `memory.ConservativeFootprintBytes()` + "RESERVED CONSERVATIVELY: …(D-02)" note naming the model; no re-typed 512 literal in recommend code (grep count 0) |
| 6 | install pick is memory-aware; MinMemBytes includes the reservation | ✓ VERIFIED | `cmd/villa/install.go:1027` `recommend.Pick(p, cat, ov, liveLoadedMemoryInputs())`; `:281` MinMemBytes sum includes `rec.EmbeddingReservationBytes` |
| 7 | memory_enabled=false (or unreadable config): preflight output/exit byte-identical, checks not emitted | ✓ VERIFIED | Fail-soft load gating in `cmd/villa/preflight.go:36`; existing preflight goldens unmodified since phase start; `TestLiveMemoryGateOffPath` green; 22-04 live negative control recorded zero MEM-PRE substrings |
| 8 | Confident shortage = BLOCK FAIL; unevaluable probe = typed-Unknown WARN | ✓ VERIFIED | `checks_memory.go`: resolver/statfs failure → WARN, free<floor → FAIL (or WARN when `EmbedderActive`, WR-03), Unknown MemAvailable → WARN with provenance; all branches table-tested |
| 9 | Opted-in install refuses-with-remediation before bringing up the memory stack | ✓ VERIFIED | `cmd/villa/install.go:1112` appends `preflight.RunMemory(...)` to the install gate results when memory enabled |
| 10 | Doctor PASS reachable on healthy memory-on stack: qdrant/embed offload WARNs down-ranked-but-visible; confident FAIL never suppressed | ✓ VERIFIED | `internal/doctor/doctor.go:365-377` — predicate is the conjunction (MemoryEnabled AND offload: prefix AND svc ∈ MemoryServices AND Status==statusWarn); pinned by doctor tests; live PASS with both WARNs visible |
| 11 | Residency proof drives a REAL /v1/embeddings workload and samples DURING the drive; confident fallback FAIL, unevaluable WARN | ✓ VERIFIED | `cmd/villa/doctor.go:444-487` — sample request launched async, journal/GTT read while it is verifiably in flight, then joined (WR-01); drive errors + sampled PASS degrade to WARN (no false-green); 12 requests, 10s/req, 60s budget, `--rm` containers |
| 12 | Doctor never mutates state; services down → proof degrades to WARN | ✓ VERIFIED | D-10 precondition gate (`doctor.go:384-401`) returns WARN on any inactive unit, no systemctl start anywhere; 22-04 negative control: villa-embed still `inactive` immediately after the run |
| 13 | memory_enabled=false: doctor output byte-identical — all new Deps fields nil/zero-safe | ✓ VERIFIED | All four Deps fields doc-commented nil/zero-safe (`doctor.go:146-172`); fold/finding emission guarded on non-nil; existing doctor goldens unchanged, `reportSchemaVersion = 1` (:57) |
| 14 | Live SC#1: recommend shows the reservation, schema 2, reservation > 0 | ✓ VERIFIED | Re-run by this verification on the post-fix binary (binary 18:26 > last commit 18:24): all three JSON assertions hold |
| 15 | Live SC#2: MEM-PRE rows against the REAL podman volume root; absent when off | ✓ VERIFIED | Re-run by this verification: MEM-PRE-disk PASS at the live-resolved root, MEM-PRE-headroom PASS (71.83 GiB ≥ 0.50 GiB); off-path absence recorded in 22-04 + pinned by tests |
| 16 | Live SC#3: doctor overall PASS with MEM-DOC-residency PASS mid-drive; stack down → WARN never PASS | ✓ VERIFIED | Post-fix live run witnessed by orchestrator (PASS / exit 0, MEM-DOC-residency BLOCK PASS in-flight sample); embed-down WARN negative control recorded in 22-04; WARN path pinned by post-fix unit tests |
| 17 | D-05 measurement recorded; constant raised ONLY if under-reserving | ✓ VERIFIED | 22-04-SUMMARY: MemoryPeak 116,240,384 B (~110.9 MiB) ≤ 536,870,912 B → constant stands; `internal/memory/footprint.go` confirms 512<<20 untouched (contingency correctly not triggered) |
| 18 | Operator's ROCm backend survives verification | ✓ VERIFIED | Live: `backend = "rocm"`, `memory_enabled = true`, villa-llama/embed/qdrant/dashboard all `active` |
| 19 | Boundary: internal/status/ + status.json.golden untouched (Phase 23) | ✓ VERIFIED | `git diff --stat f0a3ee5..HEAD -- internal/status/ cmd/villa/testdata/status.json.golden` empty; `cmd/villa/status.go:409` passes zero-value `MemoryInputs{}` with the invariance comment; `TestPickOverrideWeightInvariance` exists and passes |
| 20 | Boundary: TestSeamGrepGate green with seam_test.go unmodified | ✓ VERIFIED | `go test ./internal/inference/ -run TestSeamGrepGate` ok; `git diff f0a3ee5..HEAD -- internal/inference/seam_test.go` empty; `checks_memory.go` has zero `exec.Command` (podman via `runTool` only) |
| 21 | Boundary: recommendSchemaVersion = 2 with the recommend golden re-frozen exactly once | ✓ VERIFIED | Exactly one commit touches `recommend.golden.json` in the phase (dfc4f8c); golden contains `"schema_version": 2` + both new keys |
| 22 | All 8 post-review fixes (CR-01, WR-01..07) exist in code with regression tests | ✓ VERIFIED | See "Post-Review Fix Verification" below; all 8 commits present (6e5b1a5..493feec) and each fix located in source with a named test |

**Score:** 22/22 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/memory/footprint.go` | `func ConservativeFootprintBytes() uint64` | ✓ VERIFIED | :42, returns the same 512<<20 const the pinned nomic footprint uses; coherence test in footprint_test.go |
| `internal/recommend/recommend.go` | MemoryInputs, envelope-shrink, new fields, schema 2 | ✓ VERIFIED | MemoryInputs :138, shrink :169-173, fields :106-111 above SchemaVersion :115, const :32 |
| `cmd/villa/testdata/recommend.golden.json` | Re-frozen at schema 2 | ✓ VERIFIED | `"schema_version": 2` + both new keys; sole golden modified this phase |
| `internal/preflight/checks_memory.go` | RunMemory, MEM-PRE-disk/headroom, volumeRootFn seam, 1 GiB floor const | ✓ VERIFIED | RunMemory :175, both IDs, `minVectorDiskFloorBytes = 1 << 30` :29, no config import, no exec.Command |
| `internal/preflight/checks_memory_test.go` | Table tests, min 60 lines | ✓ VERIFIED | 222 lines; FAIL/WARN/PASS branches + conservative floor + remediation non-empty |
| `cmd/villa/preflight.go` | Fail-soft gated RunMemory append | ✓ VERIFIED | :36 |
| `internal/doctor/doctor.go` | Deps growth, fold, MEM-DOC-residency, down-rank predicate | ✓ VERIFIED | Fields :146-172, fold :227, finding :496, predicate :375-377 with statusWarn conjunction |
| `cmd/villa/doctor.go` | liveDoctorDeps growth, runProbeCurl drive, mid-load sample | ✓ VERIFIED | Accessor-only service/image names (zero typed literals), bounded contexts, in-flight sample + join |
| `.planning/.../22-04-SUMMARY.md` | On-hardware record incl. D-05 measurement | ✓ VERIFIED | Verbatim outputs, D-05 table, negative controls, box-state restore table |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| recommend.go | internal/memory | `memory.Footprint` + `ConservativeFootprintBytes` | ✓ WIRED | recommend.go:199,203 |
| cmd/villa/install.go | internal/recommend | `liveLoadedMemoryInputs()` into Pick + MinMemBytes | ✓ WIRED | install.go:1027, :281 |
| cmd/villa/preflight.go | checks_memory.go | gated `preflight.RunMemory` append | ✓ WIRED | preflight.go:36 |
| checks_memory.go | internal/memory | footprint floor + conservative default | ✓ WIRED | checks_memory.go:131,136 |
| cmd/villa/install.go | checks_memory.go | RunMemory appended to install gate | ✓ WIRED | install.go:1112 |
| internal/doctor | checks_memory.go | `Deps.RunMemoryChecks` bound in cmd tier | ✓ WIRED | doctor.go:227; binding with `EmbedderActive` at cmd/villa/doctor.go:236 |
| cmd/villa/doctor.go | install_memory.go | drive reuses `runProbeCurl` + `orchestrate.EmbedImage()` | ✓ WIRED | doctor.go:435,462 |
| cmd/villa/doctor.go | running_offload.go | `RunningOffloadVerdict` over the liveStatusDeps input set | ✓ WIRED | doctor.go:474-482 — JournalText, Props, GTTUsed, WeightBytes, catalog-resolved ModelFile (WR-06) |

### Post-Review Fix Verification (1 Critical + 7 Warnings, all claimed fixed)

| Finding | Commit | Code evidence | Regression test |
|---------|--------|---------------|-----------------|
| CR-01 errored status report → fabricated loopback FAIL | 6e5b1a5 | `report.Err()` guard, doctor.go:244+ degrades to WARN | `TestErroredStatusReportDegradesToWarn` |
| WR-01 sample-during-load not enforced | cb71a21 | async sample request, mid-flight read, join (doctor.go:444-487) | doctor drive tests green |
| WR-02 unbounded runTool podman hang | 2a69b62 | `exec.CommandContext` + `toolTimeout` + StdoutPipe bound (exec.go:34-42) | preflight suite green |
| WR-03 headroom double-count on running stack | f937c13 | `MemoryGateInput.EmbedderActive` → WARN not FAIL (checks_memory.go:149-158) | `TestCheckEmbedHeadroom` |
| WR-04 doctor exit fails open on unknown Overall | afe7e6f | `default: return exitBlocked` (doctor.go:114-120) | render tests green |
| WR-05 --dry-run could execute privileged host-prep | 1fe19f1 | `!opts.dryRun` in useWizard (install.go:305) + gap path :696 | `TestInstallDryRunNeverRunsPrivilegedHostPrep` |
| WR-06 wrong ConfigModel + missing Props in proof | 1b9ad98 | `sd.ModelFile(cfg)` + `Props: sd.Props(sd.Endpoint())` (doctor.go:415, 475) | doctor tests green |
| WR-07 --ctx overflow wraps Fits=true | 493feec | `bits.Mul64` saturating product + `addSaturating` (kv.go:38-49) | `TestKVCacheBytesSaturatesOnOverflow`, `TestAddSaturating` |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Phase test packages | `go test ./internal/recommend ./internal/memory ./internal/preflight ./internal/doctor ./cmd/villa -count=1` | all ok | ✓ PASS |
| Seam grep-gate | `go test ./internal/inference/ -run TestSeamGrepGate -count=1` | ok | ✓ PASS |
| Live recommend contract (read-only, post-fix binary) | `./villa recommend --json` | schema 2, reservation 536870912, considered true, fits true, exit 0 | ✓ PASS |
| Live preflight gates (read-only) | `./villa preflight \| grep MEM-PRE` | both rows PASS at the real volume root | ✓ PASS |
| Box state restored | config + `systemctl --user is-active` | backend rocm, memory_enabled true, 4/4 probed units active | ✓ PASS |
| Live doctor healthy path | witnessed by orchestrator (post-fix binary) | overall PASS / exit 0, MEM-DOC-residency BLOCK PASS | ✓ PASS |

### Probe Execution

No `scripts/*/tests/probe-*.sh` probes exist or are declared by this phase — SKIPPED.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| CTRL-01 | 22-01, 22-04 | recommend reserves the embedding-model footprint in the fit math | ✓ SATISFIED | Truths 1, 4-6, 14, 17 |
| CTRL-03 | 22-03, 22-04 | doctor includes memory-stack health + residency-under-load proof | ✓ SATISFIED | Truths 3, 10-13, 16 |
| CTRL-06 | 22-02, 22-04 | preflight gates host fitness for the memory stack | ✓ SATISFIED | Truths 2, 7-9, 15 |

No orphaned requirements: REQUIREMENTS.md maps exactly CTRL-01/03/06 to Phase 22 and all three are claimed by plans.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | none | — | Zero TBD/FIXME/XXX/TODO/HACK/placeholder markers across all 24 phase-modified Go files (the single "TODO" grep hit is a comment asserting "no TODO branch") |

Info-level review items IN-01..IN-07 (e.g. Sprintf URL at doctor.go:435 instead of net.JoinHostPort) were not claimed fixed and remain — consistent with the review's classification; none blocks the goal.

### Human Verification Required

### 1. Operator sign-off on the on-hardware results

**Test:** Review the 22-04 evidence (recommend schema-2 JSON, MEM-PRE rows, doctor healthy PASS + embed-down WARN negative control, D-05 MemoryPeak ~110.9 MiB vs 512 MiB "constant stands" decision, restored box state). Optionally re-run the negative control: `systemctl --user stop villa-embed.service && ./villa doctor; systemctl --user start villa-embed.service`.
**Expected:** All five items accepted; doctor reports MEM-DOC-residency WARN (never PASS) with villa-embed down and does not start the unit.
**Why human:** 22-04 Task 2 was a blocking `checkpoint:human-verify` gate auto-approved under `human_verify_mode: end-of-phase` — the operator never typed "approved". The negative control mutates service state, which this read-only verification must not do (items 1, 2 and 5 were re-confirmed live here; the healthy-path doctor PASS was witnessed post-fix).

### Gaps Summary

No gaps. Every roadmap success criterion and plan must-have is observable in the codebase, all four plans' artifacts are substantive and wired, all 8 post-review fixes are present with regression tests, the three phase-boundary constraints hold (status untouched, seam gate unmodified-and-green, single schema-2 golden re-freeze), and the live post-fix binary reproduces SC#1/SC#2 on the target host with the SC#3 healthy-path PASS directly witnessed. The only outstanding item is the formal operator sign-off that the phase itself deferred to end-of-phase.

---

_Verified: 2026-06-10T17:45:00Z_
_Verifier: Claude (gsd-verifier)_
