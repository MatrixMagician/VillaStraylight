// Package residentset is the admission control for holding several inference
// models resident at once instead of restarting the inference unit to swap
// one for another. Admit decides what a proposed resident set MAY hold; it
// never performs the swap itself — mirroring the Render/Reconcile split in
// internal/orchestrate, this package is the pure planning half only, with no
// impure "carry out the Plan" counterpart living here.
package residentset

// Slot is one model held resident: a running llama-server instance plus the
// identity/footprint fields Admit reasons about. Order is a plain,
// caller-supplied recency value (lower = less recently used) — this package
// never reads a clock, so whoever observes actual use owns recency.
type Slot struct {
	Model   string
	Quant   string
	Ctx     int
	Port    int
	Unit    string
	Primary bool
	Bytes   uint64
	Order   uint64
}

// sameWorkload reports whether two slots represent the same model/quant/ctx
// combination — the identity Admit uses to recognize "already resident"
// (NoOp) versus "a new slot" (Add). Port/Unit are connection assignments,
// not workload identity, so they are deliberately excluded: a caller may
// re-check an already-resident workload before it has settled on new
// connection info for it.
func sameWorkload(a, b Slot) bool {
	return a.Model == b.Model && a.Quant == b.Quant && a.Ctx == b.Ctx
}

// Set is the currently-resident model instances plus the usable memory
// envelope they share.
type Set struct {
	Envelope uint64
	Slots    []Slot
}

// Plan is Admit's verdict on success. Add and Evict are always a single
// candidate/eviction batch (never partial across a refusal — Admit returns
// the zero Plan on any Refusal), and NoOp marks a candidate that is already
// resident (Add and Evict both empty).
type Plan struct {
	Add   []Slot
	Evict []Slot
	NoOp  bool
}

// Policy is the caller-supplied eviction rule: whether eviction may be used
// to make room for a candidate, and how much headroom must stay free above
// whatever ends up resident.
type Policy struct {
	AllowEviction bool
	HeadroomBytes uint64
}

// RefusalReason names WHY Admit refused, so a caller can act on the specific
// promise that was upheld rather than a bare "refused" bool.
type RefusalReason string

const (
	// ReasonExceedsEnvelope: the candidate alone is larger than the whole
	// Envelope — no amount of eviction or headroom tuning could ever fit it.
	ReasonExceedsEnvelope RefusalReason = "exceeds_envelope"
	// ReasonEvictionDisallowed: the candidate does not fit what is already
	// resident, and Policy.AllowEviction is false.
	ReasonEvictionDisallowed RefusalReason = "eviction_disallowed"
	// ReasonWouldEvictPrimary: evicting every evictable (non-Primary) slot
	// leaves only the Primary in the way, and the candidate WOULD fit if the
	// Primary went too. Admit never evicts a Primary, so it refuses instead.
	// It is returned ONLY when the Primary is genuinely the blocker: when the
	// candidate would not fit an empty set either, the honest cause is
	// capacity, not the Primary, and ReasonInsufficientCapacity says so.
	ReasonWouldEvictPrimary RefusalReason = "would_evict_primary"
	// ReasonInsufficientCapacity: the candidate fits the raw Envelope but not
	// the Envelope minus the required headroom, so no eviction of any kind
	// could admit it. Distinct from ReasonWouldEvictPrimary so a refusal never
	// blames a Primary slot that is not the blocker (or does not exist).
	ReasonInsufficientCapacity RefusalReason = "insufficient_capacity"
	// ReasonPortUnitCollision: two slots in play (existing or the
	// candidate against an existing slot) share a Port or a Unit.
	ReasonPortUnitCollision RefusalReason = "port_unit_collision"
)

// Refusal is a typed rejection with a Remediation hint, matching
// internal/preflight's refuse-with-remediation convention: a non-admission
// always says what to do next, never a bare error string. The zero value
// (empty Reason) means "not refused", checked the same way an error is
// checked against nil.
type Refusal struct {
	Reason      RefusalReason
	Remediation string
}
