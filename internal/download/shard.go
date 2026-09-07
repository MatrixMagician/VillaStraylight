package download

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
)

// pullShards downloads + verifies every shard of m into modelsDir, generalizing
// downloadFile over the shard set. A model is acceptable only when ALL
// shards are present and each is individually checksum+size verified; if any one
// shard is missing or mismatched, the whole model is rejected (the error from the
// first failing shard is returned). A single-shard model is the degenerate
// one-element case.
//
// The set is m.AllShards(), so a sidecar (the vision projector) is downloaded and
// verified as part of the model rather than beside it: a model that claims vision
// is never left on disk without the file its rendered --mmproj points at.
//
// "All present" is enforced structurally: the catalog manifest enumerates the full
// -of-0000N set, and pullShards requires every enumerated shard to download and
// verify. A manifest missing a shard cannot be fixed up here — it is the catalog's
// contract that the shard list is complete.
func pullShards(ctx context.Context, client httpDoer, m catalog.Model, modelsDir string) error {
	all := m.AllShards()
	if len(all) == 0 {
		return fmt.Errorf("%w: %s", errNoShards, m.ID)
	}
	for i, sh := range all {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := downloadFile(ctx, client, sh, modelsDir); err != nil {
			return fmt.Errorf("download: model %s shard %d/%d (%s): %w", m.ID, i+1, len(all), sh.Filename, err)
		}
	}
	return verifyGeometry(m, modelsDir)
}

// verifyGeometry cross-checks the entry's declared fit dimensions against the
// header of the file just verified, refusing the pull on a disagreement.
//
// It runs on EVERY pull, including the already-on-disk short-circuit inside
// downloadFile, so a rerun of a bad entry refuses again rather than reporting a
// clean pull the second time.
//
// The file is NOT deleted. Its checksum verified, so the bytes are exactly what
// the catalog pinned; the disagreement is between two pieces of villa's own
// metadata, and discarding a 20 GB download over that would punish the operator
// for villa's bad data.
func verifyGeometry(m catalog.Model, modelsDir string) error {
	want := m.Geometry()
	if want == (gguf.Geometry{}) {
		return nil
	}
	path := filepath.Join(modelsDir, m.PrimaryFile())
	f, err := os.Open(path) //nolint:gosec // PrimaryFile is a bare filename, joined onto the caller's models dir
	if err != nil {
		return fmt.Errorf("download: model %s: cannot re-open %s to confirm its geometry: %w", m.ID, m.PrimaryFile(), err)
	}
	defer f.Close()

	h, err := gguf.ReadHeader(f)
	if err != nil {
		return fmt.Errorf("download: model %s: %s downloaded and checksum-verified, but its GGUF header could not be read: %w", m.ID, m.PrimaryFile(), err)
	}
	got, err := h.Geometry()
	if err != nil {
		return fmt.Errorf("download: model %s: %s downloaded and checksum-verified, but its geometry could not be read: %w", m.ID, m.PrimaryFile(), err)
	}
	if got != want {
		return fmt.Errorf("download: model %s: %s downloaded and checksum-verified (the file is intact), but its header disagrees with the catalog entry — "+
			"catalog n_layers=%d n_kv_heads=%d head_dim=%d; header kv_layers=%d head_count_kv=%d key_length=%d. "+
			"The file is kept; fix the %s entry in internal/catalog/seed.json to match the header, or re-pin the shard",
			m.ID, m.PrimaryFile(), m.NLayers, m.NKVHeads, m.HeadDim, got.KVLayers, got.HeadCountKV, got.KeyLength, m.ID)
	}
	return nil
}
