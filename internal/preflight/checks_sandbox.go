package preflight

import (
	"fmt"
	"slices"
	"strings"
)

// checks_sandbox.go adds PRE-09: the workspace agent's sandbox-runtime gate
// (spec v1.11 §10). A task runs inside a libkrun microVM, not a plain rootless
// container, so the gate must confirm the WHOLE chain actually works: podman
// knows about the krun OCI runtime, the packages that provide it are installed,
// /dev/kvm is usable by this user, and — the only signal that proves the other
// three actually compose — a real `podman run --runtime=krun` reports a kernel
// different from the host's. Any one of those confidently missing is a BLOCK,
// with a remediation naming exactly what to install or fix; podman itself being
// unreachable degrades the whole check to a typed-Unknown WARN, never a false
// BLOCK (the package-wide degradation rule).
//
// This is gated at the cmd tier (cmd/villa/preflight_sandbox.go), not here:
// unlike checks_selinux.go/checks_rocm.go, PRE-09 needs the persisted
// workspace_agent config flag before it runs at all, and internal/preflight
// deliberately never imports internal/config (the memory/agent gates follow the
// same split).

// sandboxRequiredRPMs is the ordered set of packages the remediation names and
// RPMQuery is asked about. crun-krun is what actually registers the krun OCI
// runtime with podman; libkrun/libkrunfw are its runtime dependencies.
var sandboxRequiredRPMs = []string{"libkrun", "libkrunfw", "crun-krun"}

// sandboxPackageRemediation is the single source for the "install these packages"
// text, shared by the krun-missing and RPM-missing BLOCK branches so they never
// drift apart.
const sandboxPackageRemediation = "Install the sandbox runtime packages (libkrun, libkrunfw, crun-krun) so podman registers the krun OCI runtime, then re-run `villa preflight`."

// SandboxDeps is the injectable seam for PRE-09 so tests feed fixture podman/rpm/
// kvm/probe results instead of touching a real host.
type SandboxDeps struct {
	// PodmanInfo returns the OCI runtime names `podman info` reports (the
	// `.host.ociRuntimes[].name` list), whether the podman binary was found on
	// PATH, and whether the command ran cleanly and was parsed.
	PodmanInfo func() (runtimes []string, found, ok bool)
	// RPMQuery returns the installed version of an RPM package, whether the rpm
	// binary was found on PATH, and whether the package is installed.
	RPMQuery func(pkg string) (version string, found, ok bool)
	// StatKVM reports whether /dev/kvm is readable and writable by the current
	// user, and whether that could be evaluated at all (a stat error → ok=false).
	StatKVM func() (readable, writable, ok bool)
	// ProbeKrun runs the resolved sandbox image under --runtime=krun and returns
	// its `uname -r` output plus whether the probe completed.
	ProbeKrun func() (kernel string, ok bool)
	// HostKernel returns this host's own `uname -r`, and whether it could be read.
	HostKernel func() (kernel string, ok bool)
}

// RunSandbox executes the PRE-09 sandbox-runtime gate. It is a single check
// wrapped in a slice (mirroring RunSELinux's shape) so the doctor SBX-01 twin and
// the cmd-tier gate can compose it exactly like every other RunX in this package.
func RunSandbox(d SandboxDeps) []CheckResult {
	return []CheckResult{checkSandboxRuntime(d)}
}

// checkSandboxRuntime is PRE-09 (BLOCK): krun must be an available OCI runtime,
// its packages must be installed, /dev/kvm must be usable, and a real probe
// container must prove it actually ran in a different kernel than the host. Every
// signal is evaluated in that order and the first confident failure wins; an
// unevaluable signal (podman unreachable, rpm unreachable, an unstat-able /dev/kvm,
// a probe that could not run) degrades to WARN, never a false BLOCK or PASS.
func checkSandboxRuntime(d SandboxDeps) CheckResult {
	const (
		id   = "PRE-09"
		name = "sandbox runtime"
	)

	runtimes, found, ok := d.PodmanInfo()
	if !found {
		return warn(id, name, TierBlock,
			"podman not found on PATH — could not verify the sandbox runtime",
			sandboxPackageRemediation, "exec.LookPath(podman)", "")
	}
	if !ok {
		return warn(id, name, TierBlock,
			"podman info could not be read — could not verify the sandbox runtime",
			sandboxPackageRemediation, "podman info --format json", "")
	}
	if !slices.Contains(runtimes, "krun") {
		return fail(id, name,
			fmt.Sprintf("krun is not among podman's OCI runtimes (%s)", strings.Join(runtimes, ", ")),
			sandboxPackageRemediation, "podman info --format json (.host.ociRuntimes)", "")
	}

	var versions []string
	for _, pkg := range sandboxRequiredRPMs {
		version, rfound, rok := d.RPMQuery(pkg)
		if !rfound {
			return warn(id, name, TierBlock,
				"rpm not found on PATH — could not verify the sandbox runtime packages",
				sandboxPackageRemediation, "exec.LookPath(rpm)", "")
		}
		if !rok {
			return fail(id, name,
				fmt.Sprintf("required package %s is not installed", pkg),
				sandboxPackageRemediation, "rpm -q "+pkg, "")
		}
		versions = append(versions, pkg+"="+version)
	}
	packageProvenance := "rpm -q (" + strings.Join(versions, ", ") + ")"

	readable, writable, kok := d.StatKVM()
	if !kok {
		return warn(id, name, TierBlock,
			"could not stat /dev/kvm — could not verify the sandbox runtime",
			sandboxPackageRemediation, "stat /dev/kvm", "")
	}
	if !readable || !writable {
		return fail(id, name,
			"/dev/kvm is not readable and writable by the current user",
			"Add the user to the kvm group (or add a udev rule granting access to /dev/kvm), then log out and back in.",
			"stat /dev/kvm", "")
	}

	hostKernel, hok := d.HostKernel()
	probeKernel, pok := d.ProbeKrun()
	if !hok || !pok {
		return warn(id, name, TierBlock,
			"could not run the krun functional probe — could not verify the sandbox runtime",
			sandboxPackageRemediation, "podman run --runtime=krun <sandbox image> uname -r", "")
	}
	if probeKernel == hostKernel {
		return fail(id, name,
			fmt.Sprintf("the krun probe reported the host kernel (%s) — the task did not actually run in a microVM", probeKernel),
			sandboxPackageRemediation, "podman run --runtime=krun <sandbox image> uname -r", "")
	}

	return pass(id, name, TierBlock,
		fmt.Sprintf("krun is available (%s), /dev/kvm is accessible, and the functional probe ran in a separate kernel (%s vs host %s)", packageProvenance, probeKernel, hostKernel),
		joinProvenance("podman info --format json (.host.ociRuntimes)", packageProvenance, "stat /dev/kvm", "podman run --runtime=krun <sandbox image> uname -r"))
}
