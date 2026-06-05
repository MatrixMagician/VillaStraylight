package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/dashboard"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
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

// stubDashboardDeps builds dashboardDeps with a config loader and a no-bind Serve stub
// so runDashboard can be driven without opening a real socket.
func stubDashboardDeps(serve func(*dashboard.Server) error) *dashboardDeps {
	return &dashboardDeps{
		LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{DashboardAddr: "127.0.0.1", DashboardPort: 8888, ChatPort: 3000}, nil
		},
		StatusDeps: status.Deps{},
		Serve:      serve,
	}
}

// TestRunDashboardCleanStart asserts runDashboard returns exitPass (0) when the serve
// dep returns nil, and prints the loopback URL (so a user knows where to point the
// browser). No real listener is bound.
func TestRunDashboardCleanStart(t *testing.T) {
	cmd, out, _ := dashboardTestCmd()

	var served bool
	d := stubDashboardDeps(func(s *dashboard.Server) error {
		served = true
		// Assert the server was composed with the loopback addr.
		if got := s.Addr(); got != "127.0.0.1:8888" {
			t.Fatalf("server addr = %q, want 127.0.0.1:8888", got)
		}
		return nil
	})

	code := runDashboard(cmd, nil, d)
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
	d := stubDashboardDeps(func(*dashboard.Server) error { return errors.New("bind: address in use") })

	code := runDashboard(cmd, nil, d)
	if code != exitBlocked {
		t.Fatalf("runDashboard on serve error = %d, want %d (exitBlocked)", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "bind: address in use") {
		t.Fatalf("stderr missing serve error\n%s", errOut.String())
	}
}

// TestRunDashboardConfigError asserts a config load failure maps to exitBlocked.
func TestRunDashboardConfigError(t *testing.T) {
	cmd, _, errOut := dashboardTestCmd()
	d := stubDashboardDeps(func(*dashboard.Server) error { return nil })
	d.LoadConfig = func() (config.VillaConfig, error) { return config.VillaConfig{}, errors.New("parse config.toml") }

	code := runDashboard(cmd, nil, d)
	if code != exitBlocked {
		t.Fatalf("runDashboard on config error = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "parse config.toml") {
		t.Fatalf("stderr missing config error\n%s", errOut.String())
	}
}
