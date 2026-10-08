package recommend

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// promptCacheCatalog holds one model that clears weights + KV + headroom with
// less than the prompt cache to spare, and one that clears all four terms.
func promptCacheCatalog() catalog.Catalog {
	return catalog.Catalog{
		SchemaVersion:  catalog.SupportedSchema,
		CatalogVersion: "test",
		Models: []catalog.Model{
			{
				ID: "tight", Quant: "Q4_K_M", WeightBytes: 80 << 30,
				NLayers: 8, NKVHeads: 2, HeadDim: 128, KVBytesPerElem: 2,
				DefaultCtx: 4096, TierGB: 64, UnifiedMemorySafe: true, BackendDefault: "rocm",
			},
			{
				ID: "roomy", Quant: "Q4_K_M", WeightBytes: 40 << 30,
				NLayers: 8, NKVHeads: 2, HeadDim: 128, KVBytesPerElem: 2,
				DefaultCtx: 4096, TierGB: 64, UnifiedMemorySafe: true, BackendDefault: "rocm",
			},
		},
	}
}

// TestPickCountsThePromptCacheInTheFit guards ADR-0021: on a 100 GiB envelope the
// 80 GiB model clears weights + KV + 12% headroom (about 92 GiB) but not the same
// sum plus the 8 GiB prompt cache, so Pick must skip it for the model that fits
// all of it. Without the term Pick would pick "tight" and the cache would be paid
// out of the headroom reserved for the OS and compute buffers.
func TestPickCountsThePromptCacheInTheFit(t *testing.T) {
	rec := Pick(profileWithEnvelope(100<<30), promptCacheCatalog(), Overrides{}, nil)
	if rec.Model != "roomy" || rec.ContextLen != 4096 {
		t.Fatalf("Pick = %q @ ctx %d, want roomy @ 4096 (tight fits only without the prompt cache): %v", rec.Model, rec.ContextLen, rec.Notes)
	}
	if !rec.Fits {
		t.Errorf("pick %q marked not-fitting", rec.Model)
	}
	for _, a := range rec.Alternatives {
		if a.Model == "tight" {
			t.Errorf("tight listed as a fitting alternative: %+v", a)
		}
	}
}

// TestRecommendationCarriesThePromptCacheTerm guards ADR-0021: the recommendation
// reports the prompt cache as its own fit term, equal to the inference seam's
// constant, and TotalBytes includes it, so the operator can see the sum.
func TestRecommendationCarriesThePromptCacheTerm(t *testing.T) {
	rec := Pick(profileWithEnvelope(124<<30), promptCacheCatalog(), Overrides{}, nil)
	if rec.PromptCacheBytes != inference.PromptCacheBytes {
		t.Errorf("PromptCacheBytes = %d, want %d", rec.PromptCacheBytes, inference.PromptCacheBytes)
	}
	want := rec.WeightBytes + rec.KVCacheBytes + rec.HeadroomBytes + rec.PromptCacheBytes
	if rec.TotalBytes != want {
		t.Errorf("TotalBytes = %d, want weights+KV+headroom+cache = %d", rec.TotalBytes, want)
	}
}

// TestOverrideCountsThePromptCache guards ADR-0021 on the --model path: forcing the
// tight model onto the 100 GiB envelope must report a non-fit, because the
// override re-validates the same fit including the prompt cache.
func TestOverrideCountsThePromptCache(t *testing.T) {
	rec := Pick(profileWithEnvelope(100<<30), promptCacheCatalog(), Overrides{Model: "tight"}, nil)
	if rec.Model != "tight" || rec.Fits {
		t.Fatalf("override tight: model %q fits %v, want tight and Fits false (cache pushes it over the envelope)", rec.Model, rec.Fits)
	}
}
