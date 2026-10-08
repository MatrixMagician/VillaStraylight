package recommend

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestRerankerRowFollowsItsGate: the registry holds a reranker row only when the
// derived gate is on, placed beside the embedding row it shares a subsystem with,
// and a reranker flag without memory reserves nothing (ADR-0028).
func TestRerankerRowFollowsItsGate(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.VillaConfig
		want []string
	}{
		{"memory on, reranker on", config.VillaConfig{MemoryEnabled: true, Reranker: true}, []string{"embedding", "reranker"}},
		{"memory on, reranker off", config.VillaConfig{MemoryEnabled: true}, []string{"embedding"}},
		{"reranker without memory", config.VillaConfig{Reranker: true}, nil},
		{"everything on", config.VillaConfig{MemoryEnabled: true, Reranker: true, WebSearchEnabled: true}, []string{"embedding", "reranker", "web_search"}},
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

// TestRerankerRowCarriesTheMeasuredSize: the reranker row is sized by the pinned
// measurement in internal/memory, and Pick subtracts it from the envelope.
func TestRerankerRowCarriesTheMeasuredSize(t *testing.T) {
	cfg := config.VillaConfig{MemoryEnabled: true, EmbeddingModel: "nomic-embed-text-v1.5", Reranker: true}
	res := ReservationsFor(cfg)
	if len(res) != 2 || res[1].Name != "reranker" || res[1].Bytes != 2147483648 {
		t.Fatalf("rows = %+v, want a 2147483648-byte reranker row second", res)
	}

	const env = uint64(64 << 30)
	cat := testCatalog()
	base := Pick(profileWithEnvelope(env), cat, Overrides{}, nil)
	rec := Pick(profileWithEnvelope(env), cat, Overrides{}, res)
	if want := base.UsableEnvelopeBytes - 536870912 - 2147483648; rec.UsableEnvelopeBytes != want {
		t.Errorf("UsableEnvelopeBytes = %d, want %d (embedding and reranker subtracted)", rec.UsableEnvelopeBytes, want)
	}
	if rec.EmbeddingReservationBytes != 536870912 {
		t.Errorf("EmbeddingReservationBytes = %d, want the embedding row alone", rec.EmbeddingReservationBytes)
	}
}
