package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// install_wizard_test.go is the automated half of the Phase-17 test map
// (02). It proves the phase's observable signals off-hardware: the
// wizard fires on a TTY, the three fallback conditions (--no-tui / --json /
// non-TTY) bypass it, the wizard- and flag-path config.toml are byte-identical
// a BLOCK-gap + privileged-consent scenario through the LIVE
// composition runs the privileged seam at most once with the preserved 0/2/1
// verdict (zero on denial), the prompt loop drives from a scripted
// stdin, and safeAutoFix returns false for both current privileged fixes.
// There is NO install golden — assertions are exit code + seam call-counts +
// strings.Contains (Patterns "Test via buffered cobra.Command, no golden").

// TestInstallWizardFires: on a TTY (interactive stdin + stdout TTY, no --json,
// no --no-tui) the wizard seam is invoked exactly once and install completes
// with exitPass (Observable signal 1). The default fake wizard returns an
// empty wizardResult (no override, nil consent), so the install proceeds through
// the single gate exactly as the flag path does.
func TestInstallWizardFires(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.interactive = func() bool { return true }
	f.stdoutIsTTY = func() bool { return true }

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
// still writes units + persists config). This is Observable signal 2 / — the
// graceful fallback that keeps the existing flag path verbatim.
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
			f.interactive = func() bool { return true }
			f.stdoutIsTTY = func() bool { return tc.tty }

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
// byte-identical to the flag path's for identical inputs. Both paths
// receive the same recommendation (the fake wizard returns an empty override +
// nil consent), so they converge on the single gateInstall and persist the same
// VillaConfig. Drives both through runInstall and compares the captured savedCfg.
func TestWizardConfigMatchesFlagPath(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	// Wizard path: interactive + TTY, no --no-tui → the wizard seam fires (empty
	// override, nil consent), then the single gate persists the recommended config.
	fw := newFakeInstallDeps(t, units, plan, passChecks())
	fw.interactive = func() bool { return true }
	fw.stdoutIsTTY = func() bool { return true }
	cmdW, _, _ := installTestCmd()
	if code := runInstall(cmdW, installOpts{}, fw.installDeps); code != exitPass {
		t.Fatalf("wizard-path install exit = %d, want exitPass", code)
	}
	if fw.wizardCalls != 1 {
		t.Fatalf("wizard-path setup error: wizard fired %d times, want 1", fw.wizardCalls)
	}

	// Flag path: --no-tui forces today's flag path verbatim.
	ff := newFakeInstallDeps(t, units, plan, passChecks())
	ff.interactive = func() bool { return true }
	ff.stdoutIsTTY = func() bool { return true }
	cmdF, _, _ := installTestCmd()
	if code := runInstall(cmdF, installOpts{noTUI: true}, ff.installDeps); code != exitPass {
		t.Fatalf("flag-path install exit = %d, want exitPass", code)
	}
	if ff.wizardCalls != 0 {
		t.Fatalf("flag-path setup error: wizard fired %d times, want 0", ff.wizardCalls)
	}

	// the persisted config.toml is byte-identical across both paths.
	if !reflect.DeepEqual(fw.savedCfg, ff.savedCfg) {
		t.Errorf("wizard-path config %+v must byte-match flag-path config %+v", fw.savedCfg, ff.savedCfg)
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
	// re-prompt.
	failConsent := func(prompt string) bool {
		t.Errorf("d.consent must NOT be called on the threaded wizard path (re-prompt for %q)", prompt)
		return false
	}

	t.Run("consent-granted-runs-seam-once", func(t *testing.T) {
		// A single BLOCK-tier privileged gap (SELinux off → PRE-05 → d.setsebool).
		f := newFakeInstallDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
		f.interactive = func() bool { return true }
		f.stdoutIsTTY = func() bool { return true }
		f.consent = failConsent
		// The wizard seam simulates the real collector's output: consent GRANTED.
		f.wizard = func(context.Context, wizardInput) (wizardResult, error) {
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
		f.interactive = func() bool { return true }
		f.stdoutIsTTY = func() bool { return true }
		f.consent = failConsent
		// The wizard seam returns consent DENIED for the BLOCK gap.
		f.wizard = func(context.Context, wizardInput) (wizardResult, error) {
			f.wizardCalls++
			return wizardResult{consentDecisions: map[string]bool{"PRE-05": false}}, nil
		}

		cmd, _, _ := installTestCmd()
		code := runInstall(cmd, installOpts{}, f.installDeps)
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

// TestWizardPromptLoopDriver drives the whole guided install off-hardware by
// scripting stdin, which is what the prompt loop replaced the accessible-mode form
// runner with. The script answers, in the order the loop asks:
//
//	"2" → the alternative model rather than the recommended pick
//	"y" → consent to the one privileged host-prep gap
//	"y" → the final Install confirm
//
// It asserts the three things the loop exists to collect: the model override, the
// per-gap consent keyed by check ID, and that it did not abort.
func TestWizardPromptLoopDriver(t *testing.T) {
	backend, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatalf("resolve backend: %v", err)
	}
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
		// One privileged BLOCK gap (PRE-05) so the loop asks exactly one consent.
		checks:       []preflight.CheckResult{seloffCheck()},
		backend:      backend,
		colorEnabled: false,
	}

	var out strings.Builder
	res, err := runWizard(t.Context(), in, strings.NewReader("2\ny\ny\n"), &out)
	if err != nil {
		t.Fatalf("runWizard: %v", err)
	}
	if res.modelOverride != "qwen2.5-1.5b" {
		t.Errorf("modelOverride = %q, want the scripted alternative %q", res.modelOverride, "qwen2.5-1.5b")
	}
	if got, ok := res.consentDecisions["PRE-05"]; !ok || !got {
		t.Errorf("consentDecisions = %v, want PRE-05=true", res.consentDecisions)
	}
	// The exact remediation command must be shown BEFORE the consent question — an
	// operator consents to a specific command, not to an unnamed "host-prep".
	if !strings.Contains(out.String(), "command: ") {
		t.Errorf("the privileged-gap question did not show its command:\n%s", out.String())
	}
}

// TestWizardKeepsRecommendedPick asserts an empty answer takes the recommended
// model and reports NO override, so runInstall does not needlessly re-run Pick.
func TestWizardKeepsRecommendedPick(t *testing.T) {
	backend, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatalf("resolve backend: %v", err)
	}
	rec := recommend.Recommendation{
		Model: "qwen2.5-0.5b", Quant: "Q4_K_M", ContextLen: 4096, Backend: "vulkan",
		Alternatives: []recommend.Alternative{{Model: "qwen2.5-1.5b", Quant: "Q4_K_M", ContextLen: 8192}},
	}
	in := wizardInput{rec: rec, alternatives: rec.Alternatives, backend: backend}

	var out strings.Builder
	res, err := runWizard(t.Context(), in, strings.NewReader("\ny\n"), &out)
	if err != nil {
		t.Fatalf("runWizard: %v", err)
	}
	if res.modelOverride != "" {
		t.Errorf("modelOverride = %q, want empty (kept the recommended pick)", res.modelOverride)
	}
}

// TestWizardCancelAndAbortNeverConsent is the safety property: neither a declined
// final confirm, nor an input stream that simply ends, may be read as approval.
// Both must return an error so runInstall aborts without mutating the host.
func TestWizardCancelAndAbortNeverConsent(t *testing.T) {
	backend, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatalf("resolve backend: %v", err)
	}
	rec := recommend.Recommendation{Model: "qwen2.5-0.5b", Quant: "Q4_K_M", ContextLen: 4096}
	base := wizardInput{rec: rec, backend: backend, checks: []preflight.CheckResult{seloffCheck()}}

	cases := map[string]struct {
		script  string
		wantErr error
	}{
		// consent y, then decline the install confirm.
		"declined install confirm": {"y\nn\n", errWizardCancelled},
		// EOF at the consent question: no answer is NOT a yes.
		"input ends at consent": {"", errWizardAborted},
		// consent y, then EOF before the final confirm.
		"input ends at final confirm": {"y\n", errWizardAborted},
		// An empty line at the final confirm takes the default, which is Cancel.
		"empty answer defaults to cancel": {"y\n\n", errWizardCancelled},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var out strings.Builder
			res, err := runWizard(t.Context(), base, strings.NewReader(tc.script), &out)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if res.modelOverride != "" || len(res.consentDecisions) != 0 {
				t.Errorf("an aborted run returned collected choices %+v — it must collect nothing", res)
			}
		})
	}
}

// TestWizardCancelledContextAborts proves Ctrl-C (a cancelled context) aborts
// rather than falling through to a default answer.
func TestWizardCancelledContextAborts(t *testing.T) {
	backend, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatalf("resolve backend: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	in := wizardInput{rec: recommend.Recommendation{Model: "m", Quant: "q", ContextLen: 1}, backend: backend}
	var out strings.Builder
	if _, err := runWizard(ctx, in, strings.NewReader("y\ny\n"), &out); !errors.Is(err, errWizardAborted) {
		t.Errorf("cancelled context err = %v, want errWizardAborted", err)
	}
}

// TestWizardRejectsOutOfRangeSelection asserts a bad model answer re-asks rather
// than silently taking a default — a mistyped number must not choose a model.
func TestWizardRejectsOutOfRangeSelection(t *testing.T) {
	backend, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatalf("resolve backend: %v", err)
	}
	rec := recommend.Recommendation{
		Model: "qwen2.5-0.5b", Quant: "Q4_K_M", ContextLen: 4096,
		Alternatives: []recommend.Alternative{{Model: "qwen2.5-1.5b", Quant: "Q4_K_M", ContextLen: 8192}},
	}
	in := wizardInput{rec: rec, alternatives: rec.Alternatives, backend: backend}

	var out strings.Builder
	// "9" is out of range and "abc" is not a number; both must be re-asked, then "2"
	// is accepted.
	res, err := runWizard(t.Context(), in, strings.NewReader("9\nabc\n2\ny\n"), &out)
	if err != nil {
		t.Fatalf("runWizard: %v", err)
	}
	if res.modelOverride != "qwen2.5-1.5b" {
		t.Errorf("modelOverride = %q, want %q after the invalid answers were re-asked", res.modelOverride, "qwen2.5-1.5b")
	}
	if !strings.Contains(out.String(), "please enter a number between 1 and 2") {
		t.Errorf("an out-of-range answer was not re-asked:\n%s", out.String())
	}
}

// TestPreflightSummaryBlockIndent asserts every per-check detail row is 2-cell
// `block`-indented under the "Preflight results" heading (17-UI-SPEC.md Spacing Scale
// `block` = 2-cell left indent / Pillar 5), and that the indent is ADDITIVE — the
// glyph/word/name content survives unchanged after the leading two spaces.
func TestPreflightSummaryBlockIndent(t *testing.T) {
	checks := []preflight.CheckResult{
		{ID: "PRE-01", Name: "kernel floor", Tier: preflight.TierBlock, Status: preflight.StatusPass},
		seloffCheck(),
	}
	got := preflightSummary(checks, true) // ascii=true → [OK]/[BLOCK] glyphs
	for _, line := range strings.Split(got, "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, blockIndent) {
			t.Errorf("preflight detail line not 2-cell `block`-indented: %q", line)
		}
	}
	// Additive: the inner glyph/word/name content is unchanged after the indent.
	if !strings.Contains(got, "[OK]") || !strings.Contains(got, "PASS") || !strings.Contains(got, "kernel floor") {
		t.Errorf("preflight PASS row lost its glyph/word/name content:\n%s", got)
	}
	if !strings.Contains(got, "[BLOCK]") || !strings.Contains(got, "SELinux container_use_devices boolean") {
		t.Errorf("preflight BLOCK row lost its glyph/name content:\n%s", got)
	}
}

// TestReviewBlockIndent asserts every review `key: value` line is 2-cell
// `block`-indented under the "Review — villa will install:" heading (Pillar 5),
// and that the "will pull:" line still renders backend.Image() via the accessor
// (no re-typed image literal — TestSeamGrepGate).
func TestReviewBlockIndent(t *testing.T) {
	backend, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatalf("resolve backend: %v", err)
	}
	in := wizardInput{
		rec:     recommend.Recommendation{Model: "qwen2.5-0.5b", Quant: "Q4_K_M", ContextLen: 4096, Backend: "vulkan"},
		backend: backend,
	}
	got := reviewBlock(in, "")
	for _, line := range strings.Split(got, "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, blockIndent) {
			t.Errorf("review detail line not 2-cell `block`-indented: %q", line)
		}
	}
	// The will-pull line renders the backend image via the accessor (not a literal).
	if !strings.Contains(got, backend.Image()) {
		t.Errorf("review block must render backend.Image() via the accessor, got:\n%s", got)
	}
}

// TestInstallNoFitEmitsContractedEmptyState proves the no-fit branch (recommend
// refused) emits the EXACT 17-UI-SPEC.md:195 empty-state copy verbatim — the
// usable-GiB envelope figure substituted from profile.UsableEnvelopeBytes, the
// "(--no-tui shows the same result.)" parity note, and exitBlocked preserved.
// A Known envelope renders the numeric GiB figure; a typed-Unknown envelope
// renders "unknown GiB usable" (never a fabricated 0).
func TestInstallNoFitEmitsContractedEmptyState(t *testing.T) {
	refuse := func(env detect.Bytes) (*installDeps, *bytes.Buffer) {
		units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
		plan := orchestrate.Plan{Changed: units}
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.probe = func() detect.HostProfile {
			return detect.HostProfile{UsableEnvelopeBytes: env}
		}
		// A refusing pick: empty Model is a clear no-fit.
		f.pick = func(detect.HostProfile, recommend.Overrides) recommend.Recommendation {
			return recommend.Recommendation{}
		}
		return f.installDeps, nil
	}

	t.Run("known-envelope-renders-numeric-gib", func(t *testing.T) {
		d, _ := refuse(detect.KnownBytes(8<<30, "mem_info_gtt_total"))
		cmd, _, errOut := installTestCmd()
		code := runInstall(cmd, installOpts{}, d)
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
		d, _ := refuse(detect.UnknownBytes("no gtt probe", ""))
		cmd, _, errOut := installTestCmd()
		code := runInstall(cmd, installOpts{}, d)
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

// TestDetectedHostSummaryTypedUnknownAdvisory proves detectedHostSummary appends
// the EXACT 17-UI-SPEC.md:196 typed-Unknown advisory as a trailing line IFF at
// least one rendered host fact is not Known — and omits it entirely when every
// rendered fact is Known. The advisory augments (never replaces) the bare
// per-field "unknown" token(s).
func TestDetectedHostSummaryTypedUnknownAdvisory(t *testing.T) {
	backend, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatalf("resolve backend: %v", err)
	}
	const advisory = "Some host facts could not be probed; villa will pick conservatively. Run villa detect for detail."

	allKnown := detect.HostProfile{
		CPUModel:            detect.KnownStr("AMD Ryzen AI Max+ 395", "lscpu"),
		UsableEnvelopeBytes: detect.KnownBytes(64<<30, "mem_info_gtt_total"),
		IGPUName:            detect.KnownStr("Radeon 8060S", "drm"),
		IGPUGfxID:           detect.KnownStr("gfx1151", "drm"),
		KernelVersion:       detect.KnownStr("6.18.4", "uname"),
	}

	t.Run("all-known-omits-advisory", func(t *testing.T) {
		got := detectedHostSummary(allKnown, backend)
		if strings.Contains(got, advisory) {
			t.Errorf("all-Known summary must NOT contain the advisory, got:\n%s", got)
		}
	})

	t.Run("typed-unknown-appends-advisory-and-keeps-token", func(t *testing.T) {
		p := allKnown
		p.UsableEnvelopeBytes = detect.UnknownBytes("no gtt probe", "")
		got := detectedHostSummary(p, backend)
		if !strings.Contains(got, advisory) {
			t.Errorf("typed-Unknown summary missing the contracted advisory, got:\n%s", got)
		}
		// The advisory augments, never replaces, the bare per-field "unknown" token.
		if !strings.Contains(got, "unknown usable envelope") {
			t.Errorf("typed-Unknown summary must still render the bare per-field unknown token, got:\n%s", got)
		}
		// The advisory is the trailing line.
		if !strings.HasSuffix(strings.TrimRight(got, "\n"), advisory) {
			t.Errorf("advisory must be the trailing line, got:\n%s", got)
		}
	})

	// Each rendered fact going typed-Unknown independently triggers the advisory.
	t.Run("each-fact-triggers-advisory", func(t *testing.T) {
		cases := map[string]detect.HostProfile{
			"cpu": func() detect.HostProfile {
				p := allKnown
				p.CPUModel = detect.UnknownStr("no lscpu", "")
				return p
			}(),
			"igpu": func() detect.HostProfile {
				p := allKnown
				p.IGPUName = detect.UnknownStr("no drm", "")
				return p
			}(),
			"kernel": func() detect.HostProfile {
				p := allKnown
				p.KernelVersion = detect.UnknownStr("no uname", "")
				return p
			}(),
		}
		for name, p := range cases {
			t.Run(name, func(t *testing.T) {
				if got := detectedHostSummary(p, backend); !strings.Contains(got, advisory) {
					t.Errorf("%s typed-Unknown must append the advisory, got:\n%s", name, got)
				}
			})
		}
	})
}

// TestSafeAutoFixReturnsFalseForPrivilegedFixes pins the conservative
// classification (interpretation 1): both current fixes — PRE-05 (setsebool -P)
// and PRE-03 (loginctl enable-linger) — are PRIVILEGED and so are NOT safe to
// auto-run. safeAutoFix must return false for both; a future reclassification to
// true must be a deliberate, test-visible change.
func TestSafeAutoFixReturnsFalseForPrivilegedFixes(t *testing.T) {
	for _, id := range []string{"PRE-03", "PRE-05"} {
		if safeAutoFix(id) {
			t.Errorf("safeAutoFix(%q) = true, want false (privileged → consent-gated)", id)
		}
	}
}

// TestReviewBlockShowsTheChosenModel is the fix for a real defect the previous
// wizard carried: an operator who picked an alternative was shown a review naming
// the RECOMMENDED model, and asked to confirm an install of something else. The
// review must describe what will actually be installed, including that
// alternative's own quant and context.
func TestReviewBlockShowsTheChosenModel(t *testing.T) {
	backend, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatalf("resolve backend: %v", err)
	}
	in := wizardInput{
		rec: recommend.Recommendation{Model: "recommended-model", Quant: "Q4_K_M", ContextLen: 131072},
		alternatives: []recommend.Alternative{
			{Model: "chosen-model", Quant: "Q3_K_XL", ContextLen: 8192},
		},
		backend: backend,
	}

	got := reviewBlock(in, "chosen-model")
	if !strings.Contains(got, "chosen-model") {
		t.Errorf("review does not name the chosen model:\n%s", got)
	}
	if strings.Contains(got, "recommended-model") {
		t.Errorf("review still names the recommended model after an override:\n%s", got)
	}
	// The alternative's OWN quant and context must be shown, not the recommendation's.
	if !strings.Contains(got, "Q3_K_XL") || !strings.Contains(got, "ctx 8192") {
		t.Errorf("review shows the recommendation's quant/ctx instead of the chosen model's:\n%s", got)
	}

	// Keeping the recommended pick still reviews the recommendation.
	kept := reviewBlock(in, "")
	if !strings.Contains(kept, "recommended-model") || !strings.Contains(kept, "ctx 131072") {
		t.Errorf("review of the kept recommendation is wrong:\n%s", kept)
	}
}
