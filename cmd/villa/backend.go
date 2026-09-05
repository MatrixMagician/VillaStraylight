package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/backendswap"
	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/residency"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// backend.go is the cmd-tier `villa backend` noun: the live host wiring that drives
// the pure internal/backendswap transactional core (Plan 08-01). It holds the ONE
// genuinely new composition — liveProve — plus the cobra surface (set/show/--dry-run),
// the Result→exit mapping, and liveBackendSwapDeps (Task 08-02-02).
//
// CRITICAL — backend-marker discipline (T-8-11): this file must stay LITERAL-FREE of
// backend marker tokens (the per-backend residency device token, the HSA override env
// var, the GPU-fault abort string, and any image/device literal). Every such marker
// arrives ONLY through inference.BackendFor(target).ResidencyProof(); the
// 08-01-03-extended TestSeamGrepGate now WALKS cmd/villa and fails CI on any such leak.
// Do NOT paste markers here.

// liveResidencyDeps wires the residency drive protocol to the real host. It is the
// ONE binding of internal/residency's seams to the live primitives, shared by every
// caller that proves residency, so no two proofs can drift onto different readers.
func liveResidencyDeps() residency.Deps {
	return residency.Deps{
		PollHealth: inference.PollHealth,
		Generate:   inference.GenerationProbe,
		GPUBusy:    detect.GPUBusyPercent,
		GTTUsed:    detect.GTTUsedBytes,
		// The INVOCATION-scoped journal (ResidencyJournal), never the whole-unit
		// journal, whose oldest bytes are stale prior-start output.
		Journal: orchestrate.NewSystemd().ResidencyJournal,
		Fold:    inference.RunningOffloadVerdict,
	}
}

// liveProve is the injected cutover gate (backendswap.Deps.Prove). It resolves what
// is being proven — the target backend, the served model file and the chat ctx — and
// hands it to the shared residency drive protocol, which owns the three gates
// (bounded readiness, a real generation probe, the residency fold) and the
// Unknown-versus-negative rule.
//
// Resolution failures are prove FAILs, never a silent fallback: an unknown backend,
// an unreadable config or an unresolvable model file each refuse here, before the
// protocol runs.
//
// All ROCm/HSA/fault markers stay behind BackendFor(target).ResidencyProof() — this
// function is literal-free of them (T-8-11).
func liveProve(ctx context.Context, target string) prove.Verdict {
	backend, err := inference.BackendFor(target)
	if err != nil {
		return prove.Verdict{Status: prove.StatusFail, Detail: err.Error()}
	}

	// The source of truth for the residency seams (ConfigModel/ConfigContext) and the
	// probe's model id.
	cfg, err := config.LoadVilla()
	if err != nil {
		return prove.Verdict{Status: prove.StatusFail, Detail: "load config: " + err.Error()}
	}

	// The catalog-resolved GGUF FILENAME for ConfigModel — the SAME concrete seam
	// status.go uses (liveModelFile), never a placeholder. The /props drift overlay
	// compares against the model FILE, so the catalog id would make it misfire.
	modelFile, err := liveModelFile(cfg)
	if err != nil {
		return prove.Verdict{Status: prove.StatusFail, Detail: "resolve model file: " + err.Error()}
	}

	return residency.ProveCutover(ctx, liveResidencyDeps(), residency.Target{
		// The endpoint is derived the SAME way the status path does: the resolved
		// backend's container runner, never a hand-rolled URL.
		Endpoint:    inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint(),
		Service:     installServiceName,
		ModelID:     cfg.Model,
		ModelFile:   modelFile,
		ContextLen:  cfg.Ctx,
		WeightBytes: liveWeightBytes(cfg),
		Markers:     backend.ResidencyProof(),
	})
}

// ---------------------------------------------------------------------------
// backend noun (BSET-01/02/03): `villa backend set <rocm|rocm-6.4.4|rocm-6.4.4-rocwmma|vulkan> [--dry-run]` and
// `villa backend show`. Cloned from the model.go swap noun: RunE returns the mapped
// exit code (body RETURNS the int so tests assert output+code without a subprocess),
// the Result→exit mapping mirrors runModelSwap, and the live Deps wire every host
// seam to the proven in-repo primitives.
// ---------------------------------------------------------------------------

// newBackend builds the `villa backend` noun and its show/set subcommands. The noun
// name does not collide with the Phase-3 lifecycle verbs.
func newBackend() *cobra.Command {
	backend := &cobra.Command{
		Use:   "backend",
		Short: "Inspect and switch the inference GPU backend (vulkan/rocm)",
		Long: "Show the active inference backend or switch it with a transactional cutover: `set <backend>` " +
			"swaps ONLY the villa-llama unit (model/quant/context preserved), refuses-with-remediation on a " +
			"fit or ROCm-preflight failure, and rolls back verbatim if the new backend does not prove healthy " +
			"(a real generation probe + GPU-residency proof within a bounded timeout). --dry-run previews the " +
			"target/fit/preflight without mutating anything.",
		Args: cobra.NoArgs,
	}
	backend.AddCommand(newBackendShow(), newBackendSet())
	return backend
}

// newBackendShow builds `villa backend show [--json]`: report the active backend
// (cfg.Backend — the source of truth) and its resolved image tag via
// inference.BackendFor(cfg.Backend).Image() (mirror status.go's BackendFor usage).
func newBackendShow() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the active inference backend and its resolved image tag",
		Long: "Print the active inference backend read from config.toml (the source of truth) and its " +
			"resolved container image tag. --json emits the machine-readable form.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code := runBackendShow(cmd, asJSON)
			os.Exit(code)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the backend info as JSON")
	return cmd
}

// backendShowEntry is the `backend show --json` shape: the active backend and its
// resolved image tag.
type backendShowEntry struct {
	Backend string `json:"backend"`
	Image   string `json:"image"`
}

// runBackendShow loads config, resolves the active backend's image, and renders the
// view, RETURNING the exit code (no os.Exit) so tests assert output + code.
func runBackendShow(cmd *cobra.Command, asJSON bool) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	cfg, err := config.LoadVilla()
	if err != nil {
		fmt.Fprintf(errOut, "backend show: load config: %v\n", err)
		return exitBlocked
	}
	// Active backend = cfg.Backend (source of truth); resolve fail-closed. The
	// resolver normalizes the empty string to the default ROCm backend; report the
	// resolved Name() so the empty-config default surfaces as "rocm".
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		fmt.Fprintf(errOut, "backend show: resolve backend %q: %v\n", cfg.Backend, err)
		return exitBlocked
	}
	entry := backendShowEntry{Backend: backend.Name(), Image: backend.Image()}

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entry); err != nil {
			fmt.Fprintf(errOut, "backend show: encode json: %v\n", err)
			return exitBlocked
		}
		return exitPass
	}
	fmt.Fprintf(out, "%-10s %s\n", "backend", entry.Backend)
	fmt.Fprintf(out, "%-10s %s\n", "image", entry.Image)
	return exitPass
}

// newBackendSet builds `villa backend set <rocm|rocm-6.4.4|rocm-6.4.4-rocwmma|vulkan> [--dry-run]`: the
// transactional cutover. RunE returns the mapped exit code via os.Exit; the body of
// runBackendSet returns the int so tests drive it without a subprocess.
func newBackendSet() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "set <backend>",
		Short: "Switch the inference backend transactionally (capture → mutate → prove → rollback)",
		Long: "Switch the inference backend (rocm — the default, rocm-6.4.4, rocm-6.4.4-rocwmma, or the " +
			"vulkan fallback) on the running install: re-check the PRESERVED " +
			"model against the target envelope (refuse-with-remediation if it no longer fits), run the ROCm " +
			"preflight for any ROCm-family target, capture the prior unit verbatim, persist config + regenerate ONLY the " +
			"villa-llama unit + restart it, and PROVE the cutover (real generation probe + GPU-residency proof " +
			"within a bounded timeout). Any mutate error or a non-pass proof rolls back verbatim — a failed " +
			"switch is a no-op to the running stack. Exits 0 on switch/no-op, 1 on refusal/error/rollback. " +
			"--dry-run previews target/fit/preflight and mutates nothing.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := runBackendSet(cmd, args[0], dryRun, liveBackendSwapDeps())
			os.Exit(code)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview target/fit/preflight without persisting, regenerating, or restarting anything")
	return cmd
}

// runBackendSet performs the dry-run preview OR the transactional switch and RETURNS
// the exit code. The dry-run branch is FIRST and side-effect-free; the real branch
// delegates to backendswap.Run and maps the typed Result to exit codes + messages
// (clone of runModelSwap). The body returns the int (no os.Exit) so tests assert
// output+code.
func runBackendSet(cmd *cobra.Command, target string, dryRun bool, d *backendswap.Deps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	// DRY-RUN FIRST: load config, compute the fit + (rocm) preflight verdicts, print
	// {target, fit, preflight}, and write NOTHING (no SaveConfig/ReconcileAndWrite/
	// Restart/CaptureUnit). A dry run has zero side effects (BSET-03).
	if dryRun {
		cfg, err := d.LoadConfig()
		if err != nil {
			fmt.Fprintf(errOut, "backend set: load config: %v\n", err)
			return exitBlocked
		}
		fmt.Fprintf(out, "dry-run: would switch backend %s -> %s (model %q preserved)\n", cfg.Backend, target, cfg.Model)

		fitOK, fitReason := d.FitsModel(cfg)
		if fitOK {
			fmt.Fprintf(out, "  fit:       PASS — %q fits the target envelope\n", cfg.Model)
		} else {
			fmt.Fprintf(out, "  fit:       FAIL — %s\n", fitReason)
		}

		// PreflightROCm is meaningful only for a rocm target; the live seam short-circuits
		// ok=true otherwise. Report it against the would-be target backend.
		cfgTarget := cfg
		cfgTarget.Backend = target
		preOK, preReason := d.PreflightROCm(cfgTarget)
		if preOK {
			fmt.Fprintf(out, "  preflight: PASS\n")
		} else {
			fmt.Fprintf(out, "  preflight: FAIL — %s\n", preReason)
		}
		fmt.Fprintf(out, "dry-run: nothing written (no config persisted, no units regenerated, no restart)\n")
		return exitPass
	}

	// REAL switch: the typed Result drives the exit mapping (clone of runModelSwap).
	res := backendswap.Run(*d, target)
	switch {
	case res.Refused:
		// A clean policy rejection (fit/preflight/capture) with zero side effects.
		if res.Reason != "" {
			fmt.Fprintf(errOut, "backend set: refusing to switch to %s — %s\n", target, res.Reason)
		} else if res.Err != nil {
			fmt.Fprintf(errOut, "backend set: refusing to switch to %s — %s failed: %v\n", target, res.FailedStep, res.Err)
		} else {
			fmt.Fprintf(errOut, "backend set: refusing to switch to %s\n", target)
		}
		return exitBlocked
	case res.RolledBack:
		// A mutate error or a non-pass prove verdict rolled back verbatim. Reason already
		// folds in an honest rollback-incomplete message when the restore did not fully
		// complete (Pitfall 5).
		fmt.Fprintf(errOut, "backend set: switch to %s failed at %q — rolled back; prior backend (%s) restored\n",
			target, res.FailedStep, res.From)
		if res.Reason != "" {
			fmt.Fprintf(errOut, "  detail: %s\n", res.Reason)
		}
		if res.Err != nil {
			fmt.Fprintf(errOut, "  error:  %v\n", res.Err)
		}
		return exitBlocked
	case res.Err != nil:
		// A non-rollback failure path (defensive; Run rolls back mutate errors).
		fmt.Fprintf(errOut, "backend set: switch to %s failed at %q: %v\n", target, res.FailedStep, res.Err)
		return exitBlocked
	case res.NoOp:
		fmt.Fprintf(out, "already on %s — no change\n", target)
		return exitPass
	default: // Switched
		fmt.Fprintf(out, "switched backend %s -> %s — config persisted, %s regenerated and restarted, cutover proven\n",
			res.From, res.To, installServiceName)
		return exitPass
	}
}

// liveBackendSwapDeps wires the transactional core to the real host: config load/save,
// the recommend fit-math against the PRESERVED model, the ROCm preflight gate, verbatim
// unit capture/restore through the traversal-guarded orchestrate seams, the render/
// reconcile/write closure (cloned from liveSwapDeps), the systemd reload/restart seam,
// and liveProve as the cutover gate. Every host-touching action is a seam so
// backend_test.go drives the flow without a live host.
func liveBackendSwapDeps() *backendswap.Deps {
	sys := orchestrate.NewSystemd()
	return &backendswap.Deps{
		InstallServiceName: installServiceName,
		LoadConfig:         config.LoadVilla,
		SaveConfig:         config.SaveVilla,
		// FitsModel: reuse the recommend fit-math against the PRESERVED config model
		// (model = config, never re-pick) — the liveSwapDeps Fits closure keyed on
		// cfg.Model. A non-fit returns the bytes-needed-vs-usable remediation.
		FitsModel: func(cfg config.VillaConfig) (bool, string) {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return false, "catalog load failed"
			}
			// Memory inputs from the PRESERVED config threaded into the closure
			// the backend-swap fit gate sees the same shrunken envelope.
			// The speculation mode is threaded from the SAME config, so
			// `speculation set` gets ResolveSpeculation's refusal for an unqualified
			// entry back through this gate as a non-fit with the note as the reason.
			rec := recommend.Pick(detect.Probe(), cat, recommend.Overrides{Model: cfg.Model, Speculation: cfg.Speculation},
				recommend.MemoryInputs{Enabled: subsystem.MemoryOn(cfg), EmbeddingModel: cfg.EmbeddingModel},
				webSearchInputsFrom(cfg))
			if rec.Fits {
				return true, ""
			}
			// A speculation refusal is a non-fit with no memory shortfall, so report
			// its note rather than a byte count that is not the problem.
			for _, n := range rec.Notes {
				if strings.HasPrefix(n, "speculation: ") {
					return false, n
				}
			}
			return false, fmt.Sprintf("needs %d bytes vs %d usable", rec.TotalBytes, rec.UsableEnvelopeBytes)
		},
		// PreflightROCm: meaningful only for a ROCm-family target. For any non-ROCm
		// backend this short-circuits ok=true (zero side effects). For a ROCm-family backend
		// it resolves the target image and runs the ROCm preflight against the REAL digest,
		// refusing on the FIRST StatusFail with that check's Detail as the remediation.
		PreflightROCm: func(cfg config.VillaConfig) (bool, string) {
			if !inference.IsROCmFamily(cfg.Backend) {
				return true, ""
			}
			// Resolve the target backend so the policy gate evaluates the ACTUAL
			// digest against imageDeny. Fail-closed on an unknown name.
			b, err := inference.BackendFor(cfg.Backend)
			if err != nil {
				return false, err.Error()
			}
			for _, c := range preflight.RunROCmForImage(detect.Probe(), b.Image()) {
				if c.Status == preflight.StatusFail {
					return false, c.Detail
				}
			}
			return true, ""
		},
		// CaptureUnit: read the verbatim prior villa-llama.container bytes from the quadlet
		// unit dir (inside quadletUnitDir() — traversal-bounded by construction).
		CaptureUnit: func() ([]byte, error) {
			dir, err := quadletUnitDir()
			if err != nil {
				return nil, err
			}
			return os.ReadFile(filepath.Join(dir, "villa-llama.container"))
		},
		// ReconcileAndWrite: render units from the persisted config, write only the
		// changed unit(s), daemon-reload inside (clone of the liveSwapDeps closure).
		ReconcileAndWrite: func(c config.VillaConfig) (bool, error) {
			dir, err := quadletUnitDir()
			if err != nil {
				return false, err
			}
			modelFile, err := liveModelFile(c)
			if err != nil {
				return false, err
			}
			backend, err := inference.BackendFor(c.Backend)
			if err != nil {
				return false, err
			}
			resident, err := liveResidentUnits(c)
			if err != nil {
				return false, err
			}
			units, err := livePinnedRender(orchestrate.RenderInput{
				Backend:       backend,
				Cfg:           c,
				ModelFile:     modelFile,
				ModelsDir:     modelsDir(),
				HostVillaPath: hostVillaPath(),
				Resident:      resident,
			})
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
		Prove:        liveProve,
	}
}
