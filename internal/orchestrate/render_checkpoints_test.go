package orchestrate

import (
	"strings"
	"testing"
)

// TestRenderCapsCheckpointsForSlidingWindowModels guards ADR-0034 at the unit
// level: the primary unit and each resident unit carry --ctx-checkpoints 1 when
// their own model has sliding-window layers, and only then, so a slot never
// inherits the primary's cap or loses its own.
func TestRenderCapsCheckpointsForSlidingWindowModels(t *testing.T) {
	in := fixtureInput()
	in.SlidingWindow = true
	in.Resident = []ResidentUnit{
		{Model: "plain", ModelFile: "plain.gguf", Ctx: 8192, Port: 8081},
		{Model: "gemma", ModelFile: "gemma.gguf", Ctx: 8192, Port: 8082, SlidingWindow: true},
	}
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for name, want := range map[string]bool{
		"villa-llama.container":       true,
		"villa-llama-plain.container": false,
		"villa-llama-gemma.container": true,
	} {
		text := unitByName(t, units, name).Text
		if got := strings.Contains(text, " --ctx-checkpoints 1\n"); got != want {
			t.Errorf("%s carries --ctx-checkpoints 1 = %v, want %v:\n%s", name, got, want, text)
		}
	}

	in.SlidingWindow = false
	units, err = Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if text := unitByName(t, units, "villa-llama.container").Text; strings.Contains(text, "--ctx-checkpoints") {
		t.Errorf("primary unit without sliding-window layers carries --ctx-checkpoints:\n%s", text)
	}
}
