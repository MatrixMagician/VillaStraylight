package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/dashboard"
)

// dashboardTestCmd builds a cobra command with captured stdout/stderr, mirroring
// statusTestCmd.
func dashboardTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "test"}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

// stubDashboardConfig is the composed Config for the configured loopback ports, with
// every collector left nil (NewServer defaults each to an honest "unavailable").
func stubDashboardConfig() dashboard.Config {
	return dashboard.Config{DashboardAddr: config.DashboardAddr, DashboardPort: 8888, ChatPort: 3000}
}

// stubServe adapts a context-free serve stub: every caller here asserts on whether
// serve ran and what it returned, not on the context, so the adapter drops it rather
// than making each test thread a parameter it does not use. No socket is bound.
func stubServe(serve func(*dashboard.Server) error) func(context.Context, *dashboard.Server) error {
	return func(_ context.Context, s *dashboard.Server) error { return serve(s) }
}

// TestRunDashboardCleanStart asserts runDashboard returns exitPass (0) when the serve
// dep returns nil, and prints the loopback URL (so a user knows where to point the
// browser). No real listener is bound.
func TestRunDashboardCleanStart(t *testing.T) {
	cmd, out, _ := dashboardTestCmd()

	var served bool
	serve := stubServe(func(s *dashboard.Server) error {
		served = true
		// Assert the server was composed with the loopback addr.
		if got := s.Addr(); got != "127.0.0.1:8888" {
			t.Fatalf("server addr = %q, want 127.0.0.1:8888", got)
		}
		return nil
	})

	code := runDashboard(cmd, stubDashboardConfig(), serve)
	if code != exitPass {
		t.Fatalf("runDashboard = %d, want %d (exitPass)", code, exitPass)
	}
	if !served {
		t.Fatalf("Serve was not called")
	}
	if !strings.Contains(out.String(), "http://127.0.0.1:8888") {
		t.Fatalf("output missing loopback URL\n%s", out.String())
	}
}

// TestRunDashboardServeError asserts a serve/bind failure maps to exitBlocked (1).
func TestRunDashboardServeError(t *testing.T) {
	cmd, _, errOut := dashboardTestCmd()
	serve := stubServe(func(*dashboard.Server) error { return errors.New("bind: address in use") })

	code := runDashboard(cmd, stubDashboardConfig(), serve)
	if code != exitBlocked {
		t.Fatalf("runDashboard on serve error = %d, want %d (exitBlocked)", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "bind: address in use") {
		t.Fatalf("stderr missing serve error\n%s", errOut.String())
	}
}

// TestRunDashboardRefusesNonLoopbackBind asserts a Config whose bind address is not
// loopback maps to exitBlocked before anything is served: the posture NewServer
// enforces reaches the operator as a refusal, never a listener on all interfaces.
func TestRunDashboardRefusesNonLoopbackBind(t *testing.T) {
	cmd, _, errOut := dashboardTestCmd()
	c := stubDashboardConfig()
	c.DashboardAddr = "0.0.0.0"
	served := false

	code := runDashboard(cmd, c, stubServe(func(*dashboard.Server) error { served = true; return nil }))
	if code != exitBlocked || served {
		t.Fatalf("runDashboard with a non-loopback bind = %d (served %v), want %d and nothing served", code, served, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "non-loopback") {
		t.Fatalf("stderr missing the loopback refusal\n%s", errOut.String())
	}
}

// TestDashboardInferenceClientReadsTheKeyPerScrape pins #253 for the Performance
// panel: its /metrics and /slots reads take the api key from config at each scrape,
// so a key written after the dashboard started (`villa up` heals a missing one) is
// the key they send, not the empty one of startup. The client's redacted String
// shows whether a key is set without printing it.
func TestDashboardInferenceClientReadsTheKeyPerScrape(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := currentInferenceClient().String(); !strings.Contains(got, "key: <none>") {
		t.Fatalf("client before a key is written = %s, want no key", got)
	}
	if err := config.SaveVilla(config.VillaConfig{InferenceSecret: "written-after-startup"}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if got := currentInferenceClient().String(); !strings.Contains(got, "key: <redacted>") {
		t.Errorf("client after a key is written = %s, want the new key set", got)
	}
}

// TestLiveDashboardDepsConfigError asserts a config.toml that does not parse refuses
// the dashboard at startup (newDashboard maps the error to exitBlocked) rather than
// serving panels composed from a config it could not read.
func TestLiveDashboardDepsConfigError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "villa"), 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "villa", "config.toml"), []byte("model = [not toml\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := liveDashboardDeps(t.Context()); err == nil || !strings.Contains(err.Error(), "config") {
		t.Fatalf("liveDashboardDeps with an unparseable config.toml = %v, want a config load error", err)
	}
}
