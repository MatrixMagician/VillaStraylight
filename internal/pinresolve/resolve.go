// Package pinresolve is the single answer to "what should this component run?".
//
// It joins two structures that are deliberately kept apart. The compiled-in
// pins.Table cannot be absent or corrupt — a malformed one is a build-time
// programming error. The pinstate store routinely is both — it may never have been
// written, or may have been hand-deleted. Merging them would blur a thing that
// cannot fail with a thing that fails all the time, and would give the combined
// type error paths half of it can never reach.
//
// The resolver is where they meet, and the rule is one line: the effective pin if
// the store has one, the vetted pin otherwise.
//
// # Why it also returns the vetted pin
//
// Because the interesting question is usually not "which pin wins" but "do they
// disagree". `--check` reports divergence, `doctor` reports running-a-digest-villa-
// never-vetted, and both need the loser as well as the winner. A resolver returning
// only the winner would force every caller to re-open the table to find out what it
// beat, and one of them would eventually get it wrong.
//
// PURE: it takes an already-loaded table and an already-loaded state. No I/O, no
// seam, nothing to inject — which is what makes the whole thing table-testable.
package pinresolve

import (
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// Resolved is one component's answer: what it should run, what villa vetted, and
// whether those are the same thing.
type Resolved struct {
	// Component is the id this answer is about.
	Component pins.ComponentID
	// Subsystem is which part of the stack gates it, carried through so a caller
	// grouping by proof unit does not re-open the table.
	Subsystem subsystem.Kind
	// Shape is what a moved pin means for this component, carried through for the
	// same reason and because it is part of the --json contract.
	Shape pins.Shape
	// Current is the pin this host should run: the effective pin when one is
	// recorded, the vetted pin otherwise.
	Current pins.Pin
	// Vetted is what villa proved on gfx1151 hardware. It is returned alongside
	// Current, not instead of it, because divergence is the thing callers report.
	Vetted pins.Pin
	// FromStore reports whether Current came from the host's state rather than the
	// compiled-in table. It is distinct from Diverged: a host can record an
	// effective pin EQUAL to the vetted one (a re-install, or an update that
	// happened to land on the same digest), and "recorded" and "different" are two
	// different facts.
	FromStore bool
}

// Diverged reports whether this host is running something other than what villa
// vetted. It is the question `doctor` asks, phrased once here so every caller
// phrases it the same way.
func (r Resolved) Diverged() bool { return r.Current.Ref != r.Vetted.Ref }

// Resolver holds the two halves. It is a value, not a constructor with hidden
// state, so a test builds one from a literal table and a literal state.
type Resolver struct {
	table []pins.Entry
	state pinstate.State
}

// New builds a resolver over the compiled-in table and an already-loaded state.
//
// The state is passed in rather than loaded here, because loading is I/O and this
// package does none. A caller that could not read the store passes the zero State,
// which resolves everything to vetted pins — the correct behaviour for an
// unreadable store, and the reason this takes a value rather than a Deps.
func New(state pinstate.State) Resolver {
	return Resolver{table: pins.Table(), state: state}
}

// NewWithTable builds a resolver over a caller-supplied table, for tests that need
// to drive component shapes the real table does not contain.
func NewWithTable(table []pins.Entry, state pinstate.State) Resolver {
	return Resolver{table: table, state: state}
}

// Resolve answers for one component. The bool is false when the table does not name
// the component at all, which is the allowlist answer rather than a lookup failure.
func (r Resolver) Resolve(id pins.ComponentID) (Resolved, bool) {
	for _, e := range r.table {
		if e.Component == id {
			return r.resolveEntry(e), true
		}
	}
	return Resolved{}, false
}

// resolveEntry applies the one rule: effective if recorded, vetted otherwise.
func (r Resolver) resolveEntry(e pins.Entry) Resolved {
	vetted := e.Vetted()
	out := Resolved{
		Component: e.Component,
		Subsystem: e.Subsystem,
		Shape:     e.Shape,
		Current:   vetted,
		Vetted:    vetted,
	}
	eff, ok := r.state.EffectiveFor(e.Component)
	// An empty recorded ref is treated as no record. A store carrying a component
	// key with a blank pin is not a host declaring it runs nothing — it is a
	// half-written document, and falling back to the vetted pin is the only reading
	// that leaves the stack runnable.
	if ok && eff.Ref != "" {
		out.Current = pins.Pin{Ref: eff.Ref, Checksum: eff.Checksum}
		out.FromStore = true
	}
	return out
}

// All answers for every component in table order.
func (r Resolver) All() []Resolved {
	out := make([]Resolved, 0, len(r.table))
	for _, e := range r.table {
		out = append(out, r.resolveEntry(e))
	}
	return out
}

// For answers for every component of one subsystem, in table order. It is what the
// update flow walks, because the proof unit is the subsystem.
func (r Resolver) For(k subsystem.Kind) []Resolved {
	var out []Resolved
	for _, e := range r.table {
		if e.Subsystem == k {
			out = append(out, r.resolveEntry(e))
		}
	}
	return out
}

// Diverged returns every component running something villa did not vet. It is the
// list `doctor` reports as a partial-update state, which the vetted/effective split
// exists to make legible rather than lurking.
func (r Resolver) Diverged() []Resolved {
	var out []Resolved
	for _, res := range r.All() {
		if res.Diverged() {
			out = append(out, res)
		}
	}
	return out
}
