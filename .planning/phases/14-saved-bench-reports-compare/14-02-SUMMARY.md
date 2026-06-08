---
phase: 14-saved-bench-reports-compare
plan: 02
subsystem: bench
tags: [benchstore, persistence, write-hook, xdg, fingerprint, known-guard, loud-non-fatal, go]

# Dependency graph
requires:
  - phase: 14-01
    provides: "internal/benchstore pure core (SavedReport/SavedSpec/SavedSide/SavedAB/Fingerprint, Deps seam AppendLine/ReadAll/Now, Append/Load, Comparable/Compare) — this plan wires its live writer"
  - phase: 09-bench (shipped v1.0)
    provides: "cmd/villa/bench.go runBench + bench.Result (pp/tg separate, VoidExhausted/Reason) the write-hook folds; sideFromStats mapping cloned"
  - phase: 05-detect (shipped)
    provides: "detect.Probe() HostProfile + IGPUGfxID/KernelVersion typed-Optional .Known — the cmd-tier fingerprint source (benchstore never imports detect)"
provides:
  - "liveBenchstoreDeps() — live XDG append seam (O_APPEND|O_CREATE|O_WRONLY 0600 under MkdirAll 0700, traversal-guarded) + ReadAll (absent => nil,nil) + Now"
  - "captureBenchFingerprint(cfg, backend) — cmd-tier .Known-guarded fingerprint (model/quant/ctx from config, host gfx/kernel from detect.Probe)"
  - "savedReportFromResult — bench.Result -> benchstore.SavedReport mapper (mode single/ab, pp/tg separate, AB deltas, void state, fingerprint)"
  - "BENCH-03 persistence write-hook in runBench: persist-always (incl. void runs), loud-but-non-fatal on write error (exit code unchanged)"
affects: [14-03, bench-compare, dashboard]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Host fingerprint captured at cmd tier from detect.Probe() .Known-guarded, passed into benchstore as plain strings (pure core stays detect-free)"
    - "Persistence is loud-but-non-fatal: a write error is a stderr WARN that never changes the measurement's exit code (clone of liveBenchDeps.Restore)"
    - "Package-level benchstoreWrite indirection (test seam) + TestMain no-op default keeps existing runBench tests hermetic against XDG"
    - "Persist-always (A5): the hook fires on BOTH the exitPass and exitWarn paths so a void-exhausted run is still recorded (VoidExhausted=true)"

key-files:
  created: []
  modified:
    - cmd/villa/bench.go
    - cmd/villa/bench_test.go

key-decisions:
  - "Re-resolved the store path (benchReportsStorePath) and traversal guard (benchAssertInsideDir) as LOCAL cmd-tier copies — benchstore.benchReportsPath/assertInsideDir are unexported in the pure core, and importing benchstore solely for a helper would not expose them; the local copies mirror the contract's chain exactly"
  - "For an --ab run the fingerprint backend axis is res.AB.From (the original/from-side); Backend is presentation-only and never a comparability blocker (Plan-01 Comparable excludes it)"
  - "Added a package-level TestMain that no-ops benchstoreWrite for the whole cmd/villa test run, so the pre-existing runBench-driving tests (TestBenchCleanPass etc.) never write to the developer's real ~/.local/share/villa store; the write-hook tests explicitly override benchstoreWrite with a recording/error stub"

patterns-established:
  - "Live benchstore append seam: assert-inside-dir -> MkdirAll 0700 -> O_APPEND|O_CREATE|O_WRONLY 0600 -> Write (never write-whole-file/truncate)"
  - "Fingerprint .Known-guard: unknown host fact -> empty sentinel -> Plan-01 Comparable treats the pair as not-comparable (no false-equal, no fabricated identity)"

requirements-completed: [BENCH-03]

# Metrics
duration: 8min
completed: 2026-06-07
---

# Phase 14 Plan 02: BENCH-03 Persistence Write-Hook Summary

**`villa bench` now persists each run (single or --ab) as ONE JSONL saved report under `$XDG_DATA_HOME/villa/bench-reports.jsonl` after the measurement — pp/tg separate, residency-void state recorded, comparability fingerprint captured at the cmd tier from config + `.Known`-guarded `detect.Probe()` (benchstore stays detect-free), with a write failure that is a loud stderr WARN but never changes the measurement's exit code.**

## Performance

- **Duration:** ~8 min
- **Tasks:** 2 (both TDD)
- **Files modified:** 2 (cmd/villa/bench.go, cmd/villa/bench_test.go)

## Accomplishments
- Wired `liveBenchstoreDeps()` — the live host append seam for the Plan-01 pure core: `AppendLine` asserts-inside-dir → `MkdirAll 0700` → `os.OpenFile(O_APPEND|O_CREATE|O_WRONLY, 0600)` → `Write` the already-`\n`-terminated line (append-only, never a write-whole-file/truncate). `ReadAll` returns `(nil,nil)` on an absent store (no reports ≠ error; wired now for Plan 03). `Now=time.Now`.
- `captureBenchFingerprint(cfg, backend)` captures the comparability key at the cmd tier: model/quant/ctx from config (single source of truth), host gfx id + kernel from `detect.Probe()` `.Known`-guarded — an UNKNOWN host fact serializes to the empty sentinel, NEVER a fabricated value (T-14-04). benchstore receives only plain strings/ints; it imports no detect (`TestSeamGrepGate` green).
- `savedReportFromResult` folds the SAME `bench.Result` data `benchEntryFromResult` maps into the benchstore types: mode single/ab, the reproducible `SavedSpec`, per-side `SavedSide` with pp/tg SEPARATE (clone of `sideFromStats`), the `SavedAB` per-metric deltas, `VoidExhausted`/`Reason`, and the captured `Fingerprint`.
- `runBench` fires the write-hook AFTER the measurement is rendered and BEFORE returning, on BOTH the `exitPass` and `exitWarn` paths — so a void-exhausted run is STILL persisted (persist-always policy A5, `VoidExhausted=true` recorded). An `--ab` run persists as ONE record (`mode=ab`), a single run as `mode=single`.
- The write is loud-but-non-fatal: a benchstore write error is a stderr WARN (`bench: WARNING — failed to persist saved report …`) that NEVER changes the measurement's exit code (T-14-05; cloned from the `liveBenchDeps.Restore` idiom).

## Task Commits

1. **Task 1: liveBenchstoreDeps + captureBenchFingerprint (.Known-guarded) + benchstoreWrite indirection** — `80a7bd3` (feat)
2. **Task 2: fire the BENCH-03 write-hook in runBench (loud-but-non-fatal, persist-always)** — `04e90a8` (feat)

_TDD per task: failing test first (RED — undefined symbols for Task 1; hook-fires-0 for Task 2), then minimal implementation to GREEN._

## Files Created/Modified
- `cmd/villa/bench.go` — added `benchReportsStorePath`/`benchAssertInsideDir` local helpers, `liveBenchstoreDeps()`, `captureBenchFingerprint()`, `benchstoreWrite` package-level indirection, `savedReportFromResult`/`savedSideFromStats` mappers, `persistBenchReport()`; fired the hook in `runBench` after render on both exit paths. Added `path/filepath`, `strings`, and `internal/benchstore` imports.
- `cmd/villa/bench_test.go` — added a package-level `TestMain` (no-ops the write-hook so existing tests stay hermetic), live-append-grows/0600/0700 test, ReadAll round-trip + absent test, data-dir-confinement test, `.Known`-guard fingerprint test, write-hook-fires-once (mode=single), --ab-one-record (mode=ab), write-non-fatal (same exit + stderr WARN), and void-persisted tests. Added `internal/benchstore` and `internal/detect` imports.

## Decisions Made
- **Local cmd-tier path + guard copies:** `benchstore.benchReportsPath`/`assertInsideDir` are unexported in the pure core, so the live wiring re-resolves the store path via the identical `$XDG_DATA_HOME/villa/bench-reports.jsonl` → `~/.local/share` → `/var/tmp` chain and uses a local `benchAssertInsideDir` mirroring the contract's guard — keeping the cmd file from depending on config solely for a helper (plan-sanctioned: "re-use the local one … do not import internal/config solely for assertInsideDir").
- **--ab fingerprint backend axis = `res.AB.From`** (the original/from-side); Backend is recorded for presentation but is deliberately not a comparability blocker.
- **`TestMain` no-op default for `benchstoreWrite`** keeps the pre-existing `runBench`-driving tests (which do not stub the hook) from writing to the developer's real XDG store; the new write-hook tests explicitly override the indirection with a recording/error stub.

## Deviations from Plan
**1. [Rule 1 — Test correctness] Fingerprint test made hardware-agnostic**
- **Found during:** Task 1 GREEN.
- **Issue:** The plan's `<behavior>` framed the fingerprint test as "off-hardware, IGPUGfxID.Known==false → HostGfxID empty". This dev box IS gfx1151, so `detect.Probe()` returns a Known gfx id and the literal-empty assertion failed — the capture was actually correct (`.Known` honored, real value carried).
- **Fix:** Rewrote `TestBenchFingerprintHonorsKnownGuard` to assert the captured field EXACTLY mirrors the `.Known` guard over the same `Probe()` (Known → value; Unknown → ""), plus an explicit "Unknown → empty sentinel, never fabricated" branch. This proves the same T-14-04 invariant on BOTH hardware and off-hardware — the no-fabricated-identity contract, not a host-specific value.
- **Files modified:** `cmd/villa/bench_test.go`
- **Commit:** `80a7bd3`

## Authentication Gates
None.

## User Setup Required
None — the store is created on first `villa bench` run under the user's XDG data dir.

## Known Stubs
None — the live writer and fingerprint capture are fully wired; no placeholder/empty-data paths remain. `ReadAll` is wired now (returns real store bytes) for Plan 03's `--compare`/`--list` consumers.

## Next Phase Readiness
- **Plan 03** adds the read-only `--compare`/`--list` flags over the now-populated store, using the `liveBenchstoreDeps().ReadAll` seam wired here + the Plan-01 `Load`/`Compare` core, with the 0/2/1 exit mapping and the `--compare --json` golden.
- On-hardware UAT (gfx1151): run `villa bench` and confirm `~/.local/share/villa/bench-reports.jsonl` gains one 0600 line with pp/tg separate + the captured `host_gfx_id: gfx1151`.

## Threat Flags
None — no security surface beyond the plan's `<threat_model>`. T-14-01 (append confined to the XDG villa dir via assert-inside-dir + 0700/0600), T-14-04 (`.Known`-guarded fingerprint, no fabricated host identity), and T-14-05 (loud-but-non-fatal write) are all mitigated as planned.

## Self-Check: PASSED

- FOUND: cmd/villa/bench.go (liveBenchstoreDeps, captureBenchFingerprint, savedReportFromResult, persistBenchReport, benchstoreWrite)
- FOUND: cmd/villa/bench_test.go (BenchstoreWrite*/BenchFingerprint*/BenchWriteHook*/BenchPersist*/BenchVoidPersist/BenchWriteNonFatal)
- FOUND commit: 80a7bd3 (Task 1)
- FOUND commit: 04e90a8 (Task 2)
- `make check` exits 0 (vet + full `go test ./...`, incl. `TestSeamGrepGate` green); `grep -RnE 'internal/(inference|detect)' internal/benchstore/benchstore.go` returns no matches (benchstore stays detect-free).

---
*Phase: 14-saved-bench-reports-compare*
*Completed: 2026-06-07*
