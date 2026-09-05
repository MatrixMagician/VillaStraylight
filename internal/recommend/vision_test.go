package recommend

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// visionCatalog holds three entries that differ only in their projector: one
// small enough to fit beside the model, one far too large to, and none at all.
func visionCatalog() catalog.Catalog {
	base := catalog.Model{
		Quant: "Q4_K_M", WeightBytes: 4 << 30,
		NLayers: 24, NKVHeads: 4, HeadDim: 128, KVBytesPerElem: 2,
		DefaultCtx: 8192, TierGB: 16, UnifiedMemorySafe: true, BackendDefault: "rocm",
	}
	fitting, huge, textOnly := base, base, base
	fitting.ID = "vision-fits"
	fitting.Projector = &catalog.Sidecar{
		Shards:      []catalog.Shard{{Filename: "vision-fits-mmproj.gguf"}},
		WeightBytes: 1 << 30,
		Provenance:  "gfx1151, test",
	}
	huge.ID = "vision-too-big"
	huge.Projector = &catalog.Sidecar{
		Shards:      []catalog.Shard{{Filename: "vision-too-big-mmproj.gguf"}},
		WeightBytes: 60 << 30,
		Provenance:  "gfx1151, test",
	}
	textOnly.ID = "text-only"
	return catalog.Catalog{
		SchemaVersion:  catalog.SupportedSchema,
		CatalogVersion: "test",
		Models:         []catalog.Model{fitting, huge, textOnly},
	}
}

// TestPickReservesProjector asserts the projector is a term of the fit, not a
// footnote: it is added to the total when it fits, dropped with a note naming the
// terms when it does not, and absent entirely for a text-only entry, whose math
// must stay what it was before this field existed.
func TestPickReservesProjector(t *testing.T) {
	cat := visionCatalog()
	p := profileWithEnvelope(64 << 30)

	cases := []struct {
		name       string
		model      string
		wantVision bool
		wantProj   uint64
		wantNote   string
	}{
		{"a projector that fits is reserved", "vision-fits", true, 1 << 30, ""},
		{"a projector that does not fit is dropped with its terms", "vision-too-big", false, 0, "this pick runs text-only"},
		{"a text-only entry says nothing about vision", "text-only", false, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := Pick(p, cat, Overrides{Model: tc.model}, MemoryInputs{}, WebSearchInputs{})
			if rec.Vision != tc.wantVision {
				t.Errorf("Vision = %v, want %v (notes %v)", rec.Vision, tc.wantVision, rec.Notes)
			}
			if rec.ProjectorBytes != tc.wantProj {
				t.Errorf("ProjectorBytes = %d, want %d", rec.ProjectorBytes, tc.wantProj)
			}

			want := rec.WeightBytes + rec.KVCacheBytes + rec.HeadroomBytes + tc.wantProj
			if rec.TotalBytes != want {
				t.Errorf("TotalBytes = %d, want %d (the fit terms plus the projector)", rec.TotalBytes, want)
			}
			if !rec.Fits {
				t.Errorf("Fits = false; every case here fits the envelope (notes %v)", rec.Notes)
			}

			notes := strings.Join(rec.Notes, " ")
			if tc.wantNote == "" {
				if strings.Contains(notes, "vision:") {
					t.Errorf("notes = %v, want no vision note", rec.Notes)
				}
				return
			}
			if !strings.Contains(notes, tc.wantNote) {
				t.Errorf("notes = %v, want one mentioning %q", rec.Notes, tc.wantNote)
			}
			if !strings.Contains(notes, "usable") {
				t.Errorf("the drop note must show the terms, got %v", rec.Notes)
			}
		})
	}
}
