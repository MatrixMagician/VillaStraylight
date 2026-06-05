package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// logs.go wires `villa logs [service] [-f]` (CLI-04): show — and optionally follow
// — a service's rootless user-journal. The non-follow path uses the bounded
// orchestrate.JournalText seam (8 KiB io.LimitReader, T-03-13); `-f` streams via
// the followJournal seam (`journalctl --user -u <svc> -f`, fixed-arg, no shell,
// T-03-11). The service name is validated against the known unit set BEFORE any
// journalctl call. runLogs RETURNS the exit code; the RunE wrapper calls os.Exit.

// logsOpts are the per-invocation flags for `villa logs`.
type logsOpts struct {
	// follow streams new log lines (journalctl -f) until the user interrupts.
	follow bool
}

// newLogs builds `villa logs [service] [-f]`.
func newLogs() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs [service]",
		Short: "Show (and optionally follow) a service's journald logs",
		Long: "Print the rootless user-journal for a service — the inference service by default, or the " +
			"named one. -f/--follow streams new lines until interrupted. The non-follow read is bounded. " +
			"Strictly local.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := runLogs(cmd, logsOpts{follow: follow}, args, liveLifecycleDeps())
			os.Exit(code)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream new log lines until interrupted")
	return cmd
}

// runLogs prints (or follows) a service's journal and RETURNS the exit code. With
// no [service] arg it defaults to the single inference service; with one it
// validates against the known service set (T-03-11) before any journalctl call.
func runLogs(cmd *cobra.Command, opts logsOpts, args []string, d *lifecycleDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	units, _, err := d.renderStack()
	if err != nil {
		fmt.Fprintf(errOut, "logs: %v\n", err)
		return exitBlocked
	}
	services := serviceUnits(units)
	if len(services) == 0 {
		fmt.Fprintf(errOut, "logs: no services in the generated stack\n")
		return exitBlocked
	}

	// Default to the single inference service when no arg is given; otherwise
	// validate the named one. resolveTargets returns the whole set for no-arg, so
	// for logs we narrow a no-arg invocation to the first (only) service.
	var svc string
	if len(args) == 0 {
		svc = services[0]
	} else {
		targets, ok := resolveTargets(errOut, args, services)
		if !ok {
			return exitBlocked
		}
		svc = targets[0]
	}

	if opts.follow {
		if err := d.followJournal(svc); err != nil {
			fmt.Fprintf(errOut, "logs: follow %s failed: %v\n", svc, err)
			return exitBlocked
		}
		return exitPass
	}

	text, found := d.journalText(svc)
	if !found {
		fmt.Fprintf(errOut, "logs: no journal output for %s (is journalctl present and the service known?)\n", svc)
		return exitWarn
	}
	fmt.Fprint(out, text)
	return exitPass
}
