package main

// bench_compare.go is the cmd-tier READ-ONLY `villa bench --compare` / `villa bench
// --list` surface (BENCH-04). It loads saved bench reports via the Plan 14-01 pure
// benchstore core (Load), runs the pure comparability guard (Compare), and renders the
// per-metric pp/tg deltas on SEPARATE lines — or, on a non-comparable pair, an honest
// "not comparable" refusal with the differing fields and NO delta. It NEVER runs a
// benchmark, NEVER touches the backend (no bench.Run / benchstoreWrite / backend swap),
// and the flag wiring (in newBench) rejects combination with the live-measurement flags
// at the cobra boundary. Exit mapping mirrors preflight: comparable delta → 0,
// not-comparable → 2, no/insufficient reports → 1 (with remediation).
//
// pp and tg stay STRUCTURALLY SEPARATE end-to-end (no blended tok/s key) — the
// --compare --json contract is byte-frozen by testdata/bench-compare.json.golden and the
// no-blended grep guards it.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/benchstore"
)

// benchCompareRun is the package-level indirection RunE calls for the read-only
// --compare/--list dispatch so bench_test.go can drive the path without os.Exit. It
// mirrors benchRun: the default runs runBenchCompare and os.Exit(code)s; a test override
// returns the captured code without exiting (tests call runBenchCompare directly).
var benchCompareRun = func(cmd *cobra.Command, list, compare, asJSON bool, d benchstore.Deps) int {
	code := runBenchCompare(cmd, list, compare, asJSON, d)
	os.Exit(code)
	return code // unreachable in the default; satisfies the signature for test overrides
}

// benchCompareSide is one report's identity + band in the --compare --json contract: the
// backend, pp/tg as SEPARATE keys, and the per-side void flag (RESEARCH Q3/A5) so a
// consumer can discount a not-authoritative side even on a comparable, delta-bearing pair.
type benchCompareSide struct {
	Backend         string  `json:"backend"`
	CapturedAt      string  `json:"captured_at"`
	PromptPerSec    float64 `json:"prompt_per_sec"`
	PredictedPerSec float64 `json:"predicted_per_sec"`
	VoidExhausted   bool    `json:"void_exhausted"`
}

// benchCompareEntry is the FROZEN `--compare --json` machine contract. On a comparable
// pair Comparable is true, DifferingFields is empty, and DeltaPromptPerSec /
// DeltaPredictedPerSec are the per-metric deltas (B − A) as SEPARATE keys — never blended.
// On a non-comparable pair Comparable is false, DifferingFields names why, and the deltas
// are ZERO (the no-false-equal refusal shape). The per-side a_void_exhausted /
// b_void_exhausted flags are carried verbatim from each report so a void side is honestly
// marked not-authoritative even when the pair is comparable.
type benchCompareEntry struct {
	Mode                 string            `json:"mode"` // always "compare"
	Comparable           bool              `json:"comparable"`
	DifferingFields      []string          `json:"differing_fields"`
	A                    *benchCompareSide `json:"a,omitempty"`
	B                    *benchCompareSide `json:"b,omitempty"`
	AVoidExhausted       bool              `json:"a_void_exhausted"`
	BVoidExhausted       bool              `json:"b_void_exhausted"`
	DeltaPromptPerSec    float64           `json:"delta_prompt_per_sec"`
	DeltaPredictedPerSec float64           `json:"delta_predicted_per_sec"`
}

// benchListEntry is the --list --json shape: the enumerated saved reports as the frozen
// Plan-01 SavedReport records, untouched. (The human --list path renders a table.)
type benchListEntry struct {
	Mode    string                   `json:"mode"` // always "list"
	Reports []benchstore.SavedReport `json:"reports"`
}

// benchCompareSideOf folds a saved report's primary measured side into a benchCompareSide
// (the Single band for a single report, else the AB.B compared-against side).
func benchCompareSideOf(r benchstore.SavedReport) benchCompareSide {
	side := r.Single
	if side == nil && r.AB != nil {
		side = &r.AB.B
	}
	s := benchCompareSide{
		Backend:       r.Fingerprint.Backend,
		CapturedAt:    r.CapturedAt,
		VoidExhausted: r.VoidExhausted,
	}
	if side != nil {
		s.PromptPerSec = side.PromptPerSec
		s.PredictedPerSec = side.PredictedPerSec
		if s.Backend == "" {
			s.Backend = side.Backend
		}
	}
	return s
}

// selectComparePair picks the pair --compare renders: the two MOST-RECENT reports that
// are comparable to each other (A8 auto-selection for v1). It scans newest-first (the
// store is append-only so later == more recent) and returns the first comparable pair.
// If no comparable pair exists, it returns the two most-recent reports and lets Compare
// report them not-comparable (an honest refusal, never a fabricated equal). reports must
// have len >= 2 (the caller guards insufficient stores).
func selectComparePair(reports []benchstore.SavedReport) (benchstore.SavedReport, benchstore.SavedReport) {
	for i := len(reports) - 1; i >= 1; i-- {
		for j := i - 1; j >= 0; j-- {
			if ok, _ := benchstore.Comparable(reports[i].Fingerprint, reports[j].Fingerprint); ok {
				// j is older, i is newer → render A=older, B=newer (delta = B − A).
				return reports[j], reports[i]
			}
		}
	}
	// No comparable pair: fall to the two most-recent (Compare will refuse).
	return reports[len(reports)-2], reports[len(reports)-1]
}

// runBenchCompare is the READ-ONLY --compare/--list path. It RETURNS the exit code (no
// os.Exit) so tests assert output+code. It loads saved reports via the seam, then:
//   - --list: enumerates the reports (table or --json), exit 0 (empty store → remediation
//     + exit 1).
//   - --compare: auto-selects the two most-recent comparable reports, runs the pure
//     Compare guard, and renders the pp/tg deltas (separate lines) OR a "not comparable"
//     refusal (no delta, exit 2). A comparable pair with a void side STILL prints the
//     delta but flags that side not-authoritative (advisory, not a refusal — exit stays 0).
//
// It NEVER calls bench.Run, benchstoreWrite, or any backend swap.
func runBenchCompare(cmd *cobra.Command, list, compare, asJSON bool, d benchstore.Deps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	reports, err := benchstore.Load(d)
	if err != nil {
		fmt.Fprintf(errOut, "bench: failed to read saved reports: %v\n", err)
		return exitBlocked
	}

	if list {
		if len(reports) == 0 {
			fmt.Fprintln(errOut, "bench: no saved reports — run `villa bench` first")
			return exitBlocked
		}
		if asJSON {
			return encodeBenchJSON(out, errOut, benchListEntry{Mode: "list", Reports: reports})
		}
		renderBenchList(out, reports)
		return exitPass
	}

	// --compare: need at least two reports to compute a delta.
	if len(reports) < 2 {
		fmt.Fprintln(errOut, "bench: need at least two saved reports to compare — run `villa bench` first "+
			"(e.g. once on vulkan and once on rocm, same model/quant/host)")
		return exitBlocked
	}

	a, b := selectComparePair(reports)
	cr := benchstore.Compare(a, b)

	if asJSON {
		entry := benchCompareEntryFrom(cr, a, b)
		if code := encodeBenchJSON(out, errOut, entry); code != exitPass {
			return code // encode failure
		}
		// The exit mapping is the SAME in JSON mode: a not-comparable pair is exit 2 even
		// though the contract was emitted (comparable:false carries the refusal).
		if !cr.Comparable {
			return exitWarn
		}
		return exitPass
	}

	if !cr.Comparable {
		renderCompareNotComparable(out, cr)
		return exitWarn
	}
	renderCompareDelta(out, cr, a, b)
	return exitPass
}

// benchCompareEntryFrom folds the pure CompareResult + the two reports into the frozen
// --json entry: comparable status, differing fields, per-side identity + void flags, and
// the per-metric deltas (zeroed on a refusal — Compare already zeroes them).
func benchCompareEntryFrom(cr benchstore.CompareResult, a, b benchstore.SavedReport) benchCompareEntry {
	sa := benchCompareSideOf(a)
	sb := benchCompareSideOf(b)
	diff := cr.DifferingFields
	if diff == nil {
		diff = []string{}
	}
	return benchCompareEntry{
		Mode:                 "compare",
		Comparable:           cr.Comparable,
		DifferingFields:      diff,
		A:                    &sa,
		B:                    &sb,
		AVoidExhausted:       a.VoidExhausted,
		BVoidExhausted:       b.VoidExhausted,
		DeltaPromptPerSec:    cr.DeltaPromptPerSec,
		DeltaPredictedPerSec: cr.DeltaPredictedPerSec,
	}
}

// encodeBenchJSON encodes v as indented JSON to out (cloning runBench's encoder shape).
// An encode error is a stderr WARN + exitBlocked.
func encodeBenchJSON(out, errOut io.Writer, v any) int {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(errOut, "bench: encode json: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

// renderBenchList writes the human --list table: one row per saved report (index,
// captured_at, model/quant/backend, pp/tg, void).
func renderBenchList(w io.Writer, reports []benchstore.SavedReport) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tcaptured_at\tmodel\tquant\tbackend\tpp tok/s\ttg tok/s\tvoid")
	for i, r := range reports {
		s := benchCompareSideOf(r)
		void := ""
		if r.VoidExhausted {
			void = "void"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%.2f\t%.2f\t%s\n",
			i, r.CapturedAt, r.Fingerprint.Model, r.Fingerprint.Quant, s.Backend,
			s.PromptPerSec, s.PredictedPerSec, void)
	}
	tw.Flush()
}

// renderCompareNotComparable writes the honest refusal: "not comparable" + the differing
// fields, with NO numeric delta (the no-false-equal posture).
func renderCompareNotComparable(w io.Writer, cr benchstore.CompareResult) {
	fmt.Fprintf(w, "not comparable: differing fields: %v\n", cr.DifferingFields)
	fmt.Fprintln(w, "  refusing to show a delta across non-comparable runs (different model/quant/ctx/host)")
}

// renderCompareDelta writes the comparable-pair human render: each side's identity +
// pp/tg, then Δpp and Δtg on SEPARATE lines. A void side STILL shows its delta but is
// flagged "not authoritative (residency void)" — advisory, never a refusal (RESEARCH Q3/A5).
func renderCompareDelta(w io.Writer, cr benchstore.CompareResult, a, b benchstore.SavedReport) {
	sa := benchCompareSideOf(a)
	sb := benchCompareSideOf(b)

	writeSide := func(label string, s benchCompareSide) {
		marker := ""
		if s.VoidExhausted {
			marker = "  [not authoritative — residency void]"
		}
		fmt.Fprintf(w, "%s (%s)%s:\n", label, s.Backend, marker)
		fmt.Fprintf(w, "  pp tok/s: %8.2f\n", s.PromptPerSec)
		fmt.Fprintf(w, "  tg tok/s: %8.2f\n", s.PredictedPerSec)
	}
	writeSide("A", sa)
	fmt.Fprintln(w)
	writeSide("B", sb)
	fmt.Fprintf(w, "\ndelta (%s -> %s):\n", sa.Backend, sb.Backend)
	fmt.Fprintf(w, "  Δpp tok/s: %+8.2f\n", cr.DeltaPromptPerSec)
	fmt.Fprintf(w, "  Δtg tok/s: %+8.2f\n", cr.DeltaPredictedPerSec)
}
