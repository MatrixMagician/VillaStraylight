package doctor

import (
	"strings"
	"testing"
)

// TestToolsDriftFindings guards TMD-01's whole truth table: the served unit must
// carry the tool-calling flag iff the gate is answered on, a mismatch in EITHER
// direction is a confident FAIL, and an unanswerable question is a typed-Unknown
// WARN rather than a matching PASS.
func TestToolsDriftFindings(t *testing.T) {
	for _, tc := range []struct {
		name       string
		served     bool
		want       bool
		ok         bool
		wantStatus string
		wantDetail string
	}{
		{"on and served", true, true, true, statusPass, "matches tools mode (on)"},
		{"off and not served", false, false, true, statusPass, "matches tools mode (off)"},
		{"on but not served", false, true, true, statusFail, "every tool call will fail"},
		{"served but off", true, false, true, statusFail, "nobody asked for"},
		{"unreadable", false, false, false, statusWarn, "could not read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoctorDeps()
			d.ToolsDrift = func() (bool, bool, bool) { return tc.served, tc.want, tc.ok }
			r := Aggregate(d)

			f, present := findingByID(r, "TMD-01")
			if !present {
				t.Fatalf("TMD-01 absent from the report")
			}
			if f.Status != tc.wantStatus {
				t.Errorf("Status = %q, want %q (detail %q)", f.Status, tc.wantStatus, f.Detail)
			}
			if !strings.Contains(f.Detail, tc.wantDetail) {
				t.Errorf("Detail = %q, want it to mention %q", f.Detail, tc.wantDetail)
			}
			if tc.wantStatus != statusPass && f.Remediation == "" {
				t.Error("a non-PASS TMD-01 carries no remediation")
			}
			if f.Provenance == "" {
				t.Error("TMD-01 carries no provenance")
			}
			if tc.wantStatus == statusFail && r.Overall != statusFail {
				t.Errorf("Overall = %q, want a confident tools-mode drift to fold FAIL", r.Overall)
			}
		})
	}
}

// TestToolsDriftSeamIsNilSafe guards the no-PASS-by-default rule: a caller that
// could not bind the seam emits no TMD-01 line at all rather than a finding claiming
// the unit matches.
func TestToolsDriftSeamIsNilSafe(t *testing.T) {
	d := newDoctorDeps()
	d.ToolsDrift = nil
	if _, present := findingByID(Aggregate(d), "TMD-01"); present {
		t.Error("a nil ToolsDrift seam emitted a TMD-01 finding")
	}
}
