---
phase: 17-guided-tui-install-capstone
audited: 2026-06-08
auditor: gsd-security-auditor
verdict: SECURED
threats_total: 8
threats_closed: 8
threats_open: 0
block_on: high
asvs_level: default
---

# Phase 17: Guided TUI Install (Capstone) — Security Audit

**Verdict:** SECURED — 8/8 declared mitigations verified present in implemented code.
All threats are `mitigate` disposition; each was verified by locating the actual
mitigation in source (file:line / test) and re-running the cited gates, not by
accepting documentation or intent.

## Threat Verification

| Threat ID | Category | Disposition | Status | Evidence |
|-----------|----------|-------------|--------|----------|
| T-17-01 | Elevation of Privilege | mitigate | CLOSED | Wizard is a pure collector: `install_wizard.go:75-96` returns `wizardResult` (override + consent map) and calls NO fix; file contains no `runGapFix(`/`resolveGap(`/`offerNonBlockingGap(` call (grep confirmed). Single execution point: `runInstall` calls `gateInstall` exactly once (`install.go:280`) AFTER the wizard branch. `runGapFix` (`install.go:687-696`) dispatches fixed-arg seams (`d.setsebool()` / `d.enableLinger(...)`) — no shell. `safeAutoFix` returns false for all ids incl. PRE-03/PRE-05 (`install.go:718-724`). Wizard gated off for non-TTY/--json by `useWizard := d.interactive() && !opts.json && !opts.noTUI && d.stdoutIsTTY()` (`install.go:245`). `TestSafeAutoFixReturnsFalseForPrivilegedFixes` + `TestInstallWizardPathRunsGateOnce` (consent-denied → seam 0) PASS. |
| T-17-02 | Tampering (model interpolation) | mitigate | CLOSED | Wizard Select options bind catalog ids only — `modelOptions` sets each `huh.NewOption(label, rec.Model/a.Model)` value (`install_wizard.go:194-211`); the bound `chosen` is a catalog id, never free text. Override re-validated through the single `d.pick(profile, recommend.Overrides{Model: res.modelOverride})` seam (`install.go:274`) → `recommend.Pick` → `pickOverride` → `catalog.FindByID` (`recommend.go:142,273`); an unknown id returns empty-Model refusal that `runInstall` rejects (`install.go:222`). Container args built fixed-arg behind the seam: `exec.Command("podman", args...)` with explicit "no `sh -c`" (`runner_podman.go:16,64`). |
| T-17-03 | Tampering / Supply Chain | mitigate | CLOSED | `go.mod:7` pins `github.com/charmbracelet/huh v1.0.0`; `bubbletea v1.3.6 // indirect` (`go.mod:20`), no v2. `go mod verify` → "all modules verified" (ran). No `bubbletea/v2`/`charm.land/bubbletea` in go.mod or go.sum (grep confirmed). CI `bubbletea v1` grep assertion present (`.github/workflows/ci.yml:36-37`) + `go mod verify` step (`:33-34`). charmbracelet org. |
| T-17-04 | Tampering (contract erosion) | mitigate | CLOSED | `tui_theme.go` renders no backend/image literal (only color tokens + glyphs). `install_wizard.go` renders backend via accessors only: `backend.Name()` (`:187,241`) and `backend.Image()` (`:242`) — no `kyuz0`/`docker.io/`/`ROCm0`/`HSA_OVERRIDE`. `TestSeamGrepGate` walks `cmd/villa` (`seam_test.go:139-154`) and PASSES (ran). Config value "vulkan" never typed in these files. |
| T-17-05 | DoS / unsafe mutation | mitigate | CLOSED | Install confirm `huh.NewConfirm().Affirmative("Install").Negative("Cancel").Value(doInstall)` with `doInstall` defaulting false → focus on Cancel (`install_wizard.go:71,162-173`; D-07). Mutation only on deliberate Install: `liveWizard` returns `errWizardCancelled` when `!doInstall` (`:79-82`); a form error (Esc/Ctrl+C) returns the error (`:75-78`). `runInstall` maps a wizard error to `exitBlocked` with no mutation, printing the abort copy (`install.go:262-267`). |
| T-17-06 | Repudiation / silent regression | mitigate | CLOSED | `TestWizardConfigMatchesFlagPath` proves wizard-path `savedCfg` byte-matches the `--no-tui` flag-path config (PASS, ran). Flag-path nil consents map preserves byte-for-byte behavior: `resolveGap`/`offerNonBlockingGap` only short-circuit when `consents != nil && recorded` (`install.go:605,651`), else fall through to today's `d.consent` prompt. Existing flag-path install tests stay green (full `-run TestInstall|TestWizard` suite PASS). |
| T-17-07 | Repudiation / double-execution | mitigate | CLOSED | Threaded `consentDecisions` consumed by the SINGLE `gateInstall` (`install.go:280`); a recorded decision routes through the same fixed-arg `runGapFix` WITHOUT calling `d.consent` (`install.go:605-616` / `651-662`). `TestInstallWizardPathRunsGateOnce` asserts the privileged seam runs exactly once (`seboolCalls == 1`) on consent-grant, 0 on deny, with a fail-the-test `d.consent` stub proving no stdin re-prompt (PASS, ran). nil map = unchanged flag path. |
| T-17-SC | Tampering (supply chain) | mitigate | CLOSED | Authoritative Go proxy + signed go.sum: `go mod verify` → "all modules verified" (ran). CI gate `go mod verify` step present (`.github/workflows/ci.yml:33-34`). huh v1.0.0 / lipgloss v1.1.0 / termenv v0.16.0 pinned direct (`go.mod:7,8,11`); bubbletea v1.3.6 indirect. RESEARCH Package Legitimacy Audit (per 17-RESEARCH) — none ASSUMED/SUS/SLOP. |

## Commands Run

- `go mod verify` → `all modules verified`.
- `go test ./internal/inference/ -run 'TestSeamGrepGate|TestROCmMarkerPresence'` → 2 passed (no backend/image literal leaked into theme or wizard).
- `go test ./cmd/villa/ -run 'TestInstallWizard|TestWizardConfigMatchesFlagPath|TestSafeAutoFix|TestVillaTheme'` → 10 passed.
- `CGO_ENABLED=0 go build ./cmd/villa` → Success (SC#4 static build intact with huh added).
- `grep -nE 'bubbletea/v2|charm\.land/bubbletea' go.mod go.sum` → no match (no v2 leak; D-11).

## Unregistered Flags

None. SUMMARY.md `## Threat Surface Scan` sections (17-02, 17-03) report no new
attack surface beyond the plan-time threat register: the wizard adds no network, no
bind, no shell interpolation; privileged fixes stay consent-gated and fixed-arg.
No `unregistered_flag` to log.

## Notes

- All 8 threats are `mitigate` disposition; no `accept`/`transfer` entries to log.
- `block_on: high` — zero unmitigated High/Critical threats; phase may advance.
- Implementation files were not modified (read-only audit).
