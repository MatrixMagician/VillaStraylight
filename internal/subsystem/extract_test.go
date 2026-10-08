package subsystem

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestExtractOnNeedsMemoryAndTheFlag: the extractor is a derived gate, not a Kind
// (ADR-0029). It is on only when the memory stack is on AND the extractor flag is
// set, so `extractor = true` with memory off renders nothing, and a memory-on
// config predating the flag keeps the stack it had.
func TestExtractOnNeedsMemoryAndTheFlag(t *testing.T) {
	for _, tc := range []struct {
		memory, extractor, want bool
	}{
		{false, false, false},
		{true, false, false},
		{false, true, false},
		{true, true, true},
	} {
		cfg := config.VillaConfig{MemoryEnabled: tc.memory, Extractor: tc.extractor}
		if got := ExtractOn(cfg); got != tc.want {
			t.Errorf("ExtractOn(memory_enabled=%v, extractor=%v) = %v, want %v", tc.memory, tc.extractor, got, tc.want)
		}
	}
}

// TestMemoryUnitsFollowTheExtractorGate: a host's memory footprint carries the
// extractor only when its gate is on, independently of the reranker's. Every
// consumer that stops, starts, captures or restarts "the memory services" reads
// this, so a memory-on host with the key unset never touches a unit it does not
// render.
func TestMemoryUnitsFollowTheExtractorGate(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cfg         config.VillaConfig
		units, svcs string
	}{
		{"extractor off", config.VillaConfig{MemoryEnabled: true},
			"villa-qdrant.container,villa-embed.container",
			"villa-qdrant.service,villa-embed.service"},
		{"extractor on, reranker off", config.VillaConfig{MemoryEnabled: true, Extractor: true},
			"villa-qdrant.container,villa-embed.container,villa-extract.container",
			"villa-qdrant.service,villa-embed.service,villa-extract.service"},
		{"both on", config.VillaConfig{MemoryEnabled: true, Reranker: true, Extractor: true},
			"villa-qdrant.container,villa-embed.container,villa-rerank.container,villa-extract.container",
			"villa-qdrant.service,villa-embed.service,villa-rerank.service,villa-extract.service"},
	} {
		units, services := Memory.Units(tc.cfg)
		if got := strings.Join(units, ","); got != tc.units {
			t.Errorf("%s: units = %s, want %s", tc.name, got, tc.units)
		}
		if got := strings.Join(services, ","); got != tc.svcs {
			t.Errorf("%s: services = %s, want %s", tc.name, got, tc.svcs)
		}
	}
}

// TestEveryUnitDeclaresTheExtractor: the declaration names every unit memory can
// render, gates aside, with the extractor fourth, after the reranker.
func TestEveryUnitDeclaresTheExtractor(t *testing.T) {
	units, services := Memory.EveryUnit()
	if len(units) != 4 || units[3] != "villa-extract.container" || services[3] != "villa-extract.service" {
		t.Errorf("Memory.EveryUnit() = (%v, %v), want the extractor fourth", units, services)
	}
}
