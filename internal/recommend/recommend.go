// Package recommend turns a detected HostProfile plus the model catalog into a
// single memory-safe Recommendation: a model/quant/context/backend that provably
// fits the usable memory envelope (model_bytes + KV-cache@ctx + headroom ≤
// usable_envelope), with every term of that inequality surfaced so the CLI can
// SHOW the user why it fits (D-06).
//
// Pick is a PURE function (no I/O) so it is exhaustively table-testable. It never
// auto-selects an entry flagged unified_memory_safe:false (REC-02), never auto-
// selects the bootstrap entry (D-12), defaults the backend to vulkan for gfx1151
// (REC-04), re-validates manual overrides and warns/fails when they don't fit
// (D-07), and degrades safely to a conservative floor when the envelope is Unknown
// — refusing only when no safe floor is derivable, never guessing high (D-14).
package recommend

import (
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// defaultBackend is the inference backend recommended for gfx1151 (REC-04). ROCm
// is opt-in only and is never auto-selected in Phase 1.
const defaultBackend = "vulkan"

// recommendSchemaVersion is the Recommendation contract self-version. It is the
// LAST tagged field of Recommendation and surfaces unconditionally in --json so
// dashboards can gate on additive growth (D-06/D-07). Start at 1.
const recommendSchemaVersion = 1

// ROCmAdvice is a typed enum surfaced on the Recommendation (REC-05 / D-05): an
// honesty-bounded hint about whether the opt-in ROCm backend is worth a benchmark
// on this host, derived PURELY from HostProfile.rocm_readiness inside Pick (no I/O,
// no new arg). It NEVER changes the recommended Backend (which stays vulkan,
// REC-04) and NEVER promises a speed-up — the on-hardware token-gen delta was
// negative (Δtg −11.15), so ROCm can REGRESS tg. Empty ("") means "not applicable"
// (a Known-bad readiness signal withholds advice and names the blocker in a Note).
type ROCmAdvice string

const (
	// ROCmAdviceReady is reserved for a host that is fully validated as ROCm-ready.
	// Phase 10 derives only worth-trying / verify-with-bench / withheld from the
	// readiness fold; the const is defined for contract completeness (D-05).
	ROCmAdviceReady ROCmAdvice = "ready"
	// ROCmAdviceWorthTrying means every readiness signal is Known-good — ROCm is
	// worth a benchmark, but the advice still points at `villa bench` and never
	// promises a win.
	ROCmAdviceWorthTrying ROCmAdvice = "worth-trying"
	// ROCmAdviceVerifyBench means at least one readiness signal is unevaluable
	// (off-hardware default) — the honest answer is "verify with villa bench".
	ROCmAdviceVerifyBench ROCmAdvice = "verify-with-bench"
)

// rocmAdviceNote is the LOCKED honesty-safe Note copy (RESEARCH Pattern 4 / D-05).
// It points the user at `villa bench --ab` and deliberately contains none of
// "faster"/"guaranteed"/"speed-up" — ROCm's win is prompt-processing-weighted and
// token generation may regress (on-hardware UAT Δtg −11.15). Tested.
const rocmAdviceNote = "ROCm: worth trying for prompt-heavy workloads — token generation may not improve (and can regress vs vulkan). Verify on your model with: villa bench --ab"

// rocmVerifyNote is the verify-with-bench Note: same honesty discipline, but it
// makes no readiness claim because at least one signal is unevaluable.
const rocmVerifyNote = "ROCm: readiness could not be fully evaluated on this host — verify whether it helps your model with: villa bench --ab"

// Recommendation is the result of Pick — and the --json / Phase-5 dashboard
// contract (D-05). A golden-file test guards its shape. Every fit term is
// populated so the command layer can render the full inequality (D-06).
type Recommendation struct {
	// The pick. Model is empty only on a refusal (no safe envelope).
	Model      string `json:"model"`
	Quant      string `json:"quant"`
	ContextLen int    `json:"context_len"`
	Backend    string `json:"backend"`

	// The four fit terms plus the ceiling and verdict (D-06): the user can see
	// WeightBytes + KVCacheBytes + HeadroomBytes = TotalBytes ≤ UsableEnvelopeBytes.
	WeightBytes         uint64 `json:"weight_bytes"`
	KVCacheBytes        uint64 `json:"kv_cache_bytes"`
	HeadroomBytes       uint64 `json:"headroom_bytes"`
	TotalBytes          uint64 `json:"total_bytes"`
	UsableEnvelopeBytes uint64 `json:"usable_envelope_bytes"`

	// Fits is the verdict; Degraded marks a conservative-floor estimate (D-14).
	Fits     bool `json:"fits"`
	Degraded bool `json:"degraded"`

	// Notes carries caveats: degraded-floor warnings, unsafe-override warnings,
	// override-doesn't-fit warnings, and refusal reasons.
	Notes []string `json:"notes"`

	// Alternatives are other fitting picks (surfaced behind --alternatives, D-06).
	Alternatives []Alternative `json:"alternatives,omitempty"`

	// ROCmAdvice + ROCmNote are the tail-appended, honesty-bounded ROCm hint
	// (REC-05 / D-05), derived purely from HostProfile.rocm_readiness inside Pick.
	// Both are omitempty so the contract is unchanged when advice is not applicable.
	// They NEVER change Backend (REC-04) and NEVER promise a speed-up.
	ROCmAdvice ROCmAdvice `json:"rocm_advice,omitempty"`
	ROCmNote   string     `json:"rocm_note,omitempty"`

	// SchemaVersion is the Recommendation contract self-version and MUST stay the
	// LAST tagged field (append-only discipline; new fields go above it, D-06/D-07).
	SchemaVersion int `json:"schema_version"`
}

// Alternative is a compact view of another fitting pick.
type Alternative struct {
	Model      string `json:"model"`
	Quant      string `json:"quant"`
	ContextLen int    `json:"context_len"`
	TotalBytes uint64 `json:"total_bytes"`
}

// Overrides are user-supplied selections (REC-03 / D-07). A zero value means
// "unset" for that field.
type Overrides struct {
	Model string
	Quant string
	Ctx   int
}

// Pick selects the single best fitting model for the host, applies and re-
// validates any overrides, and returns a fully-populated Recommendation.
func Pick(p detect.HostProfile, c catalog.Catalog, ov Overrides) Recommendation {
	envelope, degraded, ok := resolveEnvelope(p)
	if !ok {
		// No usable envelope and no safe floor derivable — refuse rather than
		// guess high (D-14). Empty Model signals the refusal.
		return finalizeRecommendation(Recommendation{
			Backend: defaultBackend,
			Notes:   []string{"refusing to recommend: usable memory envelope is unknown and no safe floor is derivable (neither GTT envelope nor total RAM detected)"},
		}, p)
	}

	var notes []string
	if degraded {
		notes = append(notes, fmt.Sprintf(
			"DEGRADED ESTIMATE: real GTT envelope unknown; sized against a conservative %.0f%%-of-RAM floor (%s). Verify before relying on this pick (D-14).",
			degradedFloorFraction*100, humanGiB(envelope)))
	}

	// An explicit --model override takes precedence and is re-validated.
	if ov.Model != "" {
		return finalizeRecommendation(pickOverride(c, ov, envelope, degraded, notes), p)
	}

	return finalizeRecommendation(pickBest(c, ov, envelope, degraded, notes), p)
}

// finalizeRecommendation stamps the additive, contract-level fields onto a
// fully-computed pick: the unconditional SchemaVersion and the purely-derived ROCm
// advice. It runs AFTER Backend is set and NEVER reassigns rec.Backend — advice can
// only annotate the pick, never auto-switch it (REC-04, T-10-06). It performs no
// I/O: the advice is folded from p.ROCmReadiness already in hand (no new Pick arg).
func finalizeRecommendation(rec Recommendation, p detect.HostProfile) Recommendation {
	rec.SchemaVersion = recommendSchemaVersion
	advice, note := deriveROCmAdvice(p.ROCmReadiness)
	rec.ROCmAdvice = advice
	if note != "" {
		rec.ROCmNote = note
	}
	return rec
}

// deriveROCmAdvice folds the five detect.ROCmReadiness signals into the honesty-
// bounded advice + Note (D-05 / RESEARCH Pattern 4). It mirrors
// status.foldROCmReadiness's Known-first, worst-wins discipline (any unevaluable
// signal → unknown wins over a confidently-bad one; no-false-green, D-04/D-08):
//
//   - all five Known-good        → worth-trying + the locked honesty-safe Note
//   - any signal unevaluable     → verify-with-bench + the verify Note
//   - all Known and any Known-bad → advice withheld ("") + a Note naming the blocker
//
// It is pure (reads the passed struct only, no I/O) and never promises a speed-up.
func deriveROCmAdvice(r detect.ROCmReadiness) (ROCmAdvice, string) {
	type signal struct {
		name string
		b    detect.Bool
	}
	signals := []signal{
		{"HSA override viable", r.HSAOverrideViable},
		{"firmware date", r.FirmwareDateOK},
		{"kernel floor", r.KernelFloorOK},
		{"rocminfo gfx1151", r.RocminfoGfx1151},
		{"image pin policy", r.ImagePolicyOK},
	}
	sawUnknown := false
	var blocker string
	for _, s := range signals {
		if !s.b.Known {
			sawUnknown = true // any unevaluable signal → unknown wins
			continue
		}
		if !s.b.Value && blocker == "" {
			blocker = s.name // first Known-bad signal names the blocker
		}
	}
	// Unknown wins over not-ready (no false-green): only withhold-with-blocker when
	// every signal is Known and at least one is bad.
	if !sawUnknown && blocker != "" {
		return "", fmt.Sprintf("ROCm: not ready on this host — blocked by %s. Staying on vulkan; re-check after resolving it (run villa status).", blocker)
	}
	if sawUnknown {
		return ROCmAdviceVerifyBench, rocmVerifyNote
	}
	return ROCmAdviceWorthTrying, rocmAdviceNote
}

// pickBest selects the largest auto-eligible model that fits, honoring an
// optional --ctx/--quant override on the chosen model.
func pickBest(c catalog.Catalog, ov Overrides, envelope uint64, degraded bool, notes []string) Recommendation {
	headroom := headroomBytes(envelope)

	var best *catalog.CatalogModel
	var bestTotal uint64
	var alts []Alternative

	for i := range c.Models {
		m := c.Models[i]
		if m.Bootstrap {
			continue // never auto-select the bootstrap entry (D-12)
		}
		if !m.UnifiedMemorySafe {
			continue // never auto-select a unified-memory-unsafe entry (REC-02)
		}
		if m.MinEnvelopeBytes > 0 && envelope < m.MinEnvelopeBytes {
			continue // secondary floor guard: model declares a minimum envelope it
			// needs to run acceptably; skip it when the host is below that floor
			// even if the raw weights+KV+headroom math would otherwise fit (IN-01).
		}
		ctx := effectiveCtx(m, ov)
		total := m.WeightBytes + kvCacheBytes(m, ctx) + headroom
		if total > envelope {
			continue // OOM guard: never select a pick that exceeds the envelope
		}
		alts = append(alts, Alternative{Model: m.ID, Quant: m.Quant, ContextLen: ctx, TotalBytes: total})
		// "Best" = the largest footprint that still fits (most capable).
		if best == nil || total > bestTotal {
			bm := m
			best = &bm
			bestTotal = total
		}
	}

	if best == nil {
		notes = append(notes, fmt.Sprintf("no catalog model fits the usable envelope of %s (with %.0f%% headroom) — consider a smaller model or larger memory", humanGiB(envelope), headroomFraction*100))
		return Recommendation{
			Backend:             defaultBackend,
			UsableEnvelopeBytes: envelope,
			Degraded:            degraded,
			Notes:               notes,
		}
	}

	ctx := effectiveCtx(*best, ov)
	rec := buildRecommendation(*best, ctx, envelope, degraded, notes)
	// Alternatives = the other fitting picks (exclude the chosen one).
	for _, a := range alts {
		if a.Model == best.ID && a.ContextLen == ctx {
			continue
		}
		rec.Alternatives = append(rec.Alternatives, a)
	}
	if ov.Quant != "" && ov.Quant != best.Quant {
		rec.Notes = append(rec.Notes, fmt.Sprintf("requested quant %q ignored: not represented in the catalog for %q (auto-selected %q)", ov.Quant, best.ID, best.Quant))
	}
	return rec
}

// pickOverride applies a --model override: the named model is used even if it is
// flagged unified_memory_safe:false (warning loudly), and the fit is re-validated
// (D-07).
func pickOverride(c catalog.Catalog, ov Overrides, envelope uint64, degraded bool, notes []string) Recommendation {
	m, found := c.FindByID(ov.Model)
	if !found {
		notes = append(notes, fmt.Sprintf("override model %q not found in catalog — no recommendation made", ov.Model))
		return Recommendation{
			Backend:             defaultBackend,
			UsableEnvelopeBytes: envelope,
			Degraded:            degraded,
			Notes:               notes,
		}
	}

	if !m.UnifiedMemorySafe {
		notes = append(notes, fmt.Sprintf("WARNING: model %q is flagged unified_memory_safe:false — it is known to misbehave on unified memory; using it only because you overrode (D-07)", m.ID))
	}
	if ov.Quant != "" && ov.Quant != m.Quant {
		notes = append(notes, fmt.Sprintf("note: override quant %q differs from the catalog entry's %q; fit math uses the catalog entry's dimensions", ov.Quant, m.Quant))
	}

	ctx := effectiveCtx(m, ov)
	rec := buildRecommendation(m, ctx, envelope, degraded, notes)
	if ov.Quant != "" {
		rec.Quant = ov.Quant
	}
	if !rec.Fits {
		rec.Notes = append(rec.Notes, fmt.Sprintf(
			"WARNING: your override does NOT fit — %s needed vs %s usable envelope; it will likely OOM. Reduce --ctx or pick a smaller model (D-07).",
			humanGiB(rec.TotalBytes), humanGiB(envelope)))
	}
	return rec
}

// buildRecommendation computes all fit terms for model m at ctx and assembles the
// Recommendation (without touching alternatives/override-specific notes).
func buildRecommendation(m catalog.CatalogModel, ctx int, envelope uint64, degraded bool, notes []string) Recommendation {
	kv := kvCacheBytes(m, ctx)
	headroom := headroomBytes(envelope)
	total := m.WeightBytes + kv + headroom

	backend := m.BackendDefault
	if backend == "" {
		backend = defaultBackend
	}

	return Recommendation{
		Model:               m.ID,
		Quant:               m.Quant,
		ContextLen:          ctx,
		Backend:             backend,
		WeightBytes:         m.WeightBytes,
		KVCacheBytes:        kv,
		HeadroomBytes:       headroom,
		TotalBytes:          total,
		UsableEnvelopeBytes: envelope,
		Fits:                total <= envelope,
		Degraded:            degraded,
		Notes:               notes,
	}
}

// effectiveCtx returns the context length to size against: the override when set
// and positive, else the model's default.
func effectiveCtx(m catalog.CatalogModel, ov Overrides) int {
	if ov.Ctx > 0 {
		return ov.Ctx
	}
	return m.DefaultCtx
}

// humanGiB renders a byte count as a GiB string for user-facing notes.
func humanGiB(b uint64) string {
	return fmt.Sprintf("%.2f GiB", float64(b)/(1<<30))
}
