package recall

import "testing"

// skew_test.go proves the D-10 embedding-skew comparison's three typed states:
// an empty recorded stamp is Unknown (no recorded truth ⇒ no alarm — the
// blockOnNewerStore "not recorded" convention), an exact model+dim match is
// Match, and ANY divergence (model OR dim) is Mismatch. EmbeddingSkew is THE
// single implementation shared by status (Plan 23-01) and the recall-index /
// install guards (Plan 23-04) — table-tested here once, never re-rolled.
func TestEmbeddingSkew(t *testing.T) {
	cases := []struct {
		name     string
		st       State
		cfgModel string
		cfgDim   int
		want     SkewState
	}{
		{
			name:     "no recorded stamp → unknown (typed-Unknown, never an alarm)",
			st:       State{},
			cfgModel: "nomic-embed-text-v1.5",
			cfgDim:   768,
			want:     SkewUnknown,
		},
		{
			name:     "model and dim match → match",
			st:       State{EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDim: 768},
			cfgModel: "nomic-embed-text-v1.5",
			cfgDim:   768,
			want:     SkewMatch,
		},
		{
			name:     "model differs → mismatch",
			st:       State{EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDim: 768},
			cfgModel: "some-other-embedder",
			cfgDim:   768,
			want:     SkewMismatch,
		},
		{
			name:     "dim differs → mismatch",
			st:       State{EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDim: 768},
			cfgModel: "nomic-embed-text-v1.5",
			cfgDim:   512,
			want:     SkewMismatch,
		},
		{
			name:     "both differ → mismatch",
			st:       State{EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDim: 768},
			cfgModel: "some-other-embedder",
			cfgDim:   1024,
			want:     SkewMismatch,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EmbeddingSkew(c.st, c.cfgModel, c.cfgDim); got != c.want {
				t.Errorf("EmbeddingSkew = %q, want %q", got, c.want)
			}
		})
	}
}
