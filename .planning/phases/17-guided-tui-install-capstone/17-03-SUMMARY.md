---
phase: 17-guided-tui-install-capstone
plan: 03
subsystem: cli
tags: [huh, tui, install, wizard, consent, accessible-mode, test, go]

# Dependency graph
requires:
  - phase: 17-guided-tui-install-capstone
    plan: 02
    provides: "install.go wizard/stdoutIsTTY seams + runInstall wizard branch + consents threading + safeAutoFix; install_wizard.go buildWizardForm/liveWizard; install_test.go fakeInstallDeps 2-arg pick + default wizard/stdoutIsTTY stubs"
provides:
  - "cmd/villa/install_wizard_test.go — TestInstallWizardFires/GateBypassesWizard/WizardConfigMatchesFlagPath/InstallWizardPathRunsGateOnce/InstallWizardAccessibleDriver/SafeAutoFixReturnsFalseForPrivilegedFixes"
  - "cmd/villa/install_test.go — fakeInstallDeps.wizardCalls counter wired into the default wizard stub"
  - "lineReader headless accessible-mode input driver (one scripted line per huh per-field bufio.Scanner)"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns: [huh accessible-mode headless driver via per-field-scanner lineReader, live-composition gate test with fake-seam-stands-in-for-TTY, fail-the-test consent stub to prove no stdin re-prompt]

key-files:
  created: [cmd/villa/install_wizard_test.go]
  modified: [cmd/villa/install_test.go]

key-decisions:
  - "Accessible-mode driver feeds input through a custom lineReader (one line per Read) rather than strings.NewReader: huh builds a FRESH bufio.Scanner per field over the same reader, and a strings.Reader lets the first field's scanner buffer the whole script and starve later fields (verified empirically — Confirms fell back to false)"
  - "Single-gate test uses the LIVE composition with the wizard SEAM standing in for the TTY-bound huh run (returning the collected consent map), so the real gateInstall→resolveGap→runGapFix→setsebool path executes and the privileged-seam call count is the true assertion of single execution"
  - "d.consent is set to a fail-the-test stub on the threaded wizard path to prove the recorded decision is honored without re-prompting stdin (D-04)"

requirements-completed: [INSTALL-01, INSTALL-02]

# Metrics
duration: 18min
completed: 2026-06-08
---

# Phase 17 Plan 03: Guided TUI Install Wizard Tests Summary

**The automated half of the Phase-17 capstone test map — six tests proving the guided wizard fires on a TTY and is bypassed by `--no-tui`/`--json`/non-TTY (SC#3), that the wizard- and flag-path `config.toml` are byte-identical for identical inputs (SC#1/SC#2), that a BLOCK-gap + privileged-consent scenario through the LIVE composition runs the privileged seam EXACTLY once with the preserved 0/2/1 verdict (zero on denial, no stdin re-prompt — D-04/D-06), that the real huh form drives correctly in accessible mode off-hardware, and that `safeAutoFix` returns false for both current privileged fixes (D-05).**

## Performance
- **Duration:** ~18 min
- **Tasks:** 2 (committed atomically)
- **Files:** 1 created, 1 modified

## Accomplishments
- **`fakeInstallDeps.wizardCalls` counter (install_test.go):** added the field and wired `f.wizardCalls++` into the existing default `wizard` stub, so every install test can assert the wizard fired (or did not) without changing the flag-path default (still `stdoutIsTTY=false`).
- **`TestInstallWizardFires`:** interactive stdin + TTY stdout (no `--json`/`--no-tui`) → the wizard seam fires exactly once and the install reaches `exitPass` (Observable signal 1 / SC#3).
- **`TestInstallGateBypassesWizard`:** a three-case table (`--no-tui`, `--json`, non-TTY stdout) each asserting `wizardCalls == 0` AND that the flag path ran (`saveCalls==1 && writeCalls==1`) (Observable signal 2 / SC#3 / INSTALL-02 fallback).
- **`TestWizardConfigMatchesFlagPath`:** drives the SAME recommendation through both paths (wizard-on vs `--no-tui`) and asserts the captured `savedCfg` `config.VillaConfig` values are byte-identical (SC#1/SC#2).
- **`TestInstallWizardPathRunsGateOnce`:** the single-gate / consent-threading guard. The wizard seam stands in for the TTY-bound huh run and returns the collected consent map, while the REST of the live composition (the single `gateInstall` consuming the threaded map → `resolveGap` → `runGapFix` → `d.setsebool`) runs for real. Consent-granted → `seboolCalls == 1` (exactly once, no double-gate) + preserved `exitPass` + units written; consent-denied → `seboolCalls == 0` + `exitBlocked` + no write/start. `d.consent` is a fail-the-test stub on both sub-cases, proving the threaded path never re-prompts (D-04/D-06/T-17-07).
- **`TestInstallWizardAccessibleDriver`:** drives the REAL `buildWizardForm` off-hardware via `WithInput(lineReader)` + `WithOutput(io.Discard)` + `WithAccessible(true)`, scripting line-based answers (model option `2`, gap confirm `y`, install confirm `y`) and asserting the bound `chosen`/`consents`/`doInstall` hold the scripted selections (Pitfall 1: STDERR render → `io.Discard`; Pitfall 2: line-based, not ANSI arrows).
- **`TestSafeAutoFixReturnsFalseForPrivilegedFixes`:** pins `safeAutoFix("PRE-03") == false` and `safeAutoFix("PRE-05") == false` (D-05 interpretation 1 — both privileged, consent-gated).
- **`lineReader` headless input driver:** returns one scripted line per `Read`, defeating huh's fresh-`bufio.Scanner`-per-field buffering that would otherwise let the first field consume the whole script (root-caused empirically).

## Task Commits
1. **Task 1: wizard fires/bypass + config-match tests** — `a0dffc7` (test)
2. **Task 2: single-gate wizard-path + accessible driver + safeAutoFix** — `35c37e4` (test)

## Files Created/Modified
- `cmd/villa/install_wizard_test.go` (new) — the six wizard tests + the `lineReader` accessible-mode input driver.
- `cmd/villa/install_test.go` — `fakeInstallDeps.wizardCalls int` counter + `f.wizardCalls++` in the default `wizard` stub.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Accessible-mode form starved later fields with a strings.Reader**
- **Found during:** Task 2 (`TestInstallWizardAccessibleDriver`).
- **Issue:** The first run scripted input via `strings.NewReader("2\ny\ny\n")`. The model Select read `2` correctly, but BOTH Confirms bound `false`. Root cause: huh's accessible runner constructs a FRESH `bufio.Scanner` per field over the same reader; a `strings.Reader` hands the entire script to the first field's scanner buffer, leaving nothing for subsequent fields (verified with a standalone reproduction — fields 2 and 3 read `<none>` and fell back to their defaults).
- **Fix:** Added a `lineReader` that returns exactly one newline-terminated line per `Read`, so each per-field scanner reads only its own answer (the off-hardware analog of typing one prompt at a time). All bound vars now hold the scripted selections.
- **Files modified:** cmd/villa/install_wizard_test.go.
- **Commit:** `35c37e4`.

This is a test-harness fidelity fix, not a production-code change — the live wizard runs against a real TTY in interactive (non-accessible) mode and is unaffected.

## Verification Results
- `go test ./cmd/villa/ -run 'TestInstall|TestWizard'` → 34 passed (30 after Task 1, 34 after Task 2).
- `go test ./cmd/villa/ -run 'TestInstallWizardPathRunsGateOnce|TestInstallWizardAccessibleDriver|TestSafeAutoFix'` → 5 passed.
- `go vet ./cmd/villa/` → no issues.
- `go test ./internal/inference/ -run TestSeamGrepGate` → passed (no leaked backend/image literal in the new test code).
- `make check` → all 20 packages ok.
- `go test ./...` → 746 passed in 20 packages.
- `CGO_ENABLED=0 go build ./cmd/villa` → success (SC#4).

## Threat Surface Scan
No new security-relevant surface. The plan's `<threat_model>` is fully satisfied:
- **T-17-01 (EoP):** `TestSafeAutoFixReturnsFalseForPrivilegedFixes` + the consent-denied sub-case of `TestInstallWizardPathRunsGateOnce` (seam 0 invocations) + the preserved existing consent tests are the regression guard for D-04.
- **T-17-04 (seam grep gate):** the new test code adds NO backend/image literal; `TestSeamGrepGate` stays green.
- **T-17-06 (silent flag-path regression):** all existing flag-path tests stay green AND `TestWizardConfigMatchesFlagPath` proves both paths converge.
- **T-17-07 (double-execution):** `TestInstallWizardPathRunsGateOnce` asserts the privileged seam runs EXACTLY once and `d.consent` is never re-invoked on the threaded path.

## Known Stubs
None. (The test-only `wizard` fakes in the gate test are deliberate seam stand-ins for the TTY-bound huh run; the live composition runs for real around them.)

## Self-Check: PASSED

- `cmd/villa/install_wizard_test.go` exists on disk (created).
- Commits `a0dffc7` and `35c37e4` present in git history.
- No file deletions in either task commit.

---
*Phase: 17-guided-tui-install-capstone*
*Completed: 2026-06-08*
</content>
