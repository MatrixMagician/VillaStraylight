package doctor

// decide.go holds the decisions doctor used to take from its command-tier adapter and
// now takes from the config the run loaded (ADR-0017): the unit drift plan, the SBX-02
// sandbox network scan, tools-mode drift and coding-agent drift. Each one asks Deps
// for raw reads (a unit file's bytes, whether the unit dir exists, a hash, the crush
// config bytes, the rendered units) and decides here, so the rules are table-testable
// without a host and a caller cannot get them wrong by wiring.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// embedServiceName is the embedder's systemd service, derived from its Quadlet unit
// name the way the status fold and the lifecycle verbs derive theirs.
func embedServiceName() string {
	return strings.TrimSuffix(orchestrate.EmbedContainerUnitName(), ".container") + ".service"
}

// inferenceUnitFile is the Quadlet unit villa-llama is rendered to.
func inferenceUnitFile() string {
	units, _ := subsystem.Inference.EveryUnit()
	return units[0]
}

// mountedVilla returns the host villa path the INSTALLED villa-websafe unit
// bind-mounts, and whether that unit is on disk at all.
//
// The drift comparison renders with this path rather than the running binary's so it
// stays config-vs-disk: the running executable's location is host state, and rendering
// with it made the same binary at a different path (a worktree build, a copy in /tmp)
// look like a hand-edited unit (issue #141). A false return means the unit is not
// installed, and the caller falls back to the running binary — the path install would
// write.
func (d Deps) mountedVilla() (string, bool) {
	text, err := d.ReadUnit(orchestrate.WebsafeContainerUnitName())
	if err != nil {
		return "", false
	}
	return orchestrate.MountedVillaPath(string(text))
}

// unitDrift renders the units cfg calls for and compares each with the file on disk,
// writing nothing. An absent unit dir is an error, not drift: the stack was never
// installed, and comparing against nothing would report every rendered unit as
// Changed and misreport "units no longer match". The caller degrades any error to the
// typed-Unknown WARN. An absent unit file IS drift (Changed), as orchestrate.Reconcile
// reads it, and a registry unit on disk the config does not render is Removed through
// the same orchestrate.Orphans Reconcile uses (ADR-0035).
func (d Deps) unitDrift(cfg config.VillaConfig) (orchestrate.Plan, error) {
	exists, err := d.UnitDirExists()
	if err != nil {
		return orchestrate.Plan{}, fmt.Errorf("resolve unit dir: %w", err)
	}
	if !exists {
		return orchestrate.Plan{}, errors.New("the Quadlet unit dir does not exist")
	}
	// Render the binary mount from what the INSTALLED unit records, not from the
	// running binary (issue #141). With no unit installed there is nothing to
	// preserve, so the running binary — the path install would write — is the honest
	// input.
	hostVilla := d.RunningVilla()
	if mounted, ok := d.mountedVilla(); ok {
		hostVilla = mounted
	}
	units, err := d.RenderUnits(cfg, hostVilla)
	if err != nil {
		return orchestrate.Plan{}, fmt.Errorf("render: %w", err)
	}
	var plan orchestrate.Plan
	for _, u := range units {
		onDisk, err := d.ReadUnit(u.Name)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			plan.Changed = append(plan.Changed, u)
		case err != nil:
			return orchestrate.Plan{}, fmt.Errorf("read unit %q: %w", u.Name, err)
		case string(onDisk) == u.Text:
			plan.Unchanged = append(plan.Unchanged, u)
		default:
			plan.Changed = append(plan.Changed, u)
		}
	}
	plan.Removed, err = orchestrate.Orphans(units, d.ReadUnit)
	if err != nil {
		return orchestrate.Plan{}, err
	}
	return plan, nil
}

// sandboxNetwork is SBX-02: the villa-sandbox.network unit must be on disk and the
// inference PROXY unit joined to it. villa-llama itself joins villa.network only
// (ADR-0011): the proxy is the one unit on both networks, so it is the one whose
// missing join leaves a task with no route to the served model. A dir that cannot be
// resolved is a typed-Unknown WARN; an unreadable unit is absent, as it is to a task.
func (d Deps) sandboxNetwork() Finding {
	if _, err := d.UnitDirExists(); err != nil {
		return Finding{
			ID:          "SBX-02",
			Name:        "Sandbox network",
			Tier:        tierWarn,
			Status:      statusWarn,
			Detail:      "could not evaluate the sandbox network (" + err.Error() + ")",
			Remediation: "run `villa install` to write the Quadlet units, then re-run `villa doctor`",
			Provenance:  sandboxNetworkProvenance,
			Raw:         err.Error(),
		}
	}
	_, networkErr := d.ReadUnit(orchestrate.SandboxNetworkName() + ".network")
	proxy, proxyErr := d.ReadUnit(orchestrate.InferproxyContainerUnitName())
	joined := proxyErr == nil && strings.Contains(string(proxy), "Network="+orchestrate.SandboxNetworkName())
	return sandboxNetworkFinding(networkErr == nil, joined)
}

// toolsDrift answers TMD-01's question: does the ON-DISK inference unit carry the
// tool-calling flag (served), and does the answered gate say it should (want)? ok
// reports whether the question could be answered at all — an unresolvable backend or
// an unreadable unit degrades to a typed-Unknown WARN, NEVER to a served==want PASS.
func (d Deps) toolsDrift(cfg config.VillaConfig) (served, want, ok bool) {
	token, err := toolsFlagToken(cfg.Backend)
	if err != nil {
		return false, false, false
	}
	unit, err := d.ReadUnit(inferenceUnitFile())
	if err != nil {
		return false, false, false
	}
	return unitCarriesToolsFlag(unit, token), subsystem.ToolsOn(cfg), true
}

// toolsFlagToken is the token the rendered inference unit carries when tool calling
// is on, DERIVED rather than typed: it is the argument the backend seam adds when
// RunSpec.Tools flips on. Deriving it keeps the flag literal inside
// internal/inference (TestSeamGrepGate) and means a rename of the flag cannot leave
// this check silently asserting a token nothing emits any more.
func toolsFlagToken(backendName string) (string, error) {
	b, err := inference.BackendFor(backendName)
	if err != nil {
		return "", err
	}
	off := map[string]int{}
	for _, a := range b.ContainerArgs(inference.RunSpec{}) {
		off[a]++
	}
	var extra []string
	for _, a := range b.ContainerArgs(inference.RunSpec{Tools: true}) {
		if off[a] > 0 {
			off[a]--
			continue
		}
		extra = append(extra, a)
	}
	if len(extra) != 1 {
		return "", fmt.Errorf("the inference seam adds %d arguments for tool calling, want exactly one", len(extra))
	}
	return extra[0], nil
}

// unitCarriesToolsFlag reports whether a rendered unit's Exec line carries the given
// token as a whole argument. It scans the Exec line rather than the whole file so a
// token appearing in a comment or a label is not mistaken for a served flag.
func unitCarriesToolsFlag(unit []byte, token string) bool {
	sc := bufio.NewScanner(bytes.NewReader(unit))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "Exec=") {
			continue
		}
		for _, f := range strings.Fields(line) {
			if f == token {
				return true
			}
		}
	}
	return false
}

// agentDrift assembles the pure agent.DetectDrift inputs from raw reads (the
// installed-binary SHA, the on-disk crush.json, a freshly rendered reference) and the
// pinned policy binary hash, and returns the report-only DriftReport. Any read error
// degrades to a typed-Unknown WARN report (BinaryDriftUnknown) rather than a
// fabricated drift — doctor never FAILs on a signal it could not evaluate.
func (d Deps) agentDrift(cfg config.VillaConfig) agent.DriftReport {
	reference, err := d.RenderCrushConfig(cfg)
	if err != nil {
		return agentDriftUnknown("could not render the reference crush.json to check drift: %v", err)
	}
	binSHA, binPresent, err := d.AgentBinarySHA()
	if err != nil {
		return agentDriftUnknown("could not hash the villa-owned Crush binary to check drift: %v", err)
	}
	onDisk, configPresent, err := d.ReadCrushConfig()
	if err != nil {
		return agentDriftUnknown("could not read the on-disk crush.json to check drift: %v", err)
	}
	var policyBinSHA string
	if asset, ok := agent.LoadCrushPolicy().Assets["linux/amd64"]; ok {
		policyBinSHA = asset.BinarySHA256
	}
	return agent.DetectDrift(agent.DriftInput{
		BinaryPresent:   binPresent,
		InstalledBinSHA: binSHA,
		PolicyBinSHA:    policyBinSHA,
		ConfigPresent:   configPresent,
		OnDiskConfig:    onDisk,
		RenderedConfig:  reference,
	})
}

func agentDriftUnknown(format string, err error) agent.DriftReport {
	return agent.DriftReport{BinaryDriftUnknown: true, Reason: fmt.Sprintf(format, err)}
}

// networksProvenance names where the networks finding's facts come from.
const networksProvenance = "rendered units vs `podman ps` + `podman network ls`"

// networks compares the podman networks the rendered units declare with the host
// (ADR-0036): every running container the units render must be on exactly the
// networks its unit joins, and every rendered Internal=true network that exists
// must be internal. Unit files cannot show either fault: Quadlet keeps a network
// that already existed (`podman network create --ignore`), and a unit rewritten
// without a restart leaves its container where it was.
func (d Deps) networks(rendered []orchestrate.Unit) Finding {
	f := Finding{ID: "networks", Name: "Container networks", Tier: tierBlock, Provenance: networksProvenance}
	unread := func(err error) Finding {
		f.Status = statusWarn
		f.Detail = "could not read the host's podman networks to compare them with the units"
		f.Remediation = "check that `podman ps` and `podman network ls` run for this user, then re-run `villa doctor`"
		f.Raw = err.Error()
		return f
	}
	running, err := d.ContainerNetworks()
	if err != nil {
		return unread(err)
	}
	internal, err := d.NetworkInternal()
	if err != nil {
		return unread(err)
	}
	return networksVerdict(f, orchestrate.NetworkTopology(rendered), running, internal)
}

// networksVerdict decides the networks finding from the rendered topology and the
// two host reads.
func networksVerdict(f Finding, top orchestrate.Topology, running map[string][]string, internal map[string]bool) Finding {
	var faults, restarts, routed []string
	for _, container := range slices.Sorted(maps.Keys(top.Joins)) {
		want := top.Joins[container]
		got, up := running[container]
		if !up || sameNetworks(got, want) {
			continue
		}
		faults = append(faults, fmt.Sprintf("%s is on %s, its unit joins %s", container, strings.Join(slices.Sorted(slices.Values(got)), ", "), strings.Join(want, ", ")))
		restarts = append(restarts, container)
	}
	for _, network := range slices.Sorted(maps.Keys(top.Internal)) {
		if isInternal, exists := internal[network]; top.Internal[network] && exists && !isInternal {
			faults = append(faults, network+" exists without Internal, so it routes off-box")
			routed = append(routed, network)
		}
	}
	if len(faults) == 0 {
		f.Status = statusPass
		f.Detail = "every running villa container is on the networks its unit joins, and every internal network has no route out"
		return f
	}
	var fixes []string
	if len(restarts) > 0 {
		fixes = append(fixes, "restart each container so it rejoins its unit's networks: `villa restart "+strings.Join(restarts, "` / `villa restart ")+"`")
	}
	for _, network := range routed {
		fixes = append(fixes, "stop the services on "+network+", run `podman network rm "+network+"`, then `villa up` to recreate it internal")
	}
	f.Status = statusFail
	f.Detail = strings.Join(faults, "; ")
	f.Remediation = strings.Join(fixes, "; ")
	return f
}

// sameNetworks reports whether two network lists hold the same set.
func sameNetworks(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}
