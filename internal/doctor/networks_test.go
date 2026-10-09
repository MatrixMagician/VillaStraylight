package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// closedStackUnits renders the units a memory-on stack declares about networks:
// villa-qdrant on the closed network only, Open WebUI on both.
func closedStackUnits() []orchestrate.Unit {
	return []orchestrate.Unit{
		{Name: "villa.network", Text: "[Network]\nNetworkName=villa\n"},
		{Name: "villa-closed.network", Text: "[Network]\nNetworkName=villa-closed\nInternal=true\n"},
		{Name: "villa-openwebui.container", Text: "[Container]\nContainerName=villa-openwebui\nNetwork=villa.network\nNetwork=villa-closed.network\n"},
		{Name: "villa-qdrant.container", Text: "[Container]\nContainerName=villa-qdrant\nNetwork=villa-closed.network\n"},
	}
}

// networksRun is a healthy run whose config renders closedStackUnits, every unit is
// on disk, and the host matches.
func networksRun() *run {
	r := newDoctorDeps()
	units := closedStackUnits()
	r.render(units...)
	for _, u := range units {
		r.units[u.Name] = u.Text
	}
	r.ContainerNetworks = func() (map[string][]string, error) {
		return map[string][]string{
			"villa-openwebui": {"villa-closed", "villa"},
			"villa-qdrant":    {"villa-closed"},
			"ollama":          {"podman"},
		}, nil
	}
	r.NetworkInternal = func() (map[string]bool, error) {
		return map[string]bool{"villa": false, "villa-closed": true, "podman": false}, nil
	}
	return r
}

func networksFinding(t *testing.T, r *run) Finding {
	t.Helper()
	f, ok := findingByID(r.aggregate(), "networks")
	if !ok {
		t.Fatal("the networks finding is missing")
	}
	return f
}

// TestNetworksPassWhenTheHostMatchesTheUnits: every running container is on exactly
// the networks its unit joins (order aside), a container the stack does not render
// is not villa's to judge, and villa-closed is internal on the host.
func TestNetworksPassWhenTheHostMatchesTheUnits(t *testing.T) {
	f := networksFinding(t, networksRun())
	if f.Status != statusPass || f.Tier != tierBlock {
		t.Fatalf("networks = %s/%s (%s), want PASS/BLOCK", f.Status, f.Tier, f.Detail)
	}
}

// TestNetworksFailWhenAClosedServiceStillRoutes is the upgrade gap: the unit was
// rewritten onto villa-closed but the container was never restarted, so it still
// has a route off-box.
func TestNetworksFailWhenAClosedServiceStillRoutes(t *testing.T) {
	r := networksRun()
	r.ContainerNetworks = func() (map[string][]string, error) {
		return map[string][]string{"villa-openwebui": {"villa", "villa-closed"}, "villa-qdrant": {"villa"}}, nil
	}
	f := networksFinding(t, r)
	if f.Status != statusFail {
		t.Fatalf("networks = %s (%s), want FAIL", f.Status, f.Detail)
	}
	if !strings.Contains(f.Detail, "villa-qdrant is on villa, its unit joins villa-closed") {
		t.Errorf("detail = %q, want it to name villa-qdrant's networks", f.Detail)
	}
	if !strings.Contains(f.Remediation, "villa restart villa-qdrant") {
		t.Errorf("remediation = %q, want the restart of villa-qdrant", f.Remediation)
	}
	if r.aggregate().Overall != statusFail {
		t.Error("a closed service with a route did not fail the report")
	}
}

// TestNetworksFailWhenOpenWebUILostTheClosedNetwork: chat on the routed network
// alone cannot reach memory.
func TestNetworksFailWhenOpenWebUILostTheClosedNetwork(t *testing.T) {
	r := networksRun()
	r.ContainerNetworks = func() (map[string][]string, error) {
		return map[string][]string{"villa-openwebui": {"villa"}, "villa-qdrant": {"villa-closed"}}, nil
	}
	f := networksFinding(t, r)
	if f.Status != statusFail || !strings.Contains(f.Detail, "villa-openwebui is on villa, its unit joins villa, villa-closed") {
		t.Fatalf("networks = %s (%q), want a FAIL naming villa-openwebui", f.Status, f.Detail)
	}
}

// TestNetworksFailWhenTheClosedNetworkRoutes: Quadlet creates a network with
// `podman network create --ignore`, so a villa-closed that existed before villa
// rendered it keeps its route.
func TestNetworksFailWhenTheClosedNetworkRoutes(t *testing.T) {
	r := networksRun()
	r.NetworkInternal = func() (map[string]bool, error) {
		return map[string]bool{"villa": false, "villa-closed": false}, nil
	}
	f := networksFinding(t, r)
	if f.Status != statusFail || !strings.Contains(f.Detail, "villa-closed exists without Internal") {
		t.Fatalf("networks = %s (%q), want a FAIL naming villa-closed", f.Status, f.Detail)
	}
	if !strings.Contains(f.Remediation, "podman network rm villa-closed") {
		t.Errorf("remediation = %q, want the network's recreation", f.Remediation)
	}
}

// TestNetworksPassBeforeTheClosedNetworkExists: a host where nothing has started
// on villa-closed yet has no network to judge.
func TestNetworksPassBeforeTheClosedNetworkExists(t *testing.T) {
	r := networksRun()
	r.ContainerNetworks = func() (map[string][]string, error) { return map[string][]string{}, nil }
	r.NetworkInternal = func() (map[string]bool, error) { return map[string]bool{"villa": false}, nil }
	if f := networksFinding(t, r); f.Status != statusPass {
		t.Fatalf("networks = %s (%q), want PASS", f.Status, f.Detail)
	}
}

// TestNetworksWarnWhenTheHostCannotBeRead: a failed read is typed-Unknown, never a
// confident verdict either way.
func TestNetworksWarnWhenTheHostCannotBeRead(t *testing.T) {
	for name, mutate := range map[string]func(*run){
		"containers": func(r *run) {
			r.ContainerNetworks = func() (map[string][]string, error) { return nil, errors.New("podman ps: exit 125") }
		},
		"networks": func(r *run) {
			r.NetworkInternal = func() (map[string]bool, error) { return nil, errors.New("podman network ls: exit 125") }
		},
	} {
		r := networksRun()
		mutate(r)
		f := networksFinding(t, r)
		if f.Status != statusWarn || f.Remediation == "" || !strings.Contains(f.Raw, "exit 125") {
			t.Errorf("%s: networks = %s (%q, raw %q), want a WARN carrying the read error", name, f.Status, f.Detail, f.Raw)
		}
	}
}

// TestNetworksNeedAnInstalledStack: with no unit dir there is no stack to judge.
func TestNetworksNeedAnInstalledStack(t *testing.T) {
	r := networksRun()
	r.dirMissing = true
	if f, ok := findingByID(r.aggregate(), "networks"); ok {
		t.Fatalf("networks finding on a host with no unit dir: %+v", f)
	}
}
