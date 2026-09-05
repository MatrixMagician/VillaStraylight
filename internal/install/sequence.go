package install

import (
	"slices"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// sequence.go owns install's mutate-and-start ordering.
//
// The ordering rules are load-bearing and used to exist only as comments inside one
// long function, with nothing asserting any of them:
//
//   - secrets are generated and persisted BEFORE the units that reference them are
//     rendered and written, so a unit never names an EnvironmentFile that is absent;
//   - the bearer env file is written BEFORE the chat UI starts, because the chat UI
//     unit references it and systemd refuses to start a unit whose EnvironmentFile
//     target does not exist;
//   - inference starts BEFORE the chat UI, so the UI comes up against a live backend
//     and its model picker is populated on first visit;
//   - each start is gated on its unit being present in the written plan, so install
//     never starts a unit systemd has never seen.
//
// Expressing the sequence as data rather than as control flow is what makes those
// rules assertable. A test reads the planned order and holds it to the constraints,
// instead of inferring them from the order of statements.

// StepKind is what a step does. The kinds are ordered by the phase they belong to,
// so a violation of the phase ordering is visible as a decreasing kind.
type StepKind int

const (
	// StepGenerateSecret generates and persists a secret. It must precede any render
	// or write of a unit that references it.
	StepGenerateSecret StepKind = iota
	// StepPersistConfig writes config.toml. Config is the single source of truth, so
	// it lands before the units that are rendered from it — otherwise a later
	// `villa up` resolves a different config than install wrote units for.
	StepPersistConfig
	// StepWriteSecretFile writes a 0600 secret file. It must precede the start of any
	// service whose unit references it via EnvironmentFile.
	StepWriteSecretFile
	// StepWriteUnits writes the changed unit files and reloads the manager.
	StepWriteUnits
	// StepStart starts one service.
	StepStart
	// StepProve runs a readiness or residency proof against a started service.
	StepProve
)

// String renders the kind for assertion messages.
func (k StepKind) String() string {
	switch k {
	case StepGenerateSecret:
		return "generate-secret"
	case StepPersistConfig:
		return "persist-config"
	case StepWriteSecretFile:
		return "write-secret-file"
	case StepWriteUnits:
		return "write-units"
	case StepStart:
		return "start"
	case StepProve:
		return "prove"
	}
	return "unknown"
}

// Step is one mutation in the install sequence.
type Step struct {
	// Kind is what this step does.
	Kind StepKind
	// Service is the unit this step starts or proves, empty for the others.
	Service string
	// Secret names the secret this step generates or writes, empty for the others.
	Secret string
	// RequiresUnit is the unit that must be present in the written plan for this
	// step to run. A start is gated on it so install never starts a unit systemd has
	// never seen. Empty means ungated.
	RequiresUnit string
}

// Sequence is the ordered mutate-and-start plan.
type Sequence struct {
	Steps []Step
}

// Services returns the services the sequence starts, in order.
func (s Sequence) Services() []string {
	var out []string
	for _, st := range s.Steps {
		if st.Kind == StepStart {
			out = append(out, st.Service)
		}
	}
	return out
}

// IndexOf returns the position of the first step matching kind and service, or -1.
// Service is ignored when empty.
func (s Sequence) IndexOf(kind StepKind, service string) int {
	for i, st := range s.Steps {
		if st.Kind != kind {
			continue
		}
		if service == "" || st.Service == service {
			return i
		}
	}
	return -1
}

// Units names the services the sequence refers to. They are values rather than
// literals in this package so the core never re-types a unit name.
type Units struct {
	Inference string
	ChatUI    string
	Qdrant    string
	Embed     string
	Searxng   string
	Websafe   string
}

// BuildSequence derives the mutate-and-start sequence for a run.
//
// The order encodes the constraints listed at the top of this file. Optional
// subsystems contribute steps only when their gate is on, so a subsystem-off
// install produces the same sequence it always did.
func BuildSequence(gates Gates, u Units, secretNeeded bool) Sequence {
	var steps []Step

	// Config first: the units are rendered from it, so it must be the source of
	// truth before anything derived from it is written.
	steps = append(steps, Step{Kind: StepPersistConfig})

	// The web-search bearer is generated and persisted BEFORE the units that
	// reference it are written, so no unit ever names an absent EnvironmentFile.
	if gates.WebSearch && secretNeeded {
		steps = append(steps, Step{Kind: StepGenerateSecret, Secret: "web-loader-bearer"})
	}

	steps = append(steps, Step{Kind: StepWriteUnits})

	// The bearer file lands before the chat UI starts: the chat UI's unit references
	// it via EnvironmentFile, and systemd refuses to start a unit whose target is
	// missing.
	if gates.WebSearch {
		steps = append(steps, Step{Kind: StepWriteSecretFile, Secret: "web-loader-bearer"})
	}

	// Inference before the chat UI, so the UI comes up against a live backend.
	steps = append(steps,
		Step{Kind: StepStart, Service: u.Inference, RequiresUnit: u.Inference},
		Step{Kind: StepStart, Service: u.ChatUI, RequiresUnit: u.ChatUI},
	)

	// The vector store before the embedder, so the embedder's peer is reachable.
	if gates.Memory {
		steps = append(steps,
			Step{Kind: StepStart, Service: u.Qdrant, RequiresUnit: u.Qdrant},
			Step{Kind: StepStart, Service: u.Embed, RequiresUnit: u.Embed},
			Step{Kind: StepProve, Service: u.Embed},
		)
	}

	if gates.WebSearch {
		steps = append(steps,
			Step{Kind: StepStart, Service: u.Searxng, RequiresUnit: u.Searxng},
			Step{Kind: StepStart, Service: u.Websafe, RequiresUnit: u.Websafe},
			Step{Kind: StepProve, Service: u.Searxng},
		)
	}

	if gates.Agent {
		steps = append(steps, Step{Kind: StepProve, Service: u.Inference})
	}

	return Sequence{Steps: steps}
}

// UnitPresent reports whether a unit is in the written plan, in either Changed or
// Unchanged. A start is gated on this so install never starts a unit systemd has
// never seen — gating on the subsystem flag alone would do exactly that on a host
// where the render produced no such unit.
func UnitPresent(plan orchestrate.Plan, unitName string) bool {
	named := func(u orchestrate.Unit) bool { return u.Name == unitName }
	return slices.ContainsFunc(plan.Changed, named) || slices.ContainsFunc(plan.Unchanged, named)
}

// AssertStartOrder checks that the services actually started match the sequence the
// core planned, in order.
//
// It exists so the plan cannot become decoration. Without it, the ordering tests in
// this package would keep passing while the command tier drifted out from under
// them, which is exactly how the rules ended up as comments nothing enforced.
//
// A service the plan expects but that was skipped because its unit was absent from
// the written plan is NOT a violation: that gate is itself one of the rules. What is
// a violation is a start the plan never expected, or expected starts arriving out of
// order.
func AssertStartOrder(seq Sequence, performed []string) error {
	planned := seq.Services()

	// Every performed start must appear in the plan, and their relative order must
	// match the plan's.
	at := 0
	for _, got := range performed {
		found := -1
		for i := at; i < len(planned); i++ {
			if planned[i] == got {
				found = i
				break
			}
		}
		if found == -1 {
			// Either unplanned, or out of order relative to what came before.
			if slices.Contains(planned[:at], got) {
				return &OrderError{Planned: planned, Performed: performed, Service: got, Reason: "started out of the planned order"}
			}
			return &OrderError{Planned: planned, Performed: performed, Service: got, Reason: "started but never planned"}
		}
		at = found + 1
	}
	return nil
}

// OrderError reports a mismatch between the planned and performed start order.
type OrderError struct {
	Planned   []string
	Performed []string
	Service   string
	Reason    string
}

func (e *OrderError) Error() string {
	return "start sequence violated: " + e.Service + " " + e.Reason +
		"; planned " + join(e.Planned) + ", performed " + join(e.Performed)
}

// join renders a service list for an error message without pulling in strings.
func join(xs []string) string {
	out := "["
	for i, x := range xs {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out + "]"
}

// ServiceFor maps a Quadlet unit file name to the systemd service it generates:
// villa-llama.container is villa-llama.service. Only container units start
// services the flow drives; other unit kinds map to a name nothing looks up.
func ServiceFor(unitName string) string {
	return strings.TrimSuffix(unitName, ".container") + ".service"
}
