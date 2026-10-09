package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// TestEveryRenderedContainerHasAStatusRow: status gives a rendered unit with no
// liveStatusServices entry a row probed by nothing, so a new service reads as
// unknown forever and no test notices. Render the stack with every gate on, the way
// status renders it, and require a probe for every container.
func TestEveryRenderedContainerHasAStatusRow(t *testing.T) {
	cfg := config.VillaConfig{
		Model: "qwen3-35b-a3b-moe-64", Quant: "UD-Q4_K_M", Ctx: 131072, Backend: "vulkan",
		MemoryEnabled: true, EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDim: 768, Reranker: true, Extractor: true,
		WebSearchEnabled: true,
		AgentEnabled:     true,
		CodingMode:       true,
		WorkspaceAgent:   true,
		VoiceEnabled:     true,
		ImageEnabled:     true,
		ImageModel:       "z-image-turbo",
	}
	for _, k := range subsystem.All {
		if !subsystem.On(cfg, k) {
			t.Fatalf("the fixture leaves %v off; a new gate must be turned on here so its units are checked", k)
		}
	}
	units, err := orchestrate.Render(orchestrate.RenderInput{
		Backend:   inference.VulkanBackend(),
		Cfg:       cfg,
		ModelFile: "qwen3-35b-a3b-moe-64.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
		Image:     pinnedFixtureImage(),
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	probed := map[string]bool{}
	for _, svc := range liveStatusServices() {
		probed[svc.Unit] = svc.Probe != nil
	}
	containers := 0
	for _, u := range units {
		if !strings.HasSuffix(u.Name, ".container") {
			continue
		}
		containers++
		if svc := unitServiceName(u.Name); !probed[svc] {
			t.Errorf("%s renders but %s has no probed liveStatusServices row, so status reports it unknown", u.Name, svc)
		}
	}
	if containers == 0 {
		t.Fatal("the render produced no container units")
	}
}

// TestVoiceAndInferproxyProbes: each row probes its own unit. The voice pair maps
// the HTTP code (200 ready, 503 loading) at the URLs voice.Service composes, and the
// inferproxy's own 403 on its root reads as up, because the probe asks only whether
// the listener answers.
func TestVoiceAndInferproxyProbes(t *testing.T) {
	answers := map[string]string{
		"http://villa-stt:8081/":        "200",
		"http://villa-tts:8880/health":  "503",
		"http://villa-inferproxy:8091/": "403",
	}
	var probed []string
	swapMemoryProbeExec(t, func(_ context.Context, _ string, curlArgs ...string) ([]byte, int, error) {
		url := probeURLOf(curlArgs)
		probed = append(probed, url)
		code, ok := answers[url]
		if !ok {
			return nil, 7, errors.New("exit status 7")
		}
		return []byte(code), 0, nil
	})

	if got := liveSttHealth(); got != status.HealthReady {
		t.Errorf("liveSttHealth = %q, want ready; probed %q", got, probed)
	}
	if got := liveTtsHealth(); got != status.HealthLoading {
		t.Errorf("liveTtsHealth = %q, want loading; probed %q", got, probed)
	}
	if got := liveInferproxyHealth(); got != status.HealthReady {
		t.Errorf("liveInferproxyHealth = %q, want ready; probed %q", got, probed)
	}
	if len(probed) != 3 {
		t.Errorf("want one probe per unit (the voice pair refreshes once), got %q", probed)
	}
}
