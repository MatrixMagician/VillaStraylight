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
// overflow (phase-22 WR-07): an absurd --ctx override (≈9.4e13+ for typical
// catalog dimensions) would otherwise wrap mod 2^64 to a SMALL total and defeat
// the D-07 fit re-validation — exactly the silent-OOM guard this math exists to
// provide. A saturated KV can never compare ≤ envelope, so Fits stays false.
func kvCacheBytes(m catalog.CatalogModel, ctx int) uint64 {
	if ctx <= 0 {
		return 0
	}
	total := uint64(2)
	for _, factor := range []uint64{
		uint64(m.NLayers),
		uint64(m.NKVHeads),
		uint64(m.HeadDim),
		uint64(ctx),
		uint64(m.KVBytesPerElem),
	} {
		hi, lo := bits.Mul64(total, factor)
		if hi != 0 {
			return math.MaxUint64 // saturate: overflow must never wrap to "fits"
		}
		total = lo
	}
	return total
}

// addSaturating sums two byte counts, saturating to math.MaxUint64 on carry —
// the addition twin of kvCacheBytes' saturating product (WR-07): once any fit
// term has saturated, the TOTAL must stay saturated (never wrap small) so the
// total ≤ envelope verdict remains honestly false.
func addSaturating(a, b uint64) uint64 {
	sum, carry := bits.Add64(a, b, 0)
	if carry != 0 {
		return math.MaxUint64
	}
	return sum
}
