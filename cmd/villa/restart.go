package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
)

// restart.go wires `villa restart [service]`: reconcile config→units
// FIRST (so a hand-edited config.toml is applied on restart) then
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
// against the known service set BEFORE any seam fires.
func runRestart(cmd *cobra.Command, args []string, d *lifecycleDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	cfg, err := d.loadConfig()
	if err != nil {
		fmt.Fprintf(errOut, "restart: load config: %v\n", err)
		return exitBlocked
	}
	units, err := stackapply.Render(d.stack, cfg)
	if err != nil {
		fmt.Fprintf(errOut, "restart: %v\n", err)
		return exitBlocked
	}

	targets, ok := resolveTargets(errOut, args, managedServices(units))
	if !ok {
		return exitBlocked
	}

	// Apply first so a config edit is applied on restart.
	if _, err := d.applyStack(out, cfg); err != nil {
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
