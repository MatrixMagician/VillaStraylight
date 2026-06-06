package detect

// readiness_rocm.go computes the v1.1 (schema 2) rocm_readiness sub-tree from
// already-bounded HostProfile facts plus the resolved ROCm image (DET-04). It is
// BACKEND-NEUTRAL: it carries no image-tag, kernel-floor, or ROCm-specific literal
// — every such literal lives behind the gpu_amd.go seam (resolvedROCmImage /
// rocmImagePolicyOK / kernelMeetsROCmFloor), so the inference TestSeamGrepGate
// stays green.
//
// The discipline is the no-false-green guarantee (D-08): a field is a real
// KnownBool ONLY when the underlying fact is Known; an undetectable off-hardware
// signal yields UnknownBool(reason, raw), which serializes as UNSET (Known=false)
// — distinct from a confident real false. Off-hardware (rocminfo absent →
// IGPUGfxID Unknown; firmware/override not probed) these fields are unset, never a
// fabricated false.

// computeROCmReadiness builds the ROCmReadiness sub-tree from the assembled
// HostProfile facts and the resolved ROCm image string. Each field returns
// KnownBool only when its source fact is Known; otherwise UnknownBool.
func computeROCmReadiness(gfxID Str, kernel Str, resolvedImage string) ROCmReadiness {
	return ROCmReadiness{
		HSAOverrideViable: hsaOverrideViable(),
		FirmwareDateOK:    firmwareDateOK(),
		KernelFloorOK:     kernelFloorOK(kernel),
		RocminfoGfx1151:   rocminfoGfx1151(gfxID),
		ImagePolicyOK:     rocmImagePolicyOK(resolvedImage),
	}
}

// rocminfoGfx1151 reports whether rocminfo enumerated the gfx1151 target. It is
// KnownBool ONLY when IGPUGfxID is Known (rocminfo ran and produced a gfx id);
// off-hardware rocminfo is absent → IGPUGfxID Unknown → this is UnknownBool, NOT a
// real false (no-false-green, D-08).
func rocminfoGfx1151(gfxID Str) Bool {
	if !gfxID.Known {
		return UnknownBool("rocminfo gfx id not enumerated (rocm readiness unevaluable)", gfxID.Raw)
	}
	return KnownBool(gfxID.Value == "gfx1151", "rocminfo:Name")
}

// kernelFloorOK reports whether the running kernel meets the gfx1151 floor. It is
// KnownBool only when KernelVersion is Known; the floor compare itself lives behind
// the gpu_amd.go seam (kernelMeetsROCmFloor) so this file carries no version
// literal.
func kernelFloorOK(kernel Str) Bool {
	if !kernel.Known {
		return UnknownBool("kernel version unknown (floor unevaluable)", kernel.Raw)
	}
	return KnownBool(kernelMeetsROCmFloor(kernel.Value), "kernel >= gfx1151 floor")
}

// firmwareDateOK is UNSET off-hardware: the linux-firmware date is not probed in
// the detect path (it mirrors the preflight firmware advisory shape), so reporting
// a real false would be a false-green. A real probe would replace this with a
// KnownBool comparison against the denylist.
func firmwareDateOK() Bool {
	return UnknownBool("firmware date not probed (advisory only off-hardware)", "")
}

// hsaOverrideViable is UNSET off-hardware: the HSA_OVERRIDE_GFX_VERSION viability
// fact is not probed in the detect path, so it serializes as unset rather than a
// fabricated false (D-08).
func hsaOverrideViable() Bool {
	return UnknownBool("HSA override viability not probed (unevaluable off-hardware)", "")
}
