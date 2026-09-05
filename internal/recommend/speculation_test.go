package recommend

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// speculationCatalog holds one qualified and one unqualified entry, both fitting a
// generous envelope, so a speculation outcome is never confounded by a fit.
func speculationCatalog() catalog.Catalog {
	return catalog.Catalog{
		SchemaVersion:  catalog.SupportedSchema,
		CatalogVersion: "test",
		Models: []catalog.Model{
			{
				ID: "qualified", Quant: "Q4_K_M", WeightBytes: 4 << 30,
				NLayers: 24, NKVHeads: 4, HeadDim: 128, KVBytesPerElem: 2,
				DefaultCtx: 8192, TierGB: 16, UnifiedMemorySafe: true, BackendDefault: "rocm",
				NgramSafe: true, NgramProvenance: "gfx1151, test probe",
			},
			{
				ID: "unqualified", Quant: "Q4_K_M", WeightBytes: 4 << 30,
				NLayers: 24, NKVHeads: 4, HeadDim: 128, KVBytesPerElem: 2,
				DefaultCtx: 8192, TierGB: 16, UnifiedMemorySafe: true, BackendDefault: "rocm",
			},
		},
	}
}

// TestResolveSpeculation covers the mode algebra directly: what an unset, an
// explicit off, an explicit ngram and an unknown value resolve to against a
// qualified and an unqualified entry.
func TestResolveSpeculation(t *testing.T) {
	cat := speculationCatalog()
	qualified, _ := cat.FindByID("qualified")
	unqualified, _ := cat.FindByID("unqualified")

	cases := []struct {
		name      string
		model     catalog.Model
		requested string
		wantMode  string
		wantOK    bool
		wantNote  string
	}{
		{"unset on a qualified entry", qualified, "", "ngram", true, "qualified for qualified"},
		{"unset on an unqualified entry", unqualified, "", "off", true, "no qualified ngram measurement"},
		{"explicit off is silent", qualified, "off", "off", true, ""},
		{"explicit ngram on a qualified entry", qualified, "ngram", "ngram", true, "qualified for qualified"},
		{"explicit ngram on an unqualified entry", unqualified, "ngram", "off", false, "refusing"},
		{"an unknown mode", qualified, "draft", "off", false, "draft"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, note, ok := ResolveSpeculation(tc.model, tc.requested)
			if mode != tc.wantMode {
				t.Errorf("mode = %q, want %q", mode, tc.wantMode)
			}
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v (note %q)", ok, tc.wantOK, note)
			}
			if tc.wantNote == "" {
				if note != "" {
					t.Errorf("note = %q, want none", note)
				}
				return
			}
			if !strings.Contains(note, tc.wantNote) {
				t.Errorf("note = %q, want it to mention %q", note, tc.wantNote)
			}
		})
	}
}

// TestPickResolvesSpeculation asserts the resolved mode reaches the
// Recommendation, and that an explicit request for a mode the picked entry is not
// qualified for is a refusal rather than a silent downgrade to off.
func TestPickResolvesSpeculation(t *testing.T) {
	cat := speculationCatalog()
	p := profileWithEnvelope(64 << 30)

	cases := []struct {
		name      string
		ov        Overrides
		wantMode  string
		wantFits  bool
		wantNoted string
	}{
		{"unset on a qualified pick", Overrides{Model: "qualified"}, "ngram", true, "speculation: ngram"},
		{"unset on an unqualified pick", Overrides{Model: "unqualified"}, "off", true, "speculation: off"},
		{"explicit ngram on an unqualified pick", Overrides{Model: "unqualified", Speculation: "ngram"}, "off", false, "refusing"},
		{"explicit off on a qualified pick", Overrides{Model: "qualified", Speculation: "off"}, "off", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := Pick(p, cat, tc.ov, MemoryInputs{}, WebSearchInputs{})
			if rec.Speculation != tc.wantMode {
				t.Errorf("Speculation = %q, want %q", rec.Speculation, tc.wantMode)
			}
			if rec.Fits != tc.wantFits {
				t.Errorf("Fits = %v, want %v (notes %v)", rec.Fits, tc.wantFits, rec.Notes)
			}
			if tc.wantNoted != "" && !strings.Contains(strings.Join(rec.Notes, " "), tc.wantNoted) {
				t.Errorf("notes = %v, want one mentioning %q", rec.Notes, tc.wantNoted)
			}
		})
	}
}

// TestRefusalLeavesSpeculationUnset asserts a Pick that names no model reports no
// speculation mode either: there is no entry whose qualification could have
// resolved one.
func TestRefusalLeavesSpeculationUnset(t *testing.T) {
	rec := Pick(profileWithEnvelope(1<<30), speculationCatalog(), Overrides{}, MemoryInputs{}, WebSearchInputs{})
	if rec.Model != "" {
		t.Fatalf("expected a refusal, got a pick of %q", rec.Model)
	}
	if rec.Speculation != "" {
		t.Errorf("Speculation = %q on a refusal, want \"\"", rec.Speculation)
	}
}
