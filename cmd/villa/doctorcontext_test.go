package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// TestDoctorProofsHonourTheCommandContext pins that a Ctrl-C can interrupt
// `villa doctor`.
//
// doctor runs three residency proofs, each of which drives a LIVE stack (podman
// probe containers issuing embed / chat / tool-call rounds) for up to its budget:
// 60s for the memory proof, 90s each for the search and agent proofs. Those
// budgets used to be the only bound, because every proof was started with
// context.Background(); the SIGINT-cancelled context main installs never reached
// them, so a Ctrl-C left the user watching a command drive the stack for minutes.
//
// Interrupting is safe: doctor is strictly read-only (it writes no unit files and
// starts no service), and every probe is an exec.CommandContext child that dies
// with the context rather than outliving the process.
//
// The assertion is twofold, because "it stopped" is not enough on its own:
//
//   - it returns PROMPTLY. A proof that ignored the context would sit in its drive
//     for the full budget, so returning far inside the shortest budget is the
//     evidence that cancellation was observed.
//   - it degrades to a typed-Unknown WARN, never a FAIL. An interrupted proof has
//     not observed a CPU fallback; reporting one would fabricate a blocking fault
//     out of a signal that was never evaluated, and doctor's exit code is what
//     scripts branch on.
func TestDoctorProofsHonourTheCommandContext(t *testing.T) {
	// A `podman` on PATH that simply sleeps. The proofs shell out to the real binary,
	// and on a host without a live stack it errors in milliseconds — which would let
	// this test pass against the pre-fix code for the wrong reason (the drive was
	// fast, not interrupted). A drive that hangs makes "returned promptly" mean
	// exactly one thing: the context was observed.
	stubSlowPodman(t)

	// A healthy fully-stubbed world, so nothing short-circuits on a precondition
	// gate: the proofs get as far as their drive, which is what must be interrupted.
	sd, err := status.StubDeps(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("status.StubDeps: %v", err)
	}
	cfg := config.VillaConfig{
		Backend: "vulkan", Model: "qwen3", Ctx: 131072,
		// All three subsystems on, so every proof clears its enabled/valid gate and
		// actually reaches the drive this test is about interrupting.
		MemoryEnabled: true, WebSearchEnabled: true, AgentEnabled: true,
		EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDim: 768,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Comfortably inside the shortest proof budget (residencyProofBudget, 60s) while
	// still leaving room for a slow CI machine, so a pass means "observed the
	// cancellation", not "got lucky on timing".
	const promptly = 10 * time.Second

	proofs := []struct {
		name string
		run  func(context.Context, config.VillaConfig, *status.Deps) inference.Verdict
	}{
		{"memory residency", runResidencyUnderLoad},
		{"search residency", runSearchResidencyUnderLoad},
		{"agent residency", runAgentResidencyUnderLoad},
	}

	for _, p := range proofs {
		t.Run(p.name, func(t *testing.T) {
			got := make(chan inference.Verdict, 1)
			start := time.Now()
			go func() { got <- p.run(ctx, cfg, &sd) }()

			select {
			case v := <-got:
				if elapsed := time.Since(start); elapsed > promptly {
					t.Errorf("%s took %v to notice its cancelled context", p.name, elapsed)
				}
				if v.Status == inference.StatusFail {
					t.Errorf("%s reported a FAIL from an interrupted run (%q) — an unevaluated "+
						"signal must degrade to a typed-Unknown WARN, never a fabricated "+
						"blocking fault", p.name, v.Detail)
				}
				if v.Remediation == "" {
					t.Errorf("%s degraded without a Remediation, leaving the user no next step", p.name)
				}
			case <-time.After(promptly):
				t.Fatalf("%s ignored its cancelled context and kept driving the stack — "+
					"a Ctrl-C cannot interrupt `villa doctor`", p.name)
			}
		})
	}
}

// stubSlowPodman puts a `podman` on PATH that sleeps far longer than this test's
// patience, so a residency drive blocks instead of failing fast.
//
// The doctor proofs run their probes via exec.CommandContext, so a cancelled
// context kills this child; that is precisely the behaviour under test, and it is
// why an interrupted `villa doctor` leaves no probe container running.
func stubSlowPodman(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nsleep 300\n"
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte(script), 0o700); err != nil {
		t.Fatalf("write podman stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
