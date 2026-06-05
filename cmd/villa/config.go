package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// config.go wires `villa config show` and `villa config set key=value` (CLI-05):
// inspect the effective config.toml and edit it through the SINGLE traversal-
// guarded 0600 writer (config.SaveVilla) — never a re-rolled writer (T-03-12).
// `set` validates the key against the known VillaConfig fields and notes that the
// change applies on the next `up`/`restart` (reconcile, CLI-05). All host-touching
// config access is behind the configDeps seam so config_test.go drives the verbs
// without touching the user's real XDG config. runX RETURNS the exit code.

// configDeps are the injectable config-access seams. Defaults wire the real
// config.LoadVilla/SaveVilla/Path; config_test.go replaces them with stubs.
type configDeps struct {
	load func() (config.VillaConfig, error)
	save func(config.VillaConfig) error
	path func() (string, error)
}

// configJSON is the stable lowercase JSON shape for `config show --json` — the
// same key vocabulary as config.toml, decoupled from the Go field names so the
// scripting/dashboard contract is independent of internal struct naming.
type configJSON struct {
	Model       string `json:"model"`
	Quant       string `json:"quant"`
	Ctx         int    `json:"ctx"`
	Backend     string `json:"backend"`
	CatalogPath string `json:"catalog_path"`
}

// liveConfigDeps wires configDeps to the real XDG TOML store.
func liveConfigDeps() *configDeps {
	return &configDeps{
		load: config.LoadVilla,
		save: config.SaveVilla,
		path: config.Path,
	}
}

// newConfig builds the `villa config` noun with `show` and `set` subcommands.
func newConfig() *cobra.Command {
	cfg := &cobra.Command{
		Use:   "config",
		Short: "Show and edit the effective villa configuration",
		Long: "Inspect (show) or edit (set) config.toml — the single source of truth lifecycle commands " +
			"render units from. Edits are written 0600 under the XDG config dir and apply on the next " +
			"`villa up`/`restart` (reconcile). Strictly local.",
		Args: cobra.NoArgs,
	}
	cfg.AddCommand(newConfigShow(), newConfigSet())
	return cfg
}

// newConfigShow builds `villa config show [--json]`.
func newConfigShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration",
		Long:  "Print the effective config.toml (typed defaults when absent) as a table, or as JSON with --json.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			code := runConfigShow(cmd, jsonOut, liveConfigDeps())
			os.Exit(code)
			return nil
		},
	}
}

// newConfigSet builds `villa config set key=value`.
func newConfigSet() *cobra.Command {
	return &cobra.Command{
		Use:   "set key=value",
		Short: "Set a configuration key (applies on next up/restart)",
		Long: "Set a single config key (model, quant, ctx, backend, catalog_path) and persist it via the " +
			"0600 traversal-guarded writer. The change applies on the next `villa up`/`restart` (reconcile).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := runConfigSet(cmd, args[0], liveConfigDeps())
			os.Exit(code)
			return nil
		},
	}
}

// runConfigShow prints the effective config and RETURNS the exit code. --json
// emits the VillaConfig struct; otherwise a readable table.
func runConfigShow(cmd *cobra.Command, asJSON bool, d *configDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	cfg, err := d.load()
	if err != nil {
		fmt.Fprintf(errOut, "config show: %v\n", err)
		return exitBlocked
	}

	if asJSON {
		// Emit a stable lowercase JSON shape (matching the config.toml keys)
		// rather than the Go field names, so the dashboard/scripting contract is
		// the same vocabulary as the file the user edits.
		view := configJSON{
			Model:       cfg.Model,
			Quant:       cfg.Quant,
			Ctx:         cfg.Ctx,
			Backend:     cfg.Backend,
			CatalogPath: cfg.CatalogPath,
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(view); err != nil {
			fmt.Fprintf(errOut, "config show: encode: %v\n", err)
			return exitBlocked
		}
		return exitPass
	}

	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "model\t%s\n", cfg.Model)
	fmt.Fprintf(tw, "quant\t%s\n", cfg.Quant)
	fmt.Fprintf(tw, "ctx\t%d\n", cfg.Ctx)
	fmt.Fprintf(tw, "backend\t%s\n", cfg.Backend)
	fmt.Fprintf(tw, "catalog_path\t%s\n", cfg.CatalogPath)
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(errOut, "config show: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

// runConfigSet parses key=value, validates the key against the known VillaConfig
// fields, applies it to the loaded config, persists via the save seam (reusing
// config.SaveVilla's 0600 + traversal guard — never a re-rolled writer, T-03-12),
// and notes it applies on the next up/restart. It RETURNS the exit code; an
// unknown key / malformed arg / bad value blocks (exit 1) and writes nothing.
func runConfigSet(cmd *cobra.Command, arg string, d *configDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	key, val, ok := strings.Cut(arg, "=")
	if !ok || key == "" {
		fmt.Fprintf(errOut, "config set: expected key=value, got %q\n", arg)
		return exitBlocked
	}
	key = strings.TrimSpace(key)

	cfg, err := d.load()
	if err != nil {
		fmt.Fprintf(errOut, "config set: %v\n", err)
		return exitBlocked
	}

	if code := applyConfigKey(errOut, &cfg, key, val); code != exitPass {
		return code
	}

	if err := d.save(cfg); err != nil {
		fmt.Fprintf(errOut, "config set: %v\n", err)
		return exitBlocked
	}

	dest := ""
	if p, perr := d.path(); perr == nil {
		dest = " (" + p + ")"
	}
	fmt.Fprintf(out, "set %s=%s%s\n", key, val, dest)
	fmt.Fprintf(out, "note: the change applies on the next `villa up` or `villa restart` (reconcile).\n")
	return exitPass
}

// applyConfigKey validates key against the known VillaConfig TOML fields and sets
// the matching field on cfg. An unknown key or an unparseable typed value (ctx is
// an int) is a clear block (exit 1) — it never silently no-ops or writes garbage.
func applyConfigKey(errOut io.Writer, cfg *config.VillaConfig, key, val string) int {
	switch key {
	case "model":
		cfg.Model = val
	case "quant":
		cfg.Quant = val
	case "backend":
		// Only backends the render path actually honors may be persisted, so the
		// key cannot lie (WR-01). Vulkan RADV is the sole v1 backend; ROCm is not
		// wired, so accepting it would silently no-op. Reject unknown values with a
		// clear error rather than writing a setting that has no effect.
		b := strings.TrimSpace(val)
		if b != "vulkan" {
			fmt.Fprintf(errOut, "config set: unsupported backend %q — only \"vulkan\" is supported in v1\n", val)
			return exitBlocked
		}
		cfg.Backend = b
	case "catalog_path":
		cfg.CatalogPath = val
	case "ctx":
		n, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil || n <= 0 {
			fmt.Fprintf(errOut, "config set: ctx must be a positive integer, got %q\n", val)
			return exitBlocked
		}
		cfg.Ctx = n
	default:
		fmt.Fprintf(errOut, "config set: unknown key %q — valid keys: model, quant, ctx, backend, catalog_path\n", key)
		return exitBlocked
	}
	return exitPass
}
