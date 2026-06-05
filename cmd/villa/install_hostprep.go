package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// install_hostprep.go holds the live host-touching seams the install verb injects:
// the consented privileged execs (setsebool / loginctl via orchestrate), the
// interactive-TTY consent prompt, and the 503-aware readiness poll. They are the
// production wiring for installDeps; runInstall's logic is exercised entirely
// through stubs in install_test.go, so these stay thin.

// readinessTimeout bounds the total readiness poll so an unreachable / never-ready
// server cannot hang install — on timeout the verdict is a typed-Unknown WARN, not
// a confident FAIL (WR-07).
const readinessTimeout = 90 * time.Second

// readinessInterval is the gap between readiness probes.
const readinessInterval = 2 * time.Second

// readinessHTTPTimeout bounds a single /health probe.
const readinessHTTPTimeout = 3 * time.Second

// liveSetsebool runs the consented SELinux fix as a FIXED-ARG exec (never a shell,
// T-03-08): `setsebool -P container_use_devices=true`. The boolean name is a
// constant, not interpolated from user input.
func liveSetsebool() error {
	if _, err := exec.LookPath("setsebool"); err != nil {
		return orchestrate.ErrToolNotFound{Tool: "setsebool"}
	}
	cmd := exec.Command("setsebool", "-P", "container_use_devices=true") // fixed args
	return cmd.Run()
}

// stdinIsInteractive reports whether stdin is a TTY (so consent prompting is
// meaningful). A piped / redirected stdin is treated as non-interactive →
// install prints the command and blocks instead of prompting (D-04/D-05).
func stdinIsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// promptConsent prints the prompt to stderr and reads a single line from stdin,
// returning true only on an explicit y/yes (case-insensitive). Anything else
// (including EOF / empty) is a decline — consent is opt-IN per D-04.
func promptConsent(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

// liveReadinessPoll polls the server for readiness until the timeout, using the
// default HTTP client. It is the production pollReady seam (Task 2 logic lives in
// pollReadiness so it is unit-testable with a stub probe).
func liveReadinessPoll(ctx context.Context, endpoint string) installReadiness {
	client := &http.Client{Timeout: readinessHTTPTimeout}
	probe := func() (int, error) {
		// Build the request with ctx so a cancelled/expired context aborts an
		// in-flight probe rather than running to the client's own timeout (WR-05).
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/health", nil)
		if err != nil {
			return 0, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
		return resp.StatusCode, nil
	}
	return pollReadiness(ctx, probe, readinessTimeout, readinessInterval)
}

// pollReadiness is the pure readiness-poll loop (Task 2): it repeatedly calls
// probe until it returns HTTP 200 (→ PASS) or the deadline elapses. A 503 (the
// server is up but still loading the model) is "keep polling", NOT a confident
// down (Pitfall 2 / WR-07). On deadline with no 200 it returns a typed-Unknown
// WARN — never a confident FAIL. probe returns (statusCode, err); a transport
// error or a non-200/503 status is also keep-polling until the deadline.
func pollReadiness(ctx context.Context, probe func() (int, error), timeout, interval time.Duration) installReadiness {
	deadline := time.Now().Add(timeout)
	var lastDetail = "server did not become ready before the timeout"

	for {
		// Check the deadline/cancellation BEFORE each probe (as well as after) so a
		// probe that could take up to one full HTTP timeout cannot push the total
		// wall-clock past the budget, and a context cancelled between probes aborts
		// promptly (WR-05).
		if time.Now().After(deadline) {
			return installReadiness{status: preflight.StatusWarn, detail: lastDetail}
		}
		select {
		case <-ctx.Done():
			return installReadiness{status: preflight.StatusWarn, detail: "readiness poll cancelled: " + ctx.Err().Error()}
		default:
		}

		code, err := probe()
		switch {
		case err != nil:
			lastDetail = fmt.Sprintf("not ready yet (%v)", err)
		case code == http.StatusOK:
			return installReadiness{status: preflight.StatusPass, detail: "server is ready (/health 200)"}
		case code == http.StatusServiceUnavailable:
			// 503: server is up but still loading — keep polling, never down (WR-07).
			lastDetail = "server is up but still loading the model (/health 503) — waiting"
		default:
			lastDetail = fmt.Sprintf("not ready yet (/health %d)", code)
		}

		if time.Now().After(deadline) {
			// Timed out without a 200: typed-Unknown → WARN, not a confident FAIL.
			return installReadiness{status: preflight.StatusWarn, detail: lastDetail}
		}

		select {
		case <-ctx.Done():
			return installReadiness{status: preflight.StatusWarn, detail: "readiness poll cancelled: " + ctx.Err().Error()}
		case <-time.After(interval):
		}
	}
}
