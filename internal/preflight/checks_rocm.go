package preflight

import (
	"fmt"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// checks_rocm.go is the REFUSE-WITH-REMEDIATION ROCm bring-up gate (PRE-06). It is
// the host-fitness verdict the Phase 8 `backend set rocm` verb consumes, and is
// demoable now off-hardware via `villa preflight --backend rocm`.
//
// It reuses the existing CheckResult / Tier / Status vocabulary (D-01) — there is
// NO new verdict type. Every ROCm signal is a TierBlock check, but it FAILs ONLY on
// a POSITIVELY-detected known-bad state (D-02/D-15): a confidently-wrong fact. Any
// unevaluable signal (a fact is Unknown, a thing is not probed off-hardware)
// degrades to WARN ("could not verify"), NEVER StatusFail — biased not to
// over-block a genuinely-working host (T-07-02). Off-hardware almost every signal
// is Unknown, so the whole verdict is WARN/PASS and exit 2, never a false exit 1.
//
// The CheckResult.IDs use a NON-COLLIDING `ROCM-PRE-*` scheme so they never clash
// with checks_gpu.go's PRE-06/PRE-07 (which are the kernel/firmware CHECK ids,
// distinct from the PRE-06 REQUIREMENT this file maps to).

// ROCm CheckResult ids. Distinct namespace from the v1.0 PRE-0N check ids.
const (
	idROCmGfx      = "ROCM-PRE-gfx"
	idROCmKernel   = "ROCM-PRE-kernel"
	idROCmFirmware = "ROCM-PRE-firmware"
	idROCmHSA      = "ROCM-PRE-hsa"
	idROCmImage    = "ROCM-PRE-image"
)

// RunROCm executes the ROCm bring-up gate against a HostProfile using the embedded
// policy. It mirrors Run→RunWithResources so tests can inject a synthetic policy
// via RunROCmWithPolicy. Pure: no os.Exit, no printing.
func RunROCm(p detect.HostProfile) []CheckResult {
	return RunROCmWithPolicy(p, loadROCmPolicy(), "")
}

// RunROCmWithPolicy is RunROCm with an explicitly-supplied policy and the
// requested/resolved ROCm image string (empty off-hardware / standalone), so tests
// drive each signal's known-bad and unevaluable branch deterministically.
//
// Two ROCm signals — the linux-firmware date stamp and the HSA_OVERRIDE_GFX_VERSION
// runtime env — are NOT probed HostProfile fields in v1.0 (Phase 1 never gathered
// them), so RunROCmWithPolicy sources them as typed-Unknown here; off-hardware they
// always degrade to WARN. The per-check functions take the typed value directly so
// the table test can inject a Known known-bad to exercise the FAIL branch and a
// future probe can wire a real value without reshaping the check.
//
// Ordering is stable (gfx, kernel, firmware, hsa, image) for deterministic tables.
func RunROCmWithPolicy(p detect.HostProfile, pol ROCmPolicy, requestedImage string) []CheckResult {
	// Not probed in v1.0 → typed-Unknown → WARN off-hardware (D-15).
	firmware := detect.UnknownStr("firmware date not probed in Phase 1", "")
	hsa := detect.UnknownStr("HSA_OVERRIDE_GFX_VERSION not probed in Phase 1", "")
	return []CheckResult{
		checkROCmGfx(p),
		checkROCmKernel(p, pol),
		checkROCmFirmware(firmware, pol),
		checkROCmHSA(hsa, pol),
		checkROCmImage(requestedImage, pol),
	}
}

// checkROCmGfx FAILs only when rocminfo confidently reports a non-gfx1151 device
// (ROCm present AND the gfx id is Known AND != gfx1151). An unknown gfx id (the
// usual off-hardware case, rocminfo absent) degrades to WARN.
func checkROCmGfx(p detect.HostProfile) CheckResult {
	const name = "ROCm iGPU is gfx1151"
	const remediation = "ROCm on Strix Halo targets gfx1151 (RDNA 3.5); a different device is unsupported by this stack — use the Vulkan RADV backend instead."

	gfx := p.IGPUGfxID
	if !gfx.Known || gfx.Value == "" {
		return warn(idROCmGfx, name, TierBlock,
			"could not verify the iGPU gfx id (rocminfo absent or unparseable)",
			remediation, gfx.Source, gfx.Raw)
	}
	if gfx.Value != "gfx1151" {
		return fail(idROCmGfx, name,
			fmt.Sprintf("iGPU reports %s, not the required gfx1151", gfx.Value),
			remediation, gfx.Source, gfx.Raw)
	}
	return pass(idROCmGfx, name, TierBlock,
		fmt.Sprintf("iGPU is %s", gfx.Value), gfx.Source)
}

// checkROCmKernel FAILs only when the kernel version is Known AND below the policy
// floor (the gfx1151 stability bug is a confirmed bring-up hazard for ROCm). An
// unknown kernel version degrades to WARN. compareVersions is reused.
func checkROCmKernel(p detect.HostProfile, pol ROCmPolicy) CheckResult {
	const name = "ROCm kernel floor"
	remediation := fmt.Sprintf("Update to a kernel ≥ %s (validated baseline %s) for gfx1151 stability before enabling ROCm.", pol.KernelFloor, pol.KernelTested)

	kv := p.KernelVersion
	if !kv.Known || kv.Value == "" {
		return warn(idROCmKernel, name, TierBlock,
			"kernel version could not be determined",
			remediation, kv.Source, kv.Raw)
	}
	if compareVersions(kv.Value, pol.KernelFloor) < 0 {
		return fail(idROCmKernel, name,
			fmt.Sprintf("kernel %s is below the %s gfx1151 stability floor — ROCm bring-up refused", kv.Value, pol.KernelFloor),
			remediation, kv.Source, kv.Raw)
	}
	return pass(idROCmKernel, name, TierBlock,
		fmt.Sprintf("kernel %s meets the %s floor", kv.Value, pol.KernelFloor), kv.Source)
}

// checkROCmFirmware FAILs only when a firmware date stamp is Known AND matches a
// policy denylist entry (the 20251125 build documented to break ROCm). Phase 1
// does not probe a firmware date, so off-hardware this degrades to a WARN advisory
// (mirroring checkFirmwareFloor's shape) rather than asserting a value it can't
// read.
func checkROCmFirmware(fw detect.Str, pol ROCmPolicy) CheckResult {
	const name = "ROCm linux-firmware not denied"
	floor := pol.FirmwareFloor
	denied := strings.Join(pol.FirmwareDeny, ", ")
	remediation := fmt.Sprintf("Install linux-firmware ≥ %s and NOT the known-bad build(s) %s (breaks ROCm on Strix Halo).", floor, denied)

	// The firmware date is not a probed HostProfile field in v1.0; off-hardware it
	// arrives typed-Unknown, so degrade to a WARN advisory (D-15) naming the denied
	// build rather than asserting a value we cannot read.
	if !fw.Known || fw.Value == "" {
		return warn(idROCmFirmware, name, TierBlock,
			fmt.Sprintf("firmware version not probed; ensure ≥ %s and avoid %s", floor, denied),
			remediation, "rocm-policy.json (firmwareDeny)", "")
	}
	for _, bad := range pol.FirmwareDeny {
		if fw.Value == bad {
			return fail(idROCmFirmware, name,
				fmt.Sprintf("linux-firmware-%s is denied (breaks ROCm on Strix Halo) — ROCm bring-up refused", fw.Value),
				remediation, fw.Source, fw.Raw)
		}
	}
	return pass(idROCmFirmware, name, TierBlock,
		fmt.Sprintf("firmware %s is not on the denylist", fw.Value), fw.Source)
}

// checkROCmHSA FAILs only when the HSA_OVERRIDE_GFX_VERSION is Known to be absent
// or wrong vs the policy's required value (ROCm on gfx1151 needs it set). An
// unknown override (unevaluable off-hardware) degrades to WARN.
func checkROCmHSA(hsa detect.Str, pol ROCmPolicy) CheckResult {
	const name = "ROCm HSA override set"
	remediation := fmt.Sprintf("Set HSA_OVERRIDE_GFX_VERSION=%s for the ROCm runtime on gfx1151.", pol.RequiredHSAOverride)

	if !hsa.Known {
		return warn(idROCmHSA, name, TierBlock,
			fmt.Sprintf("could not verify HSA_OVERRIDE_GFX_VERSION (expected %s)", pol.RequiredHSAOverride),
			remediation, hsa.Source, hsa.Raw)
	}
	if hsa.Value != pol.RequiredHSAOverride {
		detail := fmt.Sprintf("HSA_OVERRIDE_GFX_VERSION is %q, not the required %s — ROCm bring-up refused", hsa.Value, pol.RequiredHSAOverride)
		if hsa.Value == "" {
			detail = fmt.Sprintf("HSA_OVERRIDE_GFX_VERSION is unset; ROCm on gfx1151 requires %s — ROCm bring-up refused", pol.RequiredHSAOverride)
		}
		return fail(idROCmHSA, name, detail, remediation, hsa.Source, hsa.Raw)
	}
	return pass(idROCmHSA, name, TierBlock,
		fmt.Sprintf("HSA_OVERRIDE_GFX_VERSION=%s", hsa.Value), hsa.Source)
}

// checkROCmImage is CONFIG/REQUEST-driven, not a host probe (Pitfall 5): it FAILs
// only when the requested/resolved image string is Known (non-empty) AND matches a
// policy image-denylist entry (a nightly tag reintroducing the 64 GB allocation
// cap, T-07-01). An empty request (standalone / off-hardware, nothing requested)
// degrades to WARN. The in-tree digest-pinned stable image passes.
//
// The denied tag literals are POLICY DATA in rocm-policy.json (pol.ImageDeny), not
// inlined here — keeping the backend image-tag literals out of this .go file so the
// inference seam grep-gate (TestSeamGrepGate) stays green.
func checkROCmImage(requestedImage string, pol ROCmPolicy) CheckResult {
	const name = "ROCm image not denied"
	denied := strings.Join(pol.ImageDeny, ", ")
	remediation := fmt.Sprintf("Use the digest-pinned stable ROCm image; avoid the denied build(s) %s (caps allocation at 64 GB → large models fail to load).", denied)

	if strings.TrimSpace(requestedImage) == "" {
		return warn(idROCmImage, name, TierBlock,
			fmt.Sprintf("no ROCm image requested; the standalone gate cannot evaluate the image (avoid %s)", denied),
			remediation, "rocm-policy.json (imageDeny)", "")
	}
	for _, bad := range pol.ImageDeny {
		if strings.Contains(requestedImage, bad) {
			return fail(idROCmImage, name,
				fmt.Sprintf("requested image %q matches the denied %s (caps allocation at 64 GB) — ROCm bring-up refused", requestedImage, bad),
				remediation, "requested image", requestedImage)
		}
	}
	return pass(idROCmImage, name, TierBlock,
		fmt.Sprintf("requested image %q is not on the denylist", requestedImage), "requested image")
}
