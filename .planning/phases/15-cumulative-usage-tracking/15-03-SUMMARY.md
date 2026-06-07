---
phase: 15-cumulative-usage-tracking
plan: 03
subsystem: status
tags: [usage, status-report, byte-frozen-contract, schema-bump, read-only-seam, omitempty]
requires:
  - "internal/usage core (Plan 15-01): UsageTotals/ModelUsage/CounterState, Load, UsagePath"
provides:
  - "status.Report.Usage *usage.UsageTotals omitempty field (the USAGE-02 --json surface)"
  - "reportSchemaVersion bumped 1 -> 2 (the phase's ONE byte-frozen contract evolution)"
  - "status.Deps.ReadUsage read-only seam (nil/empty => field omitted)"
  - "cmd/villa liveReadUsage (usage.Load over UsagePath; NEVER writes)"
  - "re-frozen cmd/villa/testdata/status.json.golden (schema_version 2)"
affects:
  - "Plan 04 (dashboard reads the SAME Report.Usage field via handleStatus, no new endpoint — D-10)"
tech-stack:
  added: []
  patterns:
    - "additive tail-append Report field above SchemaVersion (GenTokensPerSec/ROCmReadiness precedent)"
    - "read-only Deps seam (usage.Load only; CLI never writes — D-07 sole-writer)"
    - "pointer+omitempty typed-Unknown (absent store => omitted key, never a fabricated 0)"
    - "surgical single-golden re-freeze + git-status gate (Pitfall 4)"
key-files:
  created: []
  modified:
    - internal/status/status.go
    - internal/status/status_test.go
    - cmd/villa/status.go
    - cmd/villa/testdata/status.json.golden
decisions:
  - "liveReadUsage returns nil when totals.Models is empty (not just when the file is absent) so an empty-but-present store also omits the key — matches D-09 typed-Unknown intent"
  - "Golden re-freeze yields ONLY schema_version 1->2: the golden harness (newStatusDeps) sets no ReadUsage seam, so omitempty omits the usage key exactly as the plan predicted"
metrics:
  duration: 6 min
  completed: 2026-06-07
---

# Phase 15 Plan 03: Read-Only Cumulative Usage on status.Report (schema v2) Summary

Evolved the byte-frozen `status.Report` contract by EXACTLY ONE append-only field — a `*usage.UsageTotals` (`omitempty`) inserted immediately above `SchemaVersion` — bumped `reportSchemaVersion` 1->2, wired a READ-ONLY `ReadUsage` Deps seam (`villa status` loads `usage.json` via `usage.Load` and never writes it, D-07), and re-froze the single `status.json.golden` once (only `schema_version` 1->2 changed).

## What Was Built

- **`status.Report.Usage *usage.UsageTotals` (`json:"usage,omitempty"`)** — tail-appended immediately above `SchemaVersion`, mirroring the `GenTokensPerSec`/`ROCmReadiness` precedent. Pointer+omitempty so an absent/empty store OMITS the key entirely (typed-Unknown, never a fabricated 0 — D-09).
- **`reportSchemaVersion = 2`** — exactly one increment; doc comment updated to note the Phase-15 usage field.
- **`status.Deps.ReadUsage func() *usage.UsageTotals`** — read-only seam beside `ROCmReadiness`. `Run` populates `report.Usage = d.ReadUsage()` only when the seam is non-nil; a nil seam OR a nil result leaves `Usage` nil (omitted).
- **`cmd/villa.liveReadUsage`** — wires a `usage.Deps` whose `ReadAll` reads `usage.UsagePath()` via `os.ReadFile` ((nil,nil) on `os.IsNotExist`), supplies NO `WriteAll` seam, and calls `usage.Load`. Returns a `*usage.UsageTotals` only when `Models` is non-empty; absent/empty/unreadable => nil (omitted). It can never write `usage.json` (D-07; the dashboard, Plan 04, is the sole writer).
- **Human-table render branch** — when `r.Usage != nil`, prints per-model `usage <model>  prompt N / generated N (cumulative)` rows; omitted entirely when nil.
- **Tests** — `TestUsageOmittedWhenAbsent` (nil seam AND nil-returning seam => Report.Usage nil AND no `"usage"` key in marshaled JSON; schema still 2) and `TestUsageSurfacedWhenPresent` (populated seam => Usage surfaced AND `"usage"` key + `"schema_version":2` in JSON).
- **Re-frozen golden** — `cmd/villa/testdata/status.json.golden`, single line diff `schema_version` 1->2.

## Tasks Completed

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Append the ONE usage field, bump schema, wire the read-only seam | 2e9eba1 | internal/status/status.go, internal/status/status_test.go, cmd/villa/status.go |
| 2 | Re-freeze the single status.json golden once | f7e4aa5 | cmd/villa/testdata/status.json.golden |

## Verification

- `go test ./internal/status -run 'TestUsage'` — 4 passed (omitted-when-absent x2, surfaced-when-present, helper).
- `go test ./internal/status` — 23 passed.
- `go test ./cmd/villa -run TestStatusJSONGolden` — green after re-freeze.
- `git status --short cmd/villa/testdata/` — ONLY `status.json.golden` modified (Pitfall 4 gate passed; no other golden drifted).
- `git diff cmd/villa/testdata/status.json.golden` — single line: `schema_version` 1->2; no key reordering, no usage key (omitted per omitempty, as predicted).
- `go test ./internal/inference -run TestSeamGrepGate` — passed (no backend literal leaked).
- `go build ./...` — clean. `go test ./...` — 657 passed across 19 packages.
- Write-gate: no `usage.Save` / `WriteFileAtomic` / write-seam call in `cmd/villa/status.go` (the only "WriteAll" occurrence is a doc comment stating none is wired).

## Threat Model Coverage

| Threat ID | Mitigation in this plan |
|-----------|-------------------------|
| T-15-09 (CLI writes usage.json) | `liveReadUsage` wires only `ReadAll`; no `WriteAll` seam; `usage.Load` only. No write call anywhere in the status path (D-07). |
| T-15-10 (fabricated 0 total) | pointer+omitempty: absent/empty store => field omitted = typed-Unknown. Covered by `TestUsageOmittedWhenAbsent`. |
| T-15-11 (--json contract drift via -update) | Surgical single-golden re-freeze; git-status gate confirmed ONLY `status.json.golden` changed; diff is one line. |
| T-15-12 (corrupt usage.json crashes status) | `usage.Load` fails closed to empty (Plan 01); `liveReadUsage` returns nil on read error => field omitted; status never panics. |

## Deviations from Plan

None — plan executed as written. One clarifying implementation choice (documented in frontmatter decisions): `liveReadUsage` returns nil when `totals.Models` is empty, not only when the file is absent, so an empty-but-present store also omits the key — consistent with the D-09 typed-Unknown intent and the plan's "nil when empty so the field omits" instruction.

## Known Stubs

None. The CLI status path reads the live store read-only; the live writer (dashboard) is intentionally downstream (Plan 04) per the phase decomposition. `Report.Usage` is fully wired end-to-end (field + seam + live load + render + golden).

## Self-Check: PASSED

- FOUND: internal/status/status.go
- FOUND: internal/status/status_test.go
- FOUND: cmd/villa/status.go
- FOUND: cmd/villa/testdata/status.json.golden
- FOUND commit: 2e9eba1
- FOUND commit: f7e4aa5
- reportSchemaVersion = 2 (confirmed); golden "schema_version": 2 (confirmed)
