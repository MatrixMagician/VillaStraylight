package install

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// TestWorkspaceAgentGateResolves asserts --workspace-agent turns the addon on for
// this run and that a persisted opt-in survives a bare install, matching how the
// other addon flags behave. A flag can never turn one off: disabling is an explicit
// config edit.
func TestWorkspaceAgentGateResolves(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  config.VillaConfig
		opts Opts
		want bool
	}{
		{"off by default", config.VillaConfig{}, Opts{}, false},
		{"flag turns it on", config.VillaConfig{}, Opts{WorkspaceAgent: true}, true},
		{"persisted survives a bare install", config.VillaConfig{WorkspaceAgent: true}, Opts{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := ResolveGates(tc.cfg, tc.opts, recommend.Recommendation{})
			if g.Sandbox != tc.want {
				t.Errorf("Gates.Sandbox = %v, want %v", g.Sandbox, tc.want)
			}
			if g.On(subsystem.Sandbox) != tc.want {
				t.Errorf("Gates.On(Sandbox) = %v, want %v", g.On(subsystem.Sandbox), tc.want)
			}
		})
	}
}

// TestWorkspaceAgentPersistsBothFlags asserts the install writes the gate AND tools
// mode. The agent drives the chat endpoint through tool calls, so persisting the
// addon without the flag that lets the unit serve them would leave a stack
// `villa work` refuses to run on.
func TestWorkspaceAgentPersistsBothFlags(t *testing.T) {
	rec := recommend.Recommendation{Model: "m", Quant: "q", ContextLen: 4096, Backend: "rocm"}

	plan := AssemblePlan(config.VillaConfig{}, Gates{Sandbox: true}, rec, nil)
	if !plan.Config.WorkspaceAgent || !plan.Config.ToolsMode {
		t.Errorf("plan config = workspace_agent %v / tools_mode %v, want both true",
			plan.Config.WorkspaceAgent, plan.Config.ToolsMode)
	}
	if !subsystem.SandboxOn(plan.Config) || !subsystem.ToolsOn(plan.Config) {
		t.Error("the persisted config does not answer both gates on")
	}
}

// TestWorkspaceAgentOffLeavesToolsModeAlone asserts the addon raises tools mode and
// never lowers it: an operator who ran `villa tools-mode enter` deliberately keeps it
// through an install that does not enable the workspace agent.
func TestWorkspaceAgentOffLeavesToolsModeAlone(t *testing.T) {
	rec := recommend.Recommendation{Model: "m", Quant: "q", ContextLen: 4096, Backend: "rocm"}
	cfg := config.VillaConfig{ToolsMode: true}

	plan := AssemblePlan(cfg, Gates{}, rec, nil)
	if !plan.Config.ToolsMode {
		t.Error("a bare install cleared a deliberately-entered tools mode")
	}
	if plan.Config.WorkspaceAgent {
		t.Error("a bare install enabled the workspace agent")
	}
}
