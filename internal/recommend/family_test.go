package recommend

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// TestIsROCmFamilyAgreesWithTheSeam: recommend mirrors inference.IsROCmFamily rather
// than importing it, so the two lists can drift when a backend is added (ADR-0022
// added two). A name one calls ROCm and the other does not would fit-math a pick
// against the wrong gate.
func TestIsROCmFamilyAgreesWithTheSeam(t *testing.T) {
	for _, name := range []string{
		"", "rocm", "rocm-10.0", "rocm-7.2.4", "rocm-6.4.4", "rocm-6.4.4-rocwmma",
		"vulkan", "bogus", "ROCM",
	} {
		if got, want := IsROCmFamily(name), inference.IsROCmFamily(name); got != want {
			t.Errorf("recommend.IsROCmFamily(%q) = %v, inference says %v", name, got, want)
		}
	}
}
