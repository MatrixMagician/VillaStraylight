// geometry.go is where a catalog entry meets the file it describes: the two
// projections every cross-check needs, owned once here rather than re-rolled at
// each call site.
//
// INVARIANT: these are projections, never decisions. Geometry restates the
// entry's hand-carried fit dimensions in the shape internal/gguf returns from a
// file header, so a comparison is a value comparison; it does not read a file and
// it never rewrites an entry. PrimaryFile names the on-disk GGUF an entry resolves
// to, and it was duplicated in two packages before it landed here.
package catalog

import (
	"path/filepath"

	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
)

// Geometry projects the entry's fit dimensions into the same value the GGUF
// header reader returns. The zero Geometry means the entry declares none, which
// every caller reads as "nothing to cross-check".
func (m Model) Geometry() gguf.Geometry {
	return gguf.Geometry{KVLayers: m.NLayers, HeadCountKV: m.NKVHeads, KeyLength: m.HeadDim}
}

// PrimaryFile resolves the on-disk GGUF filename for the entry: the first shard's
// filename from the download manifest, falling back to <id>.gguf when no shard
// metadata is present (test fixtures and hand-placed files).
//
// The result is always a bare filename. An external catalog is a trust boundary
// and neither the id nor a shard filename is validated at load, so callers that
// join this onto the models dir must not be handed a separator to follow.
func (m Model) PrimaryFile() string {
	if len(m.Shards) > 0 && m.Shards[0].Filename != "" {
		return filepath.Base(m.Shards[0].Filename)
	}
	return filepath.Base(m.ID + ".gguf")
}
