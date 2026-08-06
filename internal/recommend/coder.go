package recommend

import "github.com/MatrixMagician/VillaStraylight/internal/catalog"

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
// HeadroomBytes = TotalBytes ≤ post-reservation envelope) plus the derived
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
}

// sharedCoderFit is the conservative-floor coder block: no proven fit, so
// residency degrades to "shared". It is also what the no-envelope
// refusal path stamps (swap requires a proven fit).
func sharedCoderFit() CoderFit {
	return CoderFit{Fits: false, Residency: ResidencyShared}
}

// pickCoder evaluates every role:"coder" catalog entry against the
// post-reservation envelope and returns the coder block. It mirrors pickBest's
// eligibility loop (Bootstrap/UnifiedMemorySafe/MinEnvelopeBytes guards and
// the most-capable-wins rule) with two deltas: only role:"coder" entries are
// considered, and ctx is ALWAYS m.AgentCtx. The total uses the
// saturating form so an overflowing KV term can never wrap to "fits".
func pickCoder(c catalog.Catalog, envelope uint64) CoderFit {
	headroom := headroomBytes(envelope)

	var best *catalog.CatalogModel
	var bestTotal uint64

	for i := range c.Models {
		m := c.Models[i]
		if m.Role != "coder" {
			continue // coder stage considers only role:"coder" entries
		}
		if m.Bootstrap {
			continue // never auto-select the bootstrap entry
		}
		if !m.UnifiedMemorySafe {
			continue // never auto-select a unified-memory-unsafe entry
		}
		if m.MinEnvelopeBytes > 0 && envelope < m.MinEnvelopeBytes {
			continue // secondary floor guard, applied identically to pickBest
		}
		total := addSaturating(addSaturating(m.WeightBytes, kvCacheBytes(m, m.AgentCtx)), headroom)
		if total > envelope {
			continue // OOM guard: swap requires a PROVEN standalone fit
		}
		// "Best" = the largest footprint that still fits (most capable).
		if best == nil || total > bestTotal {
			bm := m
			best = &bm
			bestTotal = total
		}
	}

	if best == nil {
		// Honest no-fit mirror of pickBest's note path: shared is the floor,
		// never a refusal.
		return sharedCoderFit()
	}

	return CoderFit{
		Model:         best.ID,
		Quant:         best.Quant,
		AgentCtx:      best.AgentCtx,
		WeightBytes:   best.WeightBytes,
		KVCacheBytes:  kvCacheBytes(*best, best.AgentCtx),
		HeadroomBytes: headroom,
		TotalBytes:    bestTotal,
		Fits:          true,
		Residency:     ResidencySwap,
	}
}
