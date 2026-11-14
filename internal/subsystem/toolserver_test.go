package subsystem

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestToolWebOnNeedsWebSearch: the web unit searches through SearXNG and fetches
// through villa-websafe's guard, so the tool server alone does not render it.
func TestToolWebOnNeedsWebSearch(t *testing.T) {
	for _, tc := range []struct {
		tool, web, want bool
	}{
		{false, false, false},
		{false, true, false},
		{true, false, false},
		{true, true, true},
	} {
		cfg := config.VillaConfig{ToolServer: tc.tool, WebSearchEnabled: tc.web}
		if got := ToolWebOn(cfg); got != tc.want {
			t.Errorf("ToolWebOn(tool_server=%v, web_search=%v) = %v, want %v", tc.tool, tc.web, got, tc.want)
		}
	}
}

// TestToolRecallOnNeedsMemory: the recall unit reads Qdrant and the embedder, so
// the tool server alone does not render it.
func TestToolRecallOnNeedsMemory(t *testing.T) {
	for _, tc := range []struct {
		tool, mem, want bool
	}{
		{false, false, false},
		{false, true, false},
		{true, false, false},
		{true, true, true},
	} {
		cfg := config.VillaConfig{ToolServer: tc.tool, MemoryEnabled: tc.mem}
		if got := ToolRecallOn(cfg); got != tc.want {
			t.Errorf("ToolRecallOn(tool_server=%v, memory=%v) = %v, want %v", tc.tool, tc.mem, got, tc.want)
		}
	}
}
