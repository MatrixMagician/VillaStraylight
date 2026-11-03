package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// TestCodeAgentQwenLaunches guards the dispatch: `--agent qwen` with coding mode on
// and `qwen` on PATH writes settings once, launches through the PATH-agent exec seam
// exactly once (never the Crush Launch), forwards the trailing args verbatim and
// carries the opt-out env.
func TestCodeAgentQwenLaunches(t *testing.T) {
	rec := &codeRecorder{
		cfg:       config.VillaConfig{Model: "qwen3", CodingMode: true},
		lookFound: map[string]bool{"qwen": true},
	}
	cmd, _, _ := newCodeCmd()
	args := []string{"--foo", "bar"}
	if code := runCodeDispatch(cmd, args, "qwen", rec.deps()); code != exitPass {
		t.Fatalf("runCodeDispatch exit = %d, want %d", code, exitPass)
	}
	if len(rec.launchClaudeCalls) != 1 || len(rec.launchCalls) != 0 {
		t.Fatalf("exec seam calls = %d, Crush Launch calls = %d; want 1 and 0", len(rec.launchClaudeCalls), len(rec.launchCalls))
	}
	if len(rec.qwenWrites) != 1 {
		t.Fatalf("settings writes = %d; want 1", len(rec.qwenWrites))
	}
	got := rec.launchClaudeCalls[0]
	if got.bin != "/usr/bin/qwen" || strings.Join(got.args, " ") != "--foo bar" {
		t.Errorf("launch = %q %v; want /usr/bin/qwen --foo bar", got.bin, got.args)
	}
	found := false
	for _, e := range got.env {
		if e == "QWEN_USAGE_STATISTICS_ENABLED=false" {
			found = true
		}
	}
	if !found {
		t.Errorf("launch env lacks QWEN_USAGE_STATISTICS_ENABLED=false")
	}
}

// TestCodeAgentQwenRefusals guards that coding mode off and qwen absent both refuse
// (exitBlocked) without writing settings or launching, and say why.
func TestCodeAgentQwenRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		rec  *codeRecorder
		want string
	}{
		{"coding mode off", &codeRecorder{cfg: config.VillaConfig{Model: "qwen3"}, lookFound: map[string]bool{"qwen": true}}, "villa coding-mode enter"},
		{"binary absent", &codeRecorder{cfg: config.VillaConfig{Model: "qwen3", CodingMode: true}}, "https://github.com/QwenLM/qwen-code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _, errOut := newCodeCmd()
			if code := runCodeDispatch(cmd, nil, "qwen", tc.rec.deps()); code != exitBlocked {
				t.Fatalf("exit = %d, want %d", code, exitBlocked)
			}
			if len(tc.rec.launchClaudeCalls) != 0 || len(tc.rec.qwenWrites) != 0 {
				t.Errorf("a refusal must neither launch nor write settings")
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("stderr %q lacks %q", errOut.String(), tc.want)
			}
		})
	}
}

// TestCodeExtraArgsStillBlockedForCrush guards that extra args stay refused for the
// Crush target now that two agents accept them, and the message names both.
func TestCodeExtraArgsStillBlockedForCrush(t *testing.T) {
	rec := &codeRecorder{cfg: config.VillaConfig{Model: "qwen3", CodingMode: true}}
	cmd, _, errOut := newCodeCmd()
	if code := runCodeDispatch(cmd, []string{"extra"}, "crush", rec.deps()); code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if len(rec.launchCalls) != 0 {
		t.Errorf("Crush Launch must NOT be called")
	}
	if !strings.Contains(errOut.String(), "claude") || !strings.Contains(errOut.String(), "qwen") {
		t.Errorf("stderr %q should name both --agent claude and --agent qwen", errOut.String())
	}
}

// TestQwenBaseURLMatchesInferenceServerPort ties the rendered Qwen settings' baseUrl
// to inference.ServerPort(), the third-target twin of the Crush and Claude cross-seam
// drift guards.
func TestQwenBaseURLMatchesInferenceServerPort(t *testing.T) {
	b, err := agent.RenderQwenSettings(config.VillaConfig{Model: "qwen3", CodingMode: true})
	if err != nil {
		t.Fatalf("render qwen settings: %v", err)
	}
	want := fmt.Sprintf("127.0.0.1:%d/v1", inference.ServerPort())
	if !bytes.Contains(b, []byte(want)) {
		t.Errorf("qwen settings baseUrl does not embed inference.ServerPort()=%d (want substring %q)", inference.ServerPort(), want)
	}
}

// TestQwenSettingsWriter guards the live writer: settings.json lands at 0600 inside
// a 0700 villa-owned dir, and a second write replaces a hand-edited file.
func TestQwenSettingsWriter(t *testing.T) {
	home := filepath.Join(t.TempDir(), "villa", "qwen")
	if err := writeQwenSettings(home, []byte("{\"a\":1}\n")); err != nil {
		t.Fatalf("writeQwenSettings: %v", err)
	}
	if err := writeQwenSettings(home, []byte("{\"a\":2}\n")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	path := filepath.Join(home, "settings.json")
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "{\"a\":2}\n" {
		t.Fatalf("settings.json = %q, %v; want the second write", b, err)
	}
	fi, _ := os.Stat(path)
	di, _ := os.Stat(home)
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Errorf("modes file=%o dir=%o; want 600 and 700", fi.Mode().Perm(), di.Mode().Perm())
	}
}

// TestLiveAgentDepsWiresQwen guards that the live wiring leaves no Qwen seam nil.
func TestLiveAgentDepsWiresQwen(t *testing.T) {
	d := liveAgentDeps()
	if d.QwenHome == nil || d.WriteQwenSettings == nil {
		t.Error("liveAgentDeps leaves a Qwen seam nil; villa code --agent qwen would panic")
	}
}
