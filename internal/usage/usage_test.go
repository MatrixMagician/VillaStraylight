// Package usage tests guard the reset-aware fold (D-04), per-model keying (D-03),
// typed-Unknown no-fold discipline (D-05), the counts-only structural guarantee
// (D-11), and the XDG atomic store round-trip + traversal guard (D-02).
package usage

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestFoldResetAware proves the monotonic-then-reset accumulation: a growing raw
// counter folds the delta into the cumulative total; a BACKWARD step (server
// restart / backend swap reset the in-memory counter to a low value) is treated as
// "the whole sample is new", never a negative delta (D-04).
func TestFoldResetAware(t *testing.T) {
	prior := UsageTotals{}
	prior = Fold(prior, Sample{
		Model:                "m1",
		PromptTokensTotal:    100,
		PromptTokensKnown:    true,
		PredictedTokensTotal: 100,
		PredictedTokensKnown: true,
	})
	// Re-seat the prior to last_seen=100 by folding once more at the same raw value
	// is not what we want; instead set up the documented scenario directly: prior
	// cumulative=100/last_seen=100, then sample raw=150.
	prior = UsageTotals{
		SchemaVersion: usageSchemaVersion,
		Models: map[string]ModelUsage{
			"m1": {
				Model:     "m1",
				Prompt:    CounterState{Cumulative: 100, LastSeenRaw: 100},
				Predicted: CounterState{Cumulative: 100, LastSeenRaw: 100},
			},
		},
	}

	// Monotonic growth: raw 150 ⇒ +50 ⇒ cumulative 150, last_seen 150.
	got := Fold(prior, Sample{
		Model:                "m1",
		PromptTokensTotal:    150,
		PromptTokensKnown:    true,
		PredictedTokensTotal: 150,
		PredictedTokensKnown: true,
	})
	if mu := got.Models["m1"]; mu.Prompt.Cumulative != 150 || mu.Prompt.LastSeenRaw != 150 {
		t.Fatalf("monotonic prompt = %+v, want cumulative=150 last_seen=150", mu.Prompt)
	}
	if mu := got.Models["m1"]; mu.Predicted.Cumulative != 150 || mu.Predicted.LastSeenRaw != 150 {
		t.Fatalf("monotonic predicted = %+v, want cumulative=150 last_seen=150", mu.Predicted)
	}

	// Reset: raw 20 < last_seen 150 ⇒ whole sample (20) is new ⇒ cumulative 170, last_seen 20.
	got = Fold(got, Sample{
		Model:                "m1",
		PromptTokensTotal:    20,
		PromptTokensKnown:    true,
		PredictedTokensTotal: 20,
		PredictedTokensKnown: true,
	})
	if mu := got.Models["m1"]; mu.Prompt.Cumulative != 170 || mu.Prompt.LastSeenRaw != 20 {
		t.Fatalf("reset prompt = %+v, want cumulative=170 last_seen=20", mu.Prompt)
	}
	if mu := got.Models["m1"]; mu.Predicted.Cumulative != 170 || mu.Predicted.LastSeenRaw != 20 {
		t.Fatalf("reset predicted = %+v, want cumulative=170 last_seen=20", mu.Predicted)
	}
}

// TestFoldPerModel proves two distinct models accumulate independently and that
// folding one model again leaves the other untouched (D-03 per-model keying).
func TestFoldPerModel(t *testing.T) {
	tot := UsageTotals{}
	tot = Fold(tot, Sample{Model: "A", PromptTokensTotal: 10, PromptTokensKnown: true, PredictedTokensTotal: 5, PredictedTokensKnown: true})
	tot = Fold(tot, Sample{Model: "B", PromptTokensTotal: 100, PromptTokensKnown: true, PredictedTokensTotal: 50, PredictedTokensKnown: true})

	if a := tot.Models["A"]; a.Prompt.Cumulative != 10 || a.Predicted.Cumulative != 5 {
		t.Fatalf("model A = %+v, want prompt=10 predicted=5", a)
	}
	if b := tot.Models["B"]; b.Prompt.Cumulative != 100 || b.Predicted.Cumulative != 50 {
		t.Fatalf("model B = %+v, want prompt=100 predicted=50", b)
	}

	// Fold A again (raw grows 10→30) — only A changes; B is untouched.
	tot = Fold(tot, Sample{Model: "A", PromptTokensTotal: 30, PromptTokensKnown: true, PredictedTokensTotal: 5, PredictedTokensKnown: true})
	if a := tot.Models["A"]; a.Prompt.Cumulative != 30 {
		t.Fatalf("model A after re-fold prompt = %d, want 30", a.Prompt.Cumulative)
	}
	if b := tot.Models["B"]; b.Prompt.Cumulative != 100 || b.Predicted.Cumulative != 50 {
		t.Fatalf("model B changed after folding A: %+v", b)
	}
}

// TestFoldTypedUnknown proves a typed-Unknown (absent) counter contributes NO fold
// and NO LastSeenRaw mutation for that counter, while a Known sibling counter still
// folds; and an entirely-unknown sample produces NO new/changed model entry (D-05).
func TestFoldTypedUnknown(t *testing.T) {
	tot := UsageTotals{
		SchemaVersion: usageSchemaVersion,
		Models: map[string]ModelUsage{
			"m1": {
				Model:     "m1",
				Prompt:    CounterState{Cumulative: 100, LastSeenRaw: 100},
				Predicted: CounterState{Cumulative: 200, LastSeenRaw: 200},
			},
		},
	}

	// Prompt UNKNOWN, predicted KNOWN (raw grows 200→250).
	got := Fold(tot, Sample{
		Model:                "m1",
		PromptTokensKnown:    false, // no fold, no LastSeenRaw mutation
		PredictedTokensTotal: 250,
		PredictedTokensKnown: true,
	})
	if mu := got.Models["m1"]; mu.Prompt.Cumulative != 100 || mu.Prompt.LastSeenRaw != 100 {
		t.Fatalf("unknown prompt was folded: %+v, want unchanged cumulative=100 last_seen=100", mu.Prompt)
	}
	if mu := got.Models["m1"]; mu.Predicted.Cumulative != 250 || mu.Predicted.LastSeenRaw != 250 {
		t.Fatalf("known predicted = %+v, want cumulative=250 last_seen=250", mu.Predicted)
	}

	// Entirely-unknown sample for a NEW model ⇒ no new entry created.
	got2 := Fold(UsageTotals{}, Sample{Model: "ghost", PromptTokensKnown: false, PredictedTokensKnown: false})
	if _, ok := got2.Models["ghost"]; ok {
		t.Fatalf("entirely-unknown sample created a model entry %q, want none", "ghost")
	}
}

// TestUsageTotalsHasNoContentFields is the counts-only structural security test
// (D-11), mirroring metrics.TestParseSlotsReadsOnlyNarrowFields: it reflects over
// UsageTotals AND ModelUsage allowing ONLY count/identity field names, and asserts
// the marshaled JSON of a populated store contains none of the content denylist
// substrings — no prompt/response/content text can ever enter the store.
func TestUsageTotalsHasNoContentFields(t *testing.T) {
	allowedTotals := map[string]bool{
		"Models":        true,
		"SchemaVersion": true,
	}
	stTot := reflect.TypeOf(UsageTotals{})
	for i := 0; i < stTot.NumField(); i++ {
		name := stTot.Field(i).Name
		if !allowedTotals[name] {
			t.Errorf("UsageTotals has unexpected field %q — counts-only: no prompt/response/content fields", name)
		}
	}

	allowedModel := map[string]bool{
		"Model":     true,
		"Prompt":    true,
		"Predicted": true,
		"LastSeen":  true,
	}
	stModel := reflect.TypeOf(ModelUsage{})
	for i := 0; i < stModel.NumField(); i++ {
		name := stModel.Field(i).Name
		if !allowedModel[name] {
			t.Errorf("ModelUsage has unexpected field %q — counts-only: no prompt/response/content fields", name)
		}
	}

	// CounterState carries only the two count integers.
	allowedCounter := map[string]bool{"Cumulative": true, "LastSeenRaw": true}
	stCounter := reflect.TypeOf(CounterState{})
	for i := 0; i < stCounter.NumField(); i++ {
		name := stCounter.Field(i).Name
		if !allowedCounter[name] {
			t.Errorf("CounterState has unexpected field %q — counts-only", name)
		}
	}

	// Marshaled JSON denylist: a populated store must not contain any content word.
	populated := UsageTotals{
		SchemaVersion: usageSchemaVersion,
		Models: map[string]ModelUsage{
			"m1": {
				Model:     "m1",
				Prompt:    CounterState{Cumulative: 1234, LastSeenRaw: 1234},
				Predicted: CounterState{Cumulative: 5678, LastSeenRaw: 5678},
				LastSeen:  "2026-06-07T00:00:00Z",
			},
		},
	}
	blob, err := json.Marshal(populated)
	if err != nil {
		t.Fatalf("marshal populated UsageTotals: %v", err)
	}
	for _, banned := range []string{"prompt_text", "response", "content", "text", "messages"} {
		if strings.Contains(string(blob), banned) {
			t.Errorf("marshaled usage JSON contains banned content token %q: %s", banned, string(blob))
		}
	}
}

// TestStoreRoundTrip proves Save then Load via a buffer-backed Deps returns
// identical cumulative totals (atomic write contract proven at the seam level).
func TestStoreRoundTrip(t *testing.T) {
	var buf []byte
	d := Deps{
		WriteAll: func(b []byte) error { buf = append([]byte(nil), b...); return nil },
		ReadAll:  func() ([]byte, error) { return buf, nil },
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}

	in := UsageTotals{
		Models: map[string]ModelUsage{
			"m1": {Model: "m1", Prompt: CounterState{Cumulative: 11, LastSeenRaw: 11}, Predicted: CounterState{Cumulative: 22, LastSeenRaw: 22}},
			"m2": {Model: "m2", Prompt: CounterState{Cumulative: 33, LastSeenRaw: 33}, Predicted: CounterState{Cumulative: 44, LastSeenRaw: 44}},
		},
	}
	if err := Save(d, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(d)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.SchemaVersion != usageSchemaVersion {
		t.Errorf("loaded schema_version = %d, want %d", out.SchemaVersion, usageSchemaVersion)
	}
	if !reflect.DeepEqual(out.Models, in.Models) {
		t.Errorf("round-trip models = %+v, want %+v", out.Models, in.Models)
	}
}

// TestLoadFailsClosed proves Load degrades to an empty typed-Unknown (no error, no
// panic) on an absent store, a corrupt blob, and a schema-version mismatch — never a
// fabricated total (D-05, T-15-05).
func TestLoadFailsClosed(t *testing.T) {
	// Absent store: ReadAll ⇒ (nil,nil) ⇒ empty UsageTotals, no error.
	absent := Deps{ReadAll: func() ([]byte, error) { return nil, nil }}
	if got, err := Load(absent); err != nil || len(got.Models) != 0 {
		t.Errorf("Load(absent) = (%+v, %v), want empty, nil", got, err)
	}

	// Corrupt blob: not JSON ⇒ empty, no error.
	corrupt := Deps{ReadAll: func() ([]byte, error) { return []byte("{not json"), nil }}
	if got, err := Load(corrupt); err != nil || len(got.Models) != 0 {
		t.Errorf("Load(corrupt) = (%+v, %v), want empty, nil", got, err)
	}

	// Version skew: valid JSON, wrong schema_version ⇒ empty, no error.
	skew := Deps{ReadAll: func() ([]byte, error) {
		return []byte(`{"models":{"m1":{"model":"m1"}},"schema_version":999}`), nil
	}}
	if got, err := Load(skew); err != nil || len(got.Models) != 0 {
		t.Errorf("Load(skew) = (%+v, %v), want empty, nil", got, err)
	}
}

// TestUsagePathXDG proves the resolver honors $XDG_DATA_HOME and that assertInsideDir
// rejects a traversal escape (T-15-01).
func TestUsagePathXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	want := filepath.Join(tmp, "villa", "usage.json")
	if got := UsagePath(); got != want {
		t.Errorf("UsagePath() = %q, want %q", got, want)
	}

	dir := filepath.Join(tmp, "villa")
	if err := assertInsideDir(filepath.Join(dir, "usage.json"), dir); err != nil {
		t.Errorf("assertInsideDir rejected an in-dir path: %v", err)
	}
	if err := assertInsideDir(filepath.Join(dir, "..", "escape.json"), dir); err == nil {
		t.Error("assertInsideDir accepted a traversal escape, want rejection")
	}
}
