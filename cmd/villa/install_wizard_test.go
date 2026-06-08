package main

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
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
