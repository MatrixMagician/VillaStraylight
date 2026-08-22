package verify

import (
	"strings"
	"testing"
)

// verify_test.go covers the honesty rules the family exists to enforce. They used to
// be re-implemented per verb in the command tier, so they could only be checked one
// verb at a time and could drift apart.

// proof builds a Subject that is enabled and returns the given proof.
func proof(p Proof) Subject {
	return Subject{
		Name:        "verify thing",
		Enabled:     func() bool { return true },
		FailLabel:   "the proof",
		RejectLabel: "the proof",
		Prove:       func() Proof { return p },
	}
}

// TestRejectIsNeitherPassNorFail is the negative-control-first rule, and the reason
// the family has three outcomes rather than two.
//
// An egress block that cannot be shown to be effective is rejected. Collapsing that
// into pass would report a bound that was never demonstrated, which is the exact
// false-green the proof exists to prevent. Collapsing it into fail would invent a
// defect from a measurement that never happened.
func TestRejectIsNeitherPassNorFail(t *testing.T) {
	got := Run(proof(Proof{Status: Reject, Detail: "the canary was already unreachable unguarded"}))

	if got.Status == Pass {
		t.Error("a proof that could not be conducted must NEVER read as a pass — that is the false-green")
	}
	if got.Status == Fail {
		t.Error("a proof that could not be conducted must not be reported as a confident failure")
	}
	if got.Status != Reject {
		t.Fatalf("Status = %v, want Reject", got.Status)
	}
	if !strings.Contains(got.Message, "could not be conducted") {
		t.Errorf("the message must say the proof did not run, got %q", got.Message)
	}
	if !strings.Contains(got.Message, "already unreachable unguarded") {
		t.Errorf("the message must carry the detail explaining why, got %q", got.Message)
	}
	if !got.ToStderr {
		t.Error("a non-pass must go to stderr, or a caller piping stdout loses it")
	}
}

// TestRejectWarnsRatherThanBlocking: nothing was disproven, so the operator is told
// the proof could not run rather than that the property is broken. Blocking on it
// would make an unrunnable probe indistinguishable from a real defect.
func TestRejectWarnsRatherThanBlocking(t *testing.T) {
	const (
		pass    = 0
		blocked = 1
		warn    = 2
	)
	cases := []struct {
		status Status
		want   int
		why    string
	}{
		{Pass, pass, "a proven property exits clean"},
		{Fail, blocked, "a confident negative blocks"},
		{Reject, warn, "an unrunnable proof warns: nothing was disproven"},
		{Skip, pass, "an off subsystem is not a failure"},
	}
	for _, tc := range cases {
		t.Run(tc.status.String(), func(t *testing.T) {
			if got := ExitCode(tc.status, pass, blocked, warn); got != tc.want {
				t.Errorf("ExitCode(%v) = %d, want %d — %s", tc.status, got, tc.want, tc.why)
			}
		})
	}
}

// TestDisabledSubsystemIsNotAFailure: there is nothing to verify, so the verb says so
// and exits clean. Reporting a failure would train the operator to ignore the verb;
// reporting a pass would claim something was proven that never ran.
func TestDisabledSubsystemIsNotAFailure(t *testing.T) {
	proved := false
	got := Run(Subject{
		Name:            "verify thing",
		Enabled:         func() bool { return false },
		DisabledMessage: "not enabled — enable it, then re-run.",
		Prove:           func() Proof { proved = true; return Proof{Status: Pass} },
	})

	if proved {
		t.Error("the proof must NOT run for a disabled subsystem")
	}
	if got.Status != Skip {
		t.Errorf("Status = %v, want Skip", got.Status)
	}
	if got.ToStderr {
		t.Error("a skip is not an error and belongs on stdout")
	}
	if !strings.Contains(got.Message, "enable it") {
		t.Errorf("a skip must tell the operator how to enable the subsystem, got %q", got.Message)
	}
}

// TestFailCarriesTheRemediation: a refusal has to be actionable. The detail is what
// makes it more than a bare negative.
func TestFailCarriesTheRemediation(t *testing.T) {
	got := Run(proof(Proof{Status: Fail, Detail: "egress is NOT blocked — block outbound, then re-run"}))

	if got.Status != Fail {
		t.Fatalf("Status = %v, want Fail", got.Status)
	}
	if !strings.Contains(got.Message, "block outbound, then re-run") {
		t.Errorf("a failure must carry its remediation, got %q", got.Message)
	}
	if !got.ToStderr {
		t.Error("a failure must go to stderr")
	}
}

// TestVerbOwnsItsWording: each verb proves a different property, so the
// operator-facing label is the verb's. The core owns WHICH outcomes exist and how
// they map, not what to call them.
func TestVerbOwnsItsWording(t *testing.T) {
	s := proof(Proof{Status: Fail, Detail: "d"})
	s.Name = "verify memory"
	s.FailLabel = "runtime zero-outbound RAG proof"

	got := Run(s)
	if !strings.HasPrefix(got.Message, "verify memory: runtime zero-outbound RAG proof FAILED: d") {
		t.Errorf("message = %q, want the verb's own wording", got.Message)
	}
}

// TestPassReadsCleanly: a proven property prints its detail on stdout with no
// failure framing.
func TestPassReadsCleanly(t *testing.T) {
	got := Run(proof(Proof{Status: Pass, Detail: "real format=json query returned 27 result(s)"}))

	if got.Status != Pass {
		t.Fatalf("Status = %v, want Pass", got.Status)
	}
	if got.ToStderr {
		t.Error("a pass belongs on stdout")
	}
	if strings.Contains(got.Message, "FAILED") || strings.Contains(got.Message, "REJECT") {
		t.Errorf("a pass must not carry failure framing, got %q", got.Message)
	}
}

// TestNilEnabledMeansAlwaysVerify: a verb with no gate is always on, and must not
// silently skip.
func TestNilEnabledMeansAlwaysVerify(t *testing.T) {
	s := proof(Proof{Status: Pass, Detail: "ok"})
	s.Enabled = nil

	if got := Run(s); got.Status != Pass {
		t.Errorf("Status = %v, want Pass — an ungated verb must run its proof", got.Status)
	}
}
