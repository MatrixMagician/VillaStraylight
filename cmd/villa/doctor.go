package main

// doctor.go is the thin cobra caller for the read-only `villa doctor` health-diagnosis
// verb (DOCTOR-01/02/03): the running-install twin of `villa preflight`. The worst-wins
// decision logic — composing the preflight host-prep gate, the status read-model + its
// per-service offload Verdict, and an orchestrate.Reconcile config-vs-disk drift Plan —
// lives in the pure internal/doctor core (Plan 01). This file keeps ONLY: the cobra
// wiring + exit-code mapping (reusing the AUTHORITATIVE preflight constants), the human
// table renderer, and the live host wiring (liveDoctorDeps) that constructs doctor.Deps.
//
// doctor is strictly READ-ONLY (D-03): it mutates nothing. Note unitDirReadOnly — the
// quadletUnitDir twin that drops the directory-creation step — so a diagnosis never
// creates the Quadlet dir (Pitfall 2). There is no --force and no generation probe (D-07). No backend marker
// literal appears here (TestSeamGrepGate walks cmd/villa); ROCm is routed only via the
// core's inference.IsROCmFamily and resolved via inference.BackendFor.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/doctor"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// newDoctor builds `villa doctor`: a read-only, one-shot health diagnosis of the RUNNING
// install. It composes the pure doctor core over live host seams and maps the worst-wins
// Report to an exit code mirroring `villa preflight`: 0 (healthy), 2 (warnings/drift), or
// 1 (a blocking fault — e.g. a confident CPU fallback). It mutates nothing (D-03): no
// --force, no unit-dir creation, no generation probe. The exit-code mapping lives ENTIRELY
// here (return-not-Exit verb body; cobra RunE calls os.Exit).
func newDoctor() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the health of the running install: host conditions + service health + GPU-offload proof + config-vs-disk drift",
		Long: "Run a read-only, one-shot health diagnosis of the RUNNING stack: re-check the host-prep " +
			"conditions, fold each service's /health and running GPU-offload Verdict (residency proven, " +
			"never a false-green over a health-200), and detect config-vs-disk Quadlet drift. Every " +
			"non-healthy finding carries an actionable remediation. Exits 0 (healthy), 2 (warnings or " +
			"drift), or 1 (a blocking fault such as a confident CPU fallback). Mutates nothing — no " +
			"unit files are written or created.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps, err := liveDoctorDeps()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "doctor: %v\n", err)
				os.Exit(exitBlocked)
			}
			os.Exit(runDoctor(cmd, args, deps))
			return nil
		},
	}
}

// runDoctor builds the Report from the injected core and renders it. It RETURNS the exit
// code (no os.Exit) so doctor_test.go drives it deterministically. All printing + exit
// mapping lives here; the worst-wins fold is doctor.Aggregate.
func runDoctor(cmd *cobra.Command, _ []string, deps doctor.Deps) int {
	report := doctor.Aggregate(deps)
	return renderDoctor(cmd.OutOrStdout(), report, jsonOut, verbose)
}

// renderDoctor writes the report and RETURNS the exit code (it does not call os.Exit) so
// tests can assert both the rendered output and the mapped code without spawning a
// subprocess. It mirrors renderPreflight EXACTLY and is the single place that interprets
// the doctor findings as exit codes.
//
// CRITICAL (D-04 / Pitfall 1 — the shipped preflight constants are AUTHORITATIVE, NOT the
// inverted ROADMAP prose): a confident BLOCK-class FAIL → exitBlocked (=1); any WARN /
// drift / typed-Unknown → exitWarn (=2); all healthy → exitPass (=0). Do NOT invert.
func renderDoctor(w io.Writer, r doctor.Report, asJSON, withProvenance bool) int {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r)
	} else {
		renderDoctorTable(w, r, withProvenance)
	}

	// Classify worst-wins over the findings: collect the BLOCK-class FAILs and whether
	// any WARN is present. A confident FAIL dominates (exitBlocked); else any WARN →
	// exitWarn; else exitPass.
	var blockFails int
	anyWarn := false
	for _, f := range r.Findings {
		switch f.Status {
		case "FAIL":
			blockFails++
		case "WARN":
			anyWarn = true
		}
	}

	if blockFails > 0 {
		fmt.Fprintf(w, "\nFAULT: %d blocking finding(s) — the running install is not healthy. See the remediation(s) above.\n", blockFails)
		return exitBlocked
	}
	if anyWarn {
		return exitWarn
	}
	return exitPass
}

// renderDoctorTable writes the findings as an aligned human table (mirroring
// renderPreflightTable): the overall verdict, then one row per finding
// (ID/Tier/Status/Detail), appending " — Remediation" to the detail cell on any non-PASS
// finding. With provenance, a trailing column shows which composed core produced it.
func renderDoctorTable(w io.Writer, r doctor.Report, withProvenance bool) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "overall\t%s\n\n", r.Overall)
	for _, f := range r.Findings {
		detail := f.Detail
		if f.Status != "PASS" && f.Remediation != "" {
			detail = detail + " — " + f.Remediation
		}
		if withProvenance {
			prov := f.Provenance
			if f.Raw != "" {
				prov = prov + " | raw: " + f.Raw
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t(%s)\n", f.ID, f.Tier, f.Status, detail, prov)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.ID, f.Tier, f.Status, detail)
		}
	}
	_ = tw.Flush()
}

// unitDirReadOnly is the READ-ONLY twin of quadletUnitDir: the same fixed rootless
// Quadlet generator directory (~/.config/containers/systemd) but without the
// directory-creation step — doctor never creates it (Pitfall 2 / D-03). If the dir is absent, the drift read
// fails and the core degrades it to a typed-Unknown WARN (D-08), so resolving the path is
// all this needs to do.
func unitDirReadOnly() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "containers", "systemd"), nil
}

// liveDoctorDeps wires doctor.Deps to the real host. It REUSES liveStatusDeps wholesale
// for the running-stack read-model (no re-wired HTTP/journald/GTT probes — RESEARCH A1)
// and constructs a DriftPlan closure that renders units from config and Reconciles them
// against the on-disk unit dir, returning the Plan WITHOUT ever writing (no WriteUnits).
// It is replaced wholesale by stubbed doctor.Report fixtures in doctor_test.go.
func liveDoctorDeps() (doctor.Deps, error) {
	sd, err := liveStatusDeps()
	if err != nil {
		return doctor.Deps{}, err
	}
	cfg, err := config.LoadVilla()
	if err != nil {
		return doctor.Deps{}, fmt.Errorf("load config: %w", err)
	}
	return doctor.Deps{
		Probe:        detect.Probe,
		LoadConfig:   config.LoadVilla,
		StatusReport: func() status.Report { return status.Run(*sd) },
		Backend:      cfg.Backend,
		// DriftPlan: render units from the persisted config, resolve the backend
		// fail-closed (D-02), and Reconcile against the READ-ONLY unit dir. It NEVER
		// writes. A read error (absent/unreadable unit dir) is returned verbatim so the
		// core degrades it to a typed-Unknown WARN (D-08) rather than swallowing it.
		DriftPlan: func() (orchestrate.Plan, error) {
			c, err := config.LoadVilla()
			if err != nil {
				return orchestrate.Plan{}, fmt.Errorf("load config: %w", err)
			}
			backend, err := inference.BackendFor(c.Backend)
			if err != nil {
				return orchestrate.Plan{}, fmt.Errorf("resolve backend: %w", err)
			}
			modelFile, err := liveModelFile(c)
			if err != nil {
				return orchestrate.Plan{}, fmt.Errorf("resolve model file: %w", err)
			}
			units, err := orchestrate.Render(orchestrate.RenderInput{
				Backend:   backend,
				Cfg:       c,
				ModelFile: modelFile,
				ModelsDir: modelsDir(),
			})
			if err != nil {
				return orchestrate.Plan{}, fmt.Errorf("render units: %w", err)
			}
			dir, err := unitDirReadOnly()
			if err != nil {
				return orchestrate.Plan{}, fmt.Errorf("resolve unit dir: %w", err)
			}
			return orchestrate.Reconcile(units, dir)
		},
	}, nil
}
