// memory.go provides the two config-consuming halves of the pure internal/memory
// decision core: Decide (the fail-closed enablement-and-fields-valid gate, D-02b)
// and RenderView (the resolved-values-only orchestrate handoff, D-02c).
//
// Decide is the validation BOUNDARY between an untrusted (possibly hand-edited)
// config.VillaConfig and the rest of the memory stack: memory-on with any
// missing/invalid field is refused-with-reason (Valid:false), mirroring the
// recommend/preflight refuse-with-Notes discipline. It NEVER silently defaults a
// bad field and NEVER panics (T-18-03).
//
// RenderView is the recommend->orchestrate handoff: it carries ONLY resolved
// values (model id, dim, container-DNS addr/port PIECES). It composes no URL and
// holds no container-image literal — orchestrate builds the endpoint URLs and
// owns the image identity later (D-10 / openwebui.go precedent).
//
// PURE: no I/O, no os/exec, no container-image literal — TestSeamGrepGate stays
// green over internal/memory (D-01/SC#2).
package memory

import "github.com/MatrixMagician/VillaStraylight/internal/config"

// Decision is the typed result of the enablement-and-fields-valid gate (D-02b).
// Enabled mirrors cfg.MemoryEnabled; Valid is true when the configuration is a
// coherent state (memory off, OR memory on with every required field present and
// valid); Reasons enumerates each refusal when Valid is false (fail-closed: all
// problems are surfaced in a single pass, never just the first).
type Decision struct {
	Enabled bool
	Valid   bool
	Reasons []string
}

// Decide is the fail-closed enablement-and-fields-valid gate (D-02b). Memory off
// is a valid state ({Enabled:false, Valid:true}, no reasons). Memory on validates
// every required field — embedding model non-empty; embedding dim > 0 (the
// load-bearing pinned value, D-03); qdrant addr non-empty + port > 0; embed addr
// non-empty + port > 0 — accumulating a user-facing reason per offending field
// and returning {Enabled:true, Valid:len(reasons)==0, Reasons:reasons}. It does
// NO I/O and NEVER panics (PURE, T-18-03).
func Decide(cfg config.VillaConfig) Decision {
	if !cfg.MemoryEnabled {
		return Decision{Enabled: false, Valid: true}
	}

	var reasons []string
	if cfg.EmbeddingModel == "" {
		reasons = append(reasons, "embedding_model is empty (a pinned embedding model id is required when memory is enabled)")
	}
	if cfg.EmbeddingDim <= 0 {
		reasons = append(reasons, "embedding_dim must be a positive integer (the pinned embedding dimension is load-bearing; changing it corrupts existing vectors)")
	}
	if cfg.QdrantAddr == "" {
		reasons = append(reasons, "qdrant_addr is empty (the in-network Qdrant container-DNS name is required when memory is enabled)")
	}
	if cfg.QdrantPort <= 0 {
		reasons = append(reasons, "qdrant_port must be a positive integer (the in-network Qdrant REST port is required when memory is enabled)")
	}
	if cfg.EmbedAddr == "" {
		reasons = append(reasons, "embed_addr is empty (the in-network villa-embed container-DNS name is required when memory is enabled)")
	}
	if cfg.EmbedPort <= 0 {
		reasons = append(reasons, "embed_port must be a positive integer (the in-network villa-embed OpenAI /v1 port is required when memory is enabled)")
	}

	return Decision{Enabled: true, Valid: len(reasons) == 0, Reasons: reasons}
}

// MemoryRenderInput is the resolved-values-only recommend->orchestrate handoff
// (D-02c). It carries the memory-stack endpoint PIECES — the embedding model id
// and dimension plus the container-DNS addr/port pairs for Qdrant and villa-embed
// — and NOTHING ELSE: no composed URL, no container-image literal. orchestrate
// composes "http://villa-embed:8080/v1" and "http://villa-qdrant:6333" from these
// pieces and owns the image identity itself, later (D-10).
type MemoryRenderInput struct {
	EmbeddingModel string
	EmbeddingDim   int
	QdrantAddr     string
	QdrantPort     int
	EmbedAddr      string
	EmbedPort      int
}

// RenderView maps the cfg memory fields one-for-one into a MemoryRenderInput —
// resolved VALUES only (addr/port pieces, never composed URLs; never an image
// literal). It is PURE and does no validation (callers gate with Decide first).
func RenderView(cfg config.VillaConfig) MemoryRenderInput {
	return MemoryRenderInput{
		EmbeddingModel: cfg.EmbeddingModel,
		EmbeddingDim:   cfg.EmbeddingDim,
		QdrantAddr:     cfg.QdrantAddr,
		QdrantPort:     cfg.QdrantPort,
		EmbedAddr:      cfg.EmbedAddr,
		EmbedPort:      cfg.EmbedPort,
	}
}
