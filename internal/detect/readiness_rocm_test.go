package detect

import "testing"

// TestComputeROCmReadinessOffHardware asserts the no-false-green guarantee (D-08):
// an off-hardware HostProfile (IGPUGfxID Unknown, KernelVersion Known) yields
// rocminfo_gfx1151 and firmware_date_ok as UNSET (Known==false), NOT a real false,
// while kernel_floor_ok is a real Known value derived from the known kernel.
func TestComputeROCmReadinessOffHardware(t *testing.T) {
	r := computeROCmReadiness(
		UnknownStr("rocminfo unavailable (gfx id not enumerated)", ""),
		KnownStr("7.0.10-201.fc44.x86_64", "/proc/sys/kernel/osrelease"),
		rocmPresent(),
		UnknownStr("firmware date not probed (test)", ""),
		resolvedROCmImage(),
	)

	if r.RocminfoGfx1151.Known {
		t.Errorf("rocminfo_gfx1151 should be UNSET off-hardware (no-false-green), got Known=%v Value=%v", r.RocminfoGfx1151.Known, r.RocminfoGfx1151.Value)
	}
	if r.FirmwareDateOK.Known {
		t.Errorf("firmware_date_ok should be UNSET off-hardware (not probed), got Known=%v Value=%v", r.FirmwareDateOK.Known, r.FirmwareDateOK.Value)
	}
	if r.HSAOverrideViable.Known {
		t.Errorf("hsa_override_viable should be UNSET off-hardware (not probed), got Known=%v", r.HSAOverrideViable.Known)
	}
	// kernel_floor_ok is derivable from the Known kernel — a real Known(true) since
	// 7.0.10 > the 6.18.4 floor.
	if !r.KernelFloorOK.Known || !r.KernelFloorOK.Value {
		t.Errorf("kernel_floor_ok should be Known(true) for a 7.0.10 kernel, got %+v", r.KernelFloorOK)
	}
}

// TestKernelFloorKnownBelowFloor asserts a known-too-old kernel yields a confident
// KnownBool(false) (not Unknown) — a real, evaluable negative.
func TestKernelFloorKnownBelowFloor(t *testing.T) {
	r := computeROCmReadiness(
		UnknownStr("rocminfo unavailable", ""),
		KnownStr("6.17.0-100.fc44.x86_64", "/proc/sys/kernel/osrelease"),
		rocmPresent(),
		UnknownStr("firmware date not probed (test)", ""),
		resolvedROCmImage(),
	)
	if !r.KernelFloorOK.Known {
		t.Fatalf("kernel_floor_ok should be Known for a known kernel, got %+v", r.KernelFloorOK)
	}
	if r.KernelFloorOK.Value {
		t.Errorf("kernel_floor_ok should be false for 6.17.0 (< 6.18.4 floor), got true")
	}
}

// TestKernelFloorUnknownKernel asserts an UNKNOWN kernel version leaves
// kernel_floor_ok UNSET rather than fabricating a pass/fail.
func TestKernelFloorUnknownKernel(t *testing.T) {
	r := computeROCmReadiness(
		KnownStr("gfx1151", "rocminfo:Name"),
		UnknownStr("osrelease unreadable", "raw"),
		rocmPresent(),
		UnknownStr("firmware date not probed (test)", ""),
		resolvedROCmImage(),
	)
	if r.KernelFloorOK.Known {
		t.Errorf("kernel_floor_ok should be UNSET when kernel is Unknown, got %+v", r.KernelFloorOK)
	}
	// gfx id Known → rocminfo_gfx1151 is a real Known(true).
	if !r.RocminfoGfx1151.Known || !r.RocminfoGfx1151.Value {
		t.Errorf("rocminfo_gfx1151 should be Known(true) for gfx1151, got %+v", r.RocminfoGfx1151)
	}
}

// TestImagePolicyOK asserts image_policy_ok is computed from the resolved image
// string (config-driven, not a host probe): the in-tree pinned stable image is
// KnownBool(true) and a synthetic nightlies tag is a confident KnownBool(false).
func TestImagePolicyOK(t *testing.T) {
	stable := computeROCmReadiness(
		UnknownStr("", ""),
		UnknownStr("", ""),
		rocmPresent(),
		UnknownStr("", ""),
		resolvedROCmImage(),
	)
	if !stable.ImagePolicyOK.Known || !stable.ImagePolicyOK.Value {
		t.Errorf("image_policy_ok should be Known(true) for the pinned stable image, got %+v", stable.ImagePolicyOK)
	}

	nightly := computeROCmReadiness(
		UnknownStr("", ""),
		UnknownStr("", ""),
		rocmPresent(),
		UnknownStr("", ""),
		"docker.io/kyuz0/amd-strix-halo-toolboxes:rocm7-nightlies",
	)
	if !nightly.ImagePolicyOK.Known {
		t.Fatalf("image_policy_ok should be Known for a recognized nightly tag, got %+v", nightly.ImagePolicyOK)
	}
	if nightly.ImagePolicyOK.Value {
		t.Errorf("image_policy_ok should be false for a nightlies image (64 GB cap), got true")
	}
}

// TestFirmwareDateOK asserts the firmware-date verdict against the floor/denylist
// (gpu_amd.go seam) plus the no-false-green UNSET path for an unprobed date.
func TestFirmwareDateOK(t *testing.T) {
	cases := []struct {
		name      string
		date      Str
		wantKnown bool
		wantValue bool
	}{
		{"clear date >= floor", KnownStr("20260519", "rpm"), true, true},
		{"exactly at floor", KnownStr("20260110", "rpm"), true, true},
		{"denylisted date", KnownStr("20251125", "rpm"), true, false},
		{"sub-floor not denied", KnownStr("20251231", "rpm"), true, false},
		{"unprobed firmware", UnknownStr("rpm absent", ""), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := firmwareDateOK(tc.date)
			if got.Known != tc.wantKnown {
				t.Fatalf("firmwareDateOK(%+v).Known = %v, want %v", tc.date, got.Known, tc.wantKnown)
			}
			if tc.wantKnown && got.Value != tc.wantValue {
				t.Errorf("firmwareDateOK(%+v).Value = %v, want %v", tc.date, got.Value, tc.wantValue)
			}
		})
	}
}

// TestHSAOverrideViable asserts the HSA-override viability is a pure derivation from
// gfx-id + ROCm substrate, gated UNSET when the gfx-id is unknown (no-false-green),
// and never reads HSA_OVERRIDE_GFX_VERSION.
func TestHSAOverrideViable(t *testing.T) {
	cases := []struct {
		name        string
		gfxID       Str
		rocmPresent Bool
		wantKnown   bool
		wantValue   bool
	}{
		{"gfx1151 + rocm present", KnownStr("gfx1151", "rocminfo"), KnownBool(true, "test"), true, true},
		{"non-gfx1151 + rocm present", KnownStr("gfx1100", "rocminfo"), KnownBool(true, "test"), true, false},
		{"gfx1151 + no rocm", KnownStr("gfx1151", "rocminfo"), KnownBool(false, "test"), true, false},
		{"gfx-id unknown", UnknownStr("rocminfo absent", ""), KnownBool(true, "test"), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hsaOverrideViable(tc.gfxID, tc.rocmPresent)
			if got.Known != tc.wantKnown {
				t.Fatalf("hsaOverrideViable(%+v, %+v).Known = %v, want %v", tc.gfxID, tc.rocmPresent, got.Known, tc.wantKnown)
			}
			if tc.wantKnown && got.Value != tc.wantValue {
				t.Errorf("hsaOverrideViable(%+v, %+v).Value = %v, want %v", tc.gfxID, tc.rocmPresent, got.Value, tc.wantValue)
			}
		})
	}
}
