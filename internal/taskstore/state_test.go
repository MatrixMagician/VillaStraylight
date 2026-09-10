// state_test.go guards the transition table (spec §3.2): every legal edge is
// allowed, every illegal edge is refused, a terminal state has no legal
// outgoing edge, and Exit is the pure done/flagged/other mapping.
package taskstore

import "testing"

func TestTerminal(t *testing.T) {
	terminal := []State{Done, Flagged, Failed, Refused, Cancelled, Interrupted}
	for _, s := range terminal {
		if !Terminal(s) {
			t.Errorf("Terminal(%q) = false, want true", s)
		}
	}
	nonTerminal := []State{Queued, Running, AwaitingApproval}
	for _, s := range nonTerminal {
		if Terminal(s) {
			t.Errorf("Terminal(%q) = true, want false", s)
		}
	}
}

func TestCanTransitionLegal(t *testing.T) {
	legal := [][2]State{
		{Queued, Running},
		{Queued, Refused},
		{Queued, Cancelled},
		{Queued, Interrupted},
		{Running, AwaitingApproval},
		{Running, Done},
		{Running, Flagged},
		{Running, Failed},
		{Running, Cancelled},
		{Running, Interrupted},
		{AwaitingApproval, Running},
		{AwaitingApproval, Cancelled},
		{AwaitingApproval, Interrupted},
	}
	for _, e := range legal {
		if !CanTransition(e[0], e[1]) {
			t.Errorf("CanTransition(%q, %q) = false, want true", e[0], e[1])
		}
	}
}

func TestCanTransitionIllegal(t *testing.T) {
	illegal := [][2]State{
		{Queued, Done},
		{Queued, Flagged},
		{Queued, AwaitingApproval},
		{Running, Queued},
		{Running, Refused},
		{AwaitingApproval, Done},
		{AwaitingApproval, Refused},
		{AwaitingApproval, Flagged},
	}
	for _, e := range illegal {
		if CanTransition(e[0], e[1]) {
			t.Errorf("CanTransition(%q, %q) = true, want false", e[0], e[1])
		}
	}
}

// TestCanTransitionFromTerminalAlwaysRefused proves no terminal state has any
// legal outgoing edge, interrupted included — Transition on a terminal record
// must always be refused.
func TestCanTransitionFromTerminalAlwaysRefused(t *testing.T) {
	terminal := []State{Done, Flagged, Failed, Refused, Cancelled, Interrupted}
	targets := []State{Queued, Running, AwaitingApproval, Done, Flagged, Failed, Refused, Cancelled, Interrupted}
	for _, from := range terminal {
		for _, to := range targets {
			if CanTransition(from, to) {
				t.Errorf("CanTransition(%q, %q) = true, want false (terminal has no outgoing edge)", from, to)
			}
		}
	}
}

func TestExit(t *testing.T) {
	cases := map[State]int{
		Done:      0,
		Flagged:   2,
		Failed:    1,
		Refused:   1,
		Cancelled: 1,
		Interrupted: 1,
	}
	for state, want := range cases {
		if got := Exit(state); got != want {
			t.Errorf("Exit(%q) = %d, want %d", state, got, want)
		}
	}
}
