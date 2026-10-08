package main

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// TestPresentServicesSkipsUnitsNotOnDisk: an update restarts a subsystem's
// services, but a memory-on host with the reranker off has no villa-rerank unit,
// and restarting a service systemd has never seen fails the update. The restart
// set is therefore the services whose unit is on disk, the rule capture already
// applies.
func TestPresentServicesSkipsUnitsNotOnDisk(t *testing.T) {
	units, services := subsystem.Memory.Units()
	onDisk := map[string]bool{"villa-qdrant.container": true, "villa-embed.container": true}
	exists := func(unit string) bool { return onDisk[unit] }

	got := presentServices(units, services, exists)
	if want := "villa-qdrant.service,villa-embed.service"; strings.Join(got, ",") != want {
		t.Errorf("presentServices = %v, want %s", got, want)
	}

	onDisk["villa-rerank.container"] = true
	got = presentServices(units, services, exists)
	if want := "villa-qdrant.service,villa-embed.service,villa-rerank.service"; strings.Join(got, ",") != want {
		t.Errorf("with every unit on disk presentServices = %v, want %s", got, want)
	}
}
