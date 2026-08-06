package inference

import "testing"

// backend_test.go covers the resolver-adjacent predicates in backend.go that do NOT
// belong to a single backend impl. TestIsROCmFamily guards IsROCmFamily — the single
// place the ROCm-family NAME set is enumerated. Every caller that used to compare
// `== "rocm"` must route through this predicate so a new ROCm digest is gated identically;
// it holds only backend NAME strings (config values), never an image literal, so it stays
// seam-clean (TestSeamGrepGate covers the image-literal leak case).

// TestIsROCmFamily asserts the predicate reports true for all three ROCm-family backend
// names AND for the empty string (which BackendFor resolves to the default ROCm backend
// the two MUST agree or an unset config would run ROCm while skipping the ROCm gate), and
// false for the explicit Vulkan opt-out and unknown values — the single enumeration point
// consumed by the live PreflightROCm gate and the preflight flag router (12-02).
func TestIsROCmFamily(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"rocm", true},
		{"rocm-6.4.4", true},
		{"rocm-6.4.4-rocwmma", true},
		{"", true}, // empty == the default ROCm backend in BackendFor; must be gated as ROCm
		{"vulkan", false},
		{"bogus", false},
		{"ROCM", false}, // case-sensitive: config values are lowercase by construction
	}
	for _, tc := range cases {
		if got := IsROCmFamily(tc.name); got != tc.want {
			t.Errorf("IsROCmFamily(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
