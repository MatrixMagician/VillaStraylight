// Package toolsmode is the pure, Deps-injected transactional core for
// `villa tools-mode enter|exit`: the capture→mutate→prove→rollback state-machine
// that flipping the RUNNING chat unit into tool-calling (and back) must go through,
// so a failed or degraded cutover is a no-op to the running stack (spec v1.11 §3.5).
//
// It is internal/backendswap's frame with the backend axis swapped for a boolean
// one, and it reuses that package's Deps and Result verbatim rather than declaring a
// second vocabulary for the same seams: the two verbs restart the same service,
// capture the same unit and roll back the same way. That package's transact step is
// unexported, which is the only reason the body below repeats it.
//
// The fit guard runs BEFORE any capture. Tools mode serves the ctx floor
// max(cfg.Ctx, agent_ctx), and a floor that does not fit the memory envelope is a
// refusal with the fit remediation and zero side effects, never a cutover that rolls
// back after having already restarted the chat unit.
//
// Like backendswap, this package is literal-free of backend markers and of the
// tool-calling flag itself: the flag is a render output of internal/inference, and
// the cutover verdict arrives only through the injected Prove seam.
package toolsmode

import (
	"context"

	"github.com/MatrixMagician/VillaStraylight/internal/backendswap"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// State labels for Result.From / Result.To. They are the words the verb prints, so a
// rolled-back result names the state the operator is back on.
const (
	StateOn  = "on"
	StateOff = "off"
)

// Label renders a persisted tools-mode boolean as the Result vocabulary.
func Label(on bool) string {
	if on {
		return StateOn
	}
	return StateOff
}

// Run performs the guarded, transactional tools-mode cutover and returns
// backendswap's typed Result, so one Result shape maps to exit codes for every
// transactional verb.
//
// Ordering:
//
//	(1) LoadConfig; a target equal to the ANSWERED gate is a clean NoOp, and an exit
//	    while coding mode holds the gate on is a refusal naming that flag.
//	(2) fit-guard FIRST, seeing the TARGET state: the ctx floor tools mode serves
//	    must fit the envelope. A non-fit refuses with the remediation and zero side
//	    effects, so nothing is captured and nothing is mutated.
//	(3) CAPTURE the verbatim prior unit bytes and prior config strictly before any
//	    mutation. An uncapturable prior unit refuses without mutating.
//	(4) MUTATE: persist the flag, re-render, restart ONLY the inference service.
//	(5) PROVE: the injected verdict is the residency proof plus the tool-call edit
//	    probe. Any mutate error or non-pass verdict rolls back verbatim, and a
//	    rollback that did not fully complete is reported as such rather than as a
//	    clean no-op.
func Run(d backendswap.Deps, on bool) backendswap.Result {
	cfg, err := d.LoadConfig()
	if err != nil {
		return backendswap.Result{Refused: true, FailedStep: "load config", Err: err, To: Label(on)}
	}
	// The no-op is decided on the ANSWERED gate, not on the raw flag: coding mode
	// implies tool calling, so entering on a coding-mode stack is already there.
	from, to := Label(subsystem.ToolsOn(cfg)), Label(on)
	if subsystem.ToolsOn(cfg) == on {
		return backendswap.Result{NoOp: true, From: from, To: to}
	}

	// Exiting while coding mode holds the gate on would persist tools_mode=false,
	// render an unchanged unit and report a cutover that changed nothing. Refuse and
	// name the flag that actually holds it.
	if !on && subsystem.CodingModeOn(cfg) {
		return backendswap.Result{
			Refused: true,
			Reason:  "coding mode implies tools mode — run `villa coding-mode exit` to stop serving tool calls",
			From:    from,
			To:      to,
		}
	}

	// The guard sees the TARGET state. A same-state target is already a NoOp above,
	// so passing the persisted config would leave the ctx-floor check permanently
	// dead on the only path that can raise the served context.
	fitCfg := cfg
	fitCfg.ToolsMode = on
	if ok, reason := d.FitsModel(fitCfg); !ok {
		return backendswap.Result{Refused: true, Reason: reason, From: from, To: to}
	}

	priorUnit, err := d.CaptureUnit()
	if err != nil {
		return backendswap.Result{Refused: true, FailedStep: "capture", Err: err, From: from, To: to}
	}
	priorCfg := cfg // VillaConfig is a flat value type, so this is a deep snapshot.

	rollback := func() (bool, string) {
		ok, detail := true, ""
		if err := d.RestoreUnit(priorUnit); err != nil {
			ok, detail = false, "RestoreUnit failed: "+err.Error()
		}
		if err := d.SaveConfig(priorCfg); err != nil {
			ok, detail = false, "SaveConfig(prior) failed: "+err.Error()
		}
		if err := d.DaemonReload(); err != nil {
			ok, detail = false, "DaemonReload failed: "+err.Error()
		}
		if err := d.Restart(d.InstallServiceName); err != nil {
			ok, detail = false, "Restart(prior) failed: "+err.Error()
		}
		return ok, detail
	}

	rolledBack := func(failedStep, reason string, origErr error, v prove.Verdict) backendswap.Result {
		ok, rbDetail := rollback()
		r := backendswap.Result{
			RolledBack: true,
			FailedStep: failedStep,
			Reason:     reason,
			Err:        origErr,
			Prove:      v,
			From:       from,
			To:         to,
		}
		if !ok {
			r.Reason = "rolled back, but the restore did not fully complete (" + rbDetail +
				") — run `villa status` and inspect the villa-llama unit"
		}
		return r
	}

	cfg.ToolsMode = on
	if err := d.SaveConfig(cfg); err != nil {
		return rolledBack("save", "", err, prove.Verdict{})
	}
	if _, err := d.ReconcileAndWrite(cfg); err != nil {
		return rolledBack("write", "", err, prove.Verdict{})
	}
	if err := d.Restart(d.InstallServiceName); err != nil {
		return rolledBack("restart", "", err, prove.Verdict{})
	}

	v := d.Prove(context.Background(), cfg.Backend)
	if !v.Pass() {
		return rolledBack("prove", v.Detail, nil, v)
	}
	return backendswap.Result{Switched: true, Prove: v, From: from, To: to}
}
