package status

// services.go models the stack's services as a LIST rather than as a widening set
// of parallel fields.
//
// Deps carried seven health probes with the same shape and six service names those
// probes belonged to: thirteen fields for what is one question asked repeatedly, and
// five near-identical branches in the fold. Adding one optional subsystem cost a
// service name, a health probe and a report branch, spread across three files.
//
// A service is now one list entry. Adding a subsystem to status is one more entry.

// ServiceKind classifies what a service contributes to the overall verdict.
//
// The distinction is load-bearing rather than cosmetic. Only the inference service
// runs the model, so only it can prove or disprove GPU offload. Every other service
// carries the N/A offload representation, which is EXCLUDED from the worst-wins
// fold. Folding a managed service's absent offload as a failure would report a
// broken stack whenever the chat UI was merely up; folding it as a pass would
// manufacture offload evidence from a service that never touches the GPU.
type ServiceKind int

const (
	// Inference is the service running the model. It is the ONLY kind whose offload
	// verdict participates in the overall fold.
	Inference ServiceKind = iota
	// Managed is any other service villa runs: the chat UI, the vector store, the
	// embedder, the metasearch service, the loader, the dashboard. It has no GPU
	// offload, so its offload is the N/A representation.
	Managed
)

// Service is one row of the status report: which unit, how to probe it, and whether
// its offload counts.
//
// Probe is REQUIRED for a service to report anything but Unknown. That is the shape
// the nil-guards used to hide: a caller could omit a probe and get a row that looked
// answered. Here the absence is visible in the value, and a service with no probe
// reports HealthUnknown rather than a fabricated state.
type Service struct {
	// Unit is the systemd service name, e.g. villa-qdrant.service. The cmd tier
	// derives it from the orchestrate accessors so no unit literal is re-typed here.
	Unit string
	// Kind decides whether this service's offload folds into the overall verdict.
	Kind ServiceKind
	// Probe reports this service's health. Each service MUST use its own probe: a
	// managed service must never borrow the inference endpoint's health probe, which
	// was a real false-green — a healthy chat model made a down vector store look
	// ready.
	//
	// A nil Probe yields HealthUnknown, which is a typed Unknown and never a
	// fabricated verdict.
	Probe func() HealthState
	// Rendered reports whether this service is part of the rendered unit set. A
	// service that is not rendered produces no row at all. The inference and chat
	// rows derive this from the units; the dashboard is a native systemd service
	// rather than a container, so it sets this explicitly.
	Rendered bool
	// AlwaysRow forces a row even when the service is absent from the rendered unit
	// set. The dashboard needs this: it is a managed member of the stack but is not
	// a Quadlet container, so it never appears in the rendered units.
	AlwaysRow bool
}

// health resolves a service's health, treating an absent probe as Unknown.
func (s Service) health() HealthState {
	if s.Probe == nil {
		return HealthUnknown
	}
	return s.Probe()
}

// findService returns the configured Service for a unit name, if any.
func findService(services []Service, unit string) (Service, bool) {
	for _, s := range services {
		if s.Unit == unit {
			return s, true
		}
	}
	return Service{}, false
}
