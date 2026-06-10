package main

import (
	"errors"
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
