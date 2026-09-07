// claude.go is the second `villa code` launch target, next to Crush (agent.go):
// it execs the already-installed `claude` binary against the already-served
// inference endpoint. It renders nothing and installs nothing — RunClaude only
// reads config and PATH, then hands back a launch-ready Result. Coding mode is
// the gate (refuse, not warn) because tool use over /v1/messages needs the
// --jinja template that villa renders only in coding mode; without it Claude
// Code's tool calls have nothing to parse against.
//
// Seam discipline (same as agent.go): this file imports NEITHER
// internal/inference NOR internal/detect. The only non-config literal here is
// providerBaseURL (declared in render.go, sanctioned there) — no backend marker,
// no --jinja / sampling flag, no image tag.
package agent

import (
	"fmt"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// Target is which coding agent `villa code` launches.
type Target string

const (
	// TargetCrush is the default, existing launch target (agent.go's Run).
	TargetCrush Target = "crush"
	// TargetClaude is the Claude Code launch target (RunClaude).
	TargetClaude Target = "claude"
)

// claudeBin is the PATH-resolved binary name RunClaude looks up. villa never
// installs it — LookPath is a reference only.
const claudeBin = "claude"

// ParseTarget is the CLI boundary parse for the --agent flag value: nothing
// downstream re-validates the target string. An empty string defaults to
// TargetCrush (the flag's zero value before cobra applies its default).
func ParseTarget(s string) (Target, error) {
	switch Target(s) {
	case "":
		return TargetCrush, nil
	case TargetCrush, TargetClaude:
		return Target(s), nil
	default:
		return "", fmt.Errorf("agent: unknown --agent target %q (want %q or %q)", s, TargetCrush, TargetClaude)
	}
}

// RunClaude is the orchestration for `villa code --agent claude`. Unlike Run
// (Crush), coding mode is a hard gate: a coding-mode-off config refuses rather
// than warns, because Claude Code's tool calls need the --jinja tool-call
// template that villa renders only in coding mode. RunClaude never renders,
// never writes, and never execs itself — it returns a typed Result for the
// caller to print and then launch via the single d.LaunchClaude call.
func RunClaude(d Deps, extraArgs []string) Result {
	var res Result

	cfg, err := d.LoadConfig()
	if err != nil {
		res.Err = fmt.Errorf("agent: load config: %w", err)
		res.Reason = "could not load villa config — fix config.toml and retry"
		return res
	}

	if !subsystem.CodingModeOn(cfg) {
		res.CodingModeOff = true
		res.Reason = "coding mode is OFF — Claude Code needs the coder unit's tool-call template (--jinja), " +
			"which villa renders only in coding mode; run `villa coding-mode enter` first"
		return res
	}

	path, found := d.LookPath(claudeBin)
	if !found {
		res.BinaryAbsent = true
		res.Reason = "claude is not on PATH — install Claude Code " +
			"(https://docs.anthropic.com/en/docs/claude-code) and retry; villa never installs it"
		return res
	}

	res.LaunchBin = path
	res.LaunchArgs = append([]string(nil), extraArgs...)
	res.LaunchEnv = claudeEnv(cfg)
	res.ReadyToLaunch = true
	return res
}

// claudeEnv builds the launch env: osEnviron() plus constant-literal
// ANTHROPIC_*/DISABLE_* appends. The only non-constant value is the served
// model id (servedModelID's catalog-resolved base), never a user-controlled
// string.
func claudeEnv(cfg config.VillaConfig) []string {
	_, base := servedModelID(cfg)
	baseURL := strings.TrimSuffix(providerBaseURL, "/v1")
	return append(osEnviron(),
		"ANTHROPIC_BASE_URL="+baseURL,
		"ANTHROPIC_AUTH_TOKEN="+providerAPIKey,
		"ANTHROPIC_MODEL="+base,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL="+base,
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"DISABLE_TELEMETRY=1",
		"DISABLE_ERROR_REPORTING=1",
		"DISABLE_AUTOUPDATER=1",
		envDoNotTrack,
	)
}
