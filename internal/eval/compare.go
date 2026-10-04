package eval

// compare.go turns a run and its eval baseline into a verdict through the verify
// family's statuses (ADR-0018), so eval cannot drift onto its own codes: a
// regression is Fail and outranks everything; no baseline, or any unconducted case,
// is Reject; otherwise Pass.

import (
	"fmt"
	"strconv"

	"github.com/MatrixMagician/VillaStraylight/internal/verify"
)

// Report is the comparison of one run with its eval baseline. Each list holds the
// run's results, so a regression carries the reply excerpt the operator judges it by.
type Report struct {
	Status verify.Status
	// Detail is the one-line explanation of Status.
	Detail string
	// Regressions passed in the baseline and fail now; Improvements the reverse.
	Regressions  []Result
	Improvements []Result
	// Unconducted could not be run now. Skipped were skipped now or in the baseline,
	// and are listed rather than compared.
	Unconducted []Result
	Skipped     []Result
	// ProvenanceChanges names each provenance field that differs from the baseline's.
	ProvenanceChanges []ProvenanceChange
}

// ProvenanceChange is one provenance field that differs between the eval baseline
// and the run.
type ProvenanceChange struct {
	Field    string `json:"field"`
	Baseline string `json:"baseline"`
	Now      string `json:"now"`
}

// bucket is where one case lands in a Report.
type bucket int

const (
	unchanged bucket = iota
	regression
	improvement
	unconducted
	skipped
)

// transitions maps a (baseline, now) pair of conducted statuses onto its bucket; any
// pair not listed is unchanged.
var transitions = map[[2]CaseStatus]bucket{
	{Passed, Failed}: regression,
	{Failed, Passed}: improvement,
}

// Compare compares a run with the eval baseline recorded for run.Key, or with nil
// when none is recorded.
func Compare(run Run, baseline *Baseline) Report {
	rep := Report{Regressions: []Result{}, Improvements: []Result{}, Unconducted: []Result{},
		Skipped: []Result{}, ProvenanceChanges: []ProvenanceChange{}}
	prior := map[string]Result{}
	if baseline != nil {
		prior = index(baseline.Results)
		rep.ProvenanceChanges = provenanceChanges(baseline.Provenance, run.Provenance)
	}
	lists := map[bucket]*[]Result{regression: &rep.Regressions, improvement: &rep.Improvements,
		unconducted: &rep.Unconducted, skipped: &rep.Skipped}
	for _, now := range run.Results {
		was := prior[now.CaseID]
		if list, ok := lists[bucketOf(was, now)]; ok {
			*list = append(*list, skipNote(was, now))
		}
	}
	rep.Status, rep.Detail = verdict(rep, run.Key, baseline != nil)
	return rep
}

// bucketOf classifies one case. Unconducted now wins; a case skipped on either side
// is never compared; otherwise the transition table decides.
func bucketOf(was, now Result) bucket {
	if now.Status == Unconducted {
		return unconducted
	}
	if now.Status == Skipped || was.Status == Skipped {
		return skipped
	}
	return transitions[[2]CaseStatus{was.Status, now.Status}]
}

// skipNote explains a case that was conducted now but skipped in the baseline.
func skipNote(was, now Result) Result {
	if was.Status == Skipped && now.Status != Skipped {
		now.Detail = "skipped in the eval baseline: " + was.Detail
	}
	return now
}

// verdict resolves the report's status and its explanation.
func verdict(rep Report, key Key, hasBaseline bool) (verify.Status, string) {
	switch {
	case len(rep.Regressions) > 0:
		return verify.Fail, fmt.Sprintf("%d capability case(s) regressed against the eval baseline", len(rep.Regressions))
	case !hasBaseline:
		return verify.Reject, fmt.Sprintf("no eval baseline for %s %s at suite version %d — record one with `villa eval --record`",
			key.Model, key.Quant, key.SuiteVersion)
	case len(rep.Unconducted) > 0:
		return verify.Reject, fmt.Sprintf("%d capability case(s) could not be conducted, so the comparison is incomplete",
			len(rep.Unconducted))
	}
	return verify.Pass, fmt.Sprintf("no regressions against the eval baseline (%d improvement(s))", len(rep.Improvements))
}

// index maps results by case id.
func index(results []Result) map[string]Result {
	m := make(map[string]Result, len(results))
	for _, r := range results {
		m[r.CaseID] = r
	}
	return m
}

// provenanceChanges lists each provenance field that differs, in a fixed order.
func provenanceChanges(was, now Provenance) []ProvenanceChange {
	changes := []ProvenanceChange{}
	for _, f := range []ProvenanceChange{
		{"backend", was.Backend, now.Backend},
		{"image_digest", was.ImageDigest, now.ImageDigest},
		{"speculation", was.Speculation, now.Speculation},
		{"ctx", strconv.Itoa(was.Ctx), strconv.Itoa(now.Ctx)},
		{"tools_mode", strconv.FormatBool(was.ToolsMode), strconv.FormatBool(now.ToolsMode)},
	} {
		if f.Baseline != f.Now {
			changes = append(changes, f)
		}
	}
	return changes
}

// Record accepts a run as the eval baseline for its key. It refuses a run with any
// unconducted case: a recorded baseline holds every case's pass or fail (or a skip),
// never a hole. Writing it is the caller's.
func Record(run Run) (Baseline, error) {
	if n := Tally(run.Results).Unconducted; n > 0 {
		return Baseline{}, fmt.Errorf("eval: %d capability case(s) could not be conducted; an eval baseline with holes "+
			"is refused and nothing was written", n)
	}
	return Baseline(run), nil
}

// Score counts a run's or an eval baseline's results by status.
type Score struct {
	Passed      int `json:"passed"`
	Failed      int `json:"failed"`
	Skipped     int `json:"skipped"`
	Unconducted int `json:"unconducted"`
}

// Tally scores results.
func Tally(results []Result) Score {
	n := map[CaseStatus]int{}
	for _, r := range results {
		n[r.Status]++
	}
	return Score{Passed: n[Passed], Failed: n[Failed], Skipped: n[Skipped], Unconducted: n[Unconducted]}
}
