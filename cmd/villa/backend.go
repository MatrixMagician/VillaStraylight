package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/backendswap"
	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
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

// proveTimeout bounds the cutover readiness poll (and, by the shared deadline
// context, the whole prove). It is seeded from inference's defaultReadyTimeout (5m)
// and documented as the load_tensors-hang guard (Pitfall 2 / T-8-07): a server that
// never becomes ready before this deadline is a PROVE FAIL → rollback, NEVER an
// infinite wait. inference does not export defaultReadyTimeout, so the value is
// mirrored here as a named const with that provenance.
const proveTimeout = 5 * time.Minute

// busySampleInterval is how often liveProve re-reads detect.GPUBusyPercent() DURING
// the generation probe (D-07 / Assumption A2): a single post-probe read can miss a
// short decode's busy window, so the prove samples repeatedly while tokens stream and
// keeps the max.
const busySampleInterval = 100 * time.Millisecond

// liveProve is the injected cutover gate (backendswap.Deps.Prove). It probes the
// ALREADY-running villa-llama.service (no --rm container) and returns a backendswap
// verdict the transactional core gates on. It composes THREE required gates inside a
// single bounded deadline:
//
//	(a) bounded readiness via the EXPORTED inference.PollHealth (Pitfall 2 timeout),
//	(b) a REAL generation probe via the EXPORTED inference.GenerationProbe (tokens>0),
//	(c) residency proof via inference.RunningOffloadVerdict fed the target backend's
//	    BackendFor(target).ResidencyProof() markers + the SAME concrete liveWeightBytes
//	    / liveModelFile seams status.go uses, with detect.GPUBusyPercent() sampled
//	    DURING the decode (D-07).
//
// It maps ONLY inference.StatusPass to backendswap.ProveStatusPass; any other verdict
// (including ready+health-200-but-residency-FAIL, SC#3) is a "fail" → the core rolls
// back. All ROCm/HSA/fault markers stay behind ResidencyProof() — this function is
// literal-free of them.
func liveProve(ctx context.Context, target string) backendswap.ProveVerdict {
	// Resolve the backend fail-closed (D-02): an unknown target is a prove fail, never
	// a silent fallback. backend.ResidencyProof() is the ONLY source of backend markers.
	backend, err := inference.BackendFor(target)
	if err != nil {
		return backendswap.ProveVerdict{Status: "fail", Detail: err.Error()}
	}

	// Load the source of truth for the residency seams (ConfigModel/ConfigContext) and
	// the probe's model id. A config-load failure is a prove fail.
	cfg, err := config.LoadVilla()
	if err != nil {
		return backendswap.ProveVerdict{Status: "fail", Detail: "load config: " + err.Error()}
	}

	// Resolve the catalog-resolved GGUF filename ONCE for ConfigModel — the SAME
	// concrete seam status.go uses (cmd/villa/lifecycle.go liveModelFile), never a
	// placeholder. Its error is a prove fail.
	modelFile, err := liveModelFile(cfg)
	if err != nil {
		return backendswap.ProveVerdict{Status: "fail", Detail: "resolve model file: " + err.Error()}
	}

	// Derive the inference endpoint the SAME way the status path does (mirror
	// status.go:150): the resolved backend's container runner, never a hand-rolled URL.
	endpoint := inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint()

	// Bound the WHOLE prove by proveTimeout (Pitfall 2 / T-8-07). A never-ready server
	// trips this deadline and is a FAIL, not an infinite wait.
	deadlineCtx, cancel := context.WithTimeout(ctx, proveTimeout)
	defer cancel()

	// (a) Bounded readiness. PollHealth is the EXPORTED non-container readiness gate
	// over the already-running server; a 200 means "accepting requests", not "offload
	// happened" — that is gate (c). Never-ready before the deadline → FAIL.
	ready := inference.PollHealth(deadlineCtx, endpoint, proveTimeout)
	if !ready.Known || !ready.Value {
		return backendswap.ProveVerdict{
			Status: "fail",
			Detail: "not ready before timeout (possible load_tensors hang or CPU-fallback stall)",
		}
	}

	// (b) REAL generation probe, with the live gpu_busy_percent sampled DURING the
	// decode window (D-07 / Assumption A2 / Open Question 3). Start the probe in a
	// goroutine and poll detect.GPUBusyPercent() repeatedly while tokens stream, keeping
	// the max — a single post-probe read can miss a short decode's busy window.
	chatCh := make(chan inference.ChatResult, 1)
	go func() {
		chatCh <- inference.GenerationProbe(deadlineCtx, endpoint, cfg.Model)
	}()

	maxBusy := detect.UnknownInt("gpu_busy_percent not sampled during probe", "")
	ticker := time.NewTicker(busySampleInterval)
	defer ticker.Stop()
sampleLoop:
	for {
		// Sample once up front and on every tick so even a very short decode gets at
		// least one in-flight read.
		if b := detect.GPUBusyPercent(); b.Known && (!maxBusy.Known || b.Value > maxBusy.Value) {
			maxBusy = b
		}
		select {
		case chat := <-chatCh:
			// One last read at completion, then stop sampling.
			if b := detect.GPUBusyPercent(); b.Known && (!maxBusy.Known || b.Value > maxBusy.Value) {
				maxBusy = b
			}
			if !chat.OK || chat.Tokens == 0 {
				detail := "generation probe returned no tokens"
				if chat.Detail != "" {
					detail = "generation probe failed: " + chat.Detail
				}
				return backendswap.ProveVerdict{Status: "fail", Detail: detail}
			}
			break sampleLoop
		case <-ticker.C:
			// keep sampling
		case <-deadlineCtx.Done():
			return backendswap.ProveVerdict{
				Status: "fail",
				Detail: "generation probe did not complete before timeout (possible load_tensors hang or CPU-fallback stall)",
			}
		}
	}

	// (c) Residency proof. Read the INVOCATION-scoped journal (F-3 — ResidencyJournal,
	// not the whole-unit journal whose oldest bytes are stale prior-start output), then
	// fold the journal + GTT floor + the DURING-probe gpu_busy reading + the target
	// backend's markers into one Verdict. WeightBytes/ConfigModel/ConfigContext mirror
	// internal/status/status.go exactly via the concrete liveWeightBytes(cfg)/
	// liveModelFile(cfg)/cfg.Ctx seams — never placeholders. Markers come ONLY from
	// backend.ResidencyProof() (keeps this file literal-free, T-8-11).
	journal, _ := orchestrate.NewSystemd().ResidencyJournal(installServiceName)
	v := inference.RunningOffloadVerdict(inference.RunningOffloadInput{
		JournalText:    journal,
		GTTUsedBytes:   detect.GTTUsedBytes(),
		GPUBusyPercent: maxBusy,
		WeightBytes:    liveWeightBytes(cfg),
		ConfigModel:    modelFile,
		ConfigContext:  cfg.Ctx,
		Markers:        backend.ResidencyProof(),
	})

	// Map ONLY a true StatusPass to ProveStatusPass; everything else (FAIL/WARN, incl.
	// ready+200-but-residency-FAIL) is a fail → the core rolls back (SC#3).
	if v.Status == inference.StatusPass {
		return backendswap.ProveVerdict{Status: backendswap.ProveStatusPass, Detail: v.Detail}
	}
	return backendswap.ProveVerdict{Status: "fail", Detail: v.Detail}
}

// ---------------------------------------------------------------------------
// backend noun (BSET-01/02/03): `villa backend set <vulkan|rocm> [--dry-run]` and
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
	// Active backend = cfg.Backend (source of truth); resolve fail-closed (D-02). The
	// resolver normalizes the empty string to the default Vulkan backend; report the
	// resolved Name() so the empty-config default surfaces as "vulkan".
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

// newBackendSet builds `villa backend set <vulkan|rocm> [--dry-run]`: the
// transactional cutover. RunE returns the mapped exit code via os.Exit; the body of
// runBackendSet returns the int so tests drive it without a subprocess.
func newBackendSet() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "set <backend>",
		Short: "Switch the inference backend transactionally (capture → mutate → prove → rollback)",
		Long: "Switch the inference backend (vulkan|rocm) on the running install: re-check the PRESERVED " +
			"model against the target envelope (refuse-with-remediation if it no longer fits), run the ROCm " +
			"preflight for a rocm target, capture the prior unit verbatim, persist config + regenerate ONLY the " +
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
			target, res.FailedStep, res.FromBackend)
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
			res.FromBackend, res.ToBackend, installServiceName)
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
			rec := recommend.Pick(detect.Probe(), cat, recommend.Overrides{Model: cfg.Model})
			if rec.Fits {
				return true, ""
			}
			return false, fmt.Sprintf("needs %d bytes vs %d usable", rec.TotalBytes, rec.UsableEnvelopeBytes)
		},
		// PreflightROCm: meaningful only for a rocm target. For any non-rocm backend this
		// short-circuits ok=true (zero side effects). For rocm it runs the ROCm preflight
		// and refuses on the FIRST StatusFail with that check's Detail as the remediation.
		PreflightROCm: func(cfg config.VillaConfig) (bool, string) {
			if cfg.Backend != "rocm" {
				return true, ""
			}
			for _, c := range preflight.RunROCm(detect.Probe()) {
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
			units, err := orchestrate.Render(orchestrate.RenderInput{
				Backend:   backend,
				Cfg:       c,
				ModelFile: modelFile,
				ModelsDir: modelsDir(),
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
