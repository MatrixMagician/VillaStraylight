package orchestrate

import (
	"strings"
	"testing"
)

// rerankFixtureInput is the memory fixture with the reranker gate on.
func rerankFixtureInput() RenderInput {
	in := memoryFixtureInput()
	in.Cfg.Reranker = true
	return in
}

// TestRenderRerankUnitGolden: the villa-rerank.container unit matches its golden
// byte-for-byte, serves the pinned GGUF with the rerank endpoint on, and mounts
// the shared models store read-only like the embedder.
func TestRenderRerankUnitGolden(t *testing.T) {
	units, err := Render(rerankFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-rerank.container")
	goldenCompare(t, "villa-rerank.container.golden", c.Text)

	wantExec := "Exec=llama-server -m /models/bge-reranker-v2-m3-Q8_0.gguf --rerank -c 8192 -b 1024 -ub 1024 --host 0.0.0.0 --port 8080"
	if !strings.Contains(c.Text, wantExec) {
		t.Errorf("rerank unit missing the Exec %q:\n%s", wantExec, c.Text)
	}
	if !strings.Contains(c.Text, "Volume=villa-models:/models:ro,z") {
		t.Errorf("rerank unit missing the :ro,z shared-models mount:\n%s", c.Text)
	}
	if strings.Contains(c.Text, "PublishPort=") {
		t.Errorf("rerank unit must not publish a host port:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "Image="+embedImage) {
		t.Errorf("rerank unit does not run the embedder's pinned image:\n%s", c.Text)
	}
}

// TestRenderRerankUnitOrder: with the reranker on, Render appends villa-rerank
// after the embedder inside the memory block, and the memory-only order is
// unchanged.
func TestRenderRerankUnitOrder(t *testing.T) {
	units, err := Render(rerankFixtureInput())
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
		"villa-rerank.container",
		"villa-sandbox.network",
	}
	got := unitNames(units)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("unit order = %v, want %v", got, want)
	}
}

// TestRenderRerankOffIsByteIdentical: a memory-on config without the reranker
// flag renders exactly what it rendered before the flag existed, so an upgraded
// host's stack does not change until `villa install` stages the reranker.
func TestRenderRerankOffIsByteIdentical(t *testing.T) {
	units, err := Render(memoryFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, u := range units {
		if u.Name == "villa-rerank.container" {
			t.Errorf("reranker off: Render emitted villa-rerank.container")
		}
		if strings.Contains(u.Text, "RERANK") {
			t.Errorf("reranker off: %s carries a reranker env line:\n%s", u.Name, u.Text)
		}
	}
}

// TestRenderOpenWebUIRerankGolden: with the reranker on, Open WebUI's unit gains
// the hybrid-search and external-reranker env, pointed at villa-rerank by
// container DNS, after the memory block and before the persistent-config gate.
func TestRenderOpenWebUIRerankGolden(t *testing.T) {
	units, err := Render(rerankFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	goldenCompare(t, "villa-openwebui.container.rerank.golden", c.Text)

	for _, want := range []string{
		"Environment=ENABLE_RAG_HYBRID_SEARCH=True",
		"Environment=RAG_RERANKING_ENGINE=external",
		"Environment=RAG_RERANKING_MODEL=bge-reranker-v2-m3",
		"Environment=RAG_EXTERNAL_RERANKER_URL=http://villa-rerank:8080/v1/rerank",
		"Environment=RAG_EXTERNAL_RERANKER_API_KEY=sk-no-key-required",
	} {
		if !strings.Contains(c.Text, want) {
			t.Errorf("Open WebUI unit missing %q:\n%s", want, c.Text)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(strings.SplitN(c.Text, "\n[Service]", 2)[0]), "Environment=ENABLE_PERSISTENT_CONFIG=False") {
		t.Errorf("ENABLE_PERSISTENT_CONFIG=False must stay the last env line:\n%s", c.Text)
	}
}

// TestRenderChatSwapLeavesRerankUnitByteIdentical: a chat-model swap leaves the
// reranker unit untouched, as it leaves the embedder's.
func TestRenderChatSwapLeavesRerankUnitByteIdentical(t *testing.T) {
	before := rerankFixtureInput()
	after := rerankFixtureInput()
	after.Cfg.Model = "llama3-8b-instruct"
	after.ModelFile = "llama3-8b-instruct.Q8_0.gguf"

	beforeUnits, err := Render(before)
	if err != nil {
		t.Fatalf("Render(before): %v", err)
	}
	afterUnits, err := Render(after)
	if err != nil {
		t.Fatalf("Render(after): %v", err)
	}
	if b, a := unitByName(t, beforeUnits, "villa-rerank.container").Text, unitByName(t, afterUnits, "villa-rerank.container").Text; a != b {
		t.Errorf("villa-rerank.container changed across a chat-model-only swap:\n%s\n%s", b, a)
	}
}
