package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// TestLiveInstallWiresTheToolServer: `--tool-server` reaches Opts, and every seam the flow
// calls while the gate is on is wired, because a nil one would panic after the units were
// written and started (ADR-0042).
func TestLiveInstallWiresTheToolServer(t *testing.T) {
	if newInstall().Flags().Lookup("tool-server") == nil {
		t.Fatal("villa install has no --tool-server flag")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	d, err := liveInstallDeps(t.Context())
	if err != nil {
		t.Fatalf("liveInstallDeps: %v", err)
	}
	if d.WriteToolEnv == nil || d.WriteRecallScope == nil || d.ProveToolServer == nil {
		t.Fatalf("liveInstallDeps leaves a tool-server seam nil: env=%v scope=%v proof=%v",
			d.WriteToolEnv != nil, d.WriteRecallScope != nil, d.ProveToolServer != nil)
	}
}

// probes returns a probe set that answers per URL from a map.
func toolProbes(web, recall status.HealthState) toolServerProbes {
	return toolServerProbes{
		Web:    func(context.Context) status.HealthState { return web },
		Recall: func(context.Context) status.HealthState { return recall },
	}
}

// TestToolServerHealthWait: install waits for each rendered unit to answer /healthz in its
// own network, passes when every one does, refuses on a unit that is confidently down, and
// only warns on one whose probe could not run (typed-Unknown, never a false FAIL).
func TestToolServerHealthWait(t *testing.T) {
	both := config.VillaConfig{ToolServer: true, WebSearchEnabled: true, MemoryEnabled: true}
	for _, tc := range []struct {
		name string
		cfg  config.VillaConfig
		p    toolServerProbes
		want preflight.Status
		in   []string
	}{
		{"both ready", both, toolProbes(status.HealthReady, status.HealthReady), preflight.StatusPass, []string{"villa-tools-web", "villa-tools-recall"}},
		{"recall down", both, toolProbes(status.HealthReady, status.HealthDown), preflight.StatusFail, []string{"villa-tools-recall"}},
		{"web unknown", both, toolProbes(status.HealthUnknown, status.HealthReady), preflight.StatusWarn, []string{"villa-tools-web"}},
		{"web still loading at the deadline", both, toolProbes(status.HealthLoading, status.HealthReady), preflight.StatusWarn, []string{"villa-tools-web"}},
		{"only web renders", config.VillaConfig{ToolServer: true, WebSearchEnabled: true}, toolProbes(status.HealthReady, status.HealthDown), preflight.StatusPass, []string{"villa-tools-web"}},
	} {
		p := waitToolServer(t.Context(), tc.cfg, tc.p, time.Millisecond, 5*time.Millisecond)
		if p.Status != tc.want {
			t.Errorf("%s: status = %v (%s), want %v", tc.name, p.Status, p.Detail, tc.want)
		}
		for _, name := range tc.in {
			if !strings.Contains(p.Detail, name) {
				t.Errorf("%s: detail %q does not name %s", tc.name, p.Detail, name)
			}
		}
		if tc.name == "only web renders" && strings.Contains(p.Detail, "villa-tools-recall") {
			t.Errorf("%s: detail names a unit that does not render: %q", tc.name, p.Detail)
		}
	}
}

// TestToolServerHealthWaitPollsUntilReady: a unit that answers on a later poll passes.
func TestToolServerHealthWaitPollsUntilReady(t *testing.T) {
	calls := 0
	p := toolServerProbes{
		Web: func(context.Context) status.HealthState {
			calls++
			if calls < 3 {
				return status.HealthDown
			}
			return status.HealthReady
		},
	}
	cfg := config.VillaConfig{ToolServer: true, WebSearchEnabled: true}
	got := waitToolServer(t.Context(), cfg, p, time.Millisecond, time.Second)
	if got.Status != preflight.StatusPass || calls != 3 {
		t.Errorf("status = %v after %d polls (%s), want a pass on the third", got.Status, calls, got.Detail)
	}
}
