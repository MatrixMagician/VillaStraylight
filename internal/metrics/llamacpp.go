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
