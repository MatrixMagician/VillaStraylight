package preflight

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

func TestCheckVulkanIGPU(t *testing.T) {
	tests := []struct {
		name       string
		icd        detect.Str
		driCount   detect.Int
		wantTier   Tier
		wantStatus Status
	}{
		{
			name:       "both present passes",
			icd:        detect.KnownStr("/usr/share/vulkan/icd.d/radeon_icd.x86_64.json", "icd"),
			driCount:   detect.KnownInt(2, "/dev/dri"),
			wantTier:   TierBlock,
			wantStatus: StatusPass,
		},
		{
			name:       "both unknown downgrades to WARN not FAIL (D-15)",
			icd:        detect.UnknownStr("RADV ICD manifest absent", ""),
			driCount:   detect.UnknownInt("/dev/dri empty or absent", ""),
			wantTier:   TierBlock,
			wantStatus: StatusWarn,
		},
		{
			name:       "known-absent ICD is a real BLOCK fail",
			icd:        detect.Str{Value: "", Known: true, Source: "icd"},
			driCount:   detect.KnownInt(2, "/dev/dri"),
			wantTier:   TierBlock,
			wantStatus: StatusFail,
		},
		{
			name:       "known zero dri nodes is a real BLOCK fail",
			icd:        detect.KnownStr("/usr/share/vulkan/icd.d/radeon_icd.x86_64.json", "icd"),
			driCount:   detect.Int{Value: 0, Known: true, Source: "/dev/dri"},
			wantTier:   TierBlock,
			wantStatus: StatusFail,
		},
		{
			name:       "one known-good one unevaluable downgrades to WARN",
			icd:        detect.KnownStr("/usr/share/vulkan/icd.d/radeon_icd.x86_64.json", "icd"),
			driCount:   detect.UnknownInt("/dev/dri unreadable", ""),
			wantTier:   TierBlock,
			wantStatus: StatusWarn,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := detect.HostProfile{VulkanICDPath: tc.icd, DRINodeCount: tc.driCount}
			got := checkVulkanIGPU(p)
			if got.Tier != tc.wantTier {
				t.Errorf("tier = %v, want %v", got.Tier, tc.wantTier)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("status = %v, want %v", got.Status, tc.wantStatus)
			}
			if got.Status != StatusPass && got.Remediation == "" {
				t.Errorf("non-pass result has empty remediation")
			}
		})
	}
}

func TestCheckKernelFloor(t *testing.T) {
	tests := []struct {
		name       string
		kernel     detect.Str
		wantStatus Status
	}{
		{"at tested baseline passes", detect.KnownStr("6.18.9", "osrelease"), StatusPass},
		{"above baseline passes", detect.KnownStr("7.0.10-201.fc44.x86_64", "osrelease"), StatusPass},
		{"between floor and baseline warns", detect.KnownStr("6.18.5", "osrelease"), StatusWarn},
		{"below floor warns", detect.KnownStr("6.17.0", "osrelease"), StatusWarn},
		{"unknown warns", detect.UnknownStr("osrelease unreadable", ""), StatusWarn},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := checkKernelFloor(detect.HostProfile{KernelVersion: tc.kernel})
			if got.Status != tc.wantStatus {
				t.Errorf("status = %v, want %v", got.Status, tc.wantStatus)
			}
			if got.Tier != TierWarn {
				t.Errorf("kernel floor must be WARN tier, got %v", got.Tier)
			}
		})
	}
}

func TestCheckFirmwareFloorIsWarnAdvisory(t *testing.T) {
	got := checkFirmwareFloor(detect.HostProfile{})
	if got.Tier != TierWarn {
		t.Errorf("firmware floor must be WARN tier, got %v", got.Tier)
	}
	if got.Status != StatusWarn {
		t.Errorf("firmware floor (unprobed in Phase 1) should WARN, got %v", got.Status)
	}
	if got.Remediation == "" {
		t.Errorf("firmware advisory must carry a remediation hint")
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"6.18.4", "6.18.4", 0},
		{"6.18.3", "6.18.4", -1},
		{"6.18.5", "6.18.4", 1},
		{"7.0.10-201.fc44.x86_64", "6.18.4", 1},
		{"6.18.9-300.fc44", "6.18.9", 0},
		{"6.18", "6.18.4", -1},
	}
	for _, tc := range tests {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
