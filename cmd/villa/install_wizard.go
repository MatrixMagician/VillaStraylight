package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// install_wizard.go is the command-tier guided front-end for `villa install`
// PURE PRESENTATION + a PURE COLLECTOR. It composes the existing
// cores (recommend.Pick output, internal/preflight CheckResults, inference.Backend
// accessors); it imports NO decision logic and NEVER executes a host fix — it calls
// neither runGapFix nor resolveGap nor offerNonBlockingGap. The single gateInstall
// in runInstall consumes the consent it collects, so probe/pick/runChecks/gate each
// run exactly once for both paths.
//
// It is a stdin prompt loop over the standard library. It was a full-screen TUI
// built on huh/bubbletea/lipgloss/termenv, which were four direct dependencies and
// roughly twenty indirect ones serving this one flow. Of its five screens only two
// ever collected input, and the three display screens already composed their content
// with fmt.Fprintf into a string — so the framework was rendering text this file had
// already produced.
//
// What it asks is unchanged, and is the whole interactive surface of an install:
//
//  1. which model, from the memory-fitting shortlist;
//  2. consent per privileged host-prep gap, showing the exact command first;
//  3. a final confirm that defaults to Cancel.
//
// Quant and context are deliberately NOT questions: recommend.Pick derives them from
// the measured memory envelope, and offering them would invite a configuration that
// does not fit.
//
// It renders no backend or image literal — backend names reach it as an
// inference.Backend and are shown via Name()/Image() accessors (TestSeamGrepGate
// walks cmd/villa). Decisions: (guided default verb), (pick-from-
// alternatives, computes nothing), (privileged consent only), (final
// confirm defaults to Cancel).

// wizardInput carries ONLY what the prompt loop renders — every field is already
// computed by runInstall (steps 1-3); it computes none of them.
type wizardInput struct {
	// profile is the probed host (the detected-host summary).
	profile detect.HostProfile
	// rec is the recommendation from d.pick (the recommended option + review).
	rec recommend.Recommendation
	// alternatives are the other fitting picks (= rec.Alternatives) offered in the
	// model question.
	alternatives []recommend.Alternative
	// checks are the preflight results rendered before the consent questions.
	checks []preflight.CheckResult
	// backend is the resolved backend for the review — rendered via its
	// Name()/Image() accessors ONLY, never a re-typed image literal.
	backend inference.Backend
	// colorEnabled threads the colour gate through. With colour off the glyph
	// column falls back to [OK]/[WARN]/[BLOCK], which is also what a non-TTY gets.
	colorEnabled bool
}

// wizardResult is what the loop COLLECTS — and NOTHING that executes a fix.
type wizardResult struct {
	// modelOverride is the chosen catalog model id; empty = keep the recommended pick.
	// runInstall re-validates it through the single pick seam (recommend.Overrides).
	modelOverride string
	// consentDecisions records per-item privileged consent keyed by check ID (gap-id
	// → y/n). gateInstall honors these without re-prompting.
	consentDecisions map[string]bool
}

// errWizardCancelled is the sentinel a Cancel/decline on the final confirm returns
// so runInstall maps it to a clean, non-mutating abort.
var errWizardCancelled = errors.New("install wizard cancelled")

// liveWizard runs the guided install against the real terminal and RETURNS the
// collected choices. It runs NO host fix.
func liveWizard(ctx context.Context, in wizardInput) (wizardResult, error) {
	return runWizard(ctx, in, os.Stdin, os.Stdout)
}

// runWizard is the testable core: the same loop against injected streams, so the
// whole flow is drivable from a test with no TTY.
//
// A closed or exhausted input stream is an abort, never an implied yes — the final
// confirm and every consent question default to the safe answer.
func runWizard(ctx context.Context, in wizardInput, stdin io.Reader, stdout io.Writer) (wizardResult, error) {
	p := &prompter{in: bufio.NewReader(stdin), out: stdout, ctx: ctx}

	// 1/4 — detected host (display only).
	p.section(1, 4, "Detected host")
	p.print(detectedHostSummary(in.profile, in.backend))

	// 2/4 — the model question, the one genuinely open choice.
	p.section(2, 4, "Confirm your model")
	chosen, err := p.chooseModel(in.rec, in.alternatives)
	if err != nil {
		return wizardResult{}, err
	}

	// 3/4 — preflight results, then one consent per privileged gap.
	p.section(3, 4, "Preflight results")
	p.print(preflightSummary(in.checks, !in.colorEnabled))

	consents := map[string]bool{}
	for _, c := range in.checks {
		if !privilegedGap(c) {
			continue
		}
		cmdStr := remediationCommand(c, hostUsername(in.profile))
		p.print("")
		p.print(fmt.Sprintf("  %s\n  command: %s", c.Detail, cmdStr))
		ok, err := p.confirm(fmt.Sprintf("Run privileged host-prep for [%s]?", c.ID), false)
		if err != nil {
			return wizardResult{}, err
		}
		consents[c.ID] = ok
	}

	// 4/4 — review, then the final confirm. It defaults to Cancel.
	//
	// The review reflects the model actually CHOSEN, not the recommended one. The
	// previous wizard rendered the recommendation here regardless of the selection,
	// so an operator who picked an alternative was asked to confirm a summary naming
	// a different model than the one about to be installed.
	p.section(4, 4, "Review — villa will install:")
	p.print(reviewBlock(in, chosen))
	p.print("")
	proceed, err := p.confirm(
		"Install: this will pull images, write Quadlet units, and start services. Proceed?", false)
	if err != nil {
		return wizardResult{}, err
	}
	if !proceed {
		return wizardResult{}, errWizardCancelled
	}

	// Empty override when the user kept the recommended pick (so runInstall does not
	// needlessly re-run Pick); otherwise the chosen catalog id.
	override := ""
	if chosen != in.rec.Model {
		override = chosen
	}
	return wizardResult{modelOverride: override, consentDecisions: consents}, nil
}

// prompter is the small stdin/stdout question loop the guided install is built from.
type prompter struct {
	in  *bufio.Reader
	out io.Writer
	ctx context.Context
}

// errWizardAborted is returned when input ends (EOF/Ctrl-D) or the context is
// cancelled (Ctrl-C) before a question is answered. It is distinct from a
// deliberate Cancel, and both leave the host untouched.
var errWizardAborted = errors.New("install wizard aborted")

func (p *prompter) print(s string) { fmt.Fprintln(p.out, s) }

// section prints the "n/total Title" step header the flow is paced by.
func (p *prompter) section(n, total int, title string) {
	fmt.Fprintf(p.out, "\n%d/%d  %s\n\n", n, total, title)
}

// readLine returns the next answer, or errWizardAborted when input ends or the
// context is cancelled. It never treats an absent answer as consent.
func (p *prompter) readLine() (string, error) {
	if err := p.ctx.Err(); err != nil {
		return "", errWizardAborted
	}
	line, err := p.in.ReadString('\n')
	if err != nil {
		// A final line without a trailing newline is still a real answer.
		if line != "" {
			return strings.TrimSpace(line), nil
		}
		return "", errWizardAborted
	}
	return strings.TrimSpace(line), nil
}

// confirm asks a yes/no question. def is the answer an empty line takes, and it is
// false at every call site: the safe choice must never require typing.
func (p *prompter) confirm(question string, def bool) (bool, error) {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	for {
		fmt.Fprintf(p.out, "%s [%s] ", question, hint)
		answer, err := p.readLine()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			p.print("  please answer y or n")
		}
	}
}

// chooseModel offers the recommended pick plus the memory-fitting alternatives and
// returns the chosen catalog id. The answer is an index into a constrained
// list, never free text, so an unknown model id cannot be introduced here.
func (p *prompter) chooseModel(rec recommend.Recommendation, alts []recommend.Alternative) (string, error) {
	options := modelOptions(rec, alts)
	for i, o := range options {
		fmt.Fprintf(p.out, "  %d) %s\n", i+1, o.label)
	}
	if len(options) == 1 {
		// Nothing to choose between; state the pick rather than posing a question
		// with one answer.
		p.print("")
		return options[0].id, nil
	}

	for {
		fmt.Fprintf(p.out, "\nSelect a model [1-%d, default 1]: ", len(options))
		answer, err := p.readLine()
		if err != nil {
			return "", err
		}
		if answer == "" {
			return options[0].id, nil
		}
		n, convErr := strconv.Atoi(answer)
		if convErr != nil || n < 1 || n > len(options) {
			fmt.Fprintf(p.out, "  please enter a number between 1 and %d\n", len(options))
			continue
		}
		return options[n-1].id, nil
	}
}

// modelOption is one offered pick: the line shown, and the catalog id it resolves to.
type modelOption struct {
	label string
	id    string
}

// modelOptions builds the offered list from the recommended pick (labelled
// "recommended") plus the memory-fitting alternatives. Each line is
// model · quant · ctx; the value is the catalog model id (constrained, never free text).
func modelOptions(rec recommend.Recommendation, alts []recommend.Alternative) []modelOption {
	opts := []modelOption{{
		label: fmt.Sprintf("%s · %s · ctx %d  (recommended)", rec.Model, rec.Quant, rec.ContextLen),
		id:    rec.Model,
	}}
	for _, a := range alts {
		if a.Model == rec.Model {
			continue
		}
		opts = append(opts, modelOption{
			label: fmt.Sprintf("%s · %s · ctx %d", a.Model, a.Quant, a.ContextLen),
			id:    a.Model,
		})
	}
	return opts
}

// detectedHostSummary renders the typed-Unknown host facts: a missing fact renders
// as "unknown", never a fabricated 0.
func detectedHostSummary(p detect.HostProfile, backend inference.Backend) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%sCPU:      %s\n", blockIndent, strOrUnknown(p.CPUModel))
	fmt.Fprintf(&b, "%smemory:   %s usable envelope\n", blockIndent, bytesOrUnknown(p.UsableEnvelopeBytes))
	fmt.Fprintf(&b, "%siGPU:     %s (%s)\n", blockIndent, strOrUnknown(p.IGPUName), strOrUnknown(p.IGPUGfxID))
	fmt.Fprintf(&b, "%skernel:   %s\n", blockIndent, strOrUnknown(p.KernelVersion))
	fmt.Fprintf(&b, "%sbackend:  %s", blockIndent, backend.Name())

	// Typed-Unknown advisory: when ANY rendered fact is not Known, append the
	// contracted note. The check mirrors the exact unknown-conditions the renderers
	// use, so the advisory and the per-field tokens can never disagree. The advisory
	// AUGMENTS the bare "unknown" tokens, never replaces them.
	if !strKnown(p.CPUModel) || !p.UsableEnvelopeBytes.Known ||
		!strKnown(p.IGPUName) || !strKnown(p.KernelVersion) {
		fmt.Fprintf(&b, "\n\n  Some host facts could not be probed; villa will pick conservatively. "+
			"Run villa detect for detail.")
	}
	return b.String()
}

// strKnown reports whether a typed-Unknown Str renders as a real value (mirrors the
// strOrUnknown condition: Known AND non-empty Value) so the advisory predicate and
// the per-field renderer can never disagree about what counts as "unknown".
func strKnown(s detect.Str) bool { return s.Known && s.Value != "" }

// preflightSummary renders one row per check: glyph + status word + name +
// remediation. It is informational — consent is collected by the questions that
// follow, and no fix runs here.
func preflightSummary(checks []preflight.CheckResult, ascii bool) string {
	if len(checks) == 0 {
		return blockIndent + "no preflight checks to report"
	}
	var b strings.Builder
	for i, c := range checks {
		tier, word := statusWord(c)
		fmt.Fprintf(&b, "%s%s %s  %s", blockIndent, statusGlyph(tier, ascii), word, c.Name)
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
// be pulled (Name()/Image() accessors — NEVER a re-typed literal), and the install
// side effects.
//
// chosen is the catalog id the operator selected; when it differs from the
// recommendation the alternative's own quant and context are shown, because those
// are what will be installed. An empty chosen means the recommended pick.
func reviewBlock(in wizardInput, chosen string) string {
	model, quant, ctx := in.rec.Model, in.rec.Quant, in.rec.ContextLen
	if chosen != "" && chosen != in.rec.Model {
		for _, a := range in.alternatives {
			if a.Model == chosen {
				model, quant, ctx = a.Model, a.Quant, a.ContextLen
				break
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%smodel:      %s · %s · ctx %d\n", blockIndent, model, quant, ctx)
	fmt.Fprintf(&b, "%sbackend:    %s\n", blockIndent, in.backend.Name())
	fmt.Fprintf(&b, "%swill pull:  %s\n", blockIndent, in.backend.Image())
	fmt.Fprintf(&b, "%swill write: rootless Podman Quadlet units (config-derived)\n", blockIndent)
	fmt.Fprintf(&b, "%swill start: villa-llama, villa-openwebui, villa-dashboard", blockIndent)
	return b.String()
}

// privilegedGap reports whether a check is a privileged BLOCK/WARN gap that needs
// per-item consent — it has an automated (privileged) fix and is NOT a safe auto-fix.
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
// resolution the live path uses — display only (the loop never runs the command).
func hostUsername(detect.HostProfile) string { return installUsername() }

// strOrUnknown renders a typed-Unknown Str as its value or the "unknown" token
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
