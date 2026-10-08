package subsystem

import (
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
