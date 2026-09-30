// transact.go is the swap transaction frame (ADR-0015): the one capture → mutate →
// prove → rollback a change to the RUNNING stack goes through, so a failed or
// unproven change is a no-op to it. `backend set`, `speculation set`, `tools-mode`,
// `coding-mode` and `model swap` (CLI and dashboard) each supply only a Change: read
// the locked config, refuse or return the target. The frame owns the rest:
//
//   - the stack lock (ADR-0010), taken before the config is read, so a Change and
//     the rollback see one config no other stack mutation can write underneath;
//   - the capture of the prior config and of every unit the prior config renders;
//   - the apply through this module (Apply), which reports the units it changed;
//   - the restart of every changed service that is running, plus the proven
//     service whether or not it was, since the proof needs it serving (#251);
//   - the proof, and a rollback that restores every captured unit and the prior
//     config, reloads, restarts what the cutover restarted, and says so when a step
//     of it failed.
//
// Before this frame each of backendswap, codingmode and modelswap carried a copy of
// it, the lock was taken by five cobra callers only (bench --ab ran unlocked, #250),
// and the restart set was villa-llama alone (#251).
package stackapply

import (
	"context"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
	"github.com/MatrixMagician/VillaStraylight/internal/stacklock"
)

// TxDeps is the host a swap transaction runs against. cmd/villa wires one live set
// over this module's Apply and Restore; tests wire fakes. Every field is required.
type TxDeps struct {
	// Lock takes the stack lock: stacklock.Acquire (blocking) for a CLI verb,
	// stacklock.TryAcquire (ErrBusy at once) for the dashboard.
	Lock       func() (*stacklock.Lock, error)
	LoadConfig func() (config.VillaConfig, error)
	SaveConfig func(config.VillaConfig) error
	// Capture returns the on-disk bytes of every unit cfg renders, keyed by unit
	// filename. A unit never written is absent.
	Capture func(cfg config.VillaConfig) (map[string]string, error)
	// Apply writes the target config's changed units and reloads (Apply in this
	// package), returning the units it wrote.
	Apply func(cfg config.VillaConfig) ([]orchestrate.Unit, error)
	// Restore writes captured unit bytes back verbatim (Restore in this package).
	Restore      func(units map[string]string) error
	DaemonReload func() error
	IsActive     func(service string) (string, error)
	Restart      func(service string) error
	// Prove is the cutover gate for the target config's backend. The frame switches
	// only on prove.StatusPass.
	Prove func(ctx context.Context, backend string) prove.Verdict
	// Service is the service Prove drives (villa-llama.service).
	Service string
}

// Outcome is the frame's typed result; each swap core embeds it in its own Result.
type Outcome struct {
	// Refused: rejected with zero side effects, by the Change or because the prior
	// units could not be captured.
	Refused bool
	// Switched: config persisted, changed units restarted, and the proof passed.
	Switched bool
	// RolledBack: a mutate error or a failed proof restored the captured units and
	// config. It stays true when a rollback step failed; Reason then says so.
	RolledBack bool
	// NoOp: nothing to change, either because the Change said so or because the
	// persisted target renders the units already on disk (nothing restarted or proven).
	NoOp bool
	// Reason is the refusal, the failed proof's detail, or the rollback-incomplete note.
	Reason string
	// Err is a failure that is not a policy refusal.
	Err error
	// FailedStep names the step that failed: "lock", "load config", a Change's own
	// step, "capture", "save", "write", "restart" or "prove".
	FailedStep string
	// Prove is the cutover verdict, on a switch and on a proof-triggered rollback.
	Prove prove.Verdict
}

// A Change reads the locked config and returns the target config, or an Outcome that
// stops the transaction before anything is captured (a refusal, a no-op, a failed
// pull).
type Change func(cfg config.VillaConfig) (next config.VillaConfig, stop *Outcome)

// Transact runs change as one transaction under the stack lock:
//
//	(1) Lock, then LoadConfig; change decides the target or stops.
//	(2) Capture the prior units strictly before any mutation; a failure refuses.
//	(3) SaveConfig, Apply; nothing changed on disk is a NoOp with no restart.
//	(4) Restart every changed service that is running, and the proven service.
//	(5) Prove; switch only on a pass.
//
// Any error in (3)-(4), or a failed proof, rolls back.
func Transact(d TxDeps, change Change) Outcome {
	lock, err := d.Lock()
	if err != nil {
		return Outcome{FailedStep: "lock", Err: err}
	}
	defer func() { _ = lock.Release() }()

	prior, err := d.LoadConfig()
	if err != nil {
		return Outcome{FailedStep: "load config", Err: err}
	}
	next, stop := change(prior)
	if stop != nil {
		return *stop
	}

	units, err := d.Capture(prior)
	if err != nil {
		return Outcome{Refused: true, FailedStep: "capture", Err: err}
	}

	// restarted is every service the cutover restarted or tried to, which is the set
	// the rollback re-readies: a service never restarted still runs its prior unit.
	var restarted []string
	rollback := func(step, reason string, cause error, v prove.Verdict) Outcome {
		var fails []string
		if err := d.Restore(units); err != nil {
			fails = append(fails, "RestoreUnits failed: "+err.Error())
		}
		if err := d.SaveConfig(prior); err != nil {
			fails = append(fails, "SaveConfig(prior) failed: "+err.Error())
		}
		if err := d.DaemonReload(); err != nil {
			fails = append(fails, "DaemonReload failed: "+err.Error())
		}
		for _, svc := range restarted {
			if err := d.Restart(svc); err != nil {
				fails = append(fails, "Restart("+svc+") failed: "+err.Error())
			}
		}
		o := Outcome{RolledBack: true, FailedStep: step, Reason: reason, Err: cause, Prove: v}
		if len(fails) > 0 {
			// Never present a half-restored stack as a clean no-op.
			o.Reason = "rolled back, but the restore did not fully complete (" + strings.Join(fails, "; ") +
				") — run `villa status` and inspect the changed units"
		}
		return o
	}

	if err := d.SaveConfig(next); err != nil {
		return rollback("save", "", err, prove.Verdict{})
	}
	changed, err := d.Apply(next)
	if err != nil {
		return rollback("write", "", err, prove.Verdict{})
	}
	if len(changed) == 0 {
		return Outcome{NoOp: true}
	}
	for _, u := range changed {
		name, ok := strings.CutSuffix(u.Name, ".container")
		if !ok {
			continue
		}
		svc := name + ".service"
		if svc != d.Service && !running(d, svc) {
			continue
		}
		restarted = append(restarted, svc)
		if err := d.Restart(svc); err != nil {
			return rollback("restart", "", err, prove.Verdict{})
		}
	}

	v := d.Prove(context.Background(), next.Backend)
	if !v.Pass() {
		return rollback("prove", v.Detail, nil, v)
	}
	return Outcome{Switched: true, Prove: v}
}

// running reports whether svc is up. A state that cannot be read counts as running:
// restarting a stopped unit starts it, but skipping a running one leaves it serving
// bytes its unit file no longer names.
func running(d TxDeps, svc string) bool {
	state, err := d.IsActive(svc)
	return err != nil || state == "active" || state == "activating" || state == "reloading"
}
