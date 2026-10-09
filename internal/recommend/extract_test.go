package recommend

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestExtractorRowFollowsItsGate: the registry holds an extractor row only when
// the derived gate is on, after the reranker and before web search, and an
// extractor flag without memory reserves nothing (ADR-0033).
func TestExtractorRowFollowsItsGate(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.VillaConfig
		want []string
	}{
		{"memory on, extractor on", config.VillaConfig{MemoryEnabled: true, Extractor: true}, []string{"embedding", "extractor"}},
		{"memory on, extractor off", config.VillaConfig{MemoryEnabled: true}, []string{"embedding"}},
		{"extractor without memory", config.VillaConfig{Extractor: true}, nil},
		{"everything on", config.VillaConfig{MemoryEnabled: true, Reranker: true, Extractor: true, WebSearchEnabled: true}, []string{"embedding", "reranker", "extractor", "web_search"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, r := range ReservationsFor(tc.cfg) {
				got = append(got, r.Name)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("rows = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestExtractorRowCarriesTheMeasuredSize: the extractor row is sized by the pinned
// bound in internal/memory, and Pick subtracts it from the envelope beside the
// embedding and reranker rows.
func TestExtractorRowCarriesTheMeasuredSize(t *testing.T) {
	cfg := config.VillaConfig{MemoryEnabled: true, EmbeddingModel: "nomic-embed-text-v1.5", Reranker: true, Extractor: true}
	res := ReservationsFor(cfg)
	if len(res) != 3 || res[2].Name != "extractor" || res[2].Bytes != 2147483648 {
		t.Fatalf("rows = %+v, want a 2147483648-byte extractor row third", res)
	}

	const env = uint64(64 << 30)
	cat := testCatalog()
	base := Pick(profileWithEnvelope(env), cat, Overrides{}, nil)
	rec := Pick(profileWithEnvelope(env), cat, Overrides{}, res)
	if want := base.UsableEnvelopeBytes - 536870912 - 2147483648 - 2147483648; rec.UsableEnvelopeBytes != want {
		t.Errorf("UsableEnvelopeBytes = %d, want %d (embedding, reranker and extractor subtracted)", rec.UsableEnvelopeBytes, want)
	}
	if rec.ReservedBytes() != 536870912+2147483648+2147483648 {
		t.Errorf("ReservedBytes = %d, want the sum of the three memory rows", rec.ReservedBytes())
	}
}
