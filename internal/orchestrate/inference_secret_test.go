package orchestrate

import (
	"strings"
	"testing"
)

// TestRenderInferenceSecretEnv: the secret env-file render is the single-source
// format the installer writes at 0600 — TWO fixed lines (LLAMA_API_KEY for
// llama-server's own env contract, OPENAI_API_KEY for Open WebUI's), the bare
// filename llama.env, both carrying the SAME secret value (GHSA-qxg9, ADR-0011:
// this file is the 0600 target, NOT a 0644 file).
func TestRenderInferenceSecretEnv(t *testing.T) {
	name, text := RenderInferenceSecretEnv("hunter2hunter2")
	if name != "llama.env" {
		t.Errorf("RenderInferenceSecretEnv name = %q, want llama.env", name)
	}
	want := "LLAMA_API_KEY=hunter2hunter2\nOPENAI_API_KEY=hunter2hunter2\n"
	if text != want {
		t.Errorf("RenderInferenceSecretEnv body = %q, want %q", text, want)
	}
}

// TestInferenceSecretEnvFilePathContract: every rendered llama-server-adjacent
// unit (the primary, every resident slot, Open WebUI, and villa-inferproxy)
// references the EXPORTED InferenceSecretEnvFilePath() — the exact path the
// installer writes at 0600 — and UNCONDITIONALLY, unlike the websafe bearer,
// because inference has no opt-in gate.
func TestInferenceSecretEnvFilePathContract(t *testing.T) {
	units, err := Render(sandboxOnFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "EnvironmentFile=" + InferenceSecretEnvFilePath()
	for _, name := range []string{"villa-llama.container", "villa-openwebui.container", InferproxyContainerUnitName()} {
		c := unitByName(t, units, name)
		if !strings.Contains(c.Text, want) {
			t.Errorf("%s does not reference the contract path %q:\n%s", name, want, c.Text)
		}
	}
	if !strings.Contains(InferenceSecretEnvFilePath(), "llama.env") {
		t.Errorf("InferenceSecretEnvFilePath() %q must end at the llama.env file", InferenceSecretEnvFilePath())
	}
}

// TestInferenceSecretEnvFilePathCoversResidentSlots: a resident slot's rendered
// unit gets the SAME EnvironmentFile= as the primary — one shared secret
// protects the whole inference fleet, set once at parseContainerArgs
// (render.go), not re-added per resident call site.
func TestInferenceSecretEnvFilePathCoversResidentSlots(t *testing.T) {
	units, err := Render(residentFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "EnvironmentFile=" + InferenceSecretEnvFilePath()
	c := unitByName(t, units, "villa-llama-qwen3-6-35b-a3b.container")
	if !strings.Contains(c.Text, want) {
		t.Errorf("resident unit does not reference the contract path %q:\n%s", want, c.Text)
	}
}
