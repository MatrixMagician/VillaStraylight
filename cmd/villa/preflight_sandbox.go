package main

// preflight_sandbox.go wires PRE-09 (internal/preflight/checks_sandbox.go) and its
// doctor twin SBX-01 to the real host. Live wiring is gated at THIS tier, not
// inside internal/preflight, for the same reason the memory/agent gates are
// (cmd/villa/preflight.go's memoryGateResults, preflight_agent.go): PRE-09 needs
// the persisted workspace_agent config flag (subsystem.SandboxOn) and the
// pin-resolved sandbox image (internal/pinresolve), and internal/preflight never
// imports config.
//
// ON-HARDWARE FINDING (issue #176, podman 5.8.4 on this host): `podman info
// --format json` carries a SINGULAR `.host.ociRuntime` (the currently-selected
// default — "crun", even with crun-krun installed; libkrun support shows up as
// "+LIBKRUN" in ITS version string, not as a second named runtime) and a NULL
// `.host.ociRuntimes` (plural). Podman resolves `--runtime=krun` against its own
// built-in table of known runtime names/paths — the same mechanism `exec.LookPath`
// uses — not against anything `podman info` enumerates. Parsing a `.host.ociRuntimes`
// list (the spec's literal wording) would report krun ABSENT on every real host,
// including this one (crun-krun installed, /dev/kvm 0666, `podman run
// --runtime=krun localhost/villa-sandbox:office uname -r` returns 6.12.91 against
// a 7.1.13-200.fc44.x86_64 host) — the exact false-BLOCK the typed-Unknown
// degradation rule exists to forbid. So liveSandboxPodmanInfo reports "krun"
// available when a krun binary resolves on PATH (found=podman itself reachable,
// ok=`podman info` answered) rather than reading a JSON field that does not exist.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// sandboxProbeTimeout bounds the functional krun probe (a fresh microVM boot),
// mirroring internal/preflight's own toolTimeout discipline: a wedged podman must
// degrade to unevaluable, never hang the command indefinitely.
const sandboxProbeTimeout = 20 * time.Second

// sandboxGateResults is the OPT-IN sandbox-runtime gate (PRE-09) to append to the
// `villa preflight` results, or nil when the workspace agent is off — mirroring
// memoryGateResults (cmd/villa/preflight.go) so a sandbox-off preflight stays
// byte-identical. Live wiring: liveSandboxGateResults.
var sandboxGateResults = liveSandboxGateResults

// liveSandboxGateResults loads the persisted config FAIL-SOFT (a load error or
// absent file yields sandbox-off, mirroring liveMemoryGateResults — a broken
// config never silently enables the gate) and, only when workspace_agent is on,
// runs PRE-09 against the live host.
func liveSandboxGateResults(_ detect.HostProfile) []preflight.CheckResult {
	cfg, err := config.LoadVilla()
	if err != nil || !subsystem.SandboxOn(cfg) {
		return nil
	}
	return preflight.RunSandbox(liveSandboxDeps())
}

// liveSandboxDeps resolves the effective sandbox image through the pin resolver
// (never a literal here — the same fallback shape livePinFunc uses) and wires
// every PRE-09/SBX-01 signal to the real host. It is shared verbatim between
// `villa preflight`'s gate and `villa doctor`'s SBX-01 twin so the two can never
// observe a different host through a different probe.
func liveSandboxDeps() preflight.SandboxDeps {
	image := orchestrate.SandboxImage()
	if res, ok := liveResolver().Resolve(pins.SandboxImage); ok && res.Current.Ref != "" {
		image = res.Current.Ref
	}
	return preflight.SandboxDeps{
		PodmanInfo: liveSandboxPodmanInfo,
		RPMQuery:   liveSandboxRPMQuery,
		StatKVM:    liveSandboxStatKVM,
		ProbeKrun:  liveSandboxProbeKrun(image),
		HostKernel: liveSandboxHostKernel,
	}
}

// podmanHostInfo is the minimal shape read out of `podman info --format json` —
// only the field this gate actually needs (see the file doc comment for why the
// nonexistent `.host.ociRuntimes` list is not read).
type podmanHostInfo struct {
	Host struct {
		OCIRuntime struct {
			Name string `json:"name"`
		} `json:"ociRuntime"`
	} `json:"host"`
}

// liveSandboxPodmanInfo is the PodmanInfo seam. found reports whether the podman
// binary itself was found; ok reports whether `podman info` answered and parsed.
// "krun" is included in runtimes when a krun binary resolves on PATH — see the
// file doc comment for why that is podman's OWN resolution mechanism, not a JSON
// field.
func liveSandboxPodmanInfo() (runtimes []string, found, ok bool) {
	if _, err := exec.LookPath("podman"); err != nil {
		return nil, false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), sandboxProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "podman", "info", "--format", "json").Output()
	if err != nil {
		return nil, true, false
	}
	var info podmanHostInfo
	if jsonErr := json.Unmarshal(out, &info); jsonErr != nil || info.Host.OCIRuntime.Name == "" {
		return nil, true, false
	}
	runtimes = []string{info.Host.OCIRuntime.Name}
	if _, err := exec.LookPath("krun"); err == nil {
		runtimes = append(runtimes, "krun")
	}
	return runtimes, true, true
}

// liveSandboxRPMQuery is the RPMQuery seam: `rpm -q --qf VERSION-RELEASE <pkg>`.
// found reports whether the rpm binary itself was found; ok reports whether the
// package is installed (a non-zero exit means "not installed", not "unevaluable").
func liveSandboxRPMQuery(pkg string) (version string, found, ok bool) {
	if _, err := exec.LookPath("rpm"); err != nil {
		return "", false, false
	}
	out, err := exec.Command("rpm", "-q", "--qf", "%{VERSION}-%{RELEASE}", pkg).Output() //nolint:gosec // fixed-arg, pkg is one of sandboxRequiredRPMs
	if err != nil {
		return "", true, false
	}
	return strings.TrimSpace(string(out)), true, true
}

// liveSandboxStatKVM is the StatKVM seam: an absent /dev/kvm degrades to
// unevaluable (ok=false — the host may simply not virtualize); an actual
// O_RDWR open (closed immediately, no state changed) is the honest test of
// "readable and writable by the current user" — group membership makes a plain
// mode-bits check on the stat result insufficient.
func liveSandboxStatKVM() (readable, writable, ok bool) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return false, false, false
	}
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return false, false, true
	}
	_ = f.Close()
	return true, true, true
}

// liveSandboxHostKernel is the HostKernel seam: `uname -r` for THIS host, the
// baseline the functional probe's microVM kernel must differ from.
func liveSandboxHostKernel() (kernel string, ok bool) {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// liveSandboxProbeKrun builds the ProbeKrun seam bound to the resolved sandbox
// image: `podman run --rm --runtime=krun <image> uname -r`, bounded by
// sandboxProbeTimeout. An empty image (resolution failed) is unevaluable.
func liveSandboxProbeKrun(image string) func() (string, bool) {
	return func() (string, bool) {
		if image == "" {
			return "", false
		}
		ctx, cancel := context.WithTimeout(context.Background(), sandboxProbeTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "podman", "run", "--rm", "--runtime=krun", image, "uname", "-r").Output()
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(out)), true
	}
}
