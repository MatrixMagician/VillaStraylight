package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// Exit codes for `villa preflight` (D-04). These are the scriptable contract that
// Phase 3 install and a future `villa doctor` branch on, so they are named.
const (
	exitPass    = 0 // all checks pass
	exitWarn    = 2 // passed with warnings (or an overridden block)
	exitBlocked = 1 // an un-overridden BLOCK check failed
)

// newPreflight builds `villa preflight`: probe the host, run the reusable
// preflight package, render the tiered results with remediation, and map them to
// an exit code (0/2/1). With the global --force flag, BLOCK failures are
// downgraded to "overridden", an auditable override summary is printed, and the
// command exits 0 (D-01/D-03). The exit-code mapping lives ENTIRELY here — the
// internal/preflight package never exits or prints (D-18).
func newPreflight() *cobra.Command {
	// backend is a LOCAL preflight flag (not a persistent root flag): when set to
	// "rocm" it routes the gate to the reusable preflight.RunROCm verdict the
	// Phase 8 `backend set` verb consumes, instead of the standalone host preflight.
	var backend string
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Check whether this host is ready to install the AI stack",
		Long: "Run the host-prep gate: Vulkan ICD + iGPU enumeration, Podman rootless readiness, " +
			"user lingering, and free disk/memory — classified BLOCK vs WARN. Exits 0 (pass), " +
			"2 (warnings), or 1 (a BLOCK check failed). --force overrides BLOCK failures and prints " +
			"an auditable summary of exactly what was bypassed. With --backend rocm it gates ROCm " +
			"bring-up instead (refuse-with-remediation on a confident known-bad host). Read-only.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := detect.Probe()
			var results []preflight.CheckResult
			if backend == "rocm" {
				// ROCm bring-up gate: refuse only confident known-bad hosts; off-hardware
				// every signal is Unknown → WARN → exit 2 (never a false exit 1).
				results = preflight.RunROCm(profile)
			} else {
				// Standalone host preflight — WARN-only, behaviorally unchanged (D-03).
				results = preflight.Run(profile)
			}
			code := renderPreflight(cmd.OutOrStdout(), results, jsonOut, verbose, force)
			os.Exit(code)
			return nil
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "", "gate ROCm bring-up instead of the standalone host preflight (rocm)")
	return cmd
}

// renderPreflight writes the results and RETURNS the exit code (it does not call
// os.Exit) so tests can assert both the rendered output and the mapped code
// without spawning a subprocess. It is the single place that interprets tiers as
// exit codes and applies the --force override.
func renderPreflight(w io.Writer, results []preflight.CheckResult, asJSON, withProvenance, forced bool) int {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(results)
	} else {
		renderPreflightTable(w, results, withProvenance)
	}

	// Classify: collect the BLOCK failures and whether any WARN is present.
	var blockFails []preflight.CheckResult
	anyWarn := false
	for _, r := range results {
		switch r.Status {
		case preflight.StatusFail:
			if r.Tier == preflight.TierBlock {
				blockFails = append(blockFails, r)
			}
		case preflight.StatusWarn:
			anyWarn = true
		}
	}

	if len(blockFails) > 0 {
		if forced {
			// --force: downgrade the blocks to "overridden", print exactly which
			// blocks were bypassed (auditable, D-01/D-03), and pass with code 2 to
			// still signal "not clean" rather than 0.
			printOverrideSummary(w, blockFails)
			return exitWarn
		}
		// Un-overridden BLOCK failure → blocked.
		fmt.Fprintf(w, "\nBLOCKED: %d blocking check(s) failed. Re-run with --force to override (auditable).\n", len(blockFails))
		return exitBlocked
	}

	if anyWarn {
		return exitWarn
	}
	return exitPass
}

// printOverrideSummary prints the auditable list of BLOCK checks bypassed by
// --force (D-01/D-03). It is the record of exactly what was overridden.
func printOverrideSummary(w io.Writer, blockFails []preflight.CheckResult) {
	fmt.Fprintf(w, "\nOverridden BLOCK checks (--force): %d bypassed\n", len(blockFails))
	for _, r := range blockFails {
		fmt.Fprintf(w, "  - [%s] %s: %s\n", r.ID, r.Name, r.Detail)
	}
	fmt.Fprintln(w, "Proceeding despite blocking failures — you accepted the risk.")
}

// renderPreflightTable writes the tiered check results as an aligned table. With
// provenance, a trailing column shows which tool/path produced each result.
func renderPreflightTable(w io.Writer, results []preflight.CheckResult, withProvenance bool) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, r := range results {
		detail := r.Detail
		if r.Status != preflight.StatusPass && r.Remediation != "" {
			detail = detail + " — " + r.Remediation
		}
		if withProvenance {
			prov := r.Provenance
			if r.Raw != "" {
				prov = prov + " | raw: " + r.Raw
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t(%s)\n", r.ID, r.Tier, r.Status, detail, prov)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ID, r.Tier, r.Status, detail)
		}
	}
	_ = tw.Flush()
}
