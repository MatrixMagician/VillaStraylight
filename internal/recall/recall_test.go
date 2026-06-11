// Plan diff tests guard the D-05 algebra (new = L∖S, changed = live updated_at >
// state owui_updated_at, deleted = S∖L), purity (inputs never mutated), and the
// deterministic sorted output order that keeps run output and tests stable.
package recall

import (
	"reflect"
	"testing"
)

// TestPlan is the table-driven proof of the D-05 diff algebra: a live chat absent
// from state is an Add; present-in-both with a NEWER live updated_at is an Update;
// equal/older updated_at is untouched; a state chat absent from live is a Delete
// (by chat id — the cmd tier resolves FileID from state).
func TestPlan(t *testing.T) {
	cases := []struct {
		name  string
		live  []ChatRef
		state State
		want  PlanResult
	}{
		{
			name:  "empty live and empty state plan nothing",
			live:  nil,
			state: State{},
			want:  PlanResult{},
		},
		{
			name:  "chat in live but not state is an Add (new = L∖S)",
			live:  []ChatRef{{ID: "c1", UserID: "u1", UpdatedAt: 100}},
			state: State{},
			want:  PlanResult{Adds: []ChatRef{{ID: "c1", UserID: "u1", UpdatedAt: 100}}},
		},
		{
			name: "newer live updated_at is an Update (changed)",
			live: []ChatRef{{ID: "c1", UserID: "u1", UpdatedAt: 200}},
			state: State{Chats: map[string]ChatState{
				"c1": {UserID: "u1", OWUIUpdatedAt: 100, FileID: "f1"},
			}},
			want: PlanResult{Updates: []ChatRef{{ID: "c1", UserID: "u1", UpdatedAt: 200}}},
		},
		{
			name: "equal updated_at is untouched",
			live: []ChatRef{{ID: "c1", UserID: "u1", UpdatedAt: 100}},
			state: State{Chats: map[string]ChatState{
				"c1": {UserID: "u1", OWUIUpdatedAt: 100, FileID: "f1"},
			}},
			want: PlanResult{},
		},
		{
			name: "older live updated_at is untouched (never a spurious re-index)",
			live: []ChatRef{{ID: "c1", UserID: "u1", UpdatedAt: 50}},
			state: State{Chats: map[string]ChatState{
				"c1": {UserID: "u1", OWUIUpdatedAt: 100, FileID: "f1"},
			}},
			want: PlanResult{},
		},
		{
			name: "chat in state but not live is a Delete (deleted = S∖L)",
			live: nil,
			state: State{Chats: map[string]ChatState{
				"c1": {UserID: "u1", OWUIUpdatedAt: 100, FileID: "f1"},
			}},
			want: PlanResult{Deletes: []string{"c1"}},
		},
		{
			name: "empty state with live chats plans everything as Adds",
			live: []ChatRef{
				{ID: "c2", UserID: "u1", UpdatedAt: 2},
				{ID: "c1", UserID: "u1", UpdatedAt: 1},
			},
			state: State{},
			want: PlanResult{Adds: []ChatRef{
				{ID: "c1", UserID: "u1", UpdatedAt: 1},
				{ID: "c2", UserID: "u1", UpdatedAt: 2},
			}},
		},
		{
			name: "mixed add/update/delete/unchanged in one plan",
			live: []ChatRef{
				{ID: "a", UserID: "u1", UpdatedAt: 10},  // new
				{ID: "b", UserID: "u1", UpdatedAt: 300}, // changed (300 > 200)
				{ID: "c", UserID: "u1", UpdatedAt: 100}, // unchanged (== 100)
			},
			state: State{Chats: map[string]ChatState{
				"b": {UserID: "u1", OWUIUpdatedAt: 200, FileID: "fb"},
				"c": {UserID: "u1", OWUIUpdatedAt: 100, FileID: "fc"},
				"d": {UserID: "u1", OWUIUpdatedAt: 50, FileID: "fd"}, // deleted
			}},
			want: PlanResult{
				Adds:    []ChatRef{{ID: "a", UserID: "u1", UpdatedAt: 10}},
				Updates: []ChatRef{{ID: "b", UserID: "u1", UpdatedAt: 300}},
				Deletes: []string{"d"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Plan(tc.live, tc.state)
			if !planResultEqual(got, tc.want) {
				t.Errorf("Plan() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestPlanDeterministicOrder proves Plan sorts every output slice by chat id
// regardless of the input order, so repeated runs and tests are byte-stable.
func TestPlanDeterministicOrder(t *testing.T) {
	live := []ChatRef{
		{ID: "z", UpdatedAt: 1},
		{ID: "a", UpdatedAt: 1},
		{ID: "m", UpdatedAt: 1},
	}
	state := State{Chats: map[string]ChatState{
		"q": {OWUIUpdatedAt: 1},
		"b": {OWUIUpdatedAt: 1},
	}}
	got := Plan(live, state)
	wantAdds := []string{"a", "m", "z"}
	for i, ref := range got.Adds {
		if ref.ID != wantAdds[i] {
			t.Fatalf("Adds order = %+v, want ids %v", got.Adds, wantAdds)
		}
	}
	if !reflect.DeepEqual(got.Deletes, []string{"b", "q"}) {
		t.Errorf("Deletes = %v, want sorted [b q]", got.Deletes)
	}
}

// TestPlanDoesNotMutateInputs proves Plan is pure: the injected live slice and
// the state (including its Chats map) are bit-identical after the call (the
// usage.Fold copy-not-mutate discipline).
func TestPlanDoesNotMutateInputs(t *testing.T) {
	live := []ChatRef{
		{ID: "c2", UserID: "u1", UpdatedAt: 500},
		{ID: "c1", UserID: "u1", UpdatedAt: 100},
	}
	state := State{
		KnowledgeID: "kb1",
		Chats: map[string]ChatState{
			"c1": {UserID: "u1", OWUIUpdatedAt: 50, FileID: "f1"},
			"c9": {UserID: "u1", OWUIUpdatedAt: 9, FileID: "f9"},
		},
	}
	liveCopy := append([]ChatRef(nil), live...)
	stateCopy := State{KnowledgeID: state.KnowledgeID, Chats: map[string]ChatState{}}
	for k, v := range state.Chats {
		stateCopy.Chats[k] = v
	}

	_ = Plan(live, state)

	if !reflect.DeepEqual(live, liveCopy) {
		t.Errorf("Plan mutated the live slice: %+v, want %+v", live, liveCopy)
	}
	if !reflect.DeepEqual(state, stateCopy) {
		t.Errorf("Plan mutated the state: %+v, want %+v", state, stateCopy)
	}
}

// planResultEqual compares PlanResults treating nil and empty slices as equal
// (the algebra cares about set contents, not nil-ness).
func planResultEqual(a, b PlanResult) bool {
	refsEq := func(x, y []ChatRef) bool {
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	strsEq := func(x, y []string) bool {
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	return refsEq(a.Adds, b.Adds) && refsEq(a.Updates, b.Updates) && strsEq(a.Deletes, b.Deletes)
}
