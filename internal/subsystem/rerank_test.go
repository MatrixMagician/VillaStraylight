package subsystem

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestRerankOnNeedsMemoryAndTheFlag: the reranker is a derived gate, not a Kind.
// It is on only when the memory stack is on AND the reranker flag is set, so a
// config that carries `reranker = true` with memory off renders nothing, and a
// memory-on config predating the flag keeps the stack it had.
func TestRerankOnNeedsMemoryAndTheFlag(t *testing.T) {
	for _, tc := range []struct {
		memory, reranker, want bool
	}{
		{false, false, false},
		{true, false, false},
		{false, true, false},
		{true, true, true},
	} {
		cfg := config.VillaConfig{MemoryEnabled: tc.memory, Reranker: tc.reranker}
		if got := RerankOn(cfg); got != tc.want {
			t.Errorf("RerankOn(memory_enabled=%v, reranker=%v) = %v, want %v", tc.memory, tc.reranker, got, tc.want)
		}
	}
}

// TestRerankerMovesWithTheMemorySubsystem: the reranker runs on the embedder's
// image, so an update to that pin must restart it with the embedder. That holds
// only if its unit is declared under Memory, after the pairing it joins.
func TestRerankerMovesWithTheMemorySubsystem(t *testing.T) {
	units, services := Memory.Units()
	if len(units) != 3 || units[2] != "villa-rerank.container" {
		t.Errorf("Memory.Units() = %v, want the reranker third", units)
	}
	if len(services) != 3 || services[2] != "villa-rerank.service" {
		t.Errorf("Memory services = %v, want villa-rerank.service third", services)
	}
}
