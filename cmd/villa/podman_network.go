package main

// podman_network.go holds doctor's two raw reads of the host's podman networks
// (ADR-0036): which networks each running container is on, and which networks are
// internal. Both are fixed-arg execs; the comparison with the units is doctor's.

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func liveContainerNetworks() (map[string][]string, error) {
	out, err := exec.Command("podman", "ps", "--format", "{{.Names}}|{{.Networks}}").Output()
	if err != nil {
		return nil, fmt.Errorf("podman ps: %w", err)
	}
	return parseContainerNetworks(out)
}

func liveNetworkInternal() (map[string]bool, error) {
	out, err := exec.Command("podman", "network", "ls", "--format", "{{.Name}}|{{.Internal}}").Output()
	if err != nil {
		return nil, fmt.Errorf("podman network ls: %w", err)
	}
	return parseNetworkInternal(out)
}

// parseContainerNetworks parses `name|net1,net2` lines.
func parseContainerNetworks(out []byte) (map[string][]string, error) {
	running := map[string][]string{}
	err := eachPipeLine(out, func(name, value string) error {
		var nets []string
		if value != "" {
			nets = strings.Split(value, ",")
		}
		running[name] = nets
		return nil
	})
	return running, err
}

// parseNetworkInternal parses `name|true` lines.
func parseNetworkInternal(out []byte) (map[string]bool, error) {
	internal := map[string]bool{}
	err := eachPipeLine(out, func(name, value string) error {
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("network %q: Internal %q: %w", name, value, err)
		}
		internal[name] = b
		return nil
	})
	return internal, err
}

func eachPipeLine(out []byte, fn func(a, b string) error) error {
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		a, b, ok := strings.Cut(line, "|")
		if !ok {
			return fmt.Errorf("unparseable podman line %q", line)
		}
		if err := fn(a, b); err != nil {
			return err
		}
	}
	return nil
}
