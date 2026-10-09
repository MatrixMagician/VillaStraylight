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

// TestRoutedUnitsKeepTheirNetworks: the fetchers and the inference units stay on
// the routed network, and Open WebUI joins both so it reaches either side.
func TestRoutedUnitsKeepTheirNetworks(t *testing.T) {
	units, err := Render(statefulFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := map[string][]string{
		"villa-llama.container":      {"villa.network"},
		"villa-searxng.container":    {"villa.network"},
		"villa-websafe.container":    {"villa.network"},
		"villa-inferproxy.container": {"villa.network", "villa-sandbox.network"},
		"villa-openwebui.container":  {"villa.network", "villa-closed.network"},
	}
	for name, nets := range want {
		if got := networkLines(unitByName(t, units, name).Text); !slices.Equal(got, nets) {
			t.Errorf("%s joins %v, want %v", name, got, nets)
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
	units, err := Render(statefulFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	top := NetworkTopology(units)

	joins := map[string][]string{
		"villa-llama":      {"villa"},
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
		"villa-inferproxy": {"villa", "villa-sandbox"},
	}
	if !maps.EqualFunc(top.Joins, joins, slices.Equal) {
		t.Errorf("Joins = %v,\nwant    %v", top.Joins, joins)
	}
	internal := map[string]bool{"villa": false, "villa-sandbox": true, "villa-closed": true}
	if !maps.Equal(top.Internal, internal) {
		t.Errorf("Internal = %v, want %v", top.Internal, internal)
	}
}
