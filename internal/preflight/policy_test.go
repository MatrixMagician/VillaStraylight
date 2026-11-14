package preflight

import (
	"slices"
	"testing"
)

// TestLoadROCmPolicyMatchesV1Floors is the behavior-no-op proof for the floors
// migration: the values loaded from the embedded rocm-policy.json MUST
// equal the documented v1.0 constants byte-for-byte. If this drifts, the migration
// silently changed a preflight verdict — the loader is wrong, not the test.
func TestLoadROCmPolicyMatchesV1Floors(t *testing.T) {
	p := loadROCmPolicy()

	cases := []struct {
		name, got, want string
	}{
		{"kernelFloor", p.KernelFloor, "6.18.4"},
		{"kernelTested", p.KernelTested, "6.18.9"},
		{"firmwareFloor", p.FirmwareFloor, "20260110"},
		{"requiredHSAOverride", p.RequiredHSAOverride, "11.5.1"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("policy %s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// TestFloorsSourcedFromPolicy asserts the public Floors() accessor returns the
// v1.0 values now sourced from the embedded policy — the same contract the
// existing checks_gpu.go checks and goldens depend on.
func TestFloorsSourcedFromPolicy(t *testing.T) {
	f := Floors()
	cases := []struct {
		name, got, want string
	}{
		{"Kernel", f.Kernel, "6.18.4"},
		{"KernelTested", f.KernelTested, "6.18.9"},
		{"Firmware", f.Firmware, "20260110"},
		{"FirmwareDeny", f.FirmwareDeny, "20251125"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("Floors().%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// TestPolicyCarriesTestedPodmanMajors asserts the tested Podman majors are data
// in rocm-policy.json, surfaced through Floors(), not a literal in the check.
func TestPolicyCarriesTestedPodmanMajors(t *testing.T) {
	want := []int{5, 6}
	if got := loadROCmPolicy().PodmanTested; !slices.Equal(got, want) {
		t.Errorf("policy podmanTested = %v, want %v", got, want)
	}
	if got := Floors().PodmanTested; !slices.Equal(got, want) {
		t.Errorf("Floors().PodmanTested = %v, want %v", got, want)
	}
}

// TestPolicyTestedKernelStaysTheOldestBaseline pins the kernelTested decision:
// PRE-06 WARNs below it, so raising it to the newest validated kernel (7.3.3)
// would newly warn Fedora 44 hosts on 6.18.x. It stays the oldest tested kernel.
func TestPolicyTestedKernelStaysTheOldestBaseline(t *testing.T) {
	if got := loadROCmPolicy().KernelTested; got != "6.18.9" {
		t.Errorf("kernelTested = %q, want 6.18.9", got)
	}
}

// TestPolicyCarriesROCmDenylists asserts the NEW ROCm policy fields the RunROCm
// checks gate on are present in the embedded policy.
func TestPolicyCarriesROCmDenylists(t *testing.T) {
	p := loadROCmPolicy()

	if !contains(p.FirmwareDeny, "20251125") {
		t.Errorf("firmwareDeny = %v, want it to contain 20251125", p.FirmwareDeny)
	}
	if !contains(p.ImageDeny, "rocm7-nightlies") {
		t.Errorf("imageDeny = %v, want it to contain rocm7-nightlies", p.ImageDeny)
	}
	if p.RequiredHSAOverride != "11.5.1" {
		t.Errorf("requiredHSAOverride = %q, want 11.5.1", p.RequiredHSAOverride)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
