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
	"github.com/MatrixMagician/VillaStraylight/internal/memory"
)

// defaultBackend is the inference backend recommended for gfx1151 (REC-04). ROCm
// is opt-in only and is never auto-selected in Phase 1.
const defaultBackend = "vulkan"

// Web-search reservation constants (GROUND-03, 31-RESEARCH Assumption A6). The
// reservation is deliberately CONSERVATIVE — over-reserving keeps the chat model
// GPU-resident under search load; on-hardware tuning of these is deferred to
// Phase 33/34 (STATE.md unmeasured-ctx blocker). They are the SINGLE home of the
// reservation-math literals.
const (
	// defaultWebTopK is the retrieval top-K fallback when WebSearchInputs.TopK is
	// unset (the OWUI RAG_TOP_K default).
	defaultWebTopK = 3
	// defaultWebChunkSizeChars is the per-chunk char-size fallback when
	// WebSearchInputs.ChunkSizeChars is unset (the OWUI CHUNK_SIZE default).
	defaultWebChunkSizeChars = 1000
	// defaultWebResultCount is the fetched-result fallback when
	// WebSearchInputs.ResultCount is unset (the WEB_SEARCH_RESULT_COUNT default).
	defaultWebResultCount = 3
	// webCharsPerTokenX10 is ~3.5 chars/token expressed ×10 (35) for integer-exact
	// fixed-point division — a conservative (low) chars-per-token over-estimates tokens.
	webCharsPerTokenX10 = 35
	// webCitationTokensPerResult is the per-result citation overhead (URL + title +
	// snippet) added to the injected-chunk budget.
	webCitationTokensPerResult = 64
	// webBytesPerCtxToken is a conservative per-context-token KV-cache byte estimate
	// for the reservation (sized above typical Q4 KV-per-token to over-reserve).
	webBytesPerCtxToken = 4096
	// webSafetyFactorX10 is a 1.5× safety pad (×10 = 15) for chunk overlap, prompt
	// scaffolding, and estimation error; divided back out in webSearchReservation.
	webSafetyFactorX10 = 15
	// maxWeb* clamp pathological (hand-edited config.toml) tuning values so the reservation
	// products cannot overflow uint64 and wrap to a SMALL under-reservation (which would let
	// a search-on host silently CPU-fall-back — the exact GROUND-03 failure the reservation
	// exists to prevent). Clamping UP-TO the max over-reserves (fail-closed), never under.
	// The maxima are far above any sane OWUI config yet keep every product well below 2^64.
	maxWebTopK           = 100
	maxWebChunkSizeChars = 100000
	maxWebResultCount    = 100
)

// recommendSchemaVersion is the Recommendation contract self-version. It is the
// LAST tagged field of Recommendation and surfaces unconditionally in --json so
// dashboards can gate on additive growth (D-06/D-07). Bumped 1→2 when the
// append-only embedding_reservation_bytes + memory_considered fields landed
// (Phase 22, D-03). Bumped 2→3 when the append-only coder block landed
// (Phase 24, D-07). Bumped 3->4 when the append-only web_search_reservation_bytes
// field landed (Phase 31, GROUND-03) -- the single sanctioned recommend contract
// bump for Phase 31.
const recommendSchemaVersion = 4

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

	// EmbeddingReservationBytes is the embedding-model footprint subtracted from
	// the envelope BEFORE the chat-model fit when memory is enabled (D-01/D-03).
	// Zero when memory is off — the off-path JSON shape changes only by this key.
	EmbeddingReservationBytes uint64 `json:"embedding_reservation_bytes"`

	// MemoryConsidered marks whether the memory reservation was applied to this
	// pick (D-03): true whenever memory inputs were enabled — including refusals,
	// which honestly report the reservation they would have applied.
	MemoryConsidered bool `json:"memory_considered"`

	// Coder is the agent-profile coder fit + derived residency mode (D-06): the
	// fit is computed at each coder entry's catalog agent_ctx against the
	// post-reservation envelope. It is ALWAYS stamped — including on refusals,
	// where it carries fits:false / residency:"shared" — and deliberately NOT
	// omitempty so the residency basis is never hidden (D-07, Pitfall 6).
	Coder CoderFit `json:"coder"`

	// WebSearchReservationBytes is the web-search RAG-injection budget subtracted
	// from the envelope BEFORE the chat-model fit when web search is enabled
	// (GROUND-03), mirroring EmbeddingReservationBytes. It is reserved AFTER the
	// embedding reservation and BEFORE pickBest/pickOverride so a search-enabled
	// envelope cannot silently CPU-fall-back. Zero when web search is off — the
	// off-path JSON shape changes only by this key (byte-identical-off intent).
	WebSearchReservationBytes uint64 `json:"web_search_reservation_bytes"`

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

// MemoryInputs carries the memory-stack inputs Pick reserves BEFORE the
// chat-model fit (D-01). The zero value means memory off — provably
// byte-identical math to the pre-memory contract. Pure-core rule: callers (the
// cmd tier) load config and thread these explicitly; Pick never loads config.
type MemoryInputs struct {
	// Enabled mirrors the persisted memory_enabled gate. Callers fail SOFT: a
	// config load error threads the zero value, never an error-path change.
	Enabled bool
	// EmbeddingModel is the persisted embedding model id whose footprint is
	// reserved; an unrecognized id reserves the conservative default (D-02).
	EmbeddingModel string
}

// WebSearchInputs carries the web-search RAG inputs Pick reserves BEFORE the
// chat-model fit, AFTER the embedding reservation (GROUND-03). The zero value
// means web search off — provably byte-identical math to the pre-web-search
// contract. Pure-core rule: callers (the cmd tier) load config and thread these
// explicitly; Pick never loads config. Mirrors MemoryInputs exactly.
type WebSearchInputs struct {
	// Enabled mirrors the persisted web_search_enabled gate. Callers fail SOFT: a
	// config load error threads the zero value, never an error-path change.
	Enabled bool
	// ResultCount is the operator-tunable WEB_SEARCH_RESULT_COUNT — the number of
	// result pages fetched per query (cfg.WebSearchResultCount).
	ResultCount int
	// TopK is the retrieval top-K actually injected into the chat context per query
	// (OWUI RAG_TOP_K). It, with ChunkSizeChars, drives the reservation math.
	TopK int
	// ChunkSizeChars is the per-chunk character size OWUI chunks fetched pages into
	// (OWUI CHUNK_SIZE). TopK × ChunkSizeChars chars is the injected payload bound.
	ChunkSizeChars int
}

// Pick selects the single best fitting model for the host, applies and re-
// validates any overrides, and returns a fully-populated Recommendation. When
// memory is enabled the embedding-model footprint is reserved off the envelope
// FIRST (D-01) so the fit verdict, headroom, OOM guard and UsableEnvelopeBytes
// all see the shrunken value (SC#1).
func Pick(p detect.HostProfile, c catalog.Catalog, ov Overrides, mem MemoryInputs, web WebSearchInputs) Recommendation {
	reservation, memNotes := memoryReservation(mem)
	webRes, webNotes := webSearchReservation(web)

	envelope, degraded, ok := resolveEnvelope(p)
	if !ok {
		// No usable envelope and no safe floor derivable — refuse rather than
		// guess high (D-14). Empty Model signals the refusal. The refusal still
		// stamps both reservations as computed (honest surface, D-03/GROUND-03)
		// and the conservative-floor coder block (D-06/D-07: swap requires a
		// PROVEN fit, so a refusal stamps fits:false / residency:"shared").
		return finalizeRecommendation(Recommendation{
			Backend: defaultBackend,
			Notes:   append(combineNotes(memNotes, webNotes), "refusing to recommend: usable memory envelope is unknown and no safe floor is derivable (neither GTT envelope nor total RAM detected)"),
		}, p, mem, reservation, webRes, sharedCoderFit())
	}

	// D-01 / GROUND-03: the envelope shrinks by BOTH reservations BEFORE the
	// degraded note and BEFORE pickOverride/pickBest. Never wrap a uint64 — the
	// combined reservation is summed with saturating add (a saturated sum can
	// never be < envelope), and a total at or above the envelope clamps to 0 and
	// falls into pickBest's existing no-fit refusal.
	total := addSaturating(reservation, webRes)
	if total >= envelope {
		envelope = 0
	} else {
		envelope -= total
	}

	notes := combineNotes(memNotes, webNotes)
	if degraded {
		notes = append(notes, fmt.Sprintf(
			"DEGRADED ESTIMATE: real GTT envelope unknown; sized against a conservative %.0f%%-of-RAM floor (%s). Verify before relying on this pick (D-14).",
			degradedFloorFraction*100, humanGiB(envelope)))
	}

	// The coder fit runs against the POST-reservation envelope (D-05 ordering:
	// envelope → reservation → chat fit → coder fit) and is locked to each
	// entry's agent_ctx — pickCoder takes no Overrides by construction (D-04).
	coder := pickCoder(c, envelope)

	// An explicit --model override takes precedence and is re-validated.
	if ov.Model != "" {
		return finalizeRecommendation(pickOverride(c, ov, envelope, degraded, notes), p, mem, reservation, webRes, coder)
	}

	return finalizeRecommendation(pickBest(c, ov, envelope, degraded, notes), p, mem, reservation, webRes, coder)
}

// combineNotes concatenates two note slices, preserving the existing degraded-note
// ordering (memory notes first, then web-search notes) and returning nil when both
// are empty so the byte-identical-off note shape is preserved.
func combineNotes(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make([]string, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

// memoryReservation resolves the D-01 embedding reservation from the memory
// inputs: zero with no notes when memory is off; the pinned footprint when the
// model id is recognized; the conservative default plus an honest D-02 note
// naming the model when the footprint is typed-Unknown — NEVER a silent 0
// reservation. The byte value flows only from internal/memory (single source).
func memoryReservation(mem MemoryInputs) (uint64, []string) {
	if !mem.Enabled {
		return 0, nil
	}
	fp := memory.Footprint(mem.EmbeddingModel)
	if fp.Known {
		return fp.Value, nil
	}
	reserved := memory.ConservativeFootprintBytes()
	return reserved, []string{fmt.Sprintf(
		"RESERVED CONSERVATIVELY: no pinned footprint for embedding model %q — reserving the conservative default %s before the chat-model fit (D-02).",
		mem.EmbeddingModel, humanGiB(reserved))}
}

// webSearchReservation resolves the GROUND-03 web-search RAG-injection budget from
// the web inputs: zero with no notes when web search is off (so the off-envelope
// fit stays byte-identical to v1.4); otherwise a CONSERVATIVE byte reservation
// derived from the A6 formula, returned with an honest budget note naming it.
//
// A6 formula (deliberately conservative — over-reserving is safe; on-hardware
// tuning is deferred to Phase 33/34, STATE.md unmeasured-ctx blocker):
//
//	injected_chars   = TopK × ChunkSizeChars         (the retrieved chunks injected per query)
//	injected_tokens  = injected_chars ÷ 3.5 chars/token
//	citation_tokens  = ResultCount × citationTokensPerResult   (URL + title + snippet overhead)
//	reserved_bytes   = (injected_tokens + citation_tokens) × bytesPerCtxToken × safetyFactor
//
// bytesPerCtxToken is a conservative per-context-token KV-cache byte estimate;
// safetyFactor pads for chunk-overlap, prompt scaffolding, and estimation error.
// All terms use TopK/ChunkSizeChars/ResultCount sane fallbacks when zero so an
// Enabled:true with unset tuning still reserves a non-zero conservative budget.
func webSearchReservation(web WebSearchInputs) (uint64, []string) {
	if !web.Enabled {
		return 0, nil
	}

	// Conservative defaults (the OWUI RAG defaults documented in 31-RESEARCH A6)
	// when a caller threads an unset (zero) tuning value — never reserve 0 when on.
	topK := web.TopK
	if topK <= 0 {
		topK = defaultWebTopK
	}
	chunkChars := web.ChunkSizeChars
	if chunkChars <= 0 {
		chunkChars = defaultWebChunkSizeChars
	}
	resultCount := web.ResultCount
	if resultCount <= 0 {
		resultCount = defaultWebResultCount
	}

	// Clamp pathological hand-edited tuning to conservative maxima before the products below,
	// so an absurd value cannot overflow uint64 and wrap to a small under-reservation (GROUND-03).
	if topK > maxWebTopK {
		topK = maxWebTopK
	}
	if chunkChars > maxWebChunkSizeChars {
		chunkChars = maxWebChunkSizeChars
	}
	if resultCount > maxWebResultCount {
		resultCount = maxWebResultCount
	}

	// injected_tokens = (TopK × ChunkSizeChars) ÷ charsPerToken (×10 fixed-point
	// to keep the divide integer-exact without floats).
	injectedChars := uint64(topK) * uint64(chunkChars)
	injectedTokens := (injectedChars * 10) / webCharsPerTokenX10
	citationTokens := uint64(resultCount) * webCitationTokensPerResult
	rawTokens := injectedTokens + citationTokens

	// reserved_bytes = rawTokens × bytesPerCtxToken × safetyFactor (the ×10
	// fixed-point safety factor is divided back out).
	reserved := (rawTokens * webBytesPerCtxToken * webSafetyFactorX10) / 10

	return reserved, []string{fmt.Sprintf(
		"RESERVED for web-search: holding back %s before the chat-model fit for the web-RAG injection budget (top-K %d × %d-char chunks + %d citations) so a search-on envelope cannot silently CPU-fall-back (GROUND-03).",
		humanGiB(reserved), topK, chunkChars, resultCount)}
}

// finalizeRecommendation stamps the additive, contract-level fields onto a
// fully-computed pick: the unconditional SchemaVersion, the D-03 memory fields,
// the D-07 coder block, and the purely-derived ROCm advice. It runs AFTER
// Backend is set and NEVER reassigns rec.Backend — advice can only annotate the
// pick, never auto-switch it (REC-04, T-10-06). It performs no I/O: the advice
// is folded from p.ROCmReadiness already in hand. EVERY Pick return path —
// including the no-envelope refusal — flows through here, so the D-03 memory
// fields and the coder block are stamped unconditionally (the refusal path
// passes the conservative-floor block: fits:false / residency:"shared", D-06).
func finalizeRecommendation(rec Recommendation, p detect.HostProfile, mem MemoryInputs, reservation, webRes uint64, coder CoderFit) Recommendation {
	rec.SchemaVersion = recommendSchemaVersion
	rec.EmbeddingReservationBytes = reservation
	rec.WebSearchReservationBytes = webRes
	rec.MemoryConsidered = mem.Enabled
	rec.Coder = coder
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
		if m.Role == "coder" {
			continue // chat path excludes coder entries — absent role ⇒ chat;
			// coder entries are sized separately by pickCoder (D-03)
		}
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
	if m.Role == "coder" {
		// Warn-and-allow (Open Question 1, the D-07 override philosophy): a
		// coder entry can be forced onto the CHAT pick, but loudly. The chat KV
		// for an override still uses effectiveCtx — only pickCoder is
		// agent_ctx-locked (D-04).
		notes = append(notes, fmt.Sprintf("WARNING: model %q is a role:\"coder\" entry — using it as the CHAT pick only because you overrode (D-07); the coder fit/residency is computed separately at its agent profile", m.ID))
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
	// Saturating sum (WR-07): a saturated KV term (absurd --ctx) must keep the
	// total at MaxUint64 — a wrapped-small total would flip Fits true and feed a
	// silent OOM into the rendered unit's -c and the ceiling stress math.
	total := addSaturating(addSaturating(m.WeightBytes, kv), headroom)

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
