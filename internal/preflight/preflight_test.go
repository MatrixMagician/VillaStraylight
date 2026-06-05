package preflight

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// healthyProfile is a HostProfile resembling a ready Strix Halo host: Vulkan ICD
// present, /dev/dri enumerated, a generous envelope and free memory.
func healthyProfile() detect.HostProfile {
	return detect.HostProfile{
		VulkanICDPath:       detect.KnownStr("/usr/share/vulkan/icd.d/radeon_icd.x86_64.json", "icd"),
		DRINodeCount:        detect.KnownInt(2, "/dev/dri"),
		MemAvailableBytes:   detect.KnownBytes(100*gib, "/proc/meminfo"),
		UsableEnvelopeBytes: detect.KnownBytes(62*gib, "mem_info_gtt_total"),
		KernelVersion:       detect.KnownStr("7.0.10-201.fc44.x86_64", "osrelease"),
	}
}

// resourceResult extracts the PRE-04 result from a Run/RunWithResources slice.
func resourceResult(t *testing.T, results []CheckResult) CheckResult {
	t.Helper()
	for _, r := range results {
		if r.ID == "PRE-04" {
			return r
		}
	}
	t.Fatal("no PRE-04 result returned")
	return CheckResult{}
}

// TestRunGatesOnFloorNotEnvelope is the WR-02/WR-03 regression: standalone
// `villa preflight` must gate free memory/disk on the smallest-installable-model
// FLOOR, not the full GTT envelope. A host with only ~40 GiB free RAM (far below a
// 62 GiB envelope, but ample for a real model) must PASS — the envelope-based gate
// would have hard-BLOCKED it.
func TestRunGatesOnFloorNotEnvelope(t *testing.T) {
	p := healthyProfile()                                            // 62 GiB envelope, 100 GiB free RAM
	p.MemAvailableBytes = detect.KnownBytes(40*gib, "/proc/meminfo") // below envelope, above floor
	got := resourceResult(t, Run(p))
	if got.Status != StatusPass {
		t.Fatalf("40 GiB-free host should PASS standalone preflight (floor, not envelope), got %v (%s)", got.Status, got.Detail)
	}
}

// TestRunBlocksBelowMinimalFloor asserts a host below even the minimal runnable
// memory floor still BLOCK-FAILs (the floor is a real gate, not a free pass).
func TestRunBlocksBelowMinimalFloor(t *testing.T) {
	p := healthyProfile()
	p.MemAvailableBytes = detect.KnownBytes(1*gib, "/proc/meminfo") // below the 4 GiB floor
	got := resourceResult(t, Run(p))
	if got.Tier != TierBlock || got.Status != StatusFail {
		t.Fatalf("host below the minimal memory floor should BLOCK/FAIL, got tier=%v status=%v", got.Tier, got.Status)
	}
}

func TestRunReturnsOneResultPerRequirement(t *testing.T) {
	results := Run(healthyProfile())

	// Run must cover PRE-01..04 (plus the WARN-tier floor checks). Assert each
	// core requirement id appears exactly once.
	want := map[string]bool{"PRE-01": false, "PRE-02": false, "PRE-03": false, "PRE-04": false}
	for _, r := range results {
		if _, ok := want[r.ID]; ok {
			if want[r.ID] {
				t.Errorf("duplicate result for %s", r.ID)
			}
			want[r.ID] = true
		}
	}
	for id, seen := range want {
		if !seen {
			t.Errorf("Run did not return a result for %s", id)
		}
	}
}

func TestRunEveryNonPassHasRemediation(t *testing.T) {
	// A deliberately-bad profile to force non-pass results across the board.
	bad := detect.HostProfile{
		VulkanICDPath:     detect.Str{Value: "", Known: true, Source: "icd"},
		DRINodeCount:      detect.Int{Value: 0, Known: true, Source: "/dev/dri"},
		MemAvailableBytes: detect.UnknownBytes("MemAvailable unreadable", ""),
		KernelVersion:     detect.KnownStr("6.0.0", "osrelease"),
	}
	for _, r := range Run(bad) {
		if r.Status != StatusPass && r.Remediation == "" {
			t.Errorf("%s is %v but has no remediation", r.ID, r.Status)
		}
	}
}

func TestRunIsDeterministicOrder(t *testing.T) {
	p := healthyProfile()
	first := Run(p)
	second := Run(p)
	if len(first) != len(second) {
		t.Fatalf("result count differs between runs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Errorf("order differs at %d: %s vs %s", i, first[i].ID, second[i].ID)
		}
	}
}

func TestTierAndStatusStringsStable(t *testing.T) {
	// Goldens depend on these strings — guard them.
	if TierBlock.String() != "BLOCK" || TierWarn.String() != "WARN" {
		t.Error("tier strings drifted")
	}
	if StatusPass.String() != "PASS" || StatusWarn.String() != "WARN" || StatusFail.String() != "FAIL" {
		t.Error("status strings drifted")
	}
}
