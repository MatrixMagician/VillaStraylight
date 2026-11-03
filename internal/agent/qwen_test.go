package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

const qwenTestSecret = "test-inference-secret"

// qwenRecorder captures the QwenHome / WriteQwenSettings / LaunchClaude calls so a
// test can assert what RunQwen drove without a live host.
type qwenRecorder struct {
	cfg      config.VillaConfig
	lookPath map[string]string
	home     string
	writeErr error

	writes []struct {
		home string
		b    []byte
	}
	launches int
}

func (r *qwenRecorder) deps() Deps {
	return Deps{
		LoadConfig: func() (config.VillaConfig, error) { return r.cfg, nil },
		LookPath: func(bin string) (string, bool) {
			p, ok := r.lookPath[bin]
			return p, ok
		},
		QwenHome: func() (string, error) { return r.home, nil },
		WriteQwenSettings: func(home string, b []byte) error {
			r.writes = append(r.writes, struct {
				home string
				b    []byte
			}{home, b})
			return r.writeErr
		},
		LaunchClaude: func(string, []string, []string) error { r.launches++; return nil },
	}
}

// TestParseTargetQwen guards the CLI boundary parse for the third target: qwen
// round-trips, and the unknown-target error lists all three values.
func TestParseTargetQwen(t *testing.T) {
	got, err := ParseTarget("qwen")
	if err != nil || got != TargetQwen {
		t.Fatalf("ParseTarget(qwen) = %q, %v; want TargetQwen", got, err)
	}
	_, err = ParseTarget("junk")
	if err == nil {
		t.Fatal("ParseTarget(junk) = nil error; want a listed-values error")
	}
	for _, want := range []string{"crush", "claude", "qwen"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not list %q", err, want)
		}
	}
}

// TestRunQwenCodingModeOffRefuses guards the coding-mode gate: nothing is written
// and nothing launches when coding mode is off.
func TestRunQwenCodingModeOffRefuses(t *testing.T) {
	rec := &qwenRecorder{cfg: config.VillaConfig{Model: "qwen3"}, lookPath: map[string]string{qwenBin: "/usr/bin/qwen"}, home: "/h"}
	res := RunQwen(rec.deps(), nil)
	if !res.CodingModeOff || res.ReadyToLaunch {
		t.Fatalf("RunQwen = %+v; want CodingModeOff and not ReadyToLaunch", res)
	}
	if len(rec.writes) != 0 {
		t.Errorf("settings written %d times on a refusal; want 0", len(rec.writes))
	}
	if !strings.Contains(res.Reason, "villa coding-mode enter") {
		t.Errorf("Reason %q does not point at `villa coding-mode enter`", res.Reason)
	}
}

// TestRunQwenBinaryAbsent guards the install-hint refusal: qwen missing from PATH
// writes nothing, never launches and names the install docs.
func TestRunQwenBinaryAbsent(t *testing.T) {
	rec := &qwenRecorder{cfg: config.VillaConfig{Model: "qwen3", CodingMode: true}, home: "/h"}
	res := RunQwen(rec.deps(), nil)
	if !res.BinaryAbsent || res.ReadyToLaunch {
		t.Fatalf("RunQwen = %+v; want BinaryAbsent and not ReadyToLaunch", res)
	}
	if len(rec.writes) != 0 {
		t.Errorf("settings written %d times on a refusal; want 0", len(rec.writes))
	}
	if !strings.Contains(res.Reason, "qwen is not on PATH") || !strings.Contains(res.Reason, "https://github.com/QwenLM/qwen-code") {
		t.Errorf("Reason %q does not name the missing binary and the install docs", res.Reason)
	}
}

// TestRunQwenReadyToLaunch guards the launch contract (ADR-0039): the env carries
// every opt-out switch, the isolated QWEN_HOME and the real secret, never yolo, and
// settings.json is written to that same home on this launch.
func TestRunQwenReadyToLaunch(t *testing.T) {
	restore := osEnviron
	osEnviron = func() []string { return []string{"PATH=/usr/bin", "HOME=/home/x"} }
	defer func() { osEnviron = restore }()

	rec := &qwenRecorder{
		cfg:      config.VillaConfig{Model: "qwen3", CoderModel: "qwen3-coder", CodingMode: true, InferenceSecret: qwenTestSecret},
		lookPath: map[string]string{qwenBin: "/usr/local/bin/qwen"},
		home:     "/cfg/villa/qwen",
	}
	extra := []string{"--foo", "bar"}
	res := RunQwen(rec.deps(), extra)
	if !res.ReadyToLaunch {
		t.Fatalf("RunQwen = %+v; want ReadyToLaunch", res)
	}
	if res.LaunchBin != "/usr/local/bin/qwen" || strings.Join(res.LaunchArgs, " ") != "--foo bar" {
		t.Errorf("LaunchBin/Args = %q %v; want the LookPath result and the verbatim extra args", res.LaunchBin, res.LaunchArgs)
	}
	if len(rec.writes) != 1 || rec.writes[0].home != "/cfg/villa/qwen" {
		t.Fatalf("settings writes = %v; want exactly one, into QwenHome", rec.writes)
	}
	if rec.launches != 0 {
		t.Errorf("RunQwen launched itself; the caller owns the exec")
	}

	for _, w := range []string{
		"PATH=/usr/bin", "HOME=/home/x",
		"QWEN_HOME=/cfg/villa/qwen",
		"QWEN_USAGE_STATISTICS_ENABLED=false",
		"QWEN_CODE_MODELS_DEV_REFRESH=off",
		"OPENAI_API_KEY=" + qwenTestSecret,
		envDoNotTrack,
	} {
		if !envHas(res.LaunchEnv, w) {
			t.Errorf("LaunchEnv %v missing %q", res.LaunchEnv, w)
		}
	}
	for _, s := range append(append([]string{}, res.LaunchEnv...), res.LaunchArgs...) {
		if strings.Contains(strings.ToLower(s), "yolo") {
			t.Errorf("launch carries %q; villa never auto-approves tool calls", s)
		}
	}
	if strings.Contains(string(rec.writes[0].b), qwenTestSecret) {
		t.Error("written settings.json contains the inference secret; it belongs only in the env")
	}
}

// TestRunQwenWriteFailureRefuses guards fail-closed: if the contract file cannot be
// written, the agent is not launched with whatever the user's own config says.
func TestRunQwenWriteFailureRefuses(t *testing.T) {
	rec := &qwenRecorder{
		cfg:      config.VillaConfig{Model: "qwen3", CodingMode: true},
		lookPath: map[string]string{qwenBin: "/usr/local/bin/qwen"},
		home:     "/h", writeErr: errors.New("disk full"),
	}
	res := RunQwen(rec.deps(), nil)
	if res.Err == nil || res.ReadyToLaunch {
		t.Fatalf("RunQwen = %+v; want Err and not ReadyToLaunch", res)
	}
}

// TestRenderQwenSettingsGolden freezes the villa-written settings.json: byte
// deterministic, equal to the golden (refreeze with -update), and carrying the
// provider entry plus every opt-out key ADR-0039 names.
func TestRenderQwenSettingsGolden(t *testing.T) {
	cfg := renderTestConfig()
	got, err := RenderQwenSettings(cfg)
	if err != nil {
		t.Fatalf("RenderQwenSettings: %v", err)
	}
	again, _ := RenderQwenSettings(cfg)
	if string(got) != string(again) {
		t.Fatal("RenderQwenSettings is not byte-deterministic")
	}
	if strings.Contains(string(got), cfg.InferenceSecret) {
		t.Error("rendered settings contain the inference secret")
	}

	goldenPath := filepath.Join("testdata", "qwen-settings.json.golden")
	if *update {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("settings != golden\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
