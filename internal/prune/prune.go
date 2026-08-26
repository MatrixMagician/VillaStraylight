// Package prune decides which superseded images `villa update` may remove.
//
// It is the pure half of THE FIRST CODE IN THIS PROJECT THAT DELETES A CONTAINER
// IMAGE. There is no `podman image rm` anywhere else in the tree — even `villa
// uninstall` leaves images alone — which is why the decision lives in its own
// package with its own tests rather than inline in the apply path.
//
// # Reference counting is a safety property, not tidiness
//
// Two components share one image today: `embedImage` and `vulkanImage` are
// byte-identical digests, one image serving two roles — the inference backend and
// the embeddings server. So when memory updates, the old embedder digest becomes
// memory's retained previous WHILE that same digest may be the inference backend's
// CURRENT effective pin.
//
// A per-component prune would delete it. That is not a lost rollback; it breaks a
// running stack.
//
// An image is therefore removable only when NO current pin and NO retained previous
// anywhere in the store references it. References are counted over RESOLVED DIGEST
// VALUES rather than over component identities, so two accessors that happen to
// return the same string count as two references without either needing to know
// about the other.
//
// # The no-op must be reported
//
// A consequence worth stating because it will look like a bug: prune sometimes
// no-ops right after a successful update, because the digest it would have removed
// is still referenced elsewhere. Every decision here carries its reason, so the
// caller can say "retained — still referenced by the inference backend" instead of
// leaving a user to wonder why the old image is still on disk.
//
// # Villa can only speak for what it can see
//
// An unreadable store yields RETAIN EVERYTHING, never an empty reference set. An
// empty set reads as "nothing is referenced, delete freely", which is the one
// reading that could delete a running stack's image (ADR-0004).
//
// PURE: no podman, no I/O. The store is a value in, the decisions are values out.
package prune

import (
	"fmt"
	"sort"

	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// Action is what should happen to one image reference.
type Action string

const (
	// Remove: nothing in the store references this image. It is a removal
	// candidate.
	Remove Action = "remove"
	// Retain: something still references it. Reported, never silent, so the no-op
	// does not read as a bug.
	Retain Action = "retain"
	// MissingPrevious: the store records a previous that is not on the host. This
	// is SURFACED rather than fail-softed: someone running `podman image prune` by
	// hand loses rollback protection, and villa should say so rather than proceed
	// as though it still had a safety net.
	MissingPrevious Action = "missing_previous"
)

// Decision is one image's outcome, with the reason that produced it.
type Decision struct {
	Ref    string
	Action Action
	// Reason is the human explanation. It is produced here rather than at the
	// command tier because the reason is a fact about the reference graph, and
	// re-deriving it from an enum at each call site is how "retained" ends up
	// printed with no explanation attached.
	Reason string
}

// Input is everything a decision depends on.
type Input struct {
	// State is the whole pin state store. The WHOLE store, deliberately: a
	// per-subsystem view could not see that another subsystem still references the
	// image being considered, which is exactly the hazard reference counting exists
	// to close.
	State pinstate.State
	// StateKnown reports whether the store was READABLE. False means villa cannot
	// tell what is referenced, which is not the same as "nothing is referenced" —
	// and only one of those permits a removal.
	StateKnown bool
	// Superseded is the reference this subsystem just moved off, per component.
	Superseded map[string]string
	// Subsystem is whose update produced them.
	Subsystem subsystem.Kind
	// Present reports whether an image is on the host, so a recorded previous that
	// has gone missing can be surfaced. A nil func means villa cannot check, and
	// missing-previous detection is skipped rather than guessed at.
	Present func(ref string) bool
}

// Plan is the whole set of decisions.
type Plan struct {
	Decisions []Decision
	// Blocked is true when villa could not read the store and therefore refuses to
	// remove anything. It is separate from an empty decision list because "nothing
	// to remove" and "villa will not remove" are different statements.
	Blocked bool
	// BlockedReason explains the refusal.
	BlockedReason string
}

// Removable is the references this plan would actually delete.
func (p Plan) Removable() []string {
	var out []string
	for _, d := range p.Decisions {
		if d.Action == Remove {
			out = append(out, d.Ref)
		}
	}
	return out
}

// Decide builds the plan.
//
// The order of checks is the safety order: the store must be readable before
// anything is considered for removal, and every reference in it counts before any
// single image is judged.
func Decide(in Input) Plan {
	if !in.StateKnown {
		// THE UNSAFE DIRECTION, refused explicitly. An unreadable store yields an
		// empty reference set, which reads as "nothing is referenced, delete
		// freely" — and this is the only code in the project that would act on
		// that reading.
		return Plan{
			Blocked: true,
			BlockedReason: "villa could not read its record of which images are in use, so it will not remove any. " +
				"An unreadable record is not the same as an empty one, and only one of those makes an image safe to delete.",
		}
	}

	referenced := referenceSet(in.State)

	// The superseded references, deduplicated and ordered, so the plan is
	// deterministic and a caller printing it twice prints the same thing.
	seen := map[string]bool{}
	var refs []string
	for _, ref := range in.Superseded {
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	var plan Plan
	for _, ref := range refs {
		if holders := referenced[ref]; len(holders) > 0 {
			plan.Decisions = append(plan.Decisions, Decision{
				Ref:    ref,
				Action: Retain,
				Reason: fmt.Sprintf("still referenced by %s", describeHolders(holders)),
			})
			continue
		}
		plan.Decisions = append(plan.Decisions, Decision{
			Ref:    ref,
			Action: Remove,
			Reason: "no current pin and no retained previous references it",
		})
	}

	plan.Decisions = append(plan.Decisions, missingPreviousDecisions(in)...)
	return plan
}

// referenceSet maps every referenced image to what holds it.
//
// It walks BOTH the current effective pins and every retained previous, across the
// WHOLE store. A retained previous is a reference in exactly the sense that
// matters: it is a rollback target, and removing it removes the safety net without
// removing the record that claims one exists.
func referenceSet(state pinstate.State) map[string][]string {
	refs := map[string][]string{}
	for id, eff := range state.Pins {
		if eff.Ref != "" {
			refs[eff.Ref] = append(refs[eff.Ref], "the current pin for "+id)
		}
	}
	for sub, prev := range state.Previous {
		for id, ref := range prev.Refs {
			if ref != "" {
				refs[ref] = append(refs[ref], fmt.Sprintf("the retained previous for %s (%s)", sub, id))
			}
		}
	}
	for ref := range refs {
		sort.Strings(refs[ref])
	}
	return refs
}

// describeHolders renders the holders of a reference.
func describeHolders(holders []string) string {
	if len(holders) == 1 {
		return holders[0]
	}
	return fmt.Sprintf("%s (and %d other reference(s))", holders[0], len(holders)-1)
}

// missingPreviousDecisions surfaces a recorded previous that is no longer on the
// host.
//
// SURFACED, not fail-softed. Someone running `podman image prune` by hand loses
// rollback protection, and villa proceeding as though it still had a safety net
// would be claiming a guarantee it cannot honour. The image store is the
// operator's; villa's claim is limited to what it can see, and saying so is the
// honest version of that limit.
func missingPreviousDecisions(in Input) []Decision {
	if in.Present == nil {
		return nil
	}
	prev, ok := in.State.PreviousFor(in.Subsystem)
	if !ok {
		return nil
	}

	var out []Decision
	var refs []string
	for _, ref := range prev.Refs {
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)

	for _, ref := range refs {
		if in.Present(ref) {
			continue
		}
		out = append(out, Decision{
			Ref:    ref,
			Action: MissingPrevious,
			Reason: fmt.Sprintf("recorded as the known-good previous for %s, but it is no longer in the image store — "+
				"something removed it outside villa, so rollback protection for %s is incomplete", in.Subsystem, in.Subsystem),
		})
	}
	return out
}
