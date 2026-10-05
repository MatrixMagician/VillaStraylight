package recommend

import "testing"

// TestPickCoderCountsThePromptCache guards ADR-0021: the coder is served by
// villa-llama in coding mode, which renders --cache-ram, so a coder that fits
// only without the 8 GiB cache must not be claimed as a proven swap fit.
func TestPickCoderCountsThePromptCache(t *testing.T) {
	const env = uint64(64 << 30)
	entry := coderFitEntry()
	// Size the weights so weights + KV@agent_ctx + headroom == envelope exactly:
	// it fits without the cache and is over by exactly the cache with it.
	entry.WeightBytes = env - kvCacheBytes(entry, entry.AgentCtx) - headroomBytes(env)
	cat := testCatalog()
	cat.Models = append(cat.Models, entry)

	rec := Pick(profileWithEnvelope(env), cat, Overrides{}, MemoryInputs{}, WebSearchInputs{})

	if rec.Coder != sharedCoderFit() {
		t.Errorf("Coder = %+v, want sharedCoderFit() (fits only without the prompt cache)", rec.Coder)
	}
	if rec.Coder.Fits || rec.Coder.Residency != "shared" || rec.Coder.Model != "" {
		t.Errorf("Coder = %+v, want Fits false, Residency \"shared\", empty Model", rec.Coder)
	}
}

// TestPickCoderCarriesThePromptCacheTerm guards ADR-0021: a coder that fits
// reports the cache as its own term and TotalBytes includes it.
func TestPickCoderCarriesThePromptCacheTerm(t *testing.T) {
	const env = uint64(64 << 30)
	cat := testCatalog()
	cat.Models = append(cat.Models, coderFitEntry())

	rec := Pick(profileWithEnvelope(env), cat, Overrides{}, MemoryInputs{}, WebSearchInputs{})

	if rec.Coder.PromptCacheBytes != 8589934592 {
		t.Errorf("Coder.PromptCacheBytes = %d, want 8589934592 (8 GiB)", rec.Coder.PromptCacheBytes)
	}
	want := rec.Coder.WeightBytes + rec.Coder.KVCacheBytes + rec.Coder.HeadroomBytes + rec.Coder.PromptCacheBytes
	if rec.Coder.TotalBytes != want {
		t.Errorf("Coder.TotalBytes = %d, want weights + KV + headroom + prompt cache = %d", rec.Coder.TotalBytes, want)
	}
}
