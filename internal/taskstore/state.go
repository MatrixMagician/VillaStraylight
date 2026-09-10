// Package taskstore is the workspace agent's task record: one jsonstore-style
// document per task under the villa data root, a sibling append-only JSON-lines
// log, the state machine spec §3.2 fixes, and directory-wide List/Recover.
//
// Records are never deleted. A crash-recovered non-terminal record becomes
// Interrupted, once, idempotently — Recover never re-queues, because re-running
// a task is the operator's command, not villa's guess (spec §3.2).
package taskstore

// State is one point in the task lifecycle (spec §3.2):
//
//	queued → running → awaiting_approval → running →
//	    done | flagged | failed | refused | cancelled | interrupted
//
// refused is also a legal INITIAL state (Create), never reached via Transition.
type State string

// The task lifecycle's states.
const (
	Queued           State = "queued"
	Running          State = "running"
	AwaitingApproval State = "awaiting_approval"
	Done             State = "done"
	Flagged          State = "flagged"
	Failed           State = "failed"
	Refused          State = "refused"
	Cancelled        State = "cancelled"
	Interrupted      State = "interrupted"
)

// transitions is the legal-edge table. A state absent from this map (every
// terminal state) has no legal outgoing edge — CanTransition checks Terminal
// first for exactly that reason, so the table itself never needs a
// terminal→anything row.
var transitions = map[State][]State{
	Queued:           {Running, Refused, Cancelled, Interrupted},
	Running:          {AwaitingApproval, Done, Flagged, Failed, Cancelled, Interrupted},
	AwaitingApproval: {Running, Cancelled, Interrupted},
}

// Terminal reports whether a state is an end state: no legal outgoing edge,
// and the one state whose Exit code is ever actually read.
func Terminal(s State) bool {
	switch s {
	case Done, Flagged, Failed, Refused, Cancelled, Interrupted:
		return true
	default:
		return false
	}
}

// CanTransition reports whether the transition table permits from → to. A
// terminal from state is refused unconditionally, interrupted included.
func CanTransition(from, to State) bool {
	if Terminal(from) {
		return false
	}
	for _, candidate := range transitions[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

// Exit is state's pure exit-code mapping (spec §3.2): done → 0, flagged → 2,
// every other TERMINAL state → 1. The result is meaningless for a non-terminal
// state (callers only consult it once Terminal(state) is true — Task.Exit
// itself is nil until then, see taskstore.go).
func Exit(s State) int {
	switch s {
	case Done:
		return 0
	case Flagged:
		return 2
	default:
		return 1
	}
}
