// Package backendswap holds the guards and config mutations of the three swap verbs
// that change how the preserved model is served: `villa backend set`, `villa
// speculation set` (ADR-0006) and `villa tools-mode enter|exit` (spec v1.11 §3.5).
// Each is a stackapply.Change run through the one swap transaction frame (ADR-0015),
// which owns the stack lock, the capture, the apply, the restart of every changed
// running unit, the proof and the rollback. What stays here is what differs: the
// no-op test, the guards (fit, ROCm preflight, the coding-mode refusal) and which
// config field the change writes.
//
// Every guard is an injected Deps field so the tests drive it without a live host.
// The package is LITERAL-FREE of backend marker tokens; those arrive only through
// the frame's Prove seam, wired in cmd/villa.
package backendswap

import (
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// Deps are the guards the three verbs run inside the transaction, plus the frame.
type Deps struct {
	// Tx is the swap transaction frame's host. Its Prove is the verb's proof choice:
	// the shared residency proof, or tools mode's residency-plus-tool-call proof.
	Tx stackapply.TxDeps
	// FitsModel re-checks the PRESERVED model against the target envelope and
	// returns a human reason when it no longer fits — model = config, never re-pick
	// (BSET-01). A non-fit is a refuse-with-remediation with zero side effects.
	FitsModel func(cfg config.VillaConfig) (bool, string)
	// PreflightROCm is the ROCm preflight gate. It is meaningful only when the
	// target is ROCm-family; the live seam short-circuits ok=true otherwise.
	PreflightROCm func(cfg config.VillaConfig) (ok bool, reason string)
}

// Result is the typed outcome of a swap: the frame's Outcome plus what changed.
type Result struct {
	stackapply.Outcome
	// From / To are the previous and target values of whatever this swap changed:
	// the backend, the speculation mode, or the tools-mode state label.
	From string
	To   string
}

// Run switches the inference backend. Inside the lock: a same-backend target is a
// clean NoOp; the fit guard re-checks the PRESERVED model (BSET-01); the ROCm
// preflight sees the TARGET backend; then the change writes cfg.Backend.
func Run(d Deps, target string) Result {
	r := Result{To: target}
	r.Outcome = stackapply.Transact(d.Tx, func(cfg config.VillaConfig) (config.VillaConfig, *stackapply.Outcome) {
		r.From = cfg.Backend
		if cfg.Backend == target {
			return cfg, &stackapply.Outcome{NoOp: true}
		}
		if ok, reason := d.FitsModel(cfg); !ok {
			return cfg, &stackapply.Outcome{Refused: true, Reason: reason}
		}
		// The gate MUST see the TARGET backend: the live seam short-circuits unless it
		// is ROCm-family, so passing the source config left the kernel/firmware/HSA
		// checks dead on a vulkan→rocm switch.
		pre := cfg
		pre.Backend = target
		if ok, reason := d.PreflightROCm(pre); !ok {
			return cfg, &stackapply.Outcome{Refused: true, Reason: reason}
		}
		cfg.Backend = target
		return cfg, nil
	})
	return r
}

// RunSpeculation switches the speculation mode. An unset mode renders off, so it IS
// off for the no-op test. The fit guard sees the TARGET mode, so ResolveSpeculation's
// refusal for an unqualified entry arrives as a non-fit with its note as the reason.
// No ROCm preflight: the mode changes no image, no device and no privilege.
func RunSpeculation(d Deps, target string) Result {
	r := Result{To: target}
	r.Outcome = stackapply.Transact(d.Tx, func(cfg config.VillaConfig) (config.VillaConfig, *stackapply.Outcome) {
		r.From = cfg.Speculation
		if r.From == "" {
			r.From = config.SpeculationOff
		}
		if r.From == target {
			return cfg, &stackapply.Outcome{NoOp: true}
		}
		cfg.Speculation = target
		if ok, reason := d.FitsModel(cfg); !ok {
			return cfg, &stackapply.Outcome{Refused: true, Reason: reason}
		}
		return cfg, nil
	})
	return r
}

// State labels for a tools-mode Result's From/To. They are the words the verb
// prints, so a rolled-back result names the state the operator is back on.
const (
	toolsStateOn  = "on"
	toolsStateOff = "off"
)

// ToolsLabel renders a persisted tools-mode boolean as the Result vocabulary.
func ToolsLabel(on bool) string {
	if on {
		return toolsStateOn
	}
	return toolsStateOff
}

// RunTools flips the RUNNING chat unit into tool calling, and back. The no-op and
// the From label are decided on the ANSWERED gate rather than the raw flag, so
// entering on a coding-mode stack is already there, and an exit while coding mode
// holds the gate on is a refusal naming that flag. The fit guard sees the TARGET
// state: tools mode serves the ctx floor max(cfg.Ctx, agent_ctx), and a floor that
// does not fit refuses with the remediation before anything is captured.
//
// The package stays literal-free of the tool-calling flag: it is a render output of
// internal/inference, and the tool-call probe arrives only through the Prove seam.
func RunTools(d Deps, on bool) Result {
	r := Result{To: ToolsLabel(on)}
	r.Outcome = stackapply.Transact(d.Tx, func(cfg config.VillaConfig) (config.VillaConfig, *stackapply.Outcome) {
		r.From = ToolsLabel(subsystem.ToolsOn(cfg))
		if subsystem.ToolsOn(cfg) == on {
			return cfg, &stackapply.Outcome{NoOp: true}
		}
		if !on && subsystem.CodingModeOn(cfg) {
			return cfg, &stackapply.Outcome{
				Refused: true,
				Reason:  "coding mode implies tools mode — run `villa coding-mode exit` to stop serving tool calls",
			}
		}
		cfg.ToolsMode = on
		if ok, reason := d.FitsModel(cfg); !ok {
			return cfg, &stackapply.Outcome{Refused: true, Reason: reason}
		}
		return cfg, nil
	})
	return r
}
