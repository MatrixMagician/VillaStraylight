package memory

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestRenderViewCarriesTheExtractorEndpoint: the extractor's container-DNS name and
// port reach orchestrate through the same resolved-values handoff as the
// reranker's, so the unit and Open WebUI's Tika URL derive from one home.
func TestRenderViewCarriesTheExtractorEndpoint(t *testing.T) {
	got := RenderView(config.VillaConfig{MemoryEnabled: true, Extractor: true})
	if got.ExtractAddr != "villa-extract" || got.ExtractPort != 9998 {
		t.Errorf("RenderView extractor endpoint = %q:%d, want villa-extract:9998", got.ExtractAddr, got.ExtractPort)
	}
}

// TestExtractFootprintIsTheMeasuredReservation: the extractor's footprint is the
// bound the dev-host measurement implies (ADR-0029): the 1 GiB heap cap, the JVM's
// native memory and up to three tesseract processes, rounded to 2 GiB.
func TestExtractFootprintIsTheMeasuredReservation(t *testing.T) {
	if got := ExtractFootprintBytes(); got != 2147483648 {
		t.Errorf("ExtractFootprintBytes = %d, want 2147483648 (2 GiB)", got)
	}
}
