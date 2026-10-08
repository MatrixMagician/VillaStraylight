package subsystem

import (
	"strings"
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

// TestMemoryUnitsFollowTheRerankerGate: a host's memory footprint is Qdrant and
// the embedder, plus the reranker only when its gate is on. Every consumer that
// stops, starts, captures or restarts "the memory services" reads this, so a
// memory-on host with the key unset never touches a unit it does not render.
func TestMemoryUnitsFollowTheRerankerGate(t *testing.T) {
	off := config.VillaConfig{MemoryEnabled: true}
	units, services := Memory.Units(off)
	if got := strings.Join(units, ","); got != "villa-qdrant.container,villa-embed.container" {
		t.Errorf("reranker off: units = %v", units)
	}
	if got := strings.Join(services, ","); got != "villa-qdrant.service,villa-embed.service" {
		t.Errorf("reranker off: services = %v", services)
	}

	on := config.VillaConfig{MemoryEnabled: true, Reranker: true}
	units, services = Memory.Units(on)
	if got := strings.Join(units, ","); got != "villa-qdrant.container,villa-embed.container,villa-rerank.container" {
		t.Errorf("reranker on: units = %v", units)
	}
	if got := strings.Join(services, ","); got != "villa-qdrant.service,villa-embed.service,villa-rerank.service" {
		t.Errorf("reranker on: services = %v", services)
	}
}

// TestEveryUnitDeclaresTheReranker: the declaration names every unit a subsystem
// can render, gates aside, so install can name the services it may start and the
// render drift test can bind the whole list.
func TestEveryUnitDeclaresTheReranker(t *testing.T) {
	units, services := Memory.EveryUnit()
	if len(units) != 3 || units[2] != "villa-rerank.container" || services[2] != "villa-rerank.service" {
		t.Errorf("Memory.EveryUnit() = (%v, %v), want the reranker third", units, services)
	}
}
