package backendswap

import (
	"testing"
)

// tools_test.go covers RunTools' own decisions: the no-op is decided on the
// ANSWERED gate, an exit under coding mode is a refusal, the fit guard sees the
// TARGET state, and only the flag is written.

// TestRunToolsAnswersTheGateNotTheFlag: coding mode implies tools mode, so entering
// on a coding-mode stack is already there.
func TestRunToolsAnswersTheGateNotTheFlag(t *testing.T) {
	cfg := vulkan()
	cfg.CodingMode = true
	f := newFake(cfg)
	res := RunTools(f.deps(), true)
	if !res.NoOp || res.From != "on" || f.captured {
		t.Fatalf("expected a NoOp on the answered gate, got %+v", res)
	}
}

// TestRunToolsExitUnderCodingModeRefuses: a persisted false would render an
// unchanged unit, so the exit refuses and names the flag that holds the gate on.
func TestRunToolsExitUnderCodingModeRefuses(t *testing.T) {
	cfg := vulkan()
	cfg.CodingMode, cfg.ToolsMode = true, true
	f := newFake(cfg)
	res := RunTools(f.deps(), false)
	if !res.Refused || res.Reason == "" || f.captured {
		t.Fatalf("expected a refusal naming coding mode before capture, got %+v", res)
	}
}

// TestRunToolsFitGuardSeesTheTargetState: the ctx-floor guard sees tools mode on,
// and a floor that does not fit refuses before anything is captured.
func TestRunToolsFitGuardSeesTheTargetState(t *testing.T) {
	f := newFake(vulkan())
	f.fitOK, f.fitReason = false, "tools mode serves ctx 65536: needs more"
	res := RunTools(f.deps(), true)
	if !res.Refused || res.Reason != f.fitReason || f.captured {
		t.Fatalf("expected the ctx-floor refusal before capture, got %+v", res)
	}
	if !f.fitSaw.ToolsMode {
		t.Errorf("the fit guard must see the TARGET state (tools mode on)")
	}
}

// TestRunToolsPersistsTheFlag: enter writes true, exit writes false, nothing else.
func TestRunToolsPersistsTheFlag(t *testing.T) {
	for _, on := range []bool{true, false} {
		cfg := vulkan()
		cfg.ToolsMode = !on
		f := newFake(cfg)
		res := RunTools(f.deps(), on)
		if !res.Switched || res.To != ToolsLabel(on) || res.From != ToolsLabel(!on) {
			t.Fatalf("on=%v: expected Switched, got %+v", on, res)
		}
		want := cfg
		want.ToolsMode = on
		if len(f.saved) != 1 || f.saved[0].ToolsMode != on || f.saved[0].Model != want.Model || f.saved[0].Backend != want.Backend {
			t.Errorf("on=%v: saved %+v, want only the flag changed", on, f.saved)
		}
	}
}

// TestToolsLabel is the Result vocabulary the verb prints.
func TestToolsLabel(t *testing.T) {
	if ToolsLabel(true) != "on" || ToolsLabel(false) != "off" {
		t.Errorf("ToolsLabel = %q/%q", ToolsLabel(true), ToolsLabel(false))
	}
}
