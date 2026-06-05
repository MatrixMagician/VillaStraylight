package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// inference.go wires the user-facing close of the Phase-2 slice: the `inference`
// noun with `run` and `validate` subcommands. The noun is `inference` (D-13) so it
// reads distinctly from the Phase-3 lifecycle verbs (up/down/restart/install/status)
// and reuses the preflight exit-code contract (0 pass / 2 warn / 1 fail) + the
// global --json flag. The Verdict→exit mapping + table/JSON rendering live ENTIRELY
// here; internal/inference.Validate stays a pure library (no os.Exit/print).

// validateFn is the validation seam. It defaults to runValidation (which wires the
// real container Runner + live sysfs reads) and is overridden in tests so the verb's
// success path is exercisable without a live iGPU.
var validateFn = runValidation

// newInference builds the `villa inference` noun and its run/validate subcommands.
func newInference() *cobra.Command {
	inf := &cobra.Command{
		Use:   "inference",
		Short: "Run and validate local llama-server inference on the detected GPU",
		Long: "Start the recommended model in a rootless container and prove the iGPU offload " +
			"engaged. `validate` additionally runs a real chat completion and a near-max-context " +
			"stress probe, printing a pass/warn/fail verdict. Strictly local; the API binds loopback only.",
		Args: cobra.NoArgs,
	}
	inf.AddCommand(newInferenceRun(), newInferenceValidate())
	return inf
}

// newInferenceRun builds `villa inference run <name>`: start + readiness + offload
// assert + chat, WITHOUT the near-ceiling stress probe (the lighter run path).
func newInferenceRun() *cobra.Command {
	return &cobra.Command{
		Use:   "run <name>",
		Short: "Run a model and assert GPU offload + a chat completion (no ceiling probe)",
		Long: "Resolve <name> through the catalog, start llama-server in a Vulkan-RADV container, " +
			"poll readiness, assert the dual GPU-offload proof, and run a real chat completion. " +
			"Exits 0 (offload proven + chat OK), 2 (offload unverifiable), or 1 (CPU fallback).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runInference(cmd, args[0], false))
			return nil
		},
	}
}

// newInferenceValidate builds `villa inference validate <name>`: the full slice —
// run + offload assert + chat + the near-max-context ceiling probe.
func newInferenceValidate() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <name>",
		Short: "Validate a model end-to-end: offload proof + chat + near-max-context ceiling probe",
		Long: "The full Phase-2 validation: resolve <name>, run llama-server in a Vulkan-RADV " +
			"container, assert the dual GPU-offload proof, run a real chat completion, and push a " +
			"second run toward the context ceiling to surface OOM/long-context cliffs at validation " +
			"time. Exits 0 (pass), 2 (warn — unverifiable signal or a ceiling cliff), or 1 (CPU fallback).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runInference(cmd, args[0], true))
			return nil
		},
	}
}

// runInference resolves the catalog name, runs Validate via the seam, and maps the
// Verdict to an exit code. It RETURNS the code (no os.Exit) so tests can drive it.
// withCeiling toggles the near-max-context stress probe (validate=true, run=false).
func runInference(cmd *cobra.Command, name string, withCeiling bool) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	cat, warnings, err := catalog.Load(modelCatalogPath)
	if err != nil {
		fmt.Fprintf(errOut, "inference: catalog load failed: %v\n", err)
		return exitBlocked
	}
	for _, w := range warnings {
		fmt.Fprintf(errOut, "warning: %s\n", w)
	}

	// Resolve the name THROUGH the catalog — never as a filesystem path (T-02-14).
	m, ok := cat.FindByID(name)
	if !ok {
		fmt.Fprintf(errOut, "inference: unknown model %q — run `villa recommend` to see catalog names\n", name)
		return exitBlocked
	}

	v := validateFn(cmd.Context(), m, withCeiling)
	return renderInference(out, v, jsonOut, verbose)
}

// runValidation is the production validation path: it builds the real container
// Runner + Vulkan Backend, resolves the recommend-chosen ctx + fit terms, wires the
// live sysfs GTT reader and a ceiling Runner factory, and calls the pure
// inference.Validate. It is the validateFn default (replaced in tests).
func runValidation(ctx context.Context, m catalog.CatalogModel, withCeiling bool) inference.Verdict {
	backend := inference.VulkanBackend()
	dir := modelsDir()

	// Recompute the fit terms for THIS model so the ceiling stress math has the
	// recommend-chosen ctx + envelope (D-10). Probe the host and pick for this model.
	profile := detect.Probe()
	cat := catalog.Catalog{Models: []catalog.CatalogModel{m}}
	rec := recommend.Pick(profile, cat, recommend.Overrides{Model: m.ID})

	// Guard the recommend refusal path (CR-02): when the memory envelope is
	// undeterminable Pick returns a zero Recommendation (Model:"", ContextLen:0,
	// WeightBytes:0). Starting llama-server with -c 0 and feeding WeightBytes:0 into
	// the sysfs assert (whose zero-weight guard degrades to WARN) would mask a real
	// CPU fallback. Refuse up front with a clear FAIL instead.
	if rec.Model == "" || rec.ContextLen <= 0 || rec.WeightBytes == 0 {
		return inference.Verdict{
			Status:      inference.StatusFail,
			Detail:      "cannot validate: no fitting configuration for this model on this host (memory envelope undeterminable — recommend refused)",
			Remediation: "run `villa recommend` to inspect the fit; ensure the GPU/memory envelope is detectable (see `villa detect`)",
			Provenance:  "recommend.Pick",
		}
	}

	spec := inference.RunSpec{
		ContainerName: "villa-inference-validate",
		ModelFile:     primaryModelFile(m),
		ModelsDir:     dir,
		ContextLen:    rec.ContextLen,
	}
	runner := inference.NewContainerRunner(backend, spec)

	in := inference.ValidateInput{
		Model:         m,
		ModelsDir:     dir,
		ContextLen:    rec.ContextLen,
		WeightBytes:   rec.WeightBytes,
		KVCacheBytes:  rec.KVCacheBytes,
		HeadroomBytes: rec.HeadroomBytes,
		EnvelopeBytes: rec.UsableEnvelopeBytes,
		Runner:        runner,
		ReadGTTUsed:   detect.GTTUsedBytes,
		Markers:       backend.ResidencyProof(),
	}
	if withCeiling {
		in.NewCeilingRunner = func(stress inference.RunSpec) inference.Runner {
			stress.ModelsDir = dir
			stress.ContainerName = "villa-inference-ceiling"
			stress.ModelFile = primaryModelFile(m)
			return inference.NewContainerRunner(backend, stress)
		}
	}
	return inference.Validate(ctx, in)
}

// renderInference writes the Verdict (table or --json) and RETURNS the exit code
// (it does not call os.Exit) so tests can assert both the output and the mapped
// code. It is the single place that interprets the Verdict status as an exit code:
// PASS=0, WARN=2, FAIL=1 — the same scriptable contract as preflight (D-13).
func renderInference(w io.Writer, v inference.Verdict, asJSON, withProvenance bool) int {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(v)
	} else {
		renderInferenceTable(w, v, withProvenance)
	}

	switch v.Status {
	case inference.StatusPass:
		return exitPass
	case inference.StatusWarn:
		return exitWarn
	default: // StatusFail
		return exitBlocked
	}
}

// renderInferenceTable writes the verdict as an aligned human table: the status
// word, the detail, the remediation (on non-PASS), the observed GTT delta, and —
// with provenance — the per-signal sources.
func renderInferenceTable(w io.Writer, v inference.Verdict, withProvenance bool) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "verdict\t%s\n", v.Status)
	fmt.Fprintf(tw, "detail\t%s\n", v.Detail)
	if v.Status != inference.StatusPass && v.Remediation != "" {
		fmt.Fprintf(tw, "remediation\t%s\n", v.Remediation)
	}
	if v.GTTDeltaBytes > 0 {
		fmt.Fprintf(tw, "gtt delta\t%s\n", gib(v.GTTDeltaBytes))
	}
	if withProvenance {
		fmt.Fprintf(tw, "log offload\t%s\n", boolSignal(v.LogOffload))
		fmt.Fprintf(tw, "sysfs offload\t%s\n", boolSignal(v.SysfsOffload))
		fmt.Fprintf(tw, "provenance\t%s\n", v.Provenance)
		if v.Raw != "" {
			fmt.Fprintf(tw, "raw\t%s\n", v.Raw)
		}
	}
	_ = tw.Flush()
}

// boolSignal renders a typed-Unknown offload signal for the provenance table.
func boolSignal(b detect.Bool) string {
	if !b.Known {
		return "unknown (" + b.Source + ")"
	}
	if b.Value {
		return "yes (" + b.Source + ")"
	}
	return "no (" + b.Source + ")"
}

// primaryModelFile resolves the on-disk GGUF filename for a model (prefers the first
// shard's filename, falls back to <id>.gguf). It mirrors the inference package
// helper so the verb can name the bound file without importing an internal helper.
func primaryModelFile(m catalog.CatalogModel) string {
	if len(m.Shards) > 0 && m.Shards[0].Filename != "" {
		return m.Shards[0].Filename
	}
	return filepath.Base(m.ID + ".gguf")
}
