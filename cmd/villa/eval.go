package main

// eval.go is the thin cobra caller for `villa eval` (ADR-0018): run the embedded
// capability suite against the served model and compare it with the eval baseline
// recorded for that model; `--record` accepts the run as the new baseline. The
// decisions (grading, the verdict, the refusal of a baseline with holes) live in
// internal/eval; the store is internal/evalstore. This file loads the config,
// resolves the key and provenance, wires the completion seam, renders, and maps the
// verdict through verify.ExitCode so eval cannot drift onto its own codes.
//
// ON COMMAND ONLY, and READ-ONLY toward the stack: it takes no stack lock and
// mutates no unit and no config, like `status`. A swap that lands mid-run shows up
// as unconducted cases or changed provenance, never as a silent pass. Nothing calls
// it from status, doctor or a swap.
//
// Every completion goes through inferenceClient(cfg) (ADR-0014); the image reference
// arrives from the pin path through the backend seam, never as a literal here.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/eval"
	"github.com/MatrixMagician/VillaStraylight/internal/evalstore"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/llm"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/verify"
)

// evalCaseTimeout bounds one capability case's completion. A case that does not
// answer inside it is unconducted, never failed.
const evalCaseTimeout = 2 * time.Minute

// evalJSONSchema is the AUTHORITATIVE schema of the `villa eval --json` contract,
// frozen by cmd/villa/testdata/eval.json.golden and eval-record.json.golden. Evolve
// it append-only with a schema bump; refreeze with
// `go test ./cmd/villa/ -run 'TestEval.*Golden' -update`.
const evalJSONSchema = 1

// evalDeps are the injectable seams for `villa eval`, so the run path is testable
// off-hardware with no network. The live wiring is liveEvalDeps.
type evalDeps struct {
	// loadedConfig reads config.toml.
	loadedConfig func() (config.VillaConfig, error)
	// inferenceImage resolves the backend name and the effective image reference
	// the inference unit runs, through the pin path.
	inferenceImage func(cfg config.VillaConfig) (backend, ref string, err error)
	// suite returns the capability cases (live: the embedded suite).
	suite func() ([]eval.Case, error)
	// complete returns the completion seam for the served model.
	complete func(cfg config.VillaConfig, model string) func(context.Context, llm.ChatRequest) (llm.Reply, error)
	// rerank scores documents against a query through the memory stack's reranker
	// (ADR-0028), one score per document in document order.
	rerank func(ctx context.Context, query string, docs []string) ([]float64, error)
	// extract hands a document to the memory stack's extractor (ADR-0033) and
	// returns the text it extracted.
	extract func(ctx context.Context, name, mime string, data []byte) (string, error)
	// store is the eval-baselines.json byte seam.
	store evalstore.Deps
}

// liveEvalDeps wires evalDeps to the real host.
func liveEvalDeps() evalDeps {
	path := evalstore.Path()
	return evalDeps{
		loadedConfig:   liveLoadedConfig,
		inferenceImage: liveEvalImage,
		suite:          eval.Suite,
		complete:       liveEvalComplete,
		rerank:         liveEvalRerank,
		extract:        liveEvalExtract,
		store: evalstore.Deps{
			ReadAll:  storeReader(path), // absent store ⇒ no eval baseline (Reject)
			WriteAll: func(data []byte) error { return evalstore.WriteFileAtomic(path, data) },
		},
	}
}

// liveEvalImage resolves the configured backend, fail-closed on an unknown value,
// and applies this host's effective pin, so the provenance names the image the
// inference unit actually runs.
func liveEvalImage(cfg config.VillaConfig) (string, string, error) {
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return "", "", err
	}
	backend = livePinnedBackend(liveResolver(), backend)
	return backend.Name(), backend.Image(), nil
}

// liveEvalComplete sends each capability case to the served model through the
// authenticated inference client as one non-streamed completion, which returns the
// content and any tool call.
func liveEvalComplete(cfg config.VillaConfig, model string) func(context.Context, llm.ChatRequest) (llm.Reply, error) {
	client := inferenceClient(cfg).Chat(evalCaseTimeout)
	return func(ctx context.Context, req llm.ChatRequest) (llm.Reply, error) {
		req.Model = model
		return client.Chat(ctx, req)
	}
}

// liveEvalRerank scores docs against query through villa-rerank over villa.network.
func liveEvalRerank(ctx context.Context, query string, docs []string) ([]float64, error) {
	return postRerank(ctx, orchestrate.EmbedImage(), config.RerankAddr, config.RerankPort, query, docs)
}

// postRerank is the one rerank request villa makes: the eval seam and the install
// readiness probe both go through it. The unit is container-DNS only, so the
// request rides the in-network curl of the memory proof; helperImage is the probe
// helper, never a pin.
func postRerank(ctx context.Context, helperImage, addr string, port int, query string, docs []string) ([]float64, error) {
	body, err := json.Marshal(map[string]any{
		"model":     orchestrate.RerankModelName,
		"query":     query,
		"documents": docs,
		"top_n":     len(docs),
	})
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("http://%s:%d/v1/rerank", addr, port)
	out, err := runProbeCurl(ctx, helperImage,
		"-sf", "-X", "POST", url,
		"-H", "Content-Type: application/json",
		"-d", string(body),
	)
	if err != nil {
		return nil, err
	}
	return parseRerankScores(out, len(docs))
}

// liveEvalExtract extracts a fixture's text through villa-extract over
// villa.network. The name is the fixture's, for the record; Tika reads only the
// bytes and the mime.
func liveEvalExtract(ctx context.Context, _, mime string, data []byte) (string, error) {
	return postExtract(ctx, orchestrate.EmbedImage(), config.ExtractAddr, config.ExtractPort, mime, data)
}

// postExtract is the one extraction request villa makes: the eval seam and the
// install readiness probe both go through it. It PUTs data to the extractor's
// /tika/text with mime as its Content-Type, the request Open WebUI's loader makes,
// and returns the extracted text (ADR-0033). The unit is container-DNS only, so
// the request rides the in-network curl of the memory proof with the document on
// stdin; helperImage is the probe helper, never a pin.
func postExtract(ctx context.Context, helperImage, addr string, port int, mime string, data []byte) (string, error) {
	out, _, err := probeCurl(ctx, helperImage, data, extractCurlArgs(addr, port, mime))
	if err != nil {
		return "", err
	}
	return parseExtractText(out)
}

// extractCurlArgs is the curl half of an extraction request. The Content-Type is
// load-bearing: Tika sniffs a PUT without one as a form body and extracts nothing.
func extractCurlArgs(addr string, port int, mime string) []string {
	url := fmt.Sprintf("http://%s:%d/tika/text", addr, port)
	return []string{"-sf", "-X", "PUT", url, "-H", "Content-Type: " + mime, "--data-binary", "@-"}
}

// parseExtractText reads the extracted text from Tika's /tika/text answer, the
// X-TIKA:content key of one JSON object. A reply without that key is refused, so a
// changed answer shape never reads as a document with no text.
func parseExtractText(out []byte) (string, error) {
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", fmt.Errorf("decode extraction response: %w", err)
	}
	text, ok := resp["X-TIKA:content"].(string)
	if !ok {
		return "", errors.New("extraction response carries no X-TIKA:content text")
	}
	return text, nil
}

// parseRerankScores maps llama-server's rerank answer, one result per document
// carrying its index and a raw score in score order, onto one score per document
// in document order. An answer that does not cover every document is refused.
func parseRerankScores(out []byte, n int) ([]float64, error) {
	var resp struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}
	scores := make([]float64, n)
	seen := make([]bool, n)
	for _, r := range resp.Results {
		if r.Index < 0 || r.Index >= n {
			return nil, fmt.Errorf("rerank response names document %d of %d", r.Index, n)
		}
		scores[r.Index], seen[r.Index] = r.RelevanceScore, true
	}
	for i, ok := range seen {
		if !ok {
			return nil, fmt.Errorf("rerank response carries no score for document %d of %d", i, n)
		}
	}
	return scores, nil
}

// newEval builds `villa eval`.
func newEval() *cobra.Command {
	var record, asJSON bool
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Check the served model's answers against its recorded eval baseline",
		Long: "Run the embedded suite of capability cases (arithmetic, strict formats, extraction, JSON output, " +
			"reading code, units and dates, tool calls) against the served model, greedily and one at a time, " +
			"and compare each case with the eval baseline recorded for this model, quant and suite version. " +
			"A case that passed in the baseline and fails now is a regression (FAIL, exit 1). No baseline, or a " +
			"case that could not be conducted, is REJECT (exit 2): a comparison that did not happen is not " +
			"evidence. Tool-call cases are skipped while tools mode is off. Backend, image, speculation, ctx and " +
			"tools mode are reported when they differ from the baseline's, never keyed on. --record accepts this " +
			"run as the baseline and refuses one with unconducted cases. Takes no stack lock and changes nothing " +
			"but eval-baselines.json.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(runEval(cmd, record, asJSON, liveEvalDeps()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&record, "record", false, "accept this run as the eval baseline for the served model")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the report as the byte-frozen schema-1 JSON contract")
	return cmd
}

// runEval runs the suite, compares or records, renders, and RETURNS the exit code
// (no os.Exit) so tests drive it. In --record mode the exit code reports the record;
// the verdict still reports the comparison with the baseline it replaces.
func runEval(cmd *cobra.Command, record, asJSON bool, d evalDeps) int {
	run, prior, err := conductEval(cmd.Context(), d)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "eval: %v\n", err)
		return exitBlocked
	}
	view := newEvalView(run, prior)
	code := verify.ExitCode(view.status, exitPass, exitBlocked, exitWarn)
	if record {
		code = recordEval(&view, run, d.store)
	}
	renderEval(cmd.OutOrStdout(), cmd.ErrOrStderr(), view, asJSON)
	return code
}

// conductEval loads what a run needs before spending any inference on it, then runs
// the suite. It returns the run and the eval baseline recorded for its key, or nil.
func conductEval(ctx context.Context, d evalDeps) (eval.Run, *eval.Baseline, error) {
	cfg, err := d.loadedConfig()
	if err != nil {
		return eval.Run{}, nil, fmt.Errorf("cannot read config.toml (%w) — fix or remove it, then re-run", err)
	}
	backend, image, err := d.inferenceImage(cfg)
	if err != nil {
		return eval.Run{}, nil, fmt.Errorf("resolve the inference backend: %w", err)
	}
	cases, err := d.suite()
	if err != nil {
		return eval.Run{}, nil, err
	}
	doc, err := evalstore.Load(d.store)
	if err != nil {
		return eval.Run{}, nil, fmt.Errorf("read the eval baselines: %w", err)
	}
	run := evalTarget(cfg, backend, image)
	run.Results = eval.Execute(ctx, cases, eval.Deps{
		Complete:  d.complete(cfg, run.Key.Model),
		ToolsOn:   run.Provenance.ToolsMode,
		Rerank:    d.rerank,
		RerankOn:  subsystem.RerankOn(cfg),
		Extract:   d.extract,
		ExtractOn: subsystem.ExtractOn(cfg),
	})
	return run, doc.Find(run.Key), nil
}

// evalTarget is the run's key and provenance: the served model and its quant (the
// coder's in swap-residency coding mode) as the key, the stack as provenance.
func evalTarget(cfg config.VillaConfig, backend, image string) eval.Run {
	model, ctx := stackapply.ServedTarget(cfg)
	quant := cfg.Quant
	if subsystem.CodingModeOn(cfg) && cfg.CoderModel != "" {
		quant = cfg.CoderQuant
	}
	return eval.Run{
		Key: eval.Key{Model: model, Quant: quant, SuiteVersion: eval.SuiteVersion},
		Provenance: eval.Provenance{
			Backend:     backend,
			ImageDigest: imageDigest(image),
			Speculation: cmp.Or(cfg.Speculation, config.SpeculationOff),
			Ctx:         ctx,
			ToolsMode:   subsystem.ToolsOn(cfg),
		},
	}
}

// imageDigest is the digest part of an image reference, or the whole reference when
// it carries none.
func imageDigest(ref string) string {
	if _, digest, ok := strings.Cut(ref, "@"); ok {
		return digest
	}
	return ref
}

// evalView is the --json contract (schema 1) and the source of the human report.
type evalView struct {
	Schema     int             `json:"schema"`
	Verdict    string          `json:"verdict"`
	Detail     string          `json:"detail"`
	Key        eval.Key        `json:"key"`
	Provenance eval.Provenance `json:"provenance"`
	Score      eval.Score      `json:"score"`
	// BaselineScore is null when no eval baseline is recorded for the key.
	BaselineScore     *eval.Score             `json:"baseline_score"`
	Regressions       []eval.Result           `json:"regressions"`
	Improvements      []eval.Result           `json:"improvements"`
	Unconducted       []eval.Result           `json:"unconducted"`
	Skipped           []eval.Result           `json:"skipped"`
	ProvenanceChanges []eval.ProvenanceChange `json:"provenance_changes"`
	Results           []eval.Result           `json:"results"`
	// Record is present only with --record.
	Record *evalRecordView `json:"record,omitempty"`

	status verify.Status
}

// evalRecordView is the outcome of --record: written, with the score of the
// baseline it replaced (null for the first), or refused with the reason.
type evalRecordView struct {
	Written  bool        `json:"written"`
	Replaces *eval.Score `json:"replaces"`
	Refusal  string      `json:"refusal,omitempty"`
}

// newEvalView compares the run with its eval baseline and shapes the report.
func newEvalView(run eval.Run, prior *eval.Baseline) evalView {
	rep := eval.Compare(run, prior)
	return evalView{
		Schema:            evalJSONSchema,
		Verdict:           strings.ToUpper(rep.Status.String()),
		Detail:            rep.Detail,
		Key:               run.Key,
		Provenance:        run.Provenance,
		Score:             eval.Tally(run.Results),
		BaselineScore:     scoreOf(prior),
		Regressions:       rep.Regressions,
		Improvements:      rep.Improvements,
		Unconducted:       rep.Unconducted,
		Skipped:           rep.Skipped,
		ProvenanceChanges: rep.ProvenanceChanges,
		Results:           run.Results,
		status:            rep.Status,
	}
}

// scoreOf tallies an eval baseline, or nil when there is none.
func scoreOf(b *eval.Baseline) *eval.Score {
	if b == nil {
		return nil
	}
	s := eval.Tally(b.Results)
	return &s
}

// recordEval writes the run as the eval baseline. A run with holes is refused
// (Reject: nothing was disproven, nothing was written); a failed write blocks.
func recordEval(v *evalView, run eval.Run, store evalstore.Deps) int {
	b, err := eval.Record(run)
	if err != nil {
		v.Record = &evalRecordView{Refusal: err.Error()}
		return verify.ExitCode(verify.Reject, exitPass, exitBlocked, exitWarn)
	}
	replaced, err := evalstore.Put(store, b)
	if err != nil {
		v.Record = &evalRecordView{Refusal: err.Error()}
		return exitBlocked
	}
	v.Record = &evalRecordView{Written: true, Replaces: scoreOf(replaced)}
	return exitPass
}

// renderEval writes the report: one JSON document on stdout, or the human report
// with a non-pass verdict and a refused record on stderr, so a caller piping stdout
// does not silently lose them.
func renderEval(out, errOut io.Writer, v evalView, asJSON bool) {
	if asJSON {
		b, _ := json.MarshalIndent(v, "", "  ")
		fmt.Fprintf(out, "%s\n", b)
		return
	}
	writeEvalHeader(out, v)
	writeEvalCases(out, v)
	verdictOut := out
	if v.status != verify.Pass {
		verdictOut = errOut
	}
	fmt.Fprintf(verdictOut, "eval: %s — %s\n", v.Verdict, v.Detail)
	writeEvalRecord(out, errOut, v)
}

// onOff renders a boolean setting.
var onOff = map[bool]string{true: "on", false: "off"}

// writeEvalHeader names the key and provenance, the two scores, and each provenance
// change since the baseline.
func writeEvalHeader(out io.Writer, v evalView) {
	p := v.Provenance
	fmt.Fprintf(out, "eval: %s %s, suite version %d (backend %s, image %s, speculation %s, ctx %d, tools mode %s)\n",
		v.Key.Model, v.Key.Quant, v.Key.SuiteVersion, p.Backend, p.ImageDigest, p.Speculation, p.Ctx, onOff[p.ToolsMode])
	fmt.Fprintf(out, "score:    %s\n", scoreLine(&v.Score))
	fmt.Fprintf(out, "baseline: %s\n", scoreLine(v.BaselineScore))
	for _, c := range v.ProvenanceChanges {
		fmt.Fprintf(out, "changed since the baseline: %s %s -> %s\n", c.Field, c.Baseline, c.Now)
	}
}

// writeEvalCases lists each regression, improvement, unconducted and skipped case.
func writeEvalCases(out io.Writer, v evalView) {
	for _, list := range []struct {
		label   string
		results []eval.Result
	}{
		{"regression", v.Regressions},
		{"improvement", v.Improvements},
		{"unconducted", v.Unconducted},
		{"skipped", v.Skipped},
	} {
		for _, r := range list.results {
			fmt.Fprintf(out, "  %-11s  %s: %s\n", list.label, r.CaseID, caseNote(r))
		}
	}
}

// caseNote is why a case was not graded, or the reply it was graded on.
func caseNote(r eval.Result) string {
	if r.Detail != "" {
		return r.Detail
	}
	return fmt.Sprintf("%q", r.Excerpt)
}

// writeEvalRecord reports --record's outcome; a refusal goes to stderr.
func writeEvalRecord(out, errOut io.Writer, v evalView) {
	if v.Record == nil {
		return
	}
	if !v.Record.Written {
		fmt.Fprintf(errOut, "record: refused — %s\n", v.Record.Refusal)
		return
	}
	fmt.Fprintf(out, "record: wrote the eval baseline (%s); it replaces: %s\n", scoreLine(&v.Score), scoreLine(v.Record.Replaces))
}

// scoreLine renders a score, or says none is recorded.
func scoreLine(s *eval.Score) string {
	if s == nil {
		return "none recorded for this model, quant and suite version"
	}
	return fmt.Sprintf("%d passed, %d failed, %d skipped, %d unconducted", s.Passed, s.Failed, s.Skipped, s.Unconducted)
}
