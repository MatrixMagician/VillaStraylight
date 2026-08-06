// Package memory is the v1.3 memory-stack decision spine: a PURE decision core
// (mirroring the recommend.Pick / preflight idiom) that turns typed config input
// into typed decisions with ZERO host I/O. It imports NEITHER os/exec NOR any
// container-image literal, so TestSeamGrepGate (internal/inference) stays green
// over internal/memory. The two new image literals (Qdrant +
// villa-embed) land in internal/orchestrate later, NEVER here.
//
// footprint.go provides Footprint: the embedding model -> detect.Bytes
// reservation. It is the single home of the embedding-footprint constant and
// returns a typed-Unknown (Known=false, never bare 0) on any catalog miss
// (honesty-by-construction).
//
// PURE function (no I/O); imports no os/exec and no container-image literal
// (TestSeamGrepGate).
package memory

import "github.com/MatrixMagician/VillaStraylight/internal/detect"

// embedFootprints is the single home of the embedding-footprint reservation
// constant. It maps a pinned embedding model id to its conservative
// resident-memory reservation in bytes.
//
// nomic-embed-text-v1.5 is the pinned embedding model. The ~512 MiB value
// is a CONSERVATIVE resident reservation (the Q8_0 GGUF weights are ~146 MB on
// disk; the resident server footprint including KV/runtime is reserved high so it
// over-reserves, NEVER under-reserves on shared gfx1151 GTT). The constant is
// flagged for on-hardware refinement in Phase 19; over-reserving now is
// safe because Phase 22 reserves this BEFORE chat-model fit.
var embedFootprints = map[string]uint64{
	"nomic-embed-text-v1.5": 512 << 20, // ~512 MiB conservative reservation
}

// conservativeFootprintBytes is the conservative default reservation applied
// when an embedding model's footprint is typed-Unknown: the same ~512 MiB
// value embedFootprints pins for nomic-embed-text-v1.5 — over-reserve, NEVER
// reserve 0 on a miss.
const conservativeFootprintBytes uint64 = 512 << 20 // ~512 MiB

// ConservativeFootprintBytes returns the conservative default embedding-footprint
// reservation (mirrors the orchestrate.EmbedImage() accessor shape) so downstream
// readers (recommend's typed-Unknown fallback) never re-type the literal.
func ConservativeFootprintBytes() uint64 { return conservativeFootprintBytes }

// Footprint returns the resident-memory reservation for an embedding model as a
// typed detect.Bytes. On a hit it returns KnownBytes with provenance; on a miss
// (unknown id OR empty string) it returns a typed Unknown (Known=false) carrying
// a reason — NEVER a bare-zero sentinel, so callers test .Known to distinguish
// "no footprint known" from a real zero. PURE: no I/O.
func Footprint(modelID string) detect.Bytes {
	if b, ok := embedFootprints[modelID]; ok {
		return detect.KnownBytes(b, "memory: pinned embedding footprint reservation")
	}
	if modelID == "" {
		return detect.UnknownBytes("memory: no footprint known for empty embedding model id", modelID)
	}
	return detect.UnknownBytes("memory: no footprint known for embedding model "+modelID, modelID)
}
