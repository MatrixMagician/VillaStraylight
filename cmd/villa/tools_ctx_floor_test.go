package main

// tools_ctx_floor_test.go closes the loop issue #192 found open: the ctx floor
// `tools-mode enter` refuses on is the floor livePinnedRender actually serves.
// Seven verbs render through that one function, so the floor is asserted there
// rather than once per caller.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// agentCtxCatalogFile writes a catalog with one CHAT entry declaring an agent
// context and one declaring none. Catalog validation constrains only role "coder"
// entries, so a chat entry may carry agent_ctx — which is what the floor reads and
// what today's seed does not yet supply.
func agentCtxCatalogFile(t *testing.T) string {
	t.Helper()
	entry := func(id string, agentCtx int) string {
		extra := ""
		if agentCtx > 0 {
			extra = fmt.Sprintf(`"agent_ctx": %d,`, agentCtx)
		}
		return fmt.Sprintf(`{
      "id": %q,
      "display_name": "Fixture",
      "quant": "Q4_K_M",
      "weight_bytes": 5000000000,
      "n_layers": 32,
      "n_kv_heads": 8,
      "head_dim": 128,
      "kv_bytes_per_elem": 2,
      "default_ctx": 16384,
      "min_envelope_bytes": 7000000000,
      "tier_gb": 16,
      "unified_memory_safe": true,
      "backend_default": "vulkan",
      "bootstrap": false,
      %s
      "shards": [{"url": "https://example.invalid/a.gguf", "filename": "a.gguf", "sha256": "00", "size_bytes": 1}]
    }`, id, extra)
	}
	body := fmt.Sprintf(`{"schema_version": 4, "catalog_version": "test", "models": [%s, %s]}`,
		entry("has-agent-ctx", 65536), entry("no-agent-ctx", 0))
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	return path
}

// servedCtx returns the single -c argument on a rendered unit's Exec line, failing
// the test when the unit carries anything other than exactly one.
func servedCtx(t *testing.T, unit string) string {
	t.Helper()
	if n := strings.Count(unit, "-c "); n != 1 {
		t.Fatalf("unit must carry exactly one -c, got %d:\n%s", n, unit)
	}
	sc := bufio.NewScanner(strings.NewReader(unit))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "Exec=") {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "-c" && i+1 < len(fields) {
				return fields[i+1]
			}
		}
	}
	t.Fatalf("no -c on any Exec line:\n%s", unit)
	return ""
}

// TestLivePinnedRenderServesTheToolsCtxFloor is the fix for #192: under tools mode
// the rendered chat unit serves max(cfg.Ctx, the entry's agent_ctx). No caller sets
// RenderInput.AgentCtx for the chat entry, so before the floor was resolved inside
// livePinnedRender the served ctx was always cfg.Ctx and `tools-mode enter` refused
// on a number nothing rendered.
func TestLivePinnedRenderServesTheToolsCtxFloor(t *testing.T) {
	catalogPath := agentCtxCatalogFile(t)
	backend, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatalf("BackendFor: %v", err)
	}

	cases := []struct {
		name    string
		model   string
		tools   bool
		ctx     int
		wantCtx string
	}{
		{name: "the agent ctx raises a smaller chat ctx", model: "has-agent-ctx", tools: true, ctx: 8192, wantCtx: "65536"},
		{name: "tools off never applies the floor", model: "has-agent-ctx", tools: false, ctx: 8192, wantCtx: "8192"},
		{name: "a larger chat ctx is never lowered", model: "has-agent-ctx", tools: true, ctx: 131072, wantCtx: "131072"},
		{name: "an entry declaring none leaves cfg.Ctx alone", model: "no-agent-ctx", tools: true, ctx: 8192, wantCtx: "8192"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())

			units, err := livePinnedRender(orchestrate.RenderInput{
				Backend: backend,
				Cfg: config.VillaConfig{
					Model: tc.model, Quant: "Q4_K_M", Ctx: tc.ctx, Backend: "vulkan",
					ToolsMode: tc.tools, CatalogPath: catalogPath,
				},
				ModelFile: "a.gguf",
				ModelsDir: "/models",
			})
			if err != nil {
				t.Fatalf("livePinnedRender: %v", err)
			}
			var chat string
			for _, u := range units {
				if u.Name == "villa-llama.container" {
					chat = u.Text
				}
			}
			if chat == "" {
				t.Fatal("no villa-llama.container rendered")
			}
			if got := servedCtx(t, chat); got != tc.wantCtx {
				t.Errorf("served ctx = %s, want %s:\n%s", got, tc.wantCtx, chat)
			}
		})
	}
}

// TestLiveAgentCtxResolvesTheServedEntry covers the resolver both the fit guard and
// the render read their floor from, so the two cannot disagree about it. An entry
// the catalog does not carry answers 0 rather than refusing: villa renders for
// models it did not pick.
func TestLiveAgentCtxResolvesTheServedEntry(t *testing.T) {
	cfg := config.VillaConfig{Model: "has-agent-ctx", Ctx: 8192, CatalogPath: agentCtxCatalogFile(t)}
	got, err := liveAgentCtx(cfg)
	if err != nil {
		t.Fatalf("liveAgentCtx: %v", err)
	}
	if got != 65536 {
		t.Errorf("liveAgentCtx = %d, want the entry's agent_ctx 65536", got)
	}

	cfg.Model = "absent-from-the-catalog"
	if got, err = liveAgentCtx(cfg); err != nil || got != 0 {
		t.Errorf("liveAgentCtx for an absent entry = (%d, %v), want (0, nil)", got, err)
	}
}
