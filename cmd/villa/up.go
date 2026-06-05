package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// up.go wires `villa up [service]` (CLI-02): reconcile config→units and start the
// stack — or only the named service. It reuses the Plan-01 orchestrate core and
// the Plan-02 install reconcile pattern, so an unchanged config is a TRUE no-op
// (D-06) and a hand-edited config.toml converges exactly the changed units on the
// next `up` (D-07/CLI-05). --dry-run prints the rendered changed units and writes
// nothing. runUp RETURNS the exit code; the RunE wrapper calls os.Exit.

// upOpts are the per-invocation flags for `villa up`.
type upOpts struct {
	// dryRun prints the rendered changed units and writes/starts nothing.
	dryRun bool
}

// newUp builds `villa up [service]`: reconcile + start the whole stack (or one
// service), idempotent and --dry-run aware.
func newUp() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "up [service]",
		Short: "Reconcile config into units and start the stack (or one service)",
		Long: "Render Quadlet units from config.toml, write only what changed, daemon-reload, and start " +
			"the stack — or just the named service. A second run with unchanged config is a true no-op. " +
			"--dry-run prints the rendered changes and writes nothing. Strictly local.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := runUp(cmd, upOpts{dryRun: dryRun}, args, liveLifecycleDeps())
			os.Exit(code)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the rendered changes without writing or starting anything")
	return cmd
}

// runUp executes the up flow and RETURNS the exit code (0/2/1) — it never calls
// os.Exit, so tests drive it. It renders the stack, validates an optional
// [service] arg against the known service set BEFORE any seam fires (T-03-11),
// reconciles against disk, and starts the targeted service(s). An empty Changed
// plan is a true no-op (CLI-01/D-06).
func runUp(cmd *cobra.Command, opts upOpts, args []string, d *lifecycleDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	units, unitDir, err := d.renderStack()
	if err != nil {
		fmt.Fprintf(errOut, "up: %v\n", err)
		return exitBlocked
	}

	// Validate the target BEFORE touching disk/systemd so an unknown service fires
	// zero seam calls (T-03-11).
	targets, ok := resolveTargets(errOut, args, managedServices(units))
	if !ok {
		return exitBlocked
	}

	plan, err := d.reconcile(units, unitDir)
	if err != nil {
		fmt.Fprintf(errOut, "up: reconcile failed: %v\n", err)
		return exitBlocked
	}

	if opts.dryRun {
		return printDryRun(out, plan)
	}

	changed, err := d.applyReconcile(out, plan, unitDir)
	if err != nil {
		fmt.Fprintf(errOut, "up: %v\n", err)
		return exitBlocked
	}

	// Unchanged config is a TRUE no-op (CLI-01/D-06): nothing was written or
	// reloaded, so nothing is (re)started — the running stack already matches.
	if !changed {
		fmt.Fprintf(out, "no changes — stack already matches config\n")
		return exitPass
	}

	for _, svc := range targets {
		if err := d.start(svc); err != nil {
			fmt.Fprintf(errOut, "up: start %s failed: %v\n", svc, err)
			return exitBlocked
		}
		fmt.Fprintf(out, "started %s\n", svc)
	}
	return exitPass
}
