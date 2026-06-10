---
phase: 22-control-plane-fit-host-gate
plan: 02
subsystem: preflight
tags: [go, preflight, memory, host-gate, podman, statfs]

# Dependency graph
requires:
  - phase: 22-control-plane-fit-host-gate
    plan: 01
    provides: "memory.ConservativeFootprintBytes() single-source conservative default (D-02) consumed as the headroom fallback floor"
  - phase: 18-memory-spine
    provides: "internal/memory Footprint (typed-Unknown, pinned 512 MiB) + VillaConfig memory_* fields"
provides:
  - "preflight.RunMemory exported runner: MEM-PRE-disk (vector-index disk at the rootless podman volume root) + MEM-PRE-headroom (free memory vs embedding reservation), stable order, both TierBlock (CTRL-06/D-06/D-07)"
  - "volumeRootFn seam + liveVolumeRoot podman resolver built on the package runTool seam (zero direct exec.Command — seam grep-gate green by construction)"
  - "minVectorDiskFloorBytes 1 GiB named const with sizing rationale"
  - "cmd-tier gated emission: memoryGateResults seam var in the preflight verb, nil-safe runMemoryChecks installDeps seam in the install path — memory-off output byte-identical"
affects: [22-03-doctor, 22-04, 23-surfacing-backup-swap]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Opt-in check emission as a caller decision: the pure core exports a separate runner (RunMemory); whether it is EMITTED is gated fail-soft in the cmd tier — Run/RunWithResources and the frozen PRE-01..07 sequence untouched"
    - "Host-tool path resolution through the bounded runTool seam: tool output used ONLY as a statfs path, never executed or interpolated (T-22-05)"

key-files:
  created:
    - internal/preflight/checks_memory.go
    - internal/preflight/checks_memory_test.go
  modified:
    - cmd/villa/preflight.go
    - cmd/villa/install.go
    - cmd/villa/preflight_test.go
    - cmd/villa/install_test.go

key-decisions:
  - "liveVolumeRoot routes podman through the existing runTool seam (fixed args, 8 KiB stdout cap) — TestSeamGrepGate passes by construction with ZERO isSeam allowlist changes"
  - "install gets a nil-safe runMemoryChecks installDeps seam (doctor RunROCmImage optional-seam pattern) instead of a direct RunMemory call inside runInstall, so memory-enabled install tests stay hermetic off-hardware"
  - "preflight verb uses an injectable memoryGateResults package var (pullFn convention) — the RunE structure is unchanged and the gate is testable without spawning a subprocess"

patterns-established:
  - "MEM-PRE-* topic-prefixed check IDs for the opt-in memory subsystem (ROCM-PRE-* precedent); frozen PRE-01..07 never renumbered"

requirements-completed: [CTRL-06]

# Metrics
duration: 10min
completed: 2026-06-10
---

# Phase 22 Plan 02: Preflight Memory Host Gate Summary

**`villa preflight` and `villa install` now gate host fitness for the memory stack — vector-index disk at the live-resolved rootless podman volume root + embedder memory headroom — emitted only when memory_enabled, BLOCK on confident shortage, typed-Unknown WARN on unevaluable probes, byte-identical off path**

## Performance

- **Duration:** ~10 min
- **Started:** 2026-06-10T16:20:06Z
- **Completed:** 2026-06-10T16:29:53Z
- **Tasks:** 2 (Task 1 TDD: RED + GREEN commits)
- **Files modified:** 6

## Accomplishments

- D-06/D-07 pure core landed: `RunMemory` returns `[MEM-PRE-disk, MEM-PRE-headroom]` in stable order, both TierBlock; confident shortage = FAIL with refuse-with-remediation naming the resolved path and that the Qdrant vector index grows with indexed chats/documents; unevaluable probe (podman missing / statfs error / Unknown MemAvailable) = typed-Unknown WARN — never a false hard block, never a false-green
- T-22-05 closed by construction: `liveVolumeRoot` invokes `podman system info --format {{.Store.VolumePath}}` ONLY through the package's bounded fixed-arg `runTool` seam; `grep -c "exec.Command" checks_memory.go` = 0; `TestSeamGrepGate` green with `internal/inference/seam_test.go` provably unmodified
- D-02 single source honored: an unrecognized embedding model evaluates headroom against `memory.ConservativeFootprintBytes()` (never a re-typed `512<<20`, never a zero floor) — pinned by a below/at-floor FAIL/PASS pair in the table tests
- D-06 cmd-tier emission: the preflight verb appends the gates on both the ROCm and standard branches via the fail-soft `memoryGateResults` seam (load error/absent config → memory off, no new error path or exit-code change, T-22-08); install appends via the nil-safe `runMemoryChecks` seam through the single `gateInstall` — an opted-in unfit host refuses-with-remediation with ZERO host mutation before the memory stack comes up (proven by test: no write/start/pull/save, no embed pre-stage, no memory proof)
- Off path byte-identical: all frozen preflight goldens untouched (`git status --porcelain cmd/villa/testdata/` empty), `make check` green (420 tests across the two packages), isolated-XDG CLI run emits zero "MEM-PRE" substrings
- Live-verified on the gfx1151 dev host (real opted-in config): volume root resolved to `~/.local/share/containers/storage/volumes` (never hardcoded), both gates PASS (`free disk 469.22 GiB ≥ 1.00 GiB`, `free memory 76.67 GiB ≥ embedding reservation 0.50 GiB`), exit 0

## Task Commits

Each task was committed atomically:

1. **Task 1: checks_memory.go pure checks behind injected seams (TDD)**
   - RED: `6a99eee` (test) — failing table tests for every FAIL/WARN/PASS branch, conservative floor, default 1 GiB binding, non-empty remediation
   - GREEN: `b26200a` (feat) — RunMemory + both checks, liveVolumeRoot via runTool, minVectorDiskFloorBytes
2. **Task 2: Gated emission — preflight verb + install path** - `a464706` (feat)

## Files Created/Modified

- `internal/preflight/checks_memory.go` - NEW: `MemoryGateInput`, `RunMemory`, `checkVectorDisk`/`checkEmbedHeadroom`, `volumeRootFn` seam + `liveVolumeRoot` (runTool-backed), `minVectorDiskFloorBytes`
- `internal/preflight/checks_memory_test.go` - NEW: table tests over injected fakes for all branches incl. the D-02 conservative-default floor and remediation non-emptiness
- `cmd/villa/preflight.go` - `memoryGateResults` injectable seam + fail-soft `liveMemoryGateResults`; one-line gated append in RunE (renderPreflight + exit constants unchanged)
- `cmd/villa/install.go` - nil-safe `runMemoryChecks` installDeps seam, gated append after `runChecks`, live wiring threading `liveLoadedConfig().EmbeddingModel`
- `cmd/villa/preflight_test.go` - rendered MEM-PRE rows with a fake memory-on gate; live gate off-path nil (absent + persisted-off config) and on-path [disk, headroom] under isolated XDG_CONFIG_HOME
- `cmd/villa/install_test.go` - opted-in unfit host refuses (exitBlocked) before any mutation; memory-off install never invokes the gate

## Decisions Made

- Install uses a nil-safe injectable `runMemoryChecks` seam rather than calling `preflight.RunMemory` directly inside `runInstall` — existing memory-enabled install tests (which stub `loadedMemoryEnabled` true) stay hermetic off-hardware; live wiring binds the real runner (mirrors the doctor `RunROCmImage` optional-seam pattern)
- The preflight verb's gate is a package-level seam var (`pullFn` convention) instead of restructuring the os.Exit-bound RunE — the plan's sanctioned fallback for a non-seam-testable verb
- Headroom PASS/FAIL provenance joins the MemAvailable source with the footprint source (pinned vs conservative-fallback), so `-v` shows WHICH floor was applied

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Plan 22-03 (doctor) can fold these checks verbatim via `findingFromCheck` over `RunMemory` results — the runner signature (`detect.HostProfile` + `MemoryGateInput`) was designed for that composition
- The install gate now refuses an unfit host before the memory stack starts; the residency-under-embed-load proof (CTRL-03) remains for Plan 22-03
- Phase-close on-hardware UAT: both gates already verified PASS live on gfx1151 during execution; the FAIL/WARN paths are table-test-pinned

## Self-Check: PASSED

All created/modified files exist on disk; commits 6a99eee, b26200a, a464706 verified in git log.

---
*Phase: 22-control-plane-fit-host-gate*
*Completed: 2026-06-10*
