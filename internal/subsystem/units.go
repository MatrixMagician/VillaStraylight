package subsystem

import "time"

// units.go holds the two remaining per-subsystem properties the update path
// used to hardcode beside its flow: which Quadlet units and services move with
// a subsystem, and how long its update transaction gets.
//
// They live HERE for the reason the package doc gives for the gates and
// StateVolume: each is a property of the subsystem, and a hardcoded map in the
// update path would go stale the moment a sixth subsystem arrives — silently,
// with no compile error to catch it.
//
// The unit and service names are re-declared rather than read from
// internal/orchestrate because orchestrate imports THIS package — the
// dependency only runs one way (the stateVolumes precedent). They are bound to
// the rendered reality by a cross-package drift test in internal/orchestrate,
// so the declaration cannot quietly disagree with what is rendered.

// unitNames maps a subsystem to the Quadlet units and systemd services that
// move with it (Quadlet maps villa-x.container → villa-x.service).
//
// The grouping is the PROOF UNIT, not a convenience: one `verify memory` proves
// Qdrant and the embedder together, so they capture, mutate and roll back
// together. Splitting them would produce a pairing with no proof and no meaning.
//
// Agent is absent: the Crush binary is a file, not a unit — nothing to render
// and nothing to restart. It is still a subsystem because `verify agent`
// proves it.
var unitNames = map[Kind]struct {
	units    []string
	services []string
}{
	Inference: {[]string{"villa-llama.container"}, []string{"villa-llama.service"}},
	Chat:      {[]string{"villa-openwebui.container"}, []string{"villa-openwebui.service"}},
	Memory: {[]string{"villa-qdrant.container", "villa-embed.container"},
		[]string{"villa-qdrant.service", "villa-embed.service"}},
	WebSearch: {[]string{"villa-searxng.container", "villa-websafe.container"},
		[]string{"villa-searxng.service", "villa-websafe.service"}},
}

// Units reports the Quadlet units and services that move with this subsystem.
// Nil slices mean the subsystem has no units (Agent — a file, not a unit).
func (k Kind) Units() (units []string, services []string) {
	u := unitNames[k]
	return u.units, u.services
}

// UpdateBudget is how long one subsystem's update transaction gets.
//
// PER SUBSYSTEM, deliberately, with no global cap: a total cap would make
// failures depend on ordering, so the last subsystem gets blamed for time the
// first four spent. The values mirror what each proof already costs —
// inference carries the residency proof, which ADR-0001 calls the expensive
// part, and it runs twice.
func (k Kind) UpdateBudget() time.Duration {
	switch k {
	case Inference:
		return 10 * time.Minute
	case Chat:
		return 3 * time.Minute
	case Memory, WebSearch:
		return 5 * time.Minute
	case Agent:
		return 3 * time.Minute
	}
	return 5 * time.Minute
}
