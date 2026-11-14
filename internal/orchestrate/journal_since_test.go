package orchestrate

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// TestJournalSinceReadsOneUnitFromAnInstant: the read is journalctl scoped to one user unit
// and a lower time bound, message text only, so a tool unit's request lines are returned
// without the prefix journald adds and without anything older than the proof window.
func TestJournalSinceReadsOneUnitFromAnInstant(t *testing.T) {
	var gotName string
	var gotArgs []string
	s := Systemd{runCmd: func(name string, args ...string) (string, bool, bool) {
		gotName, gotArgs = name, args
		return "tools: tool=villa_web_search status=200\n", true, true
	}}
	out, err := s.JournalSince("villa-tools-web.service", time.Unix(1700000123, 987))
	if err != nil || out != "tools: tool=villa_web_search status=200\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	want := []string{"--user", "-u", "villa-tools-web.service", "--since", "@1700000123", "-o", "cat", "--no-pager"}
	if gotName != "journalctl" || !slices.Equal(gotArgs, want) {
		t.Errorf("ran %s %v, want journalctl %v", gotName, gotArgs, want)
	}
}

// TestJournalSinceDistinguishesEmptyFromUnreadable: no line in the window is an answer. A
// missing journalctl, or one that fails, is an error, because the caller must not read a
// failed read as "the unit logged nothing".
func TestJournalSinceDistinguishesEmptyFromUnreadable(t *testing.T) {
	empty := Systemd{runCmd: func(string, ...string) (string, bool, bool) { return "", true, true }}
	if out, err := empty.JournalSince("u.service", time.Now()); err != nil || out != "" {
		t.Errorf("an empty window: out=%q err=%v", out, err)
	}
	missing := Systemd{runCmd: func(string, ...string) (string, bool, bool) { return "", false, false }}
	_, err := missing.JournalSince("u.service", time.Now())
	var nf ErrToolNotFound
	if !errors.As(err, &nf) {
		t.Errorf("a missing journalctl must be ErrToolNotFound, got %v", err)
	}
	failed := Systemd{runCmd: func(string, ...string) (string, bool, bool) { return "partial", true, false }}
	if _, err := failed.JournalSince("u.service", time.Now()); err == nil {
		t.Error("a journalctl that exits non-zero is an error")
	}
}
