package recommend

import "github.com/MatrixMagician/VillaStraylight/internal/catalog"

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
func kvCacheBytes(m catalog.CatalogModel, ctx int) uint64 {
	if ctx <= 0 {
		return 0
	}
	return uint64(2) *
		uint64(m.NLayers) *
		uint64(m.NKVHeads) *
		uint64(m.HeadDim) *
		uint64(ctx) *
		uint64(m.KVBytesPerElem)
}
