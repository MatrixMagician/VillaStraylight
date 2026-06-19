package websafe

// verdict.go defines the GUARD-04 Verdict value type: the flag-not-block outcome of
// the heuristic injection classifier. It is an exported sibling value type (mirroring
// the Page struct) so the seam rewire (Plan 03) can thread it onto Page.Verdict and
// into the /load response metadata.guard contract that Phase 34 surfaces as counters.
//
// HONESTY POSTURE: a Verdict is a FLAG, never a block. Detected=true does NOT mean the
// content was dropped or made safe — the sanitized+fenced content is returned regardless.
// The guard layer reduces and flags; it does not eliminate prompt injection.

// Verdict is the heuristic classifier's outcome over normalized text. Detected is true
// when any rule family matched; Rules carries the matched family names (omitted from
// JSON when empty so a clean page marshals to {"detected":false}).
type Verdict struct {
	Detected bool     `json:"detected"`
	Rules    []string `json:"rules,omitempty"`
}
