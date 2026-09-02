// Package install is the PURE decision core for `villa install`.
//
// Install was the only stack-mutating flow without one. Its logic was a 642-line
// function in the command tier behind an interface of 49 injected dependencies, by
// far the widest in the repo — the three swap cores each expose one function and
// around ten. Its test fake was 51 fields and its test file the largest in the tree,
// which meant install's decisions could not be questioned without standing up the
// whole world.
//
// This package answers the decisions. It returns typed values, prints nothing, and
// exits nothing: rendering output and mapping exit codes stay in the command tier,
// which is what makes the decisions testable without 51 fields.
//
// # What a decision is here
//
// Two things, in order. First, gate resolution: which optional subsystems this run
// should treat as on, folding the persisted config with the opt-in flags. Second,
// plan assembly: the config that will be persisted and rendered from, derived from
// the recommendation and those gates.
//
// The flow that composes them is here too (flow.go, ADR-0005): Run drives the whole
// install through injected Deps and returns a typed Result; the command tier wires
// the live host and maps the outcome to an exit code.
//
// PURE: no I/O, no os/exec, no container-image literal.
package install

import (
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// Opts are the run's opt-in flags: the parts of the command line that change what
// install decides, as distinct from what it prints.
type Opts struct {
	// CodingAgent opts into the coding-agent addon for this run and persists the
	// choice, so a later bare install gates on the now-persisted value.
	CodingAgent bool
	// WebSearch opts into the web-search addon, with the same persist-and-inherit
	// behaviour as CodingAgent.
	WebSearch bool
	// DryRun prints the rendered changed units and mutates NOTHING: no write, no
	// pull, no persist, no privileged host-prep, no wizard.
	DryRun bool
	// Force overrides an un-consented BLOCK-tier host-prep gap (auditable); the
	// run then degrades to WARN even on a clean bring-up.
	Force bool
	// JSON marks a non-interactive run: no consent prompt, no wizard.
	JSON bool
	// NoTUI skips the guided wizard for the flag-driven path.
	NoTUI bool
}

// Gates is which optional subsystems this run treats as on.
//
// It is resolved ONCE, before anything is rendered, because the gates are read by
// the preflight gate, the pre-stage step, the render and the proofs. Resolving them
// per step is how the same flag came to be read eleven times in one flow, with the
// risk that two steps disagree.
type Gates struct {
	// Memory, WebSearch and Agent are the resolved gates for this run.
	Memory    bool
	WebSearch bool
	Agent     bool
	// CodingMode is entered by the agent addon, and ONLY for a real swap-residency
	// coder fit. It is never set by a flag directly.
	CodingMode bool
}

// On reports the resolved gate for a subsystem, so a caller can ask by kind rather
// than reaching for a field.
func (g Gates) On(k subsystem.Kind) bool {
	switch k {
	case subsystem.Memory:
		return g.Memory
	case subsystem.WebSearch:
		return g.WebSearch
	case subsystem.Agent:
		return g.Agent
	case subsystem.CodingMode:
		return g.CodingMode
	case subsystem.Inference, subsystem.Chat:
		// Always on: an install renders both units unconditionally, so there is no
		// resolved gate to hold and nothing a flag could turn off. Answered here
		// rather than falling through to the default so a reader is not left
		// wondering whether the case was forgotten.
		return true
	}
	return false
}

// ResolveGates folds the persisted config with this run's opt-in flags.
//
// A flag turns a subsystem ON for this run and is persisted, so the choice carries
// to later runs. A flag can never turn one OFF: disabling is an explicit config
// edit, so a bare install after an opt-in never silently tears a subsystem down.
//
// The coder fit decides coding mode, not the flag. The agent addon serves the coder
// it stages, so entering coding mode without a real swap-residency fit would render
// a unit serving an empty model. A shared-residency fit leaves coding mode off and
// is refused later by the coder-fit gate.
func ResolveGates(cfg config.VillaConfig, opts Opts, rec recommend.Recommendation) Gates {
	g := Gates{
		Memory:    subsystem.MemoryOn(cfg),
		WebSearch: subsystem.WebSearchOn(cfg) || opts.WebSearch,
		Agent:     subsystem.AgentOn(cfg) || opts.CodingAgent,
	}
	// Coding mode is entered by the addon opt-in, and only with a coder to serve.
	if opts.CodingAgent && rec.Coder.Model != "" {
		g.CodingMode = true
	}
	return g
}

// GateAgentChecks reports whether the coding-agent preflight gates should run.
//
// It is the resolved agent gate, and exists as a named decision because getting it
// wrong is a real failure: gating on the PERSISTED flag alone would let a first
// `villa install --coding-agent` stage the agent without ever checking that the host
// has the disk and the memory envelope for it.
func GateAgentChecks(g Gates) bool { return g.Agent }

// Plan is the assembled install decision: the config to persist and render from.
type Plan struct {
	// Config is the config this install will write and render from. It is the
	// persisted config with the recommendation-derived fields and the resolved gates
	// applied, never a fresh default.
	Config config.VillaConfig
	// Gates is the resolved gate set, carried alongside so the caller reads the
	// decision rather than re-deriving it from Config.
	Gates Gates
	// BackendPreserved reports that a deliberately-chosen backend in the persisted
	// config was kept instead of taking the recommendation. Surfaced so the caller
	// can explain why a re-install did not move to the default backend.
	BackendPreserved bool
}

// AssemblePlan derives the config this install will persist and render from. It
// takes the gates the caller already resolved rather than resolving its own, so a
// run cannot hold two answers.
//
// It SEEDS from the persisted config rather than from defaults, so a user's
// customised memory, dashboard and chat fields survive every install instead of
// being reset. Only the recommendation-derived fields and the resolved gates are
// overridden.
//
// The backend assignment is guarded. The recommender always returns the default
// backend and carries no backend override, so assigning it unconditionally would
// silently revert a deliberately-chosen backend on every re-install, re-rendering
// the inference unit to the default image. A valid persisted choice is PRESERVED;
// an empty or unrecognised value falls through to the recommendation. The comparison
// is on backend NAMES only, never an image literal.
func AssemblePlan(cfg config.VillaConfig, gates Gates, rec recommend.Recommendation, backendChosen func(string) bool) Plan {

	plan := Plan{Config: cfg, Gates: gates}
	plan.Config.Model = rec.Model
	plan.Config.Quant = rec.Quant
	plan.Config.Ctx = rec.ContextLen

	if backendChosen != nil && backendChosen(cfg.Backend) {
		plan.BackendPreserved = true
	} else {
		plan.Config.Backend = rec.Backend
	}

	plan.Config.MemoryEnabled = gates.Memory
	plan.Config.WebSearchEnabled = gates.WebSearch
	plan.Config.AgentEnabled = gates.Agent

	// Coding mode carries the resolved coder identity: the unit, the agent config and
	// the readiness proof must all agree on which model is served, so they come from
	// the same recommendation the disk and envelope gates were computed from.
	if gates.CodingMode {
		plan.Config.CoderModel = rec.Coder.Model
		plan.Config.CoderQuant = rec.Coder.Quant
		plan.Config.CoderAgentCtx = rec.Coder.AgentCtx
		plan.Config.CodingMode = true
	}

	return plan
}

// Fit is the resource requirement the preflight gate is run against.
type Fit struct {
	MinDiskBytes uint64
	MinMemBytes  uint64
}

// ResourceFit derives the concrete resource requirement from the recommendation.
//
// The embedding reservation is included in the memory floor rather than checked
// separately, so the gate reflects what will actually be resident. It is zero when
// memory is off, which leaves the memory-off gate unchanged.
func ResourceFit(rec recommend.Recommendation) Fit {
	return Fit{
		MinDiskBytes: rec.WeightBytes,
		MinMemBytes:  rec.WeightBytes + rec.KVCacheBytes + rec.HeadroomBytes + rec.EmbeddingReservationBytes,
	}
}

// RecommendationUsable reports whether a recommendation can actually be installed.
//
// An empty model, a non-positive context or a zero weight means no catalog model fit
// the envelope. Starting an inference server with no fit, or with a zero context, is
// never valid, so this is a refusal rather than a warning.
func RecommendationUsable(rec recommend.Recommendation) bool {
	return rec.Model != "" && rec.ContextLen > 0 && rec.WeightBytes != 0
}
