package orchestrate

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// memoryFixtureInput is the deterministic RenderInput the memory-unit goldens are
// frozen against: identical to fixtureInput() but with the full Phase-18 memory config
// spine populated and MemoryEnabled=true so Render() appends the three memory units.
// The image digests are sourced THROUGH the orchestrate managed-service consts (D-02/
// D-04), never hand-typed in the test.
func memoryFixtureInput() RenderInput {
	return RenderInput{
		Backend: inference.VulkanBackend(),
		Cfg: config.VillaConfig{
			Model: "qwen3-35b-a3b-moe-64", Quant: "UD-Q4_K_M", Ctx: 131072, Backend: "vulkan",
			MemoryEnabled:  true,
			EmbeddingModel: "nomic-embed-text-v1.5",
			EmbeddingDim:   768,
			QdrantAddr:     "villa-qdrant",
			QdrantPort:     6333,
			EmbedAddr:      "villa-embed",
			EmbedPort:      8080,
		},
		ModelFile: "qwen3-35b-a3b-moe-64.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
	}
}

// TestRenderQdrant: with memory on, the villa-qdrant.container + villa-qdrant.volume
// units match their goldens byte-for-byte (regen with -update). INFRA-01.
func TestRenderQdrant(t *testing.T) {
	units, err := Render(memoryFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-qdrant.container")
	goldenCompare(t, "villa-qdrant.container.golden", c.Text)

	v := unitByName(t, units, "villa-qdrant.volume")
	goldenCompare(t, "villa-qdrant.volume.golden", v.Text)

	if !strings.Contains(c.Text, "Volume=villa-qdrant.volume:/qdrant/storage:Z") {
		t.Errorf("qdrant unit missing durable :Z volume mount:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "Image=docker.io/qdrant/qdrant:v1.18.2-unprivileged@sha256:") {
		t.Errorf("qdrant unit missing digest-pinned image:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "Network=villa.network") {
		t.Errorf("qdrant unit missing Network=villa.network:\n%s", c.Text)
	}
	if strings.Contains(c.Text, "Environment=") {
		t.Errorf("qdrant unit must carry no Environment= block:\n%s", c.Text)
	}
}

// TestRenderEmbed: with memory on, the villa-embed.container unit matches its golden
// byte-for-byte (regen with -update) and carries the load-bearing embeddings Exec +
// read-only shared-models mount. INFRA-02 / D-05.
func TestRenderEmbed(t *testing.T) {
	units, err := Render(memoryFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-embed.container")
	goldenCompare(t, "villa-embed.container.golden", c.Text)

	wantExec := "Exec=llama-server -m /models/nomic-embed-text-v1.5.Q8_0.gguf --embeddings --pooling mean -c 8192 --host 0.0.0.0 --port 8080"
	if !strings.Contains(c.Text, wantExec) {
		t.Errorf("embed unit missing the load-bearing embeddings Exec %q:\n%s", wantExec, c.Text)
	}
	if !strings.Contains(c.Text, "Volume=villa-models:/models:ro,z") {
		t.Errorf("embed unit missing :ro,z shared-models mount:\n%s", c.Text)
	}
}

// TestRenderByteIdenticalWhenMemoryOff: with memory off, Render returns EXACTLY the
// existing 5 units and none of the three memory unit names appear (D-11 byte-identity:
// the 5 existing goldens stay unchanged, proven by the existing render tests staying
// green plus this len/name regression).
func TestRenderByteIdenticalWhenMemoryOff(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(units) != 5 {
		t.Fatalf("memory off: Render returned %d units, want exactly 5: %v", len(units), unitNames(units))
	}
	for _, name := range []string{"villa-qdrant.container", "villa-qdrant.volume", "villa-embed.container"} {
		for _, u := range units {
			if u.Name == name {
				t.Errorf("memory off: Render must NOT emit %q (got %v)", name, unitNames(units))
			}
		}
	}
}

// TestMemoryUnitsNoPublishPort: T-19-01 — none of the three memory units publishes a
// host port (container-DNS only on villa.network, SC#4/D-10).
func TestMemoryUnitsNoPublishPort(t *testing.T) {
	units, err := Render(memoryFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, name := range []string{"villa-qdrant.container", "villa-qdrant.volume", "villa-embed.container"} {
		u := unitByName(t, units, name)
		if strings.Contains(u.Text, "PublishPort=") {
			t.Errorf("memory unit %q must not publish a host port (privacy leak, T-19-01):\n%s", name, u.Text)
		}
	}
}

// TestRenderEightUnitOrderWhenMemoryOn: with memory on, Render grows from 5 to 8 units
// in a fixed deterministic order — the existing 5 THEN villa-qdrant.container,
// villa-qdrant.volume, villa-embed.container.
func TestRenderEightUnitOrderWhenMemoryOn(t *testing.T) {
	units, err := Render(memoryFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := []string{
		"villa-llama.container",
		"villa.network",
		"villa-models.volume",
		"villa-openwebui.container",
		"villa-openwebui.volume",
		"villa-qdrant.container",
		"villa-qdrant.volume",
		"villa-embed.container",
	}
	got := unitNames(units)
	if len(got) != len(want) {
		t.Fatalf("memory on: Render returned %d units, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("unit[%d] = %q, want %q (full order: %v)", i, got[i], want[i], got)
		}
	}
}

// TestRenderConsumesMemoryView: Render is keyed off in.Cfg.MemoryEnabled and the
// rendered memory units carry the config-resolved container-DNS names (villa-qdrant /
// villa-embed) threaded through the D-11 resolved-values handoff (memory.RenderView).
func TestRenderConsumesMemoryView(t *testing.T) {
	units, err := Render(memoryFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	q := unitByName(t, units, "villa-qdrant.container")
	if !strings.Contains(q.Text, "ContainerName=villa-qdrant") {
		t.Errorf("qdrant unit missing resolved DNS name ContainerName=villa-qdrant:\n%s", q.Text)
	}
	e := unitByName(t, units, "villa-embed.container")
	if !strings.Contains(e.Text, "ContainerName=villa-embed") {
		t.Errorf("embed unit missing resolved DNS name ContainerName=villa-embed:\n%s", e.Text)
	}
}
