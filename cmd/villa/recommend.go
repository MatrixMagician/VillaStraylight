package main

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// recommend command flags. These are command-local (not persistent) so they only
// attach to `villa recommend`.
type recommendFlags struct {
	model        string
	quant        string
	ctx          int
	catalogPath  string
	alternatives bool
	save         bool
}

// newRecommend builds `villa recommend`: probe the host, load the catalog,
// compute a single fitting pick with the fit math shown (D-06), re-validate any
// overrides (D-07), and — only with --save — persist the pick to config (D-20).
func newRecommend() *cobra.Command {
	var f recommendFlags

	cmd := &cobra.Command{
		Use:   "recommend",
		Short: "Recommend a model/quant/context that fits this host's memory envelope",
		Long: "Turn the detected hardware profile into a single memory-safe model/quant/context/backend " +
			"recommendation, showing the fit math (model_bytes + KV-cache@ctx + headroom ≤ usable_envelope). " +
			"Overrides (--model/--quant/--ctx) are re-validated against the envelope. Read-only unless --save.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := detect.Probe()

			// Resolve the catalog source: an explicit --catalog flag wins; otherwise
			// fall back to a saved cfg.CatalogPath so a persisted external-catalog
			// choice is honored without re-passing the flag (D-09, IN-03). A missing
			// config is not an error (read-only default, D-20).
			catalogPath := f.catalogPath
			if catalogPath == "" {
				if cfg, err := config.LoadVilla(); err == nil {
					catalogPath = cfg.CatalogPath
				}
			}

			cat, warnings, err := catalog.Load(catalogPath)
			if err != nil {
				return fmt.Errorf("recommend: load catalog: %w", err)
			}

			rec := recommend.Pick(profile, cat, recommend.Overrides{
				Model: f.model,
				Quant: f.quant,
				Ctx:   f.ctx,
			})

			if err := renderRecommend(cmd.OutOrStdout(), rec, warnings, jsonOut, f.alternatives); err != nil {
				return err
			}

			if f.save {
				// Persist the resolved catalog path (flag or inherited) so a future
				// run reuses it (IN-03).
				return saveRecommendation(cmd.OutOrStdout(), rec, catalogPath)
			}
			return nil
		},
	}

	pf := cmd.Flags()
	pf.StringVar(&f.model, "model", "", "override the model id (re-validated against the envelope)")
	pf.StringVar(&f.quant, "quant", "", "override the quantization label")
	pf.IntVar(&f.ctx, "ctx", 0, "override the context length in tokens (re-validated)")
	pf.StringVar(&f.catalogPath, "catalog", "", "path to an external catalog JSON (schema-checked; falls back to embedded on mismatch)")
	pf.BoolVar(&f.alternatives, "alternatives", false, "also list other fitting picks")
	pf.BoolVar(&f.save, "save", false, "persist the recommended pick to ~/.config/villa/config.toml")

	return cmd
}

// saveRecommendation writes the pick to config (the ONLY config writer, D-20). A
// refusal (empty Model) is not persisted. catalogPath is persisted so a saved
// external-catalog choice is reused on the next run (IN-03); empty means "use the
// embedded catalog" and round-trips as an empty field.
func saveRecommendation(w io.Writer, rec recommend.Recommendation, catalogPath string) error {
	if rec.Model == "" {
		return fmt.Errorf("recommend --save: nothing to save (no model was recommended)")
	}
	// Seed from the typed defaults so the dashboard/chat fields
	// (DashboardAddr/DashboardPort/ChatPort) are preserved on write — a partial
	// literal here would persist 0/0/"" and break the dashboard bind (gap test:1b).
	// The default literals (8888/3000/127.0.0.1) live only in defaultConfig().
	c := config.DefaultVillaConfig()
	c.Model = rec.Model
	c.Quant = rec.Quant
	c.Ctx = rec.ContextLen
	c.Backend = rec.Backend
	c.CatalogPath = catalogPath
	if err := config.SaveVilla(c); err != nil {
		return fmt.Errorf("recommend --save: %w", err)
	}
	path, _ := config.Path()
	fmt.Fprintf(w, "\nSaved recommendation to %s\n", path)
	return nil
}

// renderRecommend writes the recommendation to w. Separated from RunE so the
// golden test can inject a fixture Recommendation and capture exact JSON bytes
// (D-05 dashboard-contract guard).
func renderRecommend(w io.Writer, rec recommend.Recommendation, warnings []string, asJSON, withAlternatives bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rec)
	}
	return renderRecommendTable(w, rec, warnings, withAlternatives)
}

func renderRecommendTable(w io.Writer, rec recommend.Recommendation, warnings []string, withAlternatives bool) error {
	for _, warn := range warnings {
		fmt.Fprintf(w, "! %s\n", warn)
	}

	if rec.Model == "" {
		fmt.Fprintln(w, "No recommendation could be made.")
		for _, n := range rec.Notes {
			fmt.Fprintf(w, "  - %s\n", n)
		}
		return nil
	}

	fmt.Fprintf(w, "Recommended: %s  (quant %s, ctx %d, backend %s)\n",
		rec.Model, rec.Quant, rec.ContextLen, rec.Backend)
	if rec.Degraded {
		fmt.Fprintln(w, "  [DEGRADED ESTIMATE — see notes]")
	}
	fmt.Fprintln(w)

	// Show the fit math explicitly (D-06).
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  model_bytes\t%s\n", gib(rec.WeightBytes))
	fmt.Fprintf(tw, "+ KV-cache @ ctx %d\t%s\n", rec.ContextLen, gib(rec.KVCacheBytes))
	fmt.Fprintf(tw, "+ headroom\t%s\n", gib(rec.HeadroomBytes))
	fmt.Fprintf(tw, "= total\t%s\n", gib(rec.TotalBytes))
	fmt.Fprintf(tw, "%s usable envelope\t%s\n", fitsGlyph(rec.Fits), gib(rec.UsableEnvelopeBytes))
	if err := tw.Flush(); err != nil {
		return err
	}

	if rec.Fits {
		fmt.Fprintln(w, "\nFits: yes")
	} else {
		fmt.Fprintln(w, "\nFits: NO — this pick would not fit the usable envelope")
	}

	for _, n := range rec.Notes {
		fmt.Fprintf(w, "  - %s\n", n)
	}

	if withAlternatives && len(rec.Alternatives) > 0 {
		fmt.Fprintln(w, "\nOther fitting picks:")
		atw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		for _, a := range rec.Alternatives {
			fmt.Fprintf(atw, "  %s\tquant %s\tctx %d\ttotal %s\n", a.Model, a.Quant, a.ContextLen, gib(a.TotalBytes))
		}
		if err := atw.Flush(); err != nil {
			return err
		}
	}
	return nil
}

// gib renders bytes as a GiB string with raw bytes for the fit table.
func gib(b uint64) string {
	return fmt.Sprintf("%.3f GiB (%d bytes)", float64(b)/(1<<30), b)
}

// fitsGlyph returns a comparison glyph reflecting whether total ≤ envelope.
func fitsGlyph(fits bool) string {
	if fits {
		return "≤"
	}
	return ">"
}
