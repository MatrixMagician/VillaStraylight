package backendswap

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

const wmma = "rocm-6.4.4-rocwmma"

// TestBackendSetRefusesSpeculationUnsupportedTarget guards that switching to a
// backend whose llama.cpp build has no --spec-type, while speculation is on, is
// refused inside the frame before any capture, config write or unit write.
func TestBackendSetRefusesSpeculationUnsupportedTarget(t *testing.T) {
	for _, mode := range []string{config.SpeculationNgram, config.SpeculationDraft} {
		f := newFake(config.VillaConfig{Model: "preserved-model", Backend: "rocm", Speculation: mode})
		res := Run(f.deps(), wmma)
		if !res.Refused || f.captured || len(f.saved) != 0 || len(f.proved) != 0 {
			t.Fatalf("%s: want a refusal with no side effects, got %+v captured=%v saved=%d", mode, res, f.captured, len(f.saved))
		}
		for _, want := range []string{wmma, "7690", "--spec-type", "villa speculation set off"} {
			if !strings.Contains(res.Reason, want) {
				t.Errorf("%s: reason %q lacks %q", mode, res.Reason, want)
			}
		}
	}
}

// TestBackendSetRocwmmaProceedsWithSpeculationOff guards that the refusal is
// scoped to speculation being on: off (or unset) still switches.
func TestBackendSetRocwmmaProceedsWithSpeculationOff(t *testing.T) {
	for _, mode := range []string{"", config.SpeculationOff} {
		f := newFake(config.VillaConfig{Model: "preserved-model", Backend: "rocm", Speculation: mode})
		res := Run(f.deps(), wmma)
		if !res.Switched || len(f.saved) != 1 || f.saved[0].Backend != wmma {
			t.Fatalf("mode %q: want a switch to rocwmma, got %+v", mode, res)
		}
	}
}

// TestSpeculationSetRefusesOnUnsupportedBackend guards the other direction: turning
// speculation on while the configured backend cannot serve it is refused with no
// side effects, and the remediation names a backend swap.
func TestSpeculationSetRefusesOnUnsupportedBackend(t *testing.T) {
	for _, mode := range []string{config.SpeculationNgram, config.SpeculationDraft} {
		f := newFake(config.VillaConfig{Model: "preserved-model", Backend: wmma})
		res := RunSpeculation(f.deps(), mode)
		if !res.Refused || f.captured || len(f.saved) != 0 {
			t.Fatalf("%s: want a refusal with no side effects, got %+v", mode, res)
		}
		if !strings.Contains(res.Reason, "villa backend set") || !strings.Contains(res.Reason, "--spec-type") {
			t.Errorf("%s: reason %q lacks the remediation", mode, res.Reason)
		}
	}
	f := newFake(config.VillaConfig{Model: "preserved-model", Backend: wmma, Speculation: config.SpeculationNgram})
	if res := RunSpeculation(f.deps(), config.SpeculationOff); !res.Switched {
		t.Errorf("turning speculation off on rocwmma must still work, got %+v", res)
	}
}

// TestSpeculationStillAllowedOnOtherBackends guards that only rocwmma loses
// speculation: every other backend accepts both switches.
func TestSpeculationStillAllowedOnOtherBackends(t *testing.T) {
	for _, b := range []string{"vulkan", "", "rocm", "rocm-10.0", "rocm-7.2.4", "rocm-6.4.4"} {
		f := newFake(config.VillaConfig{Model: "preserved-model", Backend: b})
		if res := RunSpeculation(f.deps(), config.SpeculationNgram); !res.Switched {
			t.Errorf("backend %q: speculation set ngram refused: %+v", b, res)
		}
		if b == "" || b == "rocm-10.0" {
			continue
		}
		g := newFake(config.VillaConfig{Model: "preserved-model", Backend: "rocm-10.0", Speculation: config.SpeculationNgram})
		if res := Run(g.deps(), b); !res.Switched {
			t.Errorf("backend set %q under ngram refused: %+v", b, res)
		}
	}
}
