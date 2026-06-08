---
phase: 17
slug: guided-tui-install-capstone
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-08
---

# Phase 17 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` (table-driven, `httptest`, byte-golden). No third-party assert/mock. |
| **Config file** | none (Makefile-driven) |
| **Quick run command** | `go test ./cmd/villa/` |
| **Full suite command** | `make check` (`go vet ./... && go test ./...`) |
| **Static build check** | `CGO_ENABLED=0 go build ./cmd/villa` (SC#4) |
| **Estimated runtime** | ~30 seconds (cmd/villa quick); ~60s full suite |

---

## Sampling Rate

- **After every task commit:** Run `go test ./cmd/villa/`
- **After every plan wave:** Run `make check` + `CGO_ENABLED=0 go build ./cmd/villa`
- **Before `/gsd-verify-work`:** Full suite green AND static build green; on-hardware UAT (real TTY) records a manual wizard walk-through.
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 17-W0 | — | 0 | INSTALL-01 | — | wizard seam + accessible-mode driver scaffold | unit | `go test ./cmd/villa/ -run TestInstallWizard` | ❌ W0 (`install_wizard_test.go`) | ⬜ pending |
| 17-W0 | — | 0 | D-09/D-10 | T-17-04 | theme + NO_COLOR variant scaffold | unit | `go test ./cmd/villa/ -run TestVillaThemeNoColor` | ❌ W0 (`tui_theme_test.go`) | ⬜ pending |
| 17-INSTALL-01a | TBD | 1 | INSTALL-01 | — | Wizard composes pick→gate→install, computes nothing | unit | `go test ./cmd/villa/ -run TestInstallWizard` | ❌ W0 | ⬜ pending |
| 17-INSTALL-01b | TBD | 1 | INSTALL-01 | — | Wizard `config.toml` == flag-path `config.toml` for same inputs (SC#1/SC#2) | unit | `go test ./cmd/villa/ -run TestWizardConfigMatchesFlagPath` | ❌ W0 | ⬜ pending |
| 17-INSTALL-02a | TBD | 1 | INSTALL-02 | T-17-01 | Non-TTY / `--json` / `--no-tui` take flag path; wizard seam NOT invoked (SC#3) | unit | `go test ./cmd/villa/ -run TestInstallGateBypassesWizard` | ❌ W0 | ⬜ pending |
| 17-INSTALL-02b | TBD | 1/2 | INSTALL-02 | T-17-03 | `CGO_ENABLED=0` build succeeds (SC#4) | build | `CGO_ENABLED=0 go build ./cmd/villa` | ❌ W0 (`make build-static`) | ⬜ pending |
| 17-INSTALL-02c | TBD | 1/2 | INSTALL-02 | T-17-03 | bubbletea stays v1 (no v2 leak) | static | `grep -q 'bubbletea v1' go.mod` | ❌ W0 (CI step) | ⬜ pending |
| 17-D06 | TBD | 1 | INSTALL-01 | — | BLOCK without `--force` → exit code preserved, no mutation | unit | `go test ./cmd/villa/ -run TestInstallBlockWithoutConsentExits1` | ✅ exists | ⬜ pending |
| 17-D04 | TBD | 1 | INSTALL-01 | T-17-01 | Privileged fix still consent-gated; never silent | unit | `go test ./cmd/villa/ -run TestInstallConsentNoBlocksAndNeverRunsSeam` | ✅ exists | ⬜ pending |
| 17-D09 | TBD | 1 | INSTALL-02 | T-17-04 | NO_COLOR/TERM=dumb degrades theme, wizard still runs | unit | `go test ./cmd/villa/ -run TestVillaThemeNoColor` | ❌ W0 | ⬜ pending |
| 17-SEAM | TBD | 1 | INSTALL-01 | T-17-04 | Backend/model names rendered via accessors; no re-typed literal | unit | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ exists | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

**Observable signals that prove the phase works:**
1. On a TTY (no `--json`/`--no-tui`), `villa install` launches the wizard (wizard seam invoked — assertable via a fake `wizard` recording a call).
2. With `--no-tui`, `--json`, or a non-TTY stdin/stdout, the wizard seam is NOT invoked and the existing flag path runs.
3. The `config.toml` written by the wizard byte-matches the flag-path config for identical inputs (drive both through `runInstall`, compare `savedCfg`).
4. `CGO_ENABLED=0 go build ./cmd/villa` exits 0; `go mod verify` passes; `go.mod` shows `bubbletea v1`.
5. A privileged fix (PRE-05) still requires explicit consent and is never auto-run (existing tests stay green).
6. A BLOCK gap without `--force` still exits blocked with no write/start.

---

## Wave 0 Requirements

- [ ] `cmd/villa/install_wizard.go` + `install_wizard_test.go` — INSTALL-01 (wizard seam + huh accessible-mode driver: `Form.WithAccessible(true)` + `WithInput(scriptedReader)` + `WithOutput(io.Discard)`)
- [ ] `cmd/villa/tui_theme.go` + `tui_theme_test.go` — D-09/D-10 (lipgloss theme + NO_COLOR/`termenv.Ascii` variant)
- [ ] `make build-static` target — SC#4 (`CGO_ENABLED=0 go build`)
- [ ] `.github/workflows/ci.yml` — SC#4 CGO-free gate + `bubbletea v1` assertion (none exists today)
- [ ] `safeAutoFix(id) bool` classifier + test — D-05 (interpretation 1: returns false for both current privileged fixes)

*Existing infrastructure (`go test`, `make check`) covers the assertion mechanics; the above are the new test files/targets this phase must add.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real-TTY wizard walk-through (visual theme, step progress, keyboard nav, NO_COLOR render) | INSTALL-01/02, D-09/D-10 | huh interactive rendering requires a real TTY; accessible-mode tests cover logic but not the rendered visual | On a gfx1151 Fedora host, run `villa install` in a real terminal; walk all 5 screens; repeat with `NO_COLOR=1` and `TERM=dumb`; confirm flow completes unstyled. Record in phase-gate UAT. |
| `--no-tui` / piped-stdin fallback parity on hardware | INSTALL-02 | End-to-end install parity is best confirmed against the live host | `villa install --no-tui` and `villa install </dev/null` both run the flag path and produce an identical `config.toml` to the wizard path for the same recommendation. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
