package main

import (
	"context"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// TestToolServerStatusProbes: each tool unit's status row asks its own /healthz on the
// network the unit joins, the web unit on the routed one and the recall unit on the closed
// one, and maps the code through the shared doctrine (200 ready, 503 loading, a refused
// connect confidently down).
func TestToolServerStatusProbes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	type call struct{ network, url string }
	var calls []call
	answers := map[string]string{
		"http://villa-tools-web:8092/healthz":    "200",
		"http://villa-tools-recall:8093/healthz": "503",
	}
	orig := memoryProbeExec
	memoryProbeExec = func(_ context.Context, network, _ string, curlArgs ...string) ([]byte, int, error) {
		url := probeURLOf(curlArgs)
		calls = append(calls, call{network, url})
		return []byte(answers[url]), 0, nil
	}
	resetMemoryHealthCache()
	t.Cleanup(func() { memoryProbeExec = orig; resetMemoryHealthCache() })

	if got := liveToolsWebHealth(); got != status.HealthReady {
		t.Errorf("liveToolsWebHealth = %q, want ready", got)
	}
	if got := liveToolsRecallHealth(); got != status.HealthLoading {
		t.Errorf("liveToolsRecallHealth = %q, want loading", got)
	}
	want := []call{
		{routedNetwork, "http://villa-tools-web:8092/healthz"},
		{closedNetwork, "http://villa-tools-recall:8093/healthz"},
	}
	if len(calls) != 2 || calls[0] != want[0] || calls[1] != want[1] {
		t.Errorf("probes = %v, want %v", calls, want)
	}
	// A second read inside the TTL window asks nothing new.
	liveToolsWebHealth()
	liveToolsRecallHealth()
	if len(calls) != 2 {
		t.Errorf("a cached read probed again: %v", calls)
	}
}
