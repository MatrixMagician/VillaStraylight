package recommend

import (
	"math"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// TestKVCacheBytesFormula asserts the GQA-aware KV formula: it uses n_kv_heads
// (not n_heads), scales linearly with ctx, and matches a hand-computed value.
func TestKVCacheBytesFormula(t *testing.T) {
	m := catalog.CatalogModel{
		NLayers:        48,
		NKVHeads:       8,
		HeadDim:        128,
		KVBytesPerElem: 2,
	}

	// Hand-computed: 2 * 48 * 8 * 128 * 1024 * 2 = 201,326,592.
	const ctx = 1024
	const want uint64 = 2 * 48 * 8 * 128 * ctx * 2
	got := kvCacheBytes(m, ctx)
	if got != want {
		t.Fatalf("kvCacheBytes = %d, want %d", got, want)
	}

	// Doubling ctx doubles KV.
	if d := kvCacheBytes(m, ctx*2); d != got*2 {
		t.Errorf("doubling ctx: got %d, want %d (2x)", d, got*2)
	}

	// Zero/negative ctx → 0 (defensive).
	if z := kvCacheBytes(m, 0); z != 0 {
		t.Errorf("kvCacheBytes(ctx=0) = %d, want 0", z)
	}
}

// TestKVCacheBytesSaturatesOnOverflow (phase-22 WR-07): an absurd ctx whose
// five-term product exceeds 2^64 must SATURATE to MaxUint64, never wrap mod 2^64
// to a small value that would defeat the D-07 fit re-validation (silent OOM).
func TestKVCacheBytesSaturatesOnOverflow(t *testing.T) {
	m := catalog.CatalogModel{NLayers: 48, NKVHeads: 8, HeadDim: 128, KVBytesPerElem: 2}
	// Multiplier 2*48*8*128*2 = 196,608; ctx ≈ 9.4e13 wraps a naive product.
	const absurdCtx = int(1) << 50
	got := kvCacheBytes(m, absurdCtx)
	if got != math.MaxUint64 {
		t.Fatalf("kvCacheBytes(absurd ctx) = %d, want MaxUint64 (saturated, never wrapped)", got)
	}
}

// TestAddSaturating guards the addition twin: a carry saturates, normal sums pass.
func TestAddSaturating(t *testing.T) {
	if got := addSaturating(math.MaxUint64, 1); got != math.MaxUint64 {
		t.Errorf("addSaturating(MaxUint64, 1) = %d, want MaxUint64", got)
	}
	if got := addSaturating(40<<30, 2<<30); got != 42<<30 {
		t.Errorf("addSaturating(40GiB, 2GiB) = %d, want %d", got, uint64(42<<30))
	}
}

// TestKVCacheUsesKVHeadsNotAttnHeads guards Pitfall 4: a model with many
// attention heads but few KV heads must be sized by the KV heads. We can only
// observe n_kv_heads here, so assert the result scales with NKVHeads and would be
// 4x larger if a 32-head count were (wrongly) used.
func TestKVCacheUsesKVHeads(t *testing.T) {
	gqa := catalog.CatalogModel{NLayers: 1, NKVHeads: 8, HeadDim: 128, KVBytesPerElem: 2}
	wrong := catalog.CatalogModel{NLayers: 1, NKVHeads: 32, HeadDim: 128, KVBytesPerElem: 2}
	if kvCacheBytes(wrong, 1024) != 4*kvCacheBytes(gqa, 1024) {
		t.Errorf("KV did not scale with n_kv_heads as expected")
	}
}
