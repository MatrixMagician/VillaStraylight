package main

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// TestRecommendTableShowsDraftTerms asserts a reserved draft sidecar renders its
// own two fit rows, "draft weight" and "draft KV @ ctx N", beside the projector
// row — its memory cost is the user's to see, exactly as the projector's is.
func TestRecommendTableShowsDraftTerms(t *testing.T) {
	out := renderFixtureTable(t, func(r *recommend.Recommendation) {
		r.DraftBytes = 998996378
		r.DraftKVBytes = 25165824
		r.Speculation = "draft"
		r.TotalBytes += r.DraftBytes + r.DraftKVBytes
	})
	if !strings.Contains(out, "draft weight") {
		t.Errorf("table should carry the draft weight fit row, got:\n%s", out)
	}
	if !strings.Contains(out, "draft KV @ ctx 131072") {
		t.Errorf("table should carry the draft KV fit row at the served ctx, got:\n%s", out)
	}
}

// TestRecommendTableOmitsDraftForNonDraftPick asserts a pick with no draft (the
// common case) renders exactly as it did before this field existed.
func TestRecommendTableOmitsDraftForNonDraftPick(t *testing.T) {
	out := renderFixtureTable(t, func(*recommend.Recommendation) {})
	if strings.Contains(out, "draft weight") || strings.Contains(out, "draft KV") {
		t.Errorf("a non-draft pick should render no draft rows, got:\n%s", out)
	}
}
