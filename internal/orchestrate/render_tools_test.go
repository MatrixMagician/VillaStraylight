package orchestrate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// render_tools_test.go covers the tools-mode render delta: cfg.ToolsMode raises
// --jinja on the chat unit WITHOUT the coding-mode model swap, and the served
// context is the floor max(cfg.Ctx, agent ctx).

// toolsFixtureInput mirrors fixtureInput with tools mode on and an agent ctx
// resolved by the caller (RenderInput.AgentCtx, the same resolved-agent-ctx
// field coding mode uses; see the note on the tools branch in render.go).
func toolsFixtureInput(ctx, agentCtx int) RenderInput {
	in := fixtureInput()
	in.Cfg.Ctx = ctx
	in.Cfg.ToolsMode = true
	in.AgentCtx = agentCtx
	return in
}

// TestRenderToolsModeGolden: with tools mode on the rendered villa-llama unit equals
// the NEW villa-llama-tools.container.golden byte-for-byte. It carries --jinja and
// NOT the coding-mode sampling preset or --cache-reuse: tools mode is the chat model
// served with its own template, not a swap to the coder.
func TestRenderToolsModeGolden(t *testing.T) {
	units, err := Render(toolsFixtureInput(131072, 0))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")
	goldenCompare(t, "villa-llama-tools.container.golden", c.Text)

	if !strings.Contains(c.Text, "--jinja") {
		t.Errorf("tools-mode unit missing --jinja:\n%s", c.Text)
	}
	for _, unwanted := range []string{"--cache-reuse", "--temp ", "--top-p "} {
		if strings.Contains(c.Text, unwanted) {
			t.Errorf("tools-mode unit leaked coding-mode flag %q:\n%s", unwanted, c.Text)
		}
	}
}

// TestRenderToolsModeContextFloor: the served -c is max(cfg.Ctx, agent ctx), so a
// resolved agent ctx raises a smaller chat ctx and never lowers a larger one. An
// unresolved agent ctx (0) leaves cfg.Ctx alone, and with tools mode off the floor
// never applies at all.
func TestRenderToolsModeContextFloor(t *testing.T) {
	cases := []struct {
		name     string
		toolsOn  bool
		ctx      int
		agentCtx int
		want     int
	}{
		{name: "agent ctx raises", toolsOn: true, ctx: 8192, agentCtx: 65536, want: 65536},
		{name: "agent ctx unresolved", toolsOn: true, ctx: 8192, agentCtx: 0, want: 8192},
		{name: "cfg ctx already larger", toolsOn: true, ctx: 131072, agentCtx: 65536, want: 131072},
		{name: "tools off", toolsOn: false, ctx: 8192, agentCtx: 65536, want: 8192},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := toolsFixtureInput(tc.ctx, tc.agentCtx)
			in.Cfg.ToolsMode = tc.toolsOn
			units, err := Render(in)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			c := unitByName(t, units, "villa-llama.container")
			want := fmt.Sprintf("-c %d", tc.want)
			if !strings.Contains(c.Text, want) {
				t.Errorf("served ctx: want %q:\n%s", want, c.Text)
			}
			if strings.Count(c.Text, "-c ") != 1 {
				t.Errorf("unit must carry exactly one -c:\n%s", c.Text)
			}
		})
	}
}

// TestRenderCodingModeWinsOverToolsFloor: coding mode is a swap to the coder entry,
// so its resolved agent ctx is the served ctx OUTRIGHT — not a floor over the chat
// ctx. A larger cfg.Ctx must not leak into a coding-mode unit (Pitfall 1).
func TestRenderCodingModeWinsOverToolsFloor(t *testing.T) {
	in := codingFixtureInput(true)
	in.Cfg.ToolsMode = true
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")
	if !strings.Contains(c.Text, "-c 65536") {
		t.Errorf("coding unit must serve the coder agent ctx 65536:\n%s", c.Text)
	}
	if strings.Contains(c.Text, "-c 131072") {
		t.Errorf("coding unit must not serve the chat ctx 131072:\n%s", c.Text)
	}
}

// TestRenderToolsOffPathUnchanged is the opt-in guard: with tools mode absent the
// chat unit is the unchanged off-path golden, so every install that never sets the
// flag renders byte-identical units.
func TestRenderToolsOffPathUnchanged(t *testing.T) {
	in := fixtureInput()
	if in.Cfg.ToolsMode {
		t.Fatal("fixture must be tools-off")
	}
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")
	goldenCompare(t, "villa-llama.container.golden", c.Text)
	if strings.Contains(c.Text, "--jinja") {
		t.Errorf("tools-off unit leaked --jinja:\n%s", c.Text)
	}
}

// TestRenderToolsModeROCm: the delta is backend-symmetric, so the ROCm unit gains
// --jinja from the same gate rather than a second one.
func TestRenderToolsModeROCm(t *testing.T) {
	rocm, err := inference.BackendFor("rocm")
	if err != nil {
		t.Fatalf("BackendFor(rocm): %v", err)
	}
	in := RenderInput{
		Backend:   rocm,
		Cfg:       config.VillaConfig{Model: "qwen3-35b-a3b-moe-64", Quant: "UD-Q4_K_M", Ctx: 131072, Backend: "rocm", ToolsMode: true},
		ModelFile: "qwen3-35b-a3b-moe-64.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
	}
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")
	if !strings.Contains(c.Text, "--jinja") {
		t.Errorf("tools-mode ROCm unit missing --jinja:\n%s", c.Text)
	}
}
