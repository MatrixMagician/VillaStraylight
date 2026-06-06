// Package backendswap is the pure, Deps-injected transactional core for
// `villa backend set`: the capture→mutate→prove→rollback state-machine that a
// backend switch on a RUNNING install must go through so a failed or degraded
// switch is a no-op to the running stack (Phase 8 SC#2/SC#3, BSET-01/BSET-02).
//
// It clones the proven `internal/modelswap` forward skeleton (fit-guard FIRST,
// persist-before-unit-work, restart-inference-only) and wraps it in a
// transactional frame: the verbatim prior `villa-llama.container` bytes and the
// prior VillaConfig are captured STRICTLY BEFORE any mutation, the cutover is
// gated on an injected Prove verdict (real generation-probe + residency proof),
// and ANY mutate error or non-pass verdict rolls back to the verbatim captured
// unit+config and re-readies best-effort.
//
// Every host-touching action is an injected Deps field so the whole state-machine
// is driven from backendswap_test.go without a live host. The package is
// deliberately LITERAL-FREE of backend marker tokens (residency/override/image/fault) — those
// markers arrive ONLY through the injected Prove seam (the live wiring + the
// residency/generation content live in cmd/villa, Plan 02). It imports neither
// `internal/inference` nor `internal/detect`; the prove verdict is a locally
// defined value type so the marker discipline holds.
package backendswap

import (
	"context"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// ProveStatusPass is this package's own success sentinel for a cutover prove
// verdict. The cmd layer (Plan 02) sets ProveVerdict.Status to this constant when
// — and ONLY when — inference.StatusPass is reached (a real generation probe AND
// a positive residency proof). This is the package's OWN status sentinel, not a
// backend token: keeping the success marker here (rather than importing
// inference.StatusPass) is exactly what keeps backendswap free of inference/detect
// imports and of backend literals.
const ProveStatusPass = "pass"

// ProveVerdict is the LOCAL prove outcome the cutover gates on. It is defined here
// (not imported from inference) so backendswap imports neither inference nor
// detect and stays literal-free of backend markers. The cmd layer composes the
// real verdict (PollHealth + GenerationProbe + RunningOffloadVerdict) and maps it
// into this value, setting Status to ProveStatusPass only on a true pass.
type ProveVerdict struct {
	// Status is the prove outcome. The cutover succeeds ONLY when Status equals
	// ProveStatusPass; any other value (including a ready+health-200-but-residency-FAIL
	// verdict) triggers rollback — is-active/health-200 alone is NEVER success (SC#3).
	Status string
	// Detail is the human explanation carried into the Result on a non-pass verdict.
	Detail string
}

// Deps is the injectable seam set for the transactional core. Every host-touching
// action is a field so backendswap_test.go drives the whole capture→mutate→prove→
// rollback flow (and asserts ordering) without a live host. The live wiring
// (liveBackendSwapDeps) stays in cmd/villa (Plan 02).
type Deps struct {
	// LoadConfig loads the current persisted config (the source of truth). The
	// current backend is read from it; a same-backend target is a clean no-op.
	LoadConfig func() (config.VillaConfig, error)
	// FitsModel re-checks the PRESERVED model against the target envelope and
	// returns a human reason when it no longer fits — model = config, never re-pick
	// (BSET-01). A non-fit is a refuse-with-remediation with zero side effects.
	FitsModel func(cfg config.VillaConfig) (bool, string)
	// PreflightROCm is the ROCm preflight gate. It is meaningful only when the
	// target is rocm; the live seam short-circuits ok=true for non-rocm targets. A
	// not-ok is a refuse-with-remediation with zero side effects (BSET-01).
	PreflightROCm func(cfg config.VillaConfig) (ok bool, reason string)
	// CaptureUnit reads the verbatim prior villa-llama.container bytes BEFORE any
	// mutation, so a rollback restores the exact prior unit (Pitfall 4). An error
	// here refuses without mutating (an uncapturable prior unit must not be touched).
	CaptureUnit func() ([]byte, error)
	// SaveConfig persists the new backend to config.toml (the source of truth).
	SaveConfig func(c config.VillaConfig) error
	// ReconcileAndWrite renders units from the persisted config and writes only the
	// changed unit(s); the live closure performs the daemon-reload internally,
	// mirroring liveSwapDeps. It reports whether anything changed.
	ReconcileAndWrite func(c config.VillaConfig) (changed bool, err error)
	// RestoreUnit writes the verbatim captured prior unit bytes back during a
	// rollback (live impl goes through the traversal-guarded orchestrate.WriteUnits).
	RestoreUnit func(b []byte) error
	// DaemonReload reloads the user systemd manager (after a restore on rollback).
	DaemonReload func() error
	// Restart restarts ONLY the named service (the inference unit) — both on the
	// forward cutover and on the rollback re-ready.
	Restart func(service string) error
	// Prove is the injected cutover gate: it probes the ALREADY-running server and
	// returns a verdict. The core switches ONLY on ProveStatusPass. All backend
	// markers (residency/override/fault/image) live behind this seam — never in this package.
	Prove func(ctx context.Context, target string) ProveVerdict
	// InstallServiceName is the inference service the switch restarts (and ONLY that
	// service). A Deps field so backendswap need not import the cmd-layer constant.
	InstallServiceName string
}

// Result is the typed outcome of a backend switch (not an exit code), so the cobra
// caller (Plan 02) can branch on it and map it to an exit code + messages.
type Result struct {
	// Refused is true when the switch was rejected with ZERO side effects (same
	// backend is NoOp, not Refused; a fit/preflight/capture rejection is Refused).
	Refused bool
	// Switched is true when the cutover persisted config, restarted the inference
	// unit, AND the Prove verdict was ProveStatusPass.
	Switched bool
	// RolledBack is true when a mutate error or a non-pass Prove verdict triggered a
	// verbatim restore of the captured prior unit+config. It stays true even when a
	// rollback STEP itself errored — Reason/FailedStep then flag rollback-incomplete
	// (Pitfall 5: never claim a clean no-op when rollback errored).
	RolledBack bool
	// NoOp is true when the target backend equals the current backend — a clean
	// no-op with zero side effects (Open Question 1).
	NoOp bool
	// Reason is the human refusal/remediation/rollback explanation (empty on a clean
	// success).
	Reason string
	// Err is a non-refusal failure (capture/save/write/restart). Distinct from a
	// Refused (a clean policy rejection, not an error).
	Err error
	// FailedStep names the step that failed ("capture"/"save"/"write"/"restart"/
	// "prove") so the caller can print a precise message.
	FailedStep string
	// FromBackend / ToBackend are the previous and target backends.
	FromBackend string
	ToBackend   string
	// Prove carries the cutover verdict (on both a Switched and a prove-triggered
	// RolledBack result) for the caller to surface.
	Prove ProveVerdict
}

// Run performs the guarded, transactional backend switch and returns a typed
// Result. Ordering (per 08-PATTERNS.md):
//
//	(1) LoadConfig; from := cfg.Backend; same-backend → clean NoOp.
//	(2) fit-guard FIRST: re-check the PRESERVED model against the target envelope;
//	    a non-fit refuses-with-remediation, zero side effects (BSET-01).
//	(3) ROCm preflight gate (no-op for non-rocm targets); a not-ok refuses, zero
//	    side effects (BSET-01).
//	(4) CAPTURE strictly BEFORE any mutation: priorUnit bytes + priorCfg value
//	    snapshot (Pitfall 4). An uncapturable prior unit refuses without mutating.
//	(5) MUTATE: cfg.Backend = target; SaveConfig; ReconcileAndWrite; Restart ONLY
//	    the inference service. ANY error here rolls back verbatim.
//	(6) PROVE: switch ONLY on ProveStatusPass; any other verdict rolls back verbatim.
//
// Steps (5)/(6) rollback are hardened in task 08-01-02; this file already wires the
// full transactional frame.
func Run(d Deps, target string) Result {
	// (1) Load the source of truth; a same-backend target is a clean no-op with zero
	// side effects (Open Question 1).
	cfg, err := d.LoadConfig()
	if err != nil {
		return Result{Refused: true, FailedStep: "load config", Err: err, ToBackend: target}
	}
	from := cfg.Backend
	if from == target {
		return Result{NoOp: true, FromBackend: from, ToBackend: target}
	}

	// (2) Fit-guard FIRST (BSET-01): re-check the PRESERVED model against the target
	// envelope — model = config, never re-pick. A non-fit refuses-with-remediation
	// BEFORE any capture/mutate, zero side effects.
	if ok, reason := d.FitsModel(cfg); !ok {
		return Result{Refused: true, Reason: reason, FromBackend: from, ToBackend: target}
	}

	// (3) ROCm preflight gate (BSET-01). Meaningful only for a rocm target; the live
	// seam short-circuits ok=true otherwise. A not-ok refuses-with-remediation, zero
	// side effects.
	if ok, reason := d.PreflightROCm(cfg); !ok {
		return Result{Refused: true, Reason: reason, FromBackend: from, ToBackend: target}
	}

	// (4) CAPTURE strictly BEFORE any mutation (Pitfall 4): the verbatim prior unit
	// bytes and a value snapshot of the prior config. An uncapturable prior unit must
	// not be mutated — refuse with zero side effects.
	priorUnit, err := d.CaptureUnit()
	if err != nil {
		return Result{Refused: true, FailedStep: "capture", Err: err, FromBackend: from, ToBackend: target}
	}
	priorCfg := cfg // VillaConfig is a flat value type (no pointers) → safe deep snapshot.

	// rollback restores the verbatim captured prior unit+config and re-readies the
	// inference service, best-effort: it accumulates errors across all four steps
	// rather than aborting on the first, and reports whether EVERY step succeeded.
	// Per Pitfall 5, an incomplete rollback must be flagged honestly — never claim a
	// clean no-op when a restore step errored. Re-ready is best-effort and bounded by
	// the live Prove/poll wiring (Open Question 2), not by this pure core.
	rollback := func() (ok bool, detail string) {
		ok = true
		if err := d.RestoreUnit(priorUnit); err != nil {
			ok = false
			detail = "RestoreUnit failed: " + err.Error()
		}
		if err := d.SaveConfig(priorCfg); err != nil {
			ok = false
			detail = "SaveConfig(prior) failed: " + err.Error()
		}
		if err := d.DaemonReload(); err != nil {
			ok = false
			detail = "DaemonReload failed: " + err.Error()
		}
		if err := d.Restart(d.InstallServiceName); err != nil {
			ok = false
			detail = "Restart(prior) failed: " + err.Error()
		}
		return ok, detail
	}

	// rolledBack assembles a RolledBack Result, folding in an honest rollback-incomplete
	// message when the restore did not fully succeed (Pitfall 5).
	rolledBack := func(failedStep, reason string, origErr error, v ProveVerdict) Result {
		ok, rbDetail := rollback()
		r := Result{
			RolledBack:  true,
			FailedStep:  failedStep,
			Reason:      reason,
			Err:         origErr,
			Prove:       v,
			FromBackend: from,
			ToBackend:   target,
		}
		if !ok {
			// Do NOT present a half-restored stack as a clean no-op: flag it.
			r.Reason = "rolled back, but the restore did not fully complete (" + rbDetail +
				") — run `villa status` and inspect the villa-llama unit"
		}
		return r
	}

	// (5) MUTATE. ANY error here rolls back verbatim to the captured prior unit+config.
	cfg.Backend = target
	if err := d.SaveConfig(cfg); err != nil {
		return rolledBack("save", "", err, ProveVerdict{})
	}
	if _, err := d.ReconcileAndWrite(cfg); err != nil {
		return rolledBack("write", "", err, ProveVerdict{})
	}
	if err := d.Restart(d.InstallServiceName); err != nil {
		return rolledBack("restart", "", err, ProveVerdict{})
	}

	// (6) PROVE the cutover against the already-running server. Switch ONLY on
	// ProveStatusPass; ANY other verdict (including ready+health-200-but-residency-FAIL,
	// SC#3) rolls back verbatim — is-active/200 alone is never success.
	v := d.Prove(context.Background(), target)
	if v.Status != ProveStatusPass {
		return rolledBack("prove", v.Detail, nil, v)
	}
	return Result{Switched: true, Prove: v, FromBackend: from, ToBackend: target}
}
