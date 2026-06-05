package recommend

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// testCatalog is a small deterministic catalog exercising every Pick branch:
// a tiny model, a mid model, a large model, an unsafe model, and a bootstrap.
func testCatalog() catalog.Catalog {
	return catalog.Catalog{
		SchemaVersion:  catalog.SupportedSchema,
		CatalogVersion: "test",
		Models: []catalog.CatalogModel{
			{
				ID: "tiny", Quant: "Q4_K_M", WeightBytes: 4 << 30,
				NLayers: 24, NKVHeads: 4, HeadDim: 128, KVBytesPerElem: 2,
				DefaultCtx: 8192, TierGB: 16, UnifiedMemorySafe: true, BackendDefault: "vulkan",
			},
			{
				ID: "mid", Quant: "Q4_K_M", WeightBytes: 40 << 30,
				NLayers: 48, NKVHeads: 8, HeadDim: 128, KVBytesPerElem: 2,
				DefaultCtx: 32768, TierGB: 64, UnifiedMemorySafe: true, BackendDefault: "vulkan",
			},
			{
				ID: "large", Quant: "UD-Q4_K_M", WeightBytes: 90 << 30,
				NLayers: 64, NKVHeads: 8, HeadDim: 128, KVBytesPerElem: 2,
				DefaultCtx: 65536, TierGB: 124, UnifiedMemorySafe: true, BackendDefault: "vulkan",
			},
			{
				ID: "unsafe-but-tiny", Quant: "Q4_K_M", WeightBytes: 2 << 30,
				NLayers: 16, NKVHeads: 4, HeadDim: 128, KVBytesPerElem: 2,
				DefaultCtx: 4096, TierGB: 8, UnifiedMemorySafe: false, BackendDefault: "vulkan",
			},
			{
				ID: "bootstrap", Quant: "Q4_K_M", WeightBytes: 1 << 30,
				NLayers: 24, NKVHeads: 4, HeadDim: 128, KVBytesPerElem: 2,
				DefaultCtx: 8192, TierGB: 0, UnifiedMemorySafe: true, Bootstrap: true, BackendDefault: "vulkan",
			},
		},
	}
}

func profileWithEnvelope(env uint64) detect.HostProfile {
	return detect.HostProfile{
		TotalRAMBytes:       detect.KnownBytes(env+(8<<30), "test"),
		UsableEnvelopeBytes: detect.KnownBytes(env, "test"),
	}
}

// TestPickMultiEnvelopeFitAndOOMGuard asserts that across several envelopes Pick
// selects a model that fits and NEVER one that exceeds the envelope (OOM guard),
// and defaults the backend to vulkan.
func TestPickMultiEnvelopeFitAndOOMGuard(t *testing.T) {
	cat := testCatalog()
	envelopes := []struct {
		name string
		env  uint64
	}{
		{"62.5GiB", 67149381632},
		{"96GiB", 96 << 30},
		{"124GiB", 124 << 30},
	}
	for _, e := range envelopes {
		t.Run(e.name, func(t *testing.T) {
			rec := Pick(profileWithEnvelope(e.env), cat, Overrides{})
			if rec.Model == "" {
				t.Fatalf("env %s: expected a pick, got refusal: %v", e.name, rec.Notes)
			}
			if !rec.Fits {
				t.Errorf("env %s: pick %q marked not-fitting", e.name, rec.Model)
			}
			// OOM GUARD: the selected total must never exceed the envelope.
			if rec.TotalBytes > rec.UsableEnvelopeBytes {
				t.Errorf("OOM GUARD violated: total %d > envelope %d (model %q)", rec.TotalBytes, rec.UsableEnvelopeBytes, rec.Model)
			}
			if rec.Backend != "vulkan" {
				t.Errorf("env %s: backend = %q, want vulkan", e.name, rec.Backend)
			}
			// Fit terms must sum correctly so the command can SHOW the math.
			if rec.WeightBytes+rec.KVCacheBytes+rec.HeadroomBytes != rec.TotalBytes {
				t.Errorf("fit terms do not sum to total (%d+%d+%d != %d)", rec.WeightBytes, rec.KVCacheBytes, rec.HeadroomBytes, rec.TotalBytes)
			}
		})
	}
}

// TestPickHonorsMinEnvelopeFloor asserts the MinEnvelopeBytes secondary floor
// guard (IN-01): a model whose declared minimum envelope exceeds the host's
// envelope is NOT auto-selected, even when the raw weights+KV+headroom math fits.
func TestPickHonorsMinEnvelopeFloor(t *testing.T) {
	cat := catalog.Catalog{
		SchemaVersion:  catalog.SupportedSchema,
		CatalogVersion: "test",
		Models: []catalog.CatalogModel{
			{
				// Raw footprint fits a 20 GiB envelope, but it declares it needs at
				// least 50 GiB to run acceptably — so it must be skipped.
				ID: "needs-big-envelope", Quant: "Q4_K_M", WeightBytes: 4 << 30,
				NLayers: 24, NKVHeads: 4, HeadDim: 128, KVBytesPerElem: 2,
				DefaultCtx: 8192, MinEnvelopeBytes: 50 << 30,
				TierGB: 64, UnifiedMemorySafe: true, BackendDefault: "vulkan",
			},
		},
	}
	rec := Pick(profileWithEnvelope(20<<30), cat, Overrides{})
	if rec.Model == "needs-big-envelope" {
		t.Errorf("Pick auto-selected a model below its declared MinEnvelopeBytes floor")
	}

	// With a host that clears the floor, the same model becomes eligible.
	rec = Pick(profileWithEnvelope(60<<30), cat, Overrides{})
	if rec.Model != "needs-big-envelope" {
		t.Errorf("model clearing its MinEnvelopeBytes floor should be selectable, got %q (%v)", rec.Model, rec.Notes)
	}
}

// TestPickNeverAutoSelectsUnsafe asserts a unified_memory_safe:false entry is
// never auto-selected, even when it is the smallest fitting model.
func TestPickNeverAutoSelectsUnsafe(t *testing.T) {
	// A tiny envelope where only the 2GiB unsafe model and 4GiB tiny could
	// physically fit; the unsafe one must not be chosen.
	rec := Pick(profileWithEnvelope(10<<30), testCatalog(), Overrides{})
	if rec.Model == "unsafe-but-tiny" {
		t.Errorf("Pick auto-selected a unified_memory_safe:false model")
	}
}

// TestPickNeverAutoSelectsBootstrap asserts the bootstrap entry is carried but
// never auto-selected (D-12).
func TestPickNeverAutoSelectsBootstrap(t *testing.T) {
	rec := Pick(profileWithEnvelope(200<<30), testCatalog(), Overrides{})
	if rec.Model == "bootstrap" {
		t.Errorf("Pick auto-selected the bootstrap entry")
	}
}

// TestOverrideUnsafeAllowedWithWarning asserts a --model override of an unsafe
// entry is allowed but adds a loud warning Note (D-07).
func TestOverrideUnsafeAllowedWithWarning(t *testing.T) {
	rec := Pick(profileWithEnvelope(64<<30), testCatalog(), Overrides{Model: "unsafe-but-tiny"})
	if rec.Model != "unsafe-but-tiny" {
		t.Fatalf("override of unsafe model not honored, got %q", rec.Model)
	}
	if !hasNote(rec.Notes, "unified_memory_safe:false") {
		t.Errorf("expected a loud unsafe-override warning, got %v", rec.Notes)
	}
}

// TestOverrideHugeCtxRevalidatedAndFails asserts an override that breaks the fit
// sets Fits=false with a warning Note (D-07).
func TestOverrideHugeCtxRevalidatedAndFails(t *testing.T) {
	rec := Pick(profileWithEnvelope(64<<30), testCatalog(), Overrides{Model: "large", Ctx: 100_000_000})
	if rec.Model != "large" {
		t.Fatalf("override model not honored, got %q", rec.Model)
	}
	if rec.Fits {
		t.Errorf("expected Fits=false for an over-large ctx override")
	}
	if !hasNote(rec.Notes, "does NOT fit") {
		t.Errorf("expected an override-doesnt-fit warning, got %v", rec.Notes)
	}
	if rec.TotalBytes <= rec.UsableEnvelopeBytes {
		t.Errorf("expected total %d to exceed envelope %d for the failing override", rec.TotalBytes, rec.UsableEnvelopeBytes)
	}
}

// TestDegradedFloorWhenEnvelopeUnknown asserts a degraded recommendation with a
// prominent Note when the envelope is Unknown but RAM is known (D-14).
func TestDegradedFloorWhenEnvelopeUnknown(t *testing.T) {
	p := detect.HostProfile{
		TotalRAMBytes:       detect.KnownBytes(128<<30, "ghw"),
		UsableEnvelopeBytes: detect.UnknownBytes("envelope unreadable", ""),
	}
	rec := Pick(p, testCatalog(), Overrides{})
	if !rec.Degraded {
		t.Errorf("expected Degraded=true on Unknown envelope")
	}
	if !hasNote(rec.Notes, "DEGRADED ESTIMATE") {
		t.Errorf("expected a prominent degraded note, got %v", rec.Notes)
	}
	if rec.Model == "" {
		t.Errorf("expected a (degraded) pick from a derivable floor, got refusal")
	}
	if rec.TotalBytes > rec.UsableEnvelopeBytes {
		t.Errorf("degraded pick still violated the OOM guard")
	}
}

// TestRefusalWhenNoFloor asserts Pick refuses (empty Model + Note) when neither
// envelope nor RAM is known — never guesses high (D-14).
func TestRefusalWhenNoFloor(t *testing.T) {
	p := detect.HostProfile{
		TotalRAMBytes:       detect.UnknownBytes("ram unknown", ""),
		UsableEnvelopeBytes: detect.UnknownBytes("envelope unknown", ""),
	}
	rec := Pick(p, testCatalog(), Overrides{})
	if rec.Model != "" {
		t.Errorf("expected refusal (empty Model), got %q", rec.Model)
	}
	if !hasNote(rec.Notes, "refusing to recommend") {
		t.Errorf("expected a refusal note, got %v", rec.Notes)
	}
}

func hasNote(notes []string, substr string) bool {
	for _, n := range notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}
