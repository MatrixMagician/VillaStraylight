package recommend

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// TestConservativeFloorFromRAM asserts that when the GTT envelope is Unknown but
// total RAM is known, a ~50%-of-RAM floor is derived (D-14).
func TestConservativeFloorFromRAM(t *testing.T) {
	const ram uint64 = 128 << 30
	p := detect.HostProfile{
		TotalRAMBytes:       detect.KnownBytes(ram, "ghw.Memory"),
		UsableEnvelopeBytes: detect.UnknownBytes("envelope unreadable", ""),
	}
	floor, ok := conservativeFloor(p)
	if !ok {
		t.Fatalf("conservativeFloor: expected a derivable floor from known RAM")
	}
	want := uint64(float64(ram) * degradedFloorFraction)
	if floor != want {
		t.Errorf("floor = %d, want %d (50%% of RAM)", floor, want)
	}
	if floor >= ram {
		t.Errorf("floor %d must never meet or exceed RAM %d (no guessing high)", floor, ram)
	}
}

// TestNoFloorWhenNothingKnown asserts that when neither GTT nor RAM is known, no
// floor is derivable (caller must refuse, D-14).
func TestNoFloorWhenNothingKnown(t *testing.T) {
	p := detect.HostProfile{
		TotalRAMBytes:       detect.UnknownBytes("ram unknown", ""),
		UsableEnvelopeBytes: detect.UnknownBytes("envelope unknown", ""),
	}
	if _, ok := conservativeFloor(p); ok {
		t.Errorf("expected no derivable floor when nothing is known")
	}
}

// TestResolveEnvelopePrefersRealGTT asserts a known GTT envelope is used as-is
// and is NOT marked degraded.
func TestResolveEnvelopePrefersRealGTT(t *testing.T) {
	const env uint64 = 62 << 30
	p := detect.HostProfile{
		TotalRAMBytes:       detect.KnownBytes(128<<30, "ghw"),
		UsableEnvelopeBytes: detect.KnownBytes(env, "mem_info_gtt_total"),
	}
	got, degraded, ok := resolveEnvelope(p)
	if !ok || degraded || got != env {
		t.Errorf("resolveEnvelope(real GTT) = (%d, degraded=%v, ok=%v), want (%d, false, true)", got, degraded, ok, env)
	}
}

// TestHeadroomFractionApplied asserts headroomBytes applies the tunable fraction.
func TestHeadroomFractionApplied(t *testing.T) {
	const env uint64 = 100 << 30
	got := headroomBytes(env)
	want := uint64(float64(env) * headroomFraction)
	if got != want {
		t.Errorf("headroomBytes = %d, want %d", got, want)
	}
}
