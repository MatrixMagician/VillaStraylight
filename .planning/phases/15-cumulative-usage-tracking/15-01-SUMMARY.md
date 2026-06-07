---
phase: 15-cumulative-usage-tracking
plan: 01
subsystem: usage
tags: [usage, fold, xdg-store, atomic-write, counts-only, pure-core]
requires: []
provides:
  - "internal/usage pure core: Fold(prior, sample) UsageTotals + foldCounter"
  - "UsageTotals / ModelUsage / CounterState / Sample counts-only types"
  - "Deps byte-I/O seam (WriteAll/ReadAll/Now)"
  - "UsagePath XDG data-dir resolver + assertInsideDir traversal guard"
  - "WriteFileAtomic temp+rename 0600/0700 writer; Save + fail-closed Load"
  - "independent usageSchemaVersion (NOT golden-frozen)"
affects:
  - "Plan 02 (metrics extension feeds Sample)"
  - "Plan 03 (status.Report reads UsageTotals)"
  - "Plan 04 (dashboard wires live WriteAll=WriteFileAtomic, sole writer)"
tech-stack:
  added: []
  patterns:
    - "pure-core + injectable-seam (cloned from benchstore.Deps)"
    - "reset-aware fold over monotonic counter (non-monotonicity ⇒ whole-sample-new)"
    - "full-file atomic temp+rename (upgrade over benchstore O_APPEND JSONL)"
    - "counts-only structural field-set security test + JSON-key denylist"
    - "fail-closed Load (absent/corrupt/schema-skew ⇒ empty typed-Unknown)"
key-files:
  created:
    - internal/usage/usage.go
    - internal/usage/usage_test.go
  modified: []
decisions:
  - "Tasks 1 & 2 share usage.go; implemented as one cohesive package under TDD (RED commit then GREEN commit) since the test file covers both tasks' behaviors"
  - "JSON tags chosen counts-only and denylist-safe: prompt_tokens / generated_tokens / cumulative / last_seen_raw / last_seen — none contain response/content/text/messages/prompt_text"
  - "Fold returns a copied store (never mutates input) for pure-core safety"
metrics:
  duration: 2 min
  completed: 2026-06-07
---

# Phase 15 Plan 01: Pure Reset-Aware Usage Core + XDG Atomic Store Summary

Created the new pure `internal/usage` core: a reset-aware `Fold(prior, sample) -> UsageTotals` over llama.cpp's monotonic `_total` token counters (per-model, typed-Unknown-safe), plus an injected byte-I/O store seam with a self-contained XDG data-dir resolver, path-traversal guard, and full-file atomic temp+rename persistence that fails closed on absent/corrupt/version-skew input.

## What Was Built

- **`foldCounter`** — reset-aware delta: `sampleRaw >= LastSeenRaw ⇒ delta = sampleRaw - LastSeenRaw` (monotonic growth); else `delta = sampleRaw` (a backward step from a server restart / backend swap is treated as whole-sample-new, never a negative delta). D-04.
- **`Fold`** — locates/creates the per-model entry keyed by `Sample.Model` (D-03), applies `foldCounter` to the prompt and predicted counters ONLY when their `Known` flag is set (D-05 typed-Unknown no-fold, no `LastSeenRaw` mutation), stamps `LastSeen` from the sample capture time when present, and returns a copied store (pure, never mutates input — D-01). An entirely-unknown sample produces no new/changed entry.
- **Counts-only types** — `CounterState{Cumulative, LastSeenRaw uint64}`, `ModelUsage{Model, Prompt, Predicted, LastSeen}`, `UsageTotals{Models map, SchemaVersion}`, `Sample` (raw per-model reading + Known flags + optional capture time). No prompt/response/content fields (D-11).
- **`usageSchemaVersion = 1`** — the usage store's OWN version, documented as independent of `status.Report`'s `reportSchemaVersion` and NOT golden-frozen.
- **Store seam + persistence** — `Deps{WriteAll, ReadAll, Now}`; `UsagePath()` (XDG_DATA_HOME → ~/.local/share/villa → /var/tmp/villa, file `usage.json`); local `assertInsideDir` (cloned from benchstore, not imported, to keep deps narrow); `WriteFileAtomic` (assertInsideDir → MkdirAll 0700 → CreateTemp 0600 → write → Rename → Chmod-tighten, temp cleaned on any error); `Save` (whole-file replace) and fail-closed `Load` (absent ⇒ nil,nil ⇒ empty; corrupt ⇒ empty; schema mismatch ⇒ empty — never a fabricated total, never a panic). D-02, T-15-01/02/04/05.

## Tasks Completed

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| RED  | Failing tests for fold + counts-only store | 9d3d649 | internal/usage/usage_test.go |
| 1+2  | Pure fold + types + XDG atomic store (GREEN) | 696760d | internal/usage/usage.go |

> Tasks 1 and 2 both write `internal/usage/usage.go` and are covered by the single `usage_test.go`. Executed as one TDD cycle: a `test(...)` RED commit (tests fail to compile — undefined symbols) followed by a `feat(...)` GREEN commit (all tests pass). This satisfies both tasks' acceptance criteria and the plan's `type: execute` (Task 1 is `tdd="true"`, Task 2 extends the same file).

## Verification

- `go test ./internal/usage -run 'TestFold|TestUsageTotalsHasNoContentFields'` — 4 passed.
- `go test ./internal/usage` — all 7 tests pass (`TestFoldResetAware`, `TestFoldPerModel`, `TestFoldTypedUnknown`, `TestUsageTotalsHasNoContentFields`, `TestStoreRoundTrip`, `TestLoadFailsClosed`, `TestUsagePathXDG`).
- `go vet ./internal/usage` — clean.
- `go build ./...` — clean (no broader breakage from the new package).
- Acceptance greps: no content JSON tags (`response|content|messages|prompt_text`); no `O_APPEND` (full-file rewrite, not append); all required symbols present (`func Fold(`, `func foldCounter(`, `const usageSchemaVersion`, `func UsagePath(`, `func assertInsideDir(`, `func Load(`, `func Save(`, `os.Rename(`, `os.CreateTemp(`).

## Threat Model Coverage

| Threat ID | Mitigation in this plan |
|-----------|-------------------------|
| T-15-01 (path traversal) | `assertInsideDir(path, dir)` rejects traversal/absolute-escape before any write; local guard, no config import. Proven by `TestUsagePathXDG`. |
| T-15-02 (torn write) | `WriteFileAtomic` = CreateTemp same dir → Rename; temp cleaned on error. |
| T-15-03 (content disclosure) | Counts-only structural field-set test over `UsageTotals`/`ModelUsage`/`CounterState` + JSON-key denylist. `TestUsageTotalsHasNoContentFields`. |
| T-15-04 (file perms) | 0600 file / 0700 dir + `os.Chmod` tighten on the persisted file. |
| T-15-05 (corrupt/skew load) | Fail-closed `Load`: absent/corrupt/unknown-schema ⇒ empty `UsageTotals`, no error, no panic. `TestLoadFailsClosed`. |

## Deviations from Plan

None — plan executed as written. The two tasks were merged into a single TDD RED/GREEN cycle because they share one source file (`internal/usage/usage.go`) and one test file already covering both tasks' behaviors; both tasks' full acceptance criteria are met.

## Known Stubs

None. The package is self-contained and fully tested. Live wiring of `WriteAll = WriteFileAtomic` and the metrics-fed `Sample` are intentionally downstream (Plans 02/04), as designed by the phase decomposition.

## Self-Check: PASSED

- FOUND: internal/usage/usage.go
- FOUND: internal/usage/usage_test.go
- FOUND commit: 9d3d649
- FOUND commit: 696760d
