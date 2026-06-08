---
phase: 14-saved-bench-reports-compare
plan: 01
subsystem: bench
tags: [benchstore, jsonl, schema-version, golden, comparability, pure-core, xdg, go]

# Dependency graph
requires:
  - phase: 09-bench (shipped v1.0)
    provides: "internal/bench Stats/ABResult pp-tg-separate shapes + cmd/villa benchSide/benchAB JSON tags this record persists"
  - phase: 05-detect / 07 (shipped)
    provides: "detect.HostProfile schema_version-as-last-field append-only discipline + IGPUGfxID/KernelVersion typed-Optional fingerprint source (read at cmd tier, NOT imported here)"
provides:
  - "internal/benchstore pure core owning the on-disk saved-bench-report contract"
  - "SavedReport JSONL record (savedReportSchemaVersion=1) frozen byte-for-byte BEFORE any live writer"
  - "Comparability guard (Comparable/Compare) — model+quant+ctx+host match, backend may differ, unknown-host => not comparable"
  - "Deps seam (AppendLine/ReadAll/Now) + Append/Load fail-closed-per-line JSONL parse"
  - "benchReportsPath XDG resolver + local assertInsideDir traversal guard + 0600/0700 store modes"
affects: [14-02, 14-03, bench-compare, dashboard]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "On-disk JSONL contract frozen via own golden BEFORE first writer (schema_version=1, last field, append-only)"
    - "pp/tg structural separation end-to-end — no blended tok/s key anywhere"
    - "Pure core + injected Deps byte-I/O seam (clone of internal/bench), importing neither inference nor detect"
    - "Comparability refuses (no false-equal) on unknown host; backend deliberately not a blocker"

key-files:
  created:
    - internal/benchstore/benchstore.go
    - internal/benchstore/benchstore_test.go
    - internal/benchstore/testdata/record.golden
  modified: []

key-decisions:
  - "PERSIST the fixed benchPrompt reproducibility constant in SavedSpec.Prompt — the saved record is a SUPERSET of `bench --json` (which omits prompt) so a run can be reproduced from the store; the value is an in-repo constant, never user content (T-14-02 honoured)"
  - "Fold not-comparable into CompareResult.Comparable=false (single result type) rather than a separate NotComparable return; differing fields carried, deltas ZERO on refusal"
  - "Compare reads the primary measured side: Single for single-mode, AB.B for ab-mode"
  - "ctx IS a comparability blocker (A2); kernel_version recorded but secondary (not a blocker)"

patterns-established:
  - "savedReportSchemaVersion=1 as the LAST struct field, new fields append above, golden-frozen before any writer"
  - "Local assertInsideDir copy (config's is unexported) keeps the pure core dependency-narrow"

requirements-completed: [BENCH-03, BENCH-04]

# Metrics
duration: 4min
completed: 2026-06-07
---

# Phase 14 Plan 01: Saved Bench Reports Contract Summary

**Pure `internal/benchstore` core: schema_version=1 SavedReport JSONL record frozen byte-for-byte before any live writer (pp/tg separate, residency-void recorded) plus a comparability guard that compares cross-backend but refuses on unknown host — no false-equal.**

## Performance

- **Duration:** 4 min
- **Started:** 2026-06-07T16:31:55Z
- **Completed:** 2026-06-07T16:36:00Z
- **Tasks:** 2 (both TDD)
- **Files modified:** 3 created

## Accomplishments
- Froze the on-disk saved-bench-report contract (`savedReportSchemaVersion=1`, `schema_version` last field) via its OWN byte-for-byte golden BEFORE any live writer exists — the format can never silently drift into a migration (BENCH-03).
- pp and tg persist as SEPARATE record fields (`prompt_per_sec`/`predicted_per_sec` + stddevs) and the delta is per-metric — a cloned no-blended grep proves no `tok_per_sec`/`tokens_per_sec`/`throughput` key anywhere.
- `VoidExhausted`/`Reason` residency-void state round-trips write→read without loss.
- Comparability guard: two reports compare iff model+quant+ctx+host match; backend may differ (cross-backend compare is the point); an UNKNOWN host (`HostGfxID==""`) is never comparable, even against an identical-empty one (BENCH-04, no false-equal).
- `Deps` seam (AppendLine/ReadAll/Now) with `Append` (stamps version + CapturedAt) and `Load` (bounded scanner, fail-closed per JSONL line — corrupt line skipped, earlier records survive, absent store => empty).
- XDG path resolver + local traversal guard + 0600/0700 store modes ship with the contract; package imports NEITHER inference NOR detect (`TestSeamGrepGate` green).

## Task Commits

Each task was committed atomically (TDD: RED proven before GREEN within the same logical commit):

1. **Task 1: SavedReport contract + schema_version=1 + JSONL marshal + record golden** - `9269742` (feat)
2. **Task 2: Comparable/Compare guard + Deps seam + Append/Load + path helper** - `f1ab8b9` (feat)

**Plan metadata:** committed separately with STATE.md/ROADMAP.md tracking.

_TDD flow per task: failing test written first (RED confirmed — golden-missing fail for Task 1, undefined-symbol fail for Task 2), then minimal implementation to GREEN._

## Files Created/Modified
- `internal/benchstore/benchstore.go` - SavedReport/SavedSpec/SavedSide/SavedAB/Fingerprint types, `savedReportSchemaVersion`, Marshal, Comparable, Compare/CompareResult, Deps seam, Append/Load, benchReportsPath, assertInsideDir, 0600/0700 consts.
- `internal/benchstore/benchstore_test.go` - schema-version assert, void round-trip, no-blended grep, schema-version-last, record-golden freeze, comparable matrix, unknown-host, per-metric delta, append-grows-via-seam, load-skips-corrupt-line, XDG path + traversal-guard tests.
- `internal/benchstore/testdata/record.golden` - the ONE frozen schema-1 JSONL record (the on-disk contract).

## Decisions Made
- **Persist the fixed `benchPrompt` constant** in `SavedSpec.Prompt` (resolved permanently in the frozen golden): the saved record is a deliberate SUPERSET of `bench --json` (which has no `prompt` key) so a run is reproducible from the store. The value is the in-repo reproducibility constant, never user/response content (T-14-02 satisfied; 0600 keeps it owner-only at write time).
- **Single result type for refusal:** folded "not comparable" into `CompareResult.Comparable=false` (carrying `DifferingFields`, zero deltas) instead of a separate `NotComparable` return type — simpler one-type contract for the cmd tier.
- **Compare reads the primary measured side:** `Single` for single-mode reports, `AB.B` for ab-mode.
- **ctx is a comparability blocker** (A2); `kernel_version` is recorded but secondary (not a blocker).

## Deviations from Plan

None - plan executed exactly as written. (The plan offered a NotComparable-vs-folded choice and a Compare-side choice; both were resolved as documented above — explicit plan-sanctioned decisions, not deviations.)

## Issues Encountered
- The acceptance grep `grep -RnE 'internal/(inference|detect)'` initially matched a doc-comment that mentioned the package names in prose (not an import). Reworded the comment to "the inference seam"/"the detect probe" so the literal grep is clean while the import block stays stdlib-only. Genuine import purity was never violated.
- Task-2 stdlib imports (`bufio`/`bytes`/`path/filepath`/`strings`/`time`) were briefly present in the Task-1 source and flagged unused; trimmed to the Task-1 set, then restored in Task-2 — clean RED→GREEN per task.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- The on-disk record contract is locked. **Plan 02** wires the live `liveBenchstoreDeps()` writer at the cmd tier (`O_APPEND|O_CREATE|O_WRONLY 0600` under `MkdirAll 0700`, traversal-guarded) into `cmd/villa/bench.go:runBench` (loud-but-non-fatal on write error) and adds `--compare`/`--list` read-only flags with the 0/2/1 exit mapping + the `--compare --json` golden.
- `benchReportsPath`/`assertInsideDir`/`storeFileMode`/`storeDirMode` are present here for Plan 02 to call via the live Deps. Note: `storeFileMode`/`storeDirMode` are currently referenced only by the contract (the live writer in Plan 02 enforces them) — if a stricter `unused` lint is enabled before Plan 02 lands, the live wiring closes that.

## Threat Flags

None - no security surface introduced beyond the plan's `<threat_model>`. The store write path (T-14-01) ships its guard + modes here; the live writer (Plan 02) enforces them. JSONL parse (T-14-03) is bounded + fail-closed-per-line. Comparability (T-14-04) refuses on unknown host.

## Self-Check: PASSED

- FOUND: internal/benchstore/benchstore.go
- FOUND: internal/benchstore/benchstore_test.go
- FOUND: internal/benchstore/testdata/record.golden
- FOUND commit: 9269742 (Task 1)
- FOUND commit: f1ab8b9 (Task 2)
- `go test ./internal/benchstore/... -count=1` green; `go test ./internal/inference -run SeamGrepGate` green; `go vet ./internal/benchstore/...` clean; `go build ./...` clean.

---
*Phase: 14-saved-bench-reports-compare*
*Completed: 2026-06-07*
