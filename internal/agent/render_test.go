package agent

import (
	"encoding/json"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
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

// wantSandboxDisabledTools is the workspace-task denylist (spec 3.3): the four
// the coding-agent render already disables, plus the two web tools. The internal
// network is the boundary; this render is the courtesy.
var wantSandboxDisabledTools = []string{
	"fetch", "agentic_fetch", "download", "sourcegraph", "web_fetch", "web_search",
}

// TestRenderSandboxTargetsTheInternalNetworkAndDisablesTheWebTools guards the
// three things that make a workspace task's crush.json different from the coding
// agent's: the provider points at the sandbox-network address the caller
// resolved, the six web-class tools are off, and auto_lsp is off with no lsp
// block (the office image ships no language servers, and probing for them inside
// a microVM would only slow the start).
func TestRenderSandboxTargetsTheInternalNetworkAndDisablesTheWebTools(t *testing.T) {
	const baseURL = "http://villa-llama:8080/v1"

	got, err := RenderSandbox(config.VillaConfig{Model: "qwen3.6-35b-a3b", Ctx: 131072}, baseURL)
	if err != nil {
		t.Fatalf("RenderSandbox: %v", err)
	}

	var parsed struct {
		Options struct {
			DisabledTools []string `json:"disabled_tools"`
			AutoLSP       bool     `json:"auto_lsp"`
		} `json:"options"`
		Providers map[string]struct {
			BaseURL string `json:"base_url"`
			Models  []struct {
				ID            string `json:"id"`
				ContextWindow int    `json:"context_window"`
			} `json:"models"`
		} `json:"providers"`
		Models map[string]struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"models"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("rendered crush.json does not parse: %v", err)
	}

	if !equalStrings(parsed.Options.DisabledTools, wantSandboxDisabledTools) {
		t.Errorf("options.disabled_tools = %v, want %v", parsed.Options.DisabledTools, wantSandboxDisabledTools)
	}
	if parsed.Options.AutoLSP {
		t.Error("options.auto_lsp = true, want false")
	}
	if parsed.Providers["villa"].BaseURL != baseURL {
		t.Errorf("provider base_url = %q, want %q", parsed.Providers["villa"].BaseURL, baseURL)
	}
	if len(parsed.Providers["villa"].Models) != 1 {
		t.Fatalf("provider models = %v, want exactly one", parsed.Providers["villa"].Models)
	}
	if got := parsed.Providers["villa"].Models[0].ContextWindow; got != 131072 {
		t.Errorf("context_window = %d, want the config's 131072", got)
	}
	id := parsed.Providers["villa"].Models[0].ID
	for _, k := range []string{"large", "small"} {
		if parsed.Models[k].Provider != "villa" || parsed.Models[k].Model != id {
			t.Errorf("models.%s = %+v, want the villa provider's %q", k, parsed.Models[k], id)
		}
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(got, &raw); err != nil {
		t.Fatalf("rendered crush.json does not parse: %v", err)
	}
	if _, ok := raw["lsp"]; ok {
		t.Error("rendered an lsp section; the office image ships no language servers")
	}
	// A permissions.allowed_tools block would auto-approve the tool calls villa's
	// approval table exists to gate, so the workspace-task render must not carry one.
	if _, ok := raw["permissions"]; ok {
		t.Error("rendered a permissions block; every tool call must reach villa as a permission request")
	}
}

// TestRenderSandboxRefusesAnEmptyBaseURL guards fail-closed: a blank base_url
// renders a crush.json whose provider silently resolves nowhere.
func TestRenderSandboxRefusesAnEmptyBaseURL(t *testing.T) {
	if _, err := RenderSandbox(config.VillaConfig{Model: "m"}, ""); err == nil {
		t.Fatal("an empty base URL should refuse")
	}
}

// TestRenderSandboxIsDeterministic guards the same property Render has: same
// input, byte-identical output.
func TestRenderSandboxIsDeterministic(t *testing.T) {
	cfg := config.VillaConfig{Model: "m", Ctx: 4096}
	a, err := RenderSandbox(cfg, "http://h:8080/v1")
	if err != nil {
		t.Fatalf("RenderSandbox: %v", err)
	}
	b, err := RenderSandbox(cfg, "http://h:8080/v1")
	if err != nil {
		t.Fatalf("RenderSandbox: %v", err)
	}
	if string(a) != string(b) {
		t.Error("two renders of the same config differ")
	}
}
