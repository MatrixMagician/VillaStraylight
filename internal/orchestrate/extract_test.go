package orchestrate

import (
	"strings"
	"testing"
)

// extractFixtureInput is the memory fixture with the extractor gate on.
func extractFixtureInput() RenderInput {
	in := memoryFixtureInput()
	in.Cfg.Extractor = true
	return in
}

// TestRenderExtractUnitGolden: the villa-extract.container unit matches its golden
// byte-for-byte, runs the pinned Tika image with the 1 GiB heap bound the
// footprint was measured under (ADR-0033), and carries no volume, no published
// port, no device and no Exec.
func TestRenderExtractUnitGolden(t *testing.T) {
	units, err := Render(extractFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-extract.container")
	goldenCompare(t, "villa-extract.container.golden", c.Text)

	for _, want := range []string{
		"ContainerName=villa-extract",
		"Image=docker.io/apache/tika:3.3.1.0-full@sha256:d8e6ed96260ad89307a93195a1b856102987a818ac648502f8efbaf313d32470",
		"Network=villa.network",
		"Environment=JAVA_TOOL_OPTIONS=-Xmx1g",
	} {
		if !strings.Contains(c.Text, want) {
			t.Errorf("extract unit missing %q:\n%s", want, c.Text)
		}
	}
	for _, absent := range []string{"Volume=", "PublishPort=", "AddDevice=", "Exec="} {
		if strings.Contains(c.Text, absent) {
			t.Errorf("extract unit must not carry %q:\n%s", absent, c.Text)
		}
	}
}

// TestRenderExtractUnitOrder: with the reranker and the extractor on, Render
// appends villa-extract after villa-rerank inside the memory block.
func TestRenderExtractUnitOrder(t *testing.T) {
	in := extractFixtureInput()
	in.Cfg.Reranker = true
	units, err := Render(in)
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
		"villa-extract.container",
		"villa-sandbox.network",
	}
	got := unitNames(units)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("unit order = %v, want %v", got, want)
	}
}

// TestRenderExtractOffIsByteIdentical: a memory-on config without the extractor
// flag renders no extractor unit and no Tika env, so an upgraded host's stack does
// not change until `villa install` turns the extractor on.
func TestRenderExtractOffIsByteIdentical(t *testing.T) {
	units, err := Render(memoryFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, u := range units {
		if u.Name == "villa-extract.container" {
			t.Errorf("extractor off: Render emitted villa-extract.container")
		}
		if strings.Contains(u.Text, "TIKA") || strings.Contains(u.Text, "CONTENT_EXTRACTION_ENGINE") {
			t.Errorf("extractor off: %s carries an extractor env line:\n%s", u.Name, u.Text)
		}
	}
}

// TestRenderOpenWebUIExtractGolden: with the extractor on, Open WebUI's unit
// selects Tika as its content-extraction engine at villa-extract by container DNS,
// and ENABLE_PERSISTENT_CONFIG=False stays the last env line, so these DB-backed
// settings are authoritative.
func TestRenderOpenWebUIExtractGolden(t *testing.T) {
	units, err := Render(extractFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	goldenCompare(t, "villa-openwebui.container.extract.golden", c.Text)

	for _, want := range []string{
		"Environment=CONTENT_EXTRACTION_ENGINE=tika",
		"Environment=TIKA_SERVER_URL=http://villa-extract:9998",
		"Environment=TIKA_SERVER_VERSION=3",
	} {
		if !strings.Contains(c.Text, want) {
			t.Errorf("Open WebUI unit missing %q:\n%s", want, c.Text)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(strings.SplitN(c.Text, "\n[Service]", 2)[0]), "Environment=ENABLE_PERSISTENT_CONFIG=False") {
		t.Errorf("ENABLE_PERSISTENT_CONFIG=False must stay the last env line:\n%s", c.Text)
	}
}

// TestRenderOpenWebUIExtractFollowsRerankAndPrecedesWebSearch: the extractor block
// sits after the reranker's and before web search's, so turning one on never
// reorders another's lines.
func TestRenderOpenWebUIExtractFollowsRerankAndPrecedesWebSearch(t *testing.T) {
	in := extractFixtureInput()
	in.Cfg.Reranker = true
	in.Cfg.WebSearchEnabled = true
	in.Cfg.WebSearchResultCount = 3
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	text := unitByName(t, units, "villa-openwebui.container").Text
	rerank := strings.Index(text, "Environment=RAG_EXTERNAL_RERANKER_API_KEY=")
	extract := strings.Index(text, "Environment=CONTENT_EXTRACTION_ENGINE=tika")
	web := strings.Index(text, "Environment=ENABLE_WEB_SEARCH=True")
	if rerank < 0 || extract < 0 || web < 0 || rerank >= extract || extract >= web {
		t.Errorf("env order wrong: rerank@%d extract@%d web@%d, want rerank < extract < web:\n%s", rerank, extract, web, text)
	}
}

// TestRenderChatSwapLeavesExtractUnitByteIdentical: a chat-model swap leaves the
// extractor unit untouched, as it leaves the embedder's.
func TestRenderChatSwapLeavesExtractUnitByteIdentical(t *testing.T) {
	before := extractFixtureInput()
	after := extractFixtureInput()
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
	if b, a := unitByName(t, beforeUnits, "villa-extract.container").Text, unitByName(t, afterUnits, "villa-extract.container").Text; a != b {
		t.Errorf("villa-extract.container changed across a chat-model-only swap:\n%s\n%s", b, a)
	}
}

// TestRenderExtractResolvesItsOwnPin: the extractor's image resolves under its own
// component, so a recorded effective pin for the extractor reaches the unit and a
// pin recorded for the embedder does not.
func TestRenderExtractResolvesItsOwnPin(t *testing.T) {
	in := extractFixtureInput()
	in.Pin = func(component string) string {
		switch component {
		case "extractor":
			return "example.invalid/tika@sha256:eeee"
		case "embedder":
			return "example.invalid/embed@sha256:dddd"
		}
		return ""
	}
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if text := unitByName(t, units, "villa-extract.container").Text; !strings.Contains(text, "Image=example.invalid/tika@sha256:eeee") {
		t.Errorf("extract unit did not run its effective pin:\n%s", text)
	}
}
