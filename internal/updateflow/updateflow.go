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
// # For a stateful subsystem the mutation is a WINDOW, not an instant
//
// The rollback tuple is complete for a component whose image IS the state being
// changed. For chat and memory it is not: their state lives in a volume, and a
// forward data migration makes the retained image an unusable rollback target. So
// those two get a data snapshot, and taking it turns the mutation from a single
// atomic restart into an explicit window:
//
//	stop → snapshot → mutate → start
//
// THE STOP IS LOAD-BEARING, not incidental. A volume exported from under a running
// service is a torn copy — `villa backup` already stops Open WebUI for exactly this
// reason. A snapshot that cannot be restored is worse than no snapshot, because it
// is a safety net that only fails when used. Measured on hardware, exporting the
// 2.3 GB Qdrant volume takes about two seconds against a restart that was happening
// anyway.
//
// A CAPTURE FAILURE IS A REFUSAL. Mutating state villa could not snapshot is
// precisely what produced the incident, so a full disk or an unavailable Podman
// blocks updating a stateful subsystem entirely. That cost is accepted, stated, and
// not to be softened later.
//
// Every path out of the stopped window starts the services again. The stop is
// villa's, so leaving a subsystem down because the snapshot errored would turn a
// refusal into an outage.
//
// A STATELESS SUBSYSTEM KEEPS THE ATOMIC RESTART. Adding a stopped window to
// inference buys nothing and doubles the most expensive restart in the stack.
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
	// Data is the exported data volume, present only for a subsystem that owns
	// persistent state and taken only inside the stopped window. Its zero value is
	// the honest reading for a stateless subsystem, whose image IS the state.
	Data pinstate.DataSnapshot
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
	//
	// For a stateful subsystem it runs INSIDE the stopped window, so the services
	// it would restart are already down and Start brings them back afterwards. The
	// caller's implementation restarts either way, which is idempotent against a
	// stopped unit.
	Mutate func(ctx context.Context, k subsystem.Kind, refs map[string]string) error
	// Stop stops the subsystem's services, opening the window in which a volume can
	// be exported cleanly. It is called ONLY for a subsystem that owns persistent
	// state; a stateless one keeps its single atomic restart.
	Stop func(ctx context.Context, k subsystem.Kind) error
	// SnapshotData exports the subsystem's data volume while it is stopped. An
	// error REFUSES the update with nothing mutated — the same rule the capture
	// already follows, for the same reason: there would be nothing to go back to.
	SnapshotData func(ctx context.Context, k subsystem.Kind) (pinstate.DataSnapshot, error)
	// Start starts the subsystem's services, closing the window. EVERY path out of
	// the stopped window calls it, including the failing ones: villa stopped the
	// services, so leaving them down would turn a refusal into an outage.
	Start func(ctx context.Context, k subsystem.Kind) error
	// ProveNew proves the mutated subsystem. Only a true pass commits.
	ProveNew func(ctx context.Context, k subsystem.Kind) Proof
	// Restore puts the captured tuple back, verbatim.
	Restore func(ctx context.Context, k subsystem.Kind, c Capture) error
	// RestoreData imports the data snapshot back into the subsystem's volume, and
	// is called ONLY while the subsystem is stopped.
	//
	// It REPLACES rather than merges. `podman volume import` merges into existing
	// contents, so importing a pre-migration snapshot over a migrated volume would
	// leave a hybrid of both schemas — worse than either, and undetectable until
	// something reads the wrong half.
	RestoreData func(ctx context.Context, k subsystem.Kind, d pinstate.DataSnapshot) error
	// ProveRestored re-proves the restored state, so "rolled back" is a
	// DEMONSTRATED claim rather than an assumption. ADR-0003 requires honesty when
	// a rollback is incomplete; proving it is how that honesty is earned.
	ProveRestored func(ctx context.Context, k subsystem.Kind) Proof
	// Commit records the new effective pins and the retained previous tuple.
	Commit func(k subsystem.Kind, refs map[string]string, previous pinstate.Previous) error
	// Budget is the per-subsystem time budget. PER SUBSYSTEM and not global: a
	// total cap would make failures depend on ordering, so the last subsystem gets
	// blamed for time the first four spent.
	//
	// It returns a context AND its cancel, so the budget is released as soon as the
	// subsystem finishes rather than lingering until the whole run ends. A signature
	// that returned only the context would leak a timer per subsystem, which go vet
	// catches and which would matter on a run that halts early.
	Budget func(ctx context.Context, k subsystem.Kind) (context.Context, context.CancelFunc)
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
	// Snapshot is the data snapshot taken inside the stopped window, carried out to
	// the caller so the narration can state what it cost. It is the ZERO VALUE for
	// a stateless subsystem, which is the honest "there was no data to take" rather
	// than a missing field.
	Snapshot pinstate.DataSnapshot
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
		var cancel context.CancelFunc
		subCtx, cancel = d.Budget(ctx, t.Subsystem)
		defer cancel()
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
	//
	// For a subsystem that owns persistent state this is a WINDOW rather than an
	// instant: stop → snapshot → mutate → start. The stop is what makes the export
	// a clean copy rather than a torn one, and a snapshot that cannot be restored
	// is worse than no snapshot at all.
	if t.Subsystem.OwnsPersistentState() {
		return runStatefulMutation(subCtx, d, sr, capture, t)
	}

	if err := d.Mutate(subCtx, t.Subsystem, t.Pins); err != nil {
		return rollback(subCtx, d, sr, capture,
			Proof{Status: ProofReject, Detail: fmt.Sprintf("the subsystem could not be moved to the new pins: %v", err)},
			RolledBackReject, err, "mutate")
	}

	return finishAfterMutation(subCtx, d, sr, capture, t)
}

// runStatefulMutation is the stopped window for a subsystem whose data, not whose
// image, is the state being changed.
//
//	stop → snapshot → mutate → start → prove new → commit
//
// A failure BEFORE the mutation refuses with nothing changed and the services
// running again. A failure at or after the mutation rolls back, which is why the
// start happens before the proof rather than as part of it.
func runStatefulMutation(ctx context.Context, d Deps, sr SubsystemResult, capture Capture, t Target) SubsystemResult {
	if d.Stop == nil || d.SnapshotData == nil || d.Start == nil || d.RestoreData == nil {
		// A stateful subsystem with an incomplete window would silently take the
		// stateless path and mutate data it never snapshotted — the exact shape of
		// the incident. All FOUR seams are required together, because a snapshot
		// nothing can restore is not a rollback target.
		sr.Outcome = RefusedUnhealthy
		sr.FailedStep = "snapshot"
		sr.Err = errors.New("no data-snapshot seam is wired for a subsystem that owns persistent state")
		sr.Proof = Proof{Status: ProofReject, Detail: "villa cannot snapshot this subsystem's data, so it will not mutate it"}
		return sr
	}

	if err := d.Stop(ctx, t.Subsystem); err != nil {
		// Nothing has been mutated and the window never opened. Villa still starts
		// the services, because a partial stop may have taken some of them down.
		startErr := d.Start(ctx, t.Subsystem)
		sr.Outcome = RefusedUnhealthy
		sr.FailedStep = "stop"
		sr.Err = joinStartErr(err, startErr)
		sr.RollbackIncomplete = startErr != nil
		sr.Proof = Proof{Status: ProofReject, Detail: "the subsystem could not be stopped, so its data could not be snapshotted and nothing was changed"}
		return sr
	}

	snapshot, err := d.SnapshotData(ctx, t.Subsystem)
	if err != nil {
		// A REFUSAL, not a warning. Mutating state villa could not snapshot is what
		// produced the incident this window exists to prevent. Nothing is mutated,
		// and the services come back up.
		startErr := d.Start(ctx, t.Subsystem)
		sr.Outcome = RefusedUnhealthy
		sr.FailedStep = "snapshot"
		sr.Err = joinStartErr(err, startErr)
		sr.RollbackIncomplete = startErr != nil
		sr.Proof = Proof{Status: ProofReject, Detail: fmt.Sprintf("the current data could not be captured for rollback, so nothing was changed: %v", err)}
		return sr
	}
	capture.Data = snapshot
	sr.Snapshot = snapshot

	if err := d.Mutate(ctx, t.Subsystem, t.Pins); err != nil {
		// Past the point of no return: the mutation may have written pins and
		// re-rendered units before it failed. The services stay DOWN into the
		// rollback, which owns the window from here — it restores the data volume
		// while stopped and starts them once the whole tuple is back. Starting them
		// here only to stop them again would churn the service for no gain.
		return rollback(ctx, d, sr, capture,
			Proof{Status: ProofReject, Detail: fmt.Sprintf("the subsystem could not be moved to the new pins: %v", err)},
			RolledBackReject, err, "mutate")
	}

	if err := d.Start(ctx, t.Subsystem); err != nil {
		// The mutation landed but the subsystem is down, so the post-mutation proof
		// cannot run. That is a Reject and it rolls back, which is the same posture
		// as an unprovable new image.
		return rollback(ctx, d, sr, capture,
			Proof{Status: ProofReject, Detail: fmt.Sprintf("the subsystem could not be started after the update: %v", err)},
			RolledBackReject, err, "start")
	}

	return finishAfterMutation(ctx, d, sr, capture, t)
}

// joinStartErr folds a failed restart of villa's OWN stop into the cause.
//
// It is separate wording because the two failures mean different things: the first
// says why villa refused, the second says the subsystem is down and villa put it
// there. A user needs both, and the second is the more urgent one.
func joinStartErr(cause, startErr error) error {
	if startErr == nil {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("THE SERVICES ARE STILL STOPPED: villa stopped them to take the snapshot and could not start them again: %w", startErr))
}

// finishAfterMutation proves the new state and commits, the half of the transaction
// that is identical whether or not there was a stopped window.
func finishAfterMutation(ctx context.Context, d Deps, sr SubsystemResult, capture Capture, t Target) SubsystemResult {
	// (5) PROVE THE NEW STATE. ONLY a true pass commits.
	p := d.ProveNew(ctx, t.Subsystem)
	if !p.Pass() {
		outcome := RolledBackFail
		if p.Status != ProofFail {
			// A Reject, including a timeout. The image may be perfectly fine and
			// villa cannot show that it is — a different claim from "it is broken",
			// and only one of them was observed.
			outcome = RolledBackReject
		}
		return rollback(ctx, d, sr, capture, p, outcome, nil, "prove")
	}

	// (6) COMMIT. The pin becomes effective and the captured tuple becomes the
	// retained known-good previous.
	previous := pinstate.Previous{
		Refs:   capture.Refs,
		Units:  capture.Units,
		Config: capture.Config,
		Data:   capture.Data,
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

	// The DATA half, for a subsystem that owns persistent state and got as far as
	// having a snapshot taken.
	//
	// It mirrors the capture, for the same reason: stop → restore volume → restore
	// pin and unit → start. Importing into a volume a running service holds open is
	// how you get a half-restored database, which is the state this whole lifecycle
	// exists to avoid. On hardware villa got everything else right and still could
	// not undo the schema migration, because it had never captured the data — this
	// is the step that completes that rollback.
	//
	// The stop is unconditional here rather than conditional on the subsystem still
	// running: `systemctl stop` on a stopped unit is a no-op, and guessing whether
	// the failing path left it up would be a second opinion about state villa
	// already knows how to assert.
	restoredData := false
	if capture.Data.Taken() && d.RestoreData != nil {
		if d.Stop != nil {
			if err := d.Stop(ctx, sr.Subsystem); err != nil {
				// Without the stop the import would run against a live service.
				// Refusing to import is right: a half-restored volume is worse than
				// a migrated one, because it looks readable and is not.
				sr.RollbackIncomplete = true
				sr.Err = errors.Join(cause, fmt.Errorf("the subsystem could not be stopped to restore its data, so the data was left as the update made it: %w", err))
				return sr
			}
		}
		if err := d.RestoreData(ctx, sr.Subsystem, capture.Data); err != nil {
			// THE WORST STATE IN THE LIFECYCLE: the data is whatever the failed
			// import left, and villa cannot say what that is. It never reads as a
			// clean rollback (ADR-0003).
			sr.RollbackIncomplete = true
			sr.Err = errors.Join(cause, fmt.Errorf("the data volume could not be restored: %w", err))
			if d.Start != nil {
				if startErr := d.Start(ctx, sr.Subsystem); startErr != nil {
					sr.Err = errors.Join(sr.Err, fmt.Errorf("the services could not be started again: %w", startErr))
				}
			}
			return sr
		}
		restoredData = true
	}

	if err := d.Restore(ctx, sr.Subsystem, capture); err != nil {
		// The restore itself failed. This is the worst state and must never be
		// reported as a clean rollback.
		sr.RollbackIncomplete = true
		sr.Err = errors.Join(cause, fmt.Errorf("rollback failed: %w", err))
		if restoredData && d.Start != nil {
			// Villa opened the window, so it closes it even on the way out.
			if startErr := d.Start(ctx, sr.Subsystem); startErr != nil {
				sr.Err = errors.Join(sr.Err, fmt.Errorf("the services could not be started again: %w", startErr))
			}
		}
		return sr
	}

	if restoredData && d.Start != nil {
		// Close the window BEFORE the re-proof: a proof cannot observe a stopped
		// service, and reporting rollback-incomplete because villa never restarted
		// what it stopped would blame the restore for villa's own omission.
		if err := d.Start(ctx, sr.Subsystem); err != nil {
			sr.RollbackIncomplete = true
			sr.Err = errors.Join(cause, fmt.Errorf("the data and pin were restored but the subsystem could not be started: %w", err))
			return sr
		}
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
