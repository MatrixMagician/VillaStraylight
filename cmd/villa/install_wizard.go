package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// install_wizard.go is the command-tier huh wizard front-end for `villa install`
// (INSTALL-01): PURE PRESENTATION + a PURE COLLECTOR. It composes the existing
// cores (recommend.Pick output, internal/preflight CheckResults, inference.Backend
// accessors) and the shared tui_theme.go styling; it imports NO decision logic and
// NEVER executes a host fix — it calls neither runGapFix nor resolveGap nor
// offerNonBlockingGap. The single gateInstall in runInstall consumes the consent it
// collects, so probe/pick/runChecks/gate each run exactly once for both paths.
//
// It lives in cmd/villa/ by constraint: huh/lipgloss are presentation and NO
// internal/* core may import them (D-11). It renders no backend or image literal —
// backend names reach it as an inference.Backend and are shown via Name()/Image()
// accessors (TestSeamGrepGate walks cmd/villa). Decisions: D-01 (TUI default verb),
// D-02 (pick-from-alternatives, computes nothing), D-04 (privileged consent only),
// D-07 (final review confirm defaults to Cancel), D-11 (huh confined to cmd/villa).

// wizardInput carries ONLY what the wizard renders — every field is already
// computed by runInstall (steps 1-3); the wizard computes none of them.
type wizardInput struct {
	// profile is the probed host (screen 1 detected-host summary).
	profile detect.HostProfile
	// rec is the recommendation from d.pick (screen 2 recommended option + review).
	rec recommend.Recommendation
	// alternatives are the other fitting picks (= rec.Alternatives) offered in screen 2.
	alternatives []recommend.Alternative
	// checks are the preflight results rendered in screen 3 (gap rows + consent).
	checks []preflight.CheckResult
	// backend is the resolved backend for the review screen — rendered via its
	// Name()/Image() accessors ONLY, never a re-typed image literal.
	backend inference.Backend
	// colorEnabled threads the D-09 color gate into villaTheme.
	colorEnabled bool
}

// wizardResult is what the wizard COLLECTS — and NOTHING that executes a fix.
type wizardResult struct {
	// modelOverride is the chosen catalog model id; empty = keep the recommended pick.
	// runInstall re-validates it through the single pick seam (recommend.Overrides).
	modelOverride string
	// consentDecisions records per-item privileged consent keyed by check ID (gap-id
	// → y/n) collected in screen 3. gateInstall honors these without re-prompting.
	consentDecisions map[string]bool
}

// errWizardCancelled is the sentinel a Cancel/decline on the final Install confirm
// returns so runInstall maps it to a clean, non-mutating abort (D-07).
var errWizardCancelled = errors.New("install wizard cancelled")

// liveWizard builds and runs the huh 5-screen form against the real TTY and RETURNS
// the collected choices. It runs NO host fix — it presents in.rec/in.checks/in.backend
// and returns the model override + per-item consent. An Esc/Ctrl+C (form error) or a
// Cancel on the Install confirm returns an error so runInstall aborts with no mutation.
func liveWizard(ctx context.Context, in wizardInput) (wizardResult, error) {
	chosen := in.rec.Model // default to the recommended pick (empty override unless changed)
	consents := map[string]bool{}
	var doInstall bool // defaults false → focus the safe "Cancel" choice (D-07)

	var holders []*gapConsentValue
	form := buildWizardForm(in, &chosen, &holders, &doInstall)
	if err := form.RunWithContext(ctx); err != nil {
		// Esc / Ctrl+C / I/O abort — runInstall maps this to a clean non-zero return.
		return wizardResult{}, err
	}
	if !doInstall {
		// Deliberate Cancel on the final confirm — no mutation (D-07).
		return wizardResult{}, errWizardCancelled
	}

	// Reconcile each privileged-gap decision (written by huh into the holder's val)
	// into the consents map. gateInstall honors these without re-prompting (D-04).
	for _, h := range holders {
		consents[h.id] = h.val
	}

	// Empty override when the user kept the recommended pick (so runInstall does not
	// needlessly re-run Pick); otherwise the chosen catalog id.
	override := ""
	if chosen != in.rec.Model {
		override = chosen
	}
	return wizardResult{modelOverride: override, consentDecisions: consents}, nil
}

// gapConsentValue is a per-privileged-gap decision pointer bound into a huh.Confirm.
// huh writes the user's y/n into `val` during the form run; the caller (liveWizard /
// tests) reconciles each holder into the consents map after the form completes. This
// keeps each Confirm self-contained while the map stays authoritative — no globals.
type gapConsentValue struct {
	id  string
	val bool
}

// buildWizardForm composes the 5 UI-SPEC screens as huh groups, binding the Select
// into `chosen`, each privileged-gap Confirm into a fresh gapConsentValue appended to
// `*holders`, and the final Install Confirm into `doInstall`. It is split out so tests
// (Plan 03) can drive it via WithInput/WithAccessible/WithOutput without a live TTY,
// then reconcile *holders into a consents map exactly as liveWizard does. ascii mirrors
// the no-color path so the glyph column falls back to [OK]/[WARN]/[BLOCK].
func buildWizardForm(in wizardInput, chosen *string, holders *[]*gapConsentValue, doInstall *bool) *huh.Form {
	ascii := !in.colorEnabled
	groups := []*huh.Group{
		// Screen 1/5 — detected host (read-only).
		huh.NewGroup(
			huh.NewNote().
				Title(stepHeader(1, 5, in.colorEnabled) + " Detected host").
				Description(detectedHostSummary(in.profile, in.backend)),
		),
		// Screen 2/5 — confirm / adjust model (D-02). Options = recommended + alternatives.
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(stepHeader(2, 5, in.colorEnabled) + " Confirm your model").
				Description("Pick the recommended model or another memory-fitting catalog pick.").
				Options(modelOptions(in.rec, in.alternatives)...).
				Value(chosen),
		),
	}

	// Screen 3/5 — preflight gate. PASS/WARN/auto-fix rows are a read-only Note; each
	// privileged BLOCK gap gets a per-item Confirm binding into consents[id] (D-04).
	gapFields := []huh.Field{
		huh.NewNote().
			Title(stepHeader(3, 5, in.colorEnabled) + " Preflight results").
			Description(preflightSummary(in.checks, ascii)),
	}
	for _, c := range in.checks {
		if !privilegedGap(c) {
			continue
		}
		cmdStr := remediationCommand(c, hostUsername(in.profile))
		// A fresh holder per privileged gap, defaulting to decline (false) so an
		// un-touched gap is never silently run. huh writes the user's y/n into &h.val.
		h := &gapConsentValue{id: c.ID, val: false}
		*holders = append(*holders, h)
		gapFields = append(gapFields,
			huh.NewConfirm().
				Title(fmt.Sprintf("Run privileged host-prep for [%s]?", c.ID)).
				Description(fmt.Sprintf("%s\ncommand: %s", c.Detail, cmdStr)).
				Affirmative("Run it").
				Negative("Skip").
				Value(&h.val),
		)
	}
	groups = append(groups, huh.NewGroup(gapFields...))

	// Screen 4/5 — final review + the single Install confirm (D-07). Focus defaults to
	// Cancel because doInstall starts false.
	groups = append(groups,
		huh.NewGroup(
			huh.NewNote().
				Title(stepHeader(4, 5, in.colorEnabled)+" Review — villa will install:").
				Description(reviewBlock(in)),
			huh.NewConfirm().
				Title("Install: this will pull images, write Quadlet units, and start services. Proceed?").
				Affirmative("Install").
				Negative("Cancel").
				Value(doInstall),
		),
	)

	return huh.NewForm(groups...).
		WithTheme(villaTheme(in.colorEnabled)).
		WithKeyMap(villaKeyMap())
}

// detectedHostSummary renders the typed-Unknown host facts (UI-SPEC screen 1): a
// missing fact renders faint as "unknown", never a fabricated 0 (D-03 backend=vulkan
// via the resolved accessor).
func detectedHostSummary(p detect.HostProfile, backend inference.Backend) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CPU:      %s\n", strOrUnknown(p.CPUModel))
	fmt.Fprintf(&b, "memory:   %s usable envelope\n", bytesOrUnknown(p.UsableEnvelopeBytes))
	fmt.Fprintf(&b, "iGPU:     %s (%s)\n", strOrUnknown(p.IGPUName), strOrUnknown(p.IGPUGfxID))
	fmt.Fprintf(&b, "kernel:   %s\n", strOrUnknown(p.KernelVersion))
	fmt.Fprintf(&b, "backend:  %s", backend.Name())

	// Typed-Unknown advisory (17-UI-SPEC.md:196): when ANY rendered fact is not Known,
	// append the contracted help-tier note as a trailing line. The check mirrors the
	// exact unknown-conditions the renderers use — strOrUnknown also treats an empty
	// Value as unknown, so reuse it via strKnown. The advisory AUGMENTS the bare
	// per-field "unknown" tokens, never replaces them. Help-tier styling (faint), not
	// accent — this is advisory, not status. Pure presentation; no decision logic.
	if !strKnown(p.CPUModel) || !p.UsableEnvelopeBytes.Known ||
		!strKnown(p.IGPUName) || !strKnown(p.KernelVersion) {
		fmt.Fprintf(&b, "\n%s", mutedStyle().Render(
			"Some host facts could not be probed; villa will pick conservatively. Run villa detect for detail."))
	}
	return b.String()
}

// strKnown reports whether a typed-Unknown Str renders as a real value (mirrors the
// strOrUnknown condition: Known AND non-empty Value) so the advisory predicate and
// the per-field renderer can never disagree about what counts as "unknown".
func strKnown(s detect.Str) bool { return s.Known && s.Value != "" }

// modelOptions builds the Select options from the recommended pick (labelled
// "recommended") plus the memory-fitting alternatives (D-02). Each line is
// model · quant · ctx; the value is the catalog model id (constrained, never free text).
func modelOptions(rec recommend.Recommendation, alts []recommend.Alternative) []huh.Option[string] {
	opts := []huh.Option[string]{
		huh.NewOption(
			fmt.Sprintf("%s · %s · ctx %d  (recommended)", rec.Model, rec.Quant, rec.ContextLen),
			rec.Model,
		),
	}
	for _, a := range alts {
		if a.Model == rec.Model {
			continue
		}
		opts = append(opts, huh.NewOption(
			fmt.Sprintf("%s · %s · ctx %d", a.Model, a.Quant, a.ContextLen),
			a.Model,
		))
	}
	return opts
}

// preflightSummary renders one row per check (UI-SPEC Preflight Gap States): glyph +
// bold status word + name + remediation. It is informational text — the wizard records
// consent via the per-item Confirms, it does not run any fix here.
func preflightSummary(checks []preflight.CheckResult, ascii bool) string {
	if len(checks) == 0 {
		return "no preflight checks to report"
	}
	var b strings.Builder
	for i, c := range checks {
		tier, word := statusWord(c)
		glyph := statusGlyph(tier, ascii)
		fmt.Fprintf(&b, "%s %s  %s", glyph, statusStyle(tier).Render(word), c.Name)
		if c.Status != preflight.StatusPass && c.Remediation != "" {
			fmt.Fprintf(&b, " — %s", c.Remediation)
		}
		if i < len(checks)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// reviewBlock lists the chosen model/quant/ctx, the backend name, the image that will
// be pulled (Name()/Image() accessors — NEVER a re-typed literal), and the install side
// effects (UI-SPEC Copywriting "Review — villa will install:").
func reviewBlock(in wizardInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "model:      %s · %s · ctx %d\n", in.rec.Model, in.rec.Quant, in.rec.ContextLen)
	fmt.Fprintf(&b, "backend:    %s\n", in.backend.Name())
	fmt.Fprintf(&b, "will pull:  %s\n", in.backend.Image())
	fmt.Fprintf(&b, "will write: rootless Podman Quadlet units (config-derived)\n")
	fmt.Fprintf(&b, "will start: villa-llama, villa-openwebui, villa-dashboard")
	return b.String()
}

// privilegedGap reports whether a check is a privileged BLOCK/WARN gap that needs
// per-item consent in screen 3 — it has an automated (privileged) fix and is NOT a
// safe auto-fix. safeAutoFix is false for both current fixes, so PRE-03/PRE-05 qualify.
func privilegedGap(c preflight.CheckResult) bool {
	if c.Status == preflight.StatusPass {
		return false
	}
	return hasAutomatedFix(c.ID) && !safeAutoFix(c.ID)
}

// statusWord maps a CheckResult to its (tier, word) for the row renderer.
func statusWord(c preflight.CheckResult) (statusTier, string) {
	switch c.Status {
	case preflight.StatusPass:
		return statusPass, "PASS"
	case preflight.StatusFail:
		return statusBlock, "BLOCK"
	default: // StatusWarn
		if c.Tier == preflight.TierBlock {
			return statusBlock, "BLOCK"
		}
		return statusWarn, "WARN"
	}
}

// hostUsername resolves the username for a remediation command string in the review/
// gap text. The HostProfile carries no username, so it reuses the same installUsername
// resolution the live path uses — display only (the wizard never runs the command).
func hostUsername(detect.HostProfile) string { return installUsername() }

// strOrUnknown renders a typed-Unknown Str as its value or the faint "unknown" token
// (never a fabricated empty string), per the detect typed-Unknown contract.
func strOrUnknown(s detect.Str) string {
	if !s.Known || s.Value == "" {
		return "unknown"
	}
	return s.Value
}

// bytesOrUnknown renders a typed-Unknown Bytes as a GiB string or "unknown".
func bytesOrUnknown(b detect.Bytes) string {
	if !b.Known {
		return "unknown"
	}
	return gib(b.Value)
}
