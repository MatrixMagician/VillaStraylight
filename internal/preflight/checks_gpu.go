package preflight

import (
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// checkVulkanIGPU is PRE-01 (BLOCK): the host must expose a working Vulkan
// backend — a present RADV ICD manifest AND at least one enumerated /dev/dri
// node — or llama.cpp silently falls back to CPU / fails to load.
//
// It does NOT re-probe the hardware: it reuses the facts already gathered into the
// HostProfile by internal/detect (the backend seam). That keeps all Vulkan/DRI
// knowledge inside the detect package and preserves the backend-neutrality seam —
// preflight only reasons over typed facts.
//
// Degradation (D-15): if the underlying facts are Unknown (vulkaninfo missing,
// /dev/dri unreadable — i.e. we could not EVALUATE the requirement), the BLOCK
// downgrades to WARN ("could not verify") rather than a false hard block. Only a
// confident known-absence (the ICD or DRI fact is Known and bad) is a true FAIL.
func checkVulkanIGPU(p detect.HostProfile) CheckResult {
	const (
		id   = "PRE-01"
		name = "Vulkan ICD + iGPU enumeration"
	)
	const remediation = "Install Mesa RADV (e.g. `sudo dnf install mesa-vulkan-drivers`) and confirm the iGPU exposes /dev/dri nodes (`ls /dev/dri`); verify with `vulkaninfo --summary`."

	icd := p.VulkanICDPath
	dri := p.DRINodeCount

	icdKnown := icd.Known
	driKnown := dri.Known

	// Unevaluable: neither fact is Known → we cannot verify; downgrade to WARN.
	if !icdKnown && !driKnown {
		return warn(id, name, TierBlock,
			"could not verify Vulkan ICD or /dev/dri enumeration",
			remediation,
			joinProvenance(icd.Source, dri.Source),
			joinRaw(icd.Raw, dri.Raw))
	}

	// A confident known-absence of either structural signal is a true BLOCK fail.
	if icdKnown && icd.Value == "" {
		return fail(id, name,
			"Vulkan RADV ICD manifest is absent",
			remediation, icd.Source, icd.Raw)
	}
	if driKnown && dri.Value == 0 {
		return fail(id, name,
			"no /dev/dri device nodes enumerated (iGPU not visible)",
			remediation, dri.Source, dri.Raw)
	}

	// One fact known-good but the other unevaluable → we cannot fully confirm;
	// surface the uncertainty as WARN rather than claim a clean pass (D-15).
	if !icdKnown || !driKnown {
		missing := "Vulkan ICD"
		if icdKnown {
			missing = "/dev/dri enumeration"
		}
		return warn(id, name, TierBlock,
			fmt.Sprintf("partially verified: %s could not be evaluated", missing),
			remediation,
			joinProvenance(icd.Source, dri.Source),
			joinRaw(icd.Raw, dri.Raw))
	}

	// Both structural signals present.
	return pass(id, name, TierBlock,
		fmt.Sprintf("RADV ICD present (%s); %d /dev/dri node(s)", icd.Value, dri.Value),
		joinProvenance(icd.Source, dri.Source))
}

// checkKernelFloor is a WARN-tier floor gate (D-02): a kernel below KernelFloor has
// a documented gfx1151 stability bug; between KernelFloor and KernelTested the host
// is above the hard floor but below the validated baseline. Kernel version is a
// FLOOR GATE, never an envelope multiplier (Pitfall 1). Unknown version → WARN.
func checkKernelFloor(p detect.HostProfile) CheckResult {
	const (
		id   = "PRE-06"
		name = "Kernel version floor"
	)
	f := Floors()
	remediation := fmt.Sprintf("Update to a kernel ≥ %s (validated baseline %s) for gfx1151 stability.", f.Kernel, f.KernelTested)

	kv := p.KernelVersion
	if !kv.Known || kv.Value == "" {
		return warn(id, name, TierWarn,
			"kernel version could not be determined",
			remediation, kv.Source, kv.Raw)
	}
	if compareVersions(kv.Value, f.Kernel) < 0 {
		return warn(id, name, TierWarn,
			fmt.Sprintf("kernel %s is below the %s gfx1151 stability floor", kv.Value, f.Kernel),
			remediation, kv.Source, kv.Raw)
	}
	if compareVersions(kv.Value, f.KernelTested) < 0 {
		return warn(id, name, TierWarn,
			fmt.Sprintf("kernel %s is above the floor but below the tested baseline %s", kv.Value, f.KernelTested),
			remediation, kv.Source, kv.Raw)
	}
	return pass(id, name, TierWarn,
		fmt.Sprintf("kernel %s meets the tested baseline %s", kv.Value, f.KernelTested),
		kv.Source)
}

// checkFirmwareFloor is a WARN-tier floor gate for linux-firmware (D-02). It can
// only act on what the HostProfile carries; Phase 1 does not probe a firmware date
// stamp, so this check degrades to an informational WARN noting the known-bad build
// to avoid (FirmwareDeny) rather than asserting a value it cannot read. The floor
// data is externalized so a later probe can wire a real comparison without
// reshaping the check.
func checkFirmwareFloor(p detect.HostProfile) CheckResult {
	const (
		id   = "PRE-07"
		name = "linux-firmware floor"
	)
	f := Floors()
	remediation := fmt.Sprintf("Ensure linux-firmware ≥ %s and NOT the known-bad %s build (breaks ROCm on Strix Halo).", f.Firmware, f.FirmwareDeny)

	// Phase 1 has no firmware-date fact on the profile; surface the floor as a
	// WARN-tier advisory (D-15 spirit: cannot evaluate → WARN, never block).
	return warn(id, name, TierWarn,
		fmt.Sprintf("firmware version not probed in Phase 1; ensure ≥ %s and avoid %s", f.Firmware, f.FirmwareDeny),
		remediation,
		"floors.go (FirmwareFloor/FirmwareDeny)",
		"")
}
