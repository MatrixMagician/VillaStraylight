// Package usage is the cumulative-usage accumulation engine for USAGE-01.
//
// It exports a PURE, reset-aware Fold over llama.cpp's MONOTONIC `_total` token
// counters (NOT the rate gauges), keyed per model (D-03). The fold is pure
// arithmetic — no host I/O (D-01): a backward step in the raw counter (server
// restart / backend swap reset it to a low value) is treated as "the whole sample
// is new" rather than a negative delta (D-04), and a typed-Unknown (absent) counter
// contributes NO fold and NO write for that counter (D-05) — never a fabricated zero.
//
// Persistence is an injected byte-I/O seam (Deps), mirroring the established
// pure-core + injectable-seam pattern (benchstore/bench/status). The on-disk store
// (usage.json) lives under the XDG _data_ dir (D-02), not config and not cache, with
// the proven 0600/0700 + path-traversal-guard + atomic temp+rename discipline cloned
// (not imported) from internal/config + internal/benchstore. The store carries its
// OWN schema_version, INDEPENDENT of status.Report's reportSchemaVersion and NOT
// golden-frozen.
//
// The exported types are COUNTS-ONLY by construction (D-11): they carry token counts
// and model identity only — no prompt, response, or content text ever enters the
// store (asserted structurally + via a JSON-key denylist in usage_test.go).
package usage

import (
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/jsonstore"
)

// usageSchemaVersion is the usage store's OWN self-version. It is INDEPENDENT of
// status.Report's reportSchemaVersion and is NOT byte-frozen by any golden test
// (per 15-CONTEXT.md <specifics>): two separate contracts, only one of which (the
// Report) is golden-frozen. Bump only on an incompatible usage.json change.
const usageSchemaVersion = 1

// SchemaVersion exposes the usage store's OWN schema version to the Phase-16
// backup manifest (D-09). The const stays unexported (it is the store's private
// contract self-version); this one-line accessor is the only reader-of-record
// outside this package, so the manifest's UsageSchemaVersion field can never
// silently desync from the store's actual schema. No behaviour change.
func SchemaVersion() int { return usageSchemaVersion }

// CounterState is the persisted state for ONE monotonic counter of ONE model: the
// durable Cumulative total and the LastSeenRaw value from the previous scrape (used
// by the reset-aware fold to compute the per-scrape delta). Counts only.
type CounterState struct {
	Cumulative  uint64 `json:"cumulative"`
	LastSeenRaw uint64 `json:"last_seen_raw"`
}

// ModelUsage is the per-model entry: the model identity plus a prompt and a predicted
// CounterState and a LastSeen capture timestamp (RFC3339). Counts and identity only —
// no prompt/response/content fields (D-11).
type ModelUsage struct {
	Model     string       `json:"model"`
	Prompt    CounterState `json:"prompt_tokens"`
	Predicted CounterState `json:"generated_tokens"`
	LastSeen  string       `json:"last_seen,omitempty"`
}

// UsageTotals is the whole-store document: per-model cumulative usage keyed by model
// id, plus the store's own independent SchemaVersion. This is the single mutable JSON
// doc written via full-file atomic temp+rename (NOT an append-only log).
type UsageTotals struct {
	Models        map[string]ModelUsage `json:"models,omitempty"`
	SchemaVersion int                   `json:"schema_version"`
}

// Sample is one scrape's raw per-model reading. Each counter carries a Known flag:
// Known=false means the counter was absent/unparseable in the scrape (typed-Unknown)
// and MUST contribute no fold for that counter (D-05). CapturedAt is optional; when
// set it stamps the model's LastSeen.
type Sample struct {
	Model                string
	PromptTokensTotal    uint64
	PromptTokensKnown    bool
	PredictedTokensTotal uint64
	PredictedTokensKnown bool
	CapturedAt           time.Time
}

// foldCounter adds the new generation since LastSeenRaw, treating a backward step
// (sampleRaw < prior.LastSeenRaw — the server restarted and reset the in-memory
// counter low) as "the whole sample is new" rather than a negative delta (D-04).
//
// SAMPLING LIMITATION (WR-04): because this folds a monotonic counter sampled at the
// dashboard poll cadence, a counter that fully RESETS and then regrows BETWEEN two
// scrapes undercounts the pre-reset tail. E.g. with LastSeenRaw=150, the server grows
// to 200, restarts, and regrows to 30 all before the next scrape: the next sample is 30,
// 30 < 150 → "whole sample is new" → delta=30, and the 50 tokens (200−150) generated
// after the previous scrape but before the reset are silently dropped — the fold never
// observed raw=200 and cannot recover them. This biases the cumulative total DOWNWARD
// (the safe direction for a usage figure — never an over-count), and the magnitude is
// bounded by how much can be generated within one inter-scrape window at the dashboard
// poll cadence. The figure is therefore a LOWER BOUND under restart churn. This is
// inherent to sampling and is accepted, not corrected here.
func foldCounter(prior CounterState, sampleRaw uint64) CounterState {
	var delta uint64
	if sampleRaw >= prior.LastSeenRaw {
		delta = sampleRaw - prior.LastSeenRaw // normal monotonic growth
	} else {
		delta = sampleRaw // reset detected: counter went backwards → whole sample is new
	}
	return CounterState{
		Cumulative:  prior.Cumulative + delta,
		LastSeenRaw: sampleRaw,
	}
}

// Fold applies the reset-aware foldCounter to the per-model prompt and predicted
// counters keyed by sample.Model (D-03), folding ONLY the counters whose Known flag
// is set (D-05 — an unknown counter leaves its CounterState untouched, no
// LastSeenRaw mutation). An entirely-unknown sample (both counters unknown) produces
// NO new or changed model entry. Fold is pure: it returns an updated copy and never
// mutates the input store.
func Fold(prior UsageTotals, sample Sample) UsageTotals {
	// A sample with no Known counter — or no model identity to attribute counts to —
	// contributes nothing; never fabricate or MIS-KEY an entry. An empty Model only
	// arises when the writer could not resolve the active model (e.g. a transient
	// config-read error makes the dashboard's liveModelID return ""); folding it would
	// persist a phantom ""-keyed row carrying real counts that then surfaces in
	// `villa status` and gets backed up (D-03: per-model keying, never a blank key).
	if (!sample.PromptTokensKnown && !sample.PredictedTokensKnown) || sample.Model == "" {
		return prior
	}

	out := UsageTotals{
		SchemaVersion: usageSchemaVersion,
		Models:        make(map[string]ModelUsage, len(prior.Models)+1),
	}
	for k, v := range prior.Models {
		out.Models[k] = v
	}

	mu := out.Models[sample.Model]
	mu.Model = sample.Model
	if sample.PromptTokensKnown {
		mu.Prompt = foldCounter(mu.Prompt, sample.PromptTokensTotal)
	}
	if sample.PredictedTokensKnown {
		mu.Predicted = foldCounter(mu.Predicted, sample.PredictedTokensTotal)
	}
	if !sample.CapturedAt.IsZero() {
		mu.LastSeen = sample.CapturedAt.UTC().Format(time.RFC3339)
	}
	out.Models[sample.Model] = mu
	return out
}

// GetSchemaVersion and SetSchemaVersion let the shared store stamp and check this
// document's version without knowing anything else about it.
func (t *UsageTotals) GetSchemaVersion() int  { return t.SchemaVersion }
func (t *UsageTotals) SetSchemaVersion(v int) { t.SchemaVersion = v }

// store binds the shared persistence layer to this document, filename and version.
// Only the PERSISTENCE is shared: the reset-aware counter fold above — the part
// that makes a monotonic source restarting look like new tokens rather than a
// negative delta — stays here, where it belongs.
var store = jsonstore.New[UsageTotals, *UsageTotals]("usage", "usage.json", usageSchemaVersion)

// Deps is the injectable byte-I/O seam, so the package stays testable off-hardware
// with a buffer-backed Deps.
type Deps = jsonstore.Deps

// Save stamps the store's own SchemaVersion and writes the whole document via the
// seam (full-file replace, not append). D-02.
func Save(d Deps, t UsageTotals) error { return store.Save(d, t) }

// Load reads the store via the seam and fails CLOSED to an empty typed-Unknown
// UsageTotals on an absent, corrupt or version-mismatched store — never a
// fabricated total (D-05, T-15-05).
func Load(d Deps) (UsageTotals, error) { return store.Load(d) }

// UsagePath resolves the single mutable usage store, directly under the villa data
// root. Usage is durable accumulated DATA, not config and not disposable cache.
func UsagePath() string { return store.Path() }

// WriteFileAtomic is the live data-dir write seam the dashboard and restore wire: a
// traversal-guarded temp+rename write at 0600 under a 0700 directory, so a crash
// mid-write never leaves a torn usage.json (T-15-02).
func WriteFileAtomic(path string, data []byte) error { return store.WriteFileAtomic(path, data) }
