// Staleness tests guard the D-06 typed-Unknown discipline (mirroring
// memory.Decide): an unevaluable live list yields StaleKnown=false — structurally
// distinct from "0 stale, current" — while Indexed/LastIndex* still report from
// villa-side state; a partial run (started stamped, completed empty) is
// distinguishable from a clean full pass; and the attachment state folds through
// verbatim (retrieval honesty is part of staleness, Pitfall 2 / T-21-04).
package recall

import (
	"reflect"
	"strings"
	"testing"
)

// TestStaleness is the table-driven proof of Classify's D-06 contract across
// known-live counting, typed-Unknown live, partial vs complete runs, deleted-chat
// counting, attachment folding, and the everything-stale-as-new empty-state case.
func TestStaleness(t *testing.T) {
	indexedState := State{
		LastIndexStartedAt:   "2026-06-10T12:00:00Z",
		LastIndexCompletedAt: "2026-06-10T12:03:21Z",
		Chats: map[string]ChatState{
			"c1": {UserID: "u1", OWUIUpdatedAt: 100, FileID: "f1"},
			"c2": {UserID: "u1", OWUIUpdatedAt: 200, FileID: "f2"},
		},
	}

	cases := []struct {
		name       string
		live       []ChatRef
		liveKnown  bool
		attachment AttachmentState
		state      State
		want       Report
		wantReason string // substring required in Reasons ("" = Reasons must be empty)
	}{
		{
			name: "(a) known live yields Plan-algebra counts and StaleKnown",
			live: []ChatRef{
				{ID: "c1", UpdatedAt: 100}, // unchanged
				{ID: "c2", UpdatedAt: 300}, // changed (300 > 200)
				{ID: "c3", UpdatedAt: 10},  // new
			},
			liveKnown:  true,
			attachment: AttachmentAttached,
			state:      indexedState,
			want: Report{
				Indexed:              2,
				LastIndexStartedAt:   "2026-06-10T12:00:00Z",
				LastIndexCompletedAt: "2026-06-10T12:03:21Z",
				CompleteRun:          true,
				New:                  1,
				Changed:              1,
				Deleted:              0,
				Stale:                2,
				StaleKnown:           true,
				Attachment:           AttachmentAttached,
			},
		},
		{
			name:       "(b) unknown live: StaleKnown=false, villa-side truths still report",
			live:       nil,
			liveKnown:  false,
			attachment: AttachmentUnknown,
			state:      indexedState,
			want: Report{
				Indexed:              2,
				LastIndexStartedAt:   "2026-06-10T12:00:00Z",
				LastIndexCompletedAt: "2026-06-10T12:03:21Z",
				CompleteRun:          true,
				StaleKnown:           false,
				Attachment:           AttachmentUnknown,
			},
			wantReason: "Unknown",
		},
		{
			name:       "(c) partial run: started stamped, completed empty => CompleteRun=false",
			live:       []ChatRef{{ID: "c1", UpdatedAt: 100}, {ID: "c2", UpdatedAt: 200}},
			liveKnown:  true,
			attachment: AttachmentAttached,
			state: State{
				LastIndexStartedAt: "2026-06-10T12:00:00Z",
				Chats: map[string]ChatState{
					"c1": {UserID: "u1", OWUIUpdatedAt: 100, FileID: "f1"},
					"c2": {UserID: "u1", OWUIUpdatedAt: 200, FileID: "f2"},
				},
			},
			want: Report{
				Indexed:            2,
				LastIndexStartedAt: "2026-06-10T12:00:00Z",
				CompleteRun:        false,
				StaleKnown:         true,
				Attachment:         AttachmentAttached,
			},
			wantReason: "complete",
		},
		{
			name:       "(d) deleted chats counted (state chat gone from live)",
			live:       []ChatRef{{ID: "c1", UpdatedAt: 100}},
			liveKnown:  true,
			attachment: AttachmentAttached,
			state:      indexedState,
			want: Report{
				Indexed:              2,
				LastIndexStartedAt:   "2026-06-10T12:00:00Z",
				LastIndexCompletedAt: "2026-06-10T12:03:21Z",
				CompleteRun:          true,
				Deleted:              1,
				Stale:                1,
				StaleKnown:           true,
				Attachment:           AttachmentAttached,
			},
		},
		{
			name:       "(e) attachment=missing folds through verbatim",
			live:       []ChatRef{{ID: "c1", UpdatedAt: 100}, {ID: "c2", UpdatedAt: 200}},
			liveKnown:  true,
			attachment: AttachmentMissing,
			state:      indexedState,
			want: Report{
				Indexed:              2,
				LastIndexStartedAt:   "2026-06-10T12:00:00Z",
				LastIndexCompletedAt: "2026-06-10T12:03:21Z",
				CompleteRun:          true,
				StaleKnown:           true,
				Attachment:           AttachmentMissing,
			},
		},
		{
			name:       "(f) empty state with known live: everything stale-as-new",
			live:       []ChatRef{{ID: "c1", UpdatedAt: 1}, {ID: "c2", UpdatedAt: 2}, {ID: "c3", UpdatedAt: 3}},
			liveKnown:  true,
			attachment: AttachmentUnknown,
			state:      State{},
			want: Report{
				Indexed:     0,
				CompleteRun: false,
				New:         3,
				Stale:       3,
				StaleKnown:  true,
				Attachment:  AttachmentUnknown,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.live, tc.liveKnown, tc.attachment, tc.state)

			gotCmp := got
			gotCmp.Reasons = nil
			if !reflect.DeepEqual(gotCmp, tc.want) {
				t.Errorf("Classify() = %+v, want %+v", gotCmp, tc.want)
			}

			if tc.wantReason == "" {
				if len(got.Reasons) != 0 {
					t.Errorf("Reasons = %v, want none", got.Reasons)
				}
				return
			}
			found := false
			for _, r := range got.Reasons {
				if strings.Contains(r, tc.wantReason) {
					found = true
				}
				if r == "" {
					t.Error("empty reason string — every reason must name the thing AND why it matters")
				}
			}
			if !found {
				t.Errorf("Reasons = %v, want one containing %q", got.Reasons, tc.wantReason)
			}
		})
	}
}

// TestStalenessUnknownIsNotZero pins the structural heart of D-06 (T-21-04): the
// liveKnown=false report differs from a genuinely-current liveKnown=true report
// ONLY via StaleKnown — so any renderer that checks StaleKnown can never print a
// stale-count of 0 for an unevaluable live list, and a renderer that ignores it
// is caught by this test's premise being explicit.
func TestStalenessUnknownIsNotZero(t *testing.T) {
	state := State{
		LastIndexStartedAt:   "2026-06-10T12:00:00Z",
		LastIndexCompletedAt: "2026-06-10T12:03:21Z",
		Chats:                map[string]ChatState{"c1": {UserID: "u1", OWUIUpdatedAt: 100}},
	}

	current := Classify([]ChatRef{{ID: "c1", UpdatedAt: 100}}, true, AttachmentAttached, state)
	unknown := Classify(nil, false, AttachmentAttached, state)

	if !current.StaleKnown || current.Stale != 0 {
		t.Fatalf("current report = %+v, want StaleKnown=true Stale=0", current)
	}
	if unknown.StaleKnown {
		t.Fatal("unknown-live report has StaleKnown=true — Unknown must be structurally distinct from 0")
	}
	if len(unknown.Reasons) == 0 {
		t.Fatal("unknown-live report carries no reason — the WHY must be named (D-06)")
	}
	// Villa-side truths still report even when OWUI is unreachable.
	if unknown.Indexed != 1 || unknown.LastIndexCompletedAt != "2026-06-10T12:03:21Z" {
		t.Errorf("unknown-live report dropped villa-side truths: %+v", unknown)
	}
}
