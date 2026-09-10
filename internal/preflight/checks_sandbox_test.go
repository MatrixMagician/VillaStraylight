package preflight

import (
	"strings"
	"testing"
)

// fakeSandboxDeps returns a SandboxDeps whose every signal is healthy: krun listed
// as an OCI runtime, all three packages installed, /dev/kvm accessible, and the
// functional probe reporting a kernel that differs from the host's.
func fakeSandboxDeps() SandboxDeps {
	versions := map[string]string{
		"libkrun":   "1.9.1-1.fc44",
		"libkrunfw": "4.10.0-1.fc44",
		"crun-krun": "1.16-1.fc44",
	}
	return SandboxDeps{
		PodmanInfo: func() (runtimes []string, found, ok bool) {
			return []string{"crun", "krun"}, true, true
		},
		RPMQuery: func(pkg string) (version string, found, ok bool) {
			v, present := versions[pkg]
			return v, true, present
		},
		StatKVM: func() (readable, writable, ok bool) { return true, true, true },
		ProbeKrun: func() (kernel string, ok bool) {
			return "6.12.0-krun", true
		},
		HostKernel: func() (kernel string, ok bool) {
			return "6.18.9-200.fc44.x86_64", true
		},
	}
}

// TestSandboxRuntimeAllSignalsHealthy: every signal present yields PASS.
func TestSandboxRuntimeAllSignalsHealthy(t *testing.T) {
	r := checkSandboxRuntime(fakeSandboxDeps())
	if r.Status != StatusPass {
		t.Fatalf("status = %v, want PASS (detail=%q)", r.Status, r.Detail)
	}
	if r.ID != "PRE-09" {
		t.Errorf("ID = %q, want PRE-09", r.ID)
	}
	if r.Tier != TierBlock {
		t.Errorf("tier = %v, want BLOCK", r.Tier)
	}
}

// TestSandboxRuntimePodmanUnreachable covers both flavors of "podman cannot
// answer" (binary absent, and `podman info` failing/unparseable): typed-Unknown,
// never a false BLOCK.
func TestSandboxRuntimePodmanUnreachable(t *testing.T) {
	tests := []struct {
		name  string
		found bool
		ok    bool
	}{
		{"podman not on PATH", false, false},
		{"podman info failed", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := fakeSandboxDeps()
			d.PodmanInfo = func() ([]string, bool, bool) { return nil, tt.found, tt.ok }
			r := checkSandboxRuntime(d)
			if r.Status != StatusWarn {
				t.Fatalf("status = %v, want WARN (detail=%q)", r.Status, r.Detail)
			}
			if r.Remediation == "" {
				t.Error("WARN must carry a remediation")
			}
		})
	}
}

// TestSandboxRuntimeKrunMissing: krun absent from podman's OCI runtimes is a
// confident BLOCK, and the remediation names the required packages.
func TestSandboxRuntimeKrunMissing(t *testing.T) {
	d := fakeSandboxDeps()
	d.PodmanInfo = func() ([]string, bool, bool) { return []string{"crun"}, true, true }
	r := checkSandboxRuntime(d)
	if r.Status != StatusFail {
		t.Fatalf("status = %v, want FAIL (detail=%q)", r.Status, r.Detail)
	}
	for _, pkg := range []string{"libkrun", "libkrunfw", "crun-krun"} {
		if !strings.Contains(r.Remediation, pkg) {
			t.Errorf("remediation %q must name package %q", r.Remediation, pkg)
		}
	}
}

// TestSandboxRuntimeMissingPackage: a required RPM confirmed absent (even though
// podman reports krun) is a confident BLOCK.
func TestSandboxRuntimeMissingPackage(t *testing.T) {
	d := fakeSandboxDeps()
	d.RPMQuery = func(pkg string) (string, bool, bool) {
		if pkg == "libkrunfw" {
			return "", true, false
		}
		return "1.0-1", true, true
	}
	r := checkSandboxRuntime(d)
	if r.Status != StatusFail {
		t.Fatalf("status = %v, want FAIL (detail=%q)", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "libkrunfw") {
		t.Errorf("detail %q must name the missing package", r.Detail)
	}
}

// TestSandboxRuntimeRPMUnreachable: rpm binary absent degrades to WARN, not FAIL.
func TestSandboxRuntimeRPMUnreachable(t *testing.T) {
	d := fakeSandboxDeps()
	d.RPMQuery = func(string) (string, bool, bool) { return "", false, false }
	r := checkSandboxRuntime(d)
	if r.Status != StatusWarn {
		t.Fatalf("status = %v, want WARN (detail=%q)", r.Status, r.Detail)
	}
}

// TestSandboxRuntimeKVMNotWritable: /dev/kvm present but not read/write by this
// user is a confident BLOCK with a group/udev remediation.
func TestSandboxRuntimeKVMNotWritable(t *testing.T) {
	tests := []struct {
		name      string
		readable  bool
		writable  bool
		remedWant string
	}{
		{"not writable", true, false, "kvm"},
		{"not readable", false, true, "kvm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := fakeSandboxDeps()
			d.StatKVM = func() (bool, bool, bool) { return tt.readable, tt.writable, true }
			r := checkSandboxRuntime(d)
			if r.Status != StatusFail {
				t.Fatalf("status = %v, want FAIL (detail=%q)", r.Status, r.Detail)
			}
			if !strings.Contains(strings.ToLower(r.Remediation), tt.remedWant) {
				t.Errorf("remediation %q must mention %q", r.Remediation, tt.remedWant)
			}
		})
	}
}

// TestSandboxRuntimeKVMUnevaluable: a failed stat degrades to WARN.
func TestSandboxRuntimeKVMUnevaluable(t *testing.T) {
	d := fakeSandboxDeps()
	d.StatKVM = func() (bool, bool, bool) { return false, false, false }
	r := checkSandboxRuntime(d)
	if r.Status != StatusWarn {
		t.Fatalf("status = %v, want WARN (detail=%q)", r.Status, r.Detail)
	}
}

// TestSandboxRuntimeProbeMatchesHostKernel: the functional probe reporting the
// SAME kernel as the host means the task never actually ran in a microVM — a
// confident BLOCK, never a false PASS.
func TestSandboxRuntimeProbeMatchesHostKernel(t *testing.T) {
	d := fakeSandboxDeps()
	d.ProbeKrun = func() (string, bool) { return "6.18.9-200.fc44.x86_64", true }
	r := checkSandboxRuntime(d)
	if r.Status != StatusFail {
		t.Fatalf("status = %v, want FAIL (detail=%q)", r.Status, r.Detail)
	}
}

// TestSandboxRuntimeProbeUnevaluable: the probe (or the host-kernel read) failing
// to run degrades to WARN rather than a fabricated BLOCK.
func TestSandboxRuntimeProbeUnevaluable(t *testing.T) {
	tests := []struct {
		name    string
		probeOK bool
		hostOK  bool
	}{
		{"probe failed to run", false, true},
		{"host kernel unreadable", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := fakeSandboxDeps()
			d.ProbeKrun = func() (string, bool) { return "", tt.probeOK }
			d.HostKernel = func() (string, bool) { return "", tt.hostOK }
			r := checkSandboxRuntime(d)
			if r.Status != StatusWarn {
				t.Fatalf("status = %v, want WARN (detail=%q)", r.Status, r.Detail)
			}
		})
	}
}

// TestRunSandboxReturnsOneCheck: RunSandbox wraps the single PRE-09 check.
func TestRunSandboxReturnsOneCheck(t *testing.T) {
	results := RunSandbox(fakeSandboxDeps())
	if len(results) != 1 || results[0].ID != "PRE-09" {
		t.Fatalf("RunSandbox() = %+v, want exactly one PRE-09 result", results)
	}
}

// TestSandboxRemediationPresentOnNonPass mirrors the package-wide invariant: every
// non-PASS PRE-09 result must carry a Remediation.
func TestSandboxRemediationPresentOnNonPass(t *testing.T) {
	d := fakeSandboxDeps()
	d.PodmanInfo = func() ([]string, bool, bool) { return []string{"crun"}, true, true }
	r := checkSandboxRuntime(d)
	if r.Status != StatusPass && r.Remediation == "" {
		t.Errorf("non-PASS result %+v must carry a Remediation", r)
	}
}
