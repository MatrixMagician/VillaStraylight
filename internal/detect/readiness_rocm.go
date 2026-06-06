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
// HostProfile facts (gfx-id, kernel, ROCm substrate presence, probed firmware date)
// and the resolved ROCm image string. Each field returns KnownBool only when its
// source fact is Known; otherwise UnknownBool (no-false-green, D-08).
func computeROCmReadiness(gfxID Str, kernel Str, rocmPresent Bool, firmwareDate Str, resolvedImage string) ROCmReadiness {
	return ROCmReadiness{
		HSAOverrideViable: hsaOverrideViable(gfxID, rocmPresent),
		FirmwareDateOK:    firmwareDateOK(firmwareDate),
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

// firmwareDateOK scores the probed linux-firmware date against the ROCm floor/denylist.
// It is KnownBool ONLY when the date was successfully probed (firmwareDateProbe
// returned a parseable YYYYMMDD KnownStr); off-hardware (rpm absent or an unparseable
// VERSION) the date is Unknown → UnknownBool (UNSET), never a fabricated false
// (no-false-green, D-08). The floor/denylist literals live behind the gpu_amd.go seam
// (firmwareDatePolicyOK), so this backend-neutral file carries no firmware literal.
func firmwareDateOK(date Str) Bool {
	if !date.Known {
		return UnknownBool("firmware date not probed (rpm absent or unparseable)", date.Raw)
	}
	return KnownBool(firmwareDatePolicyOK(date.Value), "firmware date vs floor/denylist")
}

// hsaOverrideViable reports whether the HSA_OVERRIDE_GFX_VERSION=11.5.1 workaround
// applies on this host. It is a PURE derivation from host facts — gfx id must be
// gfx1151 and the ROCm substrate (rocminfo) must be present — with NO I/O and, by
// design, NO read of os.Getenv("HSA_OVERRIDE_GFX_VERSION") (D-03 / Pitfall 4: the env
// var is not a viability signal and must not be inspected/logged).
//
// Known-ness is gated on gfxID.Known, NOT on rocmPresent: rocmPresent is always Known
// (rocminfo present/absent is a confident fact), so gating on it could never preserve
// the off-hardware UNSET. When the gfx id is unknown the viability is unevaluable →
// UnknownBool (UNSET), never a fabricated false (no-false-green, D-08 / Assumptions A1).
func hsaOverrideViable(gfxID Str, rocmPresent Bool) Bool {
	if !gfxID.Known {
		return UnknownBool("HSA override viability unevaluable (gfx id not enumerated)", gfxID.Raw)
	}
	return KnownBool(gfxID.Value == "gfx1151" && rocmPresent.Value, "gfx1151 + rocm substrate present (HSA 11.5.1 applies)")
}
