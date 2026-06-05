package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// down.go wires `villa down [service]` (CLI-02): stop the whole stack — or one
// service — WITHOUT removing any unit file (removal is `uninstall`, D-11). It
// renders the stack only to derive the known service set for arg validation
// (T-03-11); it never writes a unit. runDown RETURNS the exit code; the RunE
// wrapper calls os.Exit.

// newDown builds `villa down [service]`: stop the whole stack or one service.
func newDown() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "down [service]",
		Short: "Stop the stack (or one service) without removing units",
		Long: "Stop the running services — the whole stack or just the named one. Units are left in place " +
			"(use `villa uninstall` to remove them). Strictly local.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := runDown(cmd, args, liveLifecycleDeps())
			os.Exit(code)
			return nil
		},
	}
	return cmd
}

// runDown stops the targeted service(s) and RETURNS the exit code. It validates an
// optional [service] arg against the known service set BEFORE any stop fires
// (T-03-11). It never writes or removes a unit file.
func runDown(cmd *cobra.Command, args []string, d *lifecycleDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	units, _, err := d.renderStack()
	if err != nil {
		fmt.Fprintf(errOut, "down: %v\n", err)
		return exitBlocked
	}

	targets, ok := resolveTargets(errOut, args, managedServices(units))
	if !ok {
		return exitBlocked
	}

	for _, svc := range targets {
		if err := d.stop(svc); err != nil {
			fmt.Fprintf(errOut, "down: stop %s failed: %v\n", svc, err)
			return exitBlocked
		}
		fmt.Fprintf(out, "stopped %s\n", svc)
	}
	return exitPass
}
