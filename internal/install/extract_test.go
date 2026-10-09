package install

import "testing"

// TestInstallTurnsOnAndStartsTheExtractor guards ADR-0033's install story: with
// memory on, install writes the extractor gate into the persisted config and
// starts villa-extract after the reranker and before the memory proof. There are
// no weights to stage. With memory off the gate stays off and nothing starts.
func TestInstallTurnsOnAndStartsTheExtractor(t *testing.T) {
	t.Run("memory on gates, starts after the reranker, then proves", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("exit = %d, want 0", code)
		}
		if !f.savedCfg.Extractor {
			t.Errorf("install must persist extractor = true beside memory_enabled, saved %+v", f.savedCfg)
		}
		rerankIdx := idx(f.callOrder, "start:villa-rerank.service")
		extractIdx := idx(f.callOrder, "start:villa-extract.service")
		proofIdx := idx(f.callOrder, "memoryProof")
		if rerankIdx < 0 || extractIdx < 0 || proofIdx < 0 {
			t.Fatalf("missing expected events in callOrder = %v", f.callOrder)
		}
		if rerankIdx >= extractIdx || extractIdx >= proofIdx {
			t.Errorf("ordering wrong: rerank(%d) must precede extract(%d), which must precede the proof(%d); callOrder = %v",
				rerankIdx, extractIdx, proofIdx, f.callOrder)
		}
	})

	t.Run("memory off leaves the gate off and starts nothing", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = false

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("exit = %d, want 0", code)
		}
		if contains(f.startOrder, "villa-extract.service") {
			t.Errorf("memory off must not start the extractor, startOrder = %v", f.startOrder)
		}
		if f.savedCfg.Extractor {
			t.Errorf("memory off must leave extractor unset, saved %+v", f.savedCfg)
		}
	})

	t.Run("a missing extractor unit in the plan refuses before any memory start", func(t *testing.T) {
		units, plan := memoryUnits()
		units = units[:len(units)-1]
		plan.Changed = units
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true

		if code, _, _ := f.run(Opts{}); code == exitPass {
			t.Fatal("a memory-on plan without the extractor unit must not pass")
		}
		if contains(f.startOrder, qdrantServiceName) {
			t.Errorf("the refusal must come before the memory starts, startOrder = %v", f.startOrder)
		}
	})
}

// TestExtractorStartsAfterTheRerankerAndBeforeTheProof: the sequence places the
// extractor start between the reranker's start and the memory proof, so the proof
// observes a started extractor.
func TestExtractorStartsAfterTheRerankerAndBeforeTheProof(t *testing.T) {
	s := BuildSequence(Gates{Memory: true}, units(), false)

	before(t, s, StepStart, units().Rerank, StepStart, units().Extract,
		"the extractor joins the memory stack after the reranker")
	before(t, s, StepStart, units().Extract, StepProve, units().Embed,
		"the memory proof must observe a started extractor")
}

// TestDefaultUnitsNameTheExtractor: the flow starts the extractor by the service
// name the subsystem declares, never a re-typed literal.
func TestDefaultUnitsNameTheExtractor(t *testing.T) {
	if got := DefaultUnits().Extract; got != "villa-extract.service" {
		t.Errorf("DefaultUnits().Extract = %q, want villa-extract.service", got)
	}
}
