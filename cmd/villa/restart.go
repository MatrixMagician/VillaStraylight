package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// restart.go wires `villa restart [service]` (CLI-02): reconcile config→units
// FIRST (so a hand-edited config.toml is applied on restart, D-07/CLI-05) then
// restart the whole stack — or one service. runRestart RETURNS the exit code; the
// RunE wrapper calls os.Exit.

// newRestart builds `villa restart [service]`: reconcile-then-restart the whole
// stack or one service.
func newRestart() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart [service]",
		Short: "Reconcile config and restart the stack (or one service)",
		Long: "Re-render units from config.toml and write any changes (so a config edit is applied), then " +
			"restart the stack — or just the named service. Strictly local.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := runRestart(cmd, args, liveLifecycleDeps())
			os.Exit(code)
			return nil
		},
	}
	return cmd
}

// runRestart reconciles (applies any config edit) then restarts the targeted
// service(s) and RETURNS the exit code. It validates an optional [service] arg
// against the known service set BEFORE any seam fires (T-03-11).
func runRestart(cmd *cobra.Command, args []string, d *lifecycleDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	units, unitDir, err := d.renderStack()
	if err != nil {
		fmt.Fprintf(errOut, "restart: %v\n", err)
		return exitBlocked
	}

	targets, ok := resolveTargets(errOut, args, managedServices(units))
	if !ok {
		return exitBlocked
	}

	// Reconcile first so a config edit is applied on restart (D-07/CLI-05).
	plan, err := d.reconcile(units, unitDir)
	if err != nil {
		fmt.Fprintf(errOut, "restart: reconcile failed: %v\n", err)
		return exitBlocked
	}
	if _, err := d.applyReconcile(out, plan, unitDir); err != nil {
		fmt.Fprintf(errOut, "restart: %v\n", err)
		return exitBlocked
	}

	for _, svc := range targets {
		if err := d.restart(svc); err != nil {
			fmt.Fprintf(errOut, "restart: restart %s failed: %v\n", svc, err)
			return exitBlocked
		}
		fmt.Fprintf(out, "restarted %s\n", svc)
	}
	return exitPass
}
