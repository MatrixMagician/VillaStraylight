package doctor

import (
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/verifystate"
)

// proofDeps is toolServerDeps with a recorded `villa verify tools` result.
func proofDeps(st *verifystate.State) *run {
	d := toolServerDeps()
	d.ReadToolsVerifyState = func() *verifystate.State { return st }
	return d
}

// TestToolServerProofFindingIsGatedOnTheGate: with the gate off Aggregate emits no
// tool-server-proof finding and never reads the record.
func TestToolServerProofFindingIsGatedOnTheGate(t *testing.T) {
	d := newDoctorDeps()
	d.ReadToolsVerifyState = func() *verifystate.State {
		t.Error("the tools verify record was read with the tool server off")
		return nil
	}
	if _, ok := findingByID(d.aggregate(), "tool-server-proof"); ok {
		t.Error("a tool-server-proof finding was emitted with the gate off")
	}
}

// TestToolServerProofFinding is the truth table over the recorded `villa verify tools`
// result: a fresh PASS is a PASS, a fresh FAIL a BLOCK-class FAIL that fails doctor, a fresh
// REJECT a WARN that says the proof could not be conducted, and anything else (stale, future,
// undated, absent, unreadable, no seam) a WARN that says which and never a PASS.
func TestToolServerProofFinding(t *testing.T) {
	cases := []struct {
		name       string
		st         *verifystate.State
		wantTier   string
		wantStatus string
		wantDetail []string
	}{
		{"fresh pass", recorded("PASS", time.Hour), tierBlock, statusPass, []string{"PASS"}},
		{"fresh fail", recorded("FAIL", time.Hour), tierBlock, statusFail, []string{"did not pass", "FAIL"}},
		{"fresh reject", recorded("REJECT", time.Hour), tierWarn, statusWarn, []string{"could not be conducted"}},
		{"stale pass", recorded("PASS", 72*time.Hour), tierWarn, statusWarn, []string{"stale"}},
		{"future-dated pass", recorded("PASS", -72*time.Hour), tierWarn, statusWarn, []string{"stale"}},
		{"undated", &verifystate.State{Verdict: "PASS", CheckedAt: "yesterday"}, tierWarn, statusWarn, []string{"stale or cannot be dated"}},
		{"nothing recorded", &verifystate.State{}, tierWarn, statusWarn, []string{"no `villa verify tools` result"}},
		{"unreadable", nil, tierWarn, statusWarn, []string{"could not be read"}},
		{"an unknown verdict is not a pass", recorded("MAYBE", time.Hour), tierWarn, statusWarn, []string{"MAYBE"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, ok := findingByID(proofDeps(c.st).aggregate(), "tool-server-proof")
			if !ok {
				t.Fatal("no tool-server-proof finding with the gate on")
			}
			if f.Tier != c.wantTier || f.Status != c.wantStatus {
				t.Errorf("(tier %s, status %s), want (%s, %s); detail %q", f.Tier, f.Status, c.wantTier, c.wantStatus, f.Detail)
			}
			for _, want := range c.wantDetail {
				if !strings.Contains(f.Detail, want) {
					t.Errorf("detail %q does not mention %q", f.Detail, want)
				}
			}
			if f.Status != statusPass && !strings.Contains(f.Remediation, "villa verify tools") {
				t.Errorf("a non-PASS finding must name the verb that re-proves it: %q", f.Remediation)
			}
			if f.Status == statusPass && f.Remediation != "" {
				t.Errorf("a PASS carries a remediation %q", f.Remediation)
			}
		})
	}
}

// TestToolServerProofWithNoSeamIsAWarn: a caller that wired no record reader reads as
// unreadable, never as a pass and never as a panic.
func TestToolServerProofWithNoSeamIsAWarn(t *testing.T) {
	d := toolServerDeps()
	d.ReadToolsVerifyState = nil
	f, ok := findingByID(d.aggregate(), "tool-server-proof")
	if !ok || f.Status != statusWarn {
		t.Errorf("finding = %+v (found=%v), want a WARN", f, ok)
	}
}

// TestFailedToolServerProofFailsDoctor: a fresh failed proof is the worst finding, so doctor's
// overall verdict is FAIL.
func TestFailedToolServerProofFailsDoctor(t *testing.T) {
	if r := proofDeps(recorded("FAIL", time.Hour)).aggregate(); r.Overall != statusFail {
		t.Errorf("Overall = %q, want FAIL", r.Overall)
	}
}
