// Package modelswap is the extracted `villa model swap` guarded orchestration core
// the ordering-is-the-security-contract sequence that resolves a model
// through the catalog, refuses a non-fitting target BEFORE any side effect, auto-pulls
// absent weights, persists config BEFORE unit work, regenerates units, restarts
// ONLY the inference service, and proves the cutover before calling it Switched.
//
// It was moved VERBATIM out of cmd/villa/model.go runModelSwap (STATE.md [03-05]: the
// swap ordering IS the security contract) so the dashboard's POST
// /api/models/switch handler can call the SAME path the CLI does, not a fork. Run
// returns a typed Result (not an exit code) so the dashboard handler can branch on it
// (RESEARCH: "Deps + Run() returning a typed result, not an exit code").
//
// #237 gave it the same capture→mutate→prove→rollback transaction the other swap
// cores (backendswap, codingmode) already had: a failure after SaveConfig used to
// leave the config out of step with the units, or leave inference down, and a
// "switched" result carried no proof that the new model was actually serving.
//
// All host-touching actions are injected via Deps; modelswap itself stays free of
// catalog-load/download/systemd coupling (the live wiring lives in cmd/villa).
package modelswap

import (
	"context"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
)

// Deps is the injectable seam set for the swap core. Every host-touching action is a
// field so modelswap_test.go drives the whole flow (and asserts ordering) without a
// live host. The live wiring (liveSwapDeps) stays in cmd/villa.
type Deps struct {
	LoadConfig     func() (config.VillaConfig, error)
	ResolveCatalog func(name string) (catalog.Model, bool)
	// Fits reports whether m fits the usable envelope (reuse recommend fit-math)
	// and a human reason when it does not — never a silent OOM at container start.
	Fits func(m catalog.Model) (bool, string)
	// IsDownloaded reports whether the model's weights are already on disk.
	IsDownloaded func(m catalog.Model) bool
	// Pull auto-downloads the verified weights (reuse download.PullModel).
	Pull func(m catalog.Model) error
	// CaptureUnit reads the verbatim prior main inference unit bytes BEFORE any
	// mutation (Pitfall 4, #237), so a rollback restores exactly what was running,
	// not merely the prior config. An error here refuses without mutating.
	CaptureUnit func() ([]byte, error)
	// SaveConfig persists the new model to config.toml (the source of truth).
	SaveConfig func(c config.VillaConfig) error
	// ReconcileAndWrite renders units from the persisted config and writes only the
	// changed unit(s). It reports whether anything actually changed so the caller can
	// skip a needless restart on a no-op swap.
	ReconcileAndWrite func(c config.VillaConfig) (bool, error)
	// RestoreUnit writes the verbatim captured prior unit bytes back during a
	// rollback.
	RestoreUnit func(b []byte) error
	// DaemonReload reloads the user systemd manager (after a restore on rollback).
	DaemonReload func() error
	// Restart restarts ONLY the named service (the inference unit) — on the
	// forward cutover and on the rollback re-ready.
	Restart func(service string) error
	// Prove is the injected cutover gate (#237): it probes the ALREADY-running
	// server after the switch and returns a verdict. The swap counts as Switched
	// ONLY on prove.StatusPass; any other verdict rolls back verbatim — is-active
	// alone is never success.
	Prove func(ctx context.Context) prove.Verdict
	// InstallServiceName is the inference service the swap restarts (and ONLY that
	// service). It is a Deps field so modelswap need not import the cmd-layer
	// install.go constant (no package-main cycle).
	InstallServiceName string
}

// Result is the typed outcome of a swap, replacing the old exit code so the dashboard
// handler can branch on it (and the cobra caller maps it to an exit code + messages).
type Result struct {
	// Refused is true when the swap was rejected with zero side effects (unknown
	// model, a non-fitting target, or an uncapturable prior unit). Reason carries
	// the human explanation.
	Refused bool
	// Reason is the refusal/rollback explanation (empty on a clean Switched/NoOp).
	Reason string
	// Unknown is true when the model id did not resolve through the catalog (a refusal
	// sub-case the caller surfaces with an "unknown model" hint).
	Unknown bool
	// Err is a non-refusal failure (config load, save, pull, reconcile, restart). It is
	// distinct from a Refused (which is a clean policy rejection, not an error).
	Err error
	// FailedStep names the step Err occurred at ("pull"/"load config"/"capture"/
	// "persist config"/"regenerate units"/"restart"/"prove") so the caller prints
	// the same message it used to.
	FailedStep string
	// Pulled is true when the target weights were auto-downloaded during the swap.
	Pulled bool
	// Switched is true when the swap persisted config, restarted the inference
	// unit, AND the Prove verdict was prove.StatusPass.
	Switched bool
	// RolledBack is true when a mutate error or a non-pass Prove verdict triggered
	// a verbatim restore of the captured prior unit+config. It stays true even
	// when a rollback STEP itself errored — Reason then flags rollback-incomplete
	// (Pitfall 5: never claim a clean no-op when rollback errored).
	RolledBack bool
	// NoOp is true when config was persisted but the units were already up to date, so
	// no restart (and no prove) was needed.
	NoOp bool
	// FromModel / ToModel are the previous and new model ids (ToModel set on any
	// resolved target; FromModel from the loaded config).
	FromModel string
	ToModel   string
	// Prove carries the cutover verdict (on both a Switched and a prove-triggered
	// RolledBack result) for the caller to surface.
	Prove prove.Verdict
}

// Run performs the guarded, transactional swap and returns a typed Result. Ordering
// is the security contract, preserved from the old runModelSwap with the ADR-0003-
// style transaction #237 added at step (4):
// (1) resolve through catalog, (2) fit-guard refuse, (3) auto-pull if absent,
// (4) CAPTURE strictly BEFORE any mutation, (5) persist config, (6) reconcileAndWrite,
// (7) restart ONLY the inference service, skipping restart+prove on a no-op,
// (8) PROVE the cutover — switch ONLY on prove.StatusPass; any other verdict, or any
// error in (5)-(7), rolls back to the captured prior unit+config verbatim.
func Run(d Deps, name string) Result {
	// (1) Resolve the name THROUGH the catalog — never as a filesystem path
	// (command-injection / path-traversal guard). Unknown → refuse, zero
	// side effects.
	m, ok := d.ResolveCatalog(name)
	if !ok {
		return Result{Refused: true, Unknown: true, Reason: "unknown model", ToModel: name}
	}

	// (2) Fit-guard: refuse a target that won't fit the envelope
	// BEFORE any pull/persist/restart — never a silent OOM at container start.
	if fits, reason := d.Fits(m); !fits {
		return Result{Refused: true, Reason: reason, ToModel: m.ID}
	}

	// (3) Auto-pull the verified weights if absent. Reuses the same
	// verified/resumable downloader as `model pull`.
	pulled := false
	if !d.IsDownloaded(m) {
		if err := d.Pull(m); err != nil {
			return Result{Err: err, FailedStep: "pull", ToModel: m.ID}
		}
		pulled = true
	}

	cfg, err := d.LoadConfig()
	if err != nil {
		return Result{Err: err, FailedStep: "load config", Pulled: pulled, ToModel: m.ID}
	}
	fromModel := cfg.Model

	// (4) CAPTURE strictly BEFORE any mutation (Pitfall 4): the verbatim prior unit
	// bytes and a value snapshot of the prior config. An uncapturable prior unit
	// must not be mutated — refuse with zero side effects.
	priorUnit, err := d.CaptureUnit()
	if err != nil {
		return Result{Refused: true, FailedStep: "capture", Err: err, Pulled: pulled, FromModel: fromModel, ToModel: m.ID}
	}
	priorCfg := cfg // VillaConfig is a flat value type (no pointers) → safe deep snapshot.

	// rollback restores the verbatim captured prior unit+config and re-readies the
	// inference service, best-effort: it accumulates errors across all four steps
	// rather than aborting on the first, and reports whether EVERY step succeeded.
	// Per Pitfall 5, an incomplete rollback must be flagged honestly.
	rollback := func() (ok bool, detail string) {
		ok = true
		var fails []string
		if err := d.RestoreUnit(priorUnit); err != nil {
			ok = false
			fails = append(fails, "RestoreUnit failed: "+err.Error())
		}
		if err := d.SaveConfig(priorCfg); err != nil {
			ok = false
			fails = append(fails, "SaveConfig(prior) failed: "+err.Error())
		}
		if err := d.DaemonReload(); err != nil {
			ok = false
			fails = append(fails, "DaemonReload failed: "+err.Error())
		}
		if err := d.Restart(d.InstallServiceName); err != nil {
			ok = false
			fails = append(fails, "Restart(prior) failed: "+err.Error())
		}
		return ok, strings.Join(fails, "; ")
	}

	// rolledBack assembles a RolledBack Result, folding in an honest rollback-
	// incomplete message when the restore did not fully succeed (Pitfall 5).
	rolledBack := func(failedStep, reason string, origErr error, v prove.Verdict) Result {
		ok, rbDetail := rollback()
		r := Result{
			RolledBack: true,
			FailedStep: failedStep,
			Reason:     reason,
			Err:        origErr,
			Prove:      v,
			Pulled:     pulled,
			FromModel:  fromModel,
			ToModel:    m.ID,
		}
		if !ok {
			// Do NOT present a half-restored stack as a clean no-op: flag it.
			r.Reason = "rolled back, but the restore did not fully complete (" + rbDetail +
				") — run `villa status` and inspect the villa-llama unit"
		}
		return r
	}

	// (5) Persist the new model to config.toml BEFORE any unit work (config is the
	// single source of truth, so it must never lag the running unit). ANY error
	// from here on rolls back verbatim to the captured prior unit+config.
	cfg.Model = m.ID
	if m.Quant != "" {
		cfg.Quant = m.Quant
	}
	if err := d.SaveConfig(cfg); err != nil {
		return rolledBack("persist config", "", err, prove.Verdict{})
	}

	// (6)-(7) Regenerate the inference unit args from the persisted config and
	// restart ONLY the inference service — but only when the regenerate actually
	// changed a unit, so a no-op swap does not trigger a multi-second model reload
	// for nothing (WR-06), and needs no prove (nothing was cut over).
	changed, err := d.ReconcileAndWrite(cfg)
	if err != nil {
		return rolledBack("regenerate units", "", err, prove.Verdict{})
	}
	if !changed {
		return Result{NoOp: true, Pulled: pulled, FromModel: fromModel, ToModel: m.ID}
	}
	if err := d.Restart(d.InstallServiceName); err != nil {
		return rolledBack("restart", "", err, prove.Verdict{})
	}

	// (8) PROVE the cutover against the already-running server. Switch ONLY on
	// prove.StatusPass; any other verdict rolls back verbatim — is-active alone is
	// never success.
	v := d.Prove(context.Background())
	if !v.Pass() {
		return rolledBack("prove", v.Detail, nil, v)
	}

	return Result{Switched: true, Pulled: pulled, Prove: v, FromModel: fromModel, ToModel: m.ID}
}
