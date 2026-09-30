package metrics

import (
	"math"
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

// TestParsePromTextStripsLabels is the guard: a labeled series
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
			t.Errorf("parsePromText leaked a labeled key %q — labels must be stripped", k)
		}
	}
}

// TestParsePerf asserts a /metrics body maps the confirmed gauges into a
// PerfSnapshot and never reads the removed KV-cache-usage gauge (Pitfall 4).
func TestParsePerf(t *testing.T) {
	body, err := os.ReadFile("testdata/metrics.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	snap := ParsePerf(body)
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

// TestParseCounters is the counter feed guard. The present case asserts the two
// monotonic cumulative counters (llamacpp:prompt_tokens_total and
// llamacpp:tokens_predicted_total) read out of a /metrics body as typed uint64
// readings with Known=true. The absent case is the typed-Unknown discipline: a body
// WITHOUT the two _total lines yields Known=false, NOT a fabricated 0. The
// unavailable and over-cap cases belong to the scrape, inference.Client.Counters.
func TestParseCounters(t *testing.T) {
	body, err := os.ReadFile("testdata/metrics.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	cs := ParseCounters(body)
	if !cs.PromptTokensKnown || cs.PromptTokensTotal != 130572 {
		t.Errorf("PromptTokensTotal = %d (known=%v), want 130572 (known=true)", cs.PromptTokensTotal, cs.PromptTokensKnown)
	}
	if !cs.PredictedTokensKnown || cs.PredictedTokensTotal != 48913 {
		t.Errorf("PredictedTokensTotal = %d (known=%v), want 48913 (known=true)", cs.PredictedTokensTotal, cs.PredictedTokensKnown)
	}

	// Absent: a body without the two _total lines → Known=false, never a fabricated 0.
	absent := ParseCounters([]byte(strings.Join([]string{
		`# TYPE llamacpp:requests_processing gauge`,
		`llamacpp:requests_processing 0`,
	}, "\n")))
	if absent.PromptTokensKnown {
		t.Errorf("PromptTokensKnown=true on an absent counter, want false (typed-Unknown, no fabricated 0)")
	}
	if absent.PredictedTokensKnown {
		t.Errorf("PredictedTokensKnown=true on an absent counter, want false (typed-Unknown, no fabricated 0)")
	}
	if absent.PromptTokensTotal != 0 || absent.PredictedTokensTotal != 0 {
		t.Errorf("absent CounterSample carries non-zero totals %+v — Known=false MUST gate the zero value", absent)
	}
}

// TestCounterFromMapRejectsNonFinite asserts counterFromMap returns the typed-Unknown
// branch (Known=false, zero total) for every value that is NOT a trustworthy
// non-negative exactly-representable integer — NaN, +Inf, -Inf, negative, and an
// over-bound value above 2^53 — so a garbage /metrics line can never be narrowed into a
// fabricated durable count. A normal finite count and an absent key are
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
// or sampling params (security: no prompt leakage).
func TestParseSlotsReadsOnlyNarrowFields(t *testing.T) {
	body, err := os.ReadFile("testdata/slots.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	slots, ok := ParseSlots(body)
	if !ok {
		t.Fatalf("ParseSlots ok=false on a valid array")
	}
	if len(slots) != 2 {
		t.Fatalf("ParseSlots len = %d, want 2", len(slots))
	}

	// Structurally assert the Slot type carries no prompt/param field (security):
	// the only fields are ID, NCtx, IsProcessing, NextToken{NDecoded,NRemain}.
	st := reflect.TypeOf(Slot{})
	allowed := map[string]bool{"ID": true, "NCtx": true, "IsProcessing": true, "NextToken": true}
	for i := range st.NumField() {
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
// renders "Idle — no active generation." (Pitfall 3).
func TestActiveAndIdleGate(t *testing.T) {
	body, err := os.ReadFile("testdata/slots.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	slots, _ := ParseSlots(body)

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

// TestParseSlotsFixture asserts the /slots fixture parses to its two slots.
func TestParseSlotsFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/slots.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	slots, ok := ParseSlots(body)
	if !ok {
		t.Fatalf("ParseSlots ok=false on the fixture")
	}
	if len(slots) != 2 {
		t.Errorf("ParseSlots len = %d, want 2", len(slots))
	}
}

// TestParseCacheCounters asserts the cache_n/prompt_n pair surfaces as a
// typed-Unknown CacheSample: both present → Known with exact counts; an absent
// counter → that one Known=false (never a fabricated 0). The ratio itself is NOT
// computed here (Plan 03 owns it).
func TestParseCacheCounters(t *testing.T) {
	cs := ParseCacheCounters([]byte(strings.Join([]string{
		"# TYPE " + mPromptCacheTokensTotal + " counter",
		mPromptCacheTokensTotal + " 4096",
		"# TYPE " + mCacheTokensTotal + " counter",
		mCacheTokensTotal + " 3072",
	}, "\n")))
	if !cs.PromptKnown || cs.PromptN != 4096 {
		t.Errorf("PromptN = %d (known=%v), want 4096 (known=true)", cs.PromptN, cs.PromptKnown)
	}
	if !cs.CacheKnown || cs.CacheN != 3072 {
		t.Errorf("CacheN = %d (known=%v), want 3072 (known=true)", cs.CacheN, cs.CacheKnown)
	}

	// Only prompt_n present → cache_n Known=false, never a fabricated 0.
	cs2 := ParseCacheCounters([]byte("# TYPE " + mPromptCacheTokensTotal + " counter\n" + mPromptCacheTokensTotal + " 100\n"))
	if !cs2.PromptKnown || cs2.PromptN != 100 {
		t.Errorf("PromptN = %d (known=%v), want 100 (known=true)", cs2.PromptN, cs2.PromptKnown)
	}
	if cs2.CacheKnown {
		t.Errorf("CacheKnown=true on an absent cache_n, want false (typed-Unknown, no fabricated 0)")
	}
	if cs2.CacheN != 0 {
		t.Errorf("absent CacheN must be the zero value gated by CacheKnown=false, got %d", cs2.CacheN)
	}

	// Both absent → both Known=false.
	cs3 := ParseCacheCounters([]byte("# TYPE llamacpp:requests_processing gauge\nllamacpp:requests_processing 0\n"))
	if cs3.CacheKnown || cs3.PromptKnown {
		t.Errorf("both-absent cache pair must be Known=false, got %+v", cs3)
	}
}

// TestCacheSampleRejectsNonFinite asserts the cache pair is read through the SAME
// counterFromMap finiteness guard as the usage counters: a NaN/Inf/negative/over-cap
// cache_n or prompt_n line degrades to Known=false (never a fabricated durable count),
// so a garbage /metrics line can never corrupt the surfacing-layer ratio.
func TestCacheSampleRejectsNonFinite(t *testing.T) {
	for _, c := range []struct {
		desc      string
		line      string
		wantKnown bool
		wantN     uint64
	}{
		{"normal", mCacheTokensTotal + " 3072", true, 3072},
		{"NaN", mCacheTokensTotal + " NaN", false, 0},
		{"+Inf", mCacheTokensTotal + " +Inf", false, 0},
		{"negative", mCacheTokensTotal + " -5", false, 0},
	} {
		t.Run(c.desc, func(t *testing.T) {
			cs := ParseCacheCounters([]byte("# TYPE " + mCacheTokensTotal + " counter\n" + c.line + "\n"))
			if cs.CacheKnown != c.wantKnown || cs.CacheN != c.wantN {
				t.Errorf("CacheN=%d known=%v, want %d known=%v", cs.CacheN, cs.CacheKnown, c.wantN, c.wantKnown)
			}
		})
	}
}
