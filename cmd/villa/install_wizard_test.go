package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// install_wizard_test.go tests the command-tier prompt loop and its rendering:
// the scripted-stdin driver, the safe defaults (cancel/abort never consent), the
// re-ask on a bad answer, and the block-indented summary/review blocks. The wizard
// GATE (fires on a TTY, bypassed by --no-tui/--json/non-TTY, converges with the
// flag path, threads consent through the single gate) is proven at the Run
// interface in internal/install/flow_wizard_test.go.

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
