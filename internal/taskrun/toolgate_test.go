package taskrun

import (
	"context"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/approval"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
)

// TestSubmitRefusesAModelThatCannotMakeToolCalls guards the turn contract's
// "executable" side: a served model whose template confidently cannot make tool
// calls is refused before any sandbox starts, with the same remediation CAP-02
// prints.
func TestSubmitRefusesAModelThatCannotMakeToolCalls(t *testing.T) {
	h := newHarness(t)
	h.deps.ServedProps = func(context.Context) *inference.PropsInfo {
		return &inference.PropsInfo{ToolsSupported: detect.Bool{Known: true, Value: false}}
	}
	h.start()
	task := h.submit(approval.ModeAsk)
	got := h.waitState(task.ID, taskstore.Refused)
	log := h.logText(task.ID)
	for _, want := range []string{inference.ToolsMissing, inference.ToolsFix} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if got.StartedAt != "" {
		t.Errorf("a refused task must never have started, got started_at %q", got.StartedAt)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.launches != 0 {
		t.Errorf("sandbox launches = %d, want 0", h.launches)
	}
}

// TestSubmitAcceptsAnUnknownToolVerdictWithAWarning guards #403's rule at the
// runner: missing /props evidence is never "unsupported", so the task is
// queued, and the log says the capability was not confirmed.
func TestSubmitAcceptsAnUnknownToolVerdictWithAWarning(t *testing.T) {
	for name, props := range map[string]*inference.PropsInfo{
		"unreadable":   nil,
		"field absent": {},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.deps.ServedProps = func(context.Context) *inference.PropsInfo { return props }
			h.start()
			task := h.submit(approval.ModeAsk)
			if task.State == taskstore.Refused {
				t.Fatalf("an unknown verdict must not refuse: %+v", task)
			}
			if log := h.logText(task.ID); !strings.Contains(log, "warning:") {
				t.Errorf("log lacks the warning:\n%s", log)
			}
		})
	}
}

// TestSubmitAcceptsAToolCapableModelSilently guards that a confirmed
// tool-capable model adds no warning to the task log.
func TestSubmitAcceptsAToolCapableModelSilently(t *testing.T) {
	h := newHarness(t)
	h.start()
	task := h.submit(approval.ModeAsk)
	if task.State == taskstore.Refused {
		t.Fatalf("a tool-capable model must not refuse: %+v", task)
	}
	if log := h.logText(task.ID); strings.Contains(log, "warning:") {
		t.Errorf("unexpected warning:\n%s", log)
	}
}
