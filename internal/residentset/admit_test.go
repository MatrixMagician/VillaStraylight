package residentset

import (
	"reflect"
	"testing"
)

// slot is a terse test-only Slot constructor so each test can name only the
// fields that matter to its invariant.
func slot(model string, ctx, port int, unit string, primary bool, bytes, order uint64) Slot {
	return Slot{Model: model, Quant: "q4", Ctx: ctx, Port: port, Unit: unit, Primary: primary, Bytes: bytes, Order: order}
}

// TestAdmitCandidateAlreadyResidentIsNoOp guards: admitting a candidate whose
// model/quant/ctx is already resident returns a NoOp plan, never a
// duplicate Add — even when the candidate's Port/Unit differ from the
// resident slot's (a re-check need not know the existing connection info).
func TestAdmitCandidateAlreadyResidentIsNoOp(t *testing.T) {
	s := Set{
		Envelope: 100 << 30,
		Slots:    []Slot{slot("m1", 4096, 8001, "villa-llama-1.service", false, 10<<30, 1)},
	}
	candidate := slot("m1", 4096, 9001, "villa-llama-9.service", false, 10<<30, 2)

	plan, refusal := Admit(s, candidate, Policy{})

	if !plan.NoOp || plan.Add != nil || plan.Evict != nil {
		t.Fatalf("want NoOp plan with no Add/Evict, got %+v", plan)
	}
	if refusal != (Refusal{}) {
		t.Fatalf("want no refusal, got %+v", refusal)
	}
}

// TestAdmitFitsWithoutEviction guards: a candidate that fits inside
// Envelope minus the headroom margin, given what is already resident, is
// admitted with an empty Evict list.
func TestAdmitFitsWithoutEviction(t *testing.T) {
	s := Set{
		Envelope: 100 << 30,
		Slots:    []Slot{slot("m1", 4096, 8001, "villa-llama-1.service", true, 40<<30, 1)},
	}
	candidate := slot("m2", 4096, 8002, "villa-llama-2.service", false, 30<<30, 2)
	policy := Policy{AllowEviction: true, HeadroomBytes: 10 << 30}

	plan, refusal := Admit(s, candidate, policy)

	if refusal != (Refusal{}) {
		t.Fatalf("want no refusal, got %+v", refusal)
	}
	if len(plan.Evict) != 0 {
		t.Fatalf("want empty Evict, got %+v", plan.Evict)
	}
	if len(plan.Add) != 1 || plan.Add[0] != candidate {
		t.Fatalf("want Add=[candidate], got %+v", plan.Add)
	}
}

// TestAdmitRefusedWhenEvictionDisallowedAndDoesNotFit guards: a candidate
// that does not fit, with eviction disallowed by policy, is REFUSED with a
// populated Remediation and no partial plan.
func TestAdmitRefusedWhenEvictionDisallowedAndDoesNotFit(t *testing.T) {
	s := Set{
		Envelope: 50 << 30,
		Slots:    []Slot{slot("m1", 4096, 8001, "villa-llama-1.service", true, 40<<30, 1)},
	}
	candidate := slot("m2", 4096, 8002, "villa-llama-2.service", false, 20<<30, 2)
	policy := Policy{AllowEviction: false}

	plan, refusal := Admit(s, candidate, policy)

	if refusal.Reason != ReasonEvictionDisallowed {
		t.Fatalf("want ReasonEvictionDisallowed, got %+v", refusal)
	}
	if refusal.Remediation == "" {
		t.Fatalf("want a populated Remediation")
	}
	if !reflect.DeepEqual(plan, Plan{}) {
		t.Fatalf("want zero (no partial) plan, got %+v", plan)
	}
}

// TestAdmitEvictsLRUNonPrimaryUntilFits guards: a candidate that does not
// fit, with eviction allowed, evicts least-recently-used non-Primary slots
// — and only as many as needed to fit.
func TestAdmitEvictsLRUNonPrimaryUntilFits(t *testing.T) {
	older := slot("m-older", 4096, 8002, "villa-llama-2.service", false, 20<<30, 1)
	newer := slot("m-newer", 4096, 8003, "villa-llama-3.service", false, 20<<30, 2)
	primary := slot("m-primary", 4096, 8001, "villa-llama-1.service", true, 20<<30, 0)
	s := Set{
		Envelope: 60 << 30,
		Slots:    []Slot{primary, older, newer},
	}
	candidate := slot("m-cand", 4096, 8004, "villa-llama-4.service", false, 15<<30, 3)
	policy := Policy{AllowEviction: true}

	plan, refusal := Admit(s, candidate, policy)

	if refusal != (Refusal{}) {
		t.Fatalf("want no refusal, got %+v", refusal)
	}
	if len(plan.Add) != 1 || plan.Add[0] != candidate {
		t.Fatalf("want Add=[candidate], got %+v", plan.Add)
	}
	if !reflect.DeepEqual(plan.Evict, []Slot{older}) {
		t.Fatalf("want only the LRU non-primary slot evicted, got %+v", plan.Evict)
	}
}

// TestAdmitNeverEvictsPrimary guards: when the only way to fit a candidate
// is evicting the Primary slot, Admit REFUSES instead — Primary is never
// evictable, even with eviction allowed.
func TestAdmitNeverEvictsPrimary(t *testing.T) {
	primary := slot("m-primary", 4096, 8001, "villa-llama-1.service", true, 45<<30, 1)
	s := Set{
		Envelope: 50 << 30,
		Slots:    []Slot{primary},
	}
	candidate := slot("m-cand", 4096, 8002, "villa-llama-2.service", false, 20<<30, 2)
	policy := Policy{AllowEviction: true}

	plan, refusal := Admit(s, candidate, policy)

	if refusal.Reason != ReasonWouldEvictPrimary {
		t.Fatalf("want ReasonWouldEvictPrimary, got %+v", refusal)
	}
	if refusal.Remediation == "" {
		t.Fatalf("want a populated Remediation")
	}
	if !reflect.DeepEqual(plan, Plan{}) {
		t.Fatalf("want zero (no partial) plan, got %+v", plan)
	}
}

// TestAdmitRefusesCandidateLargerThanEnvelope guards: a candidate larger
// than the whole Envelope is refused immediately regardless of policy, with
// a reason distinct from the evictable-but-disallowed case.
func TestAdmitRefusesCandidateLargerThanEnvelope(t *testing.T) {
	s := Set{Envelope: 10 << 30}
	candidate := slot("m-huge", 4096, 8001, "villa-llama-1.service", false, 20<<30, 1)

	plan, refusal := Admit(s, candidate, Policy{AllowEviction: true})

	if refusal.Reason != ReasonExceedsEnvelope {
		t.Fatalf("want ReasonExceedsEnvelope, got %+v", refusal)
	}
	if refusal.Reason == ReasonEvictionDisallowed {
		t.Fatalf("reason must be distinct from the evictable case")
	}
	if !reflect.DeepEqual(plan, Plan{}) {
		t.Fatalf("want zero (no partial) plan, got %+v", plan)
	}
}

// TestAdmitRefusesPortOrUnitCollisionInSet guards: a Set whose slots share
// a Port or a Unit is refused rather than planned against — two slots may
// never share either.
func TestAdmitRefusesPortOrUnitCollisionInSet(t *testing.T) {
	a := slot("m-a", 4096, 8001, "villa-llama-1.service", false, 5<<30, 1)
	b := slot("m-b", 4096, 8001, "villa-llama-2.service", false, 5<<30, 2) // shares a's Port
	s := Set{Envelope: 100 << 30, Slots: []Slot{a, b}}
	candidate := slot("m-c", 4096, 8003, "villa-llama-3.service", false, 5<<30, 3)

	_, refusal := Admit(s, candidate, Policy{AllowEviction: true})

	if refusal.Reason != ReasonPortUnitCollision {
		t.Fatalf("want ReasonPortUnitCollision, got %+v", refusal)
	}
}

// TestAdmitIsPure guards: Admit is pure. Calling it twice with the same Set
// returns the same Plan and never mutates the input Set or its Slots
// slice.
func TestAdmitIsPure(t *testing.T) {
	older := slot("m-older", 4096, 8002, "villa-llama-2.service", false, 20<<30, 1)
	newer := slot("m-newer", 4096, 8003, "villa-llama-3.service", false, 20<<30, 2)
	primary := slot("m-primary", 4096, 8001, "villa-llama-1.service", true, 20<<30, 0)
	s := Set{Envelope: 60 << 30, Slots: []Slot{primary, older, newer}}
	before := Set{Envelope: s.Envelope, Slots: append([]Slot(nil), s.Slots...)}
	candidate := slot("m-cand", 4096, 8004, "villa-llama-4.service", false, 15<<30, 3)
	policy := Policy{AllowEviction: true}

	plan1, refusal1 := Admit(s, candidate, policy)
	plan2, refusal2 := Admit(s, candidate, policy)

	if !reflect.DeepEqual(plan1, plan2) || refusal1 != refusal2 {
		t.Fatalf("want identical results across calls, got %+v/%+v and %+v/%+v", plan1, refusal1, plan2, refusal2)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatalf("Admit mutated its input Set: got %+v, want %+v", s, before)
	}
}

// TestAdmitNamesTheRealBlockerWhenNothingIsEvictable guards the honesty of the
// refusal itself: a refusal must name the cause that actually applies. With an
// empty set there is no primary slot and nothing to evict, so blaming the
// primary is a false explanation of a true refusal.
func TestAdmitNamesTheRealBlockerWhenNothingIsEvictable(t *testing.T) {
	s := Set{Envelope: 100, Slots: nil}
	cand := Slot{Model: "m", Port: 1, Unit: "u", Bytes: 60}
	_, ref := Admit(s, cand, Policy{AllowEviction: true, HeadroomBytes: 50})
	if ref.Reason == "" {
		t.Fatalf("expected a refusal, got none")
	}
	if ref.Reason == ReasonWouldEvictPrimary {
		t.Fatalf("refusal blames the primary slot, but the set is empty and has no primary: reason=%q remediation=%q", ref.Reason, ref.Remediation)
	}
}
