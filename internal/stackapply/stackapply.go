// Package stackapply is the one home for turning a config into the unit files on
// disk (ADR-0013). Every verb that regenerates units hands it the TARGET config and
// gets back the units that changed; the verb still decides which services to start
// or restart from that list.
//
// Every render input is derived here, from the config alone: the served model's
// weight file, the coding-mode descriptor and agent ctx when coding mode is on, the
// resident slots, the models dir and the host villa path. Before this package each
// verb assembled its own orchestrate.RenderInput, and only coding-mode and install
// remembered the coding descriptor, so every other verb re-rendered villa-llama out
// of coding mode (#249).
//
// Apply heals the inference secret (GHSA-qxg9, ADR-0011) BEFORE it renders, because
// a resident set bakes the secret into the chat UI's unit: rendering first and
// healing after wrote that unit with an empty key. It is the only heal route for
// every verb but install, whose transaction persists its own freshly generated
// secret (ADR-0003).
//
// Transact (transact.go) is the swap transaction frame built on Apply and Restore
// (ADR-0015): every swap verb's change reaches the running stack through it.
//
// The package does no host I/O of its own. The catalog, the pinned render, the unit
// dir, the unit writer, systemd and the secret writers arrive through Deps, which
// cmd/villa wires once (liveStackDeps).
package stackapply

import (
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// Deps is the host a stack is applied to. cmd/villa wires one live set; tests wire
// fakes. Every field is required.
type Deps struct {
	// Catalog is the model catalog every render input's id is resolved against.
	Catalog       func() (catalog.Catalog, error)
	ModelsDir     func() string
	HostVillaPath func() string
	// Render is the pinned render: it applies this host's effective pins and
	// resolves speculation, the vision projector and the tools-mode ctx floor
	// before calling orchestrate.Render.
	Render    func(orchestrate.RenderInput) ([]orchestrate.Unit, error)
	UnitDir   func() (string, error)
	Reconcile func([]orchestrate.Unit, string) (orchestrate.Plan, error)
	// WriteUnits writes the changed units; DaemonReload re-reads them.
	WriteUnits   func(orchestrate.Plan, string) error
	DaemonReload func() error
	// SaveConfig and WriteInferenceSecretEnv back the inference-secret heal.
	SaveConfig              func(config.VillaConfig) error
	WriteInferenceSecretEnv func(name, text string) error
}

// ServedTarget returns the model id and ctx villa-llama serves for cfg: the coder at
// the agent ctx in swap-residency coding mode, the chat model at the agent ctx in
// shared residency (CoderModel empty), otherwise the chat model at the chat ctx.
func ServedTarget(cfg config.VillaConfig) (model string, ctx int) {
	if subsystem.CodingModeOn(cfg) {
		if cfg.CoderModel != "" {
			return cfg.CoderModel, cfg.CoderAgentCtx
		}
		return cfg.Model, cfg.CoderAgentCtx
	}
	return cfg.Model, cfg.Ctx
}

// Render renders the whole stack for cfg without touching disk. It is what a verb
// validates a service name against, and what a capture reads the prior bytes for.
func Render(d Deps, cfg config.VillaConfig) ([]orchestrate.Unit, error) {
	in, err := input(d, cfg)
	if err != nil {
		return nil, err
	}
	units, err := d.Render(in)
	if err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	return units, nil
}

// Plan renders cfg and reconciles it against the unit dir, writing nothing: the
// --dry-run preview, and the unit set a transaction captures before it mutates.
func Plan(d Deps, cfg config.VillaConfig) (orchestrate.Plan, error) {
	units, err := Render(d, cfg)
	if err != nil {
		return orchestrate.Plan{}, err
	}
	dir, err := d.UnitDir()
	if err != nil {
		return orchestrate.Plan{}, fmt.Errorf("resolve unit dir: %w", err)
	}
	plan, err := d.Reconcile(units, dir)
	if err != nil {
		return orchestrate.Plan{}, fmt.Errorf("reconcile: %w", err)
	}
	return plan, nil
}

// Apply makes the unit files match cfg: heal the inference secret, render,
// reconcile, write the changed units and daemon-reload. It returns the units it
// wrote, and still returns them when the reload after the write fails, so a caller's
// rollback knows what is on disk. Nothing changed means no unit written and no
// reload; the heal still rewrites the inference-secret env file on every apply.
//
// Its mutating callers are the swap transaction frame (Transact, through its live
// binding) and the verbs that hold the stack lock themselves;
// TestEveryStackMutationHoldsTheLock (cmd/villa) keeps it that way.
func Apply(d Deps, cfg config.VillaConfig) ([]orchestrate.Unit, error) {
	cfg, err := heal(d, cfg)
	if err != nil {
		return nil, fmt.Errorf("ensure inference secret: %w", err)
	}
	plan, err := Plan(d, cfg)
	if err != nil {
		return nil, err
	}
	if len(plan.Changed) == 0 {
		return nil, nil
	}
	dir, err := d.UnitDir()
	if err != nil {
		return nil, fmt.Errorf("resolve unit dir: %w", err)
	}
	if err := d.WriteUnits(plan, dir); err != nil {
		return nil, fmt.Errorf("write units: %w", err)
	}
	if err := d.DaemonReload(); err != nil {
		return plan.Changed, fmt.Errorf("daemon-reload: %w", err)
	}
	return plan.Changed, nil
}

// Restore writes captured unit bytes back verbatim, keyed by unit filename. A
// re-render is not a restore: it would reproduce today's template against today's
// config, not what was running. The caller reloads and restarts, as its rollback
// frame orders them.
func Restore(d Deps, units map[string]string) error {
	if len(units) == 0 {
		return nil
	}
	dir, err := d.UnitDir()
	if err != nil {
		return fmt.Errorf("resolve unit dir: %w", err)
	}
	changed := make([]orchestrate.Unit, 0, len(units))
	for name, text := range units {
		changed = append(changed, orchestrate.Unit{Name: name, Text: text})
	}
	return d.WriteUnits(orchestrate.Plan{Changed: changed}, dir)
}

// ResidentUnits resolves each configured resident slot's catalog id to its weight
// file. An unresolvable id is an error: a unit whose -m names a fabricated file fails
// only at container start, long after the verb reported success.
func ResidentUnits(cat catalog.Catalog, slots []config.ResidentModel) ([]orchestrate.ResidentUnit, error) {
	if len(slots) == 0 {
		return nil, nil
	}
	units := make([]orchestrate.ResidentUnit, 0, len(slots))
	for _, r := range slots {
		m, ok := cat.FindByID(r.Model)
		if !ok {
			return nil, fmt.Errorf("resident model %q is not in the catalog — cannot resolve its weight file", r.Model)
		}
		units = append(units, orchestrate.ResidentUnit{Model: r.Model, ModelFile: m.PrimaryFile(), Ctx: r.Ctx, Port: r.Port})
	}
	return units, nil
}

// input derives every render input from cfg. The catalog-to-inference translation
// of the coding descriptor happens here, never in the pure renderer.
func input(d Deps, cfg config.VillaConfig) (in orchestrate.RenderInput, err error) {
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return in, fmt.Errorf("resolve backend: %w", err)
	}
	cat, err := d.Catalog()
	if err != nil {
		return in, fmt.Errorf("load model catalog: %w", err)
	}
	served, _ := ServedTarget(cfg)
	m, ok := cat.FindByID(served)
	if !ok {
		return in, fmt.Errorf("resolve model file: model %q is not in the catalog — cannot resolve its weight file", served)
	}
	resident, err := ResidentUnits(cat, cfg.Resident)
	if err != nil {
		return in, fmt.Errorf("resolve resident models: %w", err)
	}
	in = orchestrate.RenderInput{
		Backend:       backend,
		Cfg:           cfg,
		ModelFile:     m.PrimaryFile(),
		ModelsDir:     d.ModelsDir(),
		HostVillaPath: d.HostVillaPath(),
		Resident:      resident,
	}
	if subsystem.CodingModeOn(cfg) {
		in.CodingMode = codingSpec(m)
		in.AgentCtx = cfg.CoderAgentCtx
	}
	return in, nil
}

// codingSpec is the served entry's coding-mode descriptor: its fail-closed
// cache-reuse qualification and its agent sampling preset, translated from the
// catalog's shape to the inference seam's.
func codingSpec(m catalog.Model) *inference.CodingModeSpec {
	spec := &inference.CodingModeSpec{CacheReuseSafe: m.CacheReuseSafe}
	if s := m.AgentSampling; s != nil {
		spec.Sampling = &inference.Sampling{
			Temperature:   s.Temperature,
			TopP:          s.TopP,
			TopK:          s.TopK,
			RepeatPenalty: s.RepeatPenalty,
		}
	}
	return spec
}

// heal makes cfg carry the inference bearer and (re)writes its 0600 env file. An
// existing secret is reused verbatim, never rotated; a missing one is generated and
// persisted with the target config. The env file is written on every apply, which
// also restores one deleted by hand.
func heal(d Deps, cfg config.VillaConfig) (config.VillaConfig, error) {
	if cfg.InferenceSecret == "" {
		secret, err := config.GenerateInferenceSecret()
		if err != nil {
			return cfg, fmt.Errorf("generate inference secret: %w", err)
		}
		cfg.InferenceSecret = secret
		if err := d.SaveConfig(cfg); err != nil {
			return cfg, fmt.Errorf("persist inference secret: %w", err)
		}
	}
	name, text := orchestrate.RenderInferenceSecretEnv(cfg.InferenceSecret)
	if err := d.WriteInferenceSecretEnv(name, text); err != nil {
		return cfg, fmt.Errorf("write inference secret env: %w", err)
	}
	return cfg, nil
}
