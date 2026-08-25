package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// TestEvalAgentVerify table-drives the PURE runtime zero-outbound coding-agent proof core
// over its five outcomes, mirroring TestEvalRagSmoke (cmd/villa/verify_memory_test.go). The
// load-bearing invariants it locks (honesty-by-construction):
//
//   - The egress negative-control is asserted FIRST: a probe that could not RUN (err) is a
//     FAIL ("could not run the egress negative-control probe"), and an external host that WAS
//     reachable (blocked == false) is a FAIL ("egress is NOT blocked"). Neither is a skip.
//   - Only AFTER egress is proven blocked is the agent task trusted: a task error OR a task
//     that did not complete → FAIL ("the agent task did not complete under the egress block").
//   - The llama-down control then runs: an answer with villa-llama stopped is the cloud-
//     fallback smoking gun → FAIL; an error from llamaDownTask is the EXPECTED inference-down
//     outcome and is fine.
//   - All four controls passing-as-designed (egress blocked + task completed + llama-down NOT
//     answered) → PASS.
//   - There is NO WARN and NO skip path — an unevaluable result is a FAIL.
func TestEvalAgentVerify(t *testing.T) {
	cases := []struct {
		name string
		// ctrl1 negative control
		blocked   bool
		egressErr error
		// ctrl1 real task under the egress block
		completed bool
		taskErr   error
		// ctrl2 llama-down control
		answered     bool
		llamaDownErr error
		wantStatus   preflight.Status
	}{
		{
			name:       "egress negative-control probe error",
			blocked:    false,
			egressErr:  errors.New("podman: no such network"),
			wantStatus: preflight.StatusFail,
		},
		{
			name:       "egress NOT blocked (external host reachable)",
			blocked:    false,
			egressErr:  nil,
			wantStatus: preflight.StatusFail,
		},
		{
			name:       "blocked + agent task error",
			blocked:    true,
			taskErr:    errors.New("crush run: exit 1"),
			wantStatus: preflight.StatusFail,
		},
		{
			name:       "blocked + agent task did not complete",
			blocked:    true,
			completed:  false,
			wantStatus: preflight.StatusFail,
		},
		{
			name:       "blocked + completed + llama-down ANSWERED (cloud fallback)",
			blocked:    true,
			completed:  true,
			answered:   true,
			wantStatus: preflight.StatusFail,
		},
		{
			name:         "blocked + completed + llama-down errored (expected inference-down) → PASS",
			blocked:      true,
			completed:    true,
			answered:     false,
			llamaDownErr: errors.New("connection refused"),
			wantStatus:   preflight.StatusPass,
		},
		{
			name:       "all good (blocked + completed + llama-down NOT answered) → PASS",
			blocked:    true,
			completed:  true,
			answered:   false,
			wantStatus: preflight.StatusPass,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			egressBlocked := func() (bool, error) { return tc.blocked, tc.egressErr }
			agentTask := func() (bool, error) { return tc.completed, tc.taskErr }
			llamaDownTask := func() (bool, error) { return tc.answered, tc.llamaDownErr }
			got := evalAgentVerify(egressBlocked, agentTask, llamaDownTask)
			if got.status != tc.wantStatus {
				t.Errorf("status = %v, want %v (detail %q)", got.status, tc.wantStatus, got.detail)
			}
			if tc.wantStatus == preflight.StatusFail && got.detail == "" {
				t.Errorf("a FAIL verdict must carry a refuse-with-remediation detail")
			}
		})
	}
}

// TestEvalAgentVerifyNegativeControlFirst is the false-green guard: when the egress negative-
// control fails (either the probe errored OR an external host was reachable), neither the
// agent task NOR the llama-down control may run. If the agent task ran, a host that never
// actually blocked egress could still produce a green by completing the task — exactly the
// false-green forbids. Spies record whether each downstream control was invoked.
func TestEvalAgentVerifyNegativeControlFirst(t *testing.T) {
	t.Run("probe error short-circuits before the agent task", func(t *testing.T) {
		taskRan, llamaDownRan := false, false
		egressBlocked := func() (bool, error) { return false, errors.New("probe could not run") }
		agentTask := func() (bool, error) { taskRan = true; return true, nil }
		llamaDownTask := func() (bool, error) { llamaDownRan = true; return false, nil }
		got := evalAgentVerify(egressBlocked, agentTask, llamaDownTask)
		if got.status != preflight.StatusFail {
			t.Fatalf("status = %v, want FAIL when the negative control could not run", got.status)
		}
		if taskRan {
			t.Fatalf("agentTask ran despite a failed negative control — false-green hazard")
		}
		if llamaDownRan {
			t.Fatalf("llamaDownTask ran despite a failed negative control — false-green hazard")
		}
	})

	t.Run("egress reachable short-circuits before the agent task", func(t *testing.T) {
		taskRan, llamaDownRan := false, false
		egressBlocked := func() (bool, error) { return false, nil } // external host WAS reachable
		agentTask := func() (bool, error) { taskRan = true; return true, nil }
		llamaDownTask := func() (bool, error) { llamaDownRan = true; return false, nil }
		got := evalAgentVerify(egressBlocked, agentTask, llamaDownTask)
		if got.status != preflight.StatusFail {
			t.Fatalf("status = %v, want FAIL when egress is not blocked", got.status)
		}
		if taskRan {
			t.Fatalf("agentTask ran despite egress not being blocked — false-green hazard")
		}
		if llamaDownRan {
			t.Fatalf("llamaDownTask ran despite egress not being blocked — false-green hazard")
		}
	})

	t.Run("a not-completed agent task short-circuits before the llama-down control", func(t *testing.T) {
		llamaDownRan := false
		egressBlocked := func() (bool, error) { return true, nil }
		agentTask := func() (bool, error) { return false, nil } // ran but did not complete
		llamaDownTask := func() (bool, error) { llamaDownRan = true; return false, nil }
		got := evalAgentVerify(egressBlocked, agentTask, llamaDownTask)
		if got.status != preflight.StatusFail {
			t.Fatalf("status = %v, want FAIL when the agent task did not complete", got.status)
		}
		if llamaDownRan {
			t.Fatalf("llamaDownTask ran despite the agent task not completing — wasted service stop")
		}
	})
}

// TestClassifyEgressProbe is the false-green / false-FAIL guard at the live-closure
// classification granularity (the live podman/curl exec is not driveable off-hardware, so the
// closure delegates the verdict math to this pure classifier). The truth table it locks:
//
//   - ANY sanity-probe error → an infrastructure FAIL (err != nil), regardless of the external
//     result. A wholesale-broken probe environment (missing image/network, podman daemon error,
//
// curl absent, villa-llama down) must NEVER read as blocked=true (the false-green
//
//	  forbids — the negative control is the FIRST gate).
//	- sanity ok + a curl CONNECTION/TIMEOUT exit (6 could-not-resolve, 7 failed-to-connect,
//	  28 operation-timeout) → blocked=true (the external host is genuinely unreachable).
//	- sanity ok + exit 0 (external host reachable) → blocked=false (egress is NOT blocked).
//	- sanity ok + an UNCLASSIFIED non-zero exit (e.g. 127 curl-absent) or a never-started
//	  container (a non-ExitError failure, exit code <0) → an infrastructure FAIL (err != nil),
//	  NOT blocked=true. "The probe could not run" is distinct from "the host was blocked".
func TestClassifyEgressProbe(t *testing.T) {
	cases := []struct {
		name         string
		sanityErr    error
		externalExit int
		externalErr  error
		wantBlocked  bool
		wantErr      bool
	}{
		{
			name:         "sanity probe broken → infra FAIL regardless of external",
			sanityErr:    errors.New("in-network sanity probe failed"),
			externalExit: 7,
			externalErr:  errors.New("connection refused"),
			wantBlocked:  false,
			wantErr:      true,
		},
		{
			name:         "sanity ok + curl 7 (failed to connect) → blocked",
			externalExit: 7,
			externalErr:  errors.New("curl: (7) Failed to connect"),
			wantBlocked:  true,
			wantErr:      false,
		},
		{
			name:         "sanity ok + curl 28 (timeout) → blocked",
			externalExit: 28,
			externalErr:  errors.New("curl: (28) Operation timed out"),
			wantBlocked:  true,
			wantErr:      false,
		},
		{
			name:         "sanity ok + curl 6 (could not resolve host) → blocked",
			externalExit: 6,
			externalErr:  errors.New("curl: (6) Could not resolve host"),
			wantBlocked:  true,
			wantErr:      false,
		},
		{
			name:         "sanity ok + exit 0 (external reachable) → NOT blocked",
			externalExit: 0,
			externalErr:  nil,
			wantBlocked:  false,
			wantErr:      false,
		},
		{
			name:         "sanity ok + unclassified non-zero (127 curl-absent) → infra FAIL",
			externalExit: 127,
			externalErr:  errors.New("curl: command not found"),
			wantBlocked:  false,
			wantErr:      true,
		},
		{
			name:         "sanity ok + never-started container (no ExitError, code -1) → infra FAIL",
			externalExit: -1,
			externalErr:  errors.New("podman: container could not start"),
			wantBlocked:  false,
			wantErr:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocked, err := classifyEgressProbe(tc.sanityErr, tc.externalExit, tc.externalErr)
			if blocked != tc.wantBlocked {
				t.Errorf("blocked = %t, want %t", blocked, tc.wantBlocked)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %t", err, tc.wantErr)
			}
			// The cardinal invariant: an infra failure must NEVER be reported as blocked.
			if tc.wantErr && blocked {
				t.Errorf("an infrastructure failure must never read as blocked=true (false-green)")
			}
		})
	}
}

// TestEvalAgentVerifyInfraErrorFails proves the COMPOSED path: a classifyEgressProbe-style
// infrastructure failure (an egressBlocked closure returning (false, err)) reaches the pure
// evalAgentVerify core as a StatusFail with the "could not run the egress negative-control
// probe" detail — never a pass. This makes the infra-error → FAIL contract provably covered by
// the `-run 'TestClassifyEgressProbe|TestEvalAgentVerify'` verify filter.
func TestEvalAgentVerifyInfraErrorFails(t *testing.T) {
	// An infra error as classifyEgressProbe would return it for a broken probe environment.
	_, infraErr := classifyEgressProbe(errors.New("in-network sanity probe to villa-llama failed"), 0, nil)
	if infraErr == nil {
		t.Fatalf("a broken sanity probe must yield an infrastructure error")
	}
	egressBlocked := func() (bool, error) { return false, infraErr }
	agentTask := func() (bool, error) { return true, nil }
	llamaDownTask := func() (bool, error) { return false, nil }

	got := evalAgentVerify(egressBlocked, agentTask, llamaDownTask)
	if got.status != preflight.StatusFail {
		t.Fatalf("status = %v, want FAIL when the egress probe could not run", got.status)
	}
	if !strings.Contains(got.detail, "could not run the egress negative-control probe") {
		t.Errorf("detail = %q, want it to name the could-not-run-the-probe remediation", got.detail)
	}
}

// TestRestoreLlamaWarning is the message guard. A nil restore error yields NO warning
// (an empty string — the clean path emits nothing). A non-nil restore error yields a warning
// that names the service was left stopped AND carries the LITERAL manual remediation
// `systemctl --user start villa-llama.service` so the operator can recover by hand.
func TestRestoreLlamaWarning(t *testing.T) {
	if got := restoreLlamaWarning(installServiceName, nil); got != "" {
		t.Errorf("restoreLlamaWarning(nil) = %q, want empty (no warning when restore succeeded)", got)
	}
	got := restoreLlamaWarning(installServiceName, errors.New("systemctl: transient error"))
	if got == "" {
		t.Fatalf("restoreLlamaWarning(err) must produce a warning")
	}
	if !strings.Contains(got, "systemctl --user start "+installServiceName) {
		t.Errorf("warning = %q, want it to contain the manual remediation `systemctl --user start %s`", got, installServiceName)
	}
	if !strings.Contains(got, installServiceName) {
		t.Errorf("warning = %q, want it to name the service left stopped", got)
	}
}

// TestLlamaDownRestore is the behaviour guard at the control granularity. The llama-down
// control STOPS villa-llama, runs the task, then ALWAYS attempts to restore (Start) — and on a
// restore failure it must SURFACE the Start error (never swallow it), while still having
// ATTEMPTED the restore on every path. Driven via injected stop/start/task func seams (no live
// host).
func TestLlamaDownRestore(t *testing.T) {
	t.Run("restore failure is surfaced; restore still attempted", func(t *testing.T) {
		startCalled := false
		stop := func() error { return nil }
		start := func() error { startCalled = true; return errors.New("systemctl start: transient") }
		task := func() (bool, error) { return false, errors.New("connection refused") } // inference down

		answered, restoreErr := runLlamaDownControl(stop, start, task)
		if !startCalled {
			t.Errorf("the restore (Start) must be ATTEMPTED unconditionally (deferred)")
		}
		if restoreErr == nil {
			t.Errorf("a Start failure must be SURFACED, not swallowed")
		}
		if answered {
			t.Errorf("inference-down task did not answer; answered must be false")
		}
	})

	t.Run("clean restore surfaces no error", func(t *testing.T) {
		stop := func() error { return nil }
		start := func() error { return nil }
		task := func() (bool, error) { return false, nil }
		answered, restoreErr := runLlamaDownControl(stop, start, task)
		if restoreErr != nil {
			t.Errorf("a clean restore must surface no error, got %v", restoreErr)
		}
		if answered {
			t.Errorf("answered must be false")
		}
	})

	t.Run("stop failure short-circuits (no restore of a service never stopped)", func(t *testing.T) {
		startCalled := false
		stop := func() error { return errors.New("systemctl stop: failed") }
		start := func() error { startCalled = true; return nil }
		task := func() (bool, error) { t.Fatalf("task must not run when stop failed"); return false, nil }
		answered, restoreErr := runLlamaDownControl(stop, start, task)
		if startCalled {
			t.Errorf("must not restore a service that was never stopped")
		}
		if answered {
			t.Errorf("answered must be false when stop failed")
		}
		_ = restoreErr
	})
}

// TestVerifyAgentRegistered asserts `villa verify agent` is registered under the `verify`
// parent next to `verify memory` (a missing subcommand is a silent regression of the runtime
// gate).
func TestVerifyAgentRegistered(t *testing.T) {
	verify := newVerify()
	var foundAgent, foundMemory bool
	for _, c := range verify.Commands() {
		switch c.Name() {
		case "agent":
			foundAgent = true
		case "memory":
			foundMemory = true
		}
	}
	if !foundMemory {
		t.Errorf("`verify memory` is no longer registered under `verify`")
	}
	if !foundAgent {
		t.Errorf("`verify agent` is not registered under `verify` — the runtime PRIV-06 proof is unreachable")
	}
}

// TestRunVerifyAgentGate drives runVerifyAgent over the injectable seam to lock the three
// load-bearing behaviours (mirrors TestRunVerifyMemoryGate): (1) the agent addon OFF exits 0
// without ever running the proof (nothing to verify — NOT the silent-skip hazard); (2) addon
// ON + a FAIL verdict returns exitBlocked with the remediation detail on stderr; (3) addon ON
// + a PASS returns exitPass. The proof seam is injected so no live host is needed.
func TestRunVerifyAgentGate(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{}
		c.SetContext(t.Context())
		return c
	}

	t.Run("agent off exits 0 without running the proof", func(t *testing.T) {
		proofRan := false
		deps := verifyAgentDeps{
			loadedAgentEnabled: func() bool { return false },
			verifyFn: func(context.Context, verifyAgentDeps) memoryProof {
				proofRan = true
				return memoryProof{status: preflight.StatusPass}
			},
		}
		cmd := newCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if code := runVerifyAgent(cmd, nil, deps); code != exitPass {
			t.Errorf("agent-off exit = %d, want exitPass (%d)", code, exitPass)
		}
		if proofRan {
			t.Errorf("the proof must NOT run when the agent addon is off")
		}
	})

	t.Run("agent on FAIL returns exitBlocked with remediation", func(t *testing.T) {
		deps := verifyAgentDeps{
			loadedAgentEnabled: func() bool { return true },
			verifyFn: func(context.Context, verifyAgentDeps) memoryProof {
				return memoryProof{status: preflight.StatusFail, detail: "egress is NOT blocked"}
			},
		}
		cmd := newCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runVerifyAgent(cmd, nil, deps); code != exitBlocked {
			t.Errorf("agent-on FAIL exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		if errOut.Len() == 0 {
			t.Errorf("a FAIL must print a remediation to stderr")
		}
	})

	t.Run("agent on PASS returns exitPass", func(t *testing.T) {
		deps := verifyAgentDeps{
			loadedAgentEnabled: func() bool { return true },
			verifyFn: func(context.Context, verifyAgentDeps) memoryProof {
				return memoryProof{status: preflight.StatusPass, detail: "zero-outbound agent task completed; no cloud fallback (llama-down control failed as expected)"}
			},
		}
		cmd := newCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if code := runVerifyAgent(cmd, nil, deps); code != exitPass {
			t.Errorf("agent-on PASS exit = %d, want exitPass (%d)", code, exitPass)
		}
		if out.Len() == 0 {
			t.Errorf("a PASS must print the proof detail to stdout")
		}
	})
}
