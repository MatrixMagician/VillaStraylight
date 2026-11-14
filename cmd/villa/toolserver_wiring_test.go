package main

import (
	"path/filepath"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// TestLiveStackDepsWireTheToolServer: the stack apply's heal skips a seam that is nil, so
// the live wiring must carry all three, or a tool-server-on host would render a chat unit
// that names an env file nothing wrote (ADR-0042).
func TestLiveStackDepsWireTheToolServer(t *testing.T) {
	d := liveStackDeps()
	if d.WriteToolEnv == nil || d.WriteRecallScope == nil || d.RecallScopeDir == nil {
		t.Fatalf("liveStackDeps leaves a tool-server seam unwired: WriteToolEnv=%v WriteRecallScope=%v RecallScopeDir=%v",
			d.WriteToolEnv != nil, d.WriteRecallScope != nil, d.RecallScopeDir != nil)
	}
	if got := d.RecallScopeDir(); got != recall.ScopeDir() {
		t.Errorf("RecallScopeDir = %q, want the directory the scope is written to, %q", got, recall.ScopeDir())
	}
}

// TestToolsServeDefaultsMatchTheRenderedUnit: the recall unit mounts the scope directory
// at one path and `tools-serve` reads one file in it, and the unit's key variable is the
// one the command reads, so the render and the command cannot drift apart.
func TestToolsServeDefaultsMatchTheRenderedUnit(t *testing.T) {
	if want := filepath.Join(orchestrate.ToolsScopeMountDir, recall.ScopeFileName); toolsDefaultScopeFile != want {
		t.Errorf("tools-serve --scope-file default = %q, want %q", toolsDefaultScopeFile, want)
	}
	if toolsKeyEnv != orchestrate.ToolsKeyEnv {
		t.Errorf("tools-serve reads %q, the unit's env file carries %q", toolsKeyEnv, orchestrate.ToolsKeyEnv)
	}
}
