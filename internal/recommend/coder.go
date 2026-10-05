package recommend

import (
	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// This file implements the coder fit stage (CODER-02): after the embed
// reservation and the chat fit, Pick evaluates every role:"coder" catalog
// entry at its agent-profile context (m.AgentCtx, — the --ctx override is
// chat-only and is unreachable from here by construction) against the
// post-reservation envelope (ordering), and derives the residency mode as
// a PURE inequality output: "swap" when the best coder entry fits
// standalone, "shared" when none does — shared is the conservative floor,
// never a refusal. The resulting block is stamped on EVERY Pick return path,
// including refusals.

// Residency mode values. Swap requires a PROVEN fit; shared is the
// floor the agent falls back to (riding the chat endpoint). They are EXPORTED
// (ResidencySwap/ResidencyShared) so callers compare against the constant
// rather than a re-typed "swap"/"shared" literal (the install addon
// keys its swap-only refusal off ResidencyShared).
const (
	ResidencySwap   = "swap"
	ResidencyShared = "shared"
)

// CoderFit is the coder-fit block of the Recommendation contract:
// the agent-profile fit inequality (WeightBytes + KVCacheBytes@AgentCtx +
// HeadroomBytes + PromptCacheBytes = TotalBytes ≤ post-reservation envelope) plus the derived
// residency mode. NO field is omitempty — the block surfaces unconditionally
// in --json, including on refusals, so the residency basis is never hidden
// (Pitfall 6).
type CoderFit struct {
	// The best fitting coder entry. Model is empty when no coder entry fits
	// (Fits false, Residency "shared").
	Model string `json:"model"`
	Quant string `json:"quant"`

	// AgentCtx is the catalog-declared agent-profile context the fit was
	// computed at — never the --ctx chat override.
	AgentCtx int `json:"agent_ctx"`

	// The fit terms and verdict, mirroring the chat-pick inequality.
	WeightBytes   uint64 `json:"weight_bytes"`
	KVCacheBytes  uint64 `json:"kv_cache_bytes"`
	HeadroomBytes uint64 `json:"headroom_bytes"`
	TotalBytes    uint64 `json:"total_bytes"`
	Fits          bool   `json:"fits"`

	// Residency is the derived mode: "swap" when the best coder entry fits
	// standalone, "shared" when none does.
	Residency string `json:"residency"`

	// PromptCacheBytes is the llama-server RAM prompt cache's share of
	// TotalBytes (ADR-0021): the coder is served by the same villa-llama unit,
	// which renders --cache-ram. Appended last (append-only schema).
	PromptCacheBytes uint64 `json:"prompt_cache_bytes"`
}

// sharedCoderFit is the conservative-floor coder block: no proven fit, so
// residency degrades to "shared". It is also what the no-envelope
// refusal path stamps (swap requires a proven fit).
func sharedCoderFit() CoderFit {
	return CoderFit{Fits: false, Residency: ResidencyShared}
}

// pickCoder evaluates every role:"coder" catalog entry against the
// post-reservation envelope and returns the coder block. It mirrors pickBest's
// eligibility guards (Bootstrap/UnifiedMemorySafe/MinEnvelopeBytes and the
// most-capable-wins rule) with two deltas: only role:"coder" entries are
// considered, and ctx is ALWAYS m.AgentCtx. The total uses the
// saturating form so an overflowing KV term can never wrap to "fits".
func pickCoder(c catalog.Catalog, envelope uint64) CoderFit {
	headroom := headroomBytes(envelope)
	best, bestTotal := bestCoder(c, envelope, headroom)
	if best == nil {
		// Honest no-fit mirror of pickBest's note path: shared is the floor,
		// never a refusal.
		return sharedCoderFit()
	}
	return CoderFit{
		Model:            best.ID,
		Quant:            best.Quant,
		AgentCtx:         best.AgentCtx,
		WeightBytes:      best.WeightBytes,
		KVCacheBytes:     kvCacheBytes(*best, best.AgentCtx),
		HeadroomBytes:    headroom,
		PromptCacheBytes: inference.PromptCacheBytes,
		TotalBytes:       bestTotal,
		Fits:             true,
		Residency:        ResidencySwap,
	}
}

// bestCoder returns the eligible coder entry with the largest footprint that
// still fits the envelope ("most capable"), and that footprint; nil when none.
func bestCoder(c catalog.Catalog, envelope, headroom uint64) (*catalog.Model, uint64) {
	var best *catalog.Model
	var bestTotal uint64
	for i := range c.Models {
		m := c.Models[i]
		total, ok := coderFootprint(m, envelope, headroom)
		if !ok {
			continue
		}
		if best == nil || total > bestTotal {
			best, bestTotal = &m, total
		}
	}
	return best, bestTotal
}

// coderFootprint returns m's coder fit total and whether m is a candidate at all:
// eligible AND within the envelope (the OOM guard: swap requires a PROVEN
// standalone fit).
func coderFootprint(m catalog.Model, envelope, headroom uint64) (uint64, bool) {
	if !coderEligible(m, envelope) {
		return 0, false
	}
	total := coderTotal(m, headroom)
	return total, total <= envelope
}

// coderEligible is pickBest's eligibility, inverted on the role: only a
// role:"coder" entry, never the bootstrap entry, never a unified-memory-unsafe
// one, and not below its declared MinEnvelopeBytes floor.
func coderEligible(m catalog.Model, envelope uint64) bool {
	if m.Role != "coder" || m.Bootstrap || !m.UnifiedMemorySafe {
		return false
	}
	return meetsEnvelopeFloor(m, envelope)
}

// coderTotal is the coder fit inequality's left side: weights + KV at the agent
// context + headroom + the prompt cache the coding-mode llama-server may hold
// (ADR-0021), summed saturating so an overflowing term can never wrap to "fits".
func coderTotal(m catalog.Model, headroom uint64) uint64 {
	total := addSaturating(m.WeightBytes, kvCacheBytes(m, m.AgentCtx))
	return addSaturating(addSaturating(total, headroom), inference.PromptCacheBytes)
}
