# Phase 17: Guided TUI Install (Capstone) - Pattern Map

**Mapped:** 2026-06-08
**Files analyzed:** 9 (4 new source + 2 new test + 2 modified source/build + 1 new CI; go.mod/go.sum dep bump)
**Analogs found:** 6 with in-repo analogs / 9 total (3 greenfield: theme, theme-test scaffolding, CI)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `cmd/villa/install_wizard.go` (NEW) | command-tier presentation seam | request-response (collect→thread) | `cmd/villa/install.go` (`installDeps` seam + `liveInstallDeps`) | role-match (greenfield TUI body, exact seam pattern) |
| `cmd/villa/install_wizard_test.go` (NEW) | test | request-response | `cmd/villa/install_test.go` (`fakeInstallDeps`, call-count/substring asserts) | exact (test harness) |
| `cmd/villa/tui_theme.go` (NEW) | utility (pure helper + env-degradation) | transform | none (first lipgloss use) — closest: `cmd/villa/install_hostprep.go` thin env/TTY helpers (`stdinIsInteractive`) | partial / greenfield |
| `cmd/villa/tui_theme_test.go` (NEW) | test | transform | `cmd/villa/install_test.go` table-driven asserts | role-match |
| `cmd/villa/install.go` (MODIFY) | command (gate branch + flag + classifier) | request-response | self — `runInstall` head, `newInstall` flags, `hasAutomatedFix`, `liveInstallDeps` | exact (in-file extension) |
| `Makefile` (MODIFY) | config/build | batch | self — existing `build` target | exact |
| `.github/workflows/ci.yml` (NEW) | config (CI) | batch | none (no CI in repo) | greenfield — map to `make check` + research YAML |
| `go.mod` / `go.sum` (MODIFY) | config (deps) | n/a | self — existing `require` blocks | exact |

## Pattern Assignments

### `cmd/villa/install_wizard.go` (command-tier presentation seam)

**Analog:** `cmd/villa/install.go` — the `installDeps` seam struct + `liveInstallDeps()` constructor.

**Seam-struct + field-doc pattern** (`install.go:73-141`): every host/interaction effect is a `func` field on `installDeps` with a doc comment. The wizard adds ONE new field of the same shape (per research wiring section). Mirror this exact style:
```go
// installDeps are the injectable seams runInstall drives. Defaults wire the real
// host (liveInstallDeps); install_test.go replaces them with stubs.
type installDeps struct {
	probe       func() detect.HostProfile
	pick        func(detect.HostProfile) recommend.Recommendation
	...
	interactive func() bool
	consent     func(prompt string) bool
	// NEW (Phase 17): wizard runs the huh form flow on a TTY; tests inject a fake
	// returning a canned wizardResult. Mirrors interactive/consent — no internal/* import.
	// wizard func(ctx context.Context, in wizardInput) (wizardResult, error)
}
```

**Live wiring pattern** (`install.go:639-744`, `liveInstallDeps`): construct the `&installDeps{...}` literal field-by-field, wiring each seam to its real implementation. The wizard's live impl builds the huh form and runs it; wire it here alongside `interactive: stdinIsInteractive, consent: promptConsent`:
```go
return &installDeps{
	probe:       detect.Probe,
	...
	interactive: stdinIsInteractive,
	consent:     promptConsent,
	pollReady:   liveReadinessPoll,
	// wizard:   liveWizard,   // NEW — builds huh.NewForm(...).WithTheme(villaTheme(...)).Run()
}, nil
```

**Backend-name rendering via accessor (NOT a literal)** — `inference.Backend` interface (`internal/inference/inference.go:54-72`) exposes `Name()` and `Image()`. The review screen MUST render via these accessors, never a retyped `"kyuz0/..."`/image literal (`TestSeamGrepGate` walks `cmd/villa`, `seam_test.go:129-154`). The bare config VALUE `"vulkan"` is seam-clean and allowed; the IMAGE literal is not:
```go
backend, err := inference.BackendFor(cfg.Backend) // backend.go:24 — single polymorphism point
// review line: fmt.Sprintf("backend: %s", backend.Name())  // OK
// review line: fmt.Sprintf("will pull: %s", backend.Image()) // OK — accessor, not a literal
```

**Model-options from recommend.Pick (D-02)** — `recommend.Recommendation` (`recommend.go:67-103`) carries `Model/Quant/ContextLen` plus `Alternatives []Alternative` (`recommend.go:105-111`: `Model/Quant/ContextLen/TotalBytes`). Build the Select options from `rec` + `rec.Alternatives`; re-run the chosen id through `recommend.Pick(profile, cat, recommend.Overrides{Model: chosen})` (`recommend.go:113-123`). The wizard computes no fit — it presents `Pick` output:
```go
rec := d.pick(profile)
opts := []huh.Option[string]{huh.NewOption(fmt.Sprintf("%s · %s · ctx %d  (recommended)", rec.Model, rec.Quant, rec.ContextLen), rec.Model)}
for _, a := range rec.Alternatives { opts = append(opts, huh.NewOption(fmt.Sprintf("%s · %s · ctx %d", a.Model, a.Quant, a.ContextLen), a.Model)) }
```

**Privileged consent reuse (D-04)** — do NOT add prompt logic. The gap screen drives the EXISTING `installDeps.consent`/`interactive` + `runGapFix` fixed-arg seam (`install.go:516-527, 564-573`). A privileged BLOCK gap presents a `huh.NewConfirm`; on affirm it calls the SAME `resolveGap` path. `runGapFix` is fixed-arg (never a shell): `case "PRE-05": return d.setsebool()` / `case "PRE-03": return d.enableLinger(...)`.

---

### `cmd/villa/install_wizard_test.go` (test)

**Analog:** `cmd/villa/install_test.go` — `fakeInstallDeps` + counter-based assertions.

**Fake-deps embedding + counters** (`install_test.go:27-144`, `newFakeInstallDeps`): embed `*installDeps`, add `int` call counters and an order slice; default every seam to a non-host stub. Add a `wizardCalls int` counter + a `wizardResult` field so a test asserts the wizard seam fired (or did NOT, on the fallback path):
```go
type fakeInstallDeps struct {
	*installDeps
	writeCalls int; startCalls int; ...
	wizardCalls int        // NEW — assert the wizard was/ wasn't invoked
	savedCfg config.VillaConfig  // existing — reuse to prove wizard config == flag-path config
}
d.wizard = func(context.Context, wizardInput) (wizardResult, error) { f.wizardCalls++; return cannedResult, nil }
```

**Driving `runInstall` + asserting exit/counts/substrings** (`install_test.go:177-247`): there is NO install golden — tests build `installTestCmd()` (`install_test.go:146-152`, a cobra.Command with buffered Out/Err), call `runInstall(cmd, installOpts{...}, f.installDeps)`, then assert the returned exit code, seam call-counts, and `strings.Contains(out.String(), ...)`. Mirror this exactly:
```go
cmd, out, _ := installTestCmd()
code := runInstall(cmd, installOpts{}, f.installDeps)   // TTY+!json+!noTUI fake → wizard fires
if code != exitPass { t.Fatalf("exit = %d, want 0", code) }
if f.wizardCalls != 1 { t.Errorf("wizard fired %d times, want 1", f.wizardCalls) }
```

**The wizard==flag-path config test (SC#1/SC#2)**: reuse the existing `savedCfg` capture seam (`install_test.go:96`: `d.saveConfig = func(c) { f.savedCfg = c }`). Drive both paths (wizard on / `--no-tui`) through `runInstall` with identical inputs and compare `f.savedCfg` byte-for-byte.

**huh accessible-mode driver (greenfield, from RESEARCH Pattern 2)** — to test the LIVE form (not just the fake seam), build it with `WithInput(strings.NewReader(...)).WithOutput(io.Discard).WithAccessible(true)` and assert the bound `*chosen`/`*doInstall` vars. Use `WithOutput(io.Discard)` because huh renders to STDERR by default (RESEARCH Pitfall 1).

---

### `cmd/villa/tui_theme.go` (utility — greenfield)

**Analog:** none in-repo (first lipgloss use). Closest stylistic analog: `cmd/villa/install_hostprep.go` — thin, single-purpose env/TTY helpers with a file-level doc comment and `os.ModeCharDevice` checks.

**`stdoutIsTTY()` mirrors `stdinIsInteractive()`** (`install_hostprep.go:49-55`) — the new helper this phase needs is the stdout twin; copy the pattern against `os.Stdout`:
```go
// stdinIsInteractive (existing, install_hostprep.go:49):
func stdinIsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil { return false }
	return (fi.Mode() & os.ModeCharDevice) != 0
}
// stdoutIsTTY (NEW) — identical check against os.Stdout (huh renders to stdout/stderr).
```

**Theme constructor shape (from RESEARCH Pattern 3 + UI-SPEC Theme File Contract)** — `villaTheme(colorEnabled bool) *huh.Theme` from `huh.ThemeBase()`; when `!colorEnabled`, call `lipgloss.SetColorProfile(termenv.Ascii)` and return base (keeps bold/faint/underline). Accent `AdaptiveColor{Light:"63", Dark:"105"}`; status PASS green / WARN amber / BLOCK red per UI-SPEC Color table. Also expose the `Step N/M` renderer + status-glyph helpers (Unicode `✓`/`!`/`✗` with `[OK]`/`[WARN]`/`[BLOCK]` ASCII fallback, UI-SPEC Iconography). NO `internal/*` import (D-11).

**File-level doc-comment convention** (every file opens with a role comment, see `install.go:23-39`, `backend.go:5-15`): open `tui_theme.go` with a doc comment stating it is the single command-tier TUI styling source and is NO_COLOR-degradable (D-09/D-10).

---

### `cmd/villa/tui_theme_test.go` (test)

**Analog:** `cmd/villa/install_test.go` table-driven structure (Go stdlib `testing`, no third-party assert).

**Test the two variants** (D-09): a color-on case (normal TERM) asserting accent/status styles are non-empty, and a degraded case (`NO_COLOR` set OR `colorEnabled=false`) asserting `Foreground` is stripped while bold/underline/glyph-column survive. Set env via `t.Setenv("NO_COLOR", "1")`. Assert glyph fallback returns `[OK]`/`[WARN]`/`[BLOCK]` when ASCII mode is forced.

---

### `cmd/villa/install.go` (MODIFY — gate branch + flag + classifier)

**Analog:** self.

**Current `runInstall` head** (`install.go:186-202`) — the TTY-gate branch is inserted AFTER `out`/`errOut` resolve and BEFORE step (1) `d.probe()`:
```go
func runInstall(cmd *cobra.Command, opts installOpts, d *installDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	// >>> INSERT GATE HERE (D-01/D-08):
	// useWizard := d.interactive() && !opts.json && !opts.noTUI && d.stdoutIsTTY()
	// if useWizard { res, err := d.wizard(cmd.Context(), wizardInput{...}); ... thread res into Overrides/consent }
	profile := d.probe()           // (1) existing body runs unchanged for BOTH paths
	rec := d.pick(profile)         // (2)
	...
}
```

**`installOpts` flag block** (`install.go:56-63`) — add `noTUI bool`:
```go
type installOpts struct {
	dryRun bool
	force  bool
	json   bool
	// noTUI bool  // NEW — --no-tui opts out to the flag-driven path (D-01/D-08)
}
```

**`newInstall` flag registration + threading** (`install.go:156-181`) — mirror the existing `--dry-run` registration and the `installOpts{...}` literal:
```go
cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the rendered units ...")
// cmd.Flags().BoolVar(&noTUI, "no-tui", false, "skip the guided wizard; use the flag-driven install path")  // NEW
...
code := runInstall(cmd, installOpts{dryRun: dryRun, force: force, json: jsonOut /*, noTUI: noTUI*/}, deps)
```

**`safeAutoFix(id) bool` classifier (D-05)** — model it on the EXISTING `hasAutomatedFix` switch (`install.go:578-585`); per RESEARCH interpretation (1) it returns `false` for both current fixes (PRE-03, PRE-05 are privileged):
```go
// hasAutomatedFix (existing, install.go:578):
func hasAutomatedFix(id string) bool {
	switch id { case "PRE-05", "PRE-03": return true; default: return false }
}
// safeAutoFix (NEW) — true ONLY for a non-privileged fix that may auto-run with a notice.
// Returns false for PRE-03/PRE-05 (both privileged → still consent-gated, D-04).
func safeAutoFix(id string) bool { switch id { /* no current safe fix */ default: return false } }
```
Wire `safeAutoFix` into `gateInstall` (`install.go:442-468`): a gap where `safeAutoFix(c.ID)` auto-runs `runGapFix` with a visible notice (respecting `opts.json`/`!interactive()` non-interactive guard); everything else keeps today's consent path. Per RESEARCH, with no current safe fix this is a no-op on behavior — only the new `safeAutoFix`-returns-false test is added; existing tests (`TestInstallWarnLingerOfferGoesToStdout`, `TestInstallConsentYesRunsSeamOncePerGap`) stay green.

**`liveInstallDeps` gets `stdoutIsTTY` + `wizard` wiring** (`install.go:639-744`) — add the two new seams to the returned literal alongside `interactive: stdinIsInteractive`.

**Invariants to preserve** (RESEARCH D-05 finding): 0/2/1 exit via `exitPass`/`exitWarn`/`exitBlocked` (`gateInstall` returns these, `install.go:470-499`); `--json`/non-interactive never prompts AND never auto-runs a privileged fix (`resolveGap:511`, `offerNonBlockingGap:542`); BLOCK without `--force` → `exitBlocked`, no mutation.

---

### `Makefile` (MODIFY — add `build-static`)

**Analog:** self — existing `build` target (`Makefile:20-22`):
```make
.PHONY: build
build: ## Build the villa control-plane CLI to ./villa (version-stamped)
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/$(BINARY)
```
Add the CGO-free twin (SC#4, RESEARCH Makefile addition). Note: `LDFLAGS`/`BINARY`/`VERSION` are already defined at the top (`Makefile:1-10`); CGO disabled is already the project posture (comment at `Makefile:5-8`):
```make
.PHONY: build-static
build-static: ## Build a CGO-free static binary (SC#4 — must succeed with huh added)
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/$(BINARY)
```

---

### `.github/workflows/ci.yml` (NEW — greenfield)

**Analog:** none — `.github/` does not exist in the repo. Map to `make check` (`Makefile:40-41`: `vet test`) + the RESEARCH CI YAML. Required steps (RESEARCH CGO-free + bubbletea-v1 assertion):
```yaml
- run: CGO_ENABLED=0 go build ./...            # SC#4 static build gate
- run: go vet ./... && go test ./...            # mirrors `make check`
- run: go mod verify                            # supply-chain integrity
- run: grep -q 'bubbletea v1' go.mod || (echo "bubbletea v2 leaked!" && exit 1)  # D-11 / Pitfall 4
```

---

### `go.mod` / `go.sum` (MODIFY — add `charmbracelet/huh v1.0.0`)

**Analog:** self — existing `require` blocks (`go.mod:5-21`). Add `github.com/charmbracelet/huh v1.0.0` to the direct `require` block; `bubbletea v1.3.6` / `lipgloss v1.1.0` / `termenv v0.16.0` land as transitive `// indirect` (lipgloss/termenv become direct if imported in `tui_theme.go`). Run `go get github.com/charmbracelet/huh@v1.0.0 && go mod tidy && go mod verify`. NEVER `go get bubbletea@latest` (would pull v2 — Pitfall 4). Current direct deps for reference:
```
require (
	github.com/BurntSushi/toml v1.6.0
	github.com/go-chi/chi/v5 v5.3.0
	github.com/jaypipes/ghw v0.24.0
	github.com/spf13/cobra v1.10.2
	// github.com/charmbracelet/huh v1.0.0   // NEW (D-11)
)
```

## Shared Patterns

### Injectable seam + `live*Deps` / `fake*Deps`
**Source:** `cmd/villa/install.go:73-141` (struct) + `:639-744` (`liveInstallDeps`); `cmd/villa/install_test.go:27-144` (`fakeInstallDeps`).
**Apply to:** `install_wizard.go` (new `wizard`/`stdoutIsTTY` seams), `install_wizard_test.go`.
Every host/interaction effect is a `func` field; live wiring is a `&installDeps{...}` literal in `cmd/villa`; tests stub field-by-field with counters. No `internal/*` import of huh (D-11).

### Backend literal stays behind the seam (`TestSeamGrepGate`)
**Source:** `internal/inference/seam_test.go:34-155` (gate walks `cmd/villa`); `internal/inference/inference.go:54-72` (`Name()`/`Image()` accessors); `internal/inference/backend.go:24` (`BackendFor`).
**Apply to:** `install_wizard.go`, `tui_theme.go` (any review/status text). Render backend name via `backend.Name()` / image via `backend.Image()`; never retype `kyuz0`/`docker.io/`/`ROCm0`/`HSA_OVERRIDE_GFX_VERSION`. The config VALUE `"vulkan"` is allowed.

### File-level doc comment + decision-ID references
**Source:** `install.go:23-52`, `backend.go:5-15`, `seam_test.go:11-33`.
**Apply to:** all new files. Open with a role comment; cite decision IDs (`D-01`..`D-11`, `INSTALL-01/02`, `SC#1-4`) inline as the canonical traceability tokens.

### Return-not-Exit + 0/2/1 exit contract
**Source:** `install.go:186` (`runInstall` RETURNS int) + `:470-499`/`:315-355` (`exitPass`/`exitWarn`/`exitBlocked`); cobra `RunE` calls `os.Exit` (`install.go:168-177`).
**Apply to:** the wizard abort path — Esc/Ctrl+C maps to a non-zero RETURN from `runInstall` (no `os.Exit` inside), with a "use --no-tui" remediation hint (UI-SPEC Copywriting).

### Test via buffered cobra.Command + substring/call-count asserts (NO golden)
**Source:** `install_test.go:146-152` (`installTestCmd`), `:177-247` (asserts). RESEARCH + `ls testdata/` confirm: no install golden exists.
**Apply to:** `install_wizard_test.go`, `tui_theme_test.go`. Assert exit code, seam counters, and `strings.Contains(out.String(), ...)` — there is no byte-frozen contract to re-freeze for install.

## No Analog Found

Files with no close in-repo match (planner uses RESEARCH.md patterns):

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `cmd/villa/tui_theme.go` | utility | transform | First lipgloss/huh-theme use in the repo. Seam/doc-comment/TTY-helper conventions DO apply (mapped above); the theme body itself is greenfield — use RESEARCH Pattern 3 + UI-SPEC Theme File Contract. |
| `.github/workflows/ci.yml` | config (CI) | batch | No `.github/` exists. Map intent to `make check` (`Makefile:40`); use RESEARCH CI YAML for the CGO-free + bubbletea-v1 steps. |
| `cmd/villa/install_wizard.go` (huh form body) | presentation | request-response | The `installDeps` SEAM is an exact analog; the huh `NewForm`/`NewGroup`/`NewSelect`/`NewConfirm`/`NewNote` BODY is greenfield — use RESEARCH Pattern 1 + UI-SPEC Screen Sequence (5 steps). |

## Metadata

**Analog search scope:** `cmd/villa/` (install, install_hostprep, install_test, recommend), `internal/inference/` (backend, inference, seam_test), `internal/recommend/`, `Makefile`, `go.mod`, repo root (`.github` absence, `testdata` install-golden absence).
**Files scanned:** 10
**Pattern extraction date:** 2026-06-08
