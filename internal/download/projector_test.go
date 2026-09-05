package download

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// TestPullShardsIncludesProjector asserts a vision entry's pull leaves BOTH files
// on disk. A model that claims vision and lands without its projector would
// render a --mmproj at a path that does not exist, so the pull is the only place
// that can keep the pair together.
func TestPullShardsIncludesProjector(t *testing.T) {
	dir := t.TempDir()

	weights := []byte("model weights body")
	wsrv, wshard := serveOne(t, weights)
	_ = wsrv
	wshard.Filename = "vision-model-q4.gguf"

	proj := []byte("projector body!")
	psrv, pshard := serveOne(t, proj)
	_ = psrv
	pshard.Filename = "vision-model-mmproj-F16.gguf"

	m := catalog.Model{
		ID:     "vision-model",
		Shards: []catalog.Shard{wshard},
		Projector: &catalog.Sidecar{
			Shards:      []catalog.Shard{pshard},
			WeightBytes: 1100000000,
			Provenance:  "gfx1151, test",
		},
	}
	if err := pullShards(t.Context(), http.DefaultClient, m, dir); err != nil {
		t.Fatalf("pullShards: %v", err)
	}
	for _, name := range []string{"vision-model-q4.gguf", "vision-model-mmproj-F16.gguf"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s missing after pull: %v", name, err)
		}
	}
}
