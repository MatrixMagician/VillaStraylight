package preflight

import (
	"fmt"
	"slices"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// checkVulkanIGPU is PRE-01 (BLOCK): the host must expose a working Vulkan
// backend — a present RADV ICD manifest AND at least one enumerated /dev/dri
// node — or llama.cpp silently falls back to CPU / fails to load.
//
// It does NOT re-probe the hardware: it reuses the facts already gathered into the
// HostProfile by internal/detect (the backend seam). That keeps all Vulkan/DRI
// knowledge inside the detect package and preserves the backend-neutrality seam
// preflight only reasons over typed facts.
//
// Degradation: if the underlying facts are Unknown (vulkaninfo missing,
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
	// surface the uncertainty as WARN rather than claim a clean pass.
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

// checkKernelFloor is a WARN-tier floor gate: a kernel below KernelFloor has
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

// computeDeviceGroupsRemediation is the default PRE-08 remediation: the invoking
// user is missing the group membership the render/video device nodes require.
const computeDeviceGroupsRemediation = "add your user to the render and video groups (`sudo usermod -aG render,video $USER`), then log out and log back in — the new groups apply only to a new login; confirm with `id -nG`"

// computeDeviceDriverRemediation is the PRE-08 remediation for a confidently
// ABSENT /dev/kfd (the amdgpu module never created the node) — pointing at group
// membership here would send the user chasing a fix that cannot help.
const computeDeviceDriverRemediation = "``/dev/kfd`` is absent: the amdgpu kernel module is not loaded or KFD is disabled; check `lsmod | grep amdgpu` and `dmesg | grep -i kfd`"

// computeDeviceRemediation picks the driver remediation when source names a
// confidently-ABSENT device (the module never created the node — no group fix
// can help), else the general render/video groups remediation.
func computeDeviceRemediation(source string) string {
	if strings.HasSuffix(source, detect.AccessAbsent) {
		return computeDeviceDriverRemediation
	}
	return computeDeviceGroupsRemediation
}

// checkComputeDeviceAccess is PRE-08 (BLOCK): the invoking user must be able to
// open /dev/kfd (ROCm-family backends only) and at least one DRM render node for
// read+write, or a container that will never see the iGPU gets installed and the
// failure surfaces later as a unit that will not start. It reuses the detect
// probes (KFDAccess/RenderNodeAccess) rather than re-checking the filesystem
// itself — preflight stays a pure function of the HostProfile.
//
// The render node is required on EVERY backend (Vulkan and ROCm both need it); a
// confident known-absence there is always a real BLOCK fail. /dev/kfd is
// required only when requireKFD is true (ROCm-family bring-up): on the Vulkan
// fallback a denied/absent /dev/kfd is informational only, noted in the Detail.
//
// Degradation mirrors PRE-01: neither fact evaluable → WARN "could not verify";
// one evaluable and good, the other unevaluable → WARN "partially verified".
func checkComputeDeviceAccess(p detect.HostProfile, requireKFD bool) CheckResult {
	const (
		id   = "PRE-08"
		name = "compute device access"
	)

	kfd := p.KFDAccess
	render := p.RenderNodeAccess

	// Unevaluable: neither fact is Known → we cannot verify at all; downgrade to WARN.
	if !kfd.Known && !render.Known {
		return warn(id, name, TierBlock,
			"could not verify /dev/kfd or render node access",
			computeDeviceGroupsRemediation,
			joinProvenance(kfd.Source, render.Source),
			joinRaw(kfd.Raw, render.Raw))
	}

	// A confident known-false render node is always a real BLOCK fail: every
	// backend needs at least one accessible render node.
	if render.Known && !render.Value {
		return fail(id, name,
			"render node is not accessible as this user",
			computeDeviceRemediation(render.Source),
			render.Source, render.Raw)
	}

	// A confident known-false /dev/kfd is a BLOCK fail only when this backend
	// requires it (ROCm-family bring-up); Vulkan never touches /dev/kfd.
	if requireKFD && kfd.Known && !kfd.Value {
		return fail(id, name,
			"/dev/kfd is not accessible as this user",
			computeDeviceRemediation(kfd.Source),
			kfd.Source, kfd.Raw)
	}

	// One fact known, the other unevaluable → we cannot fully confirm; surface
	// the uncertainty as WARN rather than claim a clean pass.
	if !kfd.Known || !render.Known {
		missing := "/dev/kfd access"
		if kfd.Known {
			missing = "render node access"
		}
		return warn(id, name, TierBlock,
			fmt.Sprintf("partially verified: %s could not be evaluated", missing),
			computeDeviceGroupsRemediation,
			joinProvenance(kfd.Source, render.Source),
			joinRaw(kfd.Raw, render.Raw))
	}

	// Both facts are Known at this point. On a backend that does not require
	// /dev/kfd (Vulkan), a confidently-false KFD is informational only — the
	// render node alone is sufficient, so note KFD's state in the detail rather
	// than blocking on a device this backend never opens.
	if !requireKFD && !kfd.Value {
		return pass(id, name, TierBlock,
			fmt.Sprintf("%s is readable and writable; kfd: %s, informational on this backend", render.Source, kfd.Source),
			joinProvenance(kfd.Source, render.Source))
	}

	return pass(id, name, TierBlock,
		fmt.Sprintf("%s and %s are readable and writable", kfd.Source, render.Source),
		joinProvenance(kfd.Source, render.Source))
}

// checkFirmwareFloor is PRE-07, a WARN-tier floor gate for linux-firmware. It
// compares the probed HostProfile.FirmwareDate against the policy floor and
// denylist. An Unknown or non-YYYYMMDD value WARNs rather than passing: a probe
// that could not read the date must never read as a clean bill. The ROCm
// bring-up gate (checkROCmFirmware) is separate and stricter; this one never
// blocks.
func checkFirmwareFloor(p detect.HostProfile) CheckResult {
	const (
		id   = "PRE-07"
		name = "linux-firmware floor"
	)
	pol := loadROCmPolicy()
	denied := strings.Join(pol.FirmwareDeny, ", ")
	remediation := fmt.Sprintf("Ensure linux-firmware ≥ %s and NOT the known-bad %s build (breaks ROCm on Strix Halo).", pol.FirmwareFloor, denied)

	fw := p.FirmwareDate
	if !fw.Known || fw.Value == "" {
		return warn(id, name, TierWarn,
			fmt.Sprintf("firmware version not probed; ensure ≥ %s and avoid %s", pol.FirmwareFloor, denied),
			remediation, "rocm-policy.json (firmwareFloor/firmwareDeny)", fw.Raw)
	}
	if slices.Contains(pol.FirmwareDeny, fw.Value) {
		return warn(id, name, TierWarn,
			fmt.Sprintf("linux-firmware-%s is a known-bad build (breaks ROCm on Strix Halo)", fw.Value),
			remediation, fw.Source, fw.Raw)
	}
	if !isFirmwareDate(fw.Value) {
		return warn(id, name, TierWarn,
			fmt.Sprintf("linux-firmware %q is not a YYYYMMDD stamp and cannot be compared with the %s floor", fw.Value, pol.FirmwareFloor),
			remediation, fw.Source, fw.Raw)
	}
	if compareVersions(fw.Value, pol.FirmwareFloor) < 0 {
		return warn(id, name, TierWarn,
			fmt.Sprintf("linux-firmware %s is below the %s floor", fw.Value, pol.FirmwareFloor),
			remediation, fw.Source, fw.Raw)
	}
	return pass(id, name, TierWarn,
		fmt.Sprintf("linux-firmware %s meets the %s floor", fw.Value, pol.FirmwareFloor),
		fw.Source)
}
