package main

import (
	"strings"
	"testing"
)

// TestCodeOfflineFlag guards the opt-in --offline surface: it exists, defaults off, is
// carried into the launch Deps, and --help states the cost (the agent's shell inherits
// the proxy variables, so HTTPS from it fails).
func TestCodeOfflineFlag(t *testing.T) {
	cmd := newCode()
	f := cmd.Flags().Lookup("offline")
	if f == nil || f.DefValue != "false" {
		t.Fatalf("--offline flag = %+v, want a bool defaulting to false", f)
	}
	for _, want := range []string{"git clone", "go mod download", "HTTPS_PROXY"} {
		if !strings.Contains(cmd.Long+f.Usage, want) {
			t.Errorf("--help does not mention %q", want)
		}
	}
	if liveCodeDeps(false).Offline || !liveCodeDeps(true).Offline {
		t.Error("liveCodeDeps does not carry the offline flag into agent.Deps")
	}
}
