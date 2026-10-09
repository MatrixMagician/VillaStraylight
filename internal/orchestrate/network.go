package orchestrate

import "strings"

// network.go reads the podman network topology back out of rendered units, so a
// caller comparing it with the host (doctor, ADR-0036) never parses Quadlet text
// itself. Pure: it reads unit text and nothing else.

// RoutedNetworkName is the podman network with a route off-box: inference, chat
// and the fetchers join it.
func RoutedNetworkName() string { return networkName }

// ClosedNetworkName is the Internal=true podman network the services with no
// runtime egress join (ADR-0036).
func ClosedNetworkName() string { return closedNetworkName }

// Topology is what a set of rendered units says about podman networks.
type Topology struct {
	// Joins maps each container to the podman networks its unit joins, in unit order.
	Joins map[string][]string
	// Internal maps each rendered network to whether its unit sets Internal=true.
	Internal map[string]bool
}

// NetworkTopology reads the ContainerName= and Network= lines of every .container
// unit and the NetworkName= and Internal= lines of every .network unit. A Network=
// value names a .network unit file; it resolves to that unit's NetworkName=, the
// name podman reports.
func NetworkTopology(units []Unit) Topology {
	top := Topology{Joins: map[string][]string{}, Internal: map[string]bool{}}
	podmanName := map[string]string{}
	for _, u := range units {
		if !strings.HasSuffix(u.Name, ".network") {
			continue
		}
		name := unitKey(u.Text, "NetworkName")
		podmanName[u.Name] = name
		top.Internal[name] = unitKey(u.Text, "Internal") == "true"
	}
	for _, u := range units {
		if !strings.HasSuffix(u.Name, ".container") {
			continue
		}
		container := unitKey(u.Text, "ContainerName")
		if container == "" {
			continue
		}
		var nets []string
		for line := range strings.SplitSeq(u.Text, "\n") {
			if attach, ok := strings.CutPrefix(line, "Network="); ok {
				if name, known := podmanName[attach]; known {
					attach = name
				}
				nets = append(nets, attach)
			}
		}
		top.Joins[container] = nets
	}
	return top
}

func unitKey(text, key string) string {
	for line := range strings.SplitSeq(text, "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return v
		}
	}
	return ""
}
