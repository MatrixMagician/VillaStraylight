package main

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// TestLiveSandboxGateResultsOffByDefault: with no persisted config (LoadVilla's
// absent-file default), workspace_agent is false, so the gate must emit nothing
// — mirroring liveMemoryGateResults' off-by-default byte-identical guarantee.
func TestLiveSandboxGateResultsOffByDefault(t *testing.T) {
	cfg, err := config.LoadVilla()
	if err != nil {
		t.Fatalf("LoadVilla: %v", err)
	}
	if cfg.WorkspaceAgent {
		t.Fatal("test assumes no persisted config.toml enables workspace_agent on this box")
	}
	got := liveSandboxGateResults(detect.HostProfile{})
	if got != nil {
		t.Fatalf("liveSandboxGateResults() = %+v, want nil when workspace_agent is off", got)
	}
}

// TestLiveSandboxDepsWiresEverySignal proves liveSandboxDeps leaves no Deps func
// field nil — a nil field would panic at call time rather than degrade to WARN.
func TestLiveSandboxDepsWiresEverySignal(t *testing.T) {
	d := liveSandboxDeps()
	if d.PodmanInfo == nil || d.RPMQuery == nil || d.StatKVM == nil || d.ProbeKrun == nil || d.HostKernel == nil {
		t.Fatalf("liveSandboxDeps() left a nil seam: %+v", d)
	}
}

// TestLiveSandboxRPMQueryUnknownPackage: querying a package name that does not
// exist reports found=true (rpm itself ran), ok=false (not installed) — never a
// crash — as long as rpm is on PATH (skipped otherwise, off-hardware CI box).
func TestLiveSandboxRPMQueryUnknownPackage(t *testing.T) {
	version, found, ok := liveSandboxRPMQuery("villa-definitely-not-a-real-package-xyz")
	if !found {
		t.Skip("rpm not on PATH in this environment")
	}
	if ok {
		t.Errorf("ok = true for a package that cannot exist (version=%q)", version)
	}
}

// TestLiveSandboxHostKernelReadable: uname -r always succeeds on Linux, so the
// seam must report ok=true with a non-empty kernel string.
func TestLiveSandboxHostKernelReadable(t *testing.T) {
	kernel, ok := liveSandboxHostKernel()
	if !ok || kernel == "" {
		t.Fatalf("liveSandboxHostKernel() = (%q, %v), want a non-empty kernel and ok=true", kernel, ok)
	}
}

// TestLiveSandboxProbeKrunEmptyImage: an unresolved (empty) image degrades to
// unevaluable rather than shelling out with an empty argument.
func TestLiveSandboxProbeKrunEmptyImage(t *testing.T) {
	kernel, ok := liveSandboxProbeKrun("")()
	if ok || kernel != "" {
		t.Fatalf("liveSandboxProbeKrun(\"\")() = (%q, %v), want (\"\", false)", kernel, ok)
	}
}

// TestSandboxGateResultsVarDefaultsToLive documents the pullFn convention
// (mirroring memoryGateResults): the package var must default to the live
// implementation so `villa preflight` actually runs it once wired in.
func TestSandboxGateResultsVarDefaultsToLive(t *testing.T) {
	if sandboxGateResults == nil {
		t.Fatal("sandboxGateResults must default to a non-nil function")
	}
}
