package preflight

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// floors.go externalizes the kernel / Mesa / firmware version thresholds the
// preflight WARN-tier checks compare against. They live here as DATA — not
// inlined into the check logic — because the upstream sources (CLAUDE.md, the
// Strix Halo references, ROCm docs) disagree on exact values and they drift over
// time. Keeping them as package-level data means a floor can be corrected in one
// place (and, later, lifted into an embedded JSON / external override) without
// reshaping any check (Assumption A1; RESEARCH anti-pattern "hard-coding floors").
//
// These are WARN-tier gates: a host below a tested baseline still boots and runs,
// it is just less validated — so the checks they back never BLOCK (D-02).

// KernelFloor is the minimum kernel version with the gfx1151 stability fix.
// Below this, the iGPU has a documented stability bug (CLAUDE.md version table).
const KernelFloor = "6.18.4"

// KernelTested is the kernel baseline the project has actually validated against.
// Between KernelFloor and KernelTested the host is "above the hard floor but below
// the tested baseline" — exactly the WARN case D-02 describes.
const KernelTested = "6.18.9"

// MesaFloor is the minimum Mesa/RADV version for reliable Vulkan on gfx1151. The
// value is intentionally conservative; Mesa version parsing is best-effort and a
// parse miss WARNs rather than blocks (D-15).
//
// TODO(phase-2): no check consumes MesaFloor yet (IN-02). Wiring one needs a
// design decision that must NOT be guessed here: detect.MesaVersion is currently
// parsed from vulkaninfo's `driverVersion` line (the Vulkan DRIVER version), which
// is a DIFFERENT numbering scheme from a Mesa RELEASE number like "25.0.0". A
// future checkMesaFloor must compare like-for-like — source the Mesa release from
// the `driverInfo = Mesa X.Y.Z` line (scoped to the real GPU block) and compare
// that to MesaFloor, OR redefine MesaFloor as a driverVersion floor. Until that
// decision is made, MesaFloor/Floor.Mesa remain intentionally unwired rather than
// risk a cross-namespace comparison that silently mis-gates.
const MesaFloor = "25.0.0"

// FirmwareFloor is the minimum linux-firmware date stamp (YYYYMMDD) recommended
// for Strix Halo. Below it, ROCm reliability degrades (CLAUDE.md version table).
const FirmwareFloor = "20260110"

// FirmwareDeny is a specific linux-firmware build documented to BREAK ROCm on
// Strix Halo (instability/crashes). Its presence is an explicit WARN regardless of
// the date comparison — it is a known-bad point release, not just "too old"
// (CLAUDE.md "What NOT to Use": linux-firmware-20251125).
const FirmwareDeny = "20251125"

// Floor bundles the version thresholds so a future loader can replace them
// wholesale from embedded/external data without touching check logic. The
// constants above are the AUTHORING REFERENCE for the embedded policy values;
// Floors() returns them as a single value, now sourced from rocm-policy.json
// (the migration is a deliberate behavior no-op — the loaded values are
// byte-identical to the constants, asserted by policy_test.go).
type Floor struct {
	// Kernel is the minimum kernel with the gfx1151 stability fix.
	Kernel string
	// KernelTested is the validated kernel baseline.
	KernelTested string
	// Mesa is the minimum Mesa/RADV version for reliable Vulkan.
	Mesa string
	// Firmware is the minimum linux-firmware date stamp.
	Firmware string
	// FirmwareDeny is a specific known-bad linux-firmware build.
	FirmwareDeny string
}

// rocmPolicyBytes is the COMPILED-IN ROCm/version policy. Because it is embedded
// at build time it is NOT an external/runtime input — a malformed policy is a
// build-time error caught by loadROCmPolicy's panic, never a runtime parse of
// attacker-controlled data (Security V5 / T-07-03). It carries the v1.0 version
// floors (re-sourced into Floors() as a no-op migration, D-04/D-05) plus the new
// ROCm denylists and required HSA override the RunROCm checks gate on.
//
//go:embed rocm-policy.json
var rocmPolicyBytes []byte

// ROCmPolicy is the decoded shape of rocm-policy.json. It bundles the migrated
// v1.0 version floors with the new ROCm-specific policy data (firmware/image
// denylists, the required HSA override value) so a floor or denylist entry can be
// corrected in one place — the embedded JSON — without reshaping any check (D-04).
type ROCmPolicy struct {
	// KernelFloor is the minimum kernel with the gfx1151 stability fix (6.18.4).
	KernelFloor string `json:"kernelFloor"`
	// KernelTested is the validated kernel baseline (6.18.9).
	KernelTested string `json:"kernelTested"`
	// MesaFloor is the minimum Mesa/RADV version (25.0.0; migrated but UNWIRED).
	MesaFloor string `json:"mesaFloor"`
	// FirmwareFloor is the minimum linux-firmware date stamp (20260110).
	FirmwareFloor string `json:"firmwareFloor"`
	// FirmwareDeny lists specific known-bad linux-firmware builds (["20251125"]).
	FirmwareDeny []string `json:"firmwareDeny"`
	// ImageDeny lists ROCm image tags that reintroduce the 64 GB allocation cap
	// (the denied nightly tag — the literal lives in rocm-policy.json, not inlined
	// here, so the inference seam grep-gate stays green).
	ImageDeny []string `json:"imageDeny"`
	// RequiredHSAOverride is the HSA_OVERRIDE_GFX_VERSION value ROCm needs on
	// gfx1151 ("11.5.1").
	RequiredHSAOverride string `json:"requiredHSAOverride"`
}

// loadROCmPolicy decodes the embedded rocm-policy.json. It PANICS on a malformed
// embed: that is a build-time programming error (the bytes are compiled in, never
// runtime input — T-07-03), so failing loud at startup is correct and there is no
// attacker-controlled path to this panic.
func loadROCmPolicy() ROCmPolicy {
	var p ROCmPolicy
	if err := json.Unmarshal(rocmPolicyBytes, &p); err != nil {
		panic(fmt.Sprintf("preflight: malformed embedded rocm-policy.json: %v", err))
	}
	return p
}

// Floors returns the current version-floor data, sourced from the embedded
// rocm-policy.json (D-04/D-05). The returned values are byte-identical to the
// KernelFloor/KernelTested/MesaFloor/FirmwareFloor/FirmwareDeny constants — the
// migration is a deliberate behavior no-op. FirmwareDeny is collapsed to the
// FIRST denylist entry to preserve the existing scalar Floor.FirmwareDeny shape
// the v1.0 checks/goldens already consume; the full denylist is available via
// loadROCmPolicy for the ROCm checks.
func Floors() Floor {
	p := loadROCmPolicy()
	deny := ""
	if len(p.FirmwareDeny) > 0 {
		deny = p.FirmwareDeny[0]
	}
	return Floor{
		Kernel:       p.KernelFloor,
		KernelTested: p.KernelTested,
		Mesa:         p.MesaFloor,
		Firmware:     p.FirmwareFloor,
		FirmwareDeny: deny,
	}
}

// compareVersions compares two dotted numeric version strings (e.g. "6.18.4").
// It returns -1 if a < b, 0 if equal, +1 if a > b. Non-numeric or extra segments
// are tolerated: leading numeric runs are compared and anything unparseable in a
// segment is treated as 0 so a malformed version never panics — it just sorts low,
// which (for a floor gate) errs toward WARNing rather than silently passing.
func compareVersions(a, b string) int {
	as := splitVersion(a)
	bs := splitVersion(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

// splitVersion turns "6.18.9-300.fc44.x86_64" into [6 18 9], stopping each
// segment at the first non-digit so distro suffixes don't break the compare.
func splitVersion(v string) []int {
	var out []int
	cur := 0
	inNum := false
	for i := 0; i < len(v); i++ {
		ch := v[i]
		if ch >= '0' && ch <= '9' {
			cur = cur*10 + int(ch-'0')
			inNum = true
			continue
		}
		if ch == '.' {
			out = append(out, cur)
			cur = 0
			inNum = false
			continue
		}
		// Any other character (e.g. '-' in "6.18.9-300") ends the meaningful
		// portion of a dotted version for floor comparison.
		if inNum {
			out = append(out, cur)
			cur = 0
			inNum = false
		}
		// Skip to the next '.' boundary; subsequent non-numeric junk is ignored.
		for i+1 < len(v) && v[i+1] != '.' {
			i++
		}
	}
	if inNum {
		out = append(out, cur)
	}
	return out
}
