package main

import (
	"errors"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// TestEvalAgentVerify table-drives the PURE runtime zero-outbound coding-agent proof core
// over its five outcomes, mirroring TestEvalRagSmoke (cmd/villa/verify_memory_test.go). The
// load-bearing invariants it locks (PRIV-06, D-07/D-08, honesty-by-construction):
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
		answered    bool
		llamaDownErr error
		wantStatus  preflight.Status
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
// false-green PRIV-06 forbids. Spies record whether each downstream control was invoked.
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
