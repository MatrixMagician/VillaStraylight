// Package stackapply is the one home for turning a config into the unit files on
// disk (ADR-0013). Every verb that regenerates units hands it the TARGET config and
// gets back the units that changed; the verb still decides which services to start
// or restart from that list. A registry unit the target no longer renders is stopped
// and removed in the same apply (ADR-0035), so a gated-off subsystem leaves nothing
// running outside the fit.
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
// secret (ADR-0003). Right after it, Apply heals crush.json when its only drift is
// that secret (HealAgentConfig, ADR-0019), so up, restart and every swap repair a
// host whose crush.json predates the key.
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
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// Deps is the host a stack is applied to. cmd/villa wires one live set; tests wire
// fakes. Every field is required but HealAgentConfig.
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
	// WriteUnits writes the changed units, RemoveUnits removes the removed ones, and
	// DaemonReload re-reads them.
	WriteUnits   func(orchestrate.Plan, string) error
	RemoveUnits  func(orchestrate.Plan, string) error
	DaemonReload func() error
	// IsActive and Stop take down a removed unit's service before its file goes.
	IsActive func(service string) (string, error)
	Stop     func(service string) error
	// SaveConfig and WriteInferenceSecretEnv back the inference-secret heal.
	SaveConfig              func(config.VillaConfig) error
	WriteInferenceSecretEnv func(name, text string) error
	// HealAgentConfig rewrites crush.json when its only drift is villa's inference
	// key (ADR-0019), handed the config after the secret heal. The live wiring is a
	// no-op when the coding agent is off or crush.json is absent. nil skips the heal.
	HealAgentConfig func(config.VillaConfig) error
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

// Applied is what one Apply did to the host.
type Applied struct {
	// Changed are the units written.
	Changed []orchestrate.Unit
	// Removed are the registry units removed, each carrying the bytes it had on disk.
	Removed []orchestrate.Unit
	// Stopped are the removed units' services that were running and were stopped:
	// what a rollback starts again, and nothing the operator had stopped.
	Stopped []string
}

// Empty reports whether the apply left every unit file as it found it.
func (a Applied) Empty() bool { return len(a.Changed) == 0 && len(a.Removed) == 0 }

// Apply makes the unit files match cfg: heal the inference secret, heal crush.json's
// copy of it (ADR-0019), render, reconcile, stop the running services of the units
// cfg no longer renders, write the changed units, remove the removed ones and
// daemon-reload once (ADR-0035). It reports what it did even when a later step
// fails, so a caller's rollback knows what is on disk and what it stopped. Nothing
// changed or removed means no unit touched and no reload; the heal still rewrites the
// inference-secret env file on every apply.
//
// Its mutating callers are the swap transaction frame (Transact, through its live
// binding) and the verbs that hold the stack lock themselves;
// TestEveryStackMutationHoldsTheLock (cmd/villa) keeps it that way.
func Apply(d Deps, cfg config.VillaConfig) (Applied, error) {
	cfg, err := heal(d, cfg)
	if err != nil {
		return Applied{}, fmt.Errorf("ensure inference secret: %w", err)
	}
	if err := healAgentConfig(d, cfg); err != nil {
		return Applied{}, err
	}
	plan, err := Plan(d, cfg)
	if err != nil {
		return Applied{}, err
	}
	return write(d, plan)
}

// healAgentConfig runs the HealAgentConfig seam when it is wired. A failure stops
// the apply before any unit is written.
func healAgentConfig(d Deps, cfg config.VillaConfig) error {
	if d.HealAgentConfig == nil {
		return nil
	}
	if err := d.HealAgentConfig(cfg); err != nil {
		return fmt.Errorf("heal crush.json: %w", err)
	}
	return nil
}

// write stops the removed units' running services, writes the changed units, removes
// the removed ones and reloads systemd; a plan with neither is a no-op. A service is
// stopped while its generated unit is still loaded, before its file goes. A Quadlet
// unit cannot be disabled (is-enabled reports generated), so removing the file and
// reloading is its disable.
func write(d Deps, plan orchestrate.Plan) (Applied, error) {
	var applied Applied
	if len(plan.Changed) == 0 && len(plan.Removed) == 0 {
		return applied, nil
	}
	dir, err := d.UnitDir()
	if err != nil {
		return applied, fmt.Errorf("resolve unit dir: %w", err)
	}
	toStop, err := runningRemoved(d, plan.Removed)
	if err != nil {
		return applied, err
	}
	for _, svc := range toStop {
		// Recorded before the stop, so a stop that half-succeeded is started again.
		applied.Stopped = append(applied.Stopped, svc)
		if err := d.Stop(svc); err != nil {
			return applied, fmt.Errorf("stop %s: %w", svc, err)
		}
	}
	if len(plan.Changed) > 0 {
		applied.Changed = plan.Changed
		if err := d.WriteUnits(plan, dir); err != nil {
			return applied, fmt.Errorf("write units: %w", err)
		}
	}
	if len(plan.Removed) > 0 {
		applied.Removed = plan.Removed
		if err := d.RemoveUnits(plan, dir); err != nil {
			return applied, fmt.Errorf("remove units: %w", err)
		}
	}
	if err := d.DaemonReload(); err != nil {
		return applied, fmt.Errorf("daemon-reload: %w", err)
	}
	return applied, nil
}

// runningRemoved returns the removed units' services that are up. It reads every
// state before anything is stopped, written or removed, and refuses when one cannot
// be read: unlike the restart gate, which treats an unreadable state as not running
// so it never starts what the operator stopped, a removal that guessed "stopped"
// would delete the unit file under a container that keeps running, where no
// orphan-units finding keyed on the file can name it (#344).
func runningRemoved(d Deps, removed []orchestrate.Unit) ([]string, error) {
	var up []string
	for _, u := range removed {
		svc, ok := service(u.Name)
		if !ok {
			continue
		}
		state, err := d.IsActive(svc)
		if err != nil {
			return nil, fmt.Errorf("refusing to remove %s: cannot read whether %s is running (%w); "+
				"check `systemctl --user is-active %s`, stop it by hand if it is up, then re-run `villa up`",
				u.Name, svc, err, svc)
		}
		if isUp(state) {
			up = append(up, svc)
		}
	}
	return up, nil
}

// service maps a Quadlet .container unit to its service; other units are not services.
func service(unit string) (string, bool) {
	name, ok := strings.CutSuffix(unit, ".container")
	return name + ".service", ok
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
		units = append(units, orchestrate.ResidentUnit{Model: r.Model, ModelFile: m.PrimaryFile(), Ctx: r.Ctx, Port: r.Port, SlidingWindow: m.SWA != nil})
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
		SlidingWindow: m.SWA != nil,
	}
	if subsystem.CodingModeOn(cfg) {
		in.CodingMode = codingSpec(m)
		in.AgentCtx = cfg.CoderAgentCtx
	}
	if in.Image, err = ImageServe(cfg); err != nil {
		return in, err
	}
	return in, nil
}

// ImageServe is the image-table-to-renderer translation (#312): the entry cfg
// names, as the ImageServe villa-image is rendered with, or nil when image
// generation is off. It is exported for the status read-model, which assembles
// its own render input and must hand the renderer the same answer every other
// verb renders. An unknown id is an error: a unit whose weight paths name
// fabricated files fails only at container start.
func ImageServe(cfg config.VillaConfig) (*orchestrate.ImageServe, error) {
	if !subsystem.ImageOn(cfg) {
		return nil, nil
	}
	m, ok := catalog.Image(cfg.ImageModel)
	if !ok {
		return nil, fmt.Errorf("resolve image model: image model %q is not in the image table (default %q)", cfg.ImageModel, config.DefaultVillaConfig().ImageModel)
	}
	return &orchestrate.ImageServe{
		DiffusionFile: m.Diffusion.Filename, TextEncoderFile: m.TextEncoder.Filename, VAEFile: m.VAE.Filename,
		Steps: m.Steps, CfgScale: m.CfgScale, Width: m.Width, Height: m.Height,
	}, nil
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
