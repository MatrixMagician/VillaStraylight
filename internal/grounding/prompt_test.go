package grounding

import (
	"strings"
	"testing"
)

// TestPromptCarriesTemperatureThinkingOffAndContent guards spec §6's two
// wire rules (temperature 0, enable_thinking:false) and that every source and
// the document reach the model.
func TestPromptCarriesTemperatureThinkingOffAndContent(t *testing.T) {
	doc := Document{Path: "workspace/memo.md", Content: "the memo body"}
	sources := []Source{
		{Path: "workspace/notes/a.md", Content: "note a body"},
		{Path: "workspace/notes/b.md", Content: "note b body"},
	}

	req := Prompt(doc, sources)

	if req.Temperature == nil || *req.Temperature != 0 {
		t.Fatalf("Temperature = %v, want pointer to 0", req.Temperature)
	}
	if v, ok := req.ChatTemplateKwargs["enable_thinking"]; !ok || v != false {
		t.Fatalf("ChatTemplateKwargs[enable_thinking] = %v, want false", v)
	}
	if len(req.Messages) == 0 {
		t.Fatal("Messages is empty, want at least one model-agnostic message")
	}
	if req.Model != "" {
		t.Fatalf("Model = %q, want unset (model-agnostic)", req.Model)
	}

	var all string
	for _, m := range req.Messages {
		all += m.Content
	}
	for _, want := range []string{doc.Content, sources[0].Path, sources[0].Content, sources[1].Path, sources[1].Content} {
		if !strings.Contains(all, want) {
			t.Errorf("prompt content missing %q", want)
		}
	}
}
