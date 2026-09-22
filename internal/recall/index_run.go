package recall

// index_run.go is the pure pre-mutation decision layer for `villa recall
// index` (#240, ADR-0012): the single-operator shared-recall disclosure guard
// and the embedding-skew guard used to be inlined in cmd/villa's
// runRecallIndex, interleaved with the HTTP calls. PrepareIndexRun holds
// BOTH guards plus the rebuild/stamp decision (what the persisted State should
// look like going into the run, and whether the Knowledge collection itself
// needs an id-preserving reset) as one pure function. It performs no I/O: the
// Open WebUI calls (ResetKnowledge / EnsureKnowledge, and everything after) stay
// in the command tier, driven by what this returns.

import "fmt"

// IndexRunInput is everything runRecallIndex knows before it starts mutating
// anything: the flags, the CURRENT persisted state, the config's embedding
// identity, the count of human (non-service-account) users on the box, and the
// clock stamp for the run about to start.
type IndexRunInput struct {
	Rebuild         bool
	SharedRecallAck bool
	// HumanUserCount is the number of NON-service-account users found on this
	// box — the single-operator guard's input (recallHumanUsers's count).
	HumanUserCount int
	State          State
	EmbeddingModel string
	EmbeddingDim   int
	// StartedAt is the RFC3339 stamp for LastIndexStartedAt, caller-supplied so
	// this function performs no clock I/O.
	StartedAt string
}

// IndexRunPlan is the decided pre-mutation sequence.
type IndexRunPlan struct {
	// Refused is non-empty ("shared_recall" or "embedding_skew") when the run
	// must stop before any mutation. RefusalMessage is the ready-to-print
	// remediation line (a trailing newline included, matching Fprintf's callers).
	Refused        string
	RefusalMessage string
	// ResetKnowledge is true when --rebuild is set AND the state already names a
	// KnowledgeID to reset (the id-preserving reset stays a caller-driven HTTP
	// call; this only decides WHETHER it must happen).
	ResetKnowledge bool
	// PreparedState is State with the rebuild/stamp decisions already applied:
	// Chats cleared on rebuild, EmbeddingModel/Dim + LastIndexStartedAt stamped,
	// LastIndexCompletedAt cleared, and Chats guaranteed non-nil. KnowledgeID and
	// KnowledgeName still need setting by the caller once EnsureKnowledge
	// returns — that identity is the one thing this function cannot decide
	// without an I/O result.
	PreparedState State
}

// PrepareIndexRun runs the single-operator shared-recall guard FIRST (a refusal
// here must be side-effect-free — Phase-23 review — so it must precede ANY
// state/KB mutation), then the embedding-skew guard (bypassed by --rebuild,
// which re-indexes cleanly and records the new identity), and only then decides
// the rebuild/stamp shape of the state about to be persisted.
func PrepareIndexRun(in IndexRunInput) IndexRunPlan {
	if in.HumanUserCount > 1 && !in.SharedRecallAck {
		return IndexRunPlan{
			Refused:        "shared_recall",
			RefusalMessage: fmt.Sprintf("recall index: REFUSING — found %d human users on this box, but recall pools ALL users' chats into one shared collection visible (with citations) to every user of the served model. This is a cross-user disclosure. If this is a single-operator box (or you accept the shared exposure), re-run with --i-understand-shared-recall; otherwise wait for per-user-scoped recall.\n", in.HumanUserCount),
		}
	}
	if !in.Rebuild && EmbeddingSkew(in.State, in.EmbeddingModel, in.EmbeddingDim) == SkewMismatch {
		return IndexRunPlan{
			Refused: "embedding_skew",
			RefusalMessage: fmt.Sprintf("recall index: REFUSING — the index was built with %s (dim %d) but config now says %s (dim %d); indexing into a mismatched-dimension collection corrupts retrieval. Re-run with --rebuild to re-index cleanly, or revert the config.\n",
				in.State.EmbeddingModel, in.State.EmbeddingDim, in.EmbeddingModel, in.EmbeddingDim),
		}
	}

	state := in.State
	resetKnowledge := false
	if in.Rebuild {
		resetKnowledge = state.KnowledgeID != ""
		state.Chats = nil
	}
	state.EmbeddingModel = in.EmbeddingModel
	state.EmbeddingDim = in.EmbeddingDim
	state.LastIndexStartedAt = in.StartedAt
	state.LastIndexCompletedAt = ""
	if state.Chats == nil {
		state.Chats = map[string]ChatState{}
	}
	return IndexRunPlan{ResetKnowledge: resetKnowledge, PreparedState: state}
}
