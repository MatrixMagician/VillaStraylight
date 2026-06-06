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
	stable := computeROCmReadiness(UnknownStr("", ""), UnknownStr("", ""), resolvedROCmImage())
	if !stable.ImagePolicyOK.Known || !stable.ImagePolicyOK.Value {
		t.Errorf("image_policy_ok should be Known(true) for the pinned stable image, got %+v", stable.ImagePolicyOK)
	}

	nightly := computeROCmReadiness(
		UnknownStr("", ""),
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
