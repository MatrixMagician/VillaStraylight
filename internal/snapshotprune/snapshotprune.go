// Package snapshotprune decides which retained data snapshots `villa update` may
// remove, and reports the ones that have gone missing.
//
// It is the data-shaped sibling of internal/prune, split out for the same reason
// prune was split from apply: this is the DESTRUCTIVE half. Apply-without-cleanup
// is safe — it merely accumulates snapshots — which is exactly why the two can be
// separated, and why the deleting decision earns its own package and its own tests.
//
// # The graph is simpler than the image graph, and the instinct is the same
//
// Two components share one image today, so an image is removable only when nothing
// anywhere in the store references it (ADR-0004). A snapshot has no such hazard: it
// belongs to exactly one subsystem and nothing else can point at it. But the
// instinct that produced the reference count still applies — NEVER remove the
// snapshot the current retained tuple points at. That is the live rollback target,
// and removing it silently converts rollback protection into a claim villa cannot
// honour.
//
// # A missing snapshot is surfaced, not fail-softed
//
// Someone clearing disk by hand has removed the rollback target. Villa proceeding as
// though it still had a safety net would be claiming a guarantee it cannot keep, so
// the absence is reported the same way a missing previous image is.
//
// PURE: no filesystem, no podman. The store is a value in, the decisions are values
// out; whether a file exists arrives through an injected func.
package snapshotprune

import (
	"fmt"
	"sort"

	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// Action is what should happen to one snapshot.
type Action string

const (
	// Remove: this snapshot is superseded and nothing references it.
	Remove Action = "remove"
	// Retain: the current retained tuple points at it. It is the live rollback
	// target, and reported rather than silently skipped so the leftover does not
	// read as a bug.
	Retain Action = "retain"
	// Missing: the store records a snapshot that is not on disk. SURFACED, because
	// rollback protection is incomplete and only the operator can decide what to do
	// about it.
	Missing Action = "missing"
)

// Decision is one snapshot's outcome, with the reason that produced it.
type Decision struct {
	// Subsystem is whose snapshot this is.
	Subsystem subsystem.Kind
	// Path is the snapshot file.
	Path string
	// Bytes is what it occupies, so a caller can say what a removal reclaimed.
	Bytes  int64
	Action Action
	// Reason is the human explanation, produced here rather than at the command
	// tier because it is a fact about the record. Re-deriving it from an enum at
	// each call site is how "retained" ends up printed with no explanation.
	Reason string
}

// Input is everything a decision depends on.
type Input struct {
	// State is the whole pin state store, which is where the retained tuples live.
	State pinstate.State
	// StateKnown reports whether the store was READABLE. False means villa cannot
	// tell which snapshot is the live rollback target, which is not the same as
	// "none is" — and only one of those permits a removal.
	StateKnown bool
	// Superseded is the snapshot this subsystem's committed update displaced, if
	// any. Its zero value means there was nothing to displace.
	Superseded pinstate.DataSnapshot
	// Subsystem is whose update produced it.
	Subsystem subsystem.Kind
	// Present reports whether a snapshot file is on disk, so a recorded one that
	// has gone missing can be surfaced. A nil func means villa cannot check, and
	// the check is skipped rather than guessed at.
	Present func(path string) bool
}

// Plan is the whole set of decisions.
type Plan struct {
	Decisions []Decision
	// Blocked is true when villa could not read the store and therefore refuses to
	// remove anything. Separate from an empty decision list, because "nothing to
	// remove" and "villa will not remove" are different statements.
	Blocked bool
	// BlockedReason explains the refusal.
	BlockedReason string
}

// Removable is the snapshot paths this plan would actually delete.
func (p Plan) Removable() []string {
	var out []string
	for _, d := range p.Decisions {
		if d.Action == Remove {
			out = append(out, d.Path)
		}
	}
	return out
}

// Decide builds the plan.
//
// The order of checks is the safety order: the store must be readable before
// anything is considered, and the live rollback target is identified before any
// single snapshot is judged.
func Decide(in Input) Plan {
	if !in.StateKnown {
		// THE UNSAFE DIRECTION, refused explicitly. An unreadable store yields an
		// empty set of retained tuples, which reads as "no snapshot is the rollback
		// target, delete freely".
		return Plan{
			Blocked: true,
			BlockedReason: "villa could not read its record of which snapshots are rollback targets, so it will not remove any. " +
				"An unreadable record is not the same as an empty one, and only one of those makes a snapshot safe to delete.",
		}
	}

	live := liveTargets(in.State)

	var plan Plan
	if in.Superseded.Taken() {
		switch {
		case live[in.Superseded.Path] != "":
			// The current retained tuple still points at it, which happens when a
			// commit did not displace the snapshot after all. Removing it would
			// convert rollback protection into a claim villa cannot honour.
			plan.Decisions = append(plan.Decisions, Decision{
				Subsystem: in.Subsystem,
				Path:      in.Superseded.Path,
				Bytes:     in.Superseded.Bytes,
				Action:    Retain,
				Reason:    "it is the rollback target " + live[in.Superseded.Path] + " points at",
			})
		default:
			plan.Decisions = append(plan.Decisions, Decision{
				Subsystem: in.Subsystem,
				Path:      in.Superseded.Path,
				Bytes:     in.Superseded.Bytes,
				Action:    Remove,
				Reason:    "it was displaced by the snapshot taken for this update, and no retained tuple points at it",
			})
		}
	}

	plan.Decisions = append(plan.Decisions, missingDecisions(in)...)
	return plan
}

// liveTargets maps every snapshot path the store points at to a description of what
// points at it.
//
// The WHOLE store, deliberately. A per-subsystem view could not see that another
// subsystem's tuple names the same path, and while that cannot happen today — a
// snapshot belongs to exactly one subsystem — assuming it never will is the same
// assumption the image path had to unlearn.
func liveTargets(state pinstate.State) map[string]string {
	out := map[string]string{}
	for sub, prev := range state.Previous {
		if prev.Data.Taken() {
			out[prev.Data.Path] = "the retained previous for " + sub
		}
	}
	return out
}

// missingDecisions surfaces a recorded snapshot that is no longer on disk.
//
// SURFACED, not fail-softed. Someone clearing disk by hand has removed the rollback
// target, and villa proceeding as though it still had a safety net would be claiming
// a guarantee it cannot honour.
func missingDecisions(in Input) []Decision {
	if in.Present == nil {
		return nil
	}

	// Every subsystem's recorded snapshot, not only this one's: a cleanup pass is
	// the natural moment to notice that another subsystem's rollback target went
	// away, and the user is equally unable to see it either way.
	var subs []string
	for sub := range in.State.Previous {
		subs = append(subs, sub)
	}
	sort.Strings(subs)

	var out []Decision
	for _, sub := range subs {
		prev := in.State.Previous[sub]
		if !prev.Data.Taken() || in.Present(prev.Data.Path) {
			continue
		}
		out = append(out, Decision{
			Subsystem: subsystemByName(sub),
			Path:      prev.Data.Path,
			Bytes:     prev.Data.Bytes,
			Action:    Missing,
			Reason: fmt.Sprintf("recorded as the known-good data for %s, but it is no longer on disk — "+
				"something removed it outside villa, so %s can no longer be rolled back to the data its previous pin was proven against", sub, sub),
		})
	}
	return out
}

// subsystemByName resolves a stored subsystem key back to its Kind.
//
// The store keys Previous by the subsystem's String(), so this is the inverse. An
// unrecognised key yields the zero Kind rather than an error: a decision about a
// subsystem this binary no longer knows is still worth surfacing, and the path is
// what the operator acts on.
func subsystemByName(name string) subsystem.Kind {
	for _, k := range subsystem.Every {
		if k.String() == name {
			return k
		}
	}
	return subsystem.Kind(0)
}
