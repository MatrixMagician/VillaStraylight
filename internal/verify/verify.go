// Package verify is the core of the verify family: memory, web search and the
// coding agent.
//
// The family was roughly 1,800 lines in the command tier with no core at all. The
// same five-part shape appeared three times — a pure evaluator, a live driver, a
// dependency struct, a live constructor, and the cobra plumbing — once per verb, and
// the only core-side module was a small store for the last recorded outcome.
//
// This package owns the shape, so it exists once and the three evaluators become
// testable together.
//
// # The honesty posture is the point of the family
//
// A verify verb answers "is this actually working", and the whole value of that
// answer is that it cannot be fabricated. Two rules carry that, and they are
// properties of this core rather than of any one verb:
//
// A subsystem that is OFF is not a failure. There is nothing to verify, so the verb
// says so and exits clean. Reporting a failure would train the operator to ignore
// the verb; reporting a pass would claim something was proven that never ran.
//
// A proof that could not be CONDUCTED is not a pass and not a failure. It is a third
// outcome. Collapsing it into pass is the false-green the family exists to prevent,
// and collapsing it into failure manufactures a defect from a measurement that never
// happened. The bounded-outbound proof depends on this: an egress block that cannot
// be shown to be effective is rejected rather than reported as holding.
//
// PURE: no I/O, no os/exec. Every effect is the caller's.
package verify

// Status is the outcome of one verification.
type Status int

const (
	// Pass means the proof ran and the property holds.
	Pass Status = iota
	// Fail means the proof ran and the property does NOT hold. A confident negative.
	Fail
	// Reject means the proof could not be CONDUCTED, so nothing was proven either
	// way. It is distinct from both Pass and Fail on purpose: a proof that did not
	// run is not evidence.
	Reject
	// Skip means the subsystem is not enabled, so there is nothing to verify.
	Skip
)

// String renders the status for messages.
func (s Status) String() string {
	switch s {
	case Pass:
		return "pass"
	case Fail:
		return "fail"
	case Reject:
		return "reject"
	case Skip:
		return "skip"
	}
	return "unknown"
}

// Proof is one verification's outcome and its human explanation.
type Proof struct {
	Status Status
	// Detail explains the outcome. On a non-pass it carries the remediation, which is
	// what makes a refusal actionable rather than merely negative.
	Detail string
}

// Subject is what is being verified: its name, how to tell whether it is enabled,
// and what to say when it is not.
type Subject struct {
	// Name is the verb's subject, e.g. "memory", used in every message.
	Name string
	// Enabled reports whether this subsystem is on. When it is off there is nothing
	// to verify.
	Enabled func() bool
	// DisabledMessage tells the operator how to enable the subsystem. It is printed
	// on the skip path, so a clean exit still explains why nothing ran.
	DisabledMessage string
	// Prove runs the verification. It is only called when Enabled reports true.
	Prove func() Proof
	// FailLabel and RejectLabel name what could not be shown, e.g. "runtime
	// zero-outbound RAG proof". Each verb proves a different property, so the
	// operator-facing wording is the verb's, not this core's — the core owns WHICH
	// outcomes exist and how they map, not what to call them.
	FailLabel   string
	RejectLabel string
}

// Outcome is what the caller renders and maps to an exit code.
type Outcome struct {
	// Status is the resolved status, including Skip.
	Status Status
	// Message is the line to print.
	Message string
	// ToStderr reports whether the message belongs on stderr. A non-pass goes to
	// stderr so a caller piping stdout does not silently lose a refusal.
	ToStderr bool
}

// Run performs one verification: gate, drive, and resolve the message.
//
// It prints nothing and exits nothing. The caller renders the message and maps the
// status to an exit code, which is what keeps this testable without a command.
func Run(s Subject) Outcome {
	if s.Enabled != nil && !s.Enabled() {
		return Outcome{
			Status:  Skip,
			Message: s.Name + ": " + s.DisabledMessage,
		}
	}

	proof := s.Prove()
	switch proof.Status {
	case Fail:
		return Outcome{
			Status:   Fail,
			Message:  s.Name + ": " + s.FailLabel + " FAILED: " + proof.Detail,
			ToStderr: true,
		}
	case Reject:
		// A proof that could not be conducted is reported as exactly that. Saying it
		// passed would be the false-green; saying it failed would invent a defect
		// from a measurement that never happened.
		return Outcome{
			Status:   Reject,
			Message:  s.Name + ": " + s.RejectLabel + " could not be conducted (REJECT): " + proof.Detail,
			ToStderr: true,
		}
	default:
		return Outcome{Status: Pass, Message: s.Name + ": " + proof.Detail}
	}
}

// ExitCode maps a status onto the family's exit contract.
//
// Skip and Pass both exit clean: an off subsystem is not a failure. Fail blocks. A
// Reject warns rather than blocking, because nothing was disproven — the operator is
// told the proof could not run, not that the property is broken.
//
// The mapping lives here so the three verbs cannot drift onto different codes.
func ExitCode(status Status, pass, blocked, warn int) int {
	switch status {
	case Fail:
		return blocked
	case Reject:
		return warn
	default:
		return pass
	}
}
