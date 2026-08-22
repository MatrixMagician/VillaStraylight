package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/codingmode"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/residency"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// coding-mode.go is the cmd-tier `villa coding-mode` noun: the live host wiring that
// drives the pure internal/codingmode transactional core (Plan 25-02). It holds the
// coding-mode liveProve twin (the cutover gate, ConfigContext = the resolved agent ctx),
// the cobra surface (enter/exit), the Result→exit mapping, and liveCodingModeDeps.
//
// CRITICAL — backend-marker discipline: this file must stay LITERAL-FREE of
// backend marker tokens (the per-backend residency device token, the HSA override env
// var, the GPU-fault abort string, and any image/device literal) AND of coding-flag
// literals (--jinja / --cache-reuse / sampling). Backend markers arrive ONLY through
// inference.BackendFor(cfg.Backend).ResidencyProof(); the coding-flag literals live ONLY
// in internal/inference (backend_*.go) behind the seam. The Plan-01-extended
// TestSeamGrepGate WALKS cmd/villa and fails CI on any such leak. Do NOT paste markers
// or coding flags here.
//
// Decisions realized: (explicit verb shape coding-mode enter|exit; NOT `villa code`),
// (exit symmetric to enter), (under-load prove; idle-green is never green),
// (swap vs shared surfaced, never silently degraded).

// liveCodingProve is the injected cutover gate (codingmode.Deps.Prove). It resolves
// what coding mode is proving and hands it to the shared residency drive protocol,
// which owns the three gates and the Unknown-versus-negative rule.
//
// Coding mode differs from the chat-side proof in exactly two Target fields
// (Pitfall 4): the SERVED model is cfg.CoderModel when coding mode is on with swap
// residency, and the served context is the resolved AgentCtx (the rendered single
// -c), NOT cfg.Ctx. Proving the chat model at the chat ctx would compare the
// residency fit-math against the wrong KV. That difference was previously expressed
// by a 71-line copy of the protocol; it is now two field values.
//
// Backend markers arrive ONLY through BackendFor(cfg.Backend).ResidencyProof(), so
// this file stays literal-free of them.
func liveCodingProve(ctx context.Context, _ codingmode.Direction) prove.Verdict {
	cfg, err := config.LoadVilla()
	if err != nil {
		return prove.Verdict{Status: prove.StatusFail, Detail: "load config: " + err.Error()}
	}

	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return prove.Verdict{Status: prove.StatusFail, Detail: err.Error()}
	}

	servedModel, servedCtx := codingServedTarget(cfg)
	modelFile, err := codingModelFile(cfg, servedModel)
	if err != nil {
		return prove.Verdict{Status: prove.StatusFail, Detail: "resolve model file: " + err.Error()}
	}

	return residency.ProveCutover(ctx, liveResidencyDeps(), residency.Target{
		Endpoint:    inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint(),
		Service:     installServiceName,
		ModelID:     servedModel,
		ModelFile:   modelFile,
		ContextLen:  servedCtx,
		WeightBytes: codingWeightBytes(cfg, servedModel),
		Markers:     backend.ResidencyProof(),
	})
}

// codingServedTarget returns the model id + ctx the running unit serves: the coder model
// at the resolved agent ctx when coding mode is on (swap), otherwise the chat model
// at the chat ctx. On shared residency (CoderModel empty) the chat model is served with
// the agent-ctx render delta, so the served model is the chat model but the served ctx is
// the resolved agent ctx.
func codingServedTarget(cfg config.VillaConfig) (model string, ctx int) {
	if subsystem.CodingModeOn(cfg) {
		ctx = cfg.CoderAgentCtx
		if cfg.CoderModel != "" {
			return cfg.CoderModel, ctx // swap residency: coder served
		}
		return cfg.Model, ctx // shared residency: chat model + agent-ctx delta
	}
	return cfg.Model, cfg.Ctx
}

// codingModelFile resolves the on-disk GGUF filename for the SERVED model through the
// catalog (never as a path). It mirrors liveModelFile but keys on the supplied served
// model id (which may be the coder model) rather than cfg.Model.
func codingModelFile(_ config.VillaConfig, servedModel string) (string, error) {
	cat, _, err := catalog.Load(modelCatalogPath)
	if err != nil {
		return "", fmt.Errorf("load model catalog: %w", err)
	}
	m, ok := cat.FindByID(servedModel)
	if !ok {
		return "", fmt.Errorf("model %q is not in the catalog — cannot resolve its weight file", servedModel)
	}
	return primaryModelFile(m), nil
}

// codingWeightBytes returns the served model's weight bytes for the residency proof,
// keyed on the served (possibly coder) model id — mirrors liveWeightBytes' override-pick
// but for the coding-served model.
func codingWeightBytes(_ config.VillaConfig, servedModel string) uint64 {
	cat, _, err := catalog.Load(modelCatalogPath)
	if err != nil {
		return 0
	}
	rec := recommend.Pick(detect.Probe(), cat, recommend.Overrides{Model: servedModel}, recommend.MemoryInputs{}, recommend.WebSearchInputs{})
	return rec.WeightBytes
}

// ---------------------------------------------------------------------------
// coding-mode noun: `villa coding-mode enter` / `villa coding-mode exit`.
// Two explicit subcommands — NOT `villa code` (reserved for the Phase-26 agent launcher).
// Cloned from the backend.go set noun: RunE returns the mapped exit code (body RETURNS
// the int so tests assert output+code without a subprocess), and the Result→exit mapping
// mirrors runBackendSet.
// ---------------------------------------------------------------------------

// newCodingMode builds the `villa coding-mode` noun and its enter/exit subcommands.
func newCodingMode() *cobra.Command {
	cm := &cobra.Command{
		Use:   "coding-mode",
		Short: "Enter or exit the tool-calling coding mode (transactional cutover)",
		Long: "Flip the running stack into a tool-calling-ready coding mode (or back) with a transactional " +
			"cutover: capture the prior unit + config verbatim, swap the chat model for the fit-guarded coder " +
			"model (swap residency) or apply the tool-calling render delta to the chat endpoint (shared " +
			"residency), restart ONLY the villa-llama unit, and PROVE the cutover (real generation probe + " +
			"GPU-residency proof UNDER LOAD within a bounded timeout). Any mutate error or a non-pass proof " +
			"rolls back verbatim — a failed cutover is a no-op to the running stack. The mode changes ONLY via " +
			"this explicit verb; nothing auto-flips it.",
		Args: cobra.NoArgs,
	}
	cm.AddCommand(newCodingModeEnter(), newCodingModeExit())
	return cm
}

// newCodingModeEnter builds `villa coding-mode enter`.
func newCodingModeEnter() *cobra.Command {
	return &cobra.Command{
		Use:   "enter",
		Short: "Enter coding mode transactionally (capture → cutover → prove → rollback)",
		Long: "Enter the tool-calling coding mode on the running install: resolve the fit-guarded coder model " +
			"at its agent context (refuse-with-remediation if it does not fit), auto-pull the weights if absent " +
			"(swap residency), capture the prior unit verbatim, persist config + regenerate ONLY the villa-llama " +
			"unit + restart it, and PROVE the cutover (real generation probe + GPU-residency proof under load). " +
			"Any mutate error or a non-pass proof rolls back verbatim. Exits 0 on enter/no-op, 1 on " +
			"refusal/error/rollback. Already in coding mode is a clean no-op.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code := runCodingMode(cmd, codingmode.Enter, liveCodingModeDeps())
			os.Exit(code)
			return nil
		},
	}
}

// newCodingModeExit builds `villa coding-mode exit`.
func newCodingModeExit() *cobra.Command {
	return &cobra.Command{
		Use:   "exit",
		Short: "Exit coding mode transactionally (symmetric to enter)",
		Long: "Exit the tool-calling coding mode on the running install: restore the chat model under the SAME " +
			"transactional discipline as enter (capture → mutate → prove → rollback) — not a bare flip. Exits 0 " +
			"on exit/no-op, 1 on error/rollback. Already in chat mode is a clean no-op.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code := runCodingMode(cmd, codingmode.Exit, liveCodingModeDeps())
			os.Exit(code)
			return nil
		},
	}
}

// runCodingMode delegates to codingmode.Run and maps the typed Result to exit codes +
// messages (clone of runBackendSet). It surfaces the residency mode (swap vs shared) on a
// successful enter so a shared cutover is never silently presented as a swap, and
// the honest rollback-incomplete Reason verbatim on a rollback (Pitfall 5). The body
// returns the int (no os.Exit) so tests assert output+code.
func runCodingMode(cmd *cobra.Command, dir codingmode.Direction, d *codingmode.Deps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	res := codingmode.Run(*d, dir)
	switch {
	case res.Refused:
		if res.Reason != "" {
			fmt.Fprintf(errOut, "coding-mode %s: refusing — %s\n", dir, res.Reason)
		} else if res.Err != nil {
			fmt.Fprintf(errOut, "coding-mode %s: refusing — %s failed: %v\n", dir, res.FailedStep, res.Err)
		} else {
			fmt.Fprintf(errOut, "coding-mode %s: refusing\n", dir)
		}
		return exitBlocked
	case res.RolledBack:
		// A mutate error or a non-pass prove verdict rolled back verbatim. Reason already
		// folds in an honest rollback-incomplete message when the restore did not fully
		// complete (Pitfall 5).
		fmt.Fprintf(errOut, "coding-mode %s: cutover failed at %q — rolled back; prior state restored\n",
			dir, res.FailedStep)
		if res.Reason != "" {
			fmt.Fprintf(errOut, "  detail: %s\n", res.Reason)
		}
		if res.Err != nil {
			fmt.Fprintf(errOut, "  error:  %v\n", res.Err)
		}
		return exitBlocked
	case res.Err != nil:
		// A non-rollback failure path (e.g. pull). Run rolls back mutate errors; a pull
		// error happens BEFORE any mutation, so there is nothing to roll back.
		fmt.Fprintf(errOut, "coding-mode %s: failed at %q: %v\n", dir, res.FailedStep, res.Err)
		return exitBlocked
	case res.NoOp:
		if dir == codingmode.Enter {
			fmt.Fprintf(out, "already in coding mode — no change\n")
		} else {
			fmt.Fprintf(out, "already in chat mode — no change\n")
		}
		return exitPass
	default: // Switched
		if dir == codingmode.Enter {
			// Surface the residency mode so shared is never silent.
			switch res.Residency {
			case codingmode.ResidencyShared:
				fmt.Fprintf(out, "entered coding mode (shared residency: tool-calling render delta applied to the chat model %q — no model swap) — %s restarted, cutover proven under load\n",
					res.FromModel, installServiceName)
			default: // swap
				fmt.Fprintf(out, "entered coding mode (swap residency: chat model %q -> coder model %q) — %s restarted, cutover proven under load\n",
					res.FromModel, res.ToModel, installServiceName)
			}
		} else {
			fmt.Fprintf(out, "exited coding mode — chat model %q restored, %s restarted, cutover proven under load\n",
				res.ToModel, installServiceName)
		}
		return exitPass
	}
}

// liveCodingModeDeps wires the transactional core to the real host: config load/save, the
// composed modelswap resolve→fit-guard→pull for the coder model, verbatim unit
// capture/restore through the traversal-guarded orchestrate seams, the render/reconcile/
// write closure (the ONE delta from backend.go — it resolves the coder catalog entry and
// translates catalog.AgentSampling → inference.Sampling into RenderInput when coding), the
// systemd reload/restart seam, and liveCodingProve as the cutover gate. Every host-touching
// action is a seam so coding-mode_test.go drives the flow without a live host.
func liveCodingModeDeps() *codingmode.Deps {
	sys := orchestrate.NewSystemd()
	return &codingmode.Deps{
		InstallServiceName: installServiceName,
		LoadConfig:         config.LoadVilla,
		SaveConfig:         config.SaveVilla,
		// ResolveCoder: compose recommend.Pick(...).Coder (the agent-ctx fit-math + residency
		// verdict) — fit-guard FIRST (Pitfall 4). A non-fitting coder at agent ctx
		// is a refuse-with-remediation; the residency verdict (swap/shared) is a PURE fit-math
		// output, never a preference.
		ResolveCoder: func(cfg config.VillaConfig) (codingmode.CoderTarget, bool, string) {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return codingmode.CoderTarget{}, false, "catalog load failed"
			}
			// Persisted memory inputs (fail-soft): the coder fit must see the same shrunken
			// envelope the user was recommended (ordering).
			rec := recommend.Pick(detect.Probe(), cat, recommend.Overrides{}, liveLoadedMemoryInputs(), liveLoadedWebSearchInputs())
			coder := rec.Coder
			if coder.Residency == codingmode.ResidencyShared {
				// Shared residency: no coder fits standalone — apply render-delta-only on
				// the chat endpoint. Still a valid enter target (NOT a refusal); surfaced as shared.
				return codingmode.CoderTarget{
					AgentCtx:  coder.AgentCtx,
					Residency: codingmode.ResidencyShared,
				}, true, ""
			}
			if !coder.Fits || coder.Model == "" {
				// Defensive: a swap verdict must carry a fitting model. Treat an unfit/empty
				// swap as a refuse-with-remediation (never a silent OOM at container start).
				return codingmode.CoderTarget{}, false,
					fmt.Sprintf("no coder model fits the agent-ctx envelope (needs %d bytes vs %d usable)",
						coder.TotalBytes, rec.UsableEnvelopeBytes)
			}
			m, ok := cat.FindByID(coder.Model)
			downloaded := false
			if ok {
				_, statErr := os.Stat(filepath.Join(modelsDir(), primaryModelFile(m)))
				downloaded = statErr == nil
			}
			return codingmode.CoderTarget{
				Model:      coder.Model,
				Quant:      coder.Quant,
				AgentCtx:   coder.AgentCtx,
				Residency:  codingmode.ResidencySwap,
				Downloaded: downloaded,
			}, true, ""
		},
		// Pull: auto-download the verified coder weights (reuse the same downloader as
		// `model pull` / `model swap` via the pullFn seam — compose, don't fork).
		Pull: func(t codingmode.CoderTarget) error {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return err
			}
			m, ok := cat.FindByID(t.Model)
			if !ok {
				return fmt.Errorf("coder model %q is not in the catalog", t.Model)
			}
			dir := modelsDir()
			if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
				return mkErr
			}
			return pullFn(context.Background(), m, dir)
		},
		// CaptureUnit: read the verbatim prior villa-llama.container bytes from the quadlet
		// unit dir (traversal-bounded by construction).
		CaptureUnit: func() ([]byte, error) {
			dir, err := quadletUnitDir()
			if err != nil {
				return nil, err
			}
			return os.ReadFile(filepath.Join(dir, "villa-llama.container"))
		},
		// ReconcileAndWrite: the ONE delta from backend.go. When cfg.CodingMode is true,
		// resolve the coder catalog entry by cfg.CoderModel (swap) or serve the chat model
		// (shared), translate catalog.AgentSampling → inference.Sampling, and populate
		// RenderInput.CodingMode + CoderAgentCtx (the catalog import lives HERE, never in the
		// pure renderer). When off, leave them nil ⇒ off-path render is byte-identical.
		ReconcileAndWrite: func(c config.VillaConfig) (bool, error) {
			dir, err := quadletUnitDir()
			if err != nil {
				return false, err
			}
			servedModel, _ := codingServedTarget(c)
			modelFile, err := codingModelFile(c, servedModel)
			if err != nil {
				return false, err
			}
			backend, err := inference.BackendFor(c.Backend)
			if err != nil {
				return false, err
			}
			in := orchestrate.RenderInput{
				Backend:       backend,
				Cfg:           c,
				ModelFile:     modelFile,
				ModelsDir:     modelsDir(),
				HostVillaPath: hostVillaPath(),
			}
			if subsystem.CodingModeOn(c) {
				spec, derr := codingDescriptor(c, servedModel)
				if derr != nil {
					return false, derr
				}
				in.CodingMode = spec
				in.CoderAgentCtx = c.CoderAgentCtx
			}
			units, err := orchestrate.Render(in)
			if err != nil {
				return false, err
			}
			plan, err := orchestrate.Reconcile(units, dir)
			if err != nil {
				return false, err
			}
			if len(plan.Changed) == 0 {
				return false, nil
			}
			if err := orchestrate.WriteUnits(plan, dir); err != nil {
				return false, err
			}
			if err := sys.DaemonReload(); err != nil {
				return false, err
			}
			return true, nil
		},
		// RestoreUnit: write the verbatim captured prior unit bytes back through the
		// traversal-guarded orchestrate.WriteUnits (the rollback path).
		RestoreUnit: func(b []byte) error {
			dir, err := quadletUnitDir()
			if err != nil {
				return err
			}
			plan := orchestrate.Plan{Changed: []orchestrate.Unit{{Name: "villa-llama.container", Text: string(b)}}}
			return orchestrate.WriteUnits(plan, dir)
		},
		DaemonReload: sys.DaemonReload,
		Restart:      sys.Restart,
		Prove:        liveCodingProve,
	}
}

// codingDescriptor builds the inference.CodingModeSpec render descriptor from the served
// coder catalog entry (translation point — the catalog→inference translation lives in
// the live wiring, never in the pure renderer). It resolves the served entry, translates
// its catalog.AgentSampling → inference.Sampling, and carries the fail-closed
// CacheReuseSafe gate. No coding-flag literals here — those live behind the seam.
func codingDescriptor(_ config.VillaConfig, servedModel string) (*inference.CodingModeSpec, error) {
	cat, _, err := catalog.Load(modelCatalogPath)
	if err != nil {
		return nil, fmt.Errorf("load model catalog: %w", err)
	}
	m, ok := cat.FindByID(servedModel)
	if !ok {
		return nil, fmt.Errorf("coding model %q is not in the catalog", servedModel)
	}
	spec := &inference.CodingModeSpec{CacheReuseSafe: m.CacheReuseSafe}
	if s := m.AgentSampling; s != nil {
		spec.Sampling = &inference.Sampling{
			Temperature:   s.Temperature,
			TopP:          s.TopP,
			TopK:          s.TopK,
			RepeatPenalty: s.RepeatPenalty,
		}
	}
	return spec, nil
}
