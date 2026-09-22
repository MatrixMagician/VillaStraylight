package recall

// index_run_test.go table-tests PrepareIndexRun: the invariant it guards is
// that BOTH pre-mutation guards (single-operator shared-recall exposure,
// embedding skew) fire before any stamp/rebuild decision is computed, and that
// --rebuild bypasses the skew guard while still clearing Chats and deciding
// whether the KB itself needs an id-preserving reset (#240, ADR-0012).

import "testing"

func TestPrepareIndexRun(t *testing.T) {
	stamped := State{
		KnowledgeID:    "kb1",
		KnowledgeName:  "villa-recall",
		EmbeddingModel: "nomic-embed-text-v1.5",
		EmbeddingDim:   768,
		Chats:          map[string]ChatState{"c1": {FileID: "f1"}},
	}

	tests := []struct {
		name string
		in   IndexRunInput
		// want* describe the expected IndexRunPlan.
		wantRefused        string
		wantResetKnowledge bool
		// wantChatsNil is checked only when wantRefused == "" (a refusal leaves
		// PreparedState at its zero value, which callers must never use).
		wantChatsCleared bool
	}{
		{
			name: "more than one human user without the ack refuses before any decision",
			in: IndexRunInput{
				HumanUserCount:  2,
				SharedRecallAck: false,
				State:           stamped,
				EmbeddingModel:  stamped.EmbeddingModel,
				EmbeddingDim:    stamped.EmbeddingDim,
			},
			wantRefused: "shared_recall",
		},
		{
			name: "more than one human user WITH the ack proceeds",
			in: IndexRunInput{
				HumanUserCount:  2,
				SharedRecallAck: true,
				State:           stamped,
				EmbeddingModel:  stamped.EmbeddingModel,
				EmbeddingDim:    stamped.EmbeddingDim,
			},
			wantRefused: "",
		},
		{
			name: "single human user needs no ack",
			in: IndexRunInput{
				HumanUserCount: 1,
				State:          stamped,
				EmbeddingModel: stamped.EmbeddingModel,
				EmbeddingDim:   stamped.EmbeddingDim,
			},
			wantRefused: "",
		},
		{
			name: "embedding skew without --rebuild refuses",
			in: IndexRunInput{
				HumanUserCount: 1,
				State:          stamped,
				EmbeddingModel: "other-embed-model",
				EmbeddingDim:   512,
			},
			wantRefused: "embedding_skew",
		},
		{
			name: "the shared-recall guard runs BEFORE the skew guard (both would fire)",
			in: IndexRunInput{
				HumanUserCount:  2,
				SharedRecallAck: false,
				State:           stamped,
				EmbeddingModel:  "other-embed-model",
				EmbeddingDim:    512,
			},
			wantRefused: "shared_recall",
		},
		{
			name: "--rebuild bypasses the skew guard and resets an existing KnowledgeID",
			in: IndexRunInput{
				HumanUserCount: 1,
				Rebuild:        true,
				State:          stamped,
				EmbeddingModel: "other-embed-model",
				EmbeddingDim:   512,
			},
			wantRefused:        "",
			wantResetKnowledge: true,
			wantChatsCleared:   true,
		},
		{
			name: "--rebuild with no recorded KnowledgeID does not ask for a reset",
			in: IndexRunInput{
				HumanUserCount: 1,
				Rebuild:        true,
				State:          State{}, // fresh install, no KB yet
				EmbeddingModel: "other-embed-model",
				EmbeddingDim:   512,
			},
			wantRefused:        "",
			wantResetKnowledge: false,
			wantChatsCleared:   true,
		},
		{
			name: "empty recorded stamp is typed-Unknown - never refuses",
			in: IndexRunInput{
				HumanUserCount: 1,
				State:          State{}, // no EmbeddingModel recorded
				EmbeddingModel: "other-embed-model",
				EmbeddingDim:   512,
			},
			wantRefused: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrepareIndexRun(tt.in)
			if got.Refused != tt.wantRefused {
				t.Fatalf("Refused = %q, want %q (message = %q)", got.Refused, tt.wantRefused, got.RefusalMessage)
			}
			if tt.wantRefused != "" {
				if got.RefusalMessage == "" {
					t.Fatalf("a refusal must carry a remediation message")
				}
				return
			}
			if got.ResetKnowledge != tt.wantResetKnowledge {
				t.Fatalf("ResetKnowledge = %v, want %v", got.ResetKnowledge, tt.wantResetKnowledge)
			}
			if tt.wantChatsCleared && len(got.PreparedState.Chats) != 0 {
				t.Fatalf("Chats not cleared on --rebuild: %+v", got.PreparedState.Chats)
			}
			if got.PreparedState.EmbeddingModel != tt.in.EmbeddingModel || got.PreparedState.EmbeddingDim != tt.in.EmbeddingDim {
				t.Fatalf("PreparedState did not stamp the configured embedding identity: got %q/%d",
					got.PreparedState.EmbeddingModel, got.PreparedState.EmbeddingDim)
			}
			if got.PreparedState.LastIndexCompletedAt != "" {
				t.Fatalf("PreparedState must clear LastIndexCompletedAt going into a new run, got %q", got.PreparedState.LastIndexCompletedAt)
			}
			if got.PreparedState.Chats == nil {
				t.Fatalf("PreparedState.Chats must never be nil (callers index into it directly)")
			}
		})
	}
}

// TestPrepareIndexRunStartedAtStamp locks that StartedAt flows through verbatim
// (no clock I/O inside PrepareIndexRun).
func TestPrepareIndexRunStartedAtStamp(t *testing.T) {
	got := PrepareIndexRun(IndexRunInput{
		HumanUserCount: 1,
		State:          State{},
		StartedAt:      "2026-06-10T12:00:00Z",
	})
	if got.PreparedState.LastIndexStartedAt != "2026-06-10T12:00:00Z" {
		t.Fatalf("LastIndexStartedAt = %q, want the caller-supplied stamp verbatim", got.PreparedState.LastIndexStartedAt)
	}
}
