package agent

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// render.go is the PURE, DETERMINISTIC crush.json renderer (AGENT-02). crush.json
// is a DERIVED artifact of config.toml — regenerated, never hand-edited as the
// authority (config-as-source-of-truth; drift is flagged in drift.go, never
// auto-corrected). It is emitted via stdlib encoding/json with a FIXED
// indent + trailing newline so byte-output is stable (Pitfall 4 — config-drift
// compare depends on render determinism).
//
// Seam discipline: the ONLY non-config literals here are the loopback inference
// URL and the villa- model-id prefix — NEITHER is a backend marker (no image
// tags, no Vulkan0/ROCm0, no device args, no --jinja / sampling flags), so
// TestSeamGrepGate stays green. `villa code` launches against the ALREADY-served
// endpoint; this renderer does NOT render inference units.

const (
	// crushSchema is the verified v0.76.0 schema URL (frozen, 26-RESEARCH.md).
	crushSchema = "https://charm.land/crush.json"
	// providerKey is the single villa provider's object key.
	providerKey = "villa"
	// providerBaseURL is the FIXED loopback inference endpoint (Open-Q1 option ii):
	// the inference port is the constant serverPort=8080 (internal/inference), NOT a
	// config field. Loopback-only, consistent with. Not a backend marker.
	// Drift-guarded against inference.ServerPort() by
	// cmd/villa.TestCrushProviderPortMatchesInferenceServerPort (the agent seam forbids
	// importing inference here, so the coupling is asserted from the cmd/villa test tier).
	providerBaseURL = "http://127.0.0.1:8080/v1"
	// providerAPIKey is a dummy non-secret key (the existing inference probe uses
	// "local"); MUST be metachar-free (Pitfall 1).
	providerAPIKey = "local"
	// modelIDPrefix namespaces the rendered model id so it cannot collide with a
	// Catwalk built-in id (#2649 shadowing fix).
	modelIDPrefix = "villa-"
	// defaultContextWindow is the rendered context_window when cfg.CoderAgentCtx is
	// unset (a sane local default).
	defaultContextWindow = 32768
	// maxTokensDivisor derives default_max_tokens as a fraction of the context
	// window (a sane, deterministic ratio).
	maxTokensDivisor = 4
)

// allowedTools is the Phase-27 STRIDE restrictive allowlist rendered into
// permissions.allowed_tools (TOP-LEVEL permissions block, 26-RESEARCH:192-193): the
// three tools the readiness/verify `crush run` round-trip needs (view→edit→result), so
// the agent auto-accepts them without an interactive prompt and outbound tools stay off.
// PINNED against the v0.76.0 frozen schema (do NOT re-confirm at runtime).
var allowedTools = []string{"view", "edit", "write"}

// disabledTools is the Phase-27 STRIDE outbound-tool denylist rendered into
// options.disabled_tools (under the OPTIONS block, 26-RESEARCH:158/169): the agent's
// outbound tools are off by construction (defense-in-depth for).
// Disabling these does NOT harm the readiness loop, which needs only view/edit/write.
var disabledTools = []string{"fetch", "agentic_fetch", "download", "sourcegraph"}

// LSPProbe is a resolved host-PATH probe for one LSP server, produced by the live
// Deps.LookPath seam and consumed PURELY here. Found drives whether the lsp entry
// is rendered; a not-found probe yields a WARN and an omitted entry.
type LSPProbe struct {
	// Key is the lsp object key (the language/server name, e.g. "go").
	Key string
	// Command is the fixed-literal server command rendered into the entry (e.g.
	// "gopls"). MUST be metachar-free (Pitfall 1) — it is a known binary name.
	Command string
	// Found reports whether the server was present on PATH.
	Found bool
}

// crushConfig is the top-level rendered shape. Ordered struct fields give a stable
// field order (Pitfall 4). providers and lsp are maps in the schema; Go's
// encoding/json emits map keys sorted, and the golden pins the result.
type crushConfig struct {
	Schema    string                   `json:"$schema"`
	Options   crushOptions             `json:"options"`
	Providers map[string]crushProvider `json:"providers"`
	LSP       map[string]crushLSPEntry `json:"lsp,omitempty"`
	// Permissions is rendered unconditionally from Phase 27 (the STRIDE pass): a
	// non-nil block carrying the restrictive allowed_tools so the readiness/verify
	// `crush run` completes without an interactive prompt (27-RESEARCH A3). It is no
	// longer omitempty — an omitted allowed_tools makes Crush prompt and blocks readiness.
	Permissions *crushPermissions `json:"permissions,omitempty"`
}

// crushOptions carries both kill switches plus the cloud-fallback /
// determinism strengtheners (Pitfall 2/5) and the Phase-27 STRIDE outbound-tool
// denylist (DisabledTools). disabled_tools is an OPTIONS field in the v0.76.0 frozen
// schema (26-RESEARCH:158/169) — NOT a top-level key.
type crushOptions struct {
	DisableMetrics            bool `json:"disable_metrics"`
	DisableProviderAutoUpdate bool `json:"disable_provider_auto_update"`
	DisableDefaultProviders   bool `json:"disable_default_providers"`
	AutoLSP                   bool `json:"auto_lsp"`
	// DisabledTools turns the agent's outbound tools OFF by construction (Phase-27
	// STRIDE /, defense-in-depth for): fetch / agentic_fetch / download
	// / sourcegraph. Rendered unconditionally — an omitted denylist leaves outbound
	// tools on (the STRIDE FAIL), so it is NEVER omitempty.
	DisabledTools []string `json:"disabled_tools"`
}

// crushProvider is the single villa openai-compat provider block.
type crushProvider struct {
	Name    string       `json:"name"`
	Type    string       `json:"type"`
	BaseURL string       `json:"base_url"`
	APIKey  string       `json:"api_key"`
	Models  []crushModel `json:"models"`
}

// crushModel mirrors the v0.76.0 Model required-field set. Cost fields are 0
// (local, no cost); id is villa- prefixed (#2649-safe).
type crushModel struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	CostPer1MIn         float64 `json:"cost_per_1m_in"`
	CostPer1MOut        float64 `json:"cost_per_1m_out"`
	CostPer1MInCached   float64 `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached  float64 `json:"cost_per_1m_out_cached"`
	ContextWindow       int     `json:"context_window"`
	DefaultMaxTokens    int     `json:"default_max_tokens"`
	CanReason           bool    `json:"can_reason"`
	SupportsAttachments bool    `json:"supports_attachments"`
}

// crushLSPEntry is a single detected-toolchain LSP entry. Only Command is
// rendered (a fixed literal); other fields are omitted (Crush defaults apply).
type crushLSPEntry struct {
	Command string `json:"command"`
}

// crushPermissions carries the Phase-27 STRIDE restrictive allowlist (Open-Q3): from
// Phase 27 it is rendered with allowed_tools = view/edit/write (the readiness loop's
// needs). allowed_tools is no longer omitempty — it renders unconditionally so the
// agent runs auto-accepting ONLY those three tools and never prompts (27-RESEARCH A3).
type crushPermissions struct {
	AllowedTools []string `json:"allowed_tools"`
}

// servedModelID derives the served model id from config (the source of truth,
// cfg.CoderModel when coding-mode coder is set, else cfg.Model. The
// rendered crush.json model id is this value, villa- prefixed, so it (a) cannot
// collide with a Catwalk built-in (#2649) and (b) tracks the model the unit
// advertises (llama.cpp's single-model leniency makes the request model id
// immaterial — Open-Q1 option ii). An empty base id still yields a non-empty,
// prefixed id so models[] is never empty (Pitfall 3).
func servedModelID(cfg config.VillaConfig) (id, base string) {
	base = cfg.Model
	if cfg.CoderModel != "" {
		base = cfg.CoderModel
	}
	return modelIDPrefix + base, base
}

// Render produces the DETERMINISTIC crush.json bytes from config plus the
// non-blocking LSP warnings. It is pure: same (cfg, probes) → byte-
// identical output (Pitfall 4). It never returns a blocking error for a missing
// LSP server — a missing server is a WARN, the entry is omitted.
func Render(cfg config.VillaConfig, probes []LSPProbe) ([]byte, []Warning, error) {
	id, base := servedModelID(cfg)

	ctxWindow := cfg.CoderAgentCtx
	if ctxWindow <= 0 {
		ctxWindow = defaultContextWindow
	}

	model := crushModel{
		ID:                  id,
		Name:                base + " (local)",
		ContextWindow:       ctxWindow,
		DefaultMaxTokens:    ctxWindow / maxTokensDivisor,
		CanReason:           false,
		SupportsAttachments: false,
		// cost_* default to 0 (local, no cost).
	}

	lsp, warns := renderLSP(probes)

	cfgOut := crushConfig{
		Schema: crushSchema,
		Options: crushOptions{
			DisableMetrics:            true,          // D-07
			DisableProviderAutoUpdate: true,          // D-04/D-07
			DisableDefaultProviders:   true,          // Pitfall 2 — only the villa provider is usable
			AutoLSP:                   false,         // Pitfall 5 — the lsp block is authoritative
			DisabledTools:             disabledTools, // Phase-27 STRIDE — outbound tools off
		},
		Providers: map[string]crushProvider{
			providerKey: {
				Name:    "VillaStraylight (local)",
				Type:    "openai-compat", // D-08
				BaseURL: providerBaseURL, // loopback literal, not a backend marker
				APIKey:  providerAPIKey,
				Models:  []crushModel{model}, // non-empty (Pitfall 3)
			},
		},
		LSP: lsp,
		// Phase-27 STRIDE pass (Open-Q3): render the restrictive allowlist so the
		// readiness/verify `crush run` auto-accepts view/edit/write without a prompt.
		Permissions: &crushPermissions{AllowedTools: allowedTools},
	}

	b, err := json.MarshalIndent(cfgOut, "", "  ")
	if err != nil {
		return nil, warns, fmt.Errorf("agent: marshal crush.json: %w", err)
	}
	// FIXED trailing newline so the byte-output (and golden) is stable.
	out := append(b, '\n')
	return out, warns, nil
}

// renderLSP emits an lsp entry for each FOUND probe (a fixed-literal command, no
// $() — Pitfall 1) and a WARN-with-remediation for each not-found probe, OMITTING
// the entry (typed-Unknown degradation, never a BLOCK). Returns a nil map
// when no server is found so the omitempty `lsp` key disappears (still
// deterministic).
func renderLSP(probes []LSPProbe) (map[string]crushLSPEntry, []Warning) {
	var out map[string]crushLSPEntry
	var warns []Warning
	for _, pr := range probes {
		if pr.Found {
			if out == nil {
				out = map[string]crushLSPEntry{}
			}
			out[pr.Key] = crushLSPEntry{Command: pr.Command}
			continue
		}
		warns = append(warns, Warning{
			Code: "lsp_missing",
			Msg: fmt.Sprintf(
				"%s not found on PATH — code navigation degraded; install it to enable LSP (e.g. `go install golang.org/x/tools/gopls@latest` for gopls)",
				pr.Command,
			),
		})
	}
	return out, warns
}

// canonicalize re-marshals rendered crush.json bytes into a stable, semantic form
// for the config-drift compare (Pitfall 4): it unmarshals into a generic value and
// re-marshals so whitespace-only differences normalize away while semantic edits
// survive. Returns the input unchanged on a parse error (a non-JSON on-disk file
// is, semantically, a drift the byte path will catch). Used by drift.go.
func canonicalize(b []byte) []byte {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return bytes.TrimSpace(b)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return bytes.TrimSpace(b)
	}
	return out
}
