// recall.go is the pure plan/diff half of the recall core (D-05, RECALL-03): it
// computes what the indexer must add, update, and delete by diffing an INJECTED
// live chat list against the persisted State — the D-05 algebra: new = L∖S,
// changed = live updated_at > state owui_updated_at, deleted = S∖L. An update is
// the clean-replace primitive of D-04 (remove the old transcript file, upload the
// re-rendered one) — Plan only DECIDES; the cmd tier (Plan 02) performs the REST
// choreography and resolves FileIDs from state for the deletes.
//
// PURE: no I/O, no os/exec, no clock — copy-not-mutate (usage.Fold discipline),
// deterministic sorted output so run output and tests are byte-stable (D-08).
package recall

import "sort"

// ChatRef is one live chat as the Open WebUI list API reports it: the chat id,
// the owning user id, and updated_at in epoch SECONDS (the list endpoint's unit).
// It carries NO title and NO content — the diff needs identity and recency only.
type ChatRef struct {
	ID        string
	UserID    string
	UpdatedAt int64
}

// PlanResult is the typed diff verdict: chats to index for the first time (Adds),
// chats whose live updated_at outran the indexed snapshot (Updates — clean-replace
// per D-04), and chat ids present in state but gone from the live list (Deletes —
// the cmd tier resolves each id's FileID from state to remove it from the KB).
type PlanResult struct {
	Adds    []ChatRef
	Updates []ChatRef
	Deletes []string
}

// Plan computes the D-05 diff of the injected live chat list against the
// persisted state: a live chat absent from state.Chats is an Add; present in both
// with live.UpdatedAt strictly greater than the recorded OWUIUpdatedAt is an
// Update; a state chat absent from live is a Delete. Equal or older updated_at
// leaves the chat untouched. Plan is PURE — it never mutates live or state — and
// every output slice is sorted by chat id for deterministic run output.
func Plan(live []ChatRef, state State) PlanResult {
	var out PlanResult

	liveIDs := make(map[string]bool, len(live))
	for _, ref := range live {
		liveIDs[ref.ID] = true
		prior, indexed := state.Chats[ref.ID]
		switch {
		case !indexed:
			out.Adds = append(out.Adds, ref) // new = L∖S
		case ref.UpdatedAt > prior.OWUIUpdatedAt:
			out.Updates = append(out.Updates, ref) // changed
		}
	}
	for id := range state.Chats {
		if !liveIDs[id] {
			out.Deletes = append(out.Deletes, id) // deleted = S∖L
		}
	}

	sort.Slice(out.Adds, func(i, j int) bool { return out.Adds[i].ID < out.Adds[j].ID })
	sort.Slice(out.Updates, func(i, j int) bool { return out.Updates[i].ID < out.Updates[j].ID })
	sort.Strings(out.Deletes)
	return out
}
