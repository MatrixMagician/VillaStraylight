package install

import "testing"

// TestToolUnitsStartAfterWhatTheyReadAndBeforeTheImageServer: the recall unit needs the
// embedder and Qdrant up, the web unit needs SearXNG, and the image server's eager load
// keeps its place last, so the start order matches the render order (ADR-0042).
func TestToolUnitsStartAfterWhatTheyReadAndBeforeTheImageServer(t *testing.T) {
	s := BuildSequence(Gates{Memory: true, WebSearch: true, Voice: true, Image: true, ToolServer: true}, units(), true)
	before(t, s, StepStart, units().Embed, StepStart, units().ToolsRecall, "the recall unit queries the embedder")
	before(t, s, StepStart, units().Qdrant, StepStart, units().ToolsRecall, "the recall unit queries Qdrant")
	before(t, s, StepStart, units().Searxng, StepStart, units().ToolsWeb, "the web unit queries SearXNG")
	before(t, s, StepStart, units().Tts, StepStart, units().ToolsWeb, "the tool units render after voice")
	before(t, s, StepStart, units().ToolsWeb, StepStart, units().ToolsRecall, "web then recall, as they render")
	before(t, s, StepStart, units().ToolsRecall, StepStart, units().Image, "the image server's load stays last")
	before(t, s, StepStart, units().ChatUI, StepStart, units().ToolsWeb, "the chat unit registers them, so it is up first")
	for _, svc := range []string{units().ToolsWeb, units().ToolsRecall} {
		i := s.IndexOf(StepStart, svc)
		if s.Steps[i].RequiresUnit != svc {
			t.Errorf("the %s start is not gated on its unit: %+v", svc, s.Steps[i])
		}
	}
	before(t, s, StepStart, units().ToolsRecall, StepProve, units().ToolsWeb, "one health wait follows both tool starts")
}

// TestToolServerWritesItsFilesBeforeItsStarts: the chat unit and each tool unit name a 0600
// file, so the files land before any of them starts.
func TestToolServerWritesItsFilesBeforeItsStarts(t *testing.T) {
	s := BuildSequence(Gates{WebSearch: true, ToolServer: true}, units(), true)
	idx := -1
	for j, st := range s.Steps {
		if st.Kind == StepWriteSecretFile && st.Secret == "tool-bearers" {
			idx = j
		}
	}
	if idx < 0 {
		t.Fatalf("no tool-bearers file step; steps = %v", describe(s))
	}
	if chat := s.IndexOf(StepStart, units().ChatUI); idx > chat {
		t.Errorf("the tool files land after the chat unit starts; steps = %v", describe(s))
	}
}

// TestToolServerOffAddsNoSteps: the gate off is the sequence it always was, and one
// derived gate off adds no step for its unit.
func TestToolServerOffAddsNoSteps(t *testing.T) {
	off := BuildSequence(Gates{Memory: true, WebSearch: true}, units(), true)
	for _, svc := range []string{units().ToolsWeb, units().ToolsRecall} {
		if off.IndexOf(StepStart, svc) != -1 || off.IndexOf(StepProve, svc) != -1 {
			t.Errorf("gate off still planned a step for %s: %v", svc, describe(off))
		}
	}
	webOnly := BuildSequence(Gates{WebSearch: true, ToolServer: true}, units(), true)
	if webOnly.IndexOf(StepStart, units().ToolsRecall) != -1 || webOnly.IndexOf(StepStart, units().ToolsWeb) == -1 {
		t.Errorf("web search alone must plan the web unit only: %v", describe(webOnly))
	}
}
