package install

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// TestInstallStagesAndStartsTheReranker guards ADR-0028's install story: with
// memory on, install stages the reranker's weights when absent, writes the
// reranker gate into the persisted config, starts villa-rerank after the embedder
// and only then runs the memory proof. With memory off none of it happens and the
// gate stays off.
func TestInstallStagesAndStartsTheReranker(t *testing.T) {
	t.Run("memory on stages, gates, starts in order, then proves", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.rerankPresent = false

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("exit = %d, want 0", code)
		}
		if f.rerankEnsureCalls != 1 {
			t.Errorf("absent reranker weights must be staged once, calls = %d", f.rerankEnsureCalls)
		}
		if !f.savedCfg.Reranker {
			t.Errorf("install must persist reranker = true beside memory_enabled, saved %+v", f.savedCfg)
		}
		ensureIdx := idx(f.callOrder, "ensureRerankModel")
		embedIdx := idx(f.callOrder, "start:"+embedServiceName)
		rerankIdx := idx(f.callOrder, "start:villa-rerank.service")
		proofIdx := idx(f.callOrder, "memoryProof")
		if ensureIdx < 0 || embedIdx < 0 || rerankIdx < 0 || proofIdx < 0 {
			t.Fatalf("missing expected events in callOrder = %v", f.callOrder)
		}
		if ensureIdx >= rerankIdx || embedIdx >= rerankIdx || rerankIdx >= proofIdx {
			t.Errorf("ordering wrong: stage(%d) and embed(%d) must precede rerank(%d), which must precede the proof(%d); callOrder = %v",
				ensureIdx, embedIdx, rerankIdx, proofIdx, f.callOrder)
		}
	})

	t.Run("present weights are not re-pulled", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.rerankPresent = true

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("exit = %d, want 0", code)
		}
		if f.rerankEnsureCalls != 0 {
			t.Errorf("present reranker weights must not be re-pulled, calls = %d", f.rerankEnsureCalls)
		}
		if !contains(f.startOrder, "villa-rerank.service") {
			t.Errorf("the reranker must still start, startOrder = %v", f.startOrder)
		}
	})

	t.Run("memory off touches nothing and leaves the gate off", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = false
		f.rerankPresent = false

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("exit = %d, want 0", code)
		}
		if f.rerankEnsureCalls != 0 || f.rerankPresentCalls != 0 {
			t.Errorf("memory off must not touch the reranker weights (ensure=%d present=%d)", f.rerankEnsureCalls, f.rerankPresentCalls)
		}
		if contains(f.startOrder, "villa-rerank.service") {
			t.Errorf("memory off must not start the reranker, startOrder = %v", f.startOrder)
		}
		if f.savedCfg.Reranker {
			t.Errorf("memory off must leave reranker unset, saved %+v", f.savedCfg)
		}
	})

	t.Run("a missing reranker unit in the plan refuses before any memory start", func(t *testing.T) {
		units, plan := memoryUnits()
		units = units[:2]
		plan.Changed = units
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true

		if code, _, _ := f.run(Opts{}); code == exitPass {
			t.Fatal("a memory-on plan without the reranker unit must not pass")
		}
		if contains(f.startOrder, qdrantServiceName) {
			t.Errorf("the refusal must come before the memory starts, startOrder = %v", f.startOrder)
		}
	})
}

// TestRerankerStartsAfterTheEmbedderAndBeforeTheProof: the sequence places the
// reranker start between the embedder's start and the memory proof, so the proof
// observes a started reranker.
func TestRerankerStartsAfterTheEmbedderAndBeforeTheProof(t *testing.T) {
	s := BuildSequence(Gates{Memory: true}, units(), false)

	before(t, s, StepStart, units().Embed, StepStart, units().Rerank,
		"the reranker joins the memory stack after its embedder")
	before(t, s, StepStart, units().Rerank, StepProve, units().Embed,
		"the memory proof must observe a started reranker")
}

// TestDefaultUnitsNameTheReranker: the flow starts the reranker by the service name
// the subsystem declares, never a re-typed literal.
func TestDefaultUnitsNameTheReranker(t *testing.T) {
	if got := DefaultUnits().Rerank; got != "villa-rerank.service" {
		t.Errorf("DefaultUnits().Rerank = %q, want villa-rerank.service", got)
	}
}

// TestInstallPicksAgainstThePlannedReservations: the install that turns a gate on
// sizes its pick and its resource floor against the config it is about to
// persist, not the one it read. A memory-on host without the reranker and
// extractor keys is the upgrade case: the pick must already carry both rows, or
// install picks a model and a ctx for an envelope they then shrink.
func TestInstallPicksAgainstThePlannedReservations(t *testing.T) {
	units, plan := memoryUnits()
	f := newFakeDeps(t, units, plan, passChecks())
	f.memoryEnabled = true

	if code, _, _ := f.run(Opts{}); code != exitPass {
		t.Fatalf("exit = %d, want 0", code)
	}
	if got := rows(f.pickReservations); got != "embedding,reranker,extractor" {
		t.Errorf("Pick received reservations %q, want embedding,reranker,extractor for the config install persists", got)
	}

	// A web-search opt-in on the command line reserves the same way, before the fit.
	planned := PlannedReservations(config.VillaConfig{MemoryEnabled: true}, Opts{WebSearch: true})
	if got := rows(planned); got != "embedding,reranker,extractor,web_search" {
		t.Errorf("PlannedReservations with --web-search = %q, want embedding,reranker,extractor,web_search", got)
	}
}

// rows names the reservations in order.
func rows(res []recommend.Reservation) string {
	var names []string
	for _, r := range res {
		names = append(names, r.Name)
	}
	return strings.Join(names, ",")
}
