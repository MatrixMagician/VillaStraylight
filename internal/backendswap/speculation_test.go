package backendswap

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// speculation_test.go covers RunSpeculation's own decisions: an unset mode is off,
// the fit guard sees the TARGET mode, and only the mode is written.

// TestSpeculationNoOp: an unset mode renders off, so setting off on a fresh install
// is a no-op rather than a restart.
func TestSpeculationNoOp(t *testing.T) {
	f := newFake(vulkan())
	res := RunSpeculation(f.deps(), config.SpeculationOff)
	if !res.NoOp || res.From != config.SpeculationOff || f.captured {
		t.Fatalf("expected a clean NoOp from the unset (off) mode, got %+v", res)
	}
}

// TestSpeculationRefusesUnqualifiedTarget: ResolveSpeculation's refusal arrives
// through the fit guard, which must see the TARGET mode, before any capture.
func TestSpeculationRefusesUnqualifiedTarget(t *testing.T) {
	f := newFake(vulkan())
	f.fitOK, f.fitReason = false, "speculation: ngram not qualified for preserved-model, refusing"
	res := RunSpeculation(f.deps(), config.SpeculationNgram)
	if !res.Refused || res.Reason != f.fitReason || f.captured {
		t.Fatalf("expected the qualification refusal before capture, got %+v", res)
	}
	if f.fitSaw.Speculation != config.SpeculationNgram {
		t.Errorf("fit guard saw mode %q, want the target", f.fitSaw.Speculation)
	}
}

// TestSpeculationSuccessPersistsTheMode: only the mode changes, and the proof drives
// the unchanged backend.
func TestSpeculationSuccessPersistsTheMode(t *testing.T) {
	f := newFake(vulkan())
	res := RunSpeculation(f.deps(), config.SpeculationNgram)
	if !res.Switched || res.From != config.SpeculationOff || res.To != config.SpeculationNgram {
		t.Fatalf("expected Switched off→ngram, got %+v", res)
	}
	if len(f.saved) != 1 || f.saved[0].Speculation != config.SpeculationNgram || f.saved[0].Backend != "vulkan" {
		t.Errorf("saved %+v, want only the mode changed", f.saved)
	}
	if len(f.proved) != 1 || f.proved[0] != "vulkan" {
		t.Errorf("proved %v, want the unchanged backend", f.proved)
	}
}
