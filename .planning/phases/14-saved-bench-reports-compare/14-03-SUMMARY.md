---
phase: 14-saved-bench-reports-compare
plan: 03
subsystem: bench
tags: [benchstore, compare, read-only, comparability-guard, golden, exit-mapping, void-flag, go]

# Dependency graph
requires:
  - phase: 14-01
    provides: "internal/benchstore pure core — Load, Comparable, Compare/CompareResult, SavedReport/SavedSide/Fingerprint, Marshal; the read API this plan renders"
  - phase: 14-02
    provides: "liveBenchstoreDeps().ReadAll live XDG read seam over the now-populated store; benchstoreWrite/benchBackendSwap indirections spied for the read-only proof"
  - phase: 09-bench (shipped v1.0)
    provides: "cmd/villa/bench.go newBench cobra surface + exit consts (exitPass/exitWarn/exitBlocked from preflight.go) + renderBench Δpp/Δtg two-line block cloned"
provides:
  - "villa bench --compare — read-only comparability-guarded pp/tg delta render (separate lines) OR honest 'not comparable' refusal (no delta); 0/2/1 exit mapping"
  - "villa bench --list — read-only saved-report enumeration (table or --json)"
  - "runBenchCompare(cmd, list, compare, asJSON, benchstore.Deps) int — read-only path, returns exit code, never measures/writes/swaps"
  - "benchCompareEntry --compare --json contract (frozen) — comparable bool, differing_fields, per-side a/b + a_void_exhausted/b_void_exhausted, delta_prompt_per_sec/delta_predicted_per_sec separate"
  - "selectComparePair — A8 auto-selection of the two most-recent comparable reports for v1"
affects: [dashboard, milestone-v1.2]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Read-only cmd-tier path enforced, not just intended: flag-exclusivity rejects --ab/--ab-target/--reps/--warmup/--n-predict at the cobra boundary; the stub Deps.AppendLine fatals on call and benchBackendSwap is spied (T-14-06)"
    - "Exit mapping is identical in --json mode: a not-comparable pair returns exit 2 even though the comparable:false contract was emitted (no-false-green)"
    - "Void side is an advisory annotation, never a refusal (RESEARCH Q3/A5): a comparable pair with a void side STILL prints the delta and exits 0, but flags that side not-authoritative"
    - "Read-only render split into sibling cmd/villa/bench_compare.go (bench.go was ~820 lines) — flag wiring stays on the bench noun, no behavior change"

key-files:
  created:
    - cmd/villa/bench_compare.go
    - cmd/villa/testdata/bench-compare.json.golden
  modified:
    - cmd/villa/bench.go
    - cmd/villa/bench_test.go

key-decisions:
  - "Single golden file with the two --json outputs concatenated (comparable+void case THEN not-comparable refusal) rather than two golden files — one fixture exercises both the void-flag fields and the refusal shape; documented in TestBenchCompareGolden"
  - "Bare --compare auto-selects the two most-recent comparable reports (A8 v1 scope) via selectComparePair scanning newest-first; no index-pair selector flag, no subcommands (flags on the bench noun per ROADMAP)"
  - "A=older / B=newer in the rendered pair so the delta (B − A) reads as the more-recent run vs the earlier one"
  - "Not-comparable --json still emits the full contract (comparable:false, differing_fields, zeroed deltas) AND returns exit 2 — the JSON consumer gets the refusal shape, the shell gets the exit code"

patterns-established:
  - "benchCompareRun package-level os.Exit indirection (mirrors benchRun) so tests drive runBenchCompare (which RETURNS the code) without exiting"
  - "Read-only proof = stub Deps.AppendLine t.Fatal + benchBackendSwap spy (a write or a swap is a test failure, not just unasserted)"

requirements-completed: [BENCH-04]

# Metrics
duration: 11min
completed: 2026-06-07
---

# Phase 14 Plan 03: BENCH-04 Read-Only `--compare`/`--list` Surface Summary

**`villa bench --compare` (read-only) loads saved reports, runs the pure `benchstore.Compare` comparability guard, and prints per-metric pp/tg deltas on SEPARATE lines — OR, on a non-comparable pair, an honest "not comparable" + differing fields with NO delta; a comparable pair with a void side still prints the delta but flags that side not-authoritative. `villa bench --list` enumerates saved reports. Both are read-only (reject the live-measurement flags at the cobra boundary, never measure/write/swap), map to the 0/2/1 exit contract, and the `--compare --json` machine contract is byte-frozen.**

## Performance

- **Duration:** ~11 min
- **Tasks:** 2 (both TDD)
- **Files:** 2 created, 2 modified

## Accomplishments
- Added the read-only `--compare`/`--list` flags to the `bench` noun (`newBench`) with flag-exclusivity validation at the cobra boundary: `--compare`/`--list` combined with `--ab`/`--ab-target`/changed `--reps`/`--warmup`/`--n-predict` is a usage error, and `--compare && --list` together is rejected — read-only is enforced, not just intended (T-14-06).
- New `cmd/villa/bench_compare.go`: `runBenchCompare` loads saved reports via `benchstore.Load(d)`, auto-selects the two most-recent comparable reports (`selectComparePair`, A8 v1), runs the pure `benchstore.Compare`, and renders the result. It RETURNS the exit code (no `os.Exit`) so tests assert output+code; the `benchCompareRun` package-level indirection mirrors `benchRun` for the live `os.Exit` dispatch.
- `--compare` exit mapping mirrors preflight: comparable delta → `exitPass` (0), not-comparable → `exitWarn` (2), no/insufficient (<2) reports → `exitBlocked` (1) with remediation ("run `villa bench` first"). The mapping is IDENTICAL in `--json` mode — a not-comparable pair returns exit 2 even though the `comparable:false` contract was emitted (no-false-green; T-14-04).
- Comparable pair with a void side (RESEARCH Q3/A5): the delta STILL prints and the exit stays 0, but the void side is flagged `[not authoritative — residency void]` (human) / `a_void_exhausted`/`b_void_exhausted` (JSON). The void flag is advisory — it NEVER suppresses the delta and NEVER changes the exit code; the only refusal path is `!Comparable`.
- pp and tg stay STRUCTURALLY SEPARATE end-to-end: `delta_prompt_per_sec` and `delta_predicted_per_sec` are separate keys, there is no blended tok/s figure — the cloned no-blended grep guards it.
- `--list` enumerates saved reports as a tabwriter table (index, captured_at, model, quant, backend, pp/tg, void) or, with `--json`, the frozen `[]SavedReport` records.
- Froze the `--compare --json` contract in `cmd/villa/testdata/bench-compare.json.golden` — ONE fixture concatenating a comparable pair (side B void, so the void-flag fields are frozen with a `true` present) AND a not-comparable refusal (`comparable:false`, `differing_fields:["model"]`, zeroed deltas). Refreezable via `-update`.

## Task Commits

1. **Task 1: --compare/--list flags + runBenchCompare read-only path + flag-exclusivity + 0/2/1 exit mapping + render** — `bbfa59c` (feat)
2. **Task 2: freeze the --compare --json golden (comparable+void + not-comparable) + no-blended grep** — `a1122ea` (test)

_TDD per task: failing test first (RED — undefined `runBenchCompare` for Task 1; golden-missing + the JSON-mode exit-code gap for Task 2), then minimal implementation to GREEN._

## Files Created/Modified
- `cmd/villa/bench_compare.go` (created) — `runBenchCompare`, `benchCompareRun` indirection, `benchCompareEntry`/`benchCompareSide`/`benchListEntry` JSON shapes, `selectComparePair`, `benchCompareSideOf`, `benchCompareEntryFrom`, `encodeBenchJSON`, `renderBenchList`, `renderCompareNotComparable`, `renderCompareDelta`.
- `cmd/villa/testdata/bench-compare.json.golden` (created) — the byte-frozen `--compare --json` contract (comparable+void AND not-comparable cases).
- `cmd/villa/bench.go` (modified) — added `asCompare`/`asList` flags + the read-only dispatch/validation block at the top of `RunE` (before BenchSpec construction).
- `cmd/villa/bench_test.go` (modified) — `stubBenchstoreReadAll`/`comparableReport` helpers + flag-exclusivity, list, comparable, void-side, not-comparable, no-reports, read-only, golden, and no-blended tests.

## Decisions Made
- **One golden, two cases concatenated** (comparable+void THEN not-comparable) rather than two golden files — a single fixture exercises both the `a_void_exhausted`/`b_void_exhausted` void-flag fields and the refusal shape; documented in `TestBenchCompareGolden`.
- **Bare `--compare` auto-selects the two most-recent comparable reports** (A8 v1 scope) via `selectComparePair` (newest-first scan); no index-pair selector flag, no subcommands — flags stay on the bench noun per ROADMAP.
- **A=older / B=newer** in the rendered pair so the `B − A` delta reads as the more-recent run vs the earlier one.

## Deviations from Plan

**1. [Rule 1 — Bug] Not-comparable `--json` returned exit 0 instead of exit 2**
- **Found during:** Task 2 RED (`TestBenchCompareGolden` caught it: not-comparable `--json` exit = 0, want 2).
- **Issue:** The first `runBenchCompare` `--json` branch did `return encodeBenchJSON(...)`, which returns `exitPass` regardless of comparability — so a not-comparable pair emitted the `comparable:false` contract but exited 0, breaking the 0/2/1 exit contract in JSON mode (a false-green for shell consumers).
- **Fix:** After a successful JSON encode, apply the SAME exit mapping the human path uses — a not-comparable pair returns `exitWarn` (2) even though the contract was emitted. The JSON consumer gets the refusal shape; the shell gets the honest exit code.
- **Files modified:** `cmd/villa/bench_compare.go`
- **Commit:** `a1122ea`

## Authentication Gates
None.

## User Setup Required
None — `--compare`/`--list` read the store populated by prior `villa bench` runs under the user's XDG data dir.

## Known Stubs
None — the read-only surface is fully wired against the live `liveBenchstoreDeps().ReadAll` seam and the Plan-01 `Load`/`Compare` core. No placeholder/empty-data paths.

## Next Phase Readiness
- BENCH-04 is complete — Phase 14 (3/3 plans) closes the saved-bench-reports + compare milestone work.
- On-hardware UAT (gfx1151, end-of-phase): after two `villa bench` runs (vulkan + rocm, same model/quant/host), `villa bench --compare` prints pp/tg deltas and treats the same-model cross-backend pair as comparable (exit 0); a mismatched-model pair prints "not comparable" + the differing field (exit 2); `villa bench --list` enumerates both runs.

## Threat Flags
None — no security surface beyond the plan's `<threat_model>`. T-14-02 (renders only stored numeric timings + spec + fingerprint, no prompt/response content, read-only local), T-14-03 (Plan-01 bounded fail-closed `Load`, `--compare` degrades to remediation not a panic), T-14-04 (comparability guard refuses on mismatched/unknown host — no false-equal, exit 2), and T-14-06 (read-only enforced: no measure/write/swap; flag-exclusivity at the cobra boundary) are all mitigated as planned.

## Self-Check: PASSED

- FOUND: cmd/villa/bench_compare.go (runBenchCompare, benchCompareRun, benchCompareEntry, selectComparePair)
- FOUND: cmd/villa/testdata/bench-compare.json.golden (comparable+void + not-comparable, no blended key)
- FOUND: cmd/villa/bench.go (--compare/--list flags + read-only dispatch in RunE)
- FOUND: cmd/villa/bench_test.go (BenchCompareFlagExclusive/List/Comparable/VoidSide/NotComparable/NoReports/ReadOnly/Golden/NoBlendedKey)
- FOUND commit: bbfa59c (Task 1)
- FOUND commit: a1122ea (Task 2)
- `make check` exits 0 (vet + full `go test ./...`, incl. `TestSeamGrepGate`); `grep -RnE 'internal/(inference|detect)' internal/benchstore/benchstore.go` returns no matches (benchstore stays detect-free).

---
*Phase: 14-saved-bench-reports-compare*
*Completed: 2026-06-07*
