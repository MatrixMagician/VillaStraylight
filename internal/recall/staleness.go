// staleness.go is the typed-Unknown staleness classification of the recall core
// (D-06, RECALL-03), mirroring memory.Decide's typed-verdict + accumulated-reasons
// shape: Unknown is a DISTINCT state, never a zero. An unevaluable live chat list
// (OWUI unreachable, listing failed) yields StaleKnown=false with a reason naming
// WHY — the rendering layer must print "Unknown — could not evaluate", never
// "0 stale, current" (T-21-04) — while Indexed and the LastIndex* stamps still
// report from the persisted State (they are villa-side truths that hold even when
// OWUI is down). A partial index run (started stamped, completed empty) is
// structurally distinguishable from a clean full pass via CompleteRun (Pitfall 8).
//
// PURE: no I/O, no clock — the live list, its evaluability, the attachment state,
// and the persisted State are all injected (D-08).
package recall

// AttachmentState is the typed verdict of "is the recall knowledge base attached
// to the served model's meta.knowledge?" — retrieval honesty is part of staleness
// (Pitfall 2: a model swap silently detaches recall while the index looks green).
type AttachmentState string

// The three attachment verdicts: attached (KB present in the served model's
// meta.knowledge), missing (confidently absent — retrieval is OFF), and unknown
// (OWUI/model discovery unevaluable — distinct from missing, D-06).
const (
	AttachmentAttached AttachmentState = "attached"
	AttachmentMissing  AttachmentState = "missing"
	AttachmentUnknown  AttachmentState = "unknown"
)

// Report is the typed staleness verdict. The diff counts (New/Changed/Deleted/
// Stale) are ONLY meaningful when StaleKnown is true — a renderer must check
// StaleKnown before printing them, because a false carries counts of 0 that mean
// "could not evaluate", never "current" (D-06). Indexed, the LastIndex* stamps,
// and CompleteRun are villa-side truths populated from State unconditionally.
type Report struct {
	Indexed              int
	LastIndexStartedAt   string
	LastIndexCompletedAt string
	CompleteRun          bool
	New                  int
	Changed              int
	Deleted              int
	Stale                int
	StaleKnown           bool
	Attachment           AttachmentState
	Reasons              []string
}

// Classify computes the honest staleness report for `recall status` (D-06):
// villa-side truths (Indexed, LastIndex*, CompleteRun) come from state
// unconditionally; the diff counts reuse Plan (never a second copy of the D-05
// algebra) when liveKnown, and degrade to StaleKnown=false with an explanatory
// reason when the live list could not be evaluated. The attachment verdict folds
// through verbatim. PURE — no I/O, never panics.
func Classify(live []ChatRef, liveKnown bool, attachment AttachmentState, state State) Report {
	r := Report{
		Indexed:              len(state.Chats),
		LastIndexStartedAt:   state.LastIndexStartedAt,
		LastIndexCompletedAt: state.LastIndexCompletedAt,
		Attachment:           attachment,
	}

	// A clean full pass stamps last_index_completed_at; a run that started but
	// never completed leaves it empty (or older than a newer start — RFC3339 UTC
	// strings compare correctly lexicographically). Pitfall 8.
	r.CompleteRun = state.LastIndexCompletedAt != "" && state.LastIndexCompletedAt >= state.LastIndexStartedAt
	if state.LastIndexStartedAt != "" && state.LastIndexCompletedAt == "" {
		r.Reasons = append(r.Reasons,
			"the last index run started but never completed — its remainder must be treated as stale/unknown, never as indexed")
	}

	if !liveKnown {
		// The live list is unevaluable: stale/new/changed/deleted are Unknown —
		// could not evaluate — NOT 0-and-current (D-06, T-21-04).
		r.StaleKnown = false
		r.Reasons = append(r.Reasons,
			"could not list chats from Open WebUI — the stale count is Unknown, not 0 (indexed/last-indexed still report from villa-side state)")
		return r
	}

	p := Plan(live, state) // reuse the D-05 algebra — never duplicate it
	r.New = len(p.Adds)
	r.Changed = len(p.Updates)
	r.Deleted = len(p.Deletes)
	r.Stale = r.New + r.Changed + r.Deleted
	r.StaleKnown = true
	return r
}
