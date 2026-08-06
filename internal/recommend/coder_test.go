package recommend

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// coderFitEntry is a role:"coder" catalog entry that comfortably fits a 64 GiB
// envelope at its agent ctx: 18 GiB weights + ~6 GiB KV@65536 + ~7.68 GiB
// headroom ≈ 31.7 GiB.
func coderFitEntry() catalog.CatalogModel {
	return catalog.CatalogModel{
		ID: "coder-fit", Quant: "UD-Q4_K_XL", WeightBytes: 18 << 30,
		NLayers: 48, NKVHeads: 4, HeadDim: 128, KVBytesPerElem: 2,
		DefaultCtx: 32768, TierGB: 64, UnifiedMemorySafe: true, BackendDefault: "rocm",
		Role: "coder", AgentCtx: 65536,
	}
}

// coderBigEntry is a role:"coder" entry whose CHAT-path footprint (weights +
// KV@DefaultCtx + headroom ≈ 56.4 GiB at a 64 GiB envelope) would make it the
// LARGEST fitting pick if pickBest considered it — the bit-identical test
// proves it never does.
func coderBigEntry() catalog.CatalogModel {
	return catalog.CatalogModel{
		ID: "coder-big", Quant: "UD-Q4_K_XL", WeightBytes: 48 << 30,
		NLayers: 48, NKVHeads: 4, HeadDim: 128, KVBytesPerElem: 2,
		DefaultCtx: 8192, TierGB: 64, UnifiedMemorySafe: true, BackendDefault: "rocm",
		Role: "coder", AgentCtx: 65536,
	}
}

// testCatalogWithCoder is testCatalog plus coder entries — the comparison
// fixture: the chat pick over this catalog must be bit-identical to the chat
// pick over testCatalog().
func testCatalogWithCoder() catalog.Catalog {
	c := testCatalog()
	c.Models = append(c.Models, coderFitEntry(), coderBigEntry())
	return c
}

// TestPickCoderSwapWhenFits: with a coder entry whose
// weights+KV@agent_ctx+headroom fit the envelope, the coder block reports the
// exact saturating fit terms, Fits=true and Residency="swap".
func TestPickCoderSwapWhenFits(t *testing.T) {
	const env = uint64(64 << 30)
	cat := testCatalog()
	entry := coderFitEntry()
	cat.Models = append(cat.Models, entry)

	rec := Pick(profileWithEnvelope(env), cat, Overrides{}, MemoryInputs{}, WebSearchInputs{})

	if rec.Coder.Model != entry.ID {
		t.Fatalf("Coder.Model = %q, want %q", rec.Coder.Model, entry.ID)
	}
	if rec.Coder.Quant != entry.Quant {
		t.Errorf("Coder.Quant = %q, want %q", rec.Coder.Quant, entry.Quant)
	}
	if rec.Coder.AgentCtx != entry.AgentCtx {
		t.Errorf("Coder.AgentCtx = %d, want %d", rec.Coder.AgentCtx, entry.AgentCtx)
	}
	if !rec.Coder.Fits {
		t.Errorf("Coder.Fits = false, want true")
	}
	if rec.Coder.Residency != "swap" {
		t.Errorf("Coder.Residency = %q, want \"swap\" (fits standalone → swap)", rec.Coder.Residency)
	}
	// Exact fit terms: KV computed by kvCacheBytes at AgentCtx, headroom from the
	// envelope, total via the saturating sum — no second formula.
	wantKV := kvCacheBytes(entry, entry.AgentCtx)
	wantHeadroom := headroomBytes(env)
	wantTotal := addSaturating(addSaturating(entry.WeightBytes, wantKV), wantHeadroom)
	if rec.Coder.WeightBytes != entry.WeightBytes {
		t.Errorf("Coder.WeightBytes = %d, want %d", rec.Coder.WeightBytes, entry.WeightBytes)
	}
	if rec.Coder.KVCacheBytes != wantKV {
		t.Errorf("Coder.KVCacheBytes = %d, want kvCacheBytes@agent_ctx %d", rec.Coder.KVCacheBytes, wantKV)
	}
	if rec.Coder.HeadroomBytes != wantHeadroom {
		t.Errorf("Coder.HeadroomBytes = %d, want %d", rec.Coder.HeadroomBytes, wantHeadroom)
	}
	if rec.Coder.TotalBytes != wantTotal {
		t.Errorf("Coder.TotalBytes = %d, want %d", rec.Coder.TotalBytes, wantTotal)
	}
}

// TestPickCoderSharedWhenNoneFits: with only an oversized coder entry the
// block degrades honestly — Fits=false, Residency="shared", empty Model — and
// the chat pick is unaffected (shared is the floor, never a refusal).
func TestPickCoderSharedWhenNoneFits(t *testing.T) {
	const env = uint64(64 << 30)
	cat := testCatalog()
	oversized := coderFitEntry()
	oversized.ID = "coder-oversized"
	oversized.WeightBytes = 200 << 30
	cat.Models = append(cat.Models, oversized)

	rec := Pick(profileWithEnvelope(env), cat, Overrides{}, MemoryInputs{}, WebSearchInputs{})

	if rec.Coder.Fits {
		t.Errorf("Coder.Fits = true, want false for an oversized-only coder catalog")
	}
	if rec.Coder.Residency != "shared" {
		t.Errorf("Coder.Residency = %q, want \"shared\" (D-06 conservative floor)", rec.Coder.Residency)
	}
	if rec.Coder.Model != "" {
		t.Errorf("Coder.Model = %q, want empty when no coder entry fits", rec.Coder.Model)
	}
	// The chat pick must be unaffected by the unfittable coder entry.
	baseline := Pick(profileWithEnvelope(env), testCatalog(), Overrides{}, MemoryInputs{}, WebSearchInputs{})
	if rec.Model != baseline.Model || rec.Model == "" {
		t.Errorf("chat pick changed by an unfittable coder entry: %q vs baseline %q", rec.Model, baseline.Model)
	}
}

// TestPickCoderFitAtAgentCtxNeverCtxOverride: a --ctx override changes
// the CHAT KV only — the coder KV is always computed at the entry's AgentCtx.
func TestPickCoderFitAtAgentCtxNeverCtxOverride(t *testing.T) {
	const env = uint64(64 << 30)
	cat := testCatalog()
	entry := coderFitEntry()
	cat.Models = append(cat.Models, entry)

	plain := Pick(profileWithEnvelope(env), cat, Overrides{}, MemoryInputs{}, WebSearchInputs{})
	small := Pick(profileWithEnvelope(env), cat, Overrides{Ctx: 1024}, MemoryInputs{}, WebSearchInputs{})

	if small.ContextLen != 1024 {
		t.Fatalf("chat ContextLen = %d, want the 1024 override applied to the chat pick", small.ContextLen)
	}
	if small.KVCacheBytes == plain.KVCacheBytes {
		t.Errorf("chat KV unchanged by the --ctx override (%d) — precondition broken", small.KVCacheBytes)
	}
	wantKV := kvCacheBytes(entry, entry.AgentCtx)
	if small.Coder.KVCacheBytes != wantKV {
		t.Errorf("Coder.KVCacheBytes = %d with --ctx override, want %d computed at AgentCtx %d", small.Coder.KVCacheBytes, wantKV, entry.AgentCtx)
	}
	if small.Coder.AgentCtx != entry.AgentCtx {
		t.Errorf("Coder.AgentCtx = %d, want %d (override must not leak into the coder fit)", small.Coder.AgentCtx, entry.AgentCtx)
	}
	if small.Coder != plain.Coder {
		t.Errorf("coder block changed by a --ctx override: %+v vs %+v", small.Coder, plain.Coder)
	}
}

// TestPickChatBitIdenticalWithCoderEntries: the chat pick over a catalog
// WITH coder entries is bit-identical to the same catalog WITHOUT them
// pickBest never selects a role:"coder" entry, even when it would be the
// largest fitting footprint (coder-big ≈ 56.4 GiB chat-path total at 64 GiB).
func TestPickChatBitIdenticalWithCoderEntries(t *testing.T) {
	const env = uint64(64 << 30)
	with := Pick(profileWithEnvelope(env), testCatalogWithCoder(), Overrides{}, MemoryInputs{}, WebSearchInputs{})
	without := Pick(profileWithEnvelope(env), testCatalog(), Overrides{}, MemoryInputs{}, WebSearchInputs{})

	if with.Model == "coder-fit" || with.Model == "coder-big" {
		t.Fatalf("pickBest selected a role:\"coder\" entry %q for the chat pick", with.Model)
	}
	if with.Model != without.Model || with.Quant != without.Quant || with.ContextLen != without.ContextLen {
		t.Errorf("chat pick differs with coder entries present: %s/%s/%d vs %s/%s/%d",
			with.Model, with.Quant, with.ContextLen, without.Model, without.Quant, without.ContextLen)
	}
	if with.WeightBytes != without.WeightBytes || with.KVCacheBytes != without.KVCacheBytes ||
		with.HeadroomBytes != without.HeadroomBytes || with.TotalBytes != without.TotalBytes {
		t.Errorf("chat fit terms differ with coder entries present: %d/%d/%d/%d vs %d/%d/%d/%d",
			with.WeightBytes, with.KVCacheBytes, with.HeadroomBytes, with.TotalBytes,
			without.WeightBytes, without.KVCacheBytes, without.HeadroomBytes, without.TotalBytes)
	}
	if with.Fits != without.Fits {
		t.Errorf("chat Fits differs with coder entries present: %v vs %v", with.Fits, without.Fits)
	}
	// Alternatives must not carry coder entries either (they come from the same loop).
	for _, a := range with.Alternatives {
		if a.Model == "coder-fit" || a.Model == "coder-big" {
			t.Errorf("alternatives carry a coder entry %q", a.Model)
		}
	}
}

// TestPickCoderStampedOnRefusal (Pitfall 6): the no-envelope refusal
// path still stamps the coder block — Fits=false, Residency="shared" — and
// SchemaVersion 3. A conditional/omitted block would hide the residency basis.
func TestPickCoderStampedOnRefusal(t *testing.T) {
	p := detect.HostProfile{
		TotalRAMBytes:       detect.UnknownBytes("ram unknown", ""),
		UsableEnvelopeBytes: detect.UnknownBytes("envelope unknown", ""),
	}
	rec := Pick(p, testCatalogWithCoder(), Overrides{}, MemoryInputs{}, WebSearchInputs{})
	if rec.Model != "" {
		t.Fatalf("precondition: expected refusal, got %q", rec.Model)
	}
	if rec.Coder.Fits {
		t.Errorf("refusal Coder.Fits = true, want false (swap requires a PROVEN fit)")
	}
	if rec.Coder.Residency != "shared" {
		t.Errorf("refusal Coder.Residency = %q, want \"shared\" (D-06 conservative floor)", rec.Coder.Residency)
	}
	if rec.SchemaVersion != 4 {
		t.Errorf("refusal SchemaVersion = %d, want 4", rec.SchemaVersion)
	}
}

// TestPickOverrideCoderEntryWarnsAndAllows (Open Question 1, resolved
// warn-and-allow): a --model override naming a coder entry resolves via
// FindByID, succeeds as the CHAT pick, and appends a loud note naming the
// entry id and the word "coder" (the unsafe-override precedent).
func TestPickOverrideCoderEntryWarnsAndAllows(t *testing.T) {
	const env = uint64(64 << 30)
	rec := Pick(profileWithEnvelope(env), testCatalogWithCoder(), Overrides{Model: "coder-fit"}, MemoryInputs{}, WebSearchInputs{})
	if rec.Model != "coder-fit" {
		t.Fatalf("override of coder entry not honored, got %q", rec.Model)
	}
	if !hasNote(rec.Notes, "coder-fit") {
		t.Errorf("expected the warn-and-allow note to name the entry id, got %v", rec.Notes)
	}
	if !hasNote(rec.Notes, "coder") {
		t.Errorf("expected the warn-and-allow note to say \"coder\", got %v", rec.Notes)
	}
}

// TestPickCoderUsesPostReservationEnvelope (ordering): the coder fit is
// evaluated against the POST-reservation envelope — an entry whose footprint
// clears the raw envelope but not the reserved one yields residency "shared".
// Margins: W+K = 30e9; 0.88×raw = 30.24e9 (fits) vs 0.88×(raw−512MiB) =
// 29.76e9 (does not fit).
func TestPickCoderUsesPostReservationEnvelope(t *testing.T) {
	const env = uint64(32 << 30) // raw envelope
	cat := testCatalog()
	tight := catalog.CatalogModel{
		// KV@65536 = 2×24×4×128×65536×2 = 3,221,225,472; weights chosen so
		// W+K = 30,000,000,000 lands between the two 0.88×envelope bounds.
		ID: "coder-tight", Quant: "UD-Q4_K_XL", WeightBytes: 26778774528,
		NLayers: 24, NKVHeads: 4, HeadDim: 128, KVBytesPerElem: 2,
		DefaultCtx: 8192, TierGB: 32, UnifiedMemorySafe: true, BackendDefault: "rocm",
		Role: "coder", AgentCtx: 65536,
	}
	cat.Models = append(cat.Models, tight)

	// Sanity: with memory OFF the tight entry fits the raw envelope → swap.
	off := Pick(profileWithEnvelope(env), cat, Overrides{}, MemoryInputs{}, WebSearchInputs{})
	if off.Coder.Residency != "swap" {
		t.Fatalf("precondition: memory-off residency = %q, want swap (entry must fit the raw envelope)", off.Coder.Residency)
	}

	// With memory ON the 512 MiB pinned reservation shrinks the envelope FIRST
	// and the same entry no longer fits → shared.
	on := Pick(profileWithEnvelope(env), cat, Overrides{}, MemoryInputs{Enabled: true, EmbeddingModel: "nomic-embed-text-v1.5"}, WebSearchInputs{})
	if on.Coder.Residency != "shared" {
		t.Errorf("memory-on residency = %q, want shared (coder fit must see the post-reservation envelope)", on.Coder.Residency)
	}
	if on.Coder.Fits {
		t.Errorf("memory-on Coder.Fits = true, want false against the shrunken envelope")
	}
}

// TestPickCoderEligibilityGuards: pickCoder applies the pickBest eligibility
// filters identically — a unified_memory_safe:false coder entry is never
// selected, and the MinEnvelopeBytes secondary floor is honored.
func TestPickCoderEligibilityGuards(t *testing.T) {
	const env = uint64(64 << 30)

	t.Run("unsafe coder entry never selected", func(t *testing.T) {
		cat := testCatalog()
		unsafe := coderFitEntry()
		unsafe.ID = "coder-unsafe"
		unsafe.UnifiedMemorySafe = false
		cat.Models = append(cat.Models, unsafe)
		rec := Pick(profileWithEnvelope(env), cat, Overrides{}, MemoryInputs{}, WebSearchInputs{})
		if rec.Coder.Model == "coder-unsafe" {
			t.Errorf("pickCoder selected a unified_memory_safe:false entry")
		}
		if rec.Coder.Residency != "shared" {
			t.Errorf("Coder.Residency = %q, want shared when the only coder entry is unsafe", rec.Coder.Residency)
		}
	})

	t.Run("MinEnvelopeBytes floor honored", func(t *testing.T) {
		cat := testCatalog()
		floored := coderFitEntry()
		floored.ID = "coder-floored"
		floored.MinEnvelopeBytes = 128 << 30 // above the 64 GiB envelope
		cat.Models = append(cat.Models, floored)
		rec := Pick(profileWithEnvelope(env), cat, Overrides{}, MemoryInputs{}, WebSearchInputs{})
		if rec.Coder.Model == "coder-floored" {
			t.Errorf("pickCoder selected an entry below its declared MinEnvelopeBytes floor")
		}
		if rec.Coder.Residency != "shared" {
			t.Errorf("Coder.Residency = %q, want shared when the only coder entry is below its floor", rec.Coder.Residency)
		}
	})
}

// TestPickCoderMostCapableWins: with two fitting coder entries, pickCoder
// selects the LARGEST fitting total (the pickBest most-capable rule).
func TestPickCoderMostCapableWins(t *testing.T) {
	const env = uint64(64 << 30)
	rec := Pick(profileWithEnvelope(env), testCatalogWithCoder(), Overrides{}, MemoryInputs{}, WebSearchInputs{})
	// coder-big @ agent ctx: 48 GiB + 6 GiB KV + 7.68 GiB headroom ≈ 61.7 GiB
	// fits and out-foots coder-fit (≈ 31.7 GiB).
	if rec.Coder.Model != "coder-big" {
		t.Errorf("Coder.Model = %q, want the most-capable fitting entry \"coder-big\"", rec.Coder.Model)
	}
	if rec.Coder.Residency != "swap" {
		t.Errorf("Coder.Residency = %q, want swap", rec.Coder.Residency)
	}
}
