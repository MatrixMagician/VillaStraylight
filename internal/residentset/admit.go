// admit.go holds Admit itself: the one entry point that turns a proposed
// Set + candidate + Policy into a Plan or a Refusal. It upholds the
// package's zero-host-I/O invariant — every helper here reads its
// arguments and returns a value, never touching the arguments in place, so
// Admit is safe to call speculatively (e.g. to preview a plan before a
// caller decides to run it).
package residentset

import (
	"cmp"
	"slices"
)

// Admit decides whether candidate may join s: NoOp when it is already
// resident, a plain Add when it fits without evicting anything, an
// Add+Evict when it fits only after evicting least-recently-used
// non-Primary slots, or a Refusal. It performs no host I/O and mutates
// neither s nor s.Slots.
func Admit(s Set, candidate Slot, policy Policy) (Plan, Refusal) {
	for _, existing := range s.Slots {
		if sameWorkload(existing, candidate) {
			return Plan{NoOp: true}, Refusal{}
		}
	}

	if reason, remediation, collided := findCollision(s.Slots, candidate); collided {
		return Plan{}, Refusal{Reason: reason, Remediation: remediation}
	}

	if candidate.Bytes > s.Envelope {
		return Plan{}, Refusal{
			Reason:      ReasonExceedsEnvelope,
			Remediation: "this model does not fit the memory envelope at all — pick a smaller quant/context or add memory",
		}
	}

	available := saturatingSub(s.Envelope, policy.HeadroomBytes)
	used := usedBytes(s.Slots)
	if used+candidate.Bytes <= available {
		return Plan{Add: []Slot{candidate}}, Refusal{}
	}

	if !policy.AllowEviction {
		return Plan{}, Refusal{
			Reason:      ReasonEvictionDisallowed,
			Remediation: "free resident capacity manually, or retry with eviction allowed",
		}
	}

	evicted, freed := planEvictions(s.Slots, used, candidate.Bytes, available)
	if used-freed+candidate.Bytes > available {
		// Everything evictable is already in `evicted`, so what remains is the
		// Primary. Blaming it is only honest when dropping it would actually
		// admit the candidate; otherwise the envelope itself is the blocker.
		if candidate.Bytes <= available {
			return Plan{}, Refusal{
				Reason:      ReasonWouldEvictPrimary,
				Remediation: "the primary slot is never evicted — swap the primary with `villa model swap` if you want this model to lead",
			}
		}
		return Plan{}, Refusal{
			Reason:      ReasonInsufficientCapacity,
			Remediation: "this model does not fit the envelope once required headroom is reserved — pick a smaller quant/context or lower the headroom margin",
		}
	}

	return Plan{Add: []Slot{candidate}, Evict: evicted}, Refusal{}
}

// findCollision reports the first Port or Unit shared by two slots among
// s's existing slots plus candidate — an inconsistent set is refused
// rather than planned against.
func findCollision(slots []Slot, candidate Slot) (reason RefusalReason, remediation string, collided bool) {
	ports := make(map[int]bool, len(slots)+1)
	units := make(map[string]bool, len(slots)+1)
	all := append(slices.Clone(slots), candidate)
	for _, s := range all {
		if ports[s.Port] {
			return ReasonPortUnitCollision, "two resident slots cannot share a port — assign the candidate a distinct loopback port", true
		}
		ports[s.Port] = true
		if units[s.Unit] {
			return ReasonPortUnitCollision, "two resident slots cannot share a systemd unit name — assign the candidate a distinct unit", true
		}
		units[s.Unit] = true
	}
	return "", "", false
}

// planEvictions picks least-recently-used non-Primary slots (ascending
// Order) to evict, stopping as soon as the freed bytes would let candidate
// fit — so eviction never takes more than it needs. It never considers a
// Primary slot: if that leaves the candidate still over available, the
// caller reports ReasonWouldEvictPrimary rather than evicting one.
func planEvictions(slots []Slot, used, candidateBytes, available uint64) (evicted []Slot, freed uint64) {
	var evictable []Slot
	for _, s := range slots {
		if !s.Primary {
			evictable = append(evictable, s)
		}
	}
	slices.SortStableFunc(evictable, func(a, b Slot) int { return cmp.Compare(a.Order, b.Order) })

	for _, s := range evictable {
		if used-freed+candidateBytes <= available {
			break
		}
		evicted = append(evicted, s)
		freed += s.Bytes
	}
	return evicted, freed
}

// usedBytes sums the footprint of every already-resident slot.
func usedBytes(slots []Slot) uint64 {
	var total uint64
	for _, s := range slots {
		total += s.Bytes
	}
	return total
}

// saturatingSub returns a-b, floored at 0 — a headroom margin larger than
// the envelope means zero capacity, not a wrapped-huge uint64.
func saturatingSub(a, b uint64) uint64 {
	if b >= a {
		return 0
	}
	return a - b
}
