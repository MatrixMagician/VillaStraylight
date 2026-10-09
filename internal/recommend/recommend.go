// Package recommend turns a detected HostProfile plus the model catalog into a
// single memory-safe Recommendation: a model/quant/context/backend that provably
// fits the usable memory envelope (model_bytes + KV-cache@ctx + headroom ≤
// usable_envelope), with every term of that inequality surfaced so the CLI can
// SHOW the user why it fits.
//
// Pick is a PURE function (no I/O) so it is exhaustively table-testable. It never
// auto-selects an entry flagged unified_memory_safe:false, never auto-
// selects the bootstrap entry, defaults the backend to rocm for gfx1151
// re-validates manual overrides and warns/fails when they don't fit
// and degrades safely to a conservative floor when the envelope is Unknown
// — refusing only when no safe floor is derivable, never guessing high.
package recommend

import (
	"cmp"
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/memory"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/voice"
)

// defaultBackend is the inference backend recommended for gfx1151. ROCm
// is the default; Vulkan RADV remains selectable as an explicit opt-in fallback.
const defaultBackend = "rocm"

// fallbackBackend is the backend recommended INSTEAD of the ROCm default when the
// host is confidently not ROCm-ready (finalizeRecommendation). Vulkan RADV has the
// widest hardware/driver compatibility, so it is the safe landing spot.
const fallbackBackend = "vulkan"

// IsROCmFamily reports whether a recommended/persisted backend name is ROCm-family.
// It deliberately mirrors inference.IsROCmFamily rather than importing it: recommend
// is a pure fit-math package with no dependency on the inference seam, and the two
// enumerate the same NAME strings (never an image literal). The empty string counts
// because it resolves to the default ROCm backend.
func IsROCmFamily(name string) bool {
	switch name {
	case "", "rocm", "rocm-10.0", "rocm-7.2.4", "rocm-6.4.4", "rocm-6.4.4-rocwmma":
		return true
	default:
		return false
	}
}

// Web-search reservation constants (31-RESEARCH Assumption A6). The
// reservation is deliberately CONSERVATIVE — over-reserving keeps the chat model
// GPU-resident under search load; on-hardware tuning of these is deferred to
// Phase 33/34 (STATE.md unmeasured-ctx blocker). They are the SINGLE home of the
// reservation-math literals.
const (
	// defaultWebTopK is the retrieval top-K fallback when webSearchInputs.TopK is
	// unset (the OWUI RAG_TOP_K default).
	defaultWebTopK = 3
	// defaultWebChunkSizeChars is the per-chunk char-size fallback when
	// webSearchInputs.ChunkSizeChars is unset (the OWUI CHUNK_SIZE default).
	defaultWebChunkSizeChars = 1000
	// defaultWebResultCount is the fetched-result fallback when
	// webSearchInputs.ResultCount is unset (the WEB_SEARCH_RESULT_COUNT default).
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
	// a search-on host silently CPU-fall-back — the exact failure the reservation
	// exists to prevent). Clamping UP-TO the max over-reserves (fail-closed), never under.
	// The maxima are far above any sane OWUI config yet keep every product well below 2^64.
	maxWebTopK           = 100
	maxWebChunkSizeChars = 100000
	maxWebResultCount    = 100
)

// recommendSchemaVersion is the Recommendation contract self-version. It is the
// LAST tagged field of Recommendation and surfaces unconditionally in --json so
// dashboards can gate on additive growth. Bumped 1→2 when the
// append-only embedding_reservation_bytes + memory_considered fields landed
// (Phase 22). Bumped 2→3 when the append-only coder block landed
// (Phase 24). Bumped 3->4 when the append-only web_search_reservation_bytes
// field landed (Phase 31) -- the single sanctioned recommend contract
// bump for Phase 31.
// Bumped 4->5 when the append-only speculation field landed (ADR-0006).
// Bumped 5->6 when the append-only projector_bytes + vision fields landed.
// Bumped 6->7 when the append-only draft_bytes + draft_kv_bytes fields landed
// (ADR-0009): the draft sidecar's own reserved weight and KV-at-ctx terms.
// Bumped 7->8 when the append-only prompt_cache_bytes field landed (ADR-0021):
// the llama-server prompt cache, counted in the fit as its own term.
// Bumped 8->9 when the append-only reservations array landed (ADR-0027).
const recommendSchemaVersion = 9

// ROCmAdvice is a typed enum surfaced on the Recommendation: an
// honesty-bounded hint about whether the opt-in ROCm backend is worth a benchmark
// on this host, derived PURELY from HostProfile.rocm_readiness inside Pick (no I/O,
// no new arg). It NEVER promises a speed-up — the on-hardware token-gen delta was
// negative (Δtg −11.15), so ROCm can REGRESS tg. Empty ("") means "not applicable"
// (a Known-bad readiness signal withholds advice and names the blocker in a Note).
type ROCmAdvice string

const (
	// ROCmAdviceReady is reserved for a host that is fully validated as ROCm-ready.
	// Phase 10 derives only worth-trying / verify-with-bench / withheld from the
	// readiness fold; the const is defined for contract completeness.
	ROCmAdviceReady ROCmAdvice = "ready"
	// ROCmAdviceWorthTrying means every readiness signal is Known-good — ROCm is
	// worth a benchmark, but the advice still points at `villa bench` and never
	// promises a win.
	ROCmAdviceWorthTrying ROCmAdvice = "worth-trying"
	// ROCmAdviceVerifyBench means at least one readiness signal is unevaluable
	// (off-hardware default) — the honest answer is "verify with villa bench".
	ROCmAdviceVerifyBench ROCmAdvice = "verify-with-bench"
)

// rocmAdviceNote is the LOCKED honesty-safe Note copy (RESEARCH Pattern 4).
// It points the user at `villa bench --ab` and deliberately contains none of
// "faster"/"guaranteed"/"speed-up" — ROCm's win is prompt-processing-weighted and
// token generation may regress (on-hardware UAT Δtg −11.15). Since ROCm is the
// DEFAULT backend, the copy describes the selected pick rather than pitching an
// opt-in, and still refuses to claim a win: the honest answer is to measure. Tested.
const rocmAdviceNote = "ROCm: this host looks ROCm-ready, so the default rocm backend is selected. It is prompt-processing-weighted — token generation may not improve (and can regress vs vulkan). Verify on your model with: villa bench --ab"

// rocmVerifyNote is the verify-with-bench Note: same honesty discipline, but it
// makes no readiness claim because at least one signal is unevaluable.
const rocmVerifyNote = "ROCm: readiness could not be fully evaluated on this host, so the default rocm backend is kept (an unproven signal is not treated as a failure). If inference misbehaves, fall back with: villa backend set vulkan. Compare the two with: villa bench --ab"

// Recommendation is the result of Pick — and the --json / Phase-5 dashboard
// contract. A golden-file test guards its shape. Every fit term is
// populated so the command layer can render the full inequality.
type Recommendation struct {
	// The pick. Model is empty only on a refusal (no safe envelope).
	Model      string `json:"model"`
	Quant      string `json:"quant"`
	ContextLen int    `json:"context_len"`
	Backend    string `json:"backend"`

	// The fit terms plus the ceiling and verdict: the user can see
	// WeightBytes + KVCacheBytes + HeadroomBytes + PromptCacheBytes = TotalBytes
	// ≤ UsableEnvelopeBytes (before the optional projector and draft terms).
	WeightBytes         uint64 `json:"weight_bytes"`
	KVCacheBytes        uint64 `json:"kv_cache_bytes"`
	HeadroomBytes       uint64 `json:"headroom_bytes"`
	TotalBytes          uint64 `json:"total_bytes"`
	UsableEnvelopeBytes uint64 `json:"usable_envelope_bytes"`

	// Fits is the verdict; Degraded marks a conservative-floor estimate.
	Fits     bool `json:"fits"`
	Degraded bool `json:"degraded"`

	// Notes carries caveats: degraded-floor warnings, unsafe-override warnings,
	// override-doesn't-fit warnings, and refusal reasons.
	Notes []string `json:"notes"`

	// Alternatives are other fitting picks (surfaced behind --alternatives).
	Alternatives []Alternative `json:"alternatives,omitempty"`

	// ROCmAdvice + ROCmNote are the tail-appended, honesty-bounded ROCm hint
	// derived purely from HostProfile.rocm_readiness inside Pick.
	// Both are omitempty so the contract is unchanged when advice is not applicable.
	// They NEVER change Backend and NEVER promise a speed-up.
	ROCmAdvice ROCmAdvice `json:"rocm_advice,omitempty"`
	ROCmNote   string     `json:"rocm_note,omitempty"`

	// EmbeddingReservationBytes is the embedding-model footprint subtracted from
	// the envelope BEFORE the chat-model fit when memory is enabled.
	// Zero when memory is off — the off-path JSON shape changes only by this key.
	EmbeddingReservationBytes uint64 `json:"embedding_reservation_bytes"`

	// MemoryConsidered marks whether the memory reservation was applied to this
	// pick: true whenever memory inputs were enabled — including refusals,
	// which honestly report the reservation they would have applied.
	MemoryConsidered bool `json:"memory_considered"`

	// Coder is the agent-profile coder fit + derived residency mode: the
	// fit is computed at each coder entry's catalog agent_ctx against the
	// post-reservation envelope. It is ALWAYS stamped — including on refusals,
	// where it carries fits:false / residency:"shared" — and deliberately NOT
	// omitempty so the residency basis is never hidden (Pitfall 6).
	Coder CoderFit `json:"coder"`

	// WebSearchReservationBytes is the web-search RAG-injection budget subtracted
	// from the envelope BEFORE the chat-model fit when web search is enabled
	// mirroring EmbeddingReservationBytes. It is reserved AFTER the
	// embedding reservation and BEFORE pickBest/pickOverride so a search-enabled
	// envelope cannot silently CPU-fall-back. Zero when web search is off — the
	// off-path JSON shape changes only by this key (byte-identical-off intent).
	WebSearchReservationBytes uint64 `json:"web_search_reservation_bytes"`

	// Speculation is the resolved speculation mode for this pick (ADR-0006): the
	// requested value when it is honourable, else what the entry's qualification
	// licenses. Empty on a refusal that named no model, since no entry's
	// qualification could have resolved one.
	Speculation string `json:"speculation"`

	// ProjectorBytes is the vision projector's share of TotalBytes: the server's
	// worst-case runtime estimate, reserved as its own fit term so the cost is
	// visible rather than hidden inside the model weight. Zero when the picked entry
	// ships no projector AND when one was dropped for not fitting.
	ProjectorBytes uint64 `json:"projector_bytes"`

	// Vision is the resolved vision decision for this pick: true only when the
	// entry ships a projector AND it fit. A pick that dropped the projector reports
	// false and says so in a Note, because a text-only stack presented as
	// vision-capable is the one outcome this field exists to prevent.
	Vision bool `json:"vision"`

	// DraftBytes is the draft sidecar's reserved weight, its share of TotalBytes
	// (ADR-0009): zero when the picked entry ships no draft AND when one was
	// dropped for not fitting on top of the base (+ projector) fit.
	DraftBytes uint64 `json:"draft_bytes"`

	// DraftKVBytes is the draft's own KV-cache reservation at the served ctx,
	// sized from the DRAFT's dimensions (never the target's). Zero under the
	// same conditions as DraftBytes.
	DraftKVBytes uint64 `json:"draft_kv_bytes"`

	// PromptCacheBytes is the llama-server RAM prompt cache's share of TotalBytes
	// (ADR-0021): the `--cache-ram` cap villa renders, held in host RSS on top of
	// the weights and KV, so the fit reserves it rather than letting it eat the
	// headroom. It is the inference seam's constant, never restated here, and zero
	// on a refusal (which has no total).
	PromptCacheBytes uint64 `json:"prompt_cache_bytes"`

	// Reservations is every auxiliary reservation subtracted from the envelope
	// before the chat-model fit, in registry order (ADR-0027). Never nil, so the
	// key is always [] when nothing is reserved. EmbeddingReservationBytes,
	// WebSearchReservationBytes and MemoryConsidered are derived from it.
	Reservations []Reservation `json:"reservations"`

	// SchemaVersion is the Recommendation contract self-version and MUST stay the
	// LAST tagged field (append-only discipline; new fields go above it).
	SchemaVersion int `json:"schema_version"`
}

// ReservedBytes is the saturating sum of every reservation, the total Pick took
// off the envelope. Consumers of the total read it here rather than re-summing.
func (r Recommendation) ReservedBytes() uint64 {
	var total uint64
	for _, res := range r.Reservations {
		total = addSaturating(total, res.Bytes)
	}
	return total
}

// Alternative is a compact view of another fitting pick.
type Alternative struct {
	Model      string `json:"model"`
	Quant      string `json:"quant"`
	ContextLen int    `json:"context_len"`
	TotalBytes uint64 `json:"total_bytes"`
}

// Overrides are user-supplied selections. A zero value means
// "unset" for that field.
type Overrides struct {
	Model string
	Quant string
	Ctx   int
	// Speculation is the persisted or requested speculation mode. Empty lets the
	// picked entry's qualification decide.
	Speculation string
}

// ResolveSpeculation answers what speculation mode a pick of m should run, and
// whether the answer honours what was asked. An unset request is resolved from the
// entry's qualification, preferring a draft sidecar over ngram when both would
// qualify; an explicit request for a mode the entry is not qualified for (or, for
// draft, does not fit) is a REFUSAL (ok false), never a silent downgrade to off,
// because an operator who asked for speculation and got none would have no way to
// tell. draftFits is the caller's answer to whether m.Draft's reserved weight+KV
// fit on top of the base (and projector) fit at the served ctx (ADR-0009) — a
// fit question ResolveSpeculation itself has no envelope to answer.
func ResolveSpeculation(m catalog.Model, requested string, draftFits bool) (mode, note string, ok bool) {
	draftQualified := m.Draft != nil && draftFits
	switch requested {
	case "":
		if draftQualified {
			return config.SpeculationDraft, fmt.Sprintf("speculation: draft (%s is qualified for %s: %s)", m.Draft.SpecType, m.ID, m.Draft.Provenance), true
		}
		if m.NgramSafe {
			return config.SpeculationNgram, fmt.Sprintf("speculation: ngram (ngram-mod is qualified for %s: %s)", m.ID, m.NgramProvenance), true
		}
		return config.SpeculationOff, fmt.Sprintf("speculation: off (%s has no qualified ngram measurement)", m.ID), true
	case config.SpeculationOff:
		return config.SpeculationOff, "", true
	case config.SpeculationNgram:
		if m.NgramSafe {
			return config.SpeculationNgram, fmt.Sprintf("speculation: ngram (ngram-mod is qualified for %s: %s)", m.ID, m.NgramProvenance), true
		}
		return config.SpeculationOff, fmt.Sprintf("speculation: ngram requested but %s is not qualified for it; refusing", m.ID), false
	case config.SpeculationDraft:
		if draftQualified {
			return config.SpeculationDraft, fmt.Sprintf("speculation: draft (%s is qualified for %s: %s)", m.Draft.SpecType, m.ID, m.Draft.Provenance), true
		}
		return config.SpeculationOff, fmt.Sprintf("speculation: draft requested but %s is not qualified for it; refusing", m.ID), false
	default:
		return config.SpeculationOff, fmt.Sprintf("speculation: %q is not a known mode (off, ngram, draft, or unset); refusing", requested), false
	}
}

// Reservation is memory a service beside the chat model holds off the envelope
// before the chat-model fit (ADR-0027). Name is the row's stable id in the
// recommend --json reservations array. Notes are folded into the pick's notes.
type Reservation struct {
	Name  string   `json:"name"`
	Bytes uint64   `json:"bytes"`
	Notes []string `json:"-"`
}

// The registry's row names. The legacy per-service keys are derived by name.
const (
	reservationEmbedding = "embedding"
	reservationReranker  = "reranker"
	reservationExtractor = "extractor"
	reservationWebSearch = "web_search"
	reservationImage     = "image"
)

// ReservationsFor is the reservation registry: one row for each service whose
// gate is on in cfg, in a fixed order (embedding, then the reranker and the
// extractor beside it, then web search, then stt and tts, then image). A new
// service is one row here. It is pure: it reads an already-loaded config and no
// host, so a config that failed to load (the zero value) reserves nothing.
func ReservationsFor(cfg config.VillaConfig) []Reservation {
	var res []Reservation
	if subsystem.MemoryOn(cfg) {
		bytes, notes := memoryReservation(cfg.EmbeddingModel)
		res = append(res, Reservation{Name: reservationEmbedding, Bytes: bytes, Notes: notes})
	}
	if subsystem.RerankOn(cfg) {
		res = append(res, Reservation{Name: reservationReranker, Bytes: memory.RerankFootprintBytes()})
	}
	if subsystem.ExtractOn(cfg) {
		res = append(res, Reservation{Name: reservationExtractor, Bytes: memory.ExtractFootprintBytes()})
	}
	if subsystem.WebSearchOn(cfg) {
		bytes, notes := webSearchReservation(webSearchInputs{ResultCount: cfg.WebSearchResultCount})
		res = append(res, Reservation{Name: reservationWebSearch, Bytes: bytes, Notes: notes})
	}
	if subsystem.VoiceOn(cfg) {
		res = append(res,
			// Two rows, not one: two processes hold the memory and the fit table
			// should say which. The names are the services' own.
			Reservation{Name: voice.STT.Name, Bytes: voice.STTFootprintBytes()},
			Reservation{Name: voice.TTS.Name, Bytes: voice.TTSFootprintBytes()},
		)
	}
	if subsystem.ImageOn(cfg) {
		bytes, notes := imageReservation(cfg.ImageModel)
		res = append(res, Reservation{Name: reservationImage, Bytes: bytes, Notes: notes})
	}
	return res
}

// conservativeImageBytes is reserved when image_model names no image-table entry:
// over-reserve, never 0 (the memoryReservation precedent). 24 GiB is above any
// Z-Image quant's measured weights plus compute. It only shapes a recommend
// preview, because the render refuses an unknown id.
const conservativeImageBytes uint64 = 24 << 30

// imageReservation resolves the image row from the compiled-in image table: the
// entry's measured params footprint plus its peak compute at the preset, held
// from start because the unit eager-loads. The bytes flow only from
// catalog.Image, so the table stays the single source. A miss reserves the
// conservative default with a note naming the id.
func imageReservation(id string) (uint64, []string) {
	m, ok := catalog.Image(id)
	if ok {
		return addSaturating(m.WeightBytes, m.ComputeBytes), nil
	}
	return conservativeImageBytes, []string{fmt.Sprintf(
		"RESERVED CONSERVATIVELY: image model %q is not in the image table — reserving the conservative default %s before the chat-model fit.",
		id, humanGiB(conservativeImageBytes))}
}

// webSearchInputs carries the web-search RAG inputs the web-search row is sized
// from. The gate is answered by ReservationsFor, never here.
type webSearchInputs struct {
	// ResultCount is the operator-tunable WEB_SEARCH_RESULT_COUNT — the number of
	// result pages fetched per query (cfg.WebSearchResultCount).
	ResultCount int
	// TopK is the retrieval top-K actually injected into the chat context per query
	// (OWUI RAG_TOP_K). It, with ChunkSizeChars, drives the reservation math. It has
	// no config field, so the registry leaves it zero and the OWUI default applies.
	TopK int
	// ChunkSizeChars is the per-chunk character size OWUI chunks fetched pages into
	// (OWUI CHUNK_SIZE). TopK × ChunkSizeChars chars is the injected payload bound.
	ChunkSizeChars int
}

// Pick selects the single best fitting model for the host, applies and re-
// validates any overrides, and returns a fully-populated Recommendation. Every
// reservation in res is subtracted from the envelope FIRST, so the fit verdict,
// headroom, OOM guard and UsableEnvelopeBytes all see the shrunken value. A nil
// res sizes against the whole envelope.
func Pick(p detect.HostProfile, c catalog.Catalog, ov Overrides, res []Reservation) Recommendation {
	var total uint64
	var notes []string
	for _, r := range res {
		total = addSaturating(total, r.Bytes)
		notes = append(notes, r.Notes...)
	}

	envelope, degraded, ok := resolveEnvelope(p)
	if !ok {
		// No usable envelope and no safe floor derivable — refuse rather than
		// guess high. Empty Model signals the refusal. The refusal still
		// stamps the reservations as computed (honest surface)
		// and the conservative-floor coder block (swap requires a
		// PROVEN fit, so a refusal stamps fits:false / residency:"shared").
		return finalizeRecommendation(Recommendation{
			Backend: defaultBackend,
			Notes:   append(notes, "refusing to recommend: usable memory envelope is unknown and no safe floor is derivable (neither GTT envelope nor total RAM detected)"),
		}, p, res, sharedCoderFit())
	}

	// The envelope shrinks by every reservation BEFORE the degraded note and
	// BEFORE pickOverride/pickBest. Never wrap a uint64 — the total is summed with
	// saturating add (a saturated sum can never be < envelope), and a total at or
	// above the envelope clamps to 0 and falls into pickBest's existing no-fit refusal.
	if total >= envelope {
		envelope = 0
	} else {
		envelope -= total
	}

	if degraded {
		notes = append(notes, fmt.Sprintf(
			"DEGRADED ESTIMATE: real GTT envelope unknown; sized against a conservative %.0f%%-of-RAM floor (%s). Verify before relying on this pick.",
			degradedFloorFraction*100, humanGiB(envelope)))
	}

	// The coder fit runs against the POST-reservation envelope (ordering:
	// envelope → reservation → chat fit → coder fit) and is locked to each
	// entry's agent_ctx — pickCoder takes no Overrides by construction.
	coder := pickCoder(c, envelope)

	// An explicit --model override takes precedence and is re-validated.
	if ov.Model != "" {
		return finalizeRecommendation(pickOverride(c, ov, envelope, degraded, notes), p, res, coder)
	}

	return finalizeRecommendation(pickBest(c, ov, envelope, degraded, notes), p, res, coder)
}

// memoryReservation resolves the embedding reservation for the embedding model
// id: the pinned footprint when the model id is recognized; the conservative default plus an honest note
// naming the model when the footprint is typed-Unknown — NEVER a silent 0
// reservation. The byte value flows only from internal/memory (single source).
func memoryReservation(model string) (uint64, []string) {
	fp := memory.Footprint(model)
	if fp.Known {
		return fp.Value, nil
	}
	reserved := memory.ConservativeFootprintBytes()
	return reserved, []string{fmt.Sprintf(
		"RESERVED CONSERVATIVELY: no pinned footprint for embedding model %q — reserving the conservative default %s before the chat-model fit.",
		model, humanGiB(reserved))}
}

// webSearchReservation resolves the web-search RAG-injection budget from
// the web inputs: a CONSERVATIVE byte reservation
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
// All terms use TopK/ChunkSizeChars/ResultCount sane fallbacks when zero so a
// row with unset tuning still reserves a non-zero conservative budget.
func webSearchReservation(web webSearchInputs) (uint64, []string) {
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
	// so an absurd value cannot overflow uint64 and wrap to a small under-reservation.
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
		"RESERVED for web-search: holding back %s before the chat-model fit for the web-RAG injection budget (top-K %d × %d-char chunks + %d citations) so a search-on envelope cannot silently CPU-fall-back.",
		humanGiB(reserved), topK, chunkChars, resultCount)}
}

// finalizeRecommendation stamps the additive, contract-level fields onto a
// fully-computed pick: the unconditional SchemaVersion, the reservation fields,
// the coder block, and the purely-derived ROCm advice. It runs AFTER
// Backend is set. It performs no I/O: the advice is folded from p.ROCmReadiness
// already in hand.
//
// It reassigns rec.Backend in EXACTLY ONE direction: the safe fallback off the ROCm
// default onto Vulkan RADV when the host is CONFIDENTLY not ROCm-ready (every
// readiness signal Known and at least one Known-bad — the same withheld-advice
// condition deriveROCmAdvice reports, whose Note names the blocker). Since ROCm is
// now the default backend, recommending it to a host that provably cannot run it
// would hand the user a broken first install. It NEVER goes the other way: no
// readiness fold can auto-select ROCm over an explicit choice, and an unevaluable
// signal (the off-hardware default) never triggers the fallback — unknown must not
// silently downgrade a working ROCm host (no-false-red, mirroring the no-false-green
// discipline in deriveROCmAdvice). EVERY Pick return path
// including the no-envelope refusal — flows through here, so the memory
// fields and the coder block are stamped unconditionally (the refusal path
// passes the conservative-floor block: fits:false / residency:"shared").
func finalizeRecommendation(rec Recommendation, p detect.HostProfile, res []Reservation, coder CoderFit) Recommendation {
	rec.SchemaVersion = recommendSchemaVersion
	rec.Reservations = append([]Reservation{}, res...)
	for _, r := range res {
		switch r.Name {
		case reservationEmbedding:
			rec.EmbeddingReservationBytes = r.Bytes
			rec.MemoryConsidered = true
		case reservationWebSearch:
			rec.WebSearchReservationBytes = r.Bytes
		}
	}
	rec.Coder = coder
	advice, note := deriveROCmAdvice(p.ROCmReadiness)
	rec.ROCmAdvice = advice
	if note != "" {
		rec.ROCmNote = note
	}
	// Confidently-not-ROCm-ready → fall back to Vulkan RADV (see the doc comment).
	// deriveROCmAdvice withholds advice ("") ONLY in that all-Known-with-a-blocker
	// case, so the empty advice is the precise, single-sourced signal here; the
	// accompanying Note already names the blocker for the user.
	if advice == "" && IsROCmFamily(rec.Backend) {
		rec.Backend = fallbackBackend
	}
	return rec
}

// deriveROCmAdvice folds the five detect.ROCmReadiness signals into the honesty-
// bounded advice + Note (RESEARCH Pattern 4). It mirrors
// status.foldROCmReadiness's Known-first, worst-wins discipline (any unevaluable
// signal → unknown wins over a confidently-bad one; no-false-green):
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
		return "", fmt.Sprintf("ROCm: not ready on this host — blocked by %s. Falling back to the vulkan backend; re-check after resolving it (run villa status).", blocker)
	}
	if sawUnknown {
		return ROCmAdviceVerifyBench, rocmVerifyNote
	}
	return ROCmAdviceWorthTrying, rocmAdviceNote
}

// pickBest selects the heaviest auto-eligible model that fits, honoring an
// optional --ctx/--quant override on the chosen model. "Heaviest" is weight bytes,
// not total footprint — see outranks.
func pickBest(c catalog.Catalog, ov Overrides, envelope uint64, degraded bool, notes []string) Recommendation {
	best, alts := selectBest(c, ov, envelope)
	if best == nil {
		notes = append(notes, fmt.Sprintf("no catalog model fits the usable envelope of %s (with %.0f%% headroom and the %s prompt cache) — consider a smaller model or larger memory", humanGiB(envelope), headroomFraction*100, humanGiB(inference.PromptCacheBytes)))
		return Recommendation{
			Backend:             defaultBackend,
			UsableEnvelopeBytes: envelope,
			Degraded:            degraded,
			Notes:               notes,
		}
	}

	ctx := effectiveCtx(*best, ov)
	rec := buildRecommendation(*best, ov, ctx, envelope, degraded, notes)
	rec.Alternatives = alternativesExcluding(alts, best.ID, ctx)
	if ov.Quant != "" && ov.Quant != best.Quant {
		rec.Notes = append(rec.Notes, fmt.Sprintf("requested quant %q ignored: not represented in the catalog for %q (auto-selected %q)", ov.Quant, best.ID, best.Quant))
	}
	return rec
}

// selectBest walks the catalog and returns the best fitting auto-eligible model, plus
// every fitting pick (the chosen one included) as the alternatives to report.
func selectBest(c catalog.Catalog, ov Overrides, envelope uint64) (*catalog.Model, []Alternative) {
	headroom := headroomBytes(envelope)
	var best *catalog.Model
	var bestTotal uint64
	var alts []Alternative
	for i := range c.Models {
		m := c.Models[i]
		ctx, total, ok := chatFit(m, ov, envelope, headroom)
		if !ok {
			continue
		}
		alts = append(alts, Alternative{Model: m.ID, Quant: m.Quant, ContextLen: ctx, TotalBytes: total})
		if outranks(m, total, best, bestTotal) {
			bm := m
			best = &bm
			bestTotal = total
		}
	}
	return best, alts
}

// chatFit sizes m for the chat pick: its context, its fit total (weights + KV +
// headroom + the prompt cache, ADR-0021) and whether it is auto-eligible and fits.
func chatFit(m catalog.Model, ov Overrides, envelope, headroom uint64) (ctx int, total uint64, ok bool) {
	if !autoEligible(m) || !meetsEnvelopeFloor(m, envelope) {
		return 0, 0, false
	}
	ctx = effectiveCtx(m, ov)
	total = m.WeightBytes + kvCacheBytes(m, ctx) + headroom + inference.PromptCacheBytes
	// OOM guard: never select a pick that exceeds the envelope.
	return ctx, total, total <= envelope
}

// autoEligible reports whether the chat path may auto-select m: it is not a coder
// entry (coder entries are sized separately by pickCoder; an absent role means
// chat), not the bootstrap entry, and not flagged unified_memory_safe:false.
func autoEligible(m catalog.Model) bool {
	return m.Role != "coder" && !m.Bootstrap && m.UnifiedMemorySafe
}

// meetsEnvelopeFloor is the secondary floor guard: a model declaring a minimum
// envelope it needs to run acceptably is skipped when the host is below that floor,
// even if the raw weights+KV+headroom math would otherwise fit.
func meetsEnvelopeFloor(m catalog.Model, envelope uint64) bool {
	return m.MinEnvelopeBytes == 0 || envelope >= m.MinEnvelopeBytes
}

// outranks reports whether m (with fit total) beats the current best.
//
// "Best" = the most WEIGHT that still fits. Weight bytes are the capability
// proxy: they are the parameters, at the quant villa will actually run. The KV
// term is a COST the operator pays for context, not a measure of how capable
// the model is, so ranking on weights+KV lets a frugal attention geometry
// demote a genuinely larger model. Two of the seed entries are hybrids whose
// KV cache is a quarter the size of a dense entry's, and ranking by footprint
// handed the pick to the smaller model the moment their geometry was corrected.
// The total is the tie-break only, so the ordering stays deterministic when two
// entries carry identical weights.
func outranks(m catalog.Model, total uint64, best *catalog.Model, bestTotal uint64) bool {
	return best == nil || m.WeightBytes > best.WeightBytes || (m.WeightBytes == best.WeightBytes && total > bestTotal)
}

// alternativesExcluding returns the fitting picks other than the chosen model at
// the chosen context.
func alternativesExcluding(alts []Alternative, model string, ctx int) []Alternative {
	var out []Alternative
	for _, a := range alts {
		if a.Model == model && a.ContextLen == ctx {
			continue
		}
		out = append(out, a)
	}
	return out
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
		notes = append(notes, fmt.Sprintf("WARNING: model %q is flagged unified_memory_safe:false — it is known to misbehave on unified memory; using it only because you overrode", m.ID))
	}
	if m.Role == "coder" {
		// Warn-and-allow (Open Question 1, the override philosophy): a
		// coder entry can be forced onto the CHAT pick, but loudly. The chat KV
		// for an override still uses effectiveCtx — only pickCoder is
		// agent_ctx-locked.
		notes = append(notes, fmt.Sprintf("WARNING: model %q is a role:\"coder\" entry — using it as the CHAT pick only because you overrode; the coder fit/residency is computed separately at its agent profile", m.ID))
	}
	if ov.Quant != "" && ov.Quant != m.Quant {
		notes = append(notes, fmt.Sprintf("note: override quant %q differs from the catalog entry's %q; fit math uses the catalog entry's dimensions", ov.Quant, m.Quant))
	}

	ctx := effectiveCtx(m, ov)
	rec := buildRecommendation(m, ov, ctx, envelope, degraded, notes)
	if ov.Quant != "" {
		rec.Quant = ov.Quant
	}
	// Keyed on the memory comparison, not on Fits: Fits is also false for a
	// speculation refusal, which is a non-fit with no memory shortfall (#146).
	if rec.TotalBytes > envelope {
		rec.Notes = append(rec.Notes, fmt.Sprintf(
			"WARNING: your override does NOT fit — %s needed vs %s usable envelope; it will likely OOM. Reduce --ctx or pick a smaller model.",
			humanGiB(rec.TotalBytes), humanGiB(envelope)))
	}
	return rec
}

// buildRecommendation computes all fit terms for model m at ctx and assembles the
// Recommendation (without touching alternatives/override-specific notes).
func buildRecommendation(m catalog.Model, ov Overrides, ctx int, envelope uint64, degraded bool, notes []string) Recommendation {
	kv := kvCacheBytes(m, ctx)
	headroom := headroomBytes(envelope)
	// Saturating sum: a saturated KV term (absurd --ctx) must keep the
	// total at MaxUint64 — a wrapped-small total would flip Fits true and feed a
	// silent OOM into the rendered unit's -c and the ceiling stress math. The prompt
	// cache is the fifth base term (ADR-0021): the cap villa renders, held in host RSS.
	total := addSaturating(addSaturating(addSaturating(m.WeightBytes, kv), headroom), inference.PromptCacheBytes)

	backend := cmp.Or(m.BackendDefault, defaultBackend)

	proj := reserveProjector(m, total, envelope)
	draft := reserveDraft(m, ov, ctx, proj.total, envelope)
	notes = append(append(notes, proj.notes...), draft.notes...)

	return Recommendation{
		Model:               m.ID,
		Quant:               m.Quant,
		ContextLen:          ctx,
		Backend:             backend,
		WeightBytes:         m.WeightBytes,
		KVCacheBytes:        kv,
		HeadroomBytes:       headroom,
		TotalBytes:          draft.total,
		UsableEnvelopeBytes: envelope,
		PromptCacheBytes:    inference.PromptCacheBytes,
		// An unhonourable speculation request fails the fit outright: the pick
		// villa would install is not the one that was asked for.
		Fits:           draft.total <= envelope && draft.ok,
		Degraded:       degraded,
		Notes:          notes,
		Speculation:    draft.spec,
		ProjectorBytes: proj.bytes,
		Vision:         proj.vision,
		DraftBytes:     draft.bytes,
		DraftKVBytes:   draft.kv,
	}
}

// projectorFit is the vision projector's contribution to a fit: the running total
// with it reserved (or unchanged when it is not), the bytes reserved, whether vision
// is on, and the note a dropped projector owes the operator.
type projectorFit struct {
	total, bytes uint64
	vision       bool
	notes        []string
}

// reserveProjector reserves the projector only when the envelope has room for it ON
// TOP of the base fit. Dropping it is a NOTE rather than a non-fit: the model still
// runs, just text-only, and saying nothing is what would present a text-only stack
// as vision-capable.
func reserveProjector(m catalog.Model, total, envelope uint64) projectorFit {
	if m.Projector == nil {
		return projectorFit{total: total}
	}
	withProj := addSaturating(total, m.Projector.WeightBytes)
	if withProj > envelope {
		return projectorFit{total: total, notes: []string{fmt.Sprintf("vision: projector (%s) dropped — %s needed vs %s usable; this pick runs text-only",
			humanGiB(m.Projector.WeightBytes), humanGiB(withProj), humanGiB(envelope))}}
	}
	return projectorFit{total: withProj, bytes: m.Projector.WeightBytes, vision: true}
}

// draftTerms are the draft sidecar's own fit terms on top of a running total: its KV
// at the served ctx (from the DRAFT's dimensions), its weight + KV reserve, the total
// with that reserve added, and whether that total fits the envelope.
type draftTerms struct {
	kv, reserve, needed uint64
	fits                bool
}

// draftTermsFor sizes m's draft sidecar on top of total; zero when m ships none.
func draftTermsFor(m catalog.Model, ctx int, total, envelope uint64) draftTerms {
	if m.Draft == nil {
		return draftTerms{}
	}
	kv := draftKVCacheBytes(*m.Draft, ctx)
	reserve := addSaturating(m.Draft.WeightBytes, kv)
	needed := addSaturating(total, reserve)
	return draftTerms{kv: kv, reserve: reserve, needed: needed, fits: needed <= envelope}
}

// draftFit is the resolved speculation outcome: the running total with the draft
// reserved when the pick renders it, the draft's weight and KV bytes (zero otherwise),
// the resolved mode, whether that mode honours the request, and the notes owed.
type draftFit struct {
	total, bytes, kv uint64
	spec             string
	ok               bool
	notes            []string
}

// reserveDraft resolves speculation and reserves the draft sidecar AFTER the
// projector, so a tight envelope loses the speculative-decoding speedup before it
// loses vision (ADR-0009). Its own weight+KV (at the served ctx, from the DRAFT's
// dimensions) must fit ON TOP of the total so far; the fit then licenses
// ResolveSpeculation's ladder. Reserved only for a pick that will render the draft:
// an honoured ngram or off must not carry a sidecar the unit never loads.
func reserveDraft(m catalog.Model, ov Overrides, ctx int, total, envelope uint64) draftFit {
	t := draftTermsFor(m, ctx, total, envelope)
	spec, specNote, specOK := ResolveSpeculation(m, ov.Speculation, t.fits)
	d := draftFit{total: total, spec: spec, ok: specOK}
	if spec == config.SpeculationDraft {
		d.total, d.bytes, d.kv = t.needed, m.Draft.WeightBytes, t.kv
	} else if draftDropped(m, ov, t) {
		d.notes = append(d.notes, fmt.Sprintf("speculation: draft (%s) dropped — %s needed vs %s usable; falling back to %s",
			humanGiB(t.reserve), humanGiB(t.needed), humanGiB(envelope), spec))
	}
	if specNote != "" {
		d.notes = append(d.notes, specNote)
	}
	return d
}

// draftDropped reports whether m ships a draft that did not fit although the
// operator asked for it or left the choice to the entry's qualification.
func draftDropped(m catalog.Model, ov Overrides, t draftTerms) bool {
	return m.Draft != nil && !t.fits && (ov.Speculation == "" || ov.Speculation == config.SpeculationDraft)
}

// effectiveCtx returns the context length to size against: the override when set
// and positive, else the model's default.
func effectiveCtx(m catalog.Model, ov Overrides) int {
	if ov.Ctx > 0 {
		return ov.Ctx
	}
	return m.DefaultCtx
}

// humanGiB renders a byte count as a GiB string for user-facing notes.
func humanGiB(b uint64) string {
	return fmt.Sprintf("%.2f GiB", float64(b)/(1<<30))
}
