package eval

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/verify"
)

var testKey = Key{Model: "m", Quant: "Q4", SuiteVersion: 1}

var testProvenance = Provenance{Backend: "rocm", ImageDigest: "sha256:aaa", Speculation: "off", Ctx: 8192}

// res builds one result.
func res(id string, s CaseStatus) Result {
	return Result{CaseID: id, Status: s, Excerpt: "reply to " + id}
}

// run builds a run over results under testKey and testProvenance.
func run(results ...Result) Run {
	return Run{Key: testKey, Provenance: testProvenance, Results: results}
}

// baseline records results as the eval baseline for testKey.
func baseline(results ...Result) *Baseline {
	b := Baseline(run(results...))
	return &b
}

// ids lists the case ids of results, for compact assertions.
func ids(results []Result) string {
	var out []string
	for _, r := range results {
		out = append(out, r.CaseID)
	}
	return strings.Join(out, ",")
}

// TestCompareVerdicts pins ADR-0018's verdict table: a case that passed in the eval
// baseline and fails now is a regression and any regression is Fail, outranking every
// Reject; no baseline for the key, or any unconducted case, is Reject, never Pass;
// otherwise Pass, with newly passing cases reported as improvements.
func TestCompareVerdicts(t *testing.T) {
	for _, c := range []struct {
		name         string
		run          Run
		baseline     *Baseline
		want         verify.Status
		regressions  string
		improvements string
		unconducted  string
		detail       string
	}{
		{"unchanged is pass", run(res("a", Passed), res("b", Failed)), baseline(res("a", Passed), res("b", Failed)),
			verify.Pass, "", "", "", "no regressions"},
		{"newly passing is an improvement", run(res("a", Passed), res("b", Passed)), baseline(res("a", Passed), res("b", Failed)),
			verify.Pass, "", "b", "", "1 improvement"},
		{"passed then failed is a regression", run(res("a", Failed), res("b", Failed)), baseline(res("a", Passed), res("b", Failed)),
			verify.Fail, "a", "", "", "1 capability case(s) regressed"},
		{"a regression outranks an unconducted case", run(res("a", Failed), res("b", Unconducted)),
			baseline(res("a", Passed), res("b", Passed)), verify.Fail, "a", "", "b", "regressed"},
		{"an unconducted case is reject", run(res("a", Passed), res("b", Unconducted)),
			baseline(res("a", Passed), res("b", Passed)), verify.Reject, "", "", "b", "could not be conducted"},
		{"no baseline is reject", run(res("a", Passed)), nil, verify.Reject, "", "", "", "no eval baseline for m Q4 at suite version 1"},
		{"no baseline and unconducted is still reject", run(res("a", Unconducted)), nil,
			verify.Reject, "", "", "a", "no eval baseline"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rep := Compare(c.run, c.baseline)
			if rep.Status != c.want {
				t.Errorf("status = %v, want %v (%s)", rep.Status, c.want, rep.Detail)
			}
			if got := ids(rep.Regressions); got != c.regressions {
				t.Errorf("regressions = %q, want %q", got, c.regressions)
			}
			if got := ids(rep.Improvements); got != c.improvements {
				t.Errorf("improvements = %q, want %q", got, c.improvements)
			}
			if got := ids(rep.Unconducted); got != c.unconducted {
				t.Errorf("unconducted = %q, want %q", got, c.unconducted)
			}
			if !strings.Contains(rep.Detail, c.detail) {
				t.Errorf("detail = %q, want it to contain %q", rep.Detail, c.detail)
			}
		})
	}
}

// TestCompareRegressionCarriesTheReply: ADR-0018 lists each regression with an
// excerpt of the reply so the operator can judge it.
func TestCompareRegressionCarriesTheReply(t *testing.T) {
	now := Result{CaseID: "a", Status: Failed, Excerpt: "The answer is 41"}
	rep := Compare(run(now), baseline(res("a", Passed)))
	if len(rep.Regressions) != 1 || rep.Regressions[0] != now {
		t.Fatalf("regressions = %+v, want the run's result with its excerpt", rep.Regressions)
	}
}

// TestCompareSkipsWhatEitherSideSkipped: a capability case skipped on either side
// (tools mode off) is listed as skipped and never compared, so a baseline recorded
// with tools mode off can neither fail nor pass a tool case run with it on, and the
// tools-mode change itself is named as a provenance difference.
func TestCompareSkipsWhatEitherSideSkipped(t *testing.T) {
	now := run(res("text", Failed), Result{CaseID: "t1", Status: Skipped, Detail: "tools mode off"}, res("t2", Failed))
	now.Provenance.ToolsMode = false
	prior := baseline(res("text", Failed), res("t1", Passed), Result{CaseID: "t2", Status: Skipped, Detail: "tools mode off"})
	prior.Provenance.ToolsMode = true

	rep := Compare(now, prior)

	if rep.Status != verify.Pass || len(rep.Regressions) != 0 {
		t.Fatalf("status = %v, regressions %v: a skipped case must never be a regression", rep.Status, rep.Regressions)
	}
	if ids(rep.Skipped) != "t1,t2" {
		t.Fatalf("skipped = %q, want t1,t2", ids(rep.Skipped))
	}
	if rep.Skipped[0].Detail != "tools mode off" || rep.Skipped[1].Detail != "skipped in the eval baseline: tools mode off" {
		t.Errorf("skipped details = %q / %q", rep.Skipped[0].Detail, rep.Skipped[1].Detail)
	}
	if len(rep.ProvenanceChanges) != 1 || rep.ProvenanceChanges[0] != (ProvenanceChange{Field: "tools_mode", Baseline: "true", Now: "false"}) {
		t.Errorf("provenance changes = %+v, want only tools_mode true -> false", rep.ProvenanceChanges)
	}
}

// TestCompareNamesEveryProvenanceChange: a baseline belongs to a model, not a stack
// (ADR-0018), so a changed backend, image digest, speculation mode, context or tools
// mode never blocks the comparison; the report names each one that differs.
func TestCompareNamesEveryProvenanceChange(t *testing.T) {
	now := run(res("a", Passed))
	now.Provenance = Provenance{Backend: "vulkan", ImageDigest: "sha256:bbb", Speculation: "ngram", Ctx: 4096, ToolsMode: true}
	rep := Compare(now, baseline(res("a", Passed)))

	if rep.Status != verify.Pass {
		t.Fatalf("status = %v, want pass: provenance is reported, never keyed on", rep.Status)
	}
	var fields []string
	for _, p := range rep.ProvenanceChanges {
		fields = append(fields, p.Field+":"+p.Baseline+">"+p.Now)
	}
	want := "backend:rocm>vulkan,image_digest:sha256:aaa>sha256:bbb,speculation:off>ngram,ctx:8192>4096,tools_mode:false>true"
	if got := strings.Join(fields, ","); got != want {
		t.Errorf("provenance changes = %s, want %s", got, want)
	}
	if none := Compare(run(res("a", Passed)), nil); none.ProvenanceChanges == nil || len(none.ProvenanceChanges) != 0 {
		t.Errorf("with no baseline there is nothing to differ from: %+v", none.ProvenanceChanges)
	}
}

// TestRecordRefusesAnEvalBaselineWithHoles guards ADR-0018's --record rule: a run
// with any unconducted case writes nothing; a complete run (skipped cases are not
// holes) becomes the eval baseline for its key, provenance and results intact.
func TestRecordRefusesAnEvalBaselineWithHoles(t *testing.T) {
	if _, err := Record(run(res("a", Passed), res("b", Unconducted))); err == nil ||
		!strings.Contains(err.Error(), "1 capability case(s) could not be conducted") {
		t.Fatalf("Record err = %v, want a refusal naming the unconducted case count", err)
	}

	r := run(res("a", Passed), res("b", Failed), Result{CaseID: "t", Status: Skipped, Detail: "tools mode off"})
	b, err := Record(r)
	if err != nil {
		t.Fatalf("Record refused a complete run: %v", err)
	}
	if b.Key != r.Key || b.Provenance != r.Provenance || ids(b.Results) != "a,b,t" {
		t.Errorf("recorded %+v, want the run verbatim", b)
	}
}

// TestTallyCountsEachStatus: the score printed beside a run and the eval baseline it
// replaces counts each status once.
func TestTallyCountsEachStatus(t *testing.T) {
	got := Tally([]Result{res("a", Passed), res("b", Passed), res("c", Failed), res("d", Skipped), res("e", Unconducted)})
	if got != (Score{Passed: 2, Failed: 1, Skipped: 1, Unconducted: 1}) {
		t.Errorf("Tally = %+v", got)
	}
}
