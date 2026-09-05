package main

// speculation.go is the cmd-tier `villa speculation` noun: show the persisted
// speculative-decoding mode, or swap it transactionally (ADR-0006).
//
// It drives the SAME internal/backendswap frame `villa backend set` does, through the
// same liveBackendSwapDeps wiring, because a mode change is a re-render and a restart
// of the inference unit and must roll back the same way. No llama-server flag literal
// appears here; the mode is a config value and the flag lives behind the inference
// seam.

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/backendswap"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// newSpeculation builds the `villa speculation` noun and its show/set subcommands.
func newSpeculation() *cobra.Command {
	spec := &cobra.Command{
		Use:   "speculation",
		Short: "Inspect and switch the speculative-decoding mode (off/ngram)",
		Long: "Show the persisted speculation mode or switch it with a transactional cutover: `set <mode>` " +
			"re-renders ONLY the villa-llama unit, refuses-with-remediation when the served model has no " +
			"qualified measurement for the requested mode, and rolls back verbatim if the new unit does not " +
			"prove healthy. --dry-run previews the target and the fit without mutating anything.",
		Args: cobra.NoArgs,
	}
	spec.AddCommand(newSpeculationShow(), newSpeculationSet())
	return spec
}

// newSpeculationShow builds `villa speculation show [--json]`.
func newSpeculationShow() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the persisted speculation mode",
		Long: "Print the speculation mode read from config.toml (the source of truth). An unset key " +
			"reports off, because an unset config renders speculation off. --json emits the " +
			"machine-readable form.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(runSpeculationShow(cmd, asJSON))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the speculation mode as JSON")
	return cmd
}

// speculationShowEntry is the `speculation show --json` shape.
type speculationShowEntry struct {
	Speculation string `json:"speculation"`
}

// runSpeculationShow loads config and renders the persisted mode, RETURNING the exit
// code (no os.Exit) so tests assert output + code.
func runSpeculationShow(cmd *cobra.Command, asJSON bool) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	cfg, err := config.LoadVilla()
	if err != nil {
		fmt.Fprintf(errOut, "speculation show: load config: %v\n", err)
		return exitBlocked
	}
	mode := cfg.Speculation
	if mode == "" {
		mode = config.SpeculationOff
	}

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(speculationShowEntry{Speculation: mode}); err != nil {
			fmt.Fprintf(errOut, "speculation show: encode json: %v\n", err)
			return exitBlocked
		}
		return exitPass
	}
	fmt.Fprintf(out, "%-12s %s\n", "speculation", mode)
	return exitPass
}

// newSpeculationSet builds `villa speculation set <off|ngram> [--dry-run]`.
func newSpeculationSet() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "set <mode>",
		Short: "Switch the speculation mode transactionally (capture → mutate → prove → rollback)",
		Long: "Switch the speculative-decoding mode (off, or ngram for llama-server's ngram-mod) on the " +
			"running install: re-check the served model against the target mode (refuse-with-remediation " +
			"when it carries no qualified measurement), capture the prior unit verbatim, persist config + " +
			"regenerate ONLY the villa-llama unit + restart it, and PROVE the cutover. Any mutate error or " +
			"a non-pass proof rolls back verbatim. Exits 0 on switch/no-op, 1 on refusal/error/rollback.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runSpeculationSet(cmd, args[0], dryRun, liveBackendSwapDeps()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview target/fit without persisting, regenerating, or restarting anything")
	return cmd
}

// runSpeculationSet validates the mode, then previews or performs the transactional
// switch, RETURNING the exit code. The argument is checked against the config
// vocabulary BEFORE any dep is touched: an empty or unknown mode is a typo, and a
// typo must not reach the host. "" is rejected here specifically because
// config.ValidSpeculation accepts it as "unresolved", which is not a target.
func runSpeculationSet(cmd *cobra.Command, target string, dryRun bool, d *backendswap.Deps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	if target == "" || !config.ValidSpeculation(target) {
		fmt.Fprintf(errOut, "speculation set: %q is not a known mode — use %q or %q\n",
			target, config.SpeculationOff, config.SpeculationNgram)
		return exitBlocked
	}

	if dryRun {
		cfg, err := d.LoadConfig()
		if err != nil {
			fmt.Fprintf(errOut, "speculation set: load config: %v\n", err)
			return exitBlocked
		}
		from := cfg.Speculation
		if from == "" {
			from = config.SpeculationOff
		}
		fmt.Fprintf(out, "dry-run: would switch speculation %s -> %s (model %q preserved)\n", from, target, cfg.Model)

		fitCfg := cfg
		fitCfg.Speculation = target
		if ok, reason := d.FitsModel(fitCfg); ok {
			fmt.Fprintf(out, "  fit:       PASS — %q supports %s\n", cfg.Model, target)
		} else {
			fmt.Fprintf(out, "  fit:       FAIL — %s\n", reason)
		}
		fmt.Fprintf(out, "dry-run: nothing written (no config persisted, no units regenerated, no restart)\n")
		return exitPass
	}

	res := backendswap.RunSpeculation(*d, target)
	switch {
	case res.Refused:
		if res.Reason != "" {
			fmt.Fprintf(errOut, "speculation set: refusing to switch to %s — %s\n", target, res.Reason)
		} else if res.Err != nil {
			fmt.Fprintf(errOut, "speculation set: refusing to switch to %s — %s failed: %v\n", target, res.FailedStep, res.Err)
		} else {
			fmt.Fprintf(errOut, "speculation set: refusing to switch to %s\n", target)
		}
		return exitBlocked
	case res.RolledBack:
		fmt.Fprintf(errOut, "speculation set: switch to %s failed at %q — rolled back; prior mode (%s) restored\n",
			target, res.FailedStep, res.From)
		if res.Reason != "" {
			fmt.Fprintf(errOut, "  detail: %s\n", res.Reason)
		}
		if res.Err != nil {
			fmt.Fprintf(errOut, "  error:  %v\n", res.Err)
		}
		return exitBlocked
	case res.Err != nil:
		fmt.Fprintf(errOut, "speculation set: switch to %s failed at %q: %v\n", target, res.FailedStep, res.Err)
		return exitBlocked
	case res.NoOp:
		fmt.Fprintf(out, "already on %s — no change\n", target)
		return exitPass
	default:
		fmt.Fprintf(out, "switched speculation %s -> %s — config persisted, %s regenerated and restarted, cutover proven\n",
			res.From, res.To, installServiceName)
		return exitPass
	}
}
