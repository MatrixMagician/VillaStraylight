package orchestrate

// subsystemunits_drift_test.go binds internal/subsystem's unit/service
// declaration (Kind.Units) to the units this package actually renders.
//
// The declaration lives in internal/subsystem because it is a property of a
// subsystem, and it re-declares the unit names rather than reading them from
// here because the import only runs one way: orchestrate imports subsystem,
// never the reverse. Two declarations of one fact is exactly the drift a
// cross-package test exists to catch — the same shape as the statevolume and
// embed-GGUF-filename drift tests.
//
// It asserts against the RENDERED unit names, not against consts, so the test
// fails if a unit is ever renamed or a subsystem gains a unit while the
// declaration keeps its old list. What update captures and restarts must be
// what install renders.

import (
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// TestSubsystemUnitsMatchTheRenderedUnits: every .container unit
// subsystem.Units names is genuinely rendered when the whole stack is on, every
// rendered .container unit belongs to exactly one subsystem's declaration, and
// each declared service is the Quadlet mapping of its unit.
func TestSubsystemUnitsMatchTheRenderedUnits(t *testing.T) {
	units, err := Render(statefulFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	rendered := map[string]bool{}
	for _, u := range units {
		if strings.HasSuffix(u.Name, ".container") {
			rendered[u.Name] = true
		}
	}

	declared := map[string]subsystem.Kind{}
	for _, k := range subsystem.Every {
		us, svcs := k.Units()
		if len(us) != len(svcs) {
			t.Errorf("%v declares %d units but %d services — they pair positionally", k, len(us), len(svcs))
		}
		for i, unit := range us {
			// Quadlet maps villa-x.container → villa-x.service.
			wantSvc := strings.TrimSuffix(unit, ".container") + ".service"
			if i < len(svcs) && svcs[i] != wantSvc {
				t.Errorf("%v: unit %q pairs with service %q, want the Quadlet mapping %q", k, unit, svcs[i], wantSvc)
			}
			if prev, dup := declared[unit]; dup {
				t.Errorf("unit %q is declared by both %v and %v — a unit moves with exactly one subsystem", unit, prev, k)
			}
			declared[unit] = k
			if !rendered[unit] {
				t.Errorf("%v declares unit %q, which the full-stack render does not produce (the declaration drifted)", k, unit)
			}
		}
	}

	for name := range rendered {
		if _, ok := declared[name]; !ok {
			t.Errorf("rendered unit %q belongs to no subsystem's Units declaration — update would neither capture nor restart it", name)
		}
	}

	// The agent is a file, not a unit — its declaration must stay empty.
	if us, svcs := subsystem.Agent.Units(); len(us) != 0 || len(svcs) != 0 {
		t.Errorf("Agent.Units() = (%v, %v), want empty — the Crush binary is a file, not a unit", us, svcs)
	}

	// CodingMode is a configuration flip of the inference unit, not its own unit.
	if us, _ := subsystem.CodingMode.Units(); !slices.Equal(us, []string(nil)) {
		t.Errorf("CodingMode.Units() = %v, want empty — coding mode flips villa-llama, it does not own a unit", us)
	}
}
