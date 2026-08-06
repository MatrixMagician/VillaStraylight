package agent

import (
	"encoding/json"
	"testing"
)

// render_test.go holds the Phase-27 STRIDE-pass assertions over agent.Render: the
// restrictive tool config that Phase 26 deliberately deferred (26-01 STATE note:
// "permissions rendered omitted (Phase-27 STRIDE owns the restrictive allowlist)").
// The keys are PINNED against the v0.76.0 frozen schema (26-RESEARCH:158/169/192-193):
//   - permissions.allowed_tools (TOP-LEVEL permissions block) = ["view","edit","write"]
//   - options.disabled_tools   (under the options block)      = ["fetch","agentic_fetch","download","sourcegraph"]
// disabling the outbound tools does NOT harm the readiness/verify loop, which needs
// only view/edit/write (27-RESEARCH A3 / Pitfall 5). An omitted allowed_tools makes
// Crush prompt (blocks readiness); an omitted disabled_tools leaves outbound tools on
// (the STRIDE FAIL) — so both are asserted present, never optional.

// wantAllowedTools is the pinned restrictive allowlist (the readiness loop's needs).
var wantAllowedTools = []string{"view", "edit", "write"}

// wantDisabledTools is the pinned outbound-tool denylist (defense-in-depth).
var wantDisabledTools = []string{"fetch", "agentic_fetch", "download", "sourcegraph"}

// TestRenderRestrictiveTools asserts the Phase-27 security pass: the rendered crush.json
// carries permissions.allowed_tools (top-level) == view/edit/write AND options.disabled_tools
// (under options) == fetch/agentic_fetch/download/sourcegraph. Both placements are decoded
// from the rendered bytes and checked exactly (STRIDE mitigation).
func TestRenderRestrictiveTools(t *testing.T) {
	got, _, err := Render(renderTestConfig(), renderTestProbes())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var parsed struct {
		Options struct {
			DisabledTools []string `json:"disabled_tools"`
		} `json:"options"`
		Permissions struct {
			AllowedTools []string `json:"allowed_tools"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("rendered crush.json does not parse: %v", err)
	}

	if !equalStrings(parsed.Permissions.AllowedTools, wantAllowedTools) {
		t.Errorf("permissions.allowed_tools = %v, want %v (top-level permissions block, readiness needs view/edit/write)",
			parsed.Permissions.AllowedTools, wantAllowedTools)
	}
	if !equalStrings(parsed.Options.DisabledTools, wantDisabledTools) {
		t.Errorf("options.disabled_tools = %v, want %v (outbound tools off by construction)",
			parsed.Options.DisabledTools, wantDisabledTools)
	}
}

// equalStrings reports whether two string slices are equal in order and content.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
