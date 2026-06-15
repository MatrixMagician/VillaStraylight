// Package codingmode is the pure, Deps-injected transactional core for
// `villa coding-mode enter|exit` (CMODE-02): the capture→mutate→prove→rollback
// state-machine that flipping the RUNNING stack into a tool-calling coding mode
// (and back) must go through so a failed or degraded cutover is a NO-OP to the
// running stack.
//
// It CLONES the proven internal/backendswap transactional frame verbatim in shape
// (capture the prior villa-llama.container bytes + the prior VillaConfig value
// snapshot STRICTLY BEFORE any mutation → mutate (persist config, reconcile+write,
// restart inference only) → under-load Prove → ANY mutate error or non-pass verdict
// rolls back to the verbatim captured unit+config with honest rollback-incomplete
// reporting). It swaps backendswap's "backend string" axis for a "coding-mode
// Direction (enter|exit) + resolved coder model + render-delta" axis, and COMPOSES
// internal/modelswap's forward ordering (resolve→fit-guard→pull→persist→reconcile→
// restart) for the swap-residency model change — it does NOT fork modelswap (D-07).
//
// Locked decisions realized here:
//   - D-06: the mode changes ONLY via the explicit verb; nothing in this core (or any
//     caller) auto-flips coding_mode. Same-state enter/exit is a clean NoOp.
//   - D-07: clone backendswap's frame; compose modelswap's forward ordering.
//   - D-08: exit restores the captured prior chat model under the SAME (capture→
//     mutate→prove→rollback) discipline — not a bare coding_mode=false write.
//   - D-09: the cutover succeeds ONLY on ProveStatusPass (a real generation probe AND
//     a positive residency proof under load). A silent/partial CPU fallback or a
//     ready+health-200-but-residency-FAIL verdict rolls back — idle-green is never green.
//   - D-10: residency mode drives the enter path. "swap" performs the model change
//     (primary path); "shared" applies the render delta to the EXISTING chat endpoint
//     WITHOUT a model change. The core never silently degrades swap→shared — the chosen
//     residency is surfaced verbatim in the Result.
//
// Every host-touching action is an injected Deps field so the whole state-machine is
// driven from codingmode_test.go without a live host. The package is deliberately
// LITERAL-FREE of backend marker tokens (residency/override/image/fault) — those
// markers arrive ONLY through the injected Prove seam (the live wiring + the
// residency/generation content live in cmd/villa, Task 2). It imports NEITHER
// internal/inference NOR internal/detect; the prove verdict is a locally defined
// value type so the marker discipline holds (TestSeamGrepGate walks internal/).
package codingmode

import (
	"context"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// ProveStatusPass is this package's OWN success sentinel for a cutover prove verdict
// (cloned from backendswap — NOT imported from inference.StatusPass). The cmd layer
// (Task 2) sets ProveVerdict.Status to this constant when — and ONLY when —
// inference.StatusPass is reached (a real generation probe AND a positive residency
// proof under load). Keeping the success marker here (rather than importing
// inference.StatusPass) is exactly what keeps codingmode free of inference/detect
// imports and of backend literals.
const ProveStatusPass = "pass"

// Residency mode values (D-10). These mirror recommend's residency vocabulary but are
// re-declared LOCALLY so codingmode imports neither internal/recommend nor
// internal/inference: the live wiring (Task 2) maps recommend.Pick(...).Coder.Residency
// into these. "swap" performs the model change; "shared" applies render-delta-only.
const (
	ResidencySwap   = "swap"
	ResidencyShared = "shared"
)

// Direction is the axis replacing backendswap's target-backend string: which way the
// transactional cutover runs.
type Direction int

const (
	// Enter flips chat → coding mode (swap residency: swap the chat model for the
	// resolved coder model + render delta; shared residency: render-delta-only on the
	// existing chat endpoint).
	Enter Direction = iota
	// Exit restores the captured prior chat model + clears coding mode, under the SAME
	// transactional discipline as Enter (D-08).
	Exit
)

// String renders the Direction for messages/logs.
func (d Direction) String() string {
	if d == Exit {
		return "exit"
	}
	return "enter"
}

// ProveVerdict is the LOCAL prove outcome the cutover gates on. It is defined here
// (not imported from inference) so codingmode imports neither inference nor detect and
// stays literal-free of backend markers. The cmd layer composes the real verdict
// (PollHealth + GenerationProbe + RunningOffloadVerdict) and maps it into this value,
// setting Status to ProveStatusPass only on a true pass.
type ProveVerdict struct {
	// Status is the prove outcome. The cutover succeeds ONLY when Status equals
	// ProveStatusPass; any other value (including a ready+health-200-but-residency-FAIL
	// verdict) triggers rollback — is-active/health-200 alone is NEVER success (D-09).
	Status string
	// Detail is the human explanation carried into the Result on a non-pass verdict.
	Detail string
}

// CoderTarget is the resolved enter-time coder selection: the catalog-resolved model
// the enter path swaps to (swap residency) plus the agent-profile fields persisted to
// config AT ENTER (D-04). It is produced by the injected ResolveCoder seam so this core
// imports neither internal/catalog nor internal/recommend. On shared residency Model is
// empty (no model change) and only the render delta is applied (D-10).
type CoderTarget struct {
	// Model / Quant are the resolved coder catalog id + quantization (swap residency).
	// Empty on shared residency (no model change).
	Model string
	Quant string
	// AgentCtx is the catalog-declared agent-profile context the unit is rendered with
	// (the single -c, Pitfall 1) — persisted as cfg.CoderAgentCtx AT ENTER (D-04).
	AgentCtx int
	// Residency is the derived mode (ResidencySwap | ResidencyShared) — a PURE fit-math
	// output, never a preference (D-10). Surfaced verbatim in the Result so a shared
	// cutover is never silently presented as a swap.
	Residency string
	// Downloaded reports whether the resolved coder weights are already on disk; when
	// false on a swap-residency enter the core auto-pulls via the Pull seam (modelswap
	// step 3). Ignored on shared residency.
	Downloaded bool
}

// Deps is the injectable seam set for the transactional core. Every host-touching
// action is a field so codingmode_test.go drives the whole capture→mutate→prove→
// rollback flow (and asserts ordering) without a live host. The live wiring
// (liveCodingModeDeps) stays in cmd/villa (Task 2).
type Deps struct {
	// LoadConfig loads the current persisted config (the source of truth). The current
	// coding-mode state is read from it; a same-state target is a clean NoOp.
	LoadConfig func() (config.VillaConfig, error)
	// ResolveCoder resolves the enter-time coder target through the catalog + recommend
	// fit-math AT AgentCtx (D-04/D-10). It is the composed modelswap resolve→fit-guard:
	// ok=false is a refuse-with-remediation (the preserved-or-resolved model does not
	// fit the agent-ctx envelope) with ZERO side effects (Pitfall 4 — fit-guard FIRST).
	// It is meaningful only on Enter; the live seam need not be called on Exit.
	ResolveCoder func(cfg config.VillaConfig) (t CoderTarget, ok bool, reason string)
	// Pull auto-downloads the verified coder weights when absent on a swap-residency
	// enter (modelswap step 3; reuse download.PullModel). Composed, not forked.
	Pull func(t CoderTarget) error
	// CaptureUnit reads the verbatim prior villa-llama.container bytes BEFORE any
	// mutation, so a rollback restores the exact prior unit (Pitfall 5). An error here
	// refuses without mutating (an uncapturable prior unit must not be touched).
	CaptureUnit func() ([]byte, error)
	// SaveConfig persists the new coding-mode state to config.toml (the source of truth).
	SaveConfig func(c config.VillaConfig) error
	// ReconcileAndWrite renders units from the persisted config and writes only the
	// changed unit(s); the live closure performs the daemon-reload internally. It
	// reports whether anything changed.
	ReconcileAndWrite func(c config.VillaConfig) (changed bool, err error)
	// RestoreUnit writes the verbatim captured prior unit bytes back during a rollback
	// (live impl goes through the traversal-guarded orchestrate.WriteUnits).
	RestoreUnit func(b []byte) error
	// DaemonReload reloads the user systemd manager (after a restore on rollback).
	DaemonReload func() error
	// Restart restarts ONLY the named service (the inference unit) — both on the forward
	// cutover and on the rollback re-ready.
	Restart func(service string) error
	// Prove is the injected cutover gate: it probes the ALREADY-running server under load
	// and returns a verdict. The core switches ONLY on ProveStatusPass. All backend
	// markers (residency/override/fault/image) live behind this seam — never in this package.
	Prove func(ctx context.Context, dir Direction) ProveVerdict
	// InstallServiceName is the inference service the cutover restarts (and ONLY that
	// service). A Deps field so codingmode need not import the cmd-layer constant.
	InstallServiceName string
}

// Result is the typed outcome of a coding-mode cutover (not an exit code), so the cobra
// caller (Task 2) can branch on it and map it to an exit code + messages.
type Result struct {
	// Refused is true when the cutover was rejected with ZERO side effects (a same-state
	// target is NoOp, not Refused; a fit/capture rejection is Refused).
	Refused bool
	// Switched is true when the cutover persisted config, restarted the inference unit,
	// AND the Prove verdict was ProveStatusPass.
	Switched bool
	// RolledBack is true when a mutate error or a non-pass Prove verdict triggered a
	// verbatim restore of the captured prior unit+config. It stays true even when a
	// rollback STEP itself errored — Reason/FailedStep then flag rollback-incomplete
	// (Pitfall 5: never claim a clean no-op when rollback errored).
	RolledBack bool
	// NoOp is true when the target state equals the current state (enter while already
	// coding, exit while already chat) — a clean no-op with zero side effects.
	NoOp bool
	// Reason is the human refusal/remediation/rollback explanation (empty on a clean
	// success).
	Reason string
	// Err is a non-refusal failure (capture/save/write/restart/pull). Distinct from a
	// Refused (a clean policy rejection, not an error).
	Err error
	// FailedStep names the step that failed ("capture"/"pull"/"save"/"write"/"restart"/
	// "prove") so the caller can print a precise message.
	FailedStep string
	// Direction is the cutover direction (enter|exit).
	Direction Direction
	// FromModel / ToModel are the previous and target chat/coder model ids. On shared
	// residency ToModel equals FromModel (no model change, D-10).
	FromModel string
	ToModel   string
	// Residency is the residency mode the enter path took (ResidencySwap | ResidencyShared),
	// surfaced so a shared cutover is never silently presented as a swap (D-10). Empty on Exit.
	Residency string
	// Prove carries the cutover verdict (on both a Switched and a prove-triggered
	// RolledBack result) for the caller to surface.
	Prove ProveVerdict
}

// Run performs the guarded, transactional coding-mode cutover and returns a typed
// Result. Ordering CLONES backendswap.Run and interleaves modelswap's forward steps
// (D-07):
//
//	(1) LoadConfig; same-state (enter while already coding, exit while already chat) →
//	    clean NoOp, zero side effects (D-06).
//	(2) Resolve the target. On ENTER: ResolveCoder (composed modelswap resolve + fit-guard
//	    AT AgentCtx, Pitfall 4) — a non-fit refuses-with-remediation BEFORE any capture/
//	    mutate, zero side effects; on swap residency auto-pull the weights if absent
//	    (modelswap step 3). On EXIT: the target is the captured prior chat model restored
//	    from config (the model recorded before enter). Shared residency (D-10) performs NO
//	    model change — only the render delta — still capture→mutate→prove→rollback; the
//	    chosen residency is surfaced in the Result (never silently degrade swap→shared).
//	(3) CAPTURE strictly BEFORE any mutation: priorUnit bytes + priorCfg value snapshot
//	    (Pitfall 5). An uncapturable prior unit refuses without mutating.
//	(4) MUTATE: set cfg coding fields (enter) / restore prior chat model + CodingMode=false
//	    (exit) → SaveConfig → ReconcileAndWrite → Restart ONLY the inference service. ANY
//	    error here rolls back verbatim.
//	(5) PROVE: switch ONLY on ProveStatusPass; any other verdict (incl. ready+health-200-
//	    but-residency-FAIL, D-09) rolls back verbatim — idle-green is never green.
//
// Exit symmetry (D-08): the exit path runs the IDENTICAL (3)→(5) frame to restore the
// chat model — not a bare coding_mode=false write.
func Run(d Deps, dir Direction) Result {
	// (1) Load the source of truth; a same-state target is a clean no-op with zero side
	// effects (D-06). enter-while-already-coding and exit-while-already-chat both NoOp.
	cfg, err := d.LoadConfig()
	if err != nil {
		return Result{Refused: true, FailedStep: "load config", Err: err, Direction: dir}
	}
	wantCoding := dir == Enter
	if cfg.CodingMode == wantCoding {
		return Result{NoOp: true, Direction: dir, FromModel: cfg.Model}
	}

	from := cfg.Model

	// (2) Resolve the target. Compose modelswap's forward ordering for the swap-residency
	// model change; on exit the target is the captured prior chat model in cfg.Model.
	var target CoderTarget
	if dir == Enter {
		// Fit-guard FIRST (Pitfall 4): resolve the coder entry + residency and re-check
		// fit AT AgentCtx BEFORE any capture/mutate. A non-fit refuses-with-remediation,
		// zero side effects.
		t, ok, reason := d.ResolveCoder(cfg)
		if !ok {
			return Result{Refused: true, Reason: reason, Direction: dir, FromModel: from}
		}
		target = t

		// On swap residency auto-pull the coder weights if absent (modelswap step 3).
		// Shared residency performs NO model change, so nothing to pull (D-10).
		if target.Residency == ResidencySwap && !target.Downloaded {
			if err := d.Pull(target); err != nil {
				return Result{Err: err, FailedStep: "pull", Direction: dir, FromModel: from, ToModel: target.Model, Residency: target.Residency}
			}
		}
	}

	// (3) CAPTURE strictly BEFORE any mutation (Pitfall 5): the verbatim prior unit bytes
	// and a value snapshot of the prior config. An uncapturable prior unit must not be
	// mutated — refuse with zero side effects.
	priorUnit, err := d.CaptureUnit()
	if err != nil {
		return Result{Refused: true, FailedStep: "capture", Err: err, Direction: dir, FromModel: from, ToModel: targetModelID(dir, target, from), Residency: target.Residency}
	}
	priorCfg := cfg // VillaConfig is a flat value type (no pointers) → safe deep snapshot.

	// rollback restores the verbatim captured prior unit+config and re-readies the
	// inference service, best-effort: it accumulates errors across all four steps rather
	// than aborting on the first, and reports whether EVERY step succeeded. Per Pitfall 5
	// an incomplete rollback must be flagged honestly — never claim a clean no-op when a
	// restore step errored. Cloned VERBATIM from backendswap.
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
	// message when the restore did not fully succeed (Pitfall 5). Cloned VERBATIM.
	rolledBack := func(failedStep, reason string, origErr error, v ProveVerdict) Result {
		ok, rbDetail := rollback()
		r := Result{
			RolledBack: true,
			FailedStep: failedStep,
			Reason:     reason,
			Err:        origErr,
			Prove:      v,
			Direction:  dir,
			FromModel:  from,
			ToModel:    targetModelID(dir, target, from),
			Residency:  target.Residency,
		}
		if !ok {
			// Do NOT present a half-restored stack as a clean no-op: flag it.
			r.Reason = "rolled back, but the restore did not fully complete (" + rbDetail +
				") — run `villa status` and inspect the villa-llama unit"
		}
		return r
	}

	// (4) MUTATE. ANY error here rolls back verbatim to the captured prior unit+config.
	if dir == Enter {
		cfg.CodingMode = true
		// Persist the resolved coder fields AT ENTER (D-04). CRITICAL (D-08): cfg.Model is
		// LEFT as the durable chat model and is NEVER overwritten — the served coder model
		// lives in cfg.CoderModel, and the live ReconcileAndWrite closure resolves the
		// rendered -m from CoderModel WHEN cfg.CodingMode is true (D-05). This is what makes
		// exit a config-derived restore (clear the coder fields → the unit reverts to the
		// untouched chat model, byte-identical v1.3) rather than needing a separately stored
		// prior-chat-model field. On shared residency (D-10) there is NO model change: record
		// only the agent-ctx render delta; CoderModel stays empty so the chat endpoint serves.
		cfg.CoderAgentCtx = target.AgentCtx
		if target.Residency == ResidencySwap {
			cfg.CoderModel = target.Model
			cfg.CoderQuant = target.Quant
		}
	} else {
		// EXIT (D-08): clear coding mode + the coder fields under the SAME transactional
		// frame as enter (capture→mutate→prove→rollback) — not a bare flip. Because cfg.Model
		// was never overwritten at enter, clearing the coder fields here reverts the rendered
		// unit to the durable chat model (byte-identical v1.3, D-02) — a config-derived
		// restore of the prior chat model, exactly D-08.
		cfg.CodingMode = false
		cfg.CoderModel = ""
		cfg.CoderQuant = ""
		cfg.CoderAgentCtx = 0
	}

	if err := d.SaveConfig(cfg); err != nil {
		return rolledBack("save", "", err, ProveVerdict{})
	}
	if _, err := d.ReconcileAndWrite(cfg); err != nil {
		return rolledBack("write", "", err, ProveVerdict{})
	}
	if err := d.Restart(d.InstallServiceName); err != nil {
		return rolledBack("restart", "", err, ProveVerdict{})
	}

	// (5) PROVE the cutover against the already-running server UNDER LOAD. Switch ONLY on
	// ProveStatusPass; ANY other verdict (including ready+health-200-but-residency-FAIL,
	// D-09) rolls back verbatim — is-active/200 alone is never success.
	v := d.Prove(context.Background(), dir)
	if v.Status != ProveStatusPass {
		return rolledBack("prove", v.Detail, nil, v)
	}
	return Result{
		Switched:  true,
		Prove:     v,
		Direction: dir,
		FromModel: from,
		ToModel:   targetModelID(dir, target, from),
		Residency: target.Residency,
	}
}

// targetModelID returns the model id the cutover targets for Result surfacing: the
// resolved coder model on a swap-residency enter, otherwise the prior chat model (shared
// residency enter — no model change — and exit both keep the chat model).
func targetModelID(dir Direction, target CoderTarget, from string) string {
	if dir == Enter && target.Residency == ResidencySwap && target.Model != "" {
		return target.Model
	}
	return from
}
