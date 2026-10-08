package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/download"
	"github.com/MatrixMagician/VillaStraylight/internal/modelswap"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// pullFn is the downloader seam. It defaults to download.PullModel and is
// overridden in tests so the success path can be exercised without live network.
var pullFn = download.PullModel

// catalogPath is reserved for a future --catalog override; for now `model pull`
// reads the embedded seed (externalPath ""). Kept as a seam so the verb's catalog
// source is in one place.
var modelCatalogPath string

// newModel builds the `villa model` noun and its subcommands. The noun name is chosen
// to NOT collide with the Phase-3 lifecycle verbs (up/down/restart/install/status).
func newModel() *cobra.Command {
	model := &cobra.Command{
		Use:   "model",
		Short: "Acquire and inspect local model weights",
		Long:  "Manage the GGUF model weights villa downloads into the local models dir. Strictly local; the only outbound traffic is the model pull itself.",
		Args:  cobra.NoArgs,
	}
	model.AddCommand(newModelPull(), newModelList(), newModelSwap(), newModelResident())
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
	// path (V5 / command-injection + path-traversal guard).
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

	// Use the command's context, not a fresh Background: main runs the tree under a
	// SIGINT/SIGTERM-cancelled context, and this is the longest-running command in
	// it (a multi-GB weight download). With Background, Ctrl-C could not interrupt
	// the transfer.
	//
	// Cancelling mid-transfer is safe and resumable: downloadFile leaves the
	// partially-written ".part" file in place on a stream error and seeds the hash
	// from it on the next run, so an interrupted pull continues via HTTP Range
	// rather than restarting. Only a size/checksum MISMATCH deletes the partial.
	if dlErr := pullFn(cmdContext(cmd), m, modelsDir); dlErr != nil {
		fmt.Fprintf(errOut, "model pull: %s failed: %v\n", m.ID, dlErr)
		return exitBlocked
	}

	// AllShards, not Shards: the projector was pulled and verified too, so the
	// count and the byte total must say so or the line under-reports the download.
	all := m.AllShards()
	var totalBytes uint64
	for _, s := range all {
		totalBytes += s.SizeBytes
	}
	if len(all) == 1 {
		fmt.Fprintf(out, "pulled %s -> %s (%d bytes, verified)\n", m.ID, filepath.Join(modelsDir, all[0].Filename), totalBytes)
	} else {
		fmt.Fprintf(out, "pulled %s -> %s (%d shards, %d bytes, verified)\n", m.ID, modelsDir, len(all), totalBytes)
	}
	return exitPass
}

// modelsDir resolves the on-disk models directory: $XDG_DATA_HOME/villa/models
// (default ~/.local/share/villa/models). Downloaded weights live here
// and Phase 3 bind-mounts the dir read-only into the inference container.
func modelsDir() string {
	return filepath.Join(pathsafe.DataRoot(), "models")
}

// ---------------------------------------------------------------------------
// model list (MODEL-01): distinguish available (catalog) from loaded
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
// "available", with the config'd model marked "loaded". --json supported.
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
// model swap (MODEL-03): fit-guard → auto-pull → persist-config-FIRST →
// regenerate-and-restart-inference-only. Reuses recommend.Pick (fit-math),
// download.PullModel (verified pull), config.SaveVilla (source of truth), and the
// Plan-01 orchestrate render/reconcile/write + systemd seam — no new envelope
// math, no new downloader.
// ---------------------------------------------------------------------------

// newModelSwap builds `villa model swap <name>`: refuse a non-fitting target,
// auto-pull if absent, persist config FIRST, then regenerate + restart only the
// inference unit (MODEL-03).
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
			code := runModelSwap(cmd, args[0], liveSwapDeps(cmdContext(cmd)))
			os.Exit(code)
			return nil
		},
	}
}

// runModelSwap performs the swap and RETURNS the exit code. The ordering-is-the-
// security-contract sequence lives in modelswap.Run; this caller maps the
// typed modelswap.Result to the human messages it used to print and to exit codes
// (refuse/err→1, switched/no-op→0).
func runModelSwap(cmd *cobra.Command, name string, d *modelswap.Deps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	// The swap announces a pull from inside, after its fit guard, so the "pulling"
	// line names a download that is actually starting.
	deps := *d
	deps.OnPull = func(m catalog.Model) { fmt.Fprintf(out, "pulling %s (not yet downloaded)...\n", m.ID) }

	// The transaction frame holds the stack lock (ADR-0010) throughout.
	res := modelswap.Run(deps, name)

	switch {
	case res.Unknown:
		fmt.Fprintf(errOut, "model swap: unknown model %q — run `villa model list` to see catalog names\n", name)
		return exitBlocked
	case res.Refused && res.Err != nil:
		fmt.Fprintf(errOut, "model swap: refusing — %s failed: %v\n", res.FailedStep, res.Err)
		return exitBlocked
	case res.Refused && res.OverEnvelope:
		fmt.Fprintf(errOut, "model swap: %s won't fit the usable memory envelope — refusing (%s)\n", res.ToModel, res.Reason)
		return exitBlocked
	case res.Refused:
		fmt.Fprintf(errOut, "model swap: refusing — %s\n", res.Reason)
		return exitBlocked
	case res.RolledBack:
		// A mutate error or a failed proof restored the prior model; Reason carries
		// the proof detail or the rollback-incomplete note.
		fmt.Fprintf(errOut, "model swap: swap to %s failed at %q — rolled back; %s restored\n", res.ToModel, res.FailedStep, res.FromModel)
		if res.Reason != "" {
			fmt.Fprintf(errOut, "  detail: %s\n", res.Reason)
		}
		if res.Err != nil {
			fmt.Fprintf(errOut, "  error:  %v\n", res.Err)
		}
		return exitBlocked
	case res.Err != nil:
		switch res.FailedStep {
		case "pull":
			fmt.Fprintf(errOut, "model swap: auto-pull %s failed: %v\n", res.ToModel, res.Err)
		case "load config":
			fmt.Fprintf(errOut, "model swap: load config: %v\n", res.Err)
		default:
			fmt.Fprintf(errOut, "model swap: %v\n", res.Err)
		}
		return exitBlocked
	case res.NoOp:
		fmt.Fprintf(out, "swapped to %s — config persisted; units already up to date, no restart needed\n", res.ToModel)
		printSwapChanges(out, res)
		return exitPass
	default: // Switched
		fmt.Fprintf(out, "swapped to %s — config persisted and %s restarted\n", res.ToModel, installServiceName)
		printSwapChanges(out, res)
		return exitPass
	}
}

// printSwapChanges names what the swap changed besides the model: a ctx it reset to
// the target's default (#301) and a vision change (#299). The operator asked for a
// model, not for either, so a silent change would hide a persisted decision
// changing under them.
func printSwapChanges(out io.Writer, res modelswap.Result) {
	if res.ToCtx != res.FromCtx {
		fmt.Fprintf(out, "ctx reset to %d: %d does not fit %s\n", res.ToCtx, res.FromCtx, res.ToModel)
	}
	switch {
	case res.FromVision && !res.ToVision:
		fmt.Fprintf(out, "vision turned off: %s has no projector that fits beside it\n", res.ToModel)
	case !res.FromVision && res.ToVision:
		fmt.Fprintf(out, "vision turned on: %s ships a projector\n", res.ToModel)
	}
}

// swapFit sizes a swap target the way the render serves cfg (#301), through
// recommend.Pick's override path, never new envelope math: at cfg.Ctx (unset means
// the entry's default_ctx), floored at the entry's agent ctx in tools mode the way
// livePinnedRender floors it, and with the persisted speculation mode, unset being
// off as liveSpeculation renders it. Shared-residency coding mode serves the chat
// model itself at the coder agent ctx, with its own qualification picking the mode
// when one is persisted at all (stackapply.ServedTarget, liveSpeculation), so it is
// sized that way. With a
// separate coder the chat model is not served until coding mode exits, so it is
// sized for that render. `model swap` and the dashboard's fit column both fold it
// through modelswap.Size.
func swapFit(profile detect.HostProfile, cat catalog.Catalog) func(catalog.Model, config.VillaConfig) modelswap.Fit {
	return func(m catalog.Model, cfg config.VillaConfig) modelswap.Fit {
		ctx := cmp.Or(cfg.Ctx, m.DefaultCtx)
		spec := cmp.Or(cfg.Speculation, config.SpeculationOff)
		switch {
		case subsystem.CodingModeOn(cfg) && cfg.CoderModel == "":
			ctx = cmp.Or(cfg.CoderAgentCtx, ctx)
			if spec != config.SpeculationOff {
				spec = ""
			}
		case subsystem.ToolsOn(cfg) && m.AgentCtx > ctx:
			ctx = m.AgentCtx
		}
		ov := recommend.Overrides{Model: m.ID, Ctx: ctx, Speculation: spec}
		rec := recommend.Pick(profile, cat, ov, recommend.ReservationsFor(cfg))
		fit := modelswap.Fit{OK: rec.Fits, Vision: rec.Vision, Detail: fitDetail(rec)}
		if rec.Fits {
			return fit
		}
		if rec.TotalBytes > rec.UsableEnvelopeBytes {
			fit.OverEnvelope = true
			return fit
		}
		// Within the envelope and still not a fit: the speculation mode was refused. A
		// draft that was asked for and ships is refused only for not fitting beside the
		// model, a shortfall a smaller ctx can cure since its KV shrinks with the ctx;
		// the first speculation note then names the shortfall.
		fit.OverEnvelope = spec == config.SpeculationDraft && m.Draft != nil
		for _, n := range rec.Notes {
			if strings.HasPrefix(n, "speculation:") {
				fit.Detail = n
				break
			}
		}
		return fit
	}
}

// liveSwapDeps wires `model swap` to the real host — catalog resolution, the
// recommend fit-math, the on-disk weight check and the verified downloader (via the
// pullFn seam) — over the live transaction frame, whose lock blocks and whose proof
// is liveProve. The dashboard runs the same deps with a non-blocking lock.
//
// ctx is the command's SIGINT/SIGTERM-cancelled context, captured by the Pull
// closure. A swap pull is multi-GB, so without it Ctrl-C could not interrupt the
// transfer. Cancelling mid-stream is safe: download.PullModel keeps the partial
// ".part" file and resumes it via HTTP Range on the next run.
func liveSwapDeps(ctx context.Context) *modelswap.Deps {
	return &modelswap.Deps{
		Tx: liveTxDeps(acquireStackLock),
		ResolveCatalog: func(name string) (catalog.Model, bool) {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return catalog.Model{}, false
			}
			return cat.FindByID(name)
		},
		Fits: func(m catalog.Model, cfg config.VillaConfig) modelswap.Fit {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return modelswap.Fit{Detail: "catalog load failed"}
			}
			// The reservations come from the cfg being swapped (ADR-0027), so the
			// fit sees the same shrunken envelope the user was recommended.
			return swapFit(detect.Probe(), cat)(m, cfg)
		},
		IsDownloaded: modelOnDisk,
		Pull: func(m catalog.Model) error {
			dir := modelsDir()
			if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
				return mkErr
			}
			return pullFn(ctx, m, dir)
		},
	}
}
