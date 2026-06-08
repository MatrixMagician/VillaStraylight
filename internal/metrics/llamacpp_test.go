package metrics

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestParsePromTextExtractsGauges asserts the line-splitter pulls the four
// confirmed llamacpp:* gauges out of a representative /metrics body and SKIPS the
// # HELP / # TYPE comment lines (RESEARCH Pattern 3).
func TestParsePromTextExtractsGauges(t *testing.T) {
	body, err := os.ReadFile("testdata/metrics.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	m := parsePromText(string(body))

	want := map[string]float64{
		"llamacpp:prompt_tokens_seconds":    152.5,
		"llamacpp:predicted_tokens_seconds": 41.25,
		"llamacpp:requests_processing":      1,
		"llamacpp:requests_deferred":        0,
		"llamacpp:n_decode_total":           8421,
		"llamacpp:prompt_tokens_total":      130572,
		"llamacpp:tokens_predicted_total":   48913,
	}
	for k, v := range want {
		if got, ok := m[k]; !ok || got != v {
			t.Errorf("parsePromText[%q] = %v (ok=%v), want %v", k, got, ok, v)
		}
	}
	// Comment lines must never become keys.
	for k := range m {
		if strings.HasPrefix(k, "#") {
			t.Errorf("parsePromText leaked a comment line as key %q", k)
		}
	}
}

// TestParsePromTextStripsLabels is the WR-02 guard: a labeled series
// (`llamacpp:foo{slot="0"} 1`) must be keyed under its bare metric name so the unlabeled
// lookup finds it, rather than silently missing and presenting a fabricated 0.0 rate.
func TestParsePromTextStripsLabels(t *testing.T) {
	body := strings.Join([]string{
		`# HELP llamacpp:prompt_tokens_seconds prompt throughput`,
		`# TYPE llamacpp:prompt_tokens_seconds gauge`,
		`llamacpp:prompt_tokens_seconds{slot="0"} 152.5`,
		`llamacpp:predicted_tokens_seconds{slot="0",model="qwen3"} 41.25`,
		`llamacpp:requests_processing 1`,
	}, "\n")

	m := parsePromText(body)

	want := map[string]float64{
		"llamacpp:prompt_tokens_seconds":    152.5,
		"llamacpp:predicted_tokens_seconds": 41.25,
		"llamacpp:requests_processing":      1,
	}
	for k, v := range want {
		if got, ok := m[k]; !ok || got != v {
			t.Errorf("labeled parse[%q] = %v (ok=%v), want %v", k, got, ok, v)
		}
	}
	// The raw labeled key must NOT survive (it would be a silent miss on the bare lookup).
	for k := range m {
		if strings.ContainsRune(k, '{') {
			t.Errorf("parsePromText leaked a labeled key %q — labels must be stripped (WR-02)", k)
		}
	}
}

// TestScrapeMetricsFromServer asserts a 200 /metrics body maps the confirmed gauges
// into a PerfSnapshot and never queries the removed KV-cache-usage gauge (Pitfall 4).
func TestScrapeMetricsFromServer(t *testing.T) {
	body, err := os.ReadFile("testdata/metrics.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	snap, ok := ScrapeMetrics(srv.URL)
	if !ok {
		t.Fatalf("ScrapeMetrics ok=false on a 200 body")
	}
	if snap.PromptTokensPerSec != 152.5 {
		t.Errorf("PromptTokensPerSec = %v, want 152.5", snap.PromptTokensPerSec)
	}
	if snap.GenTokensPerSec != 41.25 {
		t.Errorf("GenTokensPerSec = %v, want 41.25", snap.GenTokensPerSec)
	}
	if snap.RequestsProcessing != 1 {
		t.Errorf("RequestsProcessing = %v, want 1", snap.RequestsProcessing)
	}
	if snap.RequestsDeferred != 0 {
		t.Errorf("RequestsDeferred = %v, want 0", snap.RequestsDeferred)
	}
}

// TestScrapeMetrics404IsTypedUnknown is the Pitfall 2 / D-11 guard: a 404 /metrics
// (the state when --metrics is absent) yields ok=false and a ZERO-VALUE snapshot the
// handler renders as "unavailable" — never a fabricated/zero rate presented as real.
func TestScrapeMetrics404IsTypedUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // 404 — /metrics absent
	}))
	defer srv.Close()

	snap, ok := ScrapeMetrics(srv.URL)
	if ok {
		t.Fatalf("ScrapeMetrics ok=true on a 404, want false (typed-Unknown)")
	}
	if snap != (PerfSnapshot{}) {
		t.Errorf("ScrapeMetrics 404 snapshot = %+v, want zero-value (ok=false carries unavailability)", snap)
	}
}

// TestScrapeMetricsTransportErrorIsTypedUnknown asserts an unreachable endpoint
// degrades to ok=false rather than panicking or returning zeros as a real reading.
func TestScrapeMetricsTransportErrorIsTypedUnknown(t *testing.T) {
	// 127.0.0.1:1 is the discard port — nothing listens; connect fails fast.
	if _, ok := ScrapeMetrics("http://127.0.0.1:1"); ok {
		t.Fatalf("ScrapeMetrics ok=true on a transport error, want false")
	}
}

// TestScrapeCountersTotal is the USAGE-01 / D-06 counter feed guard. The present case
// asserts the two monotonic cumulative counters (llamacpp:prompt_tokens_total and
// llamacpp:tokens_predicted_total) read out of the bounded /metrics scrape as typed
// uint64 readings with Known=true. The absent case is the D-05 typed-Unknown discipline:
// a body WITHOUT the two _total lines yields Known=false (NOT a fabricated 0), and a 404
// degrades the whole-scrape availability bool to false.
func TestScrapeCountersTotal(t *testing.T) {
	body, err := os.ReadFile("testdata/metrics.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// Present: the fixture carries both _total counters.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cs, ok := ScrapeCounters(srv.URL)
	if !ok {
		t.Fatalf("ScrapeCounters ok=false on a 200 body")
	}
	if !cs.PromptTokensKnown || cs.PromptTokensTotal != 130572 {
		t.Errorf("PromptTokensTotal = %d (known=%v), want 130572 (known=true)", cs.PromptTokensTotal, cs.PromptTokensKnown)
	}
	if !cs.PredictedTokensKnown || cs.PredictedTokensTotal != 48913 {
		t.Errorf("PredictedTokensTotal = %d (known=%v), want 48913 (known=true)", cs.PredictedTokensTotal, cs.PredictedTokensKnown)
	}

	// Absent: a body without the two _total lines → Known=false, never a fabricated 0 (D-05).
	absentBody := strings.Join([]string{
		`# TYPE llamacpp:requests_processing gauge`,
		`llamacpp:requests_processing 0`,
	}, "\n")
	absentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(absentBody))
	}))
	defer absentSrv.Close()

	cs2, ok2 := ScrapeCounters(absentSrv.URL)
	if !ok2 {
		t.Fatalf("ScrapeCounters ok=false on a 200 body (absent counters is still an available scrape)")
	}
	if cs2.PromptTokensKnown {
		t.Errorf("PromptTokensKnown=true on an absent counter, want false (typed-Unknown, no fabricated 0)")
	}
	if cs2.PredictedTokensKnown {
		t.Errorf("PredictedTokensKnown=true on an absent counter, want false (typed-Unknown, no fabricated 0)")
	}
	if cs2.PromptTokensTotal != 0 || cs2.PredictedTokensTotal != 0 {
		t.Errorf("absent CounterSample carries non-zero totals %+v — Known=false MUST gate the zero value", cs2)
	}

	// A 404 /metrics (--metrics absent) degrades the availability bool to false.
	down := httptest.NewServer(http.HandlerFunc(http.NotFound))
	defer down.Close()
	if _, ok := ScrapeCounters(down.URL); ok {
		t.Errorf("ScrapeCounters ok=true on a 404, want false (whole-scrape unavailable)")
	}
}

// TestScrapeCountersOversizedBodyUnavailable proves a /metrics body exceeding the scrape
// cap is treated as UNAVAILABLE rather than parsed. A body truncated mid-line by the cap
// can sever a counter value (e.g. `...predicted_total 1305` from `130572`); the reset-aware
// fold would mis-read the smaller-but-parseable value as a counter reset and durably
// corrupt the cumulative total (v1.2 review finding). Refusing the whole over-cap sample is
// the no-false-data posture (D-05).
func TestScrapeCountersOversizedBodyUnavailable(t *testing.T) {
	var b strings.Builder
	b.WriteString("# TYPE llamacpp:prompt_tokens_total counter\n")
	b.WriteString("llamacpp:prompt_tokens_total 130572\n")
	b.WriteString("# TYPE llamacpp:tokens_predicted_total counter\n")
	b.WriteString("llamacpp:tokens_predicted_total 48913\n")
	// Pad well past the 64 KiB scrape cap so the body is over-cap (and thus truncatable),
	// even though the counter lines themselves are valid and present.
	for b.Len() < 80<<10 {
		b.WriteString("# llamacpp_padding_comment_line_to_exceed_the_scrape_body_cap\n")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()

	if _, ok := ScrapeCounters(srv.URL); ok {
		t.Errorf("ScrapeCounters ok=true on an over-cap body, want false (truncation risk → unavailable, no partial fold)")
	}
}

// TestCounterFromMapRejectsNonFinite asserts counterFromMap returns the typed-Unknown
// branch (Known=false, zero total) for every value that is NOT a trustworthy
// non-negative exactly-representable integer — NaN, +Inf, -Inf, negative, and an
// over-bound value above 2^53 — so a garbage /metrics line can never be narrowed into a
// fabricated durable count (D-05 / WR-02). A normal finite count and an absent key are
// included as the control rows.
func TestCounterFromMapRejectsNonFinite(t *testing.T) {
	const name = "llamacpp:prompt_tokens_total"
	cases := []struct {
		desc      string
		present   bool
		val       float64
		wantVal   uint64
		wantKnown bool
	}{
		{"NaN", true, math.NaN(), 0, false},
		{"+Inf", true, math.Inf(1), 0, false},
		{"-Inf", true, math.Inf(-1), 0, false},
		{"negative", true, -1, 0, false},
		{"over-bound (>2^53)", true, float64(maxCounterValue) + 2048, 0, false},
		{"absent key", false, 0, 0, false},
		{"normal count", true, 130572, 130572, true},
		{"at bound (2^53)", true, float64(maxCounterValue), uint64(maxCounterValue), true},
		{"zero", true, 0, 0, true},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			m := map[string]float64{}
			if c.present {
				m[name] = c.val
			}
			got, known := counterFromMap(m, name)
			if known != c.wantKnown {
				t.Errorf("Known = %v, want %v (no fabricated count for %s)", known, c.wantKnown, c.desc)
			}
			if got != c.wantVal {
				t.Errorf("value = %d, want %d", got, c.wantVal)
			}
		})
	}
}

// TestParseSlotsReadsOnlyNarrowFields asserts the /slots parser counts processing
// slots and reads ONLY id/n_ctx/is_processing/next_token.n_decoded — never the prompt
// or sampling params (T-05-08 security: no prompt leakage).
func TestParseSlotsReadsOnlyNarrowFields(t *testing.T) {
	body, err := os.ReadFile("testdata/slots.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	slots, ok := parseSlots(body)
	if !ok {
		t.Fatalf("parseSlots ok=false on a valid array")
	}
	if len(slots) != 2 {
		t.Fatalf("parseSlots len = %d, want 2", len(slots))
	}

	// Structurally assert the Slot type carries no prompt/param field (security):
	// the only fields are ID, NCtx, IsProcessing, NextToken{NDecoded,NRemain}.
	st := reflect.TypeOf(Slot{})
	allowed := map[string]bool{"ID": true, "NCtx": true, "IsProcessing": true, "NextToken": true}
	for i := 0; i < st.NumField(); i++ {
		name := st.Field(i).Name
		if !allowed[name] {
			t.Errorf("Slot has unexpected field %q — only non-sensitive fields may be read (no prompt/params)", name)
		}
	}

	// Active slot = the processing one; its decoded count is read.
	if !slots[0].IsProcessing || slots[0].NextToken.NDecoded != 128 || slots[0].NCtx != 65536 {
		t.Errorf("processing slot = %+v, want IsProcessing+n_decoded=128+n_ctx=65536", slots[0])
	}
	if slots[1].IsProcessing {
		t.Errorf("slot[1] IsProcessing=true, want idle")
	}
}

// TestActiveAndIdleGate asserts the fold: with a processing slot OR
// requests_processing>0 the stack is "generating"; with neither it is idle so the UI
// renders "Idle — no active generation." (Pitfall 3 / D-10).
func TestActiveAndIdleGate(t *testing.T) {
	body, err := os.ReadFile("testdata/slots.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	slots, _ := parseSlots(body)

	if n := ActiveSlots(slots); n != 1 {
		t.Errorf("ActiveSlots = %d, want 1", n)
	}

	// Generating: a processing slot present.
	if !IsGenerating(PerfSnapshot{RequestsProcessing: 0}, slots) {
		t.Errorf("IsGenerating=false with a processing slot present, want true")
	}
	// Generating: requests_processing>0 even with no slots data.
	if !IsGenerating(PerfSnapshot{RequestsProcessing: 1}, nil) {
		t.Errorf("IsGenerating=false with requests_processing=1, want true")
	}
	// Idle: no processing slot and requests_processing==0 → idle.
	idleSlots := []Slot{{ID: 0, NCtx: 65536, IsProcessing: false}}
	if IsGenerating(PerfSnapshot{RequestsProcessing: 0}, idleSlots) {
		t.Errorf("IsGenerating=true with no processing slot and requests_processing=0, want idle (false)")
	}
}

// TestScrapeSlotsFromServer asserts ScrapeSlots fetches and parses /slots over a
// bounded body, and a non-200 (e.g. --no-slots) degrades to ok=false.
func TestScrapeSlotsFromServer(t *testing.T) {
	body, err := os.ReadFile("testdata/slots.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/slots" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	slots, ok := ScrapeSlots(srv.URL)
	if !ok {
		t.Fatalf("ScrapeSlots ok=false on a 200 body")
	}
	if len(slots) != 2 {
		t.Errorf("ScrapeSlots len = %d, want 2", len(slots))
	}

	// A server that 404s /slots → ok=false (typed-Unknown, no fabricated active count).
	down := httptest.NewServer(http.HandlerFunc(http.NotFound))
	defer down.Close()
	if _, ok := ScrapeSlots(down.URL); ok {
		t.Errorf("ScrapeSlots ok=true on a 404, want false")
	}
}
