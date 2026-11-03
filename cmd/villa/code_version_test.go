package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestCodeVersionWarningPrintsBeforeLaunchAndNeverBlocks guards the operator signal:
// an installed Claude Code or Qwen Code that is not the vetted version prints the
// warning on stderr, then still launches through the exec seam and exits pass.
func TestCodeVersionWarningPrintsBeforeLaunchAndNeverBlocks(t *testing.T) {
	for _, tc := range []struct{ target, bin, out string }{
		{"claude", "claude", "9.9.9 (Claude Code)"},
		{"qwen", "qwen", "9.9.9"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			rec := &codeRecorder{
				cfg:        config.VillaConfig{Model: "qwen3", CodingMode: true},
				lookFound:  map[string]bool{tc.bin: true},
				versionOut: map[string]string{tc.bin: tc.out},
			}
			cmd, _, errOut := newCodeCmd()
			if code := runCodeDispatch(cmd, nil, tc.target, rec.deps()); code != exitPass {
				t.Fatalf("exit = %d, want %d", code, exitPass)
			}
			if len(rec.launchClaudeCalls) != 1 {
				t.Fatalf("launches = %d; a version warning must not block", len(rec.launchClaudeCalls))
			}
			for _, want := range []string{"warning [agent_version_unvetted]", "9.9.9", "docs/RELEASING.md"} {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("stderr %q lacks %q", errOut.String(), want)
				}
			}
		})
	}
}

// TestCodeVersionMatchPrintsNoWarning guards the quiet path through the command tier.
func TestCodeVersionMatchPrintsNoWarning(t *testing.T) {
	rec := &codeRecorder{
		cfg:        config.VillaConfig{Model: "qwen3", CodingMode: true},
		lookFound:  map[string]bool{"claude": true},
		versionOut: map[string]string{"claude": agent.VettedClaudeVersion + " (Claude Code)"},
	}
	cmd, _, errOut := newCodeCmd()
	if code := runCodeDispatch(cmd, nil, "claude", rec.deps()); code != exitPass {
		t.Fatalf("exit = %d, want %d", code, exitPass)
	}
	if strings.Contains(errOut.String(), "warning") {
		t.Errorf("stderr %q has a warning for the vetted version", errOut.String())
	}
}

// TestCodeRefusalNeverProbesVersion guards that a refused launch executes nothing.
func TestCodeRefusalNeverProbesVersion(t *testing.T) {
	rec := &codeRecorder{cfg: config.VillaConfig{Model: "qwen3"}, lookFound: map[string]bool{"claude": true, "qwen": true}}
	for _, target := range []string{"claude", "qwen"} {
		cmd, _, _ := newCodeCmd()
		if code := runCodeDispatch(cmd, nil, target, rec.deps()); code != exitBlocked {
			t.Fatalf("%s exit = %d, want %d", target, code, exitBlocked)
		}
	}
	if len(rec.versionCalls) != 0 {
		t.Errorf("version probed %v on refused launches; want none", rec.versionCalls)
	}
}

// TestLiveAgentDepsWiresVersion guards that the live wiring leaves the seam non-nil.
func TestLiveAgentDepsWiresVersion(t *testing.T) {
	if liveAgentDeps().AgentVersion == nil {
		t.Error("liveAgentDeps leaves AgentVersion nil; every launch would warn that the version is unknown")
	}
}

// fakeVersionBin writes an executable shell script standing in for an agent binary.
func fakeVersionBin(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-agent")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil { //nolint:gosec // test script
		t.Fatal(err)
	}
	return p
}

// TestRunVersionProbe guards the live probe: fixed `--version` arg, stdout read, the
// given env passed through, and a hung binary cut off at the timeout with an error.
func TestRunVersionProbe(t *testing.T) {
	bin := fakeVersionBin(t, `[ "$1" = "--version" ] && echo "2.1.317 (Claude Code) $PROBE_MARK"`)
	out, err := runVersionProbe(bin, []string{"PROBE_MARK=seen"}, 5*time.Second)
	if err != nil || !strings.Contains(out, "2.1.317 (Claude Code) seen") {
		t.Fatalf("probe = %q, %v; want the script's stdout under the passed env", out, err)
	}

	hung := fakeVersionBin(t, "exec sleep 30")
	start := time.Now()
	if _, err := runVersionProbe(hung, nil, 200*time.Millisecond); err == nil {
		t.Error("a hung binary returned no error")
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("probe took %v; the timeout did not cut it off", time.Since(start))
	}

	failing := fakeVersionBin(t, "exit 3")
	if _, err := runVersionProbe(failing, nil, 5*time.Second); err == nil {
		t.Error("a non-zero exit returned no error")
	}
}
