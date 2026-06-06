package preflight

import "testing"

// TestLoadROCmPolicyMatchesV1Floors is the behavior-no-op proof for the floors
// migration (D-04/D-05): the values loaded from the embedded rocm-policy.json MUST
// equal the documented v1.0 constants byte-for-byte. If this drifts, the migration
// silently changed a preflight verdict — the loader is wrong, not the test.
func TestLoadROCmPolicyMatchesV1Floors(t *testing.T) {
	p := loadROCmPolicy()

	cases := []struct {
		name, got, want string
	}{
		{"kernelFloor", p.KernelFloor, "6.18.4"},
		{"kernelTested", p.KernelTested, "6.18.9"},
		{"mesaFloor", p.MesaFloor, "25.0.0"},
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
		{"Mesa", f.Mesa, "25.0.0"},
		{"Firmware", f.Firmware, "20260110"},
		{"FirmwareDeny", f.FirmwareDeny, "20251125"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("Floors().%s = %q, want %q", c.name, c.got, c.want)
		}
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
