package install

import (
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// gate.go is install's host-prep gate: the preflight verdict applied to the run.
// WARN checks are narrated; BLOCK gaps (a FAIL, or an unmet BLOCK-tier check) are
// OFFERED for consented privileged host-prep with the exact command shown, and run
// only on an explicit yes. Declined, --json, non-interactive and --dry-run all
// print the command and keep the gap as a block, overridable by --force. villa
// never silently runs a privileged command.
//
// The gate runs EXACTLY ONCE per install. The wizard path threads a pre-collected
// decision map (gap id → yes/no) that is honoured without re-prompting; the flag
// path passes nil and prompts through Consent. Either way a privileged fix runs at
// most once.

type gateVerdict int

const (
	gatePass gateVerdict = iota
	// gateForced: an unmet BLOCK gap was bypassed with --force. The run continues
	// but degrades to WARN.
	gateForced
	gateBlocked
)

// gate applies the checks and returns whether the run may proceed.
func gate(d Deps, checks []preflight.CheckResult, opts Opts, consents map[string]bool) gateVerdict {
	say, warn := d.say, d.warn

	var unmet []preflight.CheckResult
	for _, c := range checks {
		switch c.Status {
		case preflight.StatusPass:
		case preflight.StatusWarn:
			switch {
			case SafeAutoFix(c.ID):
				// A non-privileged safe fix auto-runs with a visible notice and no
				// consent, but only interactive, not --json and not --dry-run. With no
				// current safe fix this is the forward-looking classifier.
				if opts.JSON || opts.DryRun || !d.Interactive() {
					say("warning: [%s] %s — %s\n", c.ID, c.Detail, c.Remediation)
					break
				}
				say("auto-fixing [%s]: %s\n", c.ID, RemediationCommand(c, d.Username()))
				if err := runGapFix(c, d); err != nil {
					say("  auto-fix failed: %v — run the command manually\n", err)
				} else {
					say("  applied: %s\n", RemediationCommand(c, d.Username()))
				}
			case c.Tier == preflight.TierBlock:
				// An unmet BLOCK-tier check is a gap to resolve via consent, not a pass.
				unmet = append(unmet, c)
			case HasAutomatedFix(c.ID):
				// A WARN-tier gap with a privileged fix (linger off): OFFER it, never
				// block on a decline. It goes to stdout so a soft offer is never read as
				// an error by a script parsing stderr.
				offerNonBlockingGap(d, say, c, opts, consents)
			default:
				say("warning: [%s] %s — %s\n", c.ID, c.Detail, c.Remediation)
			}
		case preflight.StatusFail:
			unmet = append(unmet, c)
		}
	}
	if len(unmet) == 0 {
		return gatePass
	}

	var stillBlocked []preflight.CheckResult
	for _, c := range unmet {
		if resolveGap(d, say, warn, c, opts, consents) {
			continue
		}
		stillBlocked = append(stillBlocked, c)
	}
	if len(stillBlocked) == 0 {
		return gatePass
	}

	if opts.Force {
		say("\nOverridden BLOCK gap(s) (--force): %d bypassed\n", len(stillBlocked))
		for _, c := range stillBlocked {
			say("  - [%s] %s: %s\n", c.ID, c.Name, c.Detail)
		}
		say("Proceeding despite unmet host-prep — you accepted the risk.\n")
		return gateForced
	}

	warn("\nBLOCKED: %d host-prep step(s) unmet. Run the printed command(s) above, or re-run `villa install --force` to override (auditable).\n", len(stillBlocked))
	return gateBlocked
}

// resolveGap handles one BLOCK gap: prints the exact remediation and, only on an
// interactive TTY with an explicit yes (or a recorded wizard consent), runs the
// fixed-arg privileged seam. False means the gap stays a block.
func resolveGap(d Deps, say, warn func(string, ...any), c preflight.CheckResult, opts Opts, consents map[string]bool) bool {
	cmdStr := RemediationCommand(c, d.Username())
	warn("\nhost-prep needed: [%s] %s\n  command: %s\n", c.ID, c.Detail, cmdStr)

	// --dry-run NEVER mutates: checked FIRST so even a stale threaded consent can
	// never execute host-prep under a flag sold as side-effect-free.
	if opts.DryRun {
		warn("  (dry-run — run the command above, then re-run `villa install`)\n")
		return false
	}
	if decision, recorded := consents[c.ID]; consents != nil && recorded {
		if !decision {
			warn("BLOCK: %s. %s. Run the suggested command, or re-run with --no-tui --force to override (auditable).\n", c.Name, blockRemediation(c, d.Username()))
			warn("  declined — run the command above, then re-run `villa install`\n")
			return false
		}
		if err := runGapFix(c, d); err != nil {
			warn("  host-prep failed: %v — run the command manually, then re-run `villa install`\n", err)
			return false
		}
		say("  applied: %s\n", cmdStr)
		return true
	}
	if opts.JSON || !d.Interactive() {
		warn("  (non-interactive — run the command above, then re-run `villa install`)\n")
		return false
	}
	if !d.Consent(fmt.Sprintf("Run `%s` now? [y/N] ", cmdStr)) {
		warn("  declined — run the command above, then re-run `villa install`\n")
		return false
	}
	if err := runGapFix(c, d); err != nil {
		warn("  host-prep failed: %v — run the command manually, then re-run `villa install`\n", err)
		return false
	}
	say("  applied: %s\n", cmdStr)
	return true
}

// offerNonBlockingGap offers the consented fix for a WARN-tier gap and never
// blocks on a decline: boot survival, not an immediate crash. Everything goes to
// stdout.
func offerNonBlockingGap(d Deps, say func(string, ...any), c preflight.CheckResult, opts Opts, consents map[string]bool) bool {
	cmdStr := RemediationCommand(c, d.Username())
	say("\noptional host-prep (boot survival): [%s] %s\n  command: %s\n", c.ID, c.Detail, cmdStr)

	if opts.DryRun {
		say("  (dry-run — optional; run the command above to enable boot survival)\n")
		return false
	}
	if decision, recorded := consents[c.ID]; consents != nil && recorded {
		if !decision {
			say("  skipped — boot survival not enabled; install continues. Run the command above later if you want it.\n")
			return false
		}
		if err := runGapFix(c, d); err != nil {
			say("  host-prep failed: %v — run the command manually if you want boot survival; install continues.\n", err)
			return false
		}
		say("  applied: %s\n", cmdStr)
		return true
	}
	if opts.JSON || !d.Interactive() {
		say("  (optional — run the command above to enable boot survival; install continues regardless)\n")
		return false
	}
	if !d.Consent(fmt.Sprintf("Run `%s` now? [y/N] ", cmdStr)) {
		say("  skipped — boot survival not enabled; install continues. Run the command above later if you want it.\n")
		return false
	}
	if err := runGapFix(c, d); err != nil {
		say("  host-prep failed: %v — run the command manually if you want boot survival; install continues.\n", err)
		return false
	}
	say("  applied: %s\n", cmdStr)
	return true
}

// runGapFix dispatches a consented gap to its fixed-arg privileged seam by id.
func runGapFix(c preflight.CheckResult, d Deps) error {
	switch c.ID {
	case "PRE-05":
		return d.Setsebool()
	case "PRE-03":
		return d.EnableLinger(d.Username())
	default:
		return fmt.Errorf("no automated host-prep for %s", c.ID)
	}
}

// HasAutomatedFix reports whether a check id has a consented privileged seam
// install can offer to run. Everything else is a printed remediation hint.
func HasAutomatedFix(id string) bool {
	switch id {
	case "PRE-05", "PRE-03":
		return true
	default:
		return false
	}
}

// SafeAutoFix reports whether a check id has a NON-privileged automated fix that
// may auto-run with a notice and no consent. Both current fixes are privileged, so
// it is false for every id: a forward-looking classifier that is a behaviour no-op
// on the present check set.
func SafeAutoFix(string) bool { return false }

// RemediationCommand returns the exact copy-paste command for a gap, preferring
// the well-known fixed commands so the printed string matches the seam exactly.
func RemediationCommand(c preflight.CheckResult, username string) string {
	switch c.ID {
	case "PRE-05":
		return "setsebool -P container_use_devices=true"
	case "PRE-03":
		return fmt.Sprintf("loginctl enable-linger %s", username)
	default:
		if c.Remediation != "" {
			return c.Remediation
		}
		return c.Detail
	}
}

// blockRemediation is the <remediation> token of the contracted BLOCK-gap-declined
// copy: the check's Remediation when present, else the fixed command.
func blockRemediation(c preflight.CheckResult, username string) string {
	if c.Remediation != "" {
		return c.Remediation
	}
	return RemediationCommand(c, username)
}
