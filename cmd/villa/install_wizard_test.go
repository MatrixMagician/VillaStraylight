package main

import (
	"context"
	"io"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// install_wizard_test.go is the automated half of the Phase-17 test map
// (INSTALL-01/02). It proves the phase's observable signals off-hardware: the
// wizard fires on a TTY, the three fallback conditions (--no-tui / --json /
// non-TTY) bypass it, the wizard- and flag-path config.toml are byte-identical
// (SC#1/SC#2), a BLOCK-gap + privileged-consent scenario through the LIVE
// composition runs the privileged seam at most once with the preserved 0/2/1
// verdict (zero on denial, D-04/D-06), the live huh form drives in accessible
// mode, and safeAutoFix returns false for both current privileged fixes (D-05).
// There is NO install golden — assertions are exit code + seam call-counts +
// strings.Contains (Patterns "Test via buffered cobra.Command, no golden").

// TestInstallWizardFires: on a TTY (interactive stdin + stdout TTY, no --json,
// no --no-tui) the wizard seam is invoked exactly once and install completes
// with exitPass (Observable signal 1 / SC#3). The default fake wizard returns an
// empty wizardResult (no override, nil consent), so the install proceeds through
// the single gate exactly as the flag path does.
func TestInstallWizardFires(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.installDeps.interactive = func() bool { return true }
	f.installDeps.stdoutIsTTY = func() bool { return true }

	cmd, _, _ := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitPass {
		t.Fatalf("wizard-path install exit = %d, want exitPass (%d)", code, exitPass)
	}
	if f.wizardCalls != 1 {
		t.Errorf("wizard seam fired %d times on a TTY, want exactly 1", f.wizardCalls)
	}
}

// TestInstallGateBypassesWizard: each of --no-tui, --json, and a non-TTY stdout
// bypasses the wizard seam (0 invocations) and runs the flag path (the install
// still writes units + persists config). This is Observable signal 2 / SC#3 — the
// graceful fallback that keeps the existing flag path verbatim (INSTALL-02).
func TestInstallGateBypassesWizard(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	cases := []struct {
		name string
		opts installOpts
		tty  bool // stdoutIsTTY result
	}{
		// --no-tui: interactive TTY but the user opted out of the wizard.
		{name: "no-tui", opts: installOpts{noTUI: true}, tty: true},
		// --json: a JSON run is non-interactive; the wizard must never fire.
		{name: "json", opts: installOpts{json: true}, tty: true},
		// non-TTY stdout: piped/redirected output → no styled wizard.
		{name: "non-tty-stdout", opts: installOpts{}, tty: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeInstallDeps(t, units, plan, passChecks())
			// interactive stdin is true for all cases so the ONLY thing turning the
			// wizard off is the bypass condition under test.
			f.installDeps.interactive = func() bool { return true }
			f.installDeps.stdoutIsTTY = func() bool { return tc.tty }

			cmd, _, _ := installTestCmd()
			code := runInstall(cmd, tc.opts, f.installDeps)
			if code != exitPass {
				t.Fatalf("%s bypass exit = %d, want exitPass (%d)", tc.name, code, exitPass)
			}
			if f.wizardCalls != 0 {
				t.Errorf("%s must bypass the wizard, but the seam fired %d times", tc.name, f.wizardCalls)
			}
			// The flag path ran: config persisted + units written (the happy-path seams).
			if f.saveCalls != 1 || f.writeCalls != 1 {
				t.Errorf("%s must run the flag path (save=1 write=1), got save=%d write=%d", tc.name, f.saveCalls, f.writeCalls)
			}
		})
	}
}

// TestWizardConfigMatchesFlagPath: the config.toml the wizard path persists is
// byte-identical to the flag path's for identical inputs (SC#1/SC#2). Both paths
// receive the same recommendation (the fake wizard returns an empty override +
// nil consent), so they converge on the single gateInstall and persist the same
// VillaConfig. Drives both through runInstall and compares the captured savedCfg.
func TestWizardConfigMatchesFlagPath(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	// Wizard path: interactive + TTY, no --no-tui → the wizard seam fires (empty
	// override, nil consent), then the single gate persists the recommended config.
	fw := newFakeInstallDeps(t, units, plan, passChecks())
	fw.installDeps.interactive = func() bool { return true }
	fw.installDeps.stdoutIsTTY = func() bool { return true }
	cmdW, _, _ := installTestCmd()
	if code := runInstall(cmdW, installOpts{}, fw.installDeps); code != exitPass {
		t.Fatalf("wizard-path install exit = %d, want exitPass", code)
	}
	if fw.wizardCalls != 1 {
		t.Fatalf("wizard-path setup error: wizard fired %d times, want 1", fw.wizardCalls)
	}

	// Flag path: --no-tui forces today's flag path verbatim.
	ff := newFakeInstallDeps(t, units, plan, passChecks())
	ff.installDeps.interactive = func() bool { return true }
	ff.installDeps.stdoutIsTTY = func() bool { return true }
	cmdF, _, _ := installTestCmd()
	if code := runInstall(cmdF, installOpts{noTUI: true}, ff.installDeps); code != exitPass {
		t.Fatalf("flag-path install exit = %d, want exitPass", code)
	}
	if ff.wizardCalls != 0 {
		t.Fatalf("flag-path setup error: wizard fired %d times, want 0", ff.wizardCalls)
	}

	// SC#1/SC#2: the persisted config.toml is byte-identical across both paths.
	if fw.savedCfg != ff.savedCfg {
		t.Errorf("wizard-path config %+v must byte-match flag-path config %+v (SC#1/SC#2)", fw.savedCfg, ff.savedCfg)
	}
}

// TestInstallWizardPathRunsGateOnce is the single-gate / consent-threading guard
// (Blocker 3). It drives runInstall on the WIZARD path through the LIVE composition:
// the wizard SEAM stands in for the huh run (which needs a TTY) and returns the
// collected consent decisions, but the REST of the composition — the single
// gateInstall consuming the threaded map → resolveGap → runGapFix → d.setsebool —
// runs for real. It proves: (a) on consent-granted the privileged seam fires
// EXACTLY once (no double-gate, no wizard-side execution) with the preserved
// 0/2/1 verdict; (b) on consent-denied the seam fires ZERO times and the install
// exits blocked; and (c) d.consent is NEVER re-invoked on the threaded path (huh
// already consumed stdin) — a fail-the-test consent stub proves no re-prompt.
func TestInstallWizardPathRunsGateOnce(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	// failConsent fails the test if the gate ever falls back to the stdin prompt on
	// the threaded wizard path — the recorded decision must be honored WITHOUT a
	// re-prompt (D-04).
	failConsent := func(prompt string) bool {
		t.Errorf("d.consent must NOT be called on the threaded wizard path (re-prompt for %q)", prompt)
		return false
	}

	t.Run("consent-granted-runs-seam-once", func(t *testing.T) {
		// A single BLOCK-tier privileged gap (SELinux off → PRE-05 → d.setsebool).
		f := newFakeInstallDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
		f.installDeps.interactive = func() bool { return true }
		f.installDeps.stdoutIsTTY = func() bool { return true }
		f.installDeps.consent = failConsent
		// The wizard seam simulates the real collector's output: consent GRANTED.
		f.installDeps.wizard = func(context.Context, wizardInput) (wizardResult, error) {
			f.wizardCalls++
			return wizardResult{consentDecisions: map[string]bool{"PRE-05": true}}, nil
		}

		cmd, _, _ := installTestCmd()
		code := runInstall(cmd, installOpts{}, f.installDeps)
		// Preserved verdict: a consented-and-applied BLOCK gap on a clean bring-up is
		// the same exitPass the flag-path TestInstallConsentYesRunsSeamOncePerGap asserts.
		if code != exitPass {
			t.Fatalf("consent-granted wizard install exit = %d, want exitPass (%d)", code, exitPass)
		}
		if f.wizardCalls != 1 {
			t.Errorf("wizard seam fired %d times, want exactly 1", f.wizardCalls)
		}
		// The privileged seam ran EXACTLY once — via the single gateInstall→resolveGap→
		// runGapFix path, never twice (no double-gate, no wizard-side execution).
		if f.seboolCalls != 1 {
			t.Errorf("setsebool invoked %d times on the wizard path, want exactly 1 (single gate)", f.seboolCalls)
		}
		// The gap was satisfied → install proceeded to write + start.
		if f.writeCalls != 1 {
			t.Errorf("consent-granted wizard install must write units once, wrote %d times", f.writeCalls)
		}
	})

	t.Run("consent-denied-never-runs-seam-and-blocks", func(t *testing.T) {
		f := newFakeInstallDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
		f.installDeps.interactive = func() bool { return true }
		f.installDeps.stdoutIsTTY = func() bool { return true }
		f.installDeps.consent = failConsent
		// The wizard seam returns consent DENIED for the BLOCK gap.
		f.installDeps.wizard = func(context.Context, wizardInput) (wizardResult, error) {
			f.wizardCalls++
			return wizardResult{consentDecisions: map[string]bool{"PRE-05": false}}, nil
		}

		cmd, _, _ := installTestCmd()
		code := runInstall(cmd, installOpts{}, f.installDeps)
		// A denied BLOCK gap with no --force → exitBlocked, no mutation (D-04).
		if code != exitBlocked {
			t.Fatalf("consent-denied wizard install exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		if f.seboolCalls != 0 {
			t.Errorf("denied gap must NOT run setsebool, ran %d times", f.seboolCalls)
		}
		if f.writeCalls != 0 || f.startCalls != 0 {
			t.Errorf("a blocked wizard install must not write/start: write=%d start=%d", f.writeCalls, f.startCalls)
		}
	})
}

// TestInstallWizardAccessibleDriver drives the REAL huh form (not the fake seam)
// off-hardware via accessible mode: WithInput(scriptedReader) + WithOutput(io.Discard)
// + WithAccessible(true). It feeds line-based answers (Pitfall 2 — NOT raw ANSI
// arrows): the model Select reads a 1-based option index, each privileged-gap
// Confirm and the final Install Confirm read y/n. It then asserts the bound
// chosen / consents / doInstall hold the scripted selections. WithOutput(io.Discard)
// because huh renders to STDERR by default (Pitfall 1).
func TestInstallWizardAccessibleDriver(t *testing.T) {
	backend, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatalf("resolve backend: %v", err)
	}
	// rec + one alternative so the Select has two options (recommended=1, alt=2).
	rec := recommend.Recommendation{
		Model: "qwen2.5-0.5b", Quant: "Q4_K_M", ContextLen: 4096, Backend: "vulkan",
		Alternatives: []recommend.Alternative{
			{Model: "qwen2.5-1.5b", Quant: "Q4_K_M", ContextLen: 8192},
		},
	}
	in := wizardInput{
		profile:      detect.HostProfile{},
		rec:          rec,
		alternatives: rec.Alternatives,
		// One privileged BLOCK gap (PRE-05) so screen 3 gets one Confirm.
		checks:       []preflight.CheckResult{seloffCheck()},
		backend:      backend,
		colorEnabled: false,
	}

	var chosen string
	var holders []*gapConsentValue
	var doInstall bool
	form := buildWizardForm(in, &chosen, &holders, &doInstall)

	// Accessible-mode script, one line per visited field in order:
	//   screen 2 Select → "2" (the alternative qwen2.5-1.5b)
	//   screen 3 PRE-05 Confirm → "y" (run the privileged host-prep)
	//   screen 4 Install Confirm → "y" (proceed)
	// Notes consume no input. huh's accessible runner builds a FRESH bufio.Scanner
	// per field over the same reader; a plain strings.Reader hands the whole script
	// to the first field's scanner buffer, starving later fields (they fall back to
	// defaults). lineReader returns at most one line per Read so each field's scanner
	// only ever buffers its own line — the off-hardware analog of typing answers one
	// at a time at the prompt.
	form = form.WithInput(&lineReader{lines: []string{"2", "y", "y"}}).WithOutput(io.Discard).WithAccessible(true)
	if err := form.Run(); err != nil {
		t.Fatalf("accessible-mode form.Run: %v", err)
	}

	if chosen != "qwen2.5-1.5b" {
		t.Errorf("scripted Select bound chosen=%q, want the alternative %q", chosen, "qwen2.5-1.5b")
	}
	if !doInstall {
		t.Errorf("scripted final Install confirm bound doInstall=%v, want true", doInstall)
	}
	// Reconcile the holders into a consents map exactly as liveWizard does, then
	// assert the scripted privileged-gap decision bound true.
	consents := map[string]bool{}
	for _, h := range holders {
		consents[h.id] = h.val
	}
	if got, ok := consents["PRE-05"]; !ok || !got {
		t.Errorf("scripted PRE-05 confirm bound consents=%v, want PRE-05=true", consents)
	}
}

// lineReader hands out exactly one scripted line (newline-terminated) per Read
// call, then io.EOF. huh's accessible-mode runner constructs a fresh bufio.Scanner
// for every field over the SAME input reader; a strings.Reader would let the first
// field's scanner buffer the entire script and starve the rest. By returning one
// line per Read, each per-field scanner reads only its own answer — modelling a user
// typing one prompt at a time. It is the canonical headless-driver input for the
// accessible-mode form test (Pitfall 2).
type lineReader struct {
	lines []string
	i     int
}

func (lr *lineReader) Read(p []byte) (int, error) {
	if lr.i >= len(lr.lines) {
		return 0, io.EOF
	}
	line := lr.lines[lr.i] + "\n"
	lr.i++
	n := copy(p, line)
	return n, nil
}

// TestSafeAutoFixReturnsFalseForPrivilegedFixes pins the conservative D-05
// classification (interpretation 1): both current fixes — PRE-05 (setsebool -P)
// and PRE-03 (loginctl enable-linger) — are PRIVILEGED and so are NOT safe to
// auto-run. safeAutoFix must return false for both; a future reclassification to
// true must be a deliberate, test-visible change.
func TestSafeAutoFixReturnsFalseForPrivilegedFixes(t *testing.T) {
	for _, id := range []string{"PRE-03", "PRE-05"} {
		if safeAutoFix(id) {
			t.Errorf("safeAutoFix(%q) = true, want false (privileged → consent-gated, D-05/D-04)", id)
		}
	}
}
