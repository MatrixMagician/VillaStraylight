package main

// install_searxng.go holds the v1.5 WEB-SEARCH (SearXNG) readiness proof the `villa
// install` verb runs when the PERSISTED web_search_enabled is true (Phase-29,
// It is a near-verbatim clone of the shipped install_memory.go readiness-by-real-
// query seam:
//
//   - evalSearxngProof: the PURE verdict core (unit-testable off-hardware via an injected
//     probe). It maps a probe outcome to preflight.StatusPass/StatusFail with a
//     remediation detail — never a WARN (a web-search stack the user opted into that
//     cannot answer a query is a confident known-bad).
//
//   - liveSearxngProof: the production seam reusing the runProbeCurl podman-over-
// villa.network helper VERBATIM (no host port —; helper image from
//     orchestrate.EmbedImage() so no re-typed image literal trips TestSeamGrepGate). It
//     issues a REAL `GET /search?q=…&format=json` query with FIXED args + --data-urlencode
// (no shell interpolation), parses results[], and runs a bounded cold-start
//     retry loop to absorb transient single-engine timeouts before declaring FAIL.
//
// Readiness is the REAL format=json query parsing results[] — NEVER a /healthz 200 (the
// project's "offload-asserting, never liveness" principle; honesty-by-construction). An
// HTTP 200 carrying {"results": []} because every upstream engine timed out is NOT a
// healthy instance and FAILs with remediation (Open Q2).

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// liveLoadedWebSearchEnabled returns the PERSISTED config.LoadVilla().WebSearchEnabled
// the AUTHORITATIVE web-search gate source threaded into runInstall (NOT the
// DefaultVillaConfig() seed, which is false by construction). A config load error fails
// SOFT to false so a broken/absent config never silently enables the search stack (an
// opted-in user must have a readable config). This mirrors liveLoadedMemoryEnabled — the
// gate reflects the user's opt-in, not the seed's hard-coded false.
func liveLoadedWebSearchEnabled() bool {
	c, err := config.LoadVilla()
	if err != nil {
		return false
	}
	return c.WebSearchEnabled
}

// searxngServiceName is the systemd service the villa-searxng .container generates
// (Quadlet maps villa-searxng.container → villa-searxng.service). Its start is gated on
// the rendered unit being present in the written plan and on the PERSISTED
// web_search_enabled; the readiness proof below asserts it actually answers a query.
const searxngServiceName = "villa-searxng.service"

// searxngProbeQuery is the well-known probe query the readiness proof issues. A generic
// phrase a healthy general/reference engine (the keep_only allowlist) reliably
// answers, so a populated results[] is a genuine end-to-end signal (network → searxng →
// upstream engine → parsed JSON), not a contrived empty hit.
const searxngProbeQuery = "villa readiness probe"

// searxngProofRetries is the bounded number of cold-start retries the live proof issues
// before declaring FAIL. A just-started SearXNG instance can return an empty result set
// while its engines warm up / a single engine momentarily times out; a short bounded
// retry absorbs that transient without a false FAIL, and being BOUNDED it still fails
// closed on a persistently empty instance (never an infinite spin).
const searxngProofRetries = 3

// searxngProofRetryDelay is the small fixed delay between cold-start retries.
const searxngProofRetryDelay = 2 * time.Second

// searxngProof is the SearXNG readiness verdict (mirrors memoryProof / installReadiness):
// PASS when the real format=json query returns a genuine answer, FAIL with a remediation
// detail otherwise. There is NO WARN — a web-search stack the user opted into that cannot
// answer a query is a confident known-bad.
type searxngProof struct {
	status preflight.Status
	detail string
}

// searxngProofInput carries the config-resolved SearXNG container-DNS address + in-network
// port the proof probes. These are sourced from config.SearxngAddr/SearxngPort — the
// SAME values the rendered unit's container-DNS identity derives from — so the proof can
// never probe a different target than what actually runs. Values are config-resolved,
// never shell-interpolated.
type searxngProofInput struct {
	searxngAddr string
	searxngPort int
}

// searxngResultItem is one entry of the SearXNG format=json results[] array (a subset of
// the documented fields — url/title/content/engine; the rest are ignored). [VERIFIED:
// docs.searxng.org/dev/search_api]
type searxngResultItem struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Engine  string `json:"engine"`
}

// searxngResult is the top-level SearXNG format=json response shape the proof parses. Only
// the fields the verdict reads are modeled: number_of_results + results[] decide PASS/FAIL;
// unresponsive_engines is carried (as opaque values) so a transient single-engine timeout
// is observable but never, on its own, a FAIL when a real result is present.
type searxngResult struct {
	NumberOfResults     int                 `json:"number_of_results"`
	Results             []searxngResultItem `json:"results"`
	UnresponsiveEngines []any               `json:"unresponsive_engines"`
}

// hasAnswer reports whether a parsed response is a genuine engine answer: at least one
// result OR a positive number_of_results. An all-empty 200 (every engine timed out) is
// NOT an answer (Open Q2). A transient single-engine timeout still yields a non-empty
// Results, so it passes here (tolerated).
func (r searxngResult) hasAnswer() bool {
	return len(r.Results) > 0 || r.NumberOfResults > 0
}

// evalSearxngProof is the PURE proof core (unit-testable off-hardware via an injected
// probe). It runs a BOUNDED cold-start retry loop: it re-issues the probe up to
// searxngProofRetries times, returning PASS as soon as a genuine answer arrives, and
// declaring FAIL only after the bounded retries are exhausted (never an infinite spin).
//
//   - a probe error (endpoint unreachable) on the FINAL attempt → FAIL("did not answer …")
//     naming `systemctl --user status <svc>` + re-run `villa install`.
//   - a parseable response with NO answer (number_of_results == 0 AND len(results) == 0
//     every upstream engine may have timed out) on the FINAL attempt → FAIL("returned no
//     results …") with the engine-allowlist + service remediation.
//   - a parseable response WITH an answer (≥1 result OR number_of_results>0), tolerating a
//     transient single-engine unresponsive_engines entry → PASS.
//
// The retry delay is the caller's concern (the live probe sleeps between calls); the pure
// core only decides how many attempts and the verdict, so it stays fast + deterministic.
func evalSearxngProof(probe func() (searxngResult, error)) searxngProof {
	var lastErr error
	var got searxngResult
	for attempt := 0; attempt < searxngProofRetries; attempt++ {
		got, lastErr = probe()
		if lastErr == nil && got.hasAnswer() {
			return searxngProof{status: preflight.StatusPass, detail: fmt.Sprintf("real format=json query returned %d result(s)", len(got.Results))}
		}
	}
	if lastErr != nil {
		return searxngProof{
			status: preflight.StatusFail,
			detail: fmt.Sprintf("the search service did not answer the format=json probe (%v) — check `systemctl --user status %s` and its journal, then re-run `villa install`", lastErr, searxngServiceName),
		}
	}
	return searxngProof{
		status: preflight.StatusFail,
		detail: fmt.Sprintf("the search service returned no results — every upstream engine may have timed out; check the engine allowlist and `systemctl --user status %s`, then re-run `villa install`", searxngServiceName),
	}
}

// liveSearxngProof is the production proof seam: it reaches the container-DNS-only
// villa-searxng over villa.network via the runProbeCurl one-shot `podman run --rm
// --network villa --entrypoint curl` helper (no host port is opened), sourcing
// the helper image from the orchestrate accessor (EmbedImage(), which ships curl) rather
// than a re-typed image literal (keeps TestSeamGrepGate green). The query is
// issued FIXED-ARG with --data-urlencode for BOTH q= and format=json — the query string is
// NEVER concatenated into the URL (V5 fixed-arg discipline). A small fixed delay
// separates cold-start retries; the verdict is decided by the pure evalSearxngProof.
func liveSearxngProof(ctx context.Context, in searxngProofInput) searxngProof {
	helperImage := orchestrate.EmbedImage()
	url := fmt.Sprintf("http://%s:%d/search", in.searxngAddr, in.searxngPort)

	attempt := 0
	probe := func() (searxngResult, error) {
		// Space the cold-start retries with a small fixed delay (skip before the first
		// attempt). A just-started instance gets a moment to warm before the next probe.
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return searxngResult{}, ctx.Err()
			case <-time.After(searxngProofRetryDelay):
			}
		}
		attempt++
		// Fixed-arg GET with --data-urlencode for q= and format=json — never concatenated
		// into the URL, never a shell. -sf makes curl fail on a non-2xx so an
		// unreachable / erroring endpoint surfaces as a probe error, not a parsed empty.
		out, err := runProbeCurl(ctx, helperImage,
			"-sf", "-G", url,
			"--data-urlencode", "q="+searxngProbeQuery,
			"--data-urlencode", "format=json",
		)
		if err != nil {
			return searxngResult{}, err
		}
		var resp searxngResult
		if jerr := json.Unmarshal(out, &resp); jerr != nil {
			return searxngResult{}, fmt.Errorf("decode format=json response: %w", jerr)
		}
		return resp, nil
	}

	return evalSearxngProof(probe)
}
