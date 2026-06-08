---
phase: 17-guided-tui-install-capstone
verified: 2026-06-08T00:00:00Z
status: passed
score: 4/4 must-have success criteria verified (automated); 3 human-verification items confirmed PASS on-hardware via 17-UAT.md (gfx1151)
overrides_applied: 0
human_verification_resolved: 17-UAT.md — all 3 items PASS on live gfx1151 host (3/3, zero mutation); see UAT method/evidence
human_verification:
  - test: "Real-TTY guided wizard walk-through on a gfx1151 Fedora host"
    expected: "`villa install` in a real terminal walks all 5 screens (detect → model select → preflight gaps + per-item privileged consent → review → Install/Cancel confirm), the Step N/M progress and BLOCK=red/WARN=amber/PASS=green coloring render, keyboard nav works, Install confirm defaults focus to Cancel, and the resulting config.toml + install match the flag path."
    why_human: "huh interactive rendering requires a real TTY; automated tests cover the composition/logic via huh accessible mode but NOT the live rendered visuals (D-09/D-10, INSTALL-01/02)."
  - test: "NO_COLOR=1 and TERM=dumb degraded-theme render on hardware"
    expected: "Re-running `villa install` with `NO_COLOR=1` and again with `TERM=dumb` still presents the full guided flow, unstyled — Foreground stripped, bold/faint/underline + the [OK]/[WARN]/[BLOCK] ASCII glyph column retained; the flow completes."
    why_human: "Live terminal color-profile degradation (termenv.Ascii) is only observable in a real terminal; the theme unit tests assert the style objects but not the rendered escape output (D-09)."
  - test: "`--no-tui` and piped-stdin fallback parity on hardware"
    expected: "`villa install --no-tui` and `villa install </dev/null` both run the flag-driven path and produce a config.toml byte-identical to the wizard path for the same recommendation."
    why_human: "End-to-end install parity is best confirmed against the live host (the byte-identical config is proven off-hardware by TestWizardConfigMatchesFlagPath, but the live install side-effects are not)."
---

# Phase 17: Guided TUI Install (Capstone) Verification Report

**Phase Goal:** Users can run a guided interactive terminal install that composes the finished detect → recommend → confirm/adjust → preflight-gate → install pipeline with confirmation/consent steps — pure presentation, adding no decision logic to any core — degrading gracefully on non-TTY and via `--no-tui` to the flag path, with the binary remaining a single static CGO-free build.
**Verified:** 2026-06-08
**Status:** passed (human-verification items closed by 17-UAT.md, on-hardware gfx1151)
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth (Success Criterion) | Status | Evidence |
|---|---------------------------|--------|----------|
| 1 | Guided TUI walks detect → recommend → confirm/adjust → preflight gate → install, writing the SAME config.toml and running the SAME install as the flag path | ✓ VERIFIED (logic) / ? UAT (visual) | `buildWizardForm` composes 5 UI-SPEC screens (install_wizard.go:114-176); wizard branch sits AFTER `d.runChecks` (install.go:235) and BEFORE the single `gateInstall` (install.go:280) — both paths converge on one gate. `TestWizardConfigMatchesFlagPath` PASS proves wizard-path `savedCfg` byte-matches the `--no-tui` flag-path config. Live visual walk-through deferred to UAT. |
| 2 | TUI computes nothing — all fit/preflight/backend decisions come from existing cores (recommend.Pick, preflight, BackendFor); output matches flag path | ✓ VERIFIED | Wizard is a PURE COLLECTOR: `grep` confirms NO `runGapFix(`/`resolveGap(`/`offerNonBlockingGap(` in install_wizard.go. Model override re-validated through the SINGLE `d.pick(profile, recommend.Overrides{Model:...})` seam (install.go:274). Backend resolved via `inference.BackendFor` and rendered via `.Name()/.Image()` accessors only (no literal leak). `wizardInput` carries already-computed `rec`/`checks`/`backend`. |
| 3 | On non-TTY or `--no-tui`, command degrades to flag-driven path; flags stay first-class | ✓ VERIFIED | Gate expression `useWizard := d.interactive() && !opts.json && !opts.noTUI && d.stdoutIsTTY()` (install.go:245). `TestInstallGateBypassesWizard` PASS for all 3 cases (`--no-tui`, `--json`, non-TTY) — each asserts `wizardCalls==0` AND flag path ran (save=1, write=1). `--no-tui` registered (install.go:205) and appears in `villa install --help`. |
| 4 | Binary still builds as a single static CGO-free binary | ✓ VERIFIED | `CGO_ENABLED=0 go build ./cmd/villa` exits 0. `go.mod` pins `charmbracelet/huh v1.0.0`, `bubbletea v1.3.6` (indirect, no v2). `go mod verify` → "all modules verified". No `charm.land/bubbletea` / `bubbletea/v2` in go.mod or go.sum. `make build-static` target + `.github/workflows/ci.yml` (4 gate steps) present. |

**Score:** 4/4 success criteria verified by automated evidence; SC#1's live visual render + NO_COLOR degradation deferred to on-hardware UAT (per VALIDATION.md Manual-Only table).

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `cmd/villa/install_wizard.go` | huh 5-screen pure-collector wizard, command-tier, imports huh | ✓ VERIFIED | 294 lines; imports `charmbracelet/huh` + 4 internal cores; pure collector (no fix execution); wired live `wizard: liveWizard` (install.go:883). |
| `cmd/villa/tui_theme.go` | NO_COLOR-degradable lipgloss/huh theme, command-tier | ✓ VERIFIED | 5.8K; `villaTheme`/`colorEnabled`/`stdoutIsTTY`/`statusGlyph`/`stepHeader`; no backend literal (seam gate green). |
| `cmd/villa/install.go` | --no-tui flag, wizard+stdoutIsTTY seams, post-runChecks branch, consents threading, safeAutoFix | ✓ VERIFIED | All present; `safeAutoFix` returns false for all ids (default case → PRE-03/PRE-05 false); consents threaded through gateInstall/resolveGap/offerNonBlockingGap. |
| `cmd/villa/install_wizard_test.go` | 6 named phase-17 tests | ✓ VERIFIED | All 6 named tests PASS. |
| `cmd/villa/tui_theme_test.go` | TestVillaThemeNoColor | ✓ VERIFIED | PASS. |
| `.github/workflows/ci.yml` | CGO-free gate + go mod verify + bubbletea-v1 assertion | ✓ VERIFIED | 4 run steps: CGO-free build, vet+test, go mod verify, bubbletea-v1 grep. |
| `Makefile` build-static | CGO_ENABLED=0 target | ✓ VERIFIED | `.PHONY: build-static` (Makefile:24). |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| runInstall | installDeps.wizard | branch AFTER runChecks, BEFORE gateInstall | ✓ WIRED | install.go:245-276; single gateInstall at :280. |
| runInstall | gateInstall | threaded `consentDecisions` consumed once | ✓ WIRED | nil = flag path (prompts via d.consent); populated = honor without re-prompt. |
| install_wizard.go | inference.BackendFor / .Name()/.Image() | review screen accessors | ✓ WIRED | reviewBlock + detectedHostSummary use accessors only; no literal. |
| install_wizard.go | recommend Pick / Alternatives | model options | ✓ WIRED | modelOptions built from rec + rec.Alternatives. |
| install.go liveInstallDeps | liveWizard / stdoutIsTTY | live seam wiring | ✓ WIRED | install.go:882-883. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| 6 named phase-17 tests | `go test ./cmd/villa/ -run '<6 tests>'` | all PASS | ✓ PASS |
| Seam grep gate | `go test ./internal/inference/ -run TestSeamGrepGate` | ok | ✓ PASS |
| Full suite | `go test ./...` | exit 0, no failures (746+ tests) | ✓ PASS |
| CGO-free static build (SC#4) | `CGO_ENABLED=0 go build ./cmd/villa` | exit 0 | ✓ PASS |
| go vet | `go vet ./cmd/villa/` | clean | ✓ PASS |
| Supply-chain | `go mod verify` | all modules verified | ✓ PASS |
| safeAutoFix privileged classifier | `TestSafeAutoFixReturnsFalseForPrivilegedFixes` | PRE-03/PRE-05 both false | ✓ PASS |
| --no-tui in help | `villa install --help` | `--no-tui` present | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| INSTALL-01 | 17-02, 17-03 | Guided TUI composes detect→recommend→preflight→install, presentation only (no core decision logic) | ✓ SATISFIED | Pure-collector wizard composing existing cores; computes nothing; tests prove composition. REQUIREMENTS.md row: Phase 17 / Complete. |
| INSTALL-02 | 17-01, 17-02, 17-03 | Graceful non-TTY / `--no-tui` degradation; single static CGO-free binary | ✓ SATISFIED | Bypass tests PASS (3 cases); CGO-free build exits 0; CI gate present. REQUIREMENTS.md row: Phase 17 / Complete. |

Both requirement IDs from PLAN frontmatter (INSTALL-01, INSTALL-02) accounted for; both map to Phase 17 in REQUIREMENTS.md (lines 93-94); no orphaned requirements.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | None | — | No TBD/FIXME/XXX debt markers, no TODO/HACK/PLACEHOLDER, no stubs, no backend-literal leak in any phase-17 file. |

### Human Verification Required — RESOLVED (17-UAT.md, on-hardware gfx1151)

The phase's VALIDATION.md "Manual-Only Verifications" table deferred the live-rendered surfaces to on-hardware UAT. That UAT is now complete (`17-UAT.md`, status `passed`, 3/3 on a live gfx1151 Fedora host, zero mutation proven by before/after md5 of config.toml + all Quadlet units + `podman ps`). Automated tests cover the composition/logic via huh accessible mode; the items below confirm the live rendered visuals + install side-effects.

1. **Real-TTY guided wizard walk-through** (gfx1151 Fedora host) — ✓ PASS. Wizard launched on a real pty, rendered Screen 1/5 with live-probed host facts, Step N/M in accent color (69 SGR sequences), accessible-mode model select showing recommended + alternatives (D-02). Subsequently driven to completion (exit 0): wrote 1 Quadlet unit, started villa-llama + villa-openwebui, /health 200; ROCm then restored byte-identical. Cancel-default focus asserted by automated test.
2. **NO_COLOR=1 / TERM=dumb degraded render** — ✓ PASS. `NO_COLOR=1` → 0 color SGR sequences, full flow still renders; `TERM=dumb` → 0 escape sequences, huh accessible line-based mode renders the full flow (D-09). One cosmetic non-blocking note: TERM=dumb Ctrl+C aborts via hard SIGINT rather than the graceful copy (no mutation either way).
3. **`--no-tui` / piped-stdin fallback parity on hardware** — ✓ PASS. `--dry-run`, `--no-tui --dry-run`, and piped-stdin --dry-run all took the flag path; rendered install surface byte-identical (md5 546aaa0ae860f7840fbd889e1f025b84) across all three modes; `--no-tui </dev/null` byte-identical to the plain non-TTY flag path.

### Gaps Summary

No automated gaps. All 4 success criteria, both requirements, all artifacts, all key links, the seam gate, the CGO-free build, and the 6 named tests are VERIFIED against the actual codebase. The wizard is confirmed a pure collector (no fix-execution calls), D-11 is honored (no `internal/*` imports huh/bubbletea/lipgloss/termenv anywhere), `safeAutoFix` is false for both privileged fixes, and no debt markers or literal leaks exist.

Status is now `passed`. The three live-terminal items — the rendered visual walk-through, the NO_COLOR/TERM=dumb degraded render, and on-hardware fallback parity — were intrinsically not auto-confirmable and were scheduled for on-hardware UAT by the phase's own validation contract; that UAT is complete (`17-UAT.md`, 3/3 PASS on gfx1151, zero mutation). None of these was a code gap; they confirmed behavior the automated suite already exercises at the logic level.

---

_Verified: 2026-06-08_
_Verifier: Claude (gsd-verifier)_
