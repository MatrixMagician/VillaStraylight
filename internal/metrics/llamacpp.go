// Package metrics is the VillaStraylight performance collector (Phase-5 DASH-02):
// a bounded loopback scrape of llama-server's /metrics (Prometheus text) and /slots
// (JSON) that feeds the dashboard Performance panel.
//
// Two RESEARCH corrections are baked in:
//   - the old KV-cache-usage ratio gauge NO LONGER EXISTS in current llama.cpp
//     /metrics (Pitfall 4 / A1) — it is never queried; the KV/context signal is
//     derived from /slots instead.
//   - the tok/s gauges are last-window snapshots, NOT a live "is generating?" signal
//     (Pitfall 3) — callers gate "live?" on IsGenerating (requests_processing>0 OR an
//     is_processing slot) and render "Idle — no active generation." otherwise (D-10).
//
// Every scrape is bounded by io.LimitReader (T-05-07) and degrades to a typed-Unknown
// (ok=false), never a fabricated zero presented as a real reading (D-11).
package metrics

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// scrapeTimeout bounds a single /metrics or /slots GET so a hung llama-server can
// never stall the dashboard poll loop.
const scrapeTimeout = 2 * time.Second

// maxScrapeBody bounds each response body (T-05-07 memory-exhaustion guard), mirroring
// the 64 KiB cap liveOpenWebUIHealth uses in cmd/villa/status.go.
const maxScrapeBody = 64 << 10

// PerfSnapshot is the subset of llama-server /metrics gauges the Performance panel
// reads. Every field is a last-window gauge value; a snapshot is only meaningful as a
// live rate when IsGenerating reports true (the gauges are stale snapshots when idle —
// Pitfall 3). The removed KV-cache-usage ratio gauge is deliberately absent.
type PerfSnapshot struct {
	// PromptTokensPerSec is llamacpp:prompt_tokens_seconds — average prompt-eval
	// throughput; it backs the prompt tok/s figure and the prompt-eval latency caption.
	PromptTokensPerSec float64
	// GenTokensPerSec is llamacpp:predicted_tokens_seconds — average generation throughput.
	GenTokensPerSec float64
	// RequestsProcessing is llamacpp:requests_processing — the live "is generating?"
	// gauge folded into IsGenerating.
	RequestsProcessing float64
	// RequestsDeferred is llamacpp:requests_deferred — queue depth (informational).
	RequestsDeferred float64
}

// mPromptTokensTotal and mPredictedTokensTotal are the two monotonic cumulative
// counter NAME literals (declared "Counter:" in llama.cpp's tools/server/README.md).
// These are the ONLY new metric literals introduced for the cumulative-usage feature
// (USAGE-01) and — like the existing gauge names above — are confined to this package
// per D-06's single-home discipline (enforced by the grep gate in 15-VALIDATION.md).
const (
	mPromptTokensTotal    = "llamacpp:prompt_tokens_total"
	mPredictedTokensTotal = "llamacpp:tokens_predicted_total"
)

// CounterSample is the cumulative-usage counterpart to PerfSnapshot: the two monotonic
// _total counters the fold (Plan 01) accumulates from. Counters are a DIFFERENT category
// from the rate gauges (those are last-window snapshots, Pitfall 3), so they live in a
// sibling struct rather than widening PerfSnapshot.
//
// Each value carries its own typed-Unknown bool: an absent or unparseable _total line
// yields Known=false with the total left at its zero value (D-05) — the caller MUST gate
// on Known and never present the bare 0 as a real reading. Values are uint64 (counts are
// exact integers; the float64→uint64 narrowing is lossless below 2^53, Pitfall 3).
type CounterSample struct {
	// PromptTokensTotal is llamacpp:prompt_tokens_total; valid only when PromptTokensKnown.
	PromptTokensTotal uint64
	// PromptTokensKnown is the typed-Unknown signal for PromptTokensTotal (D-05).
	PromptTokensKnown bool
	// PredictedTokensTotal is llamacpp:tokens_predicted_total; valid only when PredictedTokensKnown.
	PredictedTokensTotal uint64
	// PredictedTokensKnown is the typed-Unknown signal for PredictedTokensTotal (D-05).
	PredictedTokensKnown bool
}

// maxCounterValue is the inclusive upper bound a parsed counter may take before it is
// rejected as typed-Unknown. It is 2^53, the largest integer a float64 represents
// EXACTLY: above it the strconv.ParseFloat → uint64 narrowing has already silently lost
// integer precision (two distinct counts could fold identically), so a value over this
// bound is not a trustworthy count and is dropped rather than folded into the durable
// total (D-05; defends the CounterSample "lossless below 2^53" claim).
const maxCounterValue = 1 << 53

// counterFromMap reads one cumulative counter out of a parsePromText map via its presence
// signal: ok=false (absent / unparseable line) ⇒ Known=false with a zero total, never a
// fabricated 0 presented as a real count (D-05). It then EXPLICITLY rejects every value
// that cannot be a trustworthy non-negative exact integer — NaN, ±Inf, negative, and any
// value above maxCounterValue (2^53) — by returning the typed-Unknown branch. This matters
// because `NaN < 0` and `+Inf < 0` are both false, so a bare `v < 0` guard would narrow
// uint64(NaN)/uint64(+Inf) (implementation-specific garbage) into a fabricated durable
// count that the additive, reset-aware fold would then make permanent (D-05). Only a finite,
// non-negative, exactly-representable value is narrowed to uint64 and returned Known.
func counterFromMap(m map[string]float64, name string) (uint64, bool) {
	v, ok := m[name]
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > maxCounterValue {
		return 0, false // typed-Unknown, never a fabricated count (D-05)
	}
	return uint64(v), true
}

// Slot is the NARROW view of one /slots element. It deliberately reads ONLY
// non-sensitive fields — id, n_ctx, is_processing, and next_token.{n_decoded,n_remain}
// — and NEVER the prompt or sampling params, which /slots may include (T-05-08:
// no prompt leakage into the perf panel). Adding a prompt/params field here would be a
// security regression; a structural test asserts the field set.
type Slot struct {
	ID           int  `json:"id"`
	NCtx         int  `json:"n_ctx"`
	IsProcessing bool `json:"is_processing"`
	NextToken    struct {
		NDecoded int `json:"n_decoded"`
		NRemain  int `json:"n_remain"`
	} `json:"next_token"`
}

// parsePromText is the ~stdlib Prometheus-text parser (RESEARCH Pattern 3, no
// prometheus/common dependency). It splits on newlines, skips blank and #-prefixed
// (HELP/TYPE) lines, cuts "name value" on the first space, and ParseFloats the value.
// Unparseable lines are silently skipped — a malformed gauge never panics.
//
// Any Prometheus label block ("{slot=\"0\",…}") is stripped from the metric name before
// keying so a labeled series (e.g. `llamacpp:foo{slot="0"} 1`) is keyed under the bare
// metric name `llamacpp:foo` and found by the unlabeled lookup — rather than silently
// missed and presented as a real 0.0 rate (WR-02 / D-11). The targeted llama.cpp gauges
// are unlabeled in practice; this only hardens against a labeled emission.
func parsePromText(body string) map[string]float64 {
	out := map[string]float64{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, valStr, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		// Strip any "{label=...}" block so a labeled series keys on the bare metric name.
		if i := strings.IndexByte(name, '{'); i >= 0 {
			name = name[:i]
		}
		if name == "" {
			continue
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(valStr), 64); err == nil {
			out[name] = v
		}
	}
	return out
}

// ScrapeMetrics fetches endpoint+"/metrics" with a bounded client+body and maps the
// CONFIRMED current gauges into a PerfSnapshot. A transport error or a non-200 (a 404
// is the state when --metrics is absent from llamaServerFlags) yields (zero, false) —
// a typed-Unknown the panel renders as "unavailable", NEVER a zero rate shown as real
// (Pitfall 2 / D-11). It never queries the removed KV-cache-usage gauge (Pitfall 4).
func ScrapeMetrics(endpoint string) (PerfSnapshot, bool) {
	client := &http.Client{Timeout: scrapeTimeout}
	resp, err := client.Get(endpoint + "/metrics")
	if err != nil {
		return PerfSnapshot{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return PerfSnapshot{}, false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxScrapeBody))
	m := parsePromText(string(body))
	return PerfSnapshot{
		PromptTokensPerSec: m["llamacpp:prompt_tokens_seconds"],
		GenTokensPerSec:    m["llamacpp:predicted_tokens_seconds"],
		RequestsProcessing: m["llamacpp:requests_processing"],
		RequestsDeferred:   m["llamacpp:requests_deferred"],
	}, true
}

// ScrapeCounters is the cumulative-usage sibling of ScrapeMetrics: it surfaces the two
// monotonic _total counters as a typed-Unknown CounterSample, reusing the SAME bounded
// request shape (scrapeTimeout client + maxScrapeBody LimitReader + parsePromText) — it
// adds NO second HTTP request and NO new endpoint/host literal (D-12; T-15-06/T-15-08).
//
// A transport error or non-200 (a 404 is the state when --metrics is absent) yields
// (zero, false): the whole scrape is unavailable. On a 200 body, the availability bool is
// true and each counter's own Known bool reflects its presence in the parsed map — an
// absent counter degrades to Known=false, never a fabricated 0 (D-05 / T-15-07).
func ScrapeCounters(endpoint string) (CounterSample, bool) {
	client := &http.Client{Timeout: scrapeTimeout}
	resp, err := client.Get(endpoint + "/metrics")
	if err != nil {
		return CounterSample{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CounterSample{}, false
	}
	// Read one byte past the cap so an over-cap body is DETECTED as truncated rather than
	// silently parsed. A read error (e.g. a connection reset mid-body) or a body exceeding
	// maxScrapeBody can sever a counter line mid-value (`...predicted_total 1305` from
	// `130572`); that smaller-but-parseable number would be folded by the reset-aware
	// foldCounter as a COUNTER RESET, durably corrupting the cumulative total. Refuse the
	// whole sample as unavailable instead of folding a partial read (D-05: no false data).
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxScrapeBody+1))
	if err != nil || len(body) > maxScrapeBody {
		return CounterSample{}, false
	}
	m := parsePromText(string(body))
	prompt, promptKnown := counterFromMap(m, mPromptTokensTotal)
	predicted, predictedKnown := counterFromMap(m, mPredictedTokensTotal)
	return CounterSample{
		PromptTokensTotal:    prompt,
		PromptTokensKnown:    promptKnown,
		PredictedTokensTotal: predicted,
		PredictedTokensKnown: predictedKnown,
	}, true
}

// parseSlots unmarshals a /slots body into the narrow []Slot view. Because Slot only
// declares the non-sensitive fields, json.Unmarshal discards prompt/params even when
// the wire body includes them (T-05-08). A malformed body yields (nil, false).
func parseSlots(body []byte) ([]Slot, bool) {
	var slots []Slot
	if err := json.Unmarshal(body, &slots); err != nil {
		return nil, false
	}
	return slots, true
}

// ScrapeSlots fetches endpoint+"/slots" (default-on; no flag needed) with a bounded
// client+body and returns the narrow []Slot view. A transport error / non-200 (e.g.
// --no-slots) → (nil, false), a typed-Unknown the panel renders without fabricating an
// active count.
func ScrapeSlots(endpoint string) ([]Slot, bool) {
	client := &http.Client{Timeout: scrapeTimeout}
	resp, err := client.Get(endpoint + "/slots")
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxScrapeBody))
	return parseSlots(body)
}

// ActiveSlots counts the processing slots (the active-generation count the panel shows).
func ActiveSlots(slots []Slot) int {
	n := 0
	for _, s := range slots {
		if s.IsProcessing {
			n++
		}
	}
	return n
}

// IsGenerating folds the metrics + slots into the single "is anything generating?" gate
// the dashboard uses to choose between live tok/s and "Idle — no active generation."
// (Pitfall 3 / D-10). It is true when requests_processing>0 OR any slot is processing;
// when false the tok/s gauges are stale snapshots and MUST NOT be presented as a live rate.
func IsGenerating(snap PerfSnapshot, slots []Slot) bool {
	if snap.RequestsProcessing > 0 {
		return true
	}
	return ActiveSlots(slots) > 0
}
