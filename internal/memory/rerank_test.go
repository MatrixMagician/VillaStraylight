package memory

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestRenderViewCarriesTheRerankerEndpoint: the reranker's container-DNS name and
// port reach orchestrate through the same resolved-values handoff as the
// embedder's, so the unit and Open WebUI's reranker URL derive from one home.
func TestRenderViewCarriesTheRerankerEndpoint(t *testing.T) {
	got := RenderView(config.VillaConfig{MemoryEnabled: true, Reranker: true})
	if got.RerankAddr != "villa-rerank" || got.RerankPort != 8080 {
		t.Errorf("RenderView reranker endpoint = %q:%d, want villa-rerank:8080", got.RerankAddr, got.RerankPort)
	}
}

// TestRerankFootprintIsTheMeasuredReservation: the reranker's footprint is the
// dev-host measurement rounded up (ADR-0028), never zero and larger than the
// embedder's, because it holds a bigger model and a four-times-larger batch.
func TestRerankFootprintIsTheMeasuredReservation(t *testing.T) {
	if got := RerankFootprintBytes(); got != 2147483648 {
		t.Errorf("RerankFootprintBytes = %d, want 2147483648 (2 GiB)", got)
	}
	if RerankFootprintBytes() <= ConservativeFootprintBytes() {
		t.Errorf("the reranker reserves %d, not above the embedder's %d", RerankFootprintBytes(), ConservativeFootprintBytes())
	}
}
