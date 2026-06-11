package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/download"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/modelswap"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// pullFn is the downloader seam. It defaults to download.PullModel and is
// overridden in tests so the success path can be exercised without live network.
var pullFn = download.PullModel

// catalogPath is reserved for a future --catalog override; for now `model pull`
// reads the embedded seed (externalPath ""). Kept as a seam so the verb's catalog
// source is in one place.
var modelCatalogPath string

// newModel builds the `villa model` noun and its `pull <name>` subcommand. The
// noun name is chosen to NOT collide with the Phase-3 lifecycle verbs
// (up/down/restart/install/status) — D-13.
func newModel() *cobra.Command {
	model := &cobra.Command{
		Use:   "model",
		Short: "Acquire and inspect local model weights",
		Long:  "Manage the GGUF model weights villa downloads into the local models dir. Strictly local; the only outbound traffic is the model pull itself.",
		Args:  cobra.NoArgs,
	}
	model.AddCommand(newModelPull(), newModelList(), newModelSwap())
	return model
}

// newModelPull builds `villa model pull <name>`: resolve <name> through the
// catalog (never as a path), download + verify every shard into the models dir,
// and map success/failure to exit 0/1 (MODEL-02). There is no warn tier for pull.
func newModelPull() *cobra.Command {
	return &cobra.Command{
		Use:   "pull <name>",
		Short: "Download and verify a model by catalog name into the local models dir",
		Long: "Resolve <name> to a catalog entry, download its GGUF shard(s) from HuggingFace with " +
			"SHA256 + size verification and HTTP-Range resume, and atomically place the verified file(s) " +
			"under $XDG_DATA_HOME/villa/models. Exits 0 on success, 1 on any download/verify failure.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := runModelPull(cmd, args[0])
			os.Exit(code)
			return nil
		},
	}
}

// runModelPull performs the pull and RETURNS the exit code (it does not call
// os.Exit) so tests can assert both output and the mapped code without spawning a
// subprocess. All printing + exit mapping lives here; the downloader stays a pure
// library.
func runModelPull(cmd *cobra.Command, name string) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	cat, warnings, err := catalog.Load(modelCatalogPath)
	if err != nil {
		fmt.Fprintf(errOut, "model pull: catalog load failed: %v\n", err)
		return exitBlocked
	}
	for _, w := range warnings {
		fmt.Fprintf(errOut, "warning: %s\n", w)
	}

	// Resolve the name THROUGH the catalog — never treat the arg as a filesystem
	// path (V5 / T-02-04 command-injection + path-traversal guard).
	m, ok := cat.FindByID(name)
	if !ok {
		fmt.Fprintf(errOut, "model pull: unknown model %q — run `villa recommend` to see catalog names\n", name)
		return exitBlocked
	}

	modelsDir := modelsDir()
	if mkErr := os.MkdirAll(modelsDir, 0o700); mkErr != nil {
		fmt.Fprintf(errOut, "model pull: cannot create models dir %q: %v\n", modelsDir, mkErr)
		return exitBlocked
	}

	if dlErr := pullFn(context.Background(), m, modelsDir); dlErr != nil {
		fmt.Fprintf(errOut, "model pull: %s failed: %v\n", m.ID, dlErr)
		return exitBlocked
	}

	var totalBytes uint64
	for _, s := range m.Shards {
		totalBytes += s.SizeBytes
	}
	if len(m.Shards) == 1 {
		fmt.Fprintf(out, "pulled %s -> %s (%d bytes, verified)\n", m.ID, filepath.Join(modelsDir, m.Shards[0].Filename), totalBytes)
	} else {
		fmt.Fprintf(out, "pulled %s -> %s (%d shards, %d bytes, verified)\n", m.ID, modelsDir, len(m.Shards), totalBytes)
	}
	return exitPass
}

// modelsDir resolves the on-disk models directory: $XDG_DATA_HOME/villa/models
// (default ~/.local/share/villa/models), per D-08. This mirrors the preflight
// defaultDataDir XDG logic; downloaded weights live here and Phase 3 bind-mounts
// the dir read-only into the inference container.
func modelsDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "villa", "models")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "villa", "models")
	}
	return filepath.Join("/var/tmp", "villa", "models")
}

// ---------------------------------------------------------------------------
// model list (MODEL-01 / D-10): distinguish available (catalog) from loaded
// (the model the current inference unit was generated with, read from config).
// ---------------------------------------------------------------------------

// listOpts are the per-invocation flags for `villa model list`.
type listOpts struct {
	// asJSON emits the machine-readable form instead of the table.
	asJSON bool
}

// listDeps is the injectable seam set for `model list` so the test drives the
// catalog + loaded-config sources without touching the real XDG config.
type listDeps struct {
	loadCatalog func() (catalog.Catalog, []string, error)
	loadConfig  func() (config.VillaConfig, error)
}

// liveListDeps wires `model list` to the real host: the embedded/overridden
// catalog and the persisted config (the source of truth for the loaded model).
func liveListDeps() *listDeps {
	return &listDeps{
		loadCatalog: func() (catalog.Catalog, []string, error) { return catalog.Load(modelCatalogPath) },
		loadConfig:  config.LoadVilla,
	}
}

// modelListEntry is one row of `model list --json`: a catalog model and whether it
// is the currently loaded selection.
type modelListEntry struct {
	ID     string `json:"id"`
	Quant  string `json:"quant"`
	Loaded bool   `json:"loaded"`
}

// newModelList builds `villa model list`: print every catalog model marked
// "available", with the config'd model marked "loaded" (D-10). --json supported.
func newModelList() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List catalog models (available) and the currently loaded one",
		Long: "Print every model in the catalog as 'available' and mark the model the inference unit is " +
			"currently generated with — read from config.toml, the source of truth — as 'loaded' (MODEL-01). " +
			"--json emits the machine-readable form.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code := runModelList(cmd, listOpts{asJSON: asJSON}, liveListDeps())
			os.Exit(code)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the model list as JSON")
	return cmd
}

// runModelList loads the catalog + config and renders the available-vs-loaded view,
// RETURNING the exit code (no os.Exit) so tests assert output + code.
func runModelList(cmd *cobra.Command, opts listOpts, d *listDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	cat, warnings, err := d.loadCatalog()
	if err != nil {
		fmt.Fprintf(errOut, "model list: catalog load failed: %v\n", err)
		return exitBlocked
	}
	for _, w := range warnings {
		fmt.Fprintf(errOut, "warning: %s\n", w)
	}

	cfg, err := d.loadConfig()
	if err != nil {
		fmt.Fprintf(errOut, "model list: load config: %v\n", err)
		return exitBlocked
	}

	entries := make([]modelListEntry, 0, len(cat.Models))
	for _, m := range cat.Models {
		entries = append(entries, modelListEntry{ID: m.ID, Quant: m.Quant, Loaded: m.ID == cfg.Model})
	}

	if opts.asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entries); err != nil {
			fmt.Fprintf(errOut, "model list: encode json: %v\n", err)
			return exitBlocked
		}
		return exitPass
	}

	for _, e := range entries {
		state := "available"
		if e.Loaded {
			state = "loaded"
		}
		fmt.Fprintf(out, "%-10s %-24s %s\n", state, e.ID, e.Quant)
	}
	return exitPass
}

// ---------------------------------------------------------------------------
// model swap (MODEL-03 / D-09): fit-guard → auto-pull → persist-config-FIRST →
// regenerate-and-restart-inference-only. Reuses recommend.Pick (fit-math),
// download.PullModel (verified pull), config.SaveVilla (source of truth), and the
// Plan-01 orchestrate render/reconcile/write + systemd seam — no new envelope
// math, no new downloader.
// ---------------------------------------------------------------------------

// newModelSwap builds `villa model swap <name>`: refuse a non-fitting target,
// auto-pull if absent, persist config FIRST, then regenerate + restart only the
// inference unit (MODEL-03 / D-09).
func newModelSwap() *cobra.Command {
	return &cobra.Command{
		Use:   "swap <name>",
		Short: "Switch the loaded model: fit-guard, auto-pull, persist config, restart inference",
		Long: "Resolve <name> through the catalog, REFUSE it if it won't fit the usable memory envelope " +
			"(reuse the recommend fit-math — a clear FAIL, never a silent OOM at container start), auto-pull " +
			"its weights if absent, persist it to config.toml BEFORE regenerating the inference unit args, then " +
			"restart ONLY the inference service. Exits 0 on success, 1 on refusal/failure.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := runModelSwap(cmd, args[0], liveSwapDeps())
			os.Exit(code)
			return nil
		},
	}
}

// runModelSwap performs the swap and RETURNS the exit code. The ordering-is-the-
// security-contract sequence (D-09) lives in modelswap.Run; this caller maps the
// typed modelswap.Result to the human messages it used to print and to exit codes
// (refuse/err→1, switched/no-op→0).
func runModelSwap(cmd *cobra.Command, name string, d *modelswap.Deps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	// Auto-pull emits a "pulling..." progress line before the (possibly slow) download
	// the way the old caller did. modelswap.Run pulls internally, so pre-announce when
	// the target is absent — purely cosmetic, no side effect.
	if m, ok := d.ResolveCatalog(name); ok && !d.IsDownloaded(m) {
		if fits, _ := d.Fits(m); fits {
			fmt.Fprintf(out, "pulling %s (not yet downloaded)...\n", m.ID)
		}
	}

	res := modelswap.Run(*d, name)

	switch {
	case res.Unknown:
		fmt.Fprintf(errOut, "model swap: unknown model %q — run `villa model list` to see catalog names\n", name)
		return exitBlocked
	case res.Refused:
		fmt.Fprintf(errOut, "model swap: %s won't fit the usable memory envelope — refusing (%s)\n", res.ToModel, res.Reason)
		return exitBlocked
	case res.Err != nil:
		switch res.FailedStep {
		case "pull":
			fmt.Fprintf(errOut, "model swap: auto-pull %s failed: %v\n", res.ToModel, res.Err)
		case "load config":
			fmt.Fprintf(errOut, "model swap: load config: %v\n", res.Err)
		case "persist config":
			fmt.Fprintf(errOut, "model swap: persist config: %v\n", res.Err)
		case "regenerate units":
			fmt.Fprintf(errOut, "model swap: regenerate units: %v\n", res.Err)
		case "restart":
			fmt.Fprintf(errOut, "model swap: restart %s failed: %v\n", installServiceName, res.Err)
		default:
			fmt.Fprintf(errOut, "model swap: %v\n", res.Err)
		}
		return exitBlocked
	case res.NoOp:
		fmt.Fprintf(out, "swapped to %s — config persisted; units already up to date, no restart needed\n", res.ToModel)
		return exitPass
	default: // Switched
		fmt.Fprintf(out, "swapped to %s — config persisted and %s restarted\n", res.ToModel, installServiceName)
		return exitPass
	}
}

// liveSwapDeps wires `model swap` to the real host: catalog resolution, the
// recommend fit-math, the on-disk weight check, the verified downloader (via the
// pullFn seam), config.SaveVilla, and the Plan-01 orchestrate render/reconcile/write
// + systemd restart seam — exactly the seams the context note names, no new math.
func liveSwapDeps() *modelswap.Deps {
	sys := orchestrate.NewSystemd()
	return &modelswap.Deps{
		InstallServiceName: installServiceName,
		LoadConfig:         config.LoadVilla,
		ResolveCatalog: func(name string) (catalog.CatalogModel, bool) {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return catalog.CatalogModel{}, false
			}
			return cat.FindByID(name)
		},
		Fits: func(m catalog.CatalogModel) (bool, string) {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return false, "catalog load failed"
			}
			// Reuse recommend.Pick fit-math by overriding to the swap target; the
			// override path re-validates the fit against the detected envelope and
			// sets Fits=false when it won't fit (recommend.go:188-192 / D-07).
			// Persisted memory inputs (fail-soft): swap fit re-validation must see
			// the same shrunken envelope the user was recommended (D-01).
			rec := recommend.Pick(detect.Probe(), cat, recommend.Overrides{Model: m.ID}, liveLoadedMemoryInputs())
			if rec.Fits {
				return true, ""
			}
			reason := fmt.Sprintf("needs %d bytes vs %d usable", rec.TotalBytes, rec.UsableEnvelopeBytes)
			return false, reason
		},
		IsDownloaded: func(m catalog.CatalogModel) bool {
			path := filepath.Join(modelsDir(), primaryModelFile(m))
			_, err := os.Stat(path)
			return err == nil
		},
		Pull: func(m catalog.CatalogModel) error {
			dir := modelsDir()
			if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
				return mkErr
			}
			return pullFn(context.Background(), m, dir)
		},
		SaveConfig: config.SaveVilla,
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
		Restart: sys.Restart,
	}
}
