package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// scopeNamed is a ReadRecallScope that answers a scope naming a collection.
func scopeNamed() (recall.Scope, error) {
	return recall.Scope{SchemaVersion: recall.ScopeSchemaVersion, KnowledgeID: "kb-1", Files: map[string]string{"f": "u"}}, nil
}

// toolServerDeps is a healthy run with the tool server, web search, memory and tools mode
// all on, the recall scope naming a collection, and the status rows the two units add.
func toolServerDeps() *run {
	d := newDoctorDeps()
	d.cfg.ToolServer = true
	d.cfg.WebSearchEnabled = true
	d.cfg.MemoryEnabled = true
	d.cfg.ToolsMode = true
	d.ReadRecallScope = scopeNamed
	return d
}

// TestToolServerFindingIsGatedOnTheGate: with the gate off Aggregate emits no tool-server
// finding and never reads the scope, so an off install differs only by the schema bump.
func TestToolServerFindingIsGatedOnTheGate(t *testing.T) {
	d := newDoctorDeps()
	d.cfg.WebSearchEnabled, d.cfg.MemoryEnabled = true, true
	d.ReadRecallScope = func() (recall.Scope, error) {
		t.Error("the recall scope was read with the tool server off")
		return recall.Scope{}, nil
	}
	if _, ok := findingByID(d.aggregate(), "tool-server"); ok {
		t.Error("a tool-server finding was emitted with the gate off")
	}
}

// TestToolServerFindingPassesWhenEverythingHolds is the PASS: the units can render, tools
// mode is on and the recall scope names a collection.
func TestToolServerFindingPassesWhenEverythingHolds(t *testing.T) {
	f, ok := findingByID(toolServerDeps().aggregate(), "tool-server")
	if !ok {
		t.Fatal("no tool-server finding with the gate on")
	}
	if f.Status != statusPass || f.Remediation != "" {
		t.Errorf("finding = %+v, want a PASS with no remediation", f)
	}
}

// TestToolServerFindingWarns names each condition the ADR lists, and each WARN carries a
// remediation that names the verb to run.
func TestToolServerFindingWarns(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutate   func(*run)
		detail   string
		remedies []string
	}{
		{"nothing to publish", func(d *run) { d.cfg.WebSearchEnabled, d.cfg.MemoryEnabled = false, false },
			"neither web search nor the memory stack", []string{"--tool-server", "memory_enabled"}},
		{"tools mode off", func(d *run) { d.cfg.ToolsMode = false },
			"tools mode is off", []string{"villa tools-mode enter"}},
		{"recall scope empty", func(d *run) {
			d.ReadRecallScope = func() (recall.Scope, error) { return recall.Scope{}, nil }
		}, "names no collection", []string{"villa recall index"}},
		{"recall scope unread", func(d *run) { d.ReadRecallScope = nil },
			"names no collection", []string{"villa recall index"}},
		{"recall scope read error", func(d *run) {
			d.ReadRecallScope = func() (recall.Scope, error) { return recall.Scope{}, errors.New("permission denied") }
		}, "could not be read", []string{"villa up"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := toolServerDeps()
			tc.mutate(d)
			f, ok := findingByID(d.aggregate(), "tool-server")
			if !ok {
				t.Fatal("no tool-server finding")
			}
			if f.Status != statusWarn || f.Tier != tierWarn {
				t.Errorf("status/tier = %s/%s, want a WARN", f.Status, f.Tier)
			}
			if !strings.Contains(f.Detail, tc.detail) {
				t.Errorf("detail = %q, want it to say %q", f.Detail, tc.detail)
			}
			for _, r := range tc.remedies {
				if !strings.Contains(f.Remediation, r) {
					t.Errorf("remediation = %q, want it to name %q", f.Remediation, r)
				}
			}
		})
	}
}

// TestToolServerScopeIsOnlyReadForTheRecallUnit: a web-only tool server has no recall unit,
// so an empty scope is no finding of its own and the scope is not read.
func TestToolServerScopeIsOnlyReadForTheRecallUnit(t *testing.T) {
	d := toolServerDeps()
	d.cfg.MemoryEnabled = false
	d.ReadRecallScope = func() (recall.Scope, error) {
		t.Error("the recall scope was read with no recall unit")
		return recall.Scope{}, nil
	}
	f, _ := findingByID(d.aggregate(), "tool-server")
	if f.Status != statusPass {
		t.Errorf("a web-only tool server = %+v, want a PASS", f)
	}
}
