package recommend

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// draftSidecar builds a Draft sidecar of the given weight, at fixed KV
// dimensions matching the ADR-0009 qwen3.8-27b provenance (an MTP head:
// NLayers 1).
func draftSidecar(id string, weightBytes uint64) *catalog.Draft {
	return &catalog.Draft{
		Sidecar: catalog.Sidecar{
			Shards:      []catalog.Shard{{Filename: id + "-draft.gguf"}},
			WeightBytes: weightBytes,
			Provenance:  "gfx1151, test",
		},
		SpecType: "draft-mtp",
		NMax:     3,
		NLayers:  1, NKVHeads: 4, HeadDim: 256, KVBytesPerElem: 2,
	}
}

// draftPickCatalog holds four entries that differ only in their draft sidecar
// and ngram qualification, so a Pick outcome is never confounded by a fit that
// isn't the one under test: one whose draft fits, one whose draft is too big but
// is ngram-qualified, one whose draft is too big with no ngram fallback, and one
// with a projector AND a draft where the envelope has room for only one.
func draftPickCatalog() catalog.Catalog {
	base := catalog.Model{
		Quant: "Q4_K_M", WeightBytes: 4 << 30,
		NLayers: 24, NKVHeads: 4, HeadDim: 128, KVBytesPerElem: 2,
		DefaultCtx: 8192, TierGB: 16, UnifiedMemorySafe: true, BackendDefault: "rocm",
	}
	fits, tooBigWithNgram, tooBigNoFallback, projectorFirst := base, base, base, base

	fits.ID = "draft-fits"
	fits.Draft = draftSidecar("draft-fits", 1<<30)

	tooBigWithNgram.ID = "draft-too-big-ngram-safe"
	tooBigWithNgram.NgramSafe = true
	tooBigWithNgram.NgramProvenance = "gfx1151, test"
	tooBigWithNgram.Draft = draftSidecar("draft-too-big-ngram-safe", 60<<30)

	tooBigNoFallback.ID = "draft-too-big-no-fallback"
	tooBigNoFallback.Draft = draftSidecar("draft-too-big-no-fallback", 60<<30)

	projectorFirst.ID = "projector-and-draft"
	projectorFirst.Projector = &catalog.Sidecar{
		Shards:      []catalog.Shard{{Filename: "projector-and-draft-mmproj.gguf"}},
		WeightBytes: 8 << 30,
		Provenance:  "gfx1151, test",
	}
	projectorFirst.Draft = draftSidecar("projector-and-draft", 8<<30)

	return catalog.Catalog{
		SchemaVersion:  catalog.SupportedSchema,
		CatalogVersion: "test",
		Models:         []catalog.Model{fits, tooBigWithNgram, tooBigNoFallback, projectorFirst},
	}
}

// TestPickReservesDraft asserts the draft sidecar is a term of the fit exactly
// like the projector: reserved (weight AND KV, both from the draft's own
// dimensions) when it fits on top of the base total, and dropped with a note
// naming the fallback the ladder actually resolved to when it does not.
func TestPickReservesDraft(t *testing.T) {
	cat := draftPickCatalog()
	p := profileWithEnvelope(64 << 30)

	cases := []struct {
		name         string
		model        string
		wantMode     string
		wantDraft    bool
		wantDropNote string
	}{
		{"a draft that fits is reserved", "draft-fits", "draft", true, ""},
		{"a draft that does not fit falls back to ngram", "draft-too-big-ngram-safe", "ngram", false, "falling back to ngram"},
		{"a draft that does not fit and has no ngram falls back to off", "draft-too-big-no-fallback", "off", false, "falling back to off"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := Pick(p, cat, Overrides{Model: tc.model}, MemoryInputs{}, WebSearchInputs{})
			if rec.Speculation != tc.wantMode {
				t.Errorf("Speculation = %q, want %q (notes %v)", rec.Speculation, tc.wantMode, rec.Notes)
			}
			if !rec.Fits {
				t.Errorf("Fits = false; every case here fits the envelope (notes %v)", rec.Notes)
			}
			if tc.wantDraft {
				if rec.DraftBytes == 0 || rec.DraftKVBytes == 0 {
					t.Errorf("DraftBytes=%d DraftKVBytes=%d, want both > 0", rec.DraftBytes, rec.DraftKVBytes)
				}
			} else if rec.DraftBytes != 0 || rec.DraftKVBytes != 0 {
				t.Errorf("DraftBytes=%d DraftKVBytes=%d, want both 0 (dropped)", rec.DraftBytes, rec.DraftKVBytes)
			}

			want := rec.WeightBytes + rec.KVCacheBytes + rec.HeadroomBytes + rec.ProjectorBytes + rec.DraftBytes + rec.DraftKVBytes
			if rec.TotalBytes != want {
				t.Errorf("TotalBytes = %d, want %d (the fit terms plus the draft)", rec.TotalBytes, want)
			}

			notes := strings.Join(rec.Notes, " ")
			if tc.wantDropNote == "" {
				if strings.Contains(notes, "dropped") {
					t.Errorf("notes = %v, want no draft-drop note", rec.Notes)
				}
				return
			}
			if !strings.Contains(notes, "speculation: draft (") || !strings.Contains(notes, "dropped") || !strings.Contains(notes, tc.wantDropNote) {
				t.Errorf("notes = %v, want a drop note mentioning %q", rec.Notes, tc.wantDropNote)
			}
		})
	}
}

// TestPickReservesProjectorBeforeDraft asserts the fit order the ADR fixes: when
// the envelope has room for only one sidecar, the projector wins and the draft is
// the one dropped, never the other way round.
func TestPickReservesProjectorBeforeDraft(t *testing.T) {
	cat := draftPickCatalog()
	// Room for weight+KV+headroom+projector, but not also the draft's weight+KV.
	m, _ := cat.FindByID("projector-and-draft")
	rec := Pick(profileWithEnvelope(m.WeightBytes+kvCacheBytes(m, m.DefaultCtx)+headroomBytes(64<<30)+m.Projector.WeightBytes+1<<20),
		cat, Overrides{Model: "projector-and-draft"}, MemoryInputs{}, WebSearchInputs{})

	if !rec.Vision || rec.ProjectorBytes == 0 {
		t.Fatalf("expected the projector to be reserved, got Vision=%v ProjectorBytes=%d (notes %v)", rec.Vision, rec.ProjectorBytes, rec.Notes)
	}
	if rec.DraftBytes != 0 || rec.DraftKVBytes != 0 {
		t.Errorf("expected the draft to be dropped in favor of the projector, got DraftBytes=%d DraftKVBytes=%d", rec.DraftBytes, rec.DraftKVBytes)
	}
	if !strings.Contains(strings.Join(rec.Notes, " "), "speculation: draft (") {
		t.Errorf("expected a draft-drop note, got %v", rec.Notes)
	}
}

// TestPickExplicitDraftRefusesWhenNotQualified asserts an explicit --speculation
// draft that the picked entry cannot honour (no draft, or a draft that does not
// fit) is a REFUSAL rather than a silent downgrade, mirroring ngram's contract —
// and that the refusal note stands ALONE: pickOverride's "your override does NOT
// fit" OOM warning is keyed on memory (commit "recommend: key the override OOM
// warning on memory, not on Fits", on this branch), so a speculation refusal that
// has nothing to do with memory must not also carry the OOM note.
func TestPickExplicitDraftRefusesWhenNotQualified(t *testing.T) {
	cat := draftPickCatalog()
	p := profileWithEnvelope(64 << 30)

	cases := []struct {
		name  string
		model string
	}{
		{"explicit draft that does not fit", "draft-too-big-no-fallback"},
		{"explicit draft on an entry without one", "draft-fits"}, // overridden below to strip Draft
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := cat
			if tc.model == "draft-fits" {
				// Reuse the catalog but target an entry with no draft at all —
				// filtered in place by cloning the model without its Draft.
				m, _ := c.FindByID("draft-fits")
				m.Draft = nil
				c.Models = append([]catalog.Model{m}, c.Models[1:]...)
			}
			rec := Pick(p, c, Overrides{Model: tc.model, Speculation: "draft"}, MemoryInputs{}, WebSearchInputs{})
			if rec.Fits {
				t.Fatalf("Fits = true, want false (a refused speculation request must not fit)")
			}
			if !strings.Contains(strings.Join(rec.Notes, " "), "refusing") {
				t.Errorf("notes = %v, want a refusal note", rec.Notes)
			}
			if strings.Contains(strings.Join(rec.Notes, " "), "does NOT fit") {
				t.Errorf("notes = %v, a speculation refusal must not also carry the memory OOM note", rec.Notes)
			}
		})
	}
}

// TestPickReservesDraftOnlyWhenDraftIsTheMode asserts the draft's weight and KV
// are reserved only for a pick that will actually render the draft. An explicit
// ngram or off honours the request, so the terms must be zero and the total must
// exclude them; a reservation for a sidecar the unit never loads would refuse
// contexts the host can serve.
func TestPickReservesDraftOnlyWhenDraftIsTheMode(t *testing.T) {
	cat := draftPickCatalog()
	p := profileWithEnvelope(64 << 30)
	for _, requested := range []string{"ngram", "off"} {
		t.Run(requested, func(t *testing.T) {
			m, _ := cat.FindByID("draft-fits")
			m.NgramSafe, m.NgramProvenance = true, "gfx1151, test"
			cat.Models[0] = m
			rec := Pick(p, cat, Overrides{Model: "draft-fits", Speculation: requested}, MemoryInputs{}, WebSearchInputs{})
			if rec.Speculation != requested {
				t.Fatalf("Speculation = %q, want %q (notes %v)", rec.Speculation, requested, rec.Notes)
			}
			if rec.DraftBytes != 0 || rec.DraftKVBytes != 0 {
				t.Errorf("DraftBytes=%d DraftKVBytes=%d, want both 0 when the mode is %s", rec.DraftBytes, rec.DraftKVBytes, requested)
			}
			if want := rec.WeightBytes + rec.KVCacheBytes + rec.HeadroomBytes + rec.ProjectorBytes; rec.TotalBytes != want {
				t.Errorf("TotalBytes = %d, want %d (no draft terms)", rec.TotalBytes, want)
			}
			if strings.Contains(strings.Join(rec.Notes, " "), "dropped") {
				t.Errorf("notes = %v, want no drop note for an honoured %s", rec.Notes, requested)
			}
		})
	}
}
