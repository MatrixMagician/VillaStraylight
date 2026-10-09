package updatecheck

// gate_test.go guards that the pin set a check plans for a subsystem is the set of
// pins the host RENDERS, not the subsystem's whole declaration. A memory-on host
// that has not turned the extractor on renders no villa-extract unit; a row that
// named its pin anyway would make `villa update` pull, prove and record an
// effective pin for a service the host does not run, the static-list defect of
// #317's first round in a new place.

import (
	"slices"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/manifestverify"
	"github.com/MatrixMagician/VillaStraylight/internal/pinresolve"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
)

// memoryComponents runs a check over cfg and returns the memory row's component
// names in row order.
func memoryComponents(t *testing.T, cfg config.VillaConfig, state pinstate.State) []string {
	t.Helper()
	r := Check(Input{
		Cfg:          cfg,
		Resolver:     pinresolve.New(state),
		Verdict:      acceptedVerdict(nil),
		CheckedAt:    "2026-08-26T12:00:00Z",
		VillaVersion: "v1.7",
	})
	for _, s := range r.Subsystems {
		if s.Name == "memory" {
			var names []string
			for _, c := range s.Components {
				names = append(names, c.Name)
			}
			return names
		}
	}
	t.Fatal("the report has no memory row")
	return nil
}

// TestTheMemoryPinSetFollowsTheExtractorGate: with the gate off the memory row is
// exactly the pin set it had before the extractor existed; with the gate on the
// extractor's pin joins it.
func TestTheMemoryPinSetFollowsTheExtractorGate(t *testing.T) {
	off := config.VillaConfig{Backend: "vulkan", MemoryEnabled: true}
	if got, want := memoryComponents(t, off, pinstate.State{}), []string{"qdrant", "embedder"}; !slices.Equal(got, want) {
		t.Errorf("memory row with the extractor off = %v, want %v (the host renders no villa-extract unit)", got, want)
	}

	on := off
	on.Extractor = true
	if got, want := memoryComponents(t, on, pinstate.State{}), []string{"qdrant", "embedder", "extractor"}; !slices.Equal(got, want) {
		t.Errorf("memory row with the extractor on = %v, want %v", got, want)
	}
}

// TestAStrayExtractorPinIsNotDivergenceWithTheGateOff: a recorded effective pin for
// a unit the host does not render is not a claim about what the host runs, so the
// reject's divergence list leaves it out, the same filtering a disabled subsystem
// gets; with the gate on the same record is divergence.
func TestAStrayExtractorPinIsNotDivergenceWithTheGateOff(t *testing.T) {
	stray := "docker.io/apache/tika:3.3.1.0-full@sha256:4444444444444444444444444444444444444444444444444444444444444444"
	state := pinstate.State{Pins: map[string]pinstate.Effective{string(pins.Extractor): {Ref: stray}}}
	rejected := manifestverify.Verdict{Outcome: manifestverify.Absent, Reason: manifestverify.ReasonExpired, Message: "expired"}
	divergedNames := func(cfg config.VillaConfig) []string {
		r := Check(Input{Cfg: cfg, Resolver: pinresolve.New(state), Verdict: rejected, CheckedAt: "2026-08-26T12:00:00Z", VillaVersion: "v1.7"})
		var names []string
		for _, c := range r.Diverged {
			names = append(names, c.Name)
		}
		return names
	}

	cfg := config.VillaConfig{Backend: "vulkan", MemoryEnabled: true}
	if got := divergedNames(cfg); slices.Contains(got, string(pins.Extractor)) {
		t.Errorf("diverged = %v on a host whose config renders no extractor", got)
	}
	cfg.Extractor = true
	if got, want := divergedNames(cfg), []string{string(pins.Extractor)}; !slices.Equal(got, want) {
		t.Errorf("diverged with the gate on = %v, want %v", got, want)
	}
}
