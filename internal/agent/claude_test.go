package agent

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestParseTarget guards the CLI boundary parse: empty string defaults to Crush,
// the two recognized names round-trip, and anything else is a listed-values error
// so nothing downstream re-validates the target string.
func TestParseTarget(t *testing.T) {
	tests := []struct {
		in      string
		want    Target
		wantErr bool
	}{
		{"", TargetCrush, false},
		{"crush", TargetCrush, false},
		{"claude", TargetClaude, false},
		{"bogus", "", true},
	}
	for _, tc := range tests {
		got, err := ParseTarget(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseTarget(%q) = %q, nil; want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTarget(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("ParseTarget(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// claudeRecorder captures the LaunchClaude call so a test can assert what
// RunClaude drove without a live host.
type claudeRecorder struct {
	cfg         config.VillaConfig
	lookPath    map[string]string
	launchCalls []struct {
		bin  string
		args []string
		env  []string
	}
}

func (r *claudeRecorder) deps() Deps {
	return Deps{
		LoadConfig: func() (config.VillaConfig, error) { return r.cfg, nil },
		LookPath: func(bin string) (string, bool) {
			p, ok := r.lookPath[bin]
			return p, ok
		},
		LaunchClaude: func(bin string, args, env []string) error {
			r.launchCalls = append(r.launchCalls, struct {
				bin  string
				args []string
				env  []string
			}{bin, args, env})
			return nil
		},
	}
}

// TestRunClaudeCodingModeOffRefuses guards the coding-mode gate: RunClaude never
// launches when coding mode is off, since tool use over /v1/messages needs the
// --jinja template only coding mode renders.
func TestRunClaudeCodingModeOffRefuses(t *testing.T) {
	rec := &claudeRecorder{cfg: config.VillaConfig{Model: "qwen3", CodingMode: false}}
	res := RunClaude(rec.deps(), nil)
	if !res.CodingModeOff {
		t.Fatalf("RunClaude = %+v; want CodingModeOff", res)
	}
	if res.ReadyToLaunch {
		t.Errorf("RunClaude ReadyToLaunch = true on coding-mode-off; want false")
	}
	if len(rec.launchCalls) != 0 {
		t.Errorf("LaunchClaude called %d times on coding-mode-off; want 0", len(rec.launchCalls))
	}
	if !strings.Contains(res.Reason, "coding mode is OFF") {
		t.Errorf("Reason %q does not explain the coding-mode gate", res.Reason)
	}
	if !strings.Contains(res.Reason, "villa coding-mode enter") {
		t.Errorf("Reason %q does not point at `villa coding-mode enter`", res.Reason)
	}
}

// TestRunClaudeBinaryAbsent guards the install-hint refusal: claude missing from
// PATH never launches and the reason names how to install it.
func TestRunClaudeBinaryAbsent(t *testing.T) {
	rec := &claudeRecorder{cfg: config.VillaConfig{Model: "qwen3", CodingMode: true}}
	res := RunClaude(rec.deps(), nil)
	if !res.BinaryAbsent {
		t.Fatalf("RunClaude = %+v; want BinaryAbsent", res)
	}
	if res.ReadyToLaunch {
		t.Errorf("RunClaude ReadyToLaunch = true on binary-absent; want false")
	}
	if len(rec.launchCalls) != 0 {
		t.Errorf("LaunchClaude called %d times on binary-absent; want 0", len(rec.launchCalls))
	}
	if !strings.Contains(res.Reason, "claude is not on PATH") {
		t.Errorf("Reason %q does not name the missing binary", res.Reason)
	}
	if !strings.Contains(res.Reason, "https://docs.anthropic.com/en/docs/claude-code") {
		t.Errorf("Reason %q does not point at the install docs", res.Reason)
	}
}

// TestRunClaudeReadyToLaunch guards the happy path: coding mode on, claude on
// PATH, CoderModel wins over Model, extra args forward verbatim, and the launch
// env carries every ANTHROPIC_*/DISABLE_* var appended to the inherited env.
func TestRunClaudeReadyToLaunch(t *testing.T) {
	restore := osEnviron
	osEnviron = func() []string { return []string{"PATH=/usr/bin", "HOME=/home/x"} }
	defer func() { osEnviron = restore }()

	cfg := config.VillaConfig{Model: "qwen3", CoderModel: "qwen3-coder", CodingMode: true}
	rec := &claudeRecorder{cfg: cfg, lookPath: map[string]string{claudeBin: "/usr/local/bin/claude"}}
	extraArgs := []string{"--foo", "bar"}
	res := RunClaude(rec.deps(), extraArgs)

	if !res.ReadyToLaunch {
		t.Fatalf("RunClaude = %+v; want ReadyToLaunch", res)
	}
	if res.LaunchBin != "/usr/local/bin/claude" {
		t.Errorf("LaunchBin = %q, want the fake LookPath result", res.LaunchBin)
	}
	if len(res.LaunchArgs) != len(extraArgs) {
		t.Fatalf("LaunchArgs = %v, want %v", res.LaunchArgs, extraArgs)
	}
	for i, a := range extraArgs {
		if res.LaunchArgs[i] != a {
			t.Errorf("LaunchArgs[%d] = %q, want %q", i, res.LaunchArgs[i], a)
		}
	}

	env := res.LaunchEnv
	if !envHas(env, "PATH=/usr/bin") || !envHas(env, "HOME=/home/x") {
		t.Errorf("LaunchEnv %v does not append to the inherited environment", env)
	}
	want := []string{
		"ANTHROPIC_BASE_URL=http://127.0.0.1:8080",
		"ANTHROPIC_AUTH_TOKEN=" + providerAPIKey,
		"ANTHROPIC_MODEL=qwen3-coder",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=qwen3-coder",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"DISABLE_TELEMETRY=1",
		"DISABLE_ERROR_REPORTING=1",
		"DISABLE_AUTOUPDATER=1",
		envDoNotTrack,
	}
	for _, w := range want {
		if !envHas(env, w) {
			t.Errorf("LaunchEnv %v missing %q", env, w)
		}
	}
}

func envHas(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}
