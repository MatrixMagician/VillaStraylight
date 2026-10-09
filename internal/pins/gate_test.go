package pins

// gate_test.go binds a pin to the unit it renders into. A pin that names a unit
// is in a host's pin set only when the subsystem's unit registry renders that
// unit for the host's config, so the pin set and the unit set cannot disagree:
// both read internal/subsystem's one gate.

import (
	"slices"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// fullStackConfig turns every gate on, so a walk over it sees every pin.
func fullStackConfig() config.VillaConfig {
	return config.VillaConfig{
		MemoryEnabled:    true,
		Reranker:         true,
		Extractor:        true,
		WebSearchEnabled: true,
		AgentEnabled:     true,
		WorkspaceAgent:   true,
	}
}

// TestForFollowsTheUnitRegistry: the extractor's pin is memory's only when the
// config renders villa-extract; Qdrant's and the embedder's pins are memory's
// whenever memory is.
func TestForFollowsTheUnitRegistry(t *testing.T) {
	ids := func(cfg config.VillaConfig) []ComponentID {
		var out []ComponentID
		for _, e := range For(subsystem.Memory, cfg) {
			out = append(out, e.Component)
		}
		return out
	}
	off := config.VillaConfig{MemoryEnabled: true}
	if got, want := ids(off), []ComponentID{Qdrant, Embedder}; !slices.Equal(got, want) {
		t.Errorf("memory pins with the extractor off = %v, want %v", got, want)
	}
	on := off
	on.Extractor = true
	if got, want := ids(on), []ComponentID{Qdrant, Embedder, Extractor}; !slices.Equal(got, want) {
		t.Errorf("memory pins with the extractor on = %v, want %v", got, want)
	}
}

// TestAPinsUnitIsOneItsSubsystemCanRender: a Unit a pin names must be in its
// subsystem's declaration, or the binding is a typo that silently gates the pin
// off everywhere.
func TestAPinsUnitIsOneItsSubsystemCanRender(t *testing.T) {
	for _, e := range Table() {
		if e.Unit == "" {
			continue
		}
		units, _ := e.Subsystem.EveryUnit()
		if !slices.Contains(units, e.Unit) {
			t.Errorf("%s names unit %q, which %v never renders", e.Component, e.Unit, e.Subsystem)
		}
	}
}
