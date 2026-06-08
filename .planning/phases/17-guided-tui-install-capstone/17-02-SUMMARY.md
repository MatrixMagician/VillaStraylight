---
phase: 17-guided-tui-install-capstone
plan: 02
subsystem: cli
tags: [huh, tui, install, wizard, consent, preflight, recommend, command-tier, go]

# Dependency graph
requires:
  - phase: 17-guided-tui-install-capstone
    plan: 01
    provides: "cmd/villa/tui_theme.go (villaTheme/colorEnabled/stdoutIsTTY/statusGlyph/statusStyle/stepHeader) + charmbracelet/huh v1.0.0"
provides:
  - "cmd/villa/install_wizard.go — liveWizard + buildWizardForm (5-screen huh pure collector)"
  - "installDeps.wizard + installDeps.stdoutIsTTY seams; widened installDeps.pick(detect.HostProfile, recommend.Overrides)"
  - "runInstall post-runChecks wizard branch gated by interactive() && !json && !noTUI && stdoutIsTTY()"
  - "consents map[string]bool threaded through gateInstall/resolveGap/offerNonBlockingGap (nil = unchanged flag path)"
  - "safeAutoFix(id) bool D-05 classifier (false for PRE-03/PRE-05) + auto-fix branch in gateInstall"
  - "--no-tui flag on villa install"
affects: [17-03 wizard tests]

# Tech tracking
tech-stack:
  added: []
  patterns: [command-tier huh form as pure collector, threaded-consent gate (nil=flag-path), single-polymorphism-point override re-validation via recommend.Overrides]

key-files:
  created: [cmd/villa/install_wizard.go]
  modified: [cmd/villa/install.go, cmd/villa/install_test.go]

key-decisions:
  - "Combined Task 1 + Task 2 into one commit: install.go references wizardInput/wizardResult/liveWizard (defined in install_wizard.go), so the two files are one compilable unit — splitting would yield a non-building intermediate HEAD"
  - "Per-gap decision pointers (gapConsentValue holders) reconciled into the consents map after form.Run — keeps each huh.Confirm self-contained with the map authoritative, no globals"
  - "wizard branch resolves backend via inference.BackendFor(rec.Backend); an unknown backend falls through to the flag path rather than aborting (no re-typed image literal)"
  - "safeAutoFix returns false for both current fixes (PRE-03/PRE-05 privileged, D-05 interpretation 1) — its gateInstall auto-fix branch is a behavior no-op on the current check set"

requirements-completed: [INSTALL-01, INSTALL-02]

# Metrics
duration: 22min
completed: 2026-06-08
---

# Phase 17 Plan 02: Guided TUI Install Wizard Summary

**A `charmbracelet/huh` 5-screen install wizard wired into `villa install` as a PURE COLLECTOR — it presents the already-computed detect/recommend/preflight/backend results, collects a model override + per-item privileged consent, and threads them into the SINGLE existing `gateInstall`, so probe/pick/runChecks/gate each run exactly once for both the wizard and the unchanged flag path (SC#1/SC#2; D-04/D-06 preserved).**

## Performance
- **Duration:** ~22 min
- **Tasks:** 2 (committed as one buildable unit — see Deviations)
- **Files:** 1 created, 2 modified

## Accomplishments
- **`cmd/villa/install_wizard.go` (new):** `wizardInput`/`wizardResult` types, `liveWizard` runner, and `buildWizardForm` composing the 5 UI-SPEC screens (detect Note → model Select → preflight gaps Note+per-item Confirm → review Note → Install Confirm). Backend name/image render via `inference.Backend.Name()/.Image()` accessors only (no seam-literal leak). The Install confirm defaults focus to Cancel (D-07). The wizard runs NO host fix — it returns collected choices.
- **`runInstall` wizard branch (install.go):** inserted AFTER `d.runChecks(...)` and BEFORE the single `gateInstall(...)`. Gate expression `useWizard := d.interactive() && !opts.json && !opts.noTUI && d.stdoutIsTTY()`. On a wizard error/Cancel it prints the UI-SPEC abort copy and returns `exitBlocked` with no mutation. A model override is re-validated through the SAME `d.pick(profile, recommend.Overrides{Model: ...})` polymorphism point.
- **Consent threading:** `gateInstall`/`resolveGap`/`offerNonBlockingGap` now take `consents map[string]bool`. A recorded decision is honored without re-prompting stdin (huh already consumed it); a nil map or unrecorded id falls through to today's `d.consent` path byte-for-byte. A privileged fix runs AT MOST ONCE via the single gate execution.
- **Widened `pick` seam:** `installDeps.pick` is now `func(detect.HostProfile, recommend.Overrides) recommend.Recommendation`; `liveInstallDeps` threads the Overrides into `recommend.Pick`; the flag-path call site is `d.pick(profile, recommend.Overrides{})` (unchanged behavior).
- **`safeAutoFix(id) bool` classifier (D-05):** returns false for PRE-03/PRE-05 (both privileged); wired into `gateInstall` as a forward-looking auto-fix branch (behavior no-op on the current check set).
- **`--no-tui` flag** registered on `villa install`; `stdoutIsTTY`/`liveWizard` wired in `liveInstallDeps`.

## Task Commits
1. **Task 1 + Task 2: wire guided huh install wizard as pure collector** — `e6a054b` (feat)

## Files Created/Modified
- `cmd/villa/install_wizard.go` (new) — the huh 5-screen pure-collector wizard + helpers (detectedHostSummary, modelOptions, preflightSummary, reviewBlock, privilegedGap, statusWord).
- `cmd/villa/install.go` — `noTUI` opt + flag; `wizard`/`stdoutIsTTY` seams; widened `pick`; post-runChecks wizard branch; `consents` threading; `safeAutoFix` + auto-fix branch; live wiring.
- `cmd/villa/install_test.go` — updated `fakeInstallDeps.pick` to the 2-arg signature and defaulted `stdoutIsTTY`/`wizard` seams so existing flag-path tests stay on the flag path (minimal coordination; new wizard tests are 17-03's job).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Committed Task 1 and Task 2 as a single buildable unit**
- **Found during:** Task 1 / Task 2 boundary.
- **Issue:** `install.go` (Task 1) references `wizardInput`/`wizardResult`/`liveWizard`, which are defined in `install_wizard.go` (Task 2). Committing Task 1's files alone would leave a non-compiling intermediate HEAD, violating the "every commit builds" invariant.
- **Fix:** Committed both files together in one `feat(17-02)` commit whose body enumerates both tasks. Every HEAD builds and passes `make check`.
- **Files modified:** cmd/villa/install.go, cmd/villa/install_wizard.go, cmd/villa/install_test.go.
- **Commit:** `e6a054b`.

**2. [Rule 3 - Blocking] Coordinated install_test.go seam defaults beyond the pick signature**
- **Found during:** Task 1 compile/test.
- **Issue:** Three existing flag-path tests set `interactive = true` without setting the new `stdoutIsTTY`/`wizard` seams. With the new gate, `d.stdoutIsTTY()` (nil) would be invoked and panic.
- **Fix:** Defaulted `stdoutIsTTY: func() bool { return false }` and a no-op `wizard` in `newFakeInstallDeps` so those tests stay on the flag path (the wizard branch is gated off). No new wizard tests written — that is 17-03's job. The plan anticipated the pick-signature change; this extends the same minimal coordination to keep the package compiling and existing tests green.
- **Files modified:** cmd/villa/install_test.go.
- **Commit:** `e6a054b`.

## Verification Results
- `go build ./cmd/villa` → success; `CGO_ENABLED=0 go build ./cmd/villa` → success (SC#4).
- `go vet ./cmd/villa/` → no issues.
- `go test ./cmd/villa/` → 230 passed; `-run TestInstall` → 24 passed (existing flag-path tests `TestInstallBlockWithoutConsentExits1`, `TestInstallConsentYesRunsSeamOncePerGap`, `TestInstallWarnLingerOfferGoesToStdout`, `TestInstallConsentNoBlocksAndNeverRunsSeam` green).
- `go test ./internal/inference/ -run TestSeamGrepGate` → passed (no leaked backend/image literal in the wizard).
- `make check` → all packages ok.
- `villa install --help` shows `--no-tui`.
- Source assertions: `useWizard` gate present; `consents map[string]bool` threaded; wizard branch sits after `d.runChecks` (line 235) and before `gateInstall(` (line 280); `pick` widened to `func(detect.HostProfile, recommend.Overrides)`; flag-path call site `d.pick(profile, recommend.Overrides{})`; `safeAutoFix` present; wizard file has no `runGapFix(`/`resolveGap(`/`offerNonBlockingGap(` call.

## Threat Surface Scan
No new security-relevant surface beyond the plan's `<threat_model>`. The wizard adds no network, no bind, no shell interpolation; privileged fixes remain consent-gated and fixed-arg via the single `gateInstall`→`runGapFix` path (T-17-01/T-17-07). Model choice is constrained to catalog ids surfaced in `rec.Alternatives` (T-17-02). Backend text uses accessors only (T-17-04). Install confirm defaults to Cancel (T-17-05).

## Known Stubs
None.

## Self-Check: PASSED

- `cmd/villa/install_wizard.go` exists on disk (created).
- Commit `e6a054b` present in git history.
- No file deletions in the commit.

---
*Phase: 17-guided-tui-install-capstone*
*Completed: 2026-06-08*
