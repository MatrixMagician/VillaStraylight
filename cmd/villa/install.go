package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/install"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// install.go wires the `villa install` verb. The flow itself is install.Run
// (internal/install/flow.go, ADR-0005); this file has two jobs: bind the live host
// into install.Deps, and render the Result as narration on the command's streams
// and an exit code. No install decision lives here.

// installServiceName and openWebUIServiceName are the inference and chat services,
// from the same unit map the flow starts them by.
var (
	installServiceName   = install.DefaultUnits().Inference
	openWebUIServiceName = install.DefaultUnits().ChatUI
)

// newInstall builds `villa install`: detect → recommend → preflight gate →
// consented host-prep → render → reconcile → write → daemon-reload → start →
// readiness poll, idempotent and --dry-run aware.
func newInstall() *cobra.Command {
	var opts install.Opts
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Detect, recommend, gate, generate, and bring up the local inference stack",
		Long: "Run the full managed bring-up: detect the host, recommend a fitting model, gate on a " +
			"safe host (offering privileged host-prep with per-step consent), ensure the recommended model " +
			"is downloaded, persist the selection to config.toml, render rootless Podman Quadlet units from " +
			"config, write only what changed, daemon-reload, start, and poll readiness — then print the " +
			"loopback inference endpoint. Re-running with unchanged config is a true no-op. --dry-run prints " +
			"the rendered units and writes nothing (no pull, no config write). Strictly local.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			deps, err := liveInstallDeps(cmdContext(cmd))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "install: %v\n", err)
				os.Exit(exitBlocked)
			}
			opts.Force, opts.JSON = force, jsonOut
			os.Exit(runInstall(cmd, opts, deps))
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "print the rendered units without writing, pulling, or starting anything")
	cmd.Flags().BoolVar(&opts.NoTUI, "no-tui", false, "skip the guided wizard; use the flag-driven install path")
	cmd.Flags().BoolVar(&opts.CodingAgent, "coding-agent", false, "install the local coding agent (Crush) addon: stage its pinned binary + coder model, render a locked-down config, and prove a tool-call round-trip")
	cmd.Flags().BoolVar(&opts.WebSearch, "web-search", false, "install the web-search addon: render the SearXNG service + the SSRF-guarded villa-websafe loader, wire Open WebUI's native web search, and prove SearXNG readiness (opt-in; default off)")
	return cmd
}

// runInstall renders the flow onto the command's streams and RETURNS the exit
// code (0 pass / 2 warn / 1 block); it never calls os.Exit.
func runInstall(cmd *cobra.Command, opts install.Opts, d install.Deps) int {
	d.Emit = emitTo(cmd.OutOrStdout(), cmd.ErrOrStderr())
	return install.Run(cmdContext(cmd), d, opts).Outcome.ExitCode()
}

// emitTo routes narration lines to the two streams.
func emitTo(out, errOut io.Writer) func(install.Line) {
	return func(l install.Line) {
		if l.Stderr {
			fmt.Fprint(errOut, l.Text)
			return
		}
		fmt.Fprint(out, l.Text)
	}
}

// liveInstallDeps binds install.Deps to the real host: detect.Probe, recommend.Pick
// against the loaded catalog, the orchestrate render/reconcile/write + systemd
// seam, the SELinux/linger privileged seams, the verified model downloader, the
// 0600 config writer and the readiness poll.
//
// ctx is the command's SIGINT/SIGTERM-cancelled context, captured by the closures
// that pull weights: install is the longest-running command and every transfer is
// multi-GB, so without it Ctrl-C could not interrupt a download. Cancelling
// mid-stream is safe: download.PullModel keeps the ".part" file and resumes it.
func liveInstallDeps(ctx context.Context) (install.Deps, error) {
	sys := orchestrate.NewSystemd()
	uname := installUsername()
	// The post-install endpoint line derives from the resolved backend's runner,
	// never a literal. A load failure or unknown backend blocks rather than defaults.
	cfg, err := config.LoadVilla()
	if err != nil {
		return install.Deps{}, fmt.Errorf("load config: %w", err)
	}
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return install.Deps{}, fmt.Errorf("resolve backend: %w", err)
	}
	endpoint := inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint()

	// resolveCatalogModel is the single model-id → catalog lookup for both the
	// on-disk check and the pull, so install never fabricates a weight path.
	resolveCatalogModel := func(rec recommend.Recommendation) (catalog.Model, bool) {
		cat, _, err := catalog.Load(modelCatalogPath)
		if err != nil {
			return catalog.Model{}, false
		}
		return cat.FindByID(rec.Model)
	}

	return install.Deps{
		LoadConfig: liveLoadedConfig,
		Probe:      detect.Probe,
		Pick: func(p detect.HostProfile, ov recommend.Overrides) recommend.Recommendation {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return recommend.Recommendation{}
			}
			// A persisted speculation mode is threaded in the same way, so a
			// re-install of a speculation-on stack re-resolves the mode the operator
			// chose rather than re-deciding it from the catalog.
			if ov.Speculation == "" {
				if cfg, err := config.LoadVilla(); err == nil {
					ov.Speculation = cfg.Speculation
				}
			}
			// The PERSISTED memory inputs shrink the envelope an opted-in install
			// recommends against.
			return recommend.Pick(p, cat, ov, liveLoadedMemoryInputs(), liveLoadedWebSearchInputs())
		},
		ModelFile: func(rec recommend.Recommendation) (string, error) {
			// Fabricating "<model>.gguf" would render a unit whose -m only fails at
			// runtime; an unresolvable model blocks here instead.
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return "", fmt.Errorf("load model catalog: %w", err)
			}
			m, ok := cat.FindByID(rec.Model)
			if !ok {
				return "", fmt.Errorf("model %q is not in the catalog — cannot resolve its weight file", rec.Model)
			}
			return primaryModelFile(m), nil
		},
		ModelsDir: modelsDir,
		RunChecks: preflight.RunWithResources,
		RunMemoryChecks: func(p detect.HostProfile, embeddingModel string) []preflight.CheckResult {
			return preflight.RunMemory(p, preflight.MemoryGateInput{EmbeddingModel: embeddingModel})
		},
		RunAgentChecks: liveAgentChecks,

		Interactive:  stdinIsInteractive,
		StdoutIsTTY:  stdoutIsTTY,
		Consent:      promptConsent,
		Username:     func() string { return uname },
		Wizard:       liveWizard,
		Setsebool:    liveSetsebool,
		EnableLinger: sys.EnableLinger,

		UnitDir:       quadletUnitDir,
		ResidentUnits: liveResidentUnits,
		HostVillaPath: hostVillaPath,
		CodingRender:  liveCodingRender,
		Render:        livePinnedRender,
		Reconcile:     orchestrate.Reconcile,

		ModelDownloaded: func(rec recommend.Recommendation) bool {
			// An unresolvable model reads as "not downloaded" so EnsureModel runs and
			// surfaces the catalog error rather than silently skipping the pull.
			m, ok := resolveCatalogModel(rec)
			if !ok {
				return false
			}
			return modelFilesPresent(modelsDir(), m)
		},
		EnsureModel: func(rec recommend.Recommendation) error {
			m, ok := resolveCatalogModel(rec)
			if !ok {
				return fmt.Errorf("model %q is not in the catalog — cannot download its weights", rec.Model)
			}
			dir := modelsDir()
			if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
				return mkErr
			}
			return pullFn(ctx, m, dir)
		},
		EmbedModelPresent: liveEmbedModelPresent,
		EnsureEmbedModel:  func(modelsDir string) error { return liveEnsureEmbedModel(ctx, modelsDir) },
		AgentCatalog: func() (catalog.Catalog, bool) {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return catalog.Catalog{}, false
			}
			return cat, true
		},
		CoderModelPresent: liveCoderModelPresent,
		EnsureCoderModel: func(modelsDir string, sh catalog.Shard) error {
			return liveEnsureCoderModel(ctx, modelsDir, sh)
		},
		InstallAgentBinary: liveInstallAgentBinary,
		RenderCrushConfig:  liveRenderCrushConfig,

		ReadUnit: func(dir, name string) (string, bool) {
			b, rerr := os.ReadFile(filepath.Join(dir, name))
			if rerr != nil {
				return "", false
			}
			return string(b), true
		},
		WriteUnit: writeUnitText,
		RemoveUnit: func(dir, name string) error {
			rerr := os.Remove(filepath.Join(dir, name))
			if rerr != nil && !os.IsNotExist(rerr) {
				return rerr
			}
			return nil
		},
		IsActive: sys.IsActive,
		ConfigExists: func() bool {
			path, perr := config.Path()
			if perr != nil {
				return false
			}
			_, serr := os.Stat(path)
			return serr == nil
		},
		RemoveConfig: func() error {
			path, perr := config.Path()
			if perr != nil {
				return perr
			}
			rerr := os.Remove(path)
			if rerr != nil && !os.IsNotExist(rerr) {
				return rerr
			}
			return nil
		},

		SaveConfig: config.SaveVilla,
		// The dashboard is a NATIVE user .service in the user-unit dir, not a
		// Quadlet .container; its ExecStart targets the running binary.
		UserUnitDir:       orchestrate.UserUnitDir,
		ResolveBinaryPath: resolveDashboardBinaryPath,
		ReadDashboardUnit: func(dir string) ([]byte, error) {
			return os.ReadFile(filepath.Join(dir, orchestrate.DashboardServiceName))
		},
		WriteDashboardUnit: orchestrate.WriteDashboardUnit,
		WriteUnits:         orchestrate.WriteUnits,
		DaemonReload:       sys.DaemonReload,
		Enable:             sys.Enable,
		Start:              sys.Start,
		Stop:               sys.Stop,

		// The secrets reach the containers via 0600 EnvironmentFiles, never a 0644 unit.
		WriteWebsafeSecretEnv: orchestrate.WriteWebsafeSecretEnv,
		WriteSearxngSettings:  orchestrate.WriteSearxngSettings,
		WriteSearxngSecretEnv: orchestrate.WriteSearxngSecretEnv,

		Endpoint:        func() string { return endpoint },
		PollReady:       liveReadinessPoll,
		ReadRecallState: liveRecallStateLoad,
		ProveMemory: func(ctx context.Context, cfg config.VillaConfig) install.Proof {
			p := liveMemoryProof(ctx, memoryProofInput{
				embedAddr:    config.EmbedAddr,
				embedPort:    config.EmbedPort,
				embedModel:   cfg.EmbeddingModel,
				embeddingDim: cfg.EmbeddingDim,
				qdrantAddr:   config.QdrantAddr,
				qdrantPort:   config.QdrantPort,
			})
			return install.Proof{Status: p.status, Detail: p.detail}
		},
		ProveSearch: func(ctx context.Context) install.Proof {
			p := liveSearxngProof(ctx, searxngProofInput{searxngAddr: config.SearxngAddr, searxngPort: config.SearxngPort})
			return install.Proof{Status: p.status, Detail: p.detail}
		},
		ProveAgent: func(ctx context.Context) install.Proof {
			p := evalAgentProof(liveAgentToolCallProbe(ctx))
			return install.Proof{Status: p.status, Detail: p.detail}
		},
	}, nil
}

// liveCodingRender resolves the served coder's -m file and its coding-mode
// descriptor through the same helpers `villa coding-mode enter` renders with. The
// catalog→inference translation stays here: the pure renderer never imports the
// catalog.
func liveCodingRender(cfg config.VillaConfig) (string, *inference.CodingModeSpec, error) {
	servedModel, _ := codingServedTarget(cfg)
	modelFile, err := codingModelFile(cfg, servedModel)
	if err != nil {
		return "", nil, fmt.Errorf("resolve coder model file: %w", err)
	}
	spec, err := codingDescriptor(cfg, servedModel)
	if err != nil {
		return "", nil, fmt.Errorf("build coding-mode descriptor: %w", err)
	}
	return modelFile, spec, nil
}

// liveAgentChecks runs the coding-agent preflight gates. The staged footprint
// (coder GGUF + pinned binary) resolves from the SAME catalog entry and policy pin
// the flow stages, so the disk gate cannot drift from what is written. A catalog
// failure or no coder fit yields a zero staged size; the envelope check is the
// BLOCK that refuses that case.
func liveAgentChecks(p detect.HostProfile, rec recommend.Recommendation) []preflight.CheckResult {
	var staged uint64
	if cat, _, err := catalog.Load(modelCatalogPath); err == nil {
		if sh, ok := install.CoderShardFor(rec, cat); ok {
			staged += sh.SizeBytes
		}
	}
	if asset, ok := agent.LoadCrushPolicy().Assets["linux/amd64"]; ok {
		staged += asset.Size
	}
	return runAgentChecks(p, rec, agentCheckInput{
		stagedBytes: staged,
		dataDir:     modelsDir(),
		statfs:      liveAgentStatfs,
		lookupEnv:   os.LookupEnv,
	})
}

// liveAgentStatfs reads real free space at a path via syscall.Statfs (the same
// locale-proof, no-shell-to-df discipline preflight uses; its helper is package-
// private, so the cmd-tier agent gate carries its own copy). It walks up to an
// existing ancestor so a not-yet-created models dir still reports its filesystem's
// free space. A statfs error → ok=false → a typed-Unknown WARN, never a false BLOCK.
func liveAgentStatfs(path string) (uint64, bool) {
	p := existingAncestorDir(path)
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}

// existingAncestorDir returns path if it exists, else the nearest existing parent
// (down to "/"), so statfs has a real path for a target dir not yet created.
func existingAncestorDir(path string) string {
	if path == "" {
		return "/"
	}
	p := path
	for {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "/"
		}
		p = parent
	}
}

// quadletUnitDir is the fixed rootless Quadlet generator directory
// (~/.config/containers/systemd), created if absent so the first install writes
// cleanly. It mirrors the XDG config discipline of internal/config.
func quadletUnitDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "containers", "systemd")
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return "", mkErr
	}
	return dir, nil
}

// resolveDashboardBinaryPath returns the stable absolute path of the running villa
// binary for the dashboard unit's ExecStart: os.Executable, then EvalSymlinks
// (collapse a symlinked launcher so the unit survives the symlink being swapped),
// then Abs. Correct for a dev build and an installed binary alike, no copying.
//
// A fatal os.Executable or Abs error is RETURNED so the install fails; it never
// falls back to a fixed path like ~/.local/bin/villa, which is the exact path that
// produced 203/EXEC at boot. A non-fatal EvalSymlinks failure degrades to the raw
// os.Executable path: still the running binary, still absolute.
func resolveDashboardBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("os.Executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("filepath.Abs(%q): %w", resolved, err)
	}
	return abs, nil
}

// installUsername resolves the current username for the loginctl enable-linger
// consent step, preferring os/user over $USER (matches preflight's liveLingerDeps).
func installUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// writeUnitText restores one unit file's prior contents during a rollback. It is
// separate from the forward path's WriteUnits because rollback restores a captured
// TEXT rather than re-rendering from config: re-rendering would produce whatever
// the current config says, which is exactly the config the rollback is undoing.
func writeUnitText(dir, name, text string) error {
	if err := assertWithinDir(filepath.Join(dir, name), dir); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644) //nolint:gosec // unit files are world-readable by design; secrets live in 0600 env files
}

// modelFilesPresent reports whether every file the entry needs is on disk under
// dir: the model shards and any sidecar, because a projector is pulled with its
// model. Checking only the primary file let a vision-on install skip the pull and
// write a unit that named a projector the host never downloaded.
func modelFilesPresent(dir string, m catalog.Model) bool {
	shards := m.AllShards()
	if len(shards) == 0 {
		_, err := os.Stat(filepath.Join(dir, primaryModelFile(m)))
		return err == nil
	}
	for _, sh := range shards {
		if _, err := os.Stat(filepath.Join(dir, sh.Filename)); err != nil {
			return false
		}
	}
	return true
}
