package subsystem

import "testing"

// TestSubsystemsMoveAsTheirProofUnit: the proof unit is the verify verb's scope,
// so memory's units and services are Qdrant, the embedder, the reranker AND the
// extractor.
// Splitting them would produce a pairing with no proof and no meaning.
func TestSubsystemsMoveAsTheirProofUnit(t *testing.T) {
	units, services := Memory.EveryUnit()
	if len(units) != 4 || len(services) != 4 {
		t.Errorf("memory moves %d units / %d services, want the vector store, the embedder, the reranker and the extractor", len(units), len(services))
	}
	units, services = WebSearch.EveryUnit()
	if len(units) != 2 || len(services) != 2 {
		t.Errorf("web search moves %d units / %d services, want SearXNG and the web guard together", len(units), len(services))
	}
	units, services = Voice.EveryUnit()
	if len(units) != 2 || len(services) != 2 {
		t.Errorf("voice moves %d units / %d services, want villa-stt and villa-tts together", len(units), len(services))
	}
	// The agent is a binary, not a unit: nothing to render and nothing to restart.
	units, services = Agent.EveryUnit()
	if len(units) != 0 || len(services) != 0 {
		t.Errorf("the agent subsystem renders units (%v/%v); the Crush binary is a file", units, services)
	}
}

// TestBudgetsArePerSubsystemAndNonZero: a total cap would make failures depend
// on ordering, so the last subsystem gets blamed for time the first four spent.
func TestBudgetsArePerSubsystemAndNonZero(t *testing.T) {
	for _, k := range Every {
		if k == CodingMode {
			continue
		}
		if b := k.UpdateBudget(); b <= 0 {
			t.Errorf("%v has a non-positive budget", k)
		}
	}
	// Inference gets the longest, because the residency proof is the expensive part
	// and it runs twice.
	if Inference.UpdateBudget() <= Chat.UpdateBudget() {
		t.Error("inference does not get a longer budget than chat, despite running the residency proof twice")
	}
}

// TestImageBudgetMatchesInference: the image proof eager-loads about 9 GB and runs a
// real generation, before and after the mutation, so its budget is inference's, not
// a managed service's.
func TestImageBudgetMatchesInference(t *testing.T) {
	if Image.UpdateBudget() != Inference.UpdateBudget() {
		t.Errorf("image budget %v != inference budget %v; the image proof loads and generates twice", Image.UpdateBudget(), Inference.UpdateBudget())
	}
}
