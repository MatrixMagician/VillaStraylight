package orchestrate

import (
	"strings"
	"testing"
)

// visionFixtureInput mirrors speculationFixtureInput but carries the projector
// filename, exactly what livePinnedRender populates once the config says vision.
func visionFixtureInput(t *testing.T, backend string) RenderInput {
	t.Helper()
	in := fixtureInput()
	if backend == "rocm" {
		in = rocmFixtureInput(t)
	}
	in.Cfg.Vision = true
	in.Projector = "Qwen3.6-35B-A3B-mmproj-F16.gguf"
	return in
}

// TestRenderVisionVulkan: the Vulkan unit rendered with a projector matches the
// new append-only golden.
func TestRenderVisionVulkan(t *testing.T) {
	units, err := Render(visionFixtureInput(t, "vulkan"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	goldenCompare(t, "villa-llama-vision.container.golden", unitByName(t, units, "villa-llama.container").Text)
}

// TestRenderVisionROCm: the same delta on the ROCm unit.
func TestRenderVisionROCm(t *testing.T) {
	units, err := Render(visionFixtureInput(t, "rocm"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	goldenCompare(t, "villa-llama-rocm-vision.container.golden", unitByName(t, units, "villa-llama.container").Text)
}

// TestRenderResidentUnitsIgnoreProjector asserts a resident slot renders text-only
// even when the primary unit has vision: a slot's projector is not the primary
// model's, and claiming one would point --mmproj at the wrong file.
func TestRenderResidentUnitsIgnoreProjector(t *testing.T) {
	in := visionFixtureInput(t, "vulkan")
	in.Resident = []ResidentUnit{{Model: "alt", ModelFile: "alt.gguf", Ctx: 8192, Port: 8081}}
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	u := unitByName(t, units, "villa-llama-alt.container")
	if strings.Contains(u.Text, "--mmproj") {
		t.Errorf("resident unit carries the projector delta:\n%s", u.Text)
	}
}
