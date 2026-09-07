package recommend

import (
	"math"
	"math/bits"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// kvCacheBytes computes the KV-cache size in bytes for a model at a given context
// length.
//
// Formula (llama.cpp KV layout): 2 (K+V) × n_layers × n_kv_heads × head_dim × ctx
// × kv_bytes_per_elem.
// Source: github.com/ggml-org/llama.cpp/blob/master/src/llama-kv-cache.cpp (CITED).
//
// It uses n_kv_heads, NOT n_heads: under grouped-query attention (GQA) the KV
// cache is sized by the (smaller) number of KV heads, so using n_heads would
// massively over-estimate KV memory (Pitfall 4).
//
// The product is computed via bits.Mul64 and SATURATES to math.MaxUint64 on
// overflow (phase-22): an absurd --ctx override (≈9.4e13+ for typical
// catalog dimensions) would otherwise wrap mod 2^64 to a SMALL total and defeat
// the fit re-validation — exactly the silent-OOM guard this math exists to
// provide. A saturated KV can never compare ≤ envelope, so Fits stays false.
func kvCacheBytes(m catalog.Model, ctx int) uint64 {
	return kvBytesForDims(m.NLayers, m.NKVHeads, m.HeadDim, ctx, m.KVBytesPerElem)
}

// draftKVCacheBytes computes the KV-cache size for a draft sidecar at ctx, using
// the DRAFT's own dimensions (an MTP head is typically NLayers:1) — never the
// target model's. Same math, same saturation discipline as kvCacheBytes.
func draftKVCacheBytes(d catalog.Draft, ctx int) uint64 {
	return kvBytesForDims(d.NLayers, d.NKVHeads, d.HeadDim, ctx, d.KVBytesPerElem)
}

// kvBytesForDims is the shared KV-layout math behind kvCacheBytes and
// draftKVCacheBytes: 2 (K+V) × n_layers × n_kv_heads × head_dim × ctx ×
// kv_bytes_per_elem, saturating to math.MaxUint64 on overflow.
func kvBytesForDims(nLayers, nKVHeads, headDim, ctx, kvBytesPerElem int) uint64 {
	if ctx <= 0 {
		return 0
	}
	total := uint64(2)
	for _, factor := range []uint64{
		uint64(nLayers),
		uint64(nKVHeads),
		uint64(headDim),
		uint64(ctx),
		uint64(kvBytesPerElem),
	} {
		hi, lo := bits.Mul64(total, factor)
		if hi != 0 {
			return math.MaxUint64 // saturate: overflow must never wrap to "fits"
		}
		total = lo
	}
	return total
}

// addSaturating sums two byte counts, saturating to math.MaxUint64 on carry
// the addition twin of kvCacheBytes' saturating product: once any fit
// term has saturated, the TOTAL must stay saturated (never wrap small) so the
// total ≤ envelope verdict remains honestly false.
func addSaturating(a, b uint64) uint64 {
	sum, carry := bits.Add64(a, b, 0)
	if carry != 0 {
		return math.MaxUint64
	}
	return sum
}
