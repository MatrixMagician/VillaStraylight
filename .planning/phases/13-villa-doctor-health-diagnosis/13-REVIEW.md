---
phase: 13-villa-doctor-health-diagnosis
reviewed: 2026-06-07T00:00:00Z
depth: deep
files_reviewed: 5
files_reviewed_list:
  - internal/doctor/doctor.go
  - internal/doctor/doctor_test.go
  - cmd/villa/doctor.go
  - cmd/villa/doctor_test.go
  - cmd/villa/root.go
findings:
  critical: 1
  warning: 3
  info: 2
  total: 6
status: resolved
resolution_commit: e9d4002
---

# Phase 13: Code Review Report

**Reviewed:** 2026-06-07
**Depth:** deep
**Files Reviewed:** 5
**Status:** resolved (fixes in `e9d4002`)

## Resolution (2026-06-07, commit `e9d4002`)

- **CR-01 (BLOCKER) — RESOLVED.** `healthFinding(HealthDown)` now folds to a WARN
  (status+tier), not a `statusFail`. A down/stopped stack exits 2 (warning), not 1
  (blocking fault); the blocking tier is reserved for silent-degradation faults
  (offload FAIL over health-200, preflight BLOCK, loopback breach). Restores the
  FAIL ⟺ BLOCK-class invariant. Added `TestDownStackWarnsNotBlocks` regression guard.
- **WR-01 — RESOLVED.** `liveDoctorDeps`' `DriftPlan` now stats the unit dir first; an
  absent dir (never installed) returns a read error so the core degrades to the honest
  "units not yet written" typed-Unknown WARN instead of a false "units no longer match".
- **WR-02 / IN-01 — RESOLVED.** `renderDoctor` now maps the exit code from
  `report.Overall` (the core's single worst-wins verdict); the exit code can no longer
  diverge from the JSON `overall` field, and the "BLOCK-class FAILs" comment is now accurate.
- **WR-03 (dead `Deps.LoadConfig` seam) — ACCEPTED as-is.** Documented as a reserved
  forward seam ("read only if a future finding needs config directly"); intentional, not dead.
- **IN-02 (table interpolation) — ACCEPTED as-is.** Safe today: all `Detail`/`Raw` values
  are in-repo constants and `Raw` is `json:"-"` (never in `--json`). Note retained for if
  `Detail` ever becomes host-sourced.

## Summary

`villa doctor` is a well-structured composition of shipped cores (preflight, status,
orchestrate.Reconcile) behind a pure `internal/doctor` core with injected `Deps`. The
build is green, all 11 doctor tests pass, the seam grep-gate passes (no backend marker
literals leak into `cmd/villa` or `internal/doctor`), the read-only invariant holds
(`unitDirReadOnly` is a no-create resolver; no `WriteUnits`/`MkdirAll`), the exit
constants are the authoritative `exitPass=0`/`exitWarn=2`/`exitBlocked=1` (not the
inverted prose), and doctor owns its own `schema_version: 1` golden rather than
extending `status.Report`. No command-injection, path-traversal, or info-leak surfaces
were found — exec is fully delegated to vetted cores using fixed-arg invocations.

However, deep cross-file tracing surfaced one BLOCKER: a **stopped stack is reported as a
blocking FAIL (exit 1)** in direct contradiction of the module's own documented severity
contract (a down stack should be WARN / exit 2), because the exit classifier folds on
`Status` alone and ignores the `Tier` it claims to honor. This is exactly the
"false-classification" risk the phase set out to prevent, just in the opposite direction
(over-blocking rather than false-green), and it is untested. Three warnings concern a
misleading drift message on a configured-but-not-installed host, the Tier/exit
divergence's blast radius, and a dead `Deps.LoadConfig` field.

## Critical Issues

### CR-01: A stopped (down) stack is reported as a BLOCKING FAULT (exit 1), contradicting the documented WARN contract

**File:** `internal/doctor/doctor.go:253-255`, `internal/doctor/doctor.go:203-215`, `cmd/villa/doctor.go:92-106`

**Issue:** The package doc is explicit (lines 22-26) that the WARN tier (exit 2) is for
"a WARN (preflight WARN, config-vs-disk drift, a typed-Unknown / unevaluable signal,
**a down stack**)". But:

1. `healthFinding` maps `HealthDown` to `Status: statusFail` (with `Tier: tierWarn`).
2. The worst-wins fold in `Aggregate` ranks **by `Status` only** (`statusRank`, FAIL=2),
   so a down service forces `Report.Overall = "FAIL"`.
3. `renderDoctor` classifies the exit code by `f.Status` **only** — `case "FAIL": blockFails++`
   — and never consults `f.Tier`, despite the comment claiming it collects "the BLOCK-class
   FAILs" (`cmd/villa/doctor.go:89-90`).

Net effect: a user who simply ran `villa down` (a normal, recoverable operational state)
gets `Overall=FAIL` and exit code **1** with the message "FAULT: N blocking finding(s) —
the running install is not healthy." The finding's own remediation even says "run `villa
up` **if the stack is stopped**", confirming this is an expected state, not a fault. This
conflates "user stopped the stack" with "GPU offload is broken" — the precise
mis-classification this phase aimed to avoid, inverted. No test exercises `HealthDown`, so
it slipped through (`doctor_test.go` only covers offload-FAIL, drift-WARN, and read-error).

The root cause is that the exit classifier and the worst-wins fold both ignore `Tier`.
A `tierWarn` finding must never be able to reach the blocked tier regardless of its
`Status`.

**Fix:** Make the exit classifier (and ideally the `Aggregate` fold) tier-aware so only
`tierBlock` FAILs reach `exitBlocked`; a `tierWarn` FAIL (a down stack) caps at `exitWarn`.

```go
// cmd/villa/doctor.go — renderDoctor
var blockFails, warnTierFails int
anyWarn := false
for _, f := range r.Findings {
    switch f.Status {
    case "FAIL":
        if f.Tier == "BLOCK" {
            blockFails++
        } else {
            warnTierFails++ // e.g. a down stack — recoverable, not blocking
        }
    case "WARN":
        anyWarn = true
    }
}
if blockFails > 0 {
    fmt.Fprintf(w, "\nFAULT: %d blocking finding(s) ...\n", blockFails)
    return exitBlocked
}
if anyWarn || warnTierFails > 0 {
    return exitWarn
}
return exitPass
```

Apply the same tier-awareness to `Aggregate`'s worst-wins fold so `Report.Overall`
(the `--json` contract) and the exit code stay consistent — otherwise JSON reports
`overall: "FAIL"` while the exit code (after the fix) is 2. Add a `TestHealthDownIsWarn`
case asserting a `HealthDown` service yields `Overall=="WARN"` and `exitWarn`.

## Warnings

### WR-01: Drift finding reports a misleading "units no longer match" message on a configured-but-not-installed host

**File:** `internal/doctor/doctor.go:168-200`, `cmd/villa/doctor.go:174-201`, `internal/orchestrate/reconcile.go:26-45`

**Issue:** Both the core comment (lines 167, 106-107) and the cmd-tier comment claim an
"absent/unreadable unit dir degrades to a typed-Unknown WARN (D-08)" via the `err != nil`
branch. That holds only when an *earlier* step in `DriftPlan` errors (e.g. an unresolvable
model, which is the common fresh-host case since `cfg.Model` is empty). But if the host has
a *resolvable* model in config yet `villa install` was never run (or units were manually
deleted), `liveModelFile`/`Render` succeed and `orchestrate.Reconcile` against the absent
dir returns `Plan{Changed: [...]}` with **nil error** — because `Reconcile` treats a
per-file `os.ErrNotExist` as `Changed`, not as an error (`reconcile.go:31-34`). That flows
through the `len(plan.Changed) > 0` branch, emitting "on-disk Quadlet units **no longer
match** the rendered-from-config units" — factually wrong (the units never existed). The
remediation ("re-run `villa install`") is acceptable, but the detail misdescribes the
state, and the documented "absent dir → typed-Unknown WARN" path does not actually fire
for this case.

**Fix:** Detect the absent-dir / never-installed case explicitly so the message matches
reality. Either stat the unit dir in `unitDirReadOnly`'s caller and route a missing dir to
the typed-Unknown WARN finding, or have `Aggregate` distinguish "all units Changed because
absent" from "some units drifted". Minimal version in the live wiring:

```go
dir, err := unitDirReadOnly()
if err != nil { return orchestrate.Plan{}, fmt.Errorf("resolve unit dir: %w", err) }
if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
    return orchestrate.Plan{}, fmt.Errorf("unit dir not present (run `villa install`): %w", statErr)
}
return orchestrate.Reconcile(units, dir)
```

This makes the documented D-08 typed-Unknown WARN path actually fire for a never-installed
host, with an accurate detail string.

### WR-02: `Report.Overall` (JSON contract) and the exit code are computed by two independent classifiers that can diverge

**File:** `internal/doctor/doctor.go:202-221`, `cmd/villa/doctor.go:92-110`

**Issue:** `Aggregate` computes `Report.Overall` via `statusRank` over findings; `renderDoctor`
re-derives the exit code from the same findings with a *separate* loop. They agree today only
by coincidence of identical logic. Any future change to one (e.g. the CR-01 tier-aware fix
applied to only one site) silently desynchronizes the `--json overall` field from the process
exit code — a confusing and hard-to-test contract drift, especially for scripts that branch on
`$?` vs. parse JSON.

**Fix:** Make the exit mapper consume `report.Overall` (plus a tier check for the
block/warn split) instead of re-scanning findings, OR factor a single
`func classify(r Report) (overall string, code int)` used by both. Single source of truth
for the verdict.

### WR-03: `Deps.LoadConfig` is wired but never read by the core — dead seam

**File:** `internal/doctor/doctor.go:97-99`, `cmd/villa/doctor.go:167`

**Issue:** `Deps.LoadConfig` is documented as "Reserved for the cmd-tier drift wiring; the
core reads it only if a future finding needs config directly" and is populated in
`liveDoctorDeps` (`config.LoadVilla`), but `Aggregate` never calls it. The live drift
closure loads config independently via its own `config.LoadVilla()` call. This is a dead
field on the public `Deps` struct: it widens the seam surface, invites a future caller to
assume it is used, and is untested. The `force` global in `root.go` is similarly
reserved-but-unused for doctor, though that is shared flag surface and less concerning.

**Fix:** Remove `Deps.LoadConfig` until a finding actually consumes it (YAGNI), or have
`Aggregate` use it as the single config source so the field earns its place. Do not leave a
populated-but-unread seam field.

## Info

### IN-01: Exit classifier comment claims tier-awareness it does not implement

**File:** `cmd/villa/doctor.go:89-90`

**Issue:** The comment says "collect the BLOCK-class FAILs and whether any WARN is present",
but the loop counts *all* FAILs irrespective of `Tier`. This stale comment masks CR-01 and
will mislead the next maintainer. Update the comment to match whatever final classification
logic ships (and after the CR-01 fix it can then truthfully say BLOCK-class).

### IN-02: `Detail`/`Remediation` strings are concatenated into the human table without escaping; relies on upstream cores being trusted

**File:** `cmd/villa/doctor.go:120-133`

**Issue:** `renderDoctorTable` interpolates `f.Detail`, `f.Remediation`, `f.Provenance`, and
`f.Raw` directly into tabwriter output. `f.Raw` carries "untrusted raw output" (per the
field doc, line 75-77) and is shown under `-v`. Today every Detail/Remediation originates
from in-repo constant strings and Raw is only surfaced on the verbose human path (never in
`--json`, which correctly drops it via `json:"-"`), so there is no injection or info-leak
into the frozen contract. Flagging as Info only: if a future finding ever populates Detail
from a host-derived string containing tabs/newlines, the aligned table could be visually
corrupted. No action required now; note it if Detail ever becomes host-sourced.

---

_Reviewed: 2026-06-07_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
