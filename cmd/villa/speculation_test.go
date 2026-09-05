package main

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
)

// speculation_test.go mirrors backend_test.go: the cobra/exit mapping and the
// dry-run-mutates-nothing property. The transaction itself is asserted in
// internal/backendswap.

// TestSpeculationRegistered: the `speculation` noun and its subcommands are wired
// into the command tree.
func TestSpeculationRegistered(t *testing.T) {
	root := newRoot()
	spec, _, err := root.Find([]string{"speculation"})
	if err != nil || spec.Name() != "speculation" {
		t.Fatalf("`speculation` noun not registered: %v", err)
	}
	for _, sub := range []string{"show", "set"} {
		c, _, err := root.Find([]string{"speculation", sub})
		if err != nil || c.Name() != sub {
			t.Fatalf("`speculation %s` subcommand not registered: %v", sub, err)
		}
	}
}

// TestSpeculationShow: an unset config shows off, and --json emits the one-key shape.
func TestSpeculationShow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cmd, out, _ := newTestCmd()
	if code := runSpeculationShow(cmd, false); code != exitPass {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "off") {
		t.Errorf("an unset config should show off, got %q", out.String())
	}

	cmd, out, _ = newTestCmd()
	if code := runSpeculationShow(cmd, true); code != exitPass {
		t.Fatalf("--json exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), `"speculation": "off"`) {
		t.Errorf("--json shape = %q", out.String())
	}
}

// TestSpeculationSetRejectsUnknownMode asserts the argument is validated against the
// config vocabulary BEFORE any dep is touched, so a typo never reaches the host.
func TestSpeculationSetRejectsUnknownMode(t *testing.T) {
	for _, arg := range []string{"", "draft", "yes"} {
		rec := &backendRecorder{curBackend: "rocm", fits: true, preflightOK: true, proveStatus: prove.StatusPass}
		cmd, _, errOut := newTestCmd()
		if code := runSpeculationSet(cmd, arg, false, newBackendStub(rec)); code != exitBlocked {
			t.Fatalf("arg %q: exit = %d, want 1", arg, code)
		}
		if len(rec.saved) != 0 || rec.written != 0 || len(rec.restarted) != 0 {
			t.Errorf("arg %q: an invalid mode touched the host", arg)
		}
		if !strings.Contains(errOut.String(), "speculation set") {
			t.Errorf("arg %q: refusal = %q", arg, errOut.String())
		}
	}
}

// TestSpeculationSetDryRunMutatesNothing asserts --dry-run previews the target and
// the fit and fires zero mutate seams.
func TestSpeculationSetDryRunMutatesNothing(t *testing.T) {
	rec := &backendRecorder{curBackend: "rocm", fits: true, preflightOK: true, proveStatus: prove.StatusPass}
	cmd, out, _ := newTestCmd()
	if code := runSpeculationSet(cmd, config.SpeculationNgram, true, newBackendStub(rec)); code != exitPass {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "dry-run") || !strings.Contains(out.String(), "ngram") {
		t.Errorf("dry-run output = %q", out.String())
	}
	if len(rec.saved) != 0 || rec.written != 0 || len(rec.restarted) != 0 || rec.captured != 0 {
		t.Errorf("dry-run fired a mutate seam: %+v", rec)
	}
}

// TestSpeculationSetSwitches asserts a proven cutover exits 0 and says what changed.
func TestSpeculationSetSwitches(t *testing.T) {
	rec := &backendRecorder{curBackend: "rocm", fits: true, preflightOK: true, proveStatus: prove.StatusPass}
	cmd, out, _ := newTestCmd()
	if code := runSpeculationSet(cmd, config.SpeculationNgram, false, newBackendStub(rec)); code != exitPass {
		t.Fatalf("exit = %d, want 0", code)
	}
	if len(rec.saved) != 1 || rec.saved[0].Speculation != config.SpeculationNgram {
		t.Errorf("persisted %+v, want speculation ngram", rec.saved)
	}
	if !strings.Contains(out.String(), "ngram") {
		t.Errorf("output = %q", out.String())
	}
}

// TestSpeculationSetRefusalExitsBlocked asserts an unqualified target, carried by the
// fit guard, exits 1 with the resolver's note.
func TestSpeculationSetRefusalExitsBlocked(t *testing.T) {
	rec := &backendRecorder{
		curBackend: "rocm", fits: false,
		fitReason:   "speculation: ngram requested but m is not qualified for it; refusing",
		preflightOK: true, proveStatus: prove.StatusPass,
	}
	cmd, _, errOut := newTestCmd()
	if code := runSpeculationSet(cmd, config.SpeculationNgram, false, newBackendStub(rec)); code != exitBlocked {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "not qualified") {
		t.Errorf("refusal = %q", errOut.String())
	}
	if len(rec.saved) != 0 {
		t.Errorf("a refusal persisted %+v", rec.saved)
	}
}
