package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
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

// recommendProbe is the host probe `villa recommend` fits against. Tests pin it
// to a fixed profile so the fit cannot depend on the RAM of the machine that
// runs them.
var recommendProbe = detect.Probe

// recommendConfigInputs reads the persisted inputs recommend fits with: the
// catalog path (an explicit flag wins over the saved one), the reservations, and
// the speculation mode. A missing or unreadable config is not an error; it yields
// the zero values (nothing reserved).
func recommendConfigInputs(flagCatalog string) (string, []recommend.Reservation, string) {
	cfg, err := config.LoadVilla()
	if err != nil {
		return flagCatalog, nil, ""
	}
	catalogPath := flagCatalog
	if catalogPath == "" {
		catalogPath = cfg.CatalogPath
	}
	return catalogPath, recommend.ReservationsFor(cfg), cfg.Speculation
}

// newRecommend builds `villa recommend`: probe the host, load the catalog,
// compute a single fitting pick with the fit math shown, re-validate any
// overrides, and — only with --save — persist the pick to config.
func newRecommend() *cobra.Command {
	var f recommendFlags

	cmd := &cobra.Command{
		Use:   "recommend",
		Short: "Recommend a model/quant/context that fits this host's memory envelope",
		Long: "Turn the detected hardware profile into a single memory-safe model/quant/context/backend " +
			"recommendation, showing the fit math (model_bytes + KV-cache@ctx + prompt cache + headroom ≤ usable_envelope). " +
			"Overrides (--model/--quant/--ctx) are re-validated against the envelope. Read-only unless --save.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			profile := recommendProbe()

			// Resolve the catalog source: an explicit --catalog flag wins; otherwise
			// fall back to a saved cfg.CatalogPath so a persisted external-catalog
			// choice is honored without re-passing the flag. A missing
			// config is not an error (read-only default). The SAME fail-soft
			// load sources the persisted memory inputs: a load error threads
			// the zero value (memory off), never an error-path change.
			catalogPath, res, speculation := recommendConfigInputs(f.catalogPath)

			cat, warnings, err := catalog.Load(catalogPath)
			if err != nil {
				return fmt.Errorf("recommend: load catalog: %w", err)
			}

			rec := recommend.Pick(profile, cat, recommend.Overrides{
				Model:       f.model,
				Quant:       f.quant,
				Ctx:         f.ctx,
				Speculation: speculation,
			}, res)

			if err := renderRecommend(cmd.OutOrStdout(), rec, warnings, jsonOut, f.alternatives); err != nil {
				return err
			}

			if f.save {
				// Persist the resolved catalog path (flag or inherited) so a future
				// run reuses it.
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

// saveRecommendation writes the pick to config (the ONLY config writer). A
// refusal (empty Model) is not persisted. catalogPath is persisted so a saved
// external-catalog choice is reused on the next run; empty means "use the
// embedded catalog" and round-trips as an empty field.
func saveRecommendation(w io.Writer, rec recommend.Recommendation, catalogPath string) error {
	if rec.Model == "" {
		return fmt.Errorf("recommend --save: nothing to save (no model was recommended)")
	}
	// A load→save of the whole file: hold the stack lock (ADR-0010) so a swap's
	// rollback cannot restore a config that predates this write.
	lock, err := acquireStackLock()
	if err != nil {
		return fmt.Errorf("recommend --save: %w", err)
	}
	defer func() { _ = lock.Release() }()
	// Start from the config on disk so --save changes only the pick: the
	// subsystem gates, their secrets and the resident slots are not the
	// recommendation's to reset (#149). An absent file loads as the typed
	// defaults, which is what keeps the dashboard/chat ports from being written
	// as zero (gap test:1b).
	c, err := config.LoadVilla()
	if err != nil {
		return fmt.Errorf("recommend --save: load existing config: %w", err)
	}
	c.Model = rec.Model
	c.Quant = rec.Quant
	c.Ctx = rec.ContextLen
	c.Backend = rec.Backend
	c.Speculation = rec.Speculation
	c.Vision = rec.Vision
	c.CatalogPath = catalogPath
	if err := config.SaveVilla(c); err != nil {
		return fmt.Errorf("recommend --save: %w", err)
	}
	path, _ := config.Path()
	fmt.Fprintf(w, "\nSaved recommendation to %s\n", path)
	return nil
}

// writeOptionalFitRows prints the fit terms that sit between the KV cache and the
// headroom: the prompt cache (ADR-0021), the vision projector and the draft sidecar.
// Each row is gated on a non-zero value (the ROCmAdvice gated-line pattern), so a
// pick that reserves none of a term prints no line for it and the math the table
// shows is exactly the math in TotalBytes.
func writeOptionalFitRows(w io.Writer, rec recommend.Recommendation) {
	rows := []struct {
		label string
		bytes uint64
	}{
		{"+ prompt cache", rec.PromptCacheBytes},
		{"+ vision projector", rec.ProjectorBytes},
		{"+ draft weight", rec.DraftBytes},
		{fmt.Sprintf("+ draft KV @ ctx %d", rec.ContextLen), rec.DraftKVBytes},
	}
	for _, r := range rows {
		if r.bytes > 0 {
			fmt.Fprintf(w, "%s\t%s\n", r.label, gib(r.bytes))
		}
	}
}

// renderRecommend writes the recommendation to w. Separated from RunE so the
// golden test can inject a fixture Recommendation and capture exact JSON bytes
// (dashboard-contract guard).
func renderRecommend(w io.Writer, rec recommend.Recommendation, warnings []string, asJSON, withAlternatives bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rec)
	}
	return renderRecommendTable(w, rec, warnings, withAlternatives)
}

// renderRecommendTable writes the human table: warnings, the pick, the fit math, the
// verdict, the backend advice, the coder (agent profile) section and, on request, the
// alternatives. Each section is its own helper, so the function stays a flat sequence.
func renderRecommendTable(w io.Writer, rec recommend.Recommendation, warnings []string, withAlternatives bool) error {
	writeWarnings(w, warnings)
	if rec.Model == "" {
		writeNoRecommendation(w, rec)
		return nil
	}
	writeRecommendHeader(w, rec)
	if err := writeFitSection(w, rec); err != nil {
		return err
	}
	writeBackendAdvice(w, rec)
	if err := writeCoderSection(w, rec); err != nil {
		return err
	}
	return writeAlternatives(w, rec, withAlternatives)
}

// writeWarnings prints each pre-pick warning on its own `!` line.
func writeWarnings(w io.Writer, warnings []string) {
	for _, warn := range warnings {
		fmt.Fprintf(w, "! %s\n", warn)
	}
}

// writeNoRecommendation prints the refusal: no pick, and the notes saying why.
func writeNoRecommendation(w io.Writer, rec recommend.Recommendation) {
	fmt.Fprintln(w, "No recommendation could be made.")
	for _, n := range rec.Notes {
		fmt.Fprintf(w, "  - %s\n", n)
	}
}

// writeRecommendHeader prints the pick, its speculation mode and the degraded flag.
func writeRecommendHeader(w io.Writer, rec recommend.Recommendation) {
	fmt.Fprintf(w, "Recommended: %s  (quant %s, ctx %d, backend %s)\n",
		rec.Model, rec.Quant, rec.ContextLen, rec.Backend)
	fmt.Fprintf(w, "  speculation: %s\n", rec.Speculation)
	if rec.Degraded {
		fmt.Fprintln(w, "  [DEGRADED ESTIMATE — see notes]")
	}
	fmt.Fprintln(w)
}

// writeFitSection prints the fit math and then what it decided.
func writeFitSection(w io.Writer, rec recommend.Recommendation) error {
	if err := writeFitTable(w, rec); err != nil {
		return err
	}
	writeFitVerdict(w, rec)
	return nil
}

// writeFitTable shows the fit math explicitly.
func writeFitTable(w io.Writer, rec recommend.Recommendation) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  model_bytes\t%s\n", gib(rec.WeightBytes))
	fmt.Fprintf(tw, "+ KV-cache @ ctx %d\t%s\n", rec.ContextLen, gib(rec.KVCacheBytes))
	writeOptionalFitRows(tw, rec)
	fmt.Fprintf(tw, "+ headroom\t%s\n", gib(rec.HeadroomBytes))
	fmt.Fprintf(tw, "= total\t%s\n", gib(rec.TotalBytes))
	// One row per reservation with bytes (ADR-0027), so a stack with nothing
	// reserved prints the same table as before reservations existed.
	for _, r := range rec.Reservations {
		if r.Bytes > 0 {
			fmt.Fprintf(tw, "− %s reservation\t%s\n", strings.ReplaceAll(r.Name, "_", " "), gib(r.Bytes))
		}
	}
	fmt.Fprintf(tw, "%s usable envelope\t%s\n", fitsGlyph(rec.Fits), gib(rec.UsableEnvelopeBytes))
	return tw.Flush()
}

// writeFitVerdict prints the Fits line, the vision verdict and the notes.
func writeFitVerdict(w io.Writer, rec recommend.Recommendation) {
	if rec.Fits {
		fmt.Fprintln(w, "\nFits: yes")
	} else {
		fmt.Fprintln(w, "\nFits: NO — this pick would not fit the usable envelope")
	}

	// The vision verdict is printed only for an entry that HAS a projector: either
	// one was reserved, or one was dropped and said so in a note. A text-only entry
	// gets no line, because "Vision: no" would read as a capability that was
	// withheld rather than one the model never had.
	if rec.Vision || visionDropped(rec.Notes) {
		fmt.Fprintf(w, "Vision: %s\n", yesNo(rec.Vision))
	}

	for _, n := range rec.Notes {
		fmt.Fprintf(w, "  - %s\n", n)
	}
}

// writeBackendAdvice surfaces the honesty-bounded ROCm advice after the notes, gated
// on a non-empty advice value. ROCm is the DEFAULT backend, so this annotates the
// already-selected pick. The Note points at `villa bench` and never promises a
// speed-up.
//
// The withheld case (advice == "") is the confidently-not-ready host, and it is the
// ONE case where readiness changes the recommended backend — Pick falls back to
// vulkan. It carries a Note naming the blocker instead of an advice value, so it is
// rendered on its own branch: gating the Note behind a non-empty advice would print
// NOTHING here and silently move the user off the default backend with no reason
// given. --json is unaffected (both fields are always stamped).
func writeBackendAdvice(w io.Writer, rec recommend.Recommendation) {
	if rec.ROCmAdvice != "" {
		fmt.Fprintf(w, "\nROCm advice: %s\n", rec.ROCmAdvice)
		if rec.ROCmNote != "" {
			fmt.Fprintf(w, "  - %s\n", rec.ROCmNote)
		}
	} else if rec.ROCmNote != "" {
		fmt.Fprintf(w, "\nBackend: %s\n  - %s\n", rec.Backend, rec.ROCmNote)
	}
}

// writeCoderSection renders the Coder (agent profile) section (CODER-02): the JSON
// block is ALWAYS stamped, but the human table renders compactly — the full fit
// inequality when a coder entry fits (residency "swap"), one honest line when none
// does (residency "shared", the agent rides the chat endpoint).
func writeCoderSection(w io.Writer, rec recommend.Recommendation) error {
	if rec.Coder.Model == "" {
		fmt.Fprintf(w, "\nCoder (agent profile): no coder model fits the usable envelope — residency %q (the agent rides the chat endpoint)\n",
			rec.Coder.Residency)
		return nil
	}
	fmt.Fprintf(w, "\nCoder (agent profile): %s  (quant %s, agent ctx %d)\n",
		rec.Coder.Model, rec.Coder.Quant, rec.Coder.AgentCtx)
	ctw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(ctw, "  model_bytes\t%s\n", gib(rec.Coder.WeightBytes))
	fmt.Fprintf(ctw, "+ KV-cache @ agent ctx %d\t%s\n", rec.Coder.AgentCtx, gib(rec.Coder.KVCacheBytes))
	fmt.Fprintf(ctw, "+ prompt cache\t%s\n", gib(rec.Coder.PromptCacheBytes))
	fmt.Fprintf(ctw, "+ headroom\t%s\n", gib(rec.Coder.HeadroomBytes))
	fmt.Fprintf(ctw, "= total\t%s\n", gib(rec.Coder.TotalBytes))
	fmt.Fprintf(ctw, "%s usable envelope\t%s\n", fitsGlyph(rec.Coder.Fits), gib(rec.UsableEnvelopeBytes))
	if err := ctw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "  residency: %s\n", rec.Coder.Residency)
	return nil
}

// writeAlternatives lists the other fitting picks, only when asked and only when
// there are any.
func writeAlternatives(w io.Writer, rec recommend.Recommendation, withAlternatives bool) error {
	if !withAlternatives || len(rec.Alternatives) == 0 {
		return nil
	}
	fmt.Fprintln(w, "\nOther fitting picks:")
	return writeAlternativeRows(w, rec.Alternatives)
}

// writeAlternativeRows prints one aligned row per alternative.
func writeAlternativeRows(w io.Writer, alts []recommend.Alternative) error {
	atw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, a := range alts {
		fmt.Fprintf(atw, "  %s\tquant %s\tctx %d\ttotal %s\n", a.Model, a.Quant, a.ContextLen, gib(a.TotalBytes))
	}
	return atw.Flush()
}

// liveLoadedReservations returns the reservations for the PERSISTED config
// (ADR-0027). A config load error fails SOFT to no reservations, so a broken
// config never silently enables one and never changes an error path.
func liveLoadedReservations() []recommend.Reservation {
	c, err := config.LoadVilla()
	if err != nil {
		return nil
	}
	return recommend.ReservationsFor(c)
}

// gib renders bytes as a GiB string with raw bytes for the fit table.
func gib(b uint64) string {
	return fmt.Sprintf("%.3f GiB (%d bytes)", float64(b)/(1<<30), b)
}

// yesNo renders a bool as the table's yes/no vocabulary.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// visionDropped reports whether a note says the projector was dropped. It keys on
// the note's own prefix rather than a second Recommendation field: the note is
// already the contract that says why, and a parallel flag could disagree with it.
func visionDropped(notes []string) bool {
	for _, n := range notes {
		if strings.HasPrefix(n, "vision:") {
			return true
		}
	}
	return false
}

// fitsGlyph returns a comparison glyph reflecting whether total ≤ envelope.
func fitsGlyph(fits bool) string {
	if fits {
		return "≤"
	}
	return ">"
}
