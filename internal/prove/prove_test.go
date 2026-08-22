package prove

import "testing"

// TestPassOnlyOnStatusPass holds the one invariant this type carries: a cutover
// succeeds ONLY on StatusPass. Every other status — including an empty verdict and
// the ready-but-residency-FAIL case the three cores were built to refuse — is a
// non-pass that must roll back.
func TestPassOnlyOnStatusPass(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{StatusPass, true},
		{StatusFail, false},
		{"", false},
		{"warn", false},
		{"unknown", false},
		{"PASS", false}, // case-sensitive: the sentinel is lowercase by construction
	}
	for _, tc := range cases {
		if got := (Verdict{Status: tc.status}).Pass(); got != tc.want {
			t.Errorf("Verdict{Status: %q}.Pass() = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// TestStatusSentinels pins the wire values. The three cores previously each
// declared their own copy of these constants and the restore adapter compared
// across packages, so the values are load-bearing beyond this package.
func TestStatusSentinels(t *testing.T) {
	if StatusPass != "pass" {
		t.Errorf("StatusPass = %q, want %q", StatusPass, "pass")
	}
	if StatusFail != "fail" {
		t.Errorf("StatusFail = %q, want %q", StatusFail, "fail")
	}
}
