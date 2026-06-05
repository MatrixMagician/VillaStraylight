package download

import (
	"context"
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// pullShards downloads + verifies every shard of m into modelsDir, generalizing
// downloadFile over the shard set (D-06). A model is acceptable only when ALL
// shards are present and each is individually checksum+size verified; if any one
// shard is missing or mismatched, the whole model is rejected (the error from the
// first failing shard is returned). A single-shard model is the degenerate
// one-element case.
//
// "All present" is enforced structurally: the catalog manifest enumerates the full
// -of-0000N set, and pullShards requires every enumerated shard to download and
// verify. A manifest missing a shard cannot be fixed up here — it is the catalog's
// contract that the shard list is complete.
func pullShards(ctx context.Context, client httpDoer, m catalog.CatalogModel, modelsDir string) error {
	if len(m.Shards) == 0 {
		return fmt.Errorf("%w: %s", errNoShards, m.ID)
	}
	for i, sh := range m.Shards {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := downloadFile(ctx, client, sh, modelsDir); err != nil {
			return fmt.Errorf("download: model %s shard %d/%d (%s): %w", m.ID, i+1, len(m.Shards), sh.Filename, err)
		}
	}
	return nil
}
