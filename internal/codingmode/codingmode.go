// Package codingmode holds the guards and config mutation of `villa coding-mode
// enter|exit`: flipping the RUNNING stack into a tool-calling coding mode and back.
// It is a stackapply.Change run through the one swap transaction frame (ADR-0015),
// which owns the stack lock, the capture, the apply, the restart of every changed
// running unit, the proof and the rollback. It composes internal/modelswap's
// forward ordering (resolve → fit-guard → pull) for the swap-residency model
// change; it does not fork modelswap.
//
// Locked decisions realized here:
//   - the mode changes ONLY via the explicit verb; nothing auto-flips coding_mode.
//     Same-state enter/exit is a clean NoOp.
//   - exit restores the chat model under the same transaction, not a bare
//     coding_mode=false write: cfg.Model is never overwritten at enter, so clearing
//     the coder fields reverts the rendered unit to the durable chat model.
//   - residency drives enter: "swap" changes the served model, "shared" applies the
//     render delta to the EXISTING chat endpoint. The core never silently degrades
//     swap→shared; the residency is surfaced verbatim in the Result.
//
// Every host-touching action is an injected Deps field. The package is LITERAL-FREE
// of backend marker tokens; the proof arrives only through the frame's Prove seam.
package codingmode

import (
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// Residency mode values. These mirror recommend's residency vocabulary but are
// re-declared LOCALLY so codingmode imports neither internal/recommend nor
// internal/inference. "swap" performs the model change; "shared" applies
// render-delta-only.
const (
	ResidencySwap   = "swap"
	ResidencyShared = "shared"
)

// Direction is which way the cutover runs.
type Direction int

const (
	// Enter flips chat → coding mode.
	Enter Direction = iota
	// Exit restores the chat model and clears coding mode.
	Exit
)

// String renders the Direction for messages/logs.
func (d Direction) String() string {
	if d == Exit {
		return "exit"
	}
	return "enter"
}

// CoderTarget is the resolved enter-time coder selection, produced by the injected
// ResolveCoder seam so this core imports neither internal/catalog nor
// internal/recommend. On shared residency Model is empty (no model change).
type CoderTarget struct {
	// Model / Quant are the resolved coder catalog id + quantization (swap residency).
	Model string
	Quant string
	// AgentCtx is the catalog-declared agent-profile context the unit is rendered
	// with, persisted as cfg.CoderAgentCtx at enter.
	AgentCtx int
	// Residency is the derived mode, a pure fit-math output, never a preference.
	Residency string
	// Downloaded reports whether the coder weights are already on disk; when false
	// on a swap-residency enter the core pulls them first.
	Downloaded bool
}

// Deps are the enter-path seams, plus the frame.
type Deps struct {
	// Tx is the swap transaction frame's host.
	Tx stackapply.TxDeps
	// ResolveCoder resolves the enter-time coder target through the catalog and the
	// recommend fit-math AT AgentCtx. ok=false is a refuse-with-remediation with
	// zero side effects. Called only on Enter.
	ResolveCoder func(cfg config.VillaConfig) (t CoderTarget, ok bool, reason string)
	// Pull downloads the verified coder weights when absent on a swap-residency enter.
	Pull func(t CoderTarget) error
}

// Result is the typed outcome of a coding-mode cutover.
type Result struct {
	stackapply.Outcome
	// Direction is the cutover direction (enter|exit).
	Direction Direction
	// FromModel / ToModel are the chat model and the served target. On shared
	// residency and on exit ToModel equals FromModel.
	FromModel string
	ToModel   string
	// Residency is the residency the enter path took, so a shared cutover is never
	// presented as a swap. Empty on Exit.
	Residency string
}

// Run performs the coding-mode cutover. Inside the lock: a same-state target is a
// clean NoOp. On ENTER, ResolveCoder is the fit guard (a non-fit refuses before
// anything is captured), a swap-residency coder absent from disk is pulled, and the
// change sets the coder fields; cfg.Model stays the durable chat model. On EXIT the
// change clears the coder fields, which restores the chat model's unit.
func Run(d Deps, dir Direction) Result {
	r := Result{Direction: dir}
	r.Outcome = stackapply.Transact(d.Tx, func(cfg config.VillaConfig) (config.VillaConfig, *stackapply.Outcome) {
		r.FromModel, r.ToModel = cfg.Model, cfg.Model
		if subsystem.CodingModeOn(cfg) == (dir == Enter) {
			return cfg, &stackapply.Outcome{NoOp: true}
		}
		if dir == Exit {
			cfg.CodingMode = false
			cfg.CoderModel = ""
			cfg.CoderQuant = ""
			cfg.CoderAgentCtx = 0
			return cfg, nil
		}

		t, ok, reason := d.ResolveCoder(cfg)
		if !ok {
			return cfg, &stackapply.Outcome{Refused: true, Reason: reason}
		}
		r.Residency = t.Residency
		cfg.CodingMode = true
		cfg.CoderAgentCtx = t.AgentCtx
		// Shared residency records only the agent-ctx render delta; CoderModel stays
		// empty so the chat endpoint serves.
		if t.Residency == ResidencySwap {
			if t.Model != "" {
				r.ToModel = t.Model
			}
			if !t.Downloaded {
				if err := d.Pull(t); err != nil {
					return cfg, &stackapply.Outcome{FailedStep: "pull", Err: err}
				}
			}
			cfg.CoderModel = t.Model
			cfg.CoderQuant = t.Quant
		}
		return cfg, nil
	})
	return r
}
