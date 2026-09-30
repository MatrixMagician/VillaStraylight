package doctor

import (
	"strings"
	"testing"
)

// TestToolsDriftFindings guards TMD-01's whole truth table: the served unit must
// carry the tool-calling flag iff the gate is answered on, a mismatch in EITHER
// direction is a confident FAIL, and an unanswerable question is a typed-Unknown
// WARN rather than a matching PASS. (The reads that feed it are covered by
// TestToolsDriftReadsTheServedUnit.)
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
			f := toolsDriftFinding(tc.served, tc.want, tc.ok)
			if f.ID != "TMD-01" {
				t.Fatalf("ID = %q, want TMD-01", f.ID)
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
		})
	}
}
