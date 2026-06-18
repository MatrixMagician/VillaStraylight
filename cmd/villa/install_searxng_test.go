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

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// searxngUnits builds a realistic web-search-on plan: Render appends the searxng
// .container unit when WebSearchEnabled, so it MUST be present in the plan for the WR-04
// start guard (which gates the searxng start on the unit actually being in the plan).
func searxngUnits() ([]orchestrate.Unit, orchestrate.Plan) {
	units := []orchestrate.Unit{
		{Name: "villa-llama.container", Text: "[Container]\n"},
		{Name: orchestrate.SearXNGContainerUnitName(), Text: "[Container]\n"},
	}
	return units, orchestrate.Plan{Changed: units}
}

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

// TestInstallWebSearchWiring is the Task-2 install-flow gate: with web search ON, install
// writes BOTH config artifacts (settings.yml + the 0600 secret env) BEFORE starting the
// searxng service, then runs the readiness proof and folds a PASS into success / refuses
// a FAIL with exitBlocked. With web search OFF, NONE of the searxng seams fire (the
// web-search-off install is byte-identical — no start, no proof, no writes).
func TestInstallWebSearchWiring(t *testing.T) {
	t.Run("web search on: writes config + starts + proves, PASS folds into success", func(t *testing.T) {
		units, plan := searxngUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.webSearchEnabled = true
		f.searxngProofStatus = preflight.StatusPass

		cmd, _, errOut := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("web-search-on install exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if f.searxngSettingsCalls != 1 {
			t.Errorf("settings.yml must be written once, writeSearxngSettings calls = %d", f.searxngSettingsCalls)
		}
		if f.searxngSecretEnvCalls != 1 {
			t.Errorf("the 0600 secret env file must be written once, writeSearxngSecretEnv calls = %d", f.searxngSecretEnvCalls)
		}
		if !contains(f.startOrder, searxngServiceName) {
			t.Errorf("web-search-on must start %s, startOrder = %v", searxngServiceName, f.startOrder)
		}
		if f.searxngProofCalls != 1 {
			t.Errorf("web-search-on must run the readiness proof once, proof calls = %d", f.searxngProofCalls)
		}
		// BOTH config files must be written BEFORE the searxng service starts (Pitfall 3:
		// the container reads its settings.yml + secret on first boot).
		startIdx := indexOf(f.callOrder, "start:"+searxngServiceName)
		if startIdx < 0 {
			t.Fatalf("searxng start not recorded in callOrder = %v", f.callOrder)
		}
		if si := indexOf(f.callOrder, "writeSearxngSettings"); si < 0 || si > startIdx {
			t.Errorf("settings.yml must be written BEFORE the searxng start (settings idx=%d, start idx=%d); callOrder = %v", si, startIdx, f.callOrder)
		}
		if ei := indexOf(f.callOrder, "writeSearxngSecretEnv"); ei < 0 || ei > startIdx {
			t.Errorf("the secret env file must be written BEFORE the searxng start (env idx=%d, start idx=%d); callOrder = %v", ei, startIdx, f.callOrder)
		}
		// The proof probes the config-resolved addr/port (WR-01).
		if f.searxngProofIn.searxngAddr == "" || f.searxngProofIn.searxngPort == 0 {
			t.Errorf("the proof must probe the config-resolved addr/port, got %+v", f.searxngProofIn)
		}
	})

	t.Run("web search on: proof FAIL refuses with exitBlocked", func(t *testing.T) {
		units, plan := searxngUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.webSearchEnabled = true
		f.searxngProofStatus = preflight.StatusFail
		f.searxngProofDetail = "the search service did not answer"

		cmd, _, errOut := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitBlocked {
			t.Fatalf("a searxng proof FAIL must return exitBlocked, got %d; stderr = %q", code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "search service not ready") {
			t.Errorf("the FAIL must refuse-with-remediation naming the search service; stderr = %q", errOut.String())
		}
	})

	t.Run("web search on but unit absent from plan: fails closed (WR-04)", func(t *testing.T) {
		// A web-search-on install whose searxng unit is NOT in the rendered plan must fail
		// closed with an INTERNAL-ERROR remediation — never `systemctl start` a unit systemd
		// has never seen (T-29-13).
		units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
		plan := orchestrate.Plan{Changed: units}
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.webSearchEnabled = true

		cmd, _, errOut := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitBlocked {
			t.Fatalf("a missing searxng unit must fail closed (exitBlocked), got %d; stderr = %q", code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "INTERNAL ERROR") {
			t.Errorf("a missing searxng unit must surface an INTERNAL-ERROR remediation; stderr = %q", errOut.String())
		}
		if contains(f.startOrder, searxngServiceName) {
			t.Errorf("a unit absent from the plan must NOT be started, startOrder = %v", f.startOrder)
		}
		if f.searxngProofCalls != 0 {
			t.Errorf("a fail-closed start gate must not reach the proof, proof calls = %d", f.searxngProofCalls)
		}
	})

	t.Run("web search off: no start, no proof, no config writes (byte-identical path)", func(t *testing.T) {
		units, plan := searxngUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.webSearchEnabled = false

		cmd, _, errOut := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("web-search-off install exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if f.searxngSettingsCalls != 0 || f.searxngSecretEnvCalls != 0 {
			t.Errorf("web-search-off must write no config files, settings=%d env=%d", f.searxngSettingsCalls, f.searxngSecretEnvCalls)
		}
		if contains(f.startOrder, searxngServiceName) {
			t.Errorf("web-search-off must not start the searxng service, startOrder = %v", f.startOrder)
		}
		if f.searxngProofCalls != 0 {
			t.Errorf("web-search-off must not run the readiness proof, proof calls = %d", f.searxngProofCalls)
		}
	})
}
