# Phase 17: Guided TUI Install (Capstone) - Research

**Researched:** 2026-06-08
**Domain:** Terminal TUI (charmbracelet/huh) as pure presentation over a finished Go control-plane CLI
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** TUI is the **default experience of `villa install`** on an interactive terminal. Bare `villa install` on a TTY launches the wizard. `--no-tui` opts out to today's flag path. NO `villa setup` subcommand, NO `--tui` opt-in flag.
- **D-02:** Confirm/adjust step lets the user **pick from memory-fitting alternatives** — the recommended pick plus other catalog picks that fit the detected envelope, ALL re-validated through `recommend.Pick`. The wizard computes nothing.
- **D-03:** Backend stays **Vulkan by default, never auto-switched** — changes only on explicit user action. The wizard is not where backends get flipped.
- **D-04:** Non-privileged automated fixes **auto-run with a visible notice**; **privileged (sudo) fixes always require explicit per-item consent** (reuse `promptConsent`/`interactive` seam). villa never silently runs a privileged command.
- **D-05:** Auto-run of safe fixes applies to **BOTH paths** (TUI + existing flag path). ⚠️ **Changes today's flag-path behavior** (currently offers safe fixes on y/N). See Contract-Risk note.
- **D-06:** Unmet **BLOCK** gaps still cannot proceed without `--force`; preserve the preflight **0/2/1 (PASS/BLOCK/WARN) exit contract** unchanged.
- **D-07:** Before any host mutation, show a **final review screen** with **one explicit "Install" confirm**.
- **D-08:** Fall back to flag path when **stdin/stdout is not a TTY**, OR `--json` is set, OR `--no-tui` is passed. Flags stay first-class.
- **D-09:** Honor **`NO_COLOR` / `TERM=dumb`** by degrading the **theme** (color/styling), not the whole wizard.
- **D-10:** Ship a small shared **villa `lipgloss` theme**: accent color, status coloring (BLOCK=red/WARN=amber/PASS=green), "Step N/M" progress indicator. One theme file in the command tier; subject to D-09.
- **D-11:** `charmbracelet/huh` **v1.0.0** is the ONLY new first-party dependency — pure-Go/CGO-free. Transitively pins stable `bubbletea v1.3.6` / `lipgloss v1.1.0` (NOT bubbletea/v2). Confined to `cmd/villa/`; no pure core may import it. CI must verify the `CGO_ENABLED=0` static build.

### Claude's Discretion
- Exact screen decomposition/order within `detect → recommend → confirm → gaps → review` (so long as it composes the named cores and honors D-07's final confirm).
- Whether `--dry-run` is exposed as a wizard preview screen or only via the flag/fallback path.
- Internal seam shape for testing the wizard off-hardware (follow `installDeps`/`interactive`/`consent` pattern).

### Deferred Ideas (OUT OF SCOPE)
- Any new install/recommend/preflight **logic**; changing the backend default or auto-switching; a non-install TUI (dashboard TUI, model-management TUI); remote/web install; new config fields.
- A non-install TUI surface — its own phase.
- `--dry-run` rendered as a rich in-wizard preview — Claude's discretion this phase; can be follow-up.

### ⚠️ Contract-Risk Note (a constraint, not deferred work)
D-05 unifies "auto-run non-privileged fixes" across BOTH paths. This **changes current `villa install` flag-path behavior**. The planner MUST: (a) update affected `install_test.go` expectations append-only + re-freeze intentionally; (b) confirm the preflight **0/2/1 exit contract** and the "never silently run a privileged command" rule remain intact (privileged fixes still consent-gated); (c) verify no `--json`/non-interactive regression (auto-run must still respect non-interactive mode). **See the dedicated finding below — this contract change is narrower than it first appears.**
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| INSTALL-01 | Guided interactive TUI install composes detect → recommend → preflight → install with confirm/consent, presentation only (no decision logic in any pure core). | huh form composition pattern (below); the wizard reuses `recommend.Pick`/`internal/preflight`/`inference.BackendFor`/the `installDeps` seam — it threads selections into the EXISTING `runInstall`, adding zero fit/preflight/backend logic. |
| INSTALL-02 | Degrades gracefully on non-TTY and via `--no-tui` to the flag path; binary stays single static CGO-free. | TTY-gate branch point in `runInstall`/`newInstall` (below); huh/bubbletea/lipgloss dependency tree verified pure-Go/CGO-free; `CGO_ENABLED=0` build-check + go.mod pins documented. |
</phase_requirements>

## Summary

Phase 17 is a **pure-presentation capstone**: a `charmbracelet/huh` v1.0.0 wizard that fronts the already-shipped `villa install` pipeline. The whole phase is plumbing — the wizard collects three things from the user (a model choice from `recommend.Pick`'s fitting set, per-item consent for privileged preflight fixes, and a final Install/Cancel confirm) and threads them into the EXISTING `runInstall`. It introduces no new decision logic, no new config fields, and exactly one new dependency.

The three hard problems, all solved with verified library facts: (1) **Testability** — huh forms run interactively against a real TTY, but `Form.WithAccessible(true)` + `WithInput(r)` + `WithOutput(w)` (or `WithProgramOptions(tea.WithInput, tea.WithOutput)`) let the entire wizard run in a unit test driven by a scripted `io.Reader`, with no live terminal. (2) **NO_COLOR/TERM=dumb degradation** — `lipgloss`/`termenv` already auto-honor `NO_COLOR` and `CLICOLOR` via `EnvColorProfile()`, and huh auto-flips to accessible mode when `TERM=dumb`; the explicit escape hatch is `lipgloss.SetColorProfile(termenv.Ascii)` (D-09). (3) **The D-05 contract change is narrower than it reads** — both existing automated fixes (PRE-05 `setsebool -P`, PRE-03 `loginctl enable-linger`) are **privileged**, so "auto-run non-privileged safe fixes" has **no current non-privileged fix to act on**; D-05 is mostly a forward-looking architectural unification, and the only flag-path test churn is the linger (PRE-03) WARN-offer wording.

**Primary recommendation:** Add a single `cmd/villa/install_wizard.go` (the huh form flow) + `cmd/villa/tui_theme.go` (the shared lipgloss/huh theme), gated at the top of `runInstall` by a new `wizard func(...) (wizardResult, error)` seam on `installDeps`. The wizard collects selections and returns them; `runInstall`'s existing detect→pick→gate→render→install body executes unchanged. TTY-gate is `interactive() && !opts.json && !opts.noTUI`. Add a `make build-static` (`CGO_ENABLED=0`) target and a GitHub Actions CI workflow (none exists yet).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Wizard screens / forms / theme | Command tier (`cmd/villa/`) | — | huh is presentation; D-11 forbids any `internal/*` core importing it. |
| Model fit / "alternatives" list | Pure core (`internal/recommend`) | Command tier (renders options) | D-02: the wizard shows `recommend.Pick` output; computes nothing. |
| Preflight gap data + remediation | Pure core (`internal/preflight`) | Command tier (`gateInstall` renders + consents) | Gap classification is core; consent/auto-run is the existing command-tier gap machinery. |
| Backend name/image rendering | Pure core (`internal/inference`) | Command tier (reads accessors) | D-03 + seam grep-gate: backend literals stay behind `BackendFor`/`Backend.Name()`/`Image()`. |
| TTY / color / consent detection | Command tier (injectable funcs) | — | Already the `installDeps` seam pattern (`interactive`, `consent`). |
| Host mutation (pull/write/start) | Command tier `runInstall` + `orchestrate` (impure edge) | — | Reused verbatim; wizard is a front-end, not a different install. |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/charmbracelet/huh` | v1.0.0 | Multi-screen terminal forms (Select/Confirm/Note) | The locked dep (D-11); the de-facto Go TUI form library; pure-Go/CGO-free. `[CITED: proxy.golang.org/github.com/charmbracelet/huh/@v/v1.0.0.mod]` |

### Supporting (transitive — pinned by huh v1.0.0, NOT added directly)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/charmbracelet/bubbletea` | v1.3.6 | TUI runtime huh renders on (stable v1 line, NOT v2) | Used indirectly; theme/program options reference `tea.ProgramOption`. `[VERIFIED: Go proxy go.mod]` |
| `github.com/charmbracelet/lipgloss` | v1.1.0 | Styling primitives for the villa theme (`lipgloss.Style`, `AdaptiveColor`, `SetColorProfile`) | Direct import in `tui_theme.go` only. `[VERIFIED: Go proxy go.mod]` |
| `github.com/muesli/termenv` | v0.16.0 | Color-profile detection; honors `NO_COLOR`/`CLICOLOR` | `termenv.Ascii` used as the explicit no-color escape hatch for D-09. `[VERIFIED: Go proxy go.mod]` |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `huh` (locked) | raw `bubbletea` + hand-rolled forms | More control, far more code; D-11 already locks huh. Rejected by constraint. |
| `huh` | `pterm` / `survey` | `survey` is archived/unmaintained; `pterm` is heavier. D-11 locks huh regardless. |
| `huh` v1.0.0 | `huh` v0.8.0 (would pull bubbletea/v2) | D-11 explicitly requires the STABLE v1 line; v1.0.0 is the first release pinning bubbletea v1.3.6 + lipgloss v1.1.0. **Use v1.0.0.** |

**Installation:**
```bash
go get github.com/charmbracelet/huh@v1.0.0
go get github.com/charmbracelet/lipgloss@v1.1.0   # already pinned transitively; pin direct for tui_theme.go
go mod tidy
```

**Version verification (performed this session):**
- `go list -m -versions github.com/charmbracelet/huh` → latest is `v1.0.0`. `[VERIFIED: Go proxy]`
- `huh@v1.0.0` go.mod requires `bubbletea v1.3.6`, `lipgloss v1.1.0`, `bubbles v0.21.1-...`, `x/term v0.2.1`, `creack/pty v1.1.24` (indirect), `mattn/go-isatty v0.0.20` (indirect), `muesli/termenv v0.16.0` (indirect). `[CITED: proxy.golang.org/.../huh/@v/v1.0.0.mod]`
- NONE of the transitive deps require cgo: `creack/pty` is `syscall`-only (go 1.18, pure-Go), `mattn/go-isatty` uses build-tagged pure-Go syscalls on Linux, `termenv` is pure-Go. `[VERIFIED: source inspection]`

## Package Legitimacy Audit

> slopcheck (v0.6.1, installed) targets npm/PyPI/crates and does NOT cover Go modules. Legitimacy for Go modules is therefore established via the **authoritative Go module proxy** (`proxy.golang.org`) and the published, signed go.mod / go.sum chain, plus direct source inspection — the Go-ecosystem equivalent of registry verification.

| Package | Registry | Age | Downloads | Source Repo | Check | Disposition |
|---------|----------|-----|-----------|-------------|-------|-------------|
| `charmbracelet/huh` v1.0.0 | Go proxy | charmbracelet org, multi-yr | very high (de-facto Go forms lib) | github.com/charmbracelet/huh | proxy-verified go.mod; well-known org | Approved |
| `charmbracelet/bubbletea` v1.3.6 | Go proxy | multi-yr | very high | github.com/charmbracelet/bubbletea | transitive pin, proxy-verified | Approved |
| `charmbracelet/lipgloss` v1.1.0 | Go proxy | multi-yr | very high | github.com/charmbracelet/lipgloss | transitive pin, proxy-verified | Approved |

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

*Go-module note for the planner: `go.sum` is the supply-chain integrity gate here — `go mod verify` after `go get` confirms the downloaded module hashes match the checksum database. The planner should include a `go mod verify` step. This is strictly stronger than registry-existence checking.*

## Architecture Patterns

### System Architecture Diagram

```
                          villa install [flags]
                                  │
                                  ▼
                        ┌───────────────────┐
                        │   newInstall RunE │  liveInstallDeps()
                        └─────────┬─────────┘
                                  ▼
                        ┌───────────────────┐
                        │     runInstall    │
                        │  (returns 0/2/1)  │
                        └─────────┬─────────┘
                                  │
              ┌───────────────────┴───────────────────┐
   interactive() && !json && !noTUI ?        otherwise (non-TTY / --json / --no-tui)
              │ YES                                    │ NO
              ▼                                        │
   ┌────────────────────────┐                         │
   │  d.wizard(...) seam     │  ← NEW (install_wizard.go)
   │  huh.Form: 5 screens    │                         │
   │   1 detect  (Note)      │  composes:              │
   │   2 model   (Select) ───┼──► recommend.Pick       │
   │   3 gaps    (Confirm)───┼──► gateInstall machinery │
   │   4 review  (Confirm)   │   (consent funcs reused) │
   │   5 install (delegate)  │                         │
   └───────────┬────────────┘                         │
               │ returns wizardResult                  │
               │ (chosen model + consent decisions)    │
               ▼                                        ▼
   ┌──────────────────────────────────────────────────────────┐
   │   EXISTING runInstall body (UNCHANGED logic):            │
   │   probe → pick(override) → gateInstall → render →        │
   │   reconcile → ensureModel → saveConfig → write → start → │
   │   pollReady → printPostInstall                            │
   └──────────────────────────────────────────────────────────┘
               │
               ▼
   config.toml (single source of truth) + Quadlet units + systemd
```

The wizard is an OPTIONAL front-end that COLLECTS inputs and hands them to the unchanged install body. Both paths converge on the same `runInstall` mutation sequence → guarantees SC#1/SC#2 (same config.toml, same install).

### Recommended Project Structure
```
cmd/villa/
├── install.go            # EXISTING — add the wizard seam field + the TTY-gate branch
├── install_hostprep.go   # EXISTING — stdinIsInteractive/promptConsent reused as-is
├── install_wizard.go     # NEW — the huh form flow (wizardResult, wizard runner, screens)
├── install_wizard_test.go# NEW — drives the wizard via WithInput/WithAccessible (no TTY)
├── tui_theme.go          # NEW — shared villa lipgloss/huh theme + Step N/M + glyph helpers (D-10)
└── tui_theme_test.go     # NEW — color-on vs NO_COLOR theme variants
```

### Pattern 1: huh Form composed of grouped screens
**What:** A `huh.Form` of sequential `huh.NewGroup`s; each group is one wizard screen. Fields are `huh.NewSelect`, `huh.NewConfirm`, `huh.NewNote`.
**When to use:** The 5-screen sequence from the UI-SPEC.
**Example:**
```go
// Source: huh README + form.go (github.com/charmbracelet/huh v1.0.0)
var chosen string   // selected model id
var doInstall bool  // final confirm
form := huh.NewForm(
    huh.NewGroup( // Screen 1: detected host (read-only)
        huh.NewNote().Title("Detected host").Description(hostSummary), // detect.HostProfile
    ),
    huh.NewGroup( // Screen 2: confirm/adjust model (D-02)
        huh.NewSelect[string]().
            Title("Confirm your model").
            Options(modelOptions...).   // built from rec + rec.Alternatives (recommend.Pick)
            Value(&chosen),
    ),
    // Screen 3 (preflight gaps) drives gateInstall's consent funcs, NOT a static group —
    // privileged fixes get a per-item huh.NewConfirm; safe fixes auto-run with a Note.
    huh.NewGroup( // Screen 4: final review + single Install confirm (D-07)
        huh.NewNote().Title("Review — villa will install:").Description(reviewBlock),
        huh.NewConfirm().Title("Install?").Affirmative("Install").Negative("Cancel").Value(&doInstall),
    ),
).WithTheme(villaTheme(colorEnabled))
err := form.Run()
```

### Pattern 2: Testable wizard runner seam (the central testability solution)
**What:** A `wizard` func on `installDeps`. The live impl runs huh against the real TTY; tests inject a fake that returns a canned `wizardResult`. ADDITIONALLY, the live impl itself is testable by constructing the form with `WithInput`/`WithAccessible`.
**When to use:** Mirrors `interactive`/`consent` — the established pattern (D-08 hint, CONTEXT Claude's-Discretion).
**Example:**
```go
// Source: huh form.go — WithInput/WithOutput/WithAccessible (v1.0.0, verified this session)
// In tests, drive the REAL form with scripted keystrokes and no TTY:
form := buildWizardForm(&chosen, &doInstall, theme).
    WithInput(strings.NewReader("\x1b[B\r"+"y\r")). // ↓ Enter, then y Enter
    WithOutput(io.Discard).
    WithAccessible(true)   // accessible mode = line-based, no ANSI redraw, screen-reader safe
_ = form.Run()
// chosen / doInstall now hold the scripted selections — assert on them off-hardware.
```
Accessible mode is the canonical "headless" driver: `Form.RunWithContext` branches `if f.accessible { return f.runAccessible(output||stdout, input||stdin) }`. `[CITED: huh v1.0.0 form.go]`

### Pattern 3: Theme with NO_COLOR / TERM=dumb degradation (D-09)
**What:** `tui_theme.go` exposes `villaTheme(colorEnabled bool) *huh.Theme` built from `huh.ThemeBase()` with villa overrides; when `!colorEnabled`, strip `Foreground` calls but KEEP bold/faint/underline + the glyph column.
**Example:**
```go
// Source: huh theme.go (ThemeBase) + lipgloss v1.1.0 renderer.go (SetColorProfile)
func colorEnabled() bool {
    // termenv/lipgloss already auto-detect; this is the explicit gate for the theme builder.
    return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && stdoutIsTTY()
}
func villaTheme(colorEnabled bool) *huh.Theme {
    t := huh.ThemeBase()
    if !colorEnabled {
        lipgloss.SetColorProfile(termenv.Ascii) // belt-and-braces: globally strip color
        return t                                 // ThemeBase keeps bold/underline attrs
    }
    accent := lipgloss.AdaptiveColor{Light: "63", Dark: "105"}
    t.Focused.Title = t.Focused.Title.Foreground(accent).Bold(true).Underline(true)
    t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(accent)
    // status styles (PASS green / WARN amber / BLOCK red) as named lipgloss.Styles reused by gateInstall rendering
    return t
}
```
Note: `huh` ALSO auto-enables accessible mode when `TERM=dumb` (`if os.Getenv("TERM")=="dumb" { f.WithAccessible(true) }`), and `termenv.EnvColorProfile()` returns `Ascii` whenever `NO_COLOR` is set. So D-09 is partly free; the theme builder makes it explicit and testable. `[CITED: huh v1.0.0 form.go; termenv v0.16.0 termenv.go]`

### Anti-Patterns to Avoid
- **Re-typing backend/image literals in TUI code:** the wizard MUST render backend name via `inference.BackendFor(cfg.Backend).Name()` and image via `.Image()` — `TestSeamGrepGate` walks `cmd/villa` and fails on a leaked `vulkan`/`kyuz0`/`docker.io/` image literal. The bare config-VALUE string `"vulkan"` is allowed (it's seam-clean data), but the IMAGE literal is not.
- **Importing huh in any `internal/*` package:** D-11 hard rule. The theme, forms, and glyph helpers all live in `cmd/villa/`.
- **Letting the wizard recompute fit/preflight/backend:** INSTALL-01/SC#2 — the wizard only PRESENTS `recommend.Pick` / `preflight` / `BackendFor` output.
- **Mutating before the final confirm:** D-07 — no pull/write/start until the Install confirm returns true; the Confirm's default focus is **Cancel**.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Terminal forms / keyboard nav | Custom raw-mode input loop | `huh.NewForm` | huh owns cursor, key map, focus, redraw, accessible mode — re-rolling is a rewrite. |
| Color-profile / NO_COLOR detection | env-var sniffing in TUI code | `termenv`/`lipgloss` auto-detect + `SetColorProfile(termenv.Ascii)` | Already correct per no-color.org + clicolors spec; verified honored. |
| TTY detection | manual ioctl | EXISTING `stdinIsInteractive()` + a new `stdoutIsTTY()` (same `os.ModeCharDevice` check) | The codebase already has this pattern in `install_hostprep.go`. |
| Privileged consent prompting | new prompt logic | EXISTING `installDeps.consent`/`interactive` + `gateInstall`/`resolveGap` | D-04 says reuse these semantics verbatim. |
| Model fit / alternatives | new fit math in the wizard | `recommend.Pick(...).Alternatives` | D-02/SC#2 — zero new decision logic. |

**Key insight:** Phase 17 is ~90% wiring. Every hard subproblem (forms, color, TTY, consent, fit) already has an owner — the wizard's job is to call them in sequence and thread the user's choices into `runInstall`.

## D-05 Contract Change — Detailed Finding (the planner's highest-risk item)

**What D-05 says:** auto-run non-privileged safe fixes across BOTH the TUI and the flag path (today the flag path offers safe fixes on y/N).

**What the code actually shows (verified):** the install flow has exactly TWO automated fixes (`hasAutomatedFix` in `install.go`):
- **PRE-05** → `setsebool -P container_use_devices=true` — **privileged** (system-wide SELinux boolean; needs root).
- **PRE-03** → `loginctl enable-linger <user>` — **privileged** (system-level linger registration; needs root/polkit).

**Therefore: there is currently NO non-privileged automated fix in the check set.** Both go through consent today (PRE-05 via `resolveGap` BLOCK path → stderr; PRE-03 via `offerNonBlockingGap` WARN path → stdout). This makes D-05's "auto-run safe fixes" change **narrower than the wording implies**:

1. **If the planner treats both PRE-03 and PRE-05 as privileged** (the honest reading — both literally shell to `sudo`-class operations), then **D-05's "auto-run non-privileged" rule has nothing to auto-run today**, and the flag-path behavior change is effectively a no-op on the current check set. D-05 becomes a forward-looking architectural contract: a `safeAutoFix(id) bool` classifier that, when it ever returns true for a future check, auto-runs with a Note. **This is the lowest-risk interpretation and is recommended.**

2. **If the planner reclassifies PRE-03** `loginctl enable-linger` as a per-user (non-`sudo`) operation that may auto-run (it can succeed without sudo on some polkit configs), then the change is real and touches `offerNonBlockingGap` + `TestInstallWarnLingerOfferGoesToStdout`. ⚠️ This risks silently running a privileged-ish command and contradicts D-04's "never silently run a privileged command." **Recommend AGAINST reclassifying PRE-03 as safe-auto unless the user confirms.** `[ASSUMED]` that enable-linger needs privilege — verify with the user; conservatively treat as privileged.

**Tests/contracts the planner must touch (verified — there is NO install golden):**
- There is **no `install` golden file** (`cmd/villa/testdata/` has no install golden; install tests assert on substrings + call counts). So D-05 has **no byte-frozen golden to re-freeze** — only Go-test expectations change.
- Affected tests if behavior changes: `TestInstallWarnLingerOfferGoesToStdout` (PRE-03 offer wording), `TestInstallConsentYesRunsSeamOncePerGap`, and any test asserting the y/N prompt text for a "safe" fix. If interpretation (1) is adopted, **only the introduction of the `safeAutoFix` classifier needs a new test** asserting it returns false for PRE-03/PRE-05 (privileged) — existing tests stay green.
- **Invariants to preserve regardless:** (a) 0/2/1 exit contract (`exitPass`/`exitBlocked`/`exitWarn`); (b) `--json`/non-interactive NEVER prompts and NEVER auto-runs a privileged fix; (c) BLOCK without `--force` → exit `exitBlocked` (1), no mutation.

## Wiring — the precise TTY-gate branch point (D-01/D-08)

**Where:** the top of `runInstall(cmd, opts, d)` in `cmd/villa/install.go`, AFTER resolving `out`/`errOut` and BEFORE step (1) `d.probe()`. Add a new field to `installOpts` and `installDeps`:

```go
// installOpts: add
noTUI bool   // --no-tui opts out to the flag path (D-01/D-08)

// installDeps: add a wizard runner seam (Claude's-discretion seam shape, D-08 hint)
wizard func(ctx context.Context, in wizardInput) (wizardResult, error)

// newInstall: register the flag
cmd.Flags().BoolVar(&noTUI, "no-tui", false, "skip the guided wizard; use the flag-driven install path")
// and thread it: runInstall(cmd, installOpts{dryRun: dryRun, force: force, json: jsonOut, noTUI: noTUI}, deps)
```

**The gate (in runInstall):**
```go
useWizard := d.interactive() && !opts.json && !opts.noTUI && d.stdoutIsTTY()
if useWizard {
    // RESOLVED: probe/pick/runChecks are HOISTED before this branch (they already run as
    // runInstall steps 1-3) and their results are PASSED IN — the wizard computes none of them.
    res, err := d.wizard(cmd.Context(), wizardInput{profile: profile, rec: rec, alternatives: rec.Alternatives, checks: checks, backend: backend, colorEnabled: colorEnabled()})
    if err != nil { /* Esc/Ctrl+C → clean abort, non-zero, "use --no-tui" hint, NO mutation */ }
    // thread res.modelOverride into recommend.Overrides (re-validated via Pick); thread
    // res.consentDecisions into the SINGLE gateInstall call (nil map on the flag path = today's
    // d.consent prompt; populated map = honor the recorded y/n, no stdin re-prompt).
    // The wizard NEVER runs runGapFix itself; gateInstall runs EXACTLY ONCE after this branch.
}
// fall through to the EXISTING detect→pick→gate→render→install body (unchanged)
```

**Short-circuit rules (all three force the flag path):** `--json` (a `--json` run is never a wizard), `--no-tui`, and non-TTY (stdin OR stdout not a char device). `interactive()` already checks stdin; ADD `stdoutIsTTY()` (huh renders to stdout/stderr — both must be a terminal). Mirror `stdinIsInteractive`'s `os.ModeCharDevice` test against `os.Stdout`.

**Note on composition:** the cleanest decomposition is for the wizard to OWN screens 1–4 (collect a model override + consent decisions) and RETURN to `runInstall`, which runs the existing gate + mutation. This keeps the wizard a pure collector and means `recommend.Pick`/`gateInstall` are called exactly once, in one place, for both paths — satisfying SC#2 byte-for-byte.

## Common Pitfalls

### Pitfall 1: huh writes to STDERR by default, not stdout
**What goes wrong:** `Form.WithOutput` default is "STDOUT when accessible, STDERR otherwise" — interactive huh renders to **stderr**. If the wizard or a test assumes stdout, output/redirection is wrong.
**How to avoid:** In tests, set `WithOutput(io.Discard)` or a buffer explicitly. In production, accept huh's default (stderr for the live render; the final `printPostInstall` success line still goes to stdout via the existing `runInstall`).
**Warning signs:** a test capturing stdout sees an empty wizard. `[CITED: huh v1.0.0 form.go WithOutput doc]`

### Pitfall 2: accessible mode ≠ interactive mode keystrokes
**What goes wrong:** Driving a test with raw ANSI arrow bytes works in interactive mode but accessible mode reads **line-based** input (type the option number / value + Enter). Mixing them yields stuck tests.
**How to avoid:** For tests, prefer `WithAccessible(true)` and feed line-based answers (e.g. option index + `\n`). This is also the most stable across huh versions.
**Warning signs:** test hangs or selects the wrong option.

### Pitfall 3: TERM=dumb auto-flips to accessible, bypassing the theme path
**What goes wrong:** Under `TERM=dumb`, huh forces accessible mode — the styled interactive render never runs, so theme assertions on the styled path don't fire.
**How to avoid:** Treat `TERM=dumb` as a D-09 degradation case (correct behavior); test the styled path with a normal TERM + injected I/O, and test the degraded path separately.
**Warning signs:** none — this is correct; just don't conflate the two test cases.

### Pitfall 4: bubbletea v2 accidental upgrade
**What goes wrong:** `go get charmbracelet/bubbletea@latest` could pull `v2` (`charm.land/bubbletea/v2`), violating D-11 and breaking huh v1.0.0.
**How to avoid:** Only `go get huh@v1.0.0` and let it pin bubbletea v1.3.6 transitively; add a CI assertion `grep -q 'bubbletea v1' go.mod` (or `go mod why`). Never pin bubbletea directly.
**Warning signs:** `go.mod` shows `charm.land/bubbletea/v2` or `bubbletea v2.x`.

## CGO-free Static Build Verification (SC#4)

**Facts (verified this session):** the entire huh v1.0.0 dependency tree is pure-Go. `creack/pty` (indirect) is `syscall`-only; `mattn/go-isatty` uses build-tagged pure-Go syscalls on Linux; `termenv`/`lipgloss`/`bubbletea` are pure-Go. No `import "C"` anywhere in the tree. `[VERIFIED: go.mod inspection + source]`

**Recommended Makefile addition:**
```make
.PHONY: build-static
build-static: ## Build a CGO-free static binary (SC#4 — must succeed with huh added)
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/$(BINARY)
```

**Recommended CI (none exists yet — `.github/workflows/` is absent):** add `.github/workflows/ci.yml` running on push/PR:
```yaml
- run: CGO_ENABLED=0 go build ./...          # SC#4 static build gate
- run: go vet ./... && go test ./...          # existing make check
- run: go mod verify                          # supply-chain integrity
- run: grep -q 'bubbletea v1' go.mod || (echo "bubbletea v2 leaked!" && exit 1)
```
A self-contained verification even without CI: `CGO_ENABLED=0 go build ./cmd/villa && file villa` → expect "statically linked" (or no dynamic libc beyond the Go runtime). The `CGO_ENABLED=0` build SUCCEEDING is the SC#4 signal.

## Project Constraints (from CLAUDE.md)

- **Pure-core + injectable-seam:** the wizard is command-tier presentation; add a `wizard` func seam to `installDeps`, wire it live in `liveInstallDeps`, stub it in tests. No `internal/*` core imports huh.
- **Seam grep-gate (`TestSeamGrepGate`):** backend/image literals stay behind `internal/inference`; the wizard renders names via `Backend.Name()`/`.Image()` accessors, never re-typed. The gate walks `cmd/villa`.
- **Byte-frozen `--json`/golden contracts:** `--json` always takes the flag path (never a wizard) — the wizard adds NO new `--json` output, so no `--json` golden changes. There is no install golden to break.
- **Config is the single source of truth:** the wizard threads selections into `runInstall`, which persists via `config.SaveVilla` exactly as today — same config.toml (SC#1).
- **No shell interpolation:** unchanged — all consented fixes still run via fixed-arg seams (`runGapFix`). The wizard never builds a shell command.
- **Loopback-only / no telemetry:** unchanged; the wizard adds no network or bind.
- **0/2/1 exit contract + return-not-Exit:** `runInstall` still RETURNS the code; the wizard abort (Esc) maps to a non-zero return, no `os.Exit` inside `runInstall`.

## Runtime State Inventory

> Greenfield-additive phase (new presentation layer over existing flow). No rename/refactor/migration. Included for completeness.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — wizard writes the SAME `config.toml` via the existing `saveConfig` seam. Verified: no new config fields (D-domain out-of-scope). | none |
| Live service config | None — same Quadlet units / systemd services as the flag path. | none |
| OS-registered state | None new — consented privileged fixes (setsebool/linger) are the EXISTING ones. | none |
| Secrets/env vars | Reads `NO_COLOR`/`TERM`/`CLICOLOR` (D-09) — read-only, no new secret. | none |
| Build artifacts | `go.sum`/`go.mod` gain huh + transitive entries; `villa` binary grows (still single static CGO-free). | `go mod tidy` + `go mod verify` |

**Nothing found requiring data migration** — verified: the wizard is a front-end that converges on the unchanged `runInstall` mutation sequence.

## Code Examples

### Building Select options from recommend.Pick (D-02)
```go
// Source: internal/recommend Recommendation + Alternative (verified types)
rec := recommend.Pick(profile, cat, recommend.Overrides{})
opts := []huh.Option[string]{
    huh.NewOption(fmt.Sprintf("%s · %s · ctx %d  (recommended)", rec.Model, rec.Quant, rec.ContextLen), rec.Model),
}
for _, a := range rec.Alternatives { // already memory-fitting, re-validated by Pick
    opts = append(opts, huh.NewOption(
        fmt.Sprintf("%s · %s · ctx %d", a.Model, a.Quant, a.ContextLen), a.Model))
}
// chosen model id → recommend.Overrides{Model: chosen} re-run through Pick for the final rec.
```

### Per-item privileged consent inside the gap screen (D-04)
```go
// Reuse the EXISTING consent semantics — do not add new prompt logic.
// For a privileged BLOCK gap, present a huh.NewConfirm; on affirm, call the SAME
// resolveGap path (fixed-arg runGapFix). On non-TTY/--json this branch is never reached
// (the whole wizard is gated off) — the flag path's resolveGap handles it.
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `bubbletea` v1 + manual forms | `huh` v1.0.0 high-level forms on bubbletea v1.3.6 | huh v1.0.0 (stable) | Far less form code; v1.0.0 is the stable v1 line D-11 requires. |
| `huh` v0.x pulling bubbletea/v2 pre-releases | `huh` v1.0.0 pinning stable bubbletea v1.3.6 / lipgloss v1.1.0 | v1.0.0 | D-11 chose v1.0.0 specifically to avoid bubbletea/v2. |
| `Field.WithAccessible(bool)` | `Field.RunAccessible(w, r)` direct (field-level deprecation) | huh v1.0.0 | For FIELDS the method is deprecated; for FORMS, `Form.WithAccessible(true)` is still the supported toggle. Use the Form-level toggle. |

**Deprecated/outdated:**
- `Select.WithAccessible(bool)` (field-level) is deprecated in v1.0.0 in favor of `RunAccessible`; but `Form.WithAccessible(bool)` is NOT deprecated — use the form-level toggle for the wizard test driver. `[CITED: huh v1.0.0 field_select.go + form.go]`

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `loginctl enable-linger` (PRE-03) requires privilege and must NOT auto-run silently. | D-05 finding | If it can safely run per-user, D-05 interpretation (2) becomes viable; but treating it as privileged is the conservative, D-04-compliant choice. Verify with user. |
| A2 | No `install` golden file exists (only substring/call-count tests). | D-05 finding | Verified by `ls testdata/` (no install golden) — LOW risk; planner should re-confirm before re-freezing nothing. |

**The remaining findings are VERIFIED (Go proxy go.mod, source inspection) or CITED (huh/lipgloss/termenv source at the pinned tags).**

## Open Questions (RESOLVED)

1. **Should PRE-03 (linger) be reclassified as a non-privileged safe-auto fix under D-05?**  
   **RESOLVED: NO — interpretation (1).** PRE-03/enable-linger stays PRIVILEGED and consent-gated
   ([ASSUMED] privilege; conservative, D-04-compliant). `safeAutoFix(id)` returns false for both
   current fixes; D-05 is a forward-looking classifier with nothing to auto-run on the current
   check set. villa never silently runs a privileged command.
   - What we know: it shells to `loginctl enable-linger`, currently consent-gated via `offerNonBlockingGap` (WARN/stdout).
   - What's unclear: whether the user intends D-05 to change PRE-03's behavior, or whether D-05 is purely forward-looking (no current non-privileged fix).
   - Recommendation: adopt interpretation (1) — add a `safeAutoFix(id) bool` classifier returning false for both current fixes; surface this to the user in plan review. Do NOT auto-run a privileged command (D-04).

2. **Wizard ownership of `--dry-run` (Claude's discretion):**  
   **RESOLVED:** `--dry-run` stays on the flag/fallback path only this phase (NOT an in-wizard
   preview screen) — the wizard's final review screen (D-07) already serves "preview before mutate."
   - Recommendation: keep `--dry-run` on the flag path only this phase (the wizard's final review screen already serves the "preview before mutate" purpose, D-07). Defer a rich in-wizard dry-run preview.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | 1.26.2 (go.mod) | — |
| `charmbracelet/huh` v1.0.0 | the wizard | via `go get` | v1.0.0 (Go proxy) | — |
| A TTY (for manual UAT only) | live wizard render | dev-dependent | — | `--no-tui` / tests via `WithAccessible` |
| GitHub Actions runner | CGO-free CI gate (SC#4) | ✗ (no `.github/workflows/`) | — | local `make build-static` |

**Missing dependencies with fallback:**
- CI workflow absent → add `.github/workflows/ci.yml`; until then `make build-static` + `make check` is the local SC#4 gate.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven, `httptest`, byte-golden). No third-party assert/mock. |
| Config file | none (Makefile-driven) |
| Quick run command | `go test ./cmd/villa/` |
| Full suite command | `make check` (`go vet ./... && go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| INSTALL-01 | Wizard composes pick→gate→install, computes nothing | unit | `go test ./cmd/villa/ -run TestInstallWizard` | ❌ Wave 0 (`install_wizard_test.go`) |
| INSTALL-01 | Wizard output config.toml == flag-path config.toml for same inputs (SC#1/SC#2) | unit | `go test ./cmd/villa/ -run TestWizardConfigMatchesFlagPath` | ❌ Wave 0 |
| INSTALL-02 | Non-TTY / `--json` / `--no-tui` take the flag path (SC#3) | unit | `go test ./cmd/villa/ -run TestInstallGateBypassesWizard` | ❌ Wave 0 |
| INSTALL-02 | `CGO_ENABLED=0` build succeeds (SC#4) | build | `CGO_ENABLED=0 go build ./cmd/villa` | ❌ Wave 0 (`make build-static`) |
| INSTALL-02 | bubbletea stays v1 (no v2 leak) | static | `grep -q 'bubbletea v1' go.mod` | ❌ Wave 0 (CI step) |
| D-06 | BLOCK without `--force` → exit 1, no mutation (preserved) | unit | `go test ./cmd/villa/ -run TestInstallBlockWithoutConsentExits1` | ✅ exists |
| D-04 | Privileged fix still consent-gated; never silent | unit | `go test ./cmd/villa/ -run TestInstallConsentNoBlocksAndNeverRunsSeam` | ✅ exists |
| D-09 | NO_COLOR/TERM=dumb degrades theme, wizard still runs | unit | `go test ./cmd/villa/ -run TestVillaThemeNoColor` | ❌ Wave 0 (`tui_theme_test.go`) |

**Observable signals that prove the phase works (for VALIDATION.md):**
1. On a TTY (no `--json`/`--no-tui`), `villa install` launches the wizard (the wizard seam is invoked — assertable via the fake `wizard` recording a call).
2. With `--no-tui`, `--json`, or a non-TTY stdin/stdout, the wizard seam is NOT invoked and the existing flag path runs.
3. The `config.toml` written by the wizard byte-matches the flag-path config for identical inputs (drive both through `runInstall`, compare `savedCfg`).
4. `CGO_ENABLED=0 go build ./cmd/villa` exits 0; `go mod verify` passes; `go.mod` shows `bubbletea v1`.
5. A privileged fix (PRE-05) still requires explicit consent and is never auto-run (existing tests stay green).
6. A BLOCK gap without `--force` still exits `exitBlocked` (1) with no write/start.

### Sampling Rate
- **Per task commit:** `go test ./cmd/villa/`
- **Per wave merge:** `make check` + `CGO_ENABLED=0 go build ./cmd/villa`
- **Phase gate:** full suite green + static build green before `/gsd-verify-work`; on-hardware UAT (real TTY) records a manual wizard walk-through.

### Wave 0 Gaps
- [ ] `cmd/villa/install_wizard.go` + `install_wizard_test.go` — INSTALL-01 (wizard seam + accessible-mode driver)
- [ ] `cmd/villa/tui_theme.go` + `tui_theme_test.go` — D-09/D-10 (theme + NO_COLOR variant)
- [ ] `make build-static` target — SC#4
- [ ] `.github/workflows/ci.yml` — SC#4 CGO-free gate + bubbletea-v1 assertion (none exists today)
- [ ] `safeAutoFix(id) bool` classifier + test (D-05, interpretation 1)

## Security Domain

> `security_enforcement` not found disabled in config; included. The wizard adds no new attack surface — it is local, no network, no new bind.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | local single-user CLI |
| V3 Session Management | no | n/a |
| V4 Access Control | no | n/a |
| V5 Input Validation | yes | Model choice is constrained to catalog ids from `recommend.Pick` (no free-text into commands); consent is y/n only. |
| V6 Cryptography | no | no new crypto; `go.sum` integrity via `go mod verify`. |
| V10 Malicious Code / Supply Chain | yes | One new dep from a known org via the authoritative Go proxy; `go.sum` + `go mod verify` gate; bubbletea-v2-leak CI assertion. |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Silent privileged-command execution via the wizard | Elevation of Privilege | D-04: privileged fixes ALWAYS consent-gated; `runGapFix` is fixed-arg, never a shell. |
| Shell interpolation of a model name into a command | Tampering | Catalog-resolved ids only; `ContainerArgs` builds fixed-arg slices behind the seam. |
| Dependency-confusion / slopsquat on the new dep | Tampering/Supply Chain | Pinned `huh@v1.0.0` via Go proxy + `go.sum` + `go mod verify`; org is `charmbracelet`. |
| Backend-literal leak out of the seam via TUI text | Tampering (contract erosion) | `TestSeamGrepGate` walks `cmd/villa`; render names via `Backend.Name()`/`.Image()`. |

## Sources

### Primary (HIGH confidence)
- Go module proxy `proxy.golang.org/github.com/charmbracelet/huh/@v/v1.0.0.mod` — confirmed huh v1.0.0 requires bubbletea v1.3.6, lipgloss v1.1.0; full transitive list (incl. creack/pty, mattn/go-isatty, muesli/termenv).
- `github.com/charmbracelet/huh@v1.0.0` `form.go` — `Run`/`RunWithContext`/`WithInput`/`WithOutput`/`WithAccessible`/`WithTheme`/`WithProgramOptions`; `TERM=dumb` → accessible; default output STDERR when interactive.
- `github.com/charmbracelet/huh@v1.0.0` `theme.go` / `field_select.go` — `ThemeBase`/`ThemeCharm`/...; FieldStyles (Focused/Blurred, SelectSelector, Title); `Field.RunAccessible` (field-level `WithAccessible` deprecated).
- `github.com/charmbracelet/lipgloss@v1.1.0` `renderer.go` — `SetColorProfile(termenv.Profile)`, `ColorProfile()` auto-detect, `termenv.Ascii`.
- `github.com/muesli/termenv@v0.16.0` `termenv.go` — `EnvNoColor()` honors `NO_COLOR`/`CLICOLOR` (no-color.org / bixense clicolors).
- Codebase (read this session): `cmd/villa/install.go`, `install_hostprep.go`, `install_test.go`, `internal/recommend/recommend.go`, `internal/inference/{backend,inference}.go`, `internal/inference/seam_test.go`, `internal/preflight/*`, `Makefile`, `go.mod`.

### Secondary (MEDIUM confidence)
- charmbracelet/huh README patterns (NewForm/NewGroup/NewSelect/NewConfirm/NewNote) — standard usage corroborated by source.

### Tertiary (LOW confidence)
- None relied upon for any load-bearing claim.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — huh v1.0.0 + transitive pins verified against the Go proxy go.mod and source.
- Architecture/wiring: HIGH — derived from reading the actual `runInstall`/`installDeps` seam and recommend/inference/preflight APIs.
- Pitfalls (huh output/accessible/TERM=dumb): HIGH — verified in huh v1.0.0 source at the pinned tag.
- D-05 contract finding: HIGH on the code facts (both fixes privileged; no install golden); the privilege classification of `enable-linger` is `[ASSUMED]` conservative — flagged.

**Research date:** 2026-06-08
**Valid until:** 2026-07-08 (stable pinned deps; 30 days)
