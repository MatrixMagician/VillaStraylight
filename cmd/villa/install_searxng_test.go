package main

// install_searxng_test.go drives the Phase-29 SearXNG readiness proof (SRCH-01, SC#2):
// the PURE evalSearxngProof verdict core (unit-testable off-hardware via an injected
// probe) and the install-flow wiring (Task 2). Readiness is the REAL format=json query
// parsing results[] — never a health-200 (the project's offload-asserting, never
// liveness principle). The five Task-1 behaviors are driven through injected probes; the
// Task-2 table drives runInstall via a fake searxngProofFn.

import (
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// searxngProbeResult is the canned (parsed JSON, error) pair an injected probe returns
// to evalSearxngProof per call. retrySeq lets a test simulate a cold-start: the first
// call returns an empty result, a later call returns a populated one.
type searxngProbeResult struct {
	parsed searxngResult
	err    error
}

// fakeSearxngProbe returns a probe closure that yields each queued result in order,
// repeating the last once exhausted (so a bounded retry loop converges on the final
// outcome). It records how many times it was called for the retry assertion.
func fakeSearxngProbe(seq []searxngProbeResult, calls *int) func() (searxngResult, error) {
	i := 0
	return func() (searxngResult, error) {
		*calls++
		r := seq[i]
		if i < len(seq)-1 {
			i++
		}
		return r.parsed, r.err
	}
}

// TestSearxngProofPassOnRealResults: a probe returning parseable JSON with ≥1 result and
// number_of_results>0 → StatusPass. This is the honest signal SC#2 demands — a real
// engine answer, not a health-200.
func TestSearxngProofPassOnRealResults(t *testing.T) {
	calls := 0
	probe := fakeSearxngProbe([]searxngProbeResult{
		{parsed: searxngResult{
			NumberOfResults: 3,
			Results: []searxngResultItem{
				{URL: "https://example.org", Title: "Example", Content: "…", Engine: "duckduckgo"},
			},
		}},
	}, &calls)
	got := evalSearxngProof(probe)
	if got.status != preflight.StatusPass {
		t.Fatalf("evalSearxngProof status = %v, want StatusPass (detail: %q)", got.status, got.detail)
	}
}

// TestSearxngProofFailOnUnreachable: a probe returning an error (endpoint unreachable)
// → StatusFail with a remediation detail naming `systemctl --user status` and re-run
// `villa install` (refuse-with-remediation, never a silent skip).
func TestSearxngProofFailOnUnreachable(t *testing.T) {
	calls := 0
	probe := fakeSearxngProbe([]searxngProbeResult{
		{err: errors.New("connection refused")},
	}, &calls)
	got := evalSearxngProof(probe)
	if got.status != preflight.StatusFail {
		t.Fatalf("evalSearxngProof status = %v, want StatusFail", got.status)
	}
	if !strings.Contains(got.detail, "systemctl --user status") || !strings.Contains(got.detail, "villa install") {
		t.Errorf("FAIL detail %q must name `systemctl --user status` and re-run `villa install`", got.detail)
	}
	if !strings.Contains(got.detail, searxngServiceName) {
		t.Errorf("FAIL detail %q must name the searxng service %q", got.detail, searxngServiceName)
	}
}

// TestSearxngProofFailOnAllEmpty: HTTP 200 but {"results": []} with every engine in
// unresponsive_engines → StatusFail. An all-empty result set is NOT a healthy instance
// (Open Q2): a 200 that carries no answer because every upstream engine timed out must
// fail closed, never a false-green.
func TestSearxngProofFailOnAllEmpty(t *testing.T) {
	calls := 0
	probe := fakeSearxngProbe([]searxngProbeResult{
		{parsed: searxngResult{
			NumberOfResults:     0,
			Results:             nil,
			UnresponsiveEngines: []any{[]any{"duckduckgo", "timeout"}, []any{"brave", "timeout"}},
		}},
	}, &calls)
	got := evalSearxngProof(probe)
	if got.status != preflight.StatusFail {
		t.Fatalf("evalSearxngProof status = %v, want StatusFail on an all-empty 200", got.status)
	}
	if !strings.Contains(got.detail, "no results") {
		t.Errorf("FAIL detail %q should explain the empty result set", got.detail)
	}
}

// TestSearxngProofToleratesTransientEngineFailure: one engine in unresponsive_engines but
// ≥1 result present → StatusPass. A real instance routinely has a single engine time out;
// that must NOT be a FAIL as long as the query still returned a genuine answer.
func TestSearxngProofToleratesTransientEngineFailure(t *testing.T) {
	calls := 0
	probe := fakeSearxngProbe([]searxngProbeResult{
		{parsed: searxngResult{
			NumberOfResults: 2,
			Results: []searxngResultItem{
				{URL: "https://example.org", Title: "Example", Engine: "wikipedia"},
			},
			UnresponsiveEngines: []any{[]any{"brave", "timeout"}},
		}},
	}, &calls)
	got := evalSearxngProof(probe)
	if got.status != preflight.StatusPass {
		t.Fatalf("evalSearxngProof status = %v, want StatusPass with a transient single-engine timeout", got.status)
	}
}

// TestSearxngProofColdStartRetry: a first-call empty result followed by a populated
// retry → PASS. The bounded cold-start retry absorbs a transient just-started instance
// (engines still warming) without declaring a false FAIL, and re-issues the probe more
// than once.
func TestSearxngProofColdStartRetry(t *testing.T) {
	calls := 0
	probe := fakeSearxngProbe([]searxngProbeResult{
		{parsed: searxngResult{NumberOfResults: 0}}, // cold start: empty
		{parsed: searxngResult{ // warmed: a real answer
			NumberOfResults: 1,
			Results:         []searxngResultItem{{URL: "https://example.org", Engine: "duckduckgo"}},
		}},
	}, &calls)
	got := evalSearxngProof(probe)
	if got.status != preflight.StatusPass {
		t.Fatalf("evalSearxngProof status = %v, want StatusPass after a cold-start retry", got.status)
	}
	if calls < 2 {
		t.Errorf("expected the cold-start retry to re-issue the probe (calls=%d, want ≥2)", calls)
	}
}

// TestSearxngProofRetryGivesUp: a persistently empty instance is declared FAIL after the
// bounded retries are exhausted (the retry is bounded, not infinite).
func TestSearxngProofRetryGivesUp(t *testing.T) {
	calls := 0
	probe := fakeSearxngProbe([]searxngProbeResult{
		{parsed: searxngResult{NumberOfResults: 0}}, // every call empty
	}, &calls)
	got := evalSearxngProof(probe)
	if got.status != preflight.StatusFail {
		t.Fatalf("evalSearxngProof status = %v, want StatusFail after exhausting retries", got.status)
	}
	if calls < 2 {
		t.Errorf("expected the bounded retry to re-issue the probe before giving up (calls=%d, want ≥2)", calls)
	}
}
