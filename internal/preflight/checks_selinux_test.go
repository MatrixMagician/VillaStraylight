package preflight

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// TestSELinuxContainerDevices covers the three outcomes of the SELinux
// container_use_devices boolean check (03-RESEARCH A5): on → PASS, off → WARN
// with the exact setsebool remediation, and a missing getsebool binary → WARN
// (typed-Unknown degradation), never a false PASS.
func TestSELinuxContainerDevices(t *testing.T) {
	const remediationCmd = "setsebool -P container_use_devices=true"

	tests := []struct {
		name       string
		out        string
		found      bool
		ok         bool
		wantStatus Status
		wantRemed  bool
	}{
		{
			name:       "on yields PASS",
			out:        "container_use_devices --> on",
			found:      true,
			ok:         true,
			wantStatus: StatusPass,
			wantRemed:  false,
		},
		{
			name:       "off yields WARN with remediation",
			out:        "container_use_devices --> off",
			found:      true,
			ok:         true,
			wantStatus: StatusWarn,
			wantRemed:  true,
		},
		{
			name:       "missing binary yields WARN never PASS",
			out:        "",
			found:      false,
			ok:         false,
			wantStatus: StatusWarn,
			wantRemed:  true,
		},
		{
			name:       "unparseable output yields WARN",
			out:        "garbage with no arrow",
			found:      true,
			ok:         true,
			wantStatus: StatusWarn,
			wantRemed:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := selinuxDeps{
				getsebool: func() (string, bool, bool) { return tt.out, tt.found, tt.ok },
			}
			r := checkSELinuxContainerDevices(d)
			if r.Status != tt.wantStatus {
				t.Errorf("status = %v, want %v (detail=%q)", r.Status, tt.wantStatus, r.Detail)
			}
			// The boolean check is BLOCK-tier (a wrong answer blocks rootless
			// /dev/dri passthrough at container start), per the plan.
			if r.Tier != TierBlock {
				t.Errorf("tier = %v, want BLOCK", r.Tier)
			}
			if tt.wantRemed && !strings.Contains(r.Remediation, remediationCmd) {
				t.Errorf("remediation %q must contain %q", r.Remediation, remediationCmd)
			}
			if tt.wantStatus == StatusPass && r.Status != StatusPass {
				t.Errorf("expected PASS, got %v", r.Status)
			}
		})
	}
}

// TestSELinuxCheckWiredIntoRunWithResources asserts the boolean check is part of
// the install gate set so RunWithResources surfaces it (the install verb gates on
// it via the BLOCK tier).
func TestSELinuxCheckWiredIntoRunWithResources(t *testing.T) {
	results := RunWithResources(detect.HostProfile{}, ResourceReq{MinDiskBytes: 1, MinMemBytes: 1, DataDir: t.TempDir()})
	found := false
	for _, r := range results {
		if r.ID == "PRE-05" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("RunWithResources must include the SELinux container_use_devices check (PRE-05)")
	}
}
