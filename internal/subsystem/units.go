package subsystem

import (
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// units.go holds the two remaining per-subsystem properties the update path
// used to hardcode beside its flow: which Quadlet units and services move with
// a subsystem, and how long its update transaction gets.
//
// They live HERE for the reason the package doc gives for the gates and
// StateVolume: each is a property of the subsystem, and a hardcoded map in the
// update path would go stale the moment a sixth subsystem arrives — silently,
// with no compile error to catch it.
//
// The unit names are re-declared rather than read from internal/orchestrate
// because orchestrate imports THIS package — the dependency only runs one way
// (the stateVolumes precedent). They are bound to the rendered reality by a
// cross-package drift test in internal/orchestrate, so the declaration cannot
// quietly disagree with what is rendered.

// unit is one Quadlet .container unit a subsystem renders. on is the gate an
// OPTIONAL unit inside the subsystem answers to; nil means the unit renders
// whenever the subsystem does.
//
// The reranker (ADR-0028) is the first optional unit: it is memory's, runs
// memory's pin and is proved with memory, but a memory-on host renders it only
// with `reranker = true`; the extractor (ADR-0033) is the second, on its own
// `extractor = true`. Two consumers once assumed memory's list was static and
// each grew its own on-disk check; the gate lives here instead, so stop, start,
// capture and restart all read one answer.
type unit struct {
	name string
	on   func(config.VillaConfig) bool
}

// unitTable declares every unit each subsystem can render, in start order.
//
// The grouping is the PROOF UNIT, not a convenience: one `verify memory` proves
// Qdrant, the embedder, the reranker and the extractor together, so they
// capture, mutate and roll back together. Splitting them would produce a pairing with no proof and
// no meaning.
//
// Agent is absent: the Crush binary is a file, not a unit — nothing to render
// and nothing to restart. It is still a subsystem because `verify agent`
// proves it.
var unitTable = map[Kind][]unit{
	Inference: {{name: "villa-llama.container"}},
	Chat:      {{name: "villa-openwebui.container"}},
	Memory: {
		{name: "villa-qdrant.container"},
		{name: "villa-embed.container"},
		{name: "villa-rerank.container", on: RerankOn},
		{name: "villa-extract.container", on: ExtractOn},
	},
	WebSearch: {
		{name: "villa-searxng.container"},
		{name: "villa-websafe.container"},
	},
	// Both voice units render whenever the subsystem does: one gate, one proof
	// (ADR-0030), so neither carries an `on` of its own.
	Voice: {
		{name: "villa-stt.container"},
		{name: "villa-tts.container"},
	},
}

// serviceOf is the Quadlet mapping: villa-x.container → villa-x.service.
func serviceOf(unitName string) string {
	return strings.TrimSuffix(unitName, ".container") + ".service"
}

// Units reports the Quadlet units and systemd services that move with this
// subsystem on a host running cfg: every unit the subsystem always renders, plus
// each optional unit whose gate cfg answers on. Nil slices mean the subsystem has
// no units (Agent — a file, not a unit).
//
// This is what a caller that stops, starts, captures or restarts "the
// subsystem's services" reads. A static list would name a unit the host never
// rendered, and `systemctl` on a unit systemd has never seen fails.
func (k Kind) Units(cfg config.VillaConfig) (units []string, services []string) {
	for _, u := range unitTable[k] {
		if u.on != nil && !u.on(cfg) {
			continue
		}
		units = append(units, u.name)
		services = append(services, serviceOf(u.name))
	}
	return units, services
}

// EveryUnit reports every unit and service this subsystem CAN render, gates
// aside: the declaration, for a caller that names services rather than acting
// on a host (install's service names, the render drift test).
func (k Kind) EveryUnit() (units []string, services []string) {
	for _, u := range unitTable[k] {
		units = append(units, u.name)
		services = append(services, serviceOf(u.name))
	}
	return units, services
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
	case Inference, Image:
		// Image gets inference's budget: an eager load of about 9 GB plus one real
		// generation, run before and after the mutation.
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
