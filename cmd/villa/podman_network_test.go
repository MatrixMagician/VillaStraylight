package main

import (
	"maps"
	"slices"
	"testing"
)

// TestParseContainerNetworks reads `podman ps --format {{.Names}}|{{.Networks}}`
// as podman 5.8 prints it: a comma-joined list per container, empty for a
// container with no network of its own.
func TestParseContainerNetworks(t *testing.T) {
	got, err := parseContainerNetworks([]byte("villa-openwebui|villa,villa-closed\nvilla-qdrant|villa-closed\nollama|\n\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := map[string][]string{
		"villa-openwebui": {"villa", "villa-closed"},
		"villa-qdrant":    {"villa-closed"},
		"ollama":          nil,
	}
	if !maps.EqualFunc(got, want, slices.Equal) {
		t.Errorf("got %v, want %v", got, want)
	}
	if _, err := parseContainerNetworks([]byte("no separator\n")); err == nil {
		t.Error("a line without the separator parsed")
	}
}

// TestParseNetworkInternal reads `podman network ls --format {{.Name}}|{{.Internal}}`.
func TestParseNetworkInternal(t *testing.T) {
	got, err := parseNetworkInternal([]byte("podman|false\nvilla|false\nvilla-closed|true\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if want := map[string]bool{"podman": false, "villa": false, "villa-closed": true}; !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if _, err := parseNetworkInternal([]byte("villa|maybe\n")); err == nil {
		t.Error("an Internal value that is not a bool parsed")
	}
}
