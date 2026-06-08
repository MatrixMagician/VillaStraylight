---
phase: 17
slug: guided-tui-install-capstone
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-08
validated: 2026-06-08
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
| 17-W0 | — | 0 | INSTALL-01 | — | wizard seam + accessible-mode driver scaffold | unit | `go test ./cmd/villa/ -run TestInstallWizard` | ✅ | ✅ green |
| 17-W0 | — | 0 | D-09/D-10 | T-17-04 | theme + NO_COLOR variant scaffold | unit | `go test ./cmd/villa/ -run TestVillaThemeNoColor` | ✅ | ✅ green |
| 17-INSTALL-01a | 02 | 1 | INSTALL-01 | — | Wizard composes pick→gate→install, computes nothing | unit | `go test ./cmd/villa/ -run TestInstallWizard` | ✅ | ✅ green |
| 17-INSTALL-01b | 03 | 1 | INSTALL-01 | — | Wizard `config.toml` == flag-path `config.toml` for same inputs (SC#1/SC#2) | unit | `go test ./cmd/villa/ -run TestWizardConfigMatchesFlagPath` | ✅ | ✅ green |
| 17-INSTALL-02a | 03 | 1 | INSTALL-02 | T-17-01 | Non-TTY / `--json` / `--no-tui` take flag path; wizard seam NOT invoked (SC#3) | unit | `go test ./cmd/villa/ -run TestInstallGateBypassesWizard` | ✅ | ✅ green |
| 17-INSTALL-02b | 01 | 1/2 | INSTALL-02 | T-17-03 | `CGO_ENABLED=0` build succeeds (SC#4) | build | `CGO_ENABLED=0 go build ./cmd/villa` (`make build-static`) | ✅ | ✅ green |
| 17-INSTALL-02c | 01 | 1/2 | INSTALL-02 | T-17-03 | bubbletea stays v1 (no v2 leak) | static | `go.mod` → `bubbletea v1.3.6`; CI assertion in `.github/workflows/ci.yml` | ✅ | ✅ green |
| 17-D06 | 02 | 1 | INSTALL-01 | — | BLOCK without `--force` → exit code preserved, no mutation | unit | `go test ./cmd/villa/ -run TestInstallBlockWithoutConsentExits1` | ✅ | ✅ green |
| 17-D04 | 02 | 1 | INSTALL-01 | T-17-01 | Privileged fix still consent-gated; never silent | unit | `go test ./cmd/villa/ -run TestInstallConsentNoBlocksAndNeverRunsSeam` | ✅ | ✅ green |
| 17-D09 | 01 | 1 | INSTALL-02 | T-17-04 | NO_COLOR/TERM=dumb degrades theme, wizard still runs | unit | `go test ./cmd/villa/ -run TestVillaThemeNoColor` | ✅ | ✅ green |
| 17-SEAM | 02 | 1 | INSTALL-01 | T-17-04 | Backend/model names rendered via accessors; no re-typed literal | unit | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky · reconciled 2026-06-08 (all tests + CGO-free build + seam gate green)*

**Covering tests (reconciled 2026-06-08, all green):** `TestInstallWizardFires`,
`TestInstallWizardAccessibleDriver`, `TestInstallWizardPathRunsGateOnce`,
`TestWizardConfigMatchesFlagPath`, `TestInstallGateBypassesWizard`, `TestWizardBlockDeclinedCopy`,
`TestVillaThemeColorOn`, `TestVillaThemeNoColor`, `TestVillaThemeDumbTerm`,
`TestStatusGlyphASCIIFallback`, `TestInstallBlockWithoutConsentExits1`,
`TestInstallForceOverridesBlock`, `TestInstallConsentYesRunsSeamOncePerGap`,
`TestInstallConsentNoBlocksAndNeverRunsSeam`, `TestInstallNonInteractiveBlocksAndNeverPrompts`,
`TestPreflightSummaryBlockIndent`, `TestReviewBlockIndent` · `internal/inference`: `TestSeamGrepGate`
· build: `CGO_ENABLED=0 go build ./cmd/villa` OK + `go mod verify` clean (bubbletea v1.3.6).

**Observable signals that prove the phase works:**
1. On a TTY (no `--json`/`--no-tui`), `villa install` launches the wizard (wizard seam invoked — assertable via a fake `wizard` recording a call).
2. With `--no-tui`, `--json`, or a non-TTY stdin/stdout, the wizard seam is NOT invoked and the existing flag path runs.
3. The `config.toml` written by the wizard byte-matches the flag-path config for identical inputs (drive both through `runInstall`, compare `savedCfg`).
4. `CGO_ENABLED=0 go build ./cmd/villa` exits 0; `go mod verify` passes; `go.mod` shows `bubbletea v1`.
5. A privileged fix (PRE-05) still requires explicit consent and is never auto-run (existing tests stay green).
6. A BLOCK gap without `--force` still exits blocked with no write/start.

---

## Wave 0 Requirements

- [x] `cmd/villa/install_wizard.go` + `install_wizard_test.go` — INSTALL-01 (wizard seam + huh accessible-mode driver: `Form.WithAccessible(true)` + `WithInput(scriptedReader)` + `WithOutput(io.Discard)`)
- [x] `cmd/villa/tui_theme.go` + `tui_theme_test.go` — D-09/D-10 (lipgloss theme + NO_COLOR/`termenv.Ascii` variant)
- [x] `make build-static` target — SC#4 (`CGO_ENABLED=0 go build`)
- [x] `.github/workflows/ci.yml` — SC#4 CGO-free gate + `bubbletea v1` assertion
- [x] `safeAutoFix(id) bool` classifier + test — D-05 (returns false for both current privileged fixes; guarded by `TestInstallConsentNoBlocksAndNeverRunsSeam`)

*Existing infrastructure (`go test`, `make check`) covers the assertion mechanics; the above are the new test files/targets this phase must add.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real-TTY wizard walk-through (visual theme, step progress, keyboard nav, NO_COLOR render) | INSTALL-01/02, D-09/D-10 | huh interactive rendering requires a real TTY; accessible-mode tests cover logic but not the rendered visual | On a gfx1151 Fedora host, run `villa install` in a real terminal; walk all 5 screens; repeat with `NO_COLOR=1` and `TERM=dumb`; confirm flow completes unstyled. Record in phase-gate UAT. |
| `--no-tui` / piped-stdin fallback parity on hardware | INSTALL-02 | End-to-end install parity is best confirmed against the live host | `villa install --no-tui` and `villa install </dev/null` both run the flag path and produce an identical `config.toml` to the wizard path for the same recommendation. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 60s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** ✅ validated 2026-06-08

---

## Validation Audit 2026-06-08

State A reconciliation: the planning-time VALIDATION.md was never updated after Phase 17
executed. All 11 per-task verifications are covered by green automated tests in `cmd/villa`
(wizard fires on TTY, three-way bypass `--no-tui`/`--json`/non-TTY, wizard==flag-path config
byte-match, huh accessible-mode driver, theme NO_COLOR/dumb degradation + ASCII glyph fallback,
consent-gated privileged seam, BLOCK-without-force exit preserved) plus `internal/inference`
`TestSeamGrepGate`. SC#4 confirmed live: `CGO_ENABLED=0 go build ./cmd/villa` exits 0,
`go mod verify` clean, `go.mod` pins bubbletea v1.3.6 (no v2 leak), `make build-static` +
`.github/workflows/ci.yml` present. The 3 Manual-Only items (real-TTY wizard walk-through,
NO_COLOR/dumb on-hardware render, `--no-tui`/piped-stdin parity) were performed and PASSED on
gfx1151 (17-UAT.md, 3/3, zero mutation). No auditor spawn needed — zero gaps.

| Metric | Count |
|--------|-------|
| Gaps found | 0 |
| Resolved | 0 |
| Escalated | 0 |
| Tasks COVERED (automated, green) | 11/11 |
| Manual-only (UAT, passed) | 3 |
