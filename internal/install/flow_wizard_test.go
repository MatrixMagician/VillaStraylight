package install

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// flow_wizard_test.go proves the wizard gate at the Run interface, off-hardware:
// the wizard seam fires on a TTY, the three fallback conditions (--no-tui /
// --json / non-TTY) bypass it, the wizard and flag paths converge on the same
// gate resolution and persist the same config, a BLOCK-gap + privileged-consent
// scenario runs the privileged seam at most once with the preserved 0/2/1
// verdict (zero on denial), the no-fit refusal renders the contracted empty-state
// copy, and SafeAutoFix returns false for both current privileged fixes. The
// prompt loop itself (runWizard) is command-tier presentation and is tested there.

// TestInstallWizardFires: on a TTY (interactive stdin + stdout TTY, no --json,
// no --no-tui) the wizard seam is invoked exactly once and install completes
// with exitPass (Observable signal 1). The default fake wizard returns an
// empty WizardResult (no override, nil consent), so the install proceeds through
// the single gate exactly as the flag path does.
func TestInstallWizardFires(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())
	f.Interactive = func() bool { return true }
	f.StdoutIsTTY = func() bool { return true }

	code, _, _ := f.run(Opts{})
	if code != exitPass {
		t.Fatalf("wizard-path install exit = %d, want exitPass (%d)", code, exitPass)
	}
	if f.wizardCalls != 1 {
		t.Errorf("wizard seam fired %d times on a TTY, want exactly 1", f.wizardCalls)
	}
}

// TestInstallGateBypassesWizard: each of --no-tui, --json, and a non-TTY stdout
// bypasses the wizard seam (0 invocations) and runs the flag path (the install
// still writes units + persists config). This is Observable signal 2 / — the
// graceful fallback that keeps the existing flag path verbatim.
func TestInstallGateBypassesWizard(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	cases := []struct {
		name string
		opts Opts
		tty  bool // StdoutIsTTY result
	}{
		// --no-tui: interactive TTY but the user opted out of the wizard.
		{name: "no-tui", opts: Opts{NoTUI: true}, tty: true},
		// --json: a JSON run is non-interactive; the wizard must never fire.
		{name: "json", opts: Opts{JSON: true}, tty: true},
		// non-TTY stdout: piped/redirected output → no styled wizard.
		{name: "non-tty-stdout", opts: Opts{}, tty: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDeps(t, units, plan, passChecks())
			// interactive stdin is true for all cases so the ONLY thing turning the
			// wizard off is the bypass condition under test.
			f.Interactive = func() bool { return true }
			f.StdoutIsTTY = func() bool { return tc.tty }

			code, _, _ := f.run(tc.opts)
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
// byte-identical to the flag path's for identical inputs, and the two paths
// converge on the same gate resolution — the same Result and the same privileged
// seam counts — proven at the Run interface. Both paths receive the same
// recommendation (the fake wizard returns an empty override + nil consent), so
// they converge on the single gate and persist the same VillaConfig.
func TestWizardConfigMatchesFlagPath(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	// A real privileged gap (linger off) so the gate resolution has something to
	// converge ON: the wizard records the consent, the flag path prompts for it.
	checks := append(passChecks(), lingeroffCheck())

	// Wizard path: interactive + TTY, no --no-tui → the wizard seam fires (empty
	// override, consent recorded), then the single gate persists the recommended config.
	fw := newFakeDeps(t, units, plan, checks)
	fw.Interactive = func() bool { return true }
	fw.StdoutIsTTY = func() bool { return true }
	fw.Wizard = func(context.Context, WizardInput) (WizardResult, error) {
		fw.wizardCalls++
		return WizardResult{Consents: map[string]bool{"PRE-03": true}}, nil
	}
	resW, _, _ := fw.runResult(Opts{})
	if code := resW.Outcome.ExitCode(); code != exitPass {
		t.Fatalf("wizard-path install exit = %d, want exitPass", code)
	}
	if fw.wizardCalls != 1 {
		t.Fatalf("wizard-path setup error: wizard fired %d times, want 1", fw.wizardCalls)
	}

	// Flag path: --no-tui forces today's flag path verbatim.
	ff := newFakeDeps(t, units, plan, checks)
	ff.Interactive = func() bool { return true }
	ff.StdoutIsTTY = func() bool { return true }
	ff.Consent = func(string) bool { return true }
	resF, _, _ := ff.runResult(Opts{NoTUI: true})
	if code := resF.Outcome.ExitCode(); code != exitPass {
		t.Fatalf("flag-path install exit = %d, want exitPass", code)
	}
	if ff.wizardCalls != 0 {
		t.Fatalf("flag-path setup error: wizard fired %d times, want 0", ff.wizardCalls)
	}

	// The persisted config.toml is byte-identical across both paths.
	if !reflect.DeepEqual(fw.savedCfg, ff.savedCfg) {
		t.Errorf("wizard-path config %+v must byte-match flag-path config %+v", fw.savedCfg, ff.savedCfg)
	}
	// The two paths reach the same Result: one gate, one outcome.
	if resW != resF {
		t.Errorf("wizard-path Result %+v must equal flag-path Result %+v", resW, resF)
	}
	// And the same gate resolution: the privileged seams fired identically, and
	// the gap was actually resolved once on each path.
	if fw.lingerCalls != 1 {
		t.Errorf("wizard path: linger fix ran %d times, want 1", fw.lingerCalls)
	}
	if fw.seboolCalls != ff.seboolCalls || fw.lingerCalls != ff.lingerCalls {
		t.Errorf("gate resolution diverged: wizard sebool=%d linger=%d, flag sebool=%d linger=%d",
			fw.seboolCalls, fw.lingerCalls, ff.seboolCalls, ff.lingerCalls)
	}
}

// TestInstallWizardPathRunsGateOnce is the single-gate / consent-threading guard
// (Blocker 3). It drives Run on the WIZARD path through the LIVE composition:
// the wizard SEAM stands in for the prompt loop (which needs a TTY) and returns the
// collected consent decisions, but the REST of the composition — the single
// gate consuming the threaded map → resolveGap → runGapFix → d.Setsebool —
// runs for real. It proves: (a) on consent-granted the privileged seam fires
// EXACTLY once (no double-gate, no wizard-side execution) with the preserved
// 0/2/1 verdict; (b) on consent-denied the seam fires ZERO times and the install
// exits blocked; and (c) d.Consent is NEVER re-invoked on the threaded path (the
// wizard already consumed stdin) — a fail-the-test consent stub proves no re-prompt.
func TestInstallWizardPathRunsGateOnce(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	// failConsent fails the test if the gate ever falls back to the stdin prompt on
	// the threaded wizard path — the recorded decision must be honored WITHOUT a
	// re-prompt.
	failConsent := func(prompt string) bool {
		t.Errorf("d.Consent must NOT be called on the threaded wizard path (re-prompt for %q)", prompt)
		return false
	}

	t.Run("consent-granted-runs-seam-once", func(t *testing.T) {
		// A single BLOCK-tier privileged gap (SELinux off → PRE-05 → d.Setsebool).
		f := newFakeDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
		f.Interactive = func() bool { return true }
		f.StdoutIsTTY = func() bool { return true }
		f.Consent = failConsent
		// The wizard seam simulates the real collector's output: consent GRANTED.
		f.Wizard = func(context.Context, WizardInput) (WizardResult, error) {
			f.wizardCalls++
			return WizardResult{Consents: map[string]bool{"PRE-05": true}}, nil
		}

		code, _, _ := f.run(Opts{})
		// Preserved verdict: a consented-and-applied BLOCK gap on a clean bring-up is
		// the same exitPass the flag-path TestInstallConsentYesRunsSeamOncePerGap asserts.
		if code != exitPass {
			t.Fatalf("consent-granted wizard install exit = %d, want exitPass (%d)", code, exitPass)
		}
		if f.wizardCalls != 1 {
			t.Errorf("wizard seam fired %d times, want exactly 1", f.wizardCalls)
		}
		// The privileged seam ran EXACTLY once — via the single gate→resolveGap→
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
		f := newFakeDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
		f.Interactive = func() bool { return true }
		f.StdoutIsTTY = func() bool { return true }
		f.Consent = failConsent
		// The wizard seam returns consent DENIED for the BLOCK gap.
		f.Wizard = func(context.Context, WizardInput) (WizardResult, error) {
			f.wizardCalls++
			return WizardResult{Consents: map[string]bool{"PRE-05": false}}, nil
		}

		code, _, _ := f.run(Opts{})
		// A denied BLOCK gap with no --force → exitBlocked, no mutation.
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

// TestInstallNoFitEmitsContractedEmptyState proves the no-fit branch (recommend
// refused) emits the EXACT 17-UI-SPEC.md:195 empty-state copy verbatim — the
// usable-GiB envelope figure substituted from profile.UsableEnvelopeBytes, the
// "(--no-tui shows the same result.)" parity note, and exitBlocked preserved.
// A Known envelope renders the numeric GiB figure; a typed-Unknown envelope
// renders "unknown GiB usable" (never a fabricated 0).
func TestInstallNoFitEmitsContractedEmptyState(t *testing.T) {
	refuse := func(env detect.Bytes) *fakeDeps {
		units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
		plan := orchestrate.Plan{Changed: units}
		f := newFakeDeps(t, units, plan, passChecks())
		f.Probe = func() detect.HostProfile {
			return detect.HostProfile{UsableEnvelopeBytes: env}
		}
		// A refusing pick: empty Model is a clear no-fit.
		f.Pick = func(detect.HostProfile, recommend.Overrides) recommend.Recommendation {
			return recommend.Recommendation{}
		}
		return f
	}

	t.Run("known-envelope-renders-numeric-gib", func(t *testing.T) {
		f := refuse(detect.KnownBytes(8<<30, "mem_info_gtt_total"))
		code, _, errOut := f.run(Opts{})
		if code != exitBlocked {
			t.Fatalf("no-fit exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		got := errOut.String()
		want := "No catalog model fits the detected memory envelope (8 GiB usable). Free memory or supply a larger-envelope host, then re-run villa install. (--no-tui shows the same result.)"
		if !strings.Contains(got, want) {
			t.Errorf("no-fit output missing contracted empty-state copy.\n got: %q\nwant substring: %q", got, want)
		}
	})

	t.Run("unknown-envelope-renders-unknown", func(t *testing.T) {
		f := refuse(detect.UnknownBytes("no gtt probe", ""))
		code, _, errOut := f.run(Opts{})
		if code != exitBlocked {
			t.Fatalf("no-fit exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		got := errOut.String()
		want := "No catalog model fits the detected memory envelope (unknown GiB usable). Free memory or supply a larger-envelope host, then re-run villa install. (--no-tui shows the same result.)"
		if !strings.Contains(got, want) {
			t.Errorf("typed-Unknown no-fit output missing contracted copy.\n got: %q\nwant substring: %q", got, want)
		}
	})
}

// TestSafeAutoFixReturnsFalseForPrivilegedFixes pins the conservative
// classification (interpretation 1): both current fixes — PRE-05 (setsebool -P)
// and PRE-03 (loginctl enable-linger) — are PRIVILEGED and so are NOT safe to
// auto-run. SafeAutoFix must return false for both; a future reclassification to
// true must be a deliberate, test-visible change.
func TestSafeAutoFixReturnsFalseForPrivilegedFixes(t *testing.T) {
	for _, id := range []string{"PRE-03", "PRE-05"} {
		if SafeAutoFix(id) {
			t.Errorf("SafeAutoFix(%q) = true, want false (privileged → consent-gated)", id)
		}
	}
}
