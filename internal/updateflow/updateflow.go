// Package updateflow is the transactional core of `villa update`: the pure,
// Deps-injected state machine that moves one subsystem's pins and either commits or
// puts everything back.
//
// It clones internal/backendswap's frame — capture strictly before mutating, gate
// the commit on an injected verdict, restore verbatim on any non-pass — and adds
// the two things updating needs that a swap did not.
//
// # Proving the CURRENT state first is not optional
//
// A swap presupposes a running stack, so it mutates something demonstrably working.
// `update` has no such guarantee. Without a pre-check villa could mutate an
// already-broken subsystem, fail, roll back, and report "the update failed" when it
// was broken beforehand — blaming itself for someone else's outage, and rolling
// back TO a state that was already broken.
//
// So a pre-existing failure is a REFUSAL, not an update failure. Those are
// different outcomes with different wording, and the distinction is carried in the
// result rather than left to the caller to infer.
//
// # Reject rolls back, unchanged
//
// residency.Cutover's rule is inherited without softening: only a true pass is a
// pass. A contradicted FAIL and an unevaluable REJECT alike are non-passes that
// roll back, because a transaction cannot commit on evidence it does not have.
//
// The case that makes this bite is specific to updating, and ADR-0001 predicted it:
// "the log format drifts as the upstream image rebuilds on llama.cpp master". An
// image update is exactly when marker drift happens, so a new image may be
// PERFECTLY GOOD YET UNPROVABLE. Villa rolls back and says so — it cannot show the
// image is fine, and will not commit a pin on evidence it does not have.
//
// A TIMEOUT is a Reject, not a Fail. Both roll back, so only the wording differs,
// and it matters: "did not become healthy within 90s" is a different claim from "is
// broken", and only the first was observed.
//
// # One subsystem at a time, halting on the first failure
//
// Not one transaction. Each subsystem is proven and committed before the next
// begins, so a failure reverts only that subsystem. Order: inference → chat →
// memory → search → agent.
//
// Budgets are PER SUBSYSTEM, with no global cap. A total cap would make failures
// depend on ordering, so the last subsystem gets blamed for time the first four
// spent.
//
// The proof unit is the verify verb's scope, so Qdrant and the embedder move
// together under one `verify memory`, as do SearXNG and the web guard. Splitting
// them would produce pairings with no proof and no meaning.
//
// PURE: every host effect is a Deps field. No os/exec, no HTTP, no file I/O, and no
// image literal — so the whole flow, including every failure and rollback path, is
// driven from tests without a live host.
package updateflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// Outcome is what happened to one subsystem.
type Outcome string

const (
	// Committed: proven before, mutated, proven after, pin recorded.
	Committed Outcome = "committed"
	// RefusedUnhealthy: the subsystem was ALREADY not healthy before anything was
	// attempted. Nothing was changed. This is emphatically not an update failure —
	// villa did not cause it and will not claim it did.
	RefusedUnhealthy Outcome = "refused_unhealthy"
	// RolledBackFail: the new state was proven and CONFIDENTLY FAILED. Villa
	// observed something broken.
	RolledBackFail Outcome = "rolled_back_fail"
	// RolledBackReject: the new state could not be proven either way. The image may
	// be perfectly fine; villa cannot show that it is. Distinct from a Fail because
	// only one of them is a claim about the image being broken.
	RolledBackReject Outcome = "rolled_back_reject"
	// NotTried: an earlier subsystem halted the run before this one began. It is
	// reported explicitly so "what state am I in?" has a complete answer.
	NotTried Outcome = "not_tried"
	// NothingToDo: this subsystem is already at the target pins.
	NothingToDo Outcome = "nothing_to_do"
)

// Committed reports whether this outcome left a new pin in place.
func (o Outcome) Committed() bool { return o == Committed }

// Halts reports whether this outcome stops the sequence.
func (o Outcome) Halts() bool {
	switch o {
	case RefusedUnhealthy, RolledBackFail, RolledBackReject:
		return true
	}
	return false
}

// ProofStatus is the outcome of one proof. It is a local type rather than
// prove.Verdict because updating needs a third value that a cutover never did.
type ProofStatus string

const (
	// ProofPass: the property was demonstrated.
	ProofPass ProofStatus = "pass"
	// ProofFail: the property was demonstrated NOT to hold. A confident negative.
	ProofFail ProofStatus = "fail"
	// ProofReject: the property could not be evaluated. NOT a negative — the thing
	// may be perfectly fine and villa cannot tell. A timeout lands here.
	ProofReject ProofStatus = "reject"
)

// Proof is one proof's result.
type Proof struct {
	Status ProofStatus
	// Detail is the human explanation, carried through to the caller so the wording
	// a user reads comes from the thing that actually observed the state.
	Detail string
}

// Pass reports whether this proof demonstrated the property.
func (p Proof) Pass() bool { return p.Status == ProofPass }

// Target is one subsystem's desired end state: the new pin per component.
type Target struct {
	Subsystem subsystem.Kind
	// Pins is the target pin per component id. A subsystem with several components
	// moves them together, because they are proven together.
	Pins map[string]string
}

// Capture is the rollback point taken strictly before any mutation.
type Capture struct {
	// Refs are the pins each component was running.
	Refs map[string]string
	// Units are the verbatim prior unit bytes, keyed by unit filename. Verbatim
	// because a re-render is not a restore: it reproduces today's template against
	// today's config, which is not what was proven.
	Units map[string][]byte
	// Config is the serialised config the pins were proven under.
	Config string
}

// Deps is the injectable seam set. Every field is a host effect.
type Deps struct {
	// ProveCurrent proves a subsystem is healthy BEFORE anything is touched. This
	// is the seam a swap never needed, and the reason a pre-existing failure can be
	// told apart from one this run caused.
	ProveCurrent func(ctx context.Context, k subsystem.Kind) Proof
	// CaptureState reads the rollback tuple. An error here refuses WITHOUT
	// mutating: a subsystem whose prior state cannot be captured must not be
	// touched, because there would be nothing to go back to.
	CaptureState func(k subsystem.Kind) (Capture, error)
	// Pull fetches the new pins. It happens before the mutation and is not itself
	// a mutation of the running stack — a pulled-but-unused image is inert.
	Pull func(ctx context.Context, refs map[string]string) error
	// Mutate writes the new pins into the rendered units and restarts the
	// subsystem's services. ANY error rolls back.
	Mutate func(ctx context.Context, k subsystem.Kind, refs map[string]string) error
	// ProveNew proves the mutated subsystem. Only a true pass commits.
	ProveNew func(ctx context.Context, k subsystem.Kind) Proof
	// Restore puts the captured tuple back, verbatim.
	Restore func(ctx context.Context, k subsystem.Kind, c Capture) error
	// ProveRestored re-proves the restored state, so "rolled back" is a
	// DEMONSTRATED claim rather than an assumption. ADR-0003 requires honesty when
	// a rollback is incomplete; proving it is how that honesty is earned.
	ProveRestored func(ctx context.Context, k subsystem.Kind) Proof
	// Commit records the new effective pins and the retained previous tuple.
	Commit func(k subsystem.Kind, refs map[string]string, previous pinstate.Previous) error
	// Budget is the per-subsystem time budget. PER SUBSYSTEM and not global: a
	// total cap would make failures depend on ordering, so the last subsystem gets
	// blamed for time the first four spent.
	Budget func(k subsystem.Kind) context.Context
	// Now supplies the capture timestamp.
	Now func() string
}

// SubsystemResult is one subsystem's outcome.
type SubsystemResult struct {
	Subsystem subsystem.Kind
	Outcome   Outcome
	// Proof carries the verdict that decided this outcome, so the caller renders
	// wording that came from the thing that observed the state.
	Proof Proof
	// Committed and Previous are the pins now running and the retained rollback
	// target, on a committed subsystem.
	Committed map[string]string
	Previous  map[string]string
	// RollbackIncomplete is true when a rollback step ITSELF failed. It is separate
	// from the outcome because "villa put it back" and "villa tried to put it back
	// and could not" are different things a user must be told apart (ADR-0003).
	RollbackIncomplete bool
	// Err is a non-proof failure (capture, pull, mutate, restore, commit).
	Err error
	// FailedStep names where it failed, so the caller prints a precise message.
	FailedStep string
}

// Result is the whole run.
type Result struct {
	// Subsystems is one entry per subsystem the run considered, in apply order,
	// INCLUDING the ones never tried. "What state am I in?" is the question a user
	// asks immediately after a halt, and it cannot be answered by a list that omits
	// the untried.
	Subsystems []SubsystemResult
	// Halted is true when a failure stopped the sequence.
	Halted bool
}

// CommittedCount is how many subsystems now run new pins.
func (r Result) CommittedCount() int {
	n := 0
	for _, s := range r.Subsystems {
		if s.Outcome.Committed() {
			n++
		}
	}
	return n
}

// Run applies the targets in order, halting on the first failure.
//
// Order is the caller's, and the caller derives it from the subsystem enumeration,
// so `--check` and apply can never disagree about what happens in what sequence.
func Run(ctx context.Context, d Deps, targets []Target) Result {
	var result Result

	for i, t := range targets {
		if result.Halted {
			// Everything after a halt is explicitly NOT TRIED rather than absent.
			// An omitted subsystem reads as "fine", which is the one thing villa
			// cannot say about a subsystem it never looked at.
			result.Subsystems = append(result.Subsystems, SubsystemResult{
				Subsystem: t.Subsystem,
				Outcome:   NotTried,
			})
			continue
		}
		_ = i
		sr := runOne(ctx, d, t)
		result.Subsystems = append(result.Subsystems, sr)
		if sr.Outcome.Halts() {
			result.Halted = true
		}
	}
	return result
}

// runOne is the per-subsystem transaction:
//
//	prove current → capture → pull → mutate → prove new → commit
//	                   │                          │
//	                   └──── rollback ────────────┘
func runOne(ctx context.Context, d Deps, t Target) SubsystemResult {
	sr := SubsystemResult{Subsystem: t.Subsystem}

	if len(t.Pins) == 0 {
		sr.Outcome = NothingToDo
		return sr
	}

	// Per-subsystem budget. Each subsystem gets its own, so a slow one cannot
	// consume the time a later one needs and then have the later one blamed.
	subCtx := ctx
	if d.Budget != nil {
		subCtx = d.Budget(t.Subsystem)
	}

	// (1) PROVE THE CURRENT STATE. Strictly first, and strictly before the capture,
	// so it cannot read as part of the update.
	//
	// A non-pass here is a REFUSAL, not an update failure. Villa did not cause it,
	// nothing has been changed, and rolling back would mean restoring a state that
	// was already broken. Note that a REJECT refuses too: villa cannot demonstrate
	// the subsystem is healthy, and mutating something it cannot vouch for would
	// make any later failure unattributable.
	if p := d.ProveCurrent(subCtx, t.Subsystem); !p.Pass() {
		sr.Outcome = RefusedUnhealthy
		sr.Proof = p
		return sr
	}

	// (2) CAPTURE, strictly before any mutation. An uncapturable prior state
	// refuses WITHOUT mutating: there would be nothing to go back to.
	capture, err := d.CaptureState(t.Subsystem)
	if err != nil {
		sr.Outcome = RefusedUnhealthy
		sr.Err = err
		sr.FailedStep = "capture"
		sr.Proof = Proof{Status: ProofReject, Detail: "the rollback point could not be captured, so nothing was changed"}
		return sr
	}

	// (3) PULL. Not a mutation of the running stack — a pulled-but-unused image is
	// inert — so a pull failure needs no rollback, only a refusal.
	if d.Pull != nil {
		if err := d.Pull(subCtx, t.Pins); err != nil {
			sr.Outcome = RefusedUnhealthy
			sr.Err = err
			sr.FailedStep = "pull"
			sr.Proof = Proof{Status: ProofReject, Detail: "the new pins could not be fetched, so nothing was changed"}
			return sr
		}
	}

	// (4) MUTATE. From here on, any failure rolls back.
	if err := d.Mutate(subCtx, t.Subsystem, t.Pins); err != nil {
		return rollback(subCtx, d, sr, capture,
			Proof{Status: ProofReject, Detail: fmt.Sprintf("the subsystem could not be moved to the new pins: %v", err)},
			RolledBackReject, err, "mutate")
	}

	// (5) PROVE THE NEW STATE. ONLY a true pass commits.
	p := d.ProveNew(subCtx, t.Subsystem)
	if !p.Pass() {
		outcome := RolledBackFail
		if p.Status != ProofFail {
			// A Reject, including a timeout. The image may be perfectly fine and
			// villa cannot show that it is — a different claim from "it is broken",
			// and only one of them was observed.
			outcome = RolledBackReject
		}
		return rollback(subCtx, d, sr, capture, p, outcome, nil, "prove")
	}

	// (6) COMMIT. The pin becomes effective and the captured tuple becomes the
	// retained known-good previous.
	previous := pinstate.Previous{
		Refs:   capture.Refs,
		Units:  capture.Units,
		Config: capture.Config,
	}
	if d.Now != nil {
		previous.CapturedAt = d.Now()
	}
	if err := d.Commit(t.Subsystem, t.Pins, previous); err != nil {
		// A commit failure is genuinely awkward: the stack is running the new pins
		// and proven, but villa could not record it. Rolling back would undo a
		// PROVEN good state over a bookkeeping failure, so it does not — it reports
		// instead, which leaves the host running something good and villa's record
		// behind, rather than something unproven and the record tidy.
		sr.Outcome = Committed
		sr.Proof = p
		sr.Committed = t.Pins
		sr.Previous = capture.Refs
		sr.Err = err
		sr.FailedStep = "commit"
		return sr
	}

	sr.Outcome = Committed
	sr.Proof = p
	sr.Committed = t.Pins
	sr.Previous = capture.Refs
	return sr
}

// rollback restores the captured tuple and re-proves the restored state.
//
// Re-proving is what makes "rolled back" a demonstrated claim rather than an
// assumption. ADR-0003 requires honesty when a rollback is incomplete, and a
// restore that silently did not take is exactly the case that honesty is for.
func rollback(ctx context.Context, d Deps, sr SubsystemResult, capture Capture,
	p Proof, outcome Outcome, cause error, step string) SubsystemResult {
	sr.Outcome = outcome
	sr.Proof = p
	sr.Err = cause
	sr.FailedStep = step

	if err := d.Restore(ctx, sr.Subsystem, capture); err != nil {
		// The restore itself failed. This is the worst state and must never be
		// reported as a clean rollback.
		sr.RollbackIncomplete = true
		sr.Err = errors.Join(cause, fmt.Errorf("rollback failed: %w", err))
		return sr
	}

	// Re-prove. A restored state that cannot be proven is still rollback-incomplete:
	// villa put the bytes back but cannot show the subsystem is working, and
	// claiming otherwise would be asserting exactly the kind of thing this whole
	// package refuses to assert without evidence.
	if d.ProveRestored != nil {
		if rp := d.ProveRestored(ctx, sr.Subsystem); !rp.Pass() {
			sr.RollbackIncomplete = true
			sr.Err = errors.Join(cause, fmt.Errorf("the restored state could not be proven: %s", rp.Detail))
		}
	}
	return sr
}
