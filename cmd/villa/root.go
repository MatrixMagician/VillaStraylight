package main

import (
	"context"

	"github.com/spf13/cobra"
)

// cmdContext returns the command's context, falling back to Background when it is
// nil.
//
// cobra leaves Context() nil on a Command that was never Execute()d, which is how
// the cmd-tier tests construct one. The live path (main's ExecuteContext) always
// supplies the real SIGINT/SIGTERM-cancelled context, so this fallback only ever
// applies to a directly-invoked run body.
func cmdContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// Global persistent flags shared by all villa subcommands.
//
// jsonOut — emit the structured --json contract (the Phase 5 dashboard struct).
// verbose — -v/--verbose: show per-value provenance (which tool/sysfs path).
// force — reserved for plan 03's preflight override; registered now so
//
//	the flag surface is stable from day one.
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
		Version:       villaVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.BoolVar(&jsonOut, "json", false, "emit structured JSON output")
	pf.BoolVarP(&verbose, "verbose", "v", false, "show provenance for each detected value")
	pf.BoolVar(&force, "force", false, "override blocking preflight checks (auditable)")

	root.AddCommand(newDetect(), newRecommend(), newPreflight(), newModel(), newInference(), newInstall(),
		newUp(), newDown(), newRestart(), newLogs(), newConfig(), newStatus(), newDoctor(), newVerify(), newRecall(), newDashboard(), newWebsafe(), newBackend(), newSpeculation(), newCodingMode(), newCode(), newBench(), newBackup(), newRestore(), newUninstall(), newUpdate())

	return root
}
