package orchestrate

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// closedServiceUnits are the units with no runtime need to reach off-box (ADR-0036).
var closedServiceUnits = []string{
	"villa-qdrant.container",
	"villa-embed.container",
	"villa-rerank.container",
	"villa-extract.container",
	"villa-image.container",
	"villa-stt.container",
	"villa-tts.container",
}

// networkLines returns a unit's Network= values in order.
func networkLines(text string) []string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if v, ok := strings.CutPrefix(line, "Network="); ok {
			out = append(out, v)
		}
	}
	return out
}

// TestRenderClosedNetworkGolden: villa-closed.network is rendered Internal=true in
// every config, last, so no existing unit moves (ADR-0036).
func TestRenderClosedNetworkGolden(t *testing.T) {
	for name, in := range map[string]RenderInput{"minimal": fixtureInput(), "everything on": statefulFixtureInput()} {
		units, err := Render(in)
		if err != nil {
			t.Fatalf("%s: Render: %v", name, err)
		}
		if last := units[len(units)-1].Name; last != "villa-closed.network" {
			t.Errorf("%s: last unit = %q, want villa-closed.network", name, last)
		}
		goldenCompare(t, "villa-closed.network.golden", unitByName(t, units, "villa-closed.network").Text)
	}
}

// TestClosedServicesJoinOnlyTheClosedNetwork: every service with no runtime egress
// joins villa-closed and nothing else, so it has no route off-box.
func TestClosedServicesJoinOnlyTheClosedNetwork(t *testing.T) {
	units, err := Render(statefulFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, name := range closedServiceUnits {
		u := unitByName(t, units, name)
		if got := networkLines(u.Text); !slices.Equal(got, []string{"villa-closed.network"}) {
			t.Errorf("%s joins %v, want [villa-closed.network] only", name, got)
		}
		if !strings.Contains(u.Text, "After=villa-closed-network.service") || strings.Contains(u.Text, "After=villa-network.service") {
			t.Errorf("%s is not ordered after the closed network it joins:\n%s", name, u.Text)
		}
	}
}

// TestRoutedUnitsKeepTheirNetworks: the fetchers stay on the routed network, and
// Open WebUI joins both so it reaches either side.
func TestRoutedUnitsKeepTheirNetworks(t *testing.T) {
	units, err := Render(statefulFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := map[string][]string{
		"villa-searxng.container":   {"villa.network"},
		"villa-websafe.container":   {"villa.network"},
		"villa-openwebui.container": {"villa.network", "villa-closed.network"},
	}
	for name, nets := range want {
		if got := networkLines(unitByName(t, units, name).Text); !slices.Equal(got, nets) {
			t.Errorf("%s joins %v, want %v", name, got, nets)
		}
	}
}

// residentStatefulInput is the whole stack plus one resident slot.
func residentStatefulInput() RenderInput {
	in := statefulFixtureInput()
	r := residentFixtureInput()
	in.Cfg.Resident, in.Resident = r.Cfg.Resident, r.Resident
	return in
}

// TestInferenceUnitsJoinOnlyTheClosedNetwork: villa-llama and every resident slot
// join villa-closed and nothing else, so inference has no route off-box (ADR-0036,
// amended by ADR-0043). They keep their loopback publish: the host reaches them
// through it, and the network change must not remove it.
func TestInferenceUnitsJoinOnlyTheClosedNetwork(t *testing.T) {
	units, err := Render(residentStatefulInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, name := range []string{"villa-llama.container", residentUnitName} {
		u := unitByName(t, units, name)
		if got := networkLines(u.Text); !slices.Equal(got, []string{"villa-closed.network"}) {
			t.Errorf("%s joins %v, want [villa-closed.network] only", name, got)
		}
		if !strings.Contains(u.Text, "After=villa-closed-network.service") || strings.Contains(u.Text, "After=villa-network.service") {
			t.Errorf("%s is not ordered after the closed network it joins:\n%s", name, u.Text)
		}
		if !strings.Contains(u.Text, "PublishPort=127.0.0.1:") {
			t.Errorf("%s lost its loopback publish:\n%s", name, u.Text)
		}
	}
}

// TestInferproxyBridgesTheClosedAndSandboxNetworks: the proxy reaches villa-llama on
// the closed network and serves the sandbox on its own, and villa-llama never joins
// the sandbox network (ADR-0011). The proxy is off the routed network: nothing it
// does needs a route off-box.
func TestInferproxyBridgesTheClosedAndSandboxNetworks(t *testing.T) {
	units, err := Render(residentStatefulInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	proxy := unitByName(t, units, "villa-inferproxy.container")
	if got := networkLines(proxy.Text); !slices.Equal(got, []string{"villa-closed.network", "villa-sandbox.network"}) {
		t.Errorf("villa-inferproxy joins %v, want [villa-closed.network villa-sandbox.network]", got)
	}
	if !strings.Contains(proxy.Text, "After=villa-closed-network.service") || strings.Contains(proxy.Text, "After=villa-network.service") {
		t.Errorf("villa-inferproxy is not ordered after the closed network:\n%s", proxy.Text)
	}
	for _, name := range []string{"villa-llama.container", residentUnitName} {
		for _, n := range networkLines(unitByName(t, units, name).Text) {
			if n == "villa-sandbox.network" {
				t.Errorf("%s joins the sandbox network", name)
			}
		}
	}
}

// TestInferenceUnitsCarryNoFetchFlag: the closed network is only safe for inference
// if llama-server never needs to fetch anything at runtime. Weights are a read-only
// bind mount, so no rendered argv may name a download flag; a future one would fail
// here instead of failing at start on a network with no route.
func TestInferenceUnitsCarryNoFetchFlag(t *testing.T) {
	fetch := []string{
		"-hf", "--hf-repo", "-hff", "--hf-file", "-hfv", "--hf-repo-v", "-hfd", "--hf-repo-draft",
		"-hft", "--hf-token", "-mu", "--model-url", "-mmu", "--mmproj-url", "-dr", "--docker-repo",
	}
	inputs := map[string]RenderInput{
		"minimal":  fixtureInput(),
		"resident": residentStatefulInput(),
		"draft":    draftFixtureInput(t, "vulkan"),
		"rocm":     rocmFixtureInput(t),
		"vision":   visionFixtureInput(t, "vulkan"),
	}
	for name, in := range inputs {
		units, err := Render(in)
		if err != nil {
			t.Fatalf("%s: Render: %v", name, err)
		}
		for _, u := range units {
			if !strings.HasPrefix(u.Name, "villa-llama") || !strings.HasSuffix(u.Name, ".container") {
				continue
			}
			exec := lineWithPrefix(t, u, "Exec=")
			for tok := range strings.FieldsSeq(exec) {
				if slices.Contains(fetch, tok) {
					t.Errorf("%s: %s argv carries fetch flag %q:\n%s", name, u.Name, tok, exec)
				}
			}
		}
	}
}

// TestOpenWebUIJoinsTheClosedNetworkWithEverythingOff: the chat unit has one shape;
// it joins villa-closed even when no closed service is rendered.
func TestOpenWebUIJoinsTheClosedNetworkWithEverythingOff(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := networkLines(unitByName(t, units, "villa-openwebui.container").Text)
	if !slices.Equal(got, []string{"villa.network", "villa-closed.network"}) {
		t.Errorf("villa-openwebui joins %v, want [villa.network villa-closed.network]", got)
	}
}

// TestNetworkTopologyReadsTheRenderedUnits: the topology doctor compares with the
// host names podman networks, not Quadlet files, and knows which are internal.
func TestNetworkTopologyReadsTheRenderedUnits(t *testing.T) {
	units, err := Render(residentStatefulInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	top := NetworkTopology(units)

	joins := map[string][]string{
		"villa-llama": {"villa-closed"},
		"villa-llama-" + residentSlug(residentModelID): {"villa-closed"},
		"villa-openwebui":  {"villa", "villa-closed"},
		"villa-qdrant":     {"villa-closed"},
		"villa-embed":      {"villa-closed"},
		"villa-rerank":     {"villa-closed"},
		"villa-extract":    {"villa-closed"},
		"villa-image":      {"villa-closed"},
		"villa-stt":        {"villa-closed"},
		"villa-tts":        {"villa-closed"},
		"villa-searxng":    {"villa"},
		"villa-websafe":    {"villa"},
		"villa-inferproxy": {"villa-closed", "villa-sandbox"},
	}
	if !maps.EqualFunc(top.Joins, joins, slices.Equal) {
		t.Errorf("Joins = %v,\nwant    %v", top.Joins, joins)
	}
	internal := map[string]bool{"villa": false, "villa-sandbox": true, "villa-closed": true}
	if !maps.Equal(top.Internal, internal) {
		t.Errorf("Internal = %v, want %v", top.Internal, internal)
	}
}
