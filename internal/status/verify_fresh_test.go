package status

import (
	"testing"
	"time"
)

// TestVerifyFresh: a recorded check is fresh only when it parses, is not in the future and is
// within the window. It is the one rule `villa status` and doctor's tool-server-proof share.
func TestVerifyFresh(t *testing.T) {
	at := func(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }
	for _, c := range []struct {
		name string
		in   string
		want bool
	}{
		{"an hour ago", at(-time.Hour), true},
		{"just inside the window", at(-VerifyFreshnessWindow + time.Minute), true},
		{"just outside the window", at(-VerifyFreshnessWindow - time.Minute), false},
		{"in the future", at(time.Hour), false},
		{"undated", "", false},
		{"not a date", "yesterday", false},
	} {
		if got := VerifyFresh(c.in); got != c.want {
			t.Errorf("%s: VerifyFresh(%q) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}
