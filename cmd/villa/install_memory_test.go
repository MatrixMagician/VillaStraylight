package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// install_memory_test.go — Phase-23 D-10/D-11 read-only WARN surface tests
// (CTRL-05, T-23-18): `villa install`'s memory readiness flow WARNs (with
// remediation) on a CONFIDENT embedding model/dim mismatch between the recall-state
// stamp and the configured identity — and does NOTHING else: never a block, never
// an exit-code change, never a state write, never an auto-reindex. The comparison
// is the single Plan 23-01 helper (recall.EmbeddingSkew); the state read goes
// through the injectable readRecallState seam so these tests stay hermetic.

// TestInstallMemorySkewWarn drives runInstall through the memory-on fixture with a
// controllable recall-state seam and asserts the WARN matrix: confident mismatch ⇒
// one WARN line with remediation, everything else (empty stamp, matching stamp,
// unreadable state, memory off) ⇒ silence. Exit codes and the memory proof flow
// are unchanged in every case (read-only, D-11).
func TestInstallMemorySkewWarn(t *testing.T) {
	stamped := recall.State{
		KnowledgeID:    "kb1",
		EmbeddingModel: "old-embed-model",
		EmbeddingDim:   512,
	}

	t.Run("confident mismatch WARNs with remediation, read-only, exit unchanged", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		reads := 0
		f.installDeps.readRecallState = func() (recall.State, error) {
			reads++
			return stamped, nil
		}

		cmd, _, errOut := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("skew WARN must NEVER block: exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		msg := errOut.String()
		for _, want := range []string{
			"WARN",
			"old-embed-model", "512", // the stamped identity
			"nomic-embed-text-v1.5", "768", // the configured identity (DefaultVillaConfig)
			"villa recall index --rebuild", // the sanctioned re-index
			"revert",                       // ...or revert embedding_model/embedding_dim
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("install skew WARN must contain %q; stderr = %q", want, msg)
			}
		}
		// Read-only (D-11): exactly one state read, the proof still ran, and no extra
		// mutation fired (the seam surface offers no recall-state writer at all —
		// saveConfig's single call is install's own config persist, unrelated).
		if reads != 1 {
			t.Errorf("readRecallState calls = %d, want exactly 1", reads)
		}
		if f.memoryProofCalls != 1 {
			t.Errorf("the WARN must not displace the readiness proof, proof calls = %d", f.memoryProofCalls)
		}
	})

	t.Run("empty stamp is typed-Unknown - silent", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.installDeps.readRecallState = func() (recall.State, error) {
			return recall.State{}, nil // nothing recorded (fresh install / pre-stamp store)
		}

		cmd, _, errOut := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if strings.Contains(errOut.String(), "recall index") {
			t.Errorf("an empty stamp must raise no alarm (typed-Unknown); stderr = %q", errOut.String())
		}
	})

	t.Run("matching stamp is silent", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.installDeps.readRecallState = func() (recall.State, error) {
			return recall.State{EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDim: 768}, nil
		}

		cmd, _, errOut := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if strings.Contains(errOut.String(), "recall index") {
			t.Errorf("a matching stamp must print nothing; stderr = %q", errOut.String())
		}
	})

	t.Run("unreadable state is typed-Unknown - silent, never a block", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.installDeps.readRecallState = func() (recall.State, error) {
			return recall.State{}, errors.New("permission denied")
		}

		cmd, _, errOut := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("an unevaluable state read must never change the exit: %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if strings.Contains(errOut.String(), "recall index") {
			t.Errorf("an unevaluable read must raise no alarm (typed-Unknown); stderr = %q", errOut.String())
		}
	})

	t.Run("memory off never reads the recall state", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = false
		reads := 0
		f.installDeps.readRecallState = func() (recall.State, error) {
			reads++
			return stamped, nil
		}

		cmd, _, _ := installTestCmd()
		_ = runInstall(cmd, installOpts{}, f.installDeps)
		if reads != 0 {
			t.Errorf("the skew WARN is memory-on only; readRecallState calls = %d, want 0", reads)
		}
	})

	t.Run("nil seam is safe - no panic, silent", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		// readRecallState left nil (the test-double default): the WARN helper must
		// degrade silently, mirroring the doctor optional-seam pattern.

		cmd, _, errOut := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if strings.Contains(errOut.String(), "recall index") {
			t.Errorf("a nil seam must be silent; stderr = %q", errOut.String())
		}
	})
}

// TestExtractExitCode anchors the WR-01 load-bearing exit-code mapping that runProbeCurlCode
// relies on to tell a genuine block (curl CONNECTION/TIMEOUT exit 6/7/28) from "the probe
// could not run" (-1). The classifier (classifyEgressProbe) is exhaustively tested with
// SYNTHETIC codes; this test anchors the extraction itself against the REAL os/exec runtime so
// a future Go/exec behavior drift in the *exec.ExitError → ExitCode() path (or the never-started
// non-ExitError path) is caught here rather than silently miscategorizing a timeout as a block
// (or vice-versa) at runtime on the host. It uses a real fixed-arg command, never a shell of the
// production helper, so it stays hermetic and off-hardware (no podman/curl needed).
func TestExtractExitCode(t *testing.T) {
	t.Run("process ran and exited non-zero → its real exit code (anchors errors.As/ExitCode)", func(t *testing.T) {
		// A real process that exits with a known non-zero status (mirrors curl 6/7/28 — a genuine
		// connection/timeout block surfacing as the container process's exit code).
		runErr := exec.Command("sh", "-c", "exit 7").Run()
		if runErr == nil {
			t.Fatalf("expected a non-nil run error for `sh -c 'exit 7'`")
		}
		if got := extractExitCode(runErr); got != 7 {
			t.Errorf("extractExitCode(exit-7 error) = %d, want 7", got)
		}
	})

	t.Run("binary never started → -1, NEVER a curl exit value (infra, never a block)", func(t *testing.T) {
		// A binary that does not exist never produces a process exit code; cmd.Run returns a
		// non-*exec.ExitError (an *exec.Error / *fs.PathError), exactly the never-started case
		// (podman missing / daemon error) the classifier must read as infrastructure, not a block.
		runErr := exec.Command("villa-no-such-binary-extractexitcode-xyzzy").Run()
		if runErr == nil {
			t.Fatalf("expected a non-nil run error for a nonexistent binary")
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			t.Fatalf("a nonexistent binary must NOT surface as *exec.ExitError; got %T", runErr)
		}
		if got := extractExitCode(runErr); got != -1 {
			t.Errorf("extractExitCode(never-started error) = %d, want -1", got)
		}
	})
}
