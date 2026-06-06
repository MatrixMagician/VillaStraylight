package main

import "github.com/spf13/cobra"

// Global persistent flags shared by all villa subcommands.
//
//	jsonOut — emit the structured --json contract (the Phase 5 dashboard struct, D-05).
//	verbose — -v/--verbose: show per-value provenance (which tool/sysfs path, D-08).
//	force   — reserved for plan 03's preflight override (D-01/D-03); registered now so
//	          the flag surface is stable from day one.
var (
	jsonOut bool
	verbose bool
	force   bool
)

// newRoot builds the villa cobra command tree: the root command, its persistent
// global flags, and the registered subcommands. Later plans register recommend
// and preflight here alongside detect.
func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "villa",
		Short:         "VillaStraylight — local AI server control plane",
		Long:          "villa detects the host hardware, recommends a fitting model/quant/context, and gates installs behind a preflight check — strictly local, zero telemetry.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.BoolVar(&jsonOut, "json", false, "emit structured JSON output")
	pf.BoolVarP(&verbose, "verbose", "v", false, "show provenance for each detected value")
	pf.BoolVar(&force, "force", false, "override blocking preflight checks (auditable)")

	root.AddCommand(newDetect(), newRecommend(), newPreflight(), newModel(), newInference(), newInstall(),
		newUp(), newDown(), newRestart(), newLogs(), newConfig(), newStatus(), newDashboard(), newBackend(), newUninstall())

	return root
}
