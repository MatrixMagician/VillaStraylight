package install

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// sequence_test.go holds install to its ordering rules. They were load-bearing and
// existed only as comments inside one long function, with nothing asserting them.

func units() Units {
	return Units{
		Inference: "villa-llama.service",
		ChatUI:    "villa-openwebui.service",
		Qdrant:    "villa-qdrant.service",
		Embed:     "villa-embed.service",
		Searxng:   "villa-searxng.service",
		Websafe:   "villa-websafe.service",
	}
}

// before asserts that step a happens before step b, reporting the whole sequence on
// failure so the violation is readable.
func before(t *testing.T, s Sequence, aKind StepKind, aSvc string, bKind StepKind, bSvc string, why string) {
	t.Helper()
	ai := s.IndexOf(aKind, aSvc)
	bi := s.IndexOf(bKind, bSvc)
	if ai == -1 {
		t.Fatalf("sequence has no %s %s step; steps = %v", aKind, aSvc, describe(s))
	}
	if bi == -1 {
		t.Fatalf("sequence has no %s %s step; steps = %v", bKind, bSvc, describe(s))
	}
	if ai >= bi {
		t.Errorf("%s %s must come before %s %s: %s\nsteps = %v", aKind, aSvc, bKind, bSvc, why, describe(s))
	}
}

func describe(s Sequence) []string {
	var out []string
	for _, st := range s.Steps {
		label := st.Kind.String()
		if st.Service != "" {
			label += ":" + st.Service
		}
		if st.Secret != "" {
			label += ":" + st.Secret
		}
		out = append(out, label)
	}
	return out
}

// TestSecretsLandBeforeTheUnitsThatReferenceThem is the first ordering rule. A unit
// that names an EnvironmentFile which does not exist cannot be started at all, so
// generating the secret after writing the units would produce a stack that refuses
// to come up.
func TestSecretsLandBeforeTheUnitsThatReferenceThem(t *testing.T) {
	s := BuildSequence(Gates{WebSearch: true}, units(), true)

	before(t, s, StepGenerateSecret, "", StepWriteUnits, "",
		"a unit must never be written naming a secret that has not been generated")
}

// TestBearerFileIsWrittenBeforeTheChatUIStarts is the rule that a real failure came
// from. The chat UI's unit references the bearer env file, and systemd refuses to
// start a unit whose EnvironmentFile target is absent, so writing the file after the
// start is a start that fails.
func TestBearerFileIsWrittenBeforeTheChatUIStarts(t *testing.T) {
	s := BuildSequence(Gates{WebSearch: true}, units(), true)

	before(t, s, StepWriteSecretFile, "", StepStart, units().ChatUI,
		"systemd refuses to start a unit whose EnvironmentFile target does not exist")
}

// TestInferenceStartsBeforeTheChatUI: the chat UI must come up against a live
// backend, or its model picker is empty on first visit.
func TestInferenceStartsBeforeTheChatUI(t *testing.T) {
	for _, tc := range []struct {
		name  string
		gates Gates
	}{
		{"bare", Gates{}},
		{"memory on", Gates{Memory: true}},
		{"web search on", Gates{WebSearch: true}},
		{"everything on", Gates{Memory: true, WebSearch: true, Agent: true, CodingMode: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := BuildSequence(tc.gates, units(), true)
			before(t, s, StepStart, units().Inference, StepStart, units().ChatUI,
				"the chat UI must come up against a live backend")
		})
	}
}

// TestConfigIsPersistedBeforeUnitsAreWritten: config is the single source of truth
// and the units are rendered from it. Writing units first would leave a window where
// a `villa up` resolves a different config than the units on disk were built for.
func TestConfigIsPersistedBeforeUnitsAreWritten(t *testing.T) {
	s := BuildSequence(Gates{}, units(), false)

	before(t, s, StepPersistConfig, "", StepWriteUnits, "",
		"units are rendered from config, so config must be the source of truth first")
}

// TestVectorStoreStartsBeforeTheEmbedder: the embedder's peer must be reachable when
// it comes up.
func TestVectorStoreStartsBeforeTheEmbedder(t *testing.T) {
	s := BuildSequence(Gates{Memory: true}, units(), false)

	before(t, s, StepStart, units().Qdrant, StepStart, units().Embed,
		"the embedder must find the vector store reachable")
}

// TestProofsFollowTheStartsTheyProve: a proof run before its service starts would
// report on a stack that is not up yet.
func TestProofsFollowTheStartsTheyProve(t *testing.T) {
	s := BuildSequence(Gates{Memory: true, WebSearch: true}, units(), true)

	before(t, s, StepStart, units().Embed, StepProve, units().Embed,
		"a proof must observe a started service")
	before(t, s, StepStart, units().Searxng, StepProve, units().Searxng,
		"a proof must observe a started service")
}

// TestSubsystemOffProducesNoStepsForIt: a subsystem-off install must be exactly the
// sequence it always was, with no start, no secret and no proof for the absent
// subsystem.
func TestSubsystemOffProducesNoStepsForIt(t *testing.T) {
	s := BuildSequence(Gates{}, units(), true)

	want := []string{units().Inference, units().ChatUI}
	got := s.Services()
	if len(got) != len(want) {
		t.Fatalf("a bare install starts %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("start[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if s.IndexOf(StepGenerateSecret, "") != -1 || s.IndexOf(StepWriteSecretFile, "") != -1 {
		t.Errorf("a web-search-off install must touch no secret; steps = %v", describe(s))
	}
	if s.IndexOf(StepProve, "") != -1 {
		t.Errorf("a bare install runs no subsystem proof; steps = %v", describe(s))
	}
}

// TestSecretIsGeneratedOncePerOptIn: a re-install reuses the existing bearer rather
// than churning it, which would break the trust between the chat UI and the loader
// on every install.
func TestSecretIsGeneratedOncePerOptIn(t *testing.T) {
	first := BuildSequence(Gates{WebSearch: true}, units(), true)
	if first.IndexOf(StepGenerateSecret, "") == -1 {
		t.Error("a first web-search opt-in must generate the bearer")
	}

	reinstall := BuildSequence(Gates{WebSearch: true}, units(), false)
	if reinstall.IndexOf(StepGenerateSecret, "") != -1 {
		t.Error("a re-install must reuse the persisted bearer, not churn the trust")
	}
	if reinstall.IndexOf(StepWriteSecretFile, "") == -1 {
		t.Error("a re-install must still write the env file — its target must exist regardless of unit churn")
	}
}

// TestEveryStartIsGatedOnItsUnit: gating a start on the subsystem flag alone would
// start a unit systemd has never seen on a host where the render produced no such
// unit.
func TestEveryStartIsGatedOnItsUnit(t *testing.T) {
	s := BuildSequence(Gates{Memory: true, WebSearch: true}, units(), true)

	for _, st := range s.Steps {
		if st.Kind != StepStart {
			continue
		}
		if st.RequiresUnit == "" {
			t.Errorf("start %s is ungated; install would start a unit systemd may never have seen", st.Service)
		}
		if st.RequiresUnit != st.Service {
			t.Errorf("start %s is gated on %q, want its own unit", st.Service, st.RequiresUnit)
		}
	}
}

// TestUnitPresentCoversBothPlanHalves: a unit already on disk and unchanged is still
// present. Checking only the changed set would skip the start on every re-install
// where nothing about that unit moved.
func TestUnitPresentCoversBothPlanHalves(t *testing.T) {
	plan := orchestrate.Plan{
		Changed:   []orchestrate.Unit{{Name: "villa-llama.container"}},
		Unchanged: []orchestrate.Unit{{Name: "villa-qdrant.container"}},
	}

	if !UnitPresent(plan, "villa-llama.container") {
		t.Error("a changed unit must count as present")
	}
	if !UnitPresent(plan, "villa-qdrant.container") {
		t.Error("an unchanged unit is still on disk and must count as present")
	}
	if UnitPresent(plan, "villa-searxng.container") {
		t.Error("a unit in neither half must not count as present")
	}
}

// TestPreparationPrecedesActivation is the invariant behind the whole ordering.
//
// The sequence is not globally monotonic by phase, and should not be: each optional
// subsystem legitimately starts its services and then proves them, so a start
// follows a proof belonging to an earlier subsystem. What must hold is the boundary
// between PREPARATION — persisting config, generating secrets, writing units and
// secret files — and ACTIVATION, which is every start and proof. Nothing may be
// prepared after the stack has begun coming up, because a unit or secret written
// then is a change the already-started services never saw.
func TestPreparationPrecedesActivation(t *testing.T) {
	prep := map[StepKind]bool{
		StepGenerateSecret:  true,
		StepPersistConfig:   true,
		StepWriteSecretFile: true,
		StepWriteUnits:      true,
	}

	for _, tc := range []struct {
		name  string
		gates Gates
	}{
		{"bare", Gates{}},
		{"memory", Gates{Memory: true}},
		{"web search", Gates{WebSearch: true}},
		{"agent", Gates{Agent: true, CodingMode: true}},
		{"everything", Gates{Memory: true, WebSearch: true, Agent: true, CodingMode: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := BuildSequence(tc.gates, units(), true)
			activated := false
			for _, st := range s.Steps {
				if !prep[st.Kind] {
					activated = true
					continue
				}
				if activated {
					t.Errorf("%s runs after the stack started coming up; a unit or secret written then is a change the started services never saw\nsteps = %v",
						st.Kind, describe(s))
				}
			}
		})
	}
}

// TestActivationOrdersStartsBeforeTheirProofs: within activation, each subsystem
// starts its services and only then proves them.
func TestActivationOrdersStartsBeforeTheirProofs(t *testing.T) {
	s := BuildSequence(Gates{Memory: true, WebSearch: true, Agent: true}, units(), true)

	// Every prove step must follow a start of the same service, or of the inference
	// service in the agent case (which proves the already-running coder).
	for i, st := range s.Steps {
		if st.Kind != StepProve {
			continue
		}
		startedBefore := false
		for _, earlier := range s.Steps[:i] {
			if earlier.Kind == StepStart && earlier.Service == st.Service {
				startedBefore = true
			}
		}
		if !startedBefore {
			t.Errorf("prove %s runs with no prior start of it; the proof would report on a service that is not up\nsteps = %v",
				st.Service, describe(s))
		}
	}
}

// TestAssertStartOrderCatchesDrift is what keeps the planned sequence from becoming
// decoration. Without this check, the ordering tests above would keep passing while
// the command tier drifted out from under them.
func TestAssertStartOrderCatchesDrift(t *testing.T) {
	seq := BuildSequence(Gates{Memory: true}, units(), false)
	u := units()

	t.Run("the planned order passes", func(t *testing.T) {
		if err := AssertStartOrder(seq, []string{u.Inference, u.ChatUI, u.Qdrant, u.Embed}); err != nil {
			t.Errorf("the planned order must pass, got %v", err)
		}
	})

	t.Run("a skipped start is not a violation", func(t *testing.T) {
		// A unit absent from the written plan is deliberately not started; that gate
		// is one of the rules, not a breach of them.
		if err := AssertStartOrder(seq, []string{u.Inference, u.ChatUI}); err != nil {
			t.Errorf("a gated-off start must not be reported as drift, got %v", err)
		}
	})

	t.Run("starting the chat UI before inference is caught", func(t *testing.T) {
		err := AssertStartOrder(seq, []string{u.ChatUI, u.Inference})
		if err == nil {
			t.Fatal("the chat UI starting before inference must be caught")
		}
		if !strings.Contains(err.Error(), "out of the planned order") {
			t.Errorf("error must name the ordering violation, got %q", err)
		}
	})

	t.Run("starting the embedder before the vector store is caught", func(t *testing.T) {
		if err := AssertStartOrder(seq, []string{u.Inference, u.ChatUI, u.Embed, u.Qdrant}); err == nil {
			t.Error("the embedder starting before the vector store must be caught")
		}
	})

	t.Run("an unplanned start is caught", func(t *testing.T) {
		err := AssertStartOrder(seq, []string{u.Inference, u.ChatUI, u.Searxng})
		if err == nil {
			t.Fatal("starting a service the plan never expected must be caught")
		}
		if !strings.Contains(err.Error(), "never planned") {
			t.Errorf("error must say the start was unplanned, got %q", err)
		}
	})
}
