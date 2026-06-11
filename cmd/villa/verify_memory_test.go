package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// TestEvalRagSmoke table-drives the PURE runtime zero-outbound RAG-smoke core over the six
// outcomes, mirroring TestEvalMemoryProof (cmd/villa/install_test.go). The load-bearing
// invariants it locks (D-10, PRIV-05, honesty-by-construction):
//
//   - The egress negative-control is asserted FIRST: a probe that could not RUN (err) is a
//     FAIL ("refusing to declare zero-outbound"), and an external host that WAS reachable
//     (blocked == false) is a FAIL ("egress is NOT blocked"). Neither is a silent skip.
//   - Only AFTER egress is proven blocked is the upload drive trusted: an upload error, a
//     missing planted fact, or a missing citation each → FAIL.
//   - All-good (egress blocked + fact present + cited) → PASS.
//   - There is NO WARN and NO skip path — an unevaluable result is a FAIL.
//   - A separate spy test proves uploadCite is NEVER invoked when the negative control
//     fails (false-green prevention: the drive must not run if egress isn't proven blocked).
func TestEvalRagSmoke(t *testing.T) {
	const wantFact = "the moon-base reactor serial is QX-7741"

	cases := []struct {
		name string
		// negative control
		blocked    bool
		egressErr  error
		// upload drive
		answer     string
		cited      bool
		uploadErr  error
		wantStatus preflight.Status
	}{
		{
			name:       "negative-control probe error",
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
			name:       "upload error",
			blocked:    true,
			uploadErr:  errors.New("knowledge/create 500"),
			wantStatus: preflight.StatusFail,
		},
		{
			name:       "fact missing from answer",
			blocked:    true,
			answer:     "I could not find that information in the documents.",
			cited:      true,
			wantStatus: preflight.StatusFail,
		},
		{
			name:       "answer not cited",
			blocked:    true,
			answer:     "the moon-base reactor serial is QX-7741",
			cited:      false,
			wantStatus: preflight.StatusFail,
		},
		{
			name:       "all good (blocked + fact + cited)",
			blocked:    true,
			answer:     "According to the upload, the moon-base reactor serial is QX-7741.",
			cited:      true,
			wantStatus: preflight.StatusPass,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			egressBlocked := func() (bool, error) { return tc.blocked, tc.egressErr }
			uploadCite := func() (string, bool, error) { return tc.answer, tc.cited, tc.uploadErr }
			got := evalRagSmoke(egressBlocked, uploadCite, wantFact)
			if got.status != tc.wantStatus {
				t.Errorf("status = %v, want %v (detail %q)", got.status, tc.wantStatus, got.detail)
			}
			if tc.wantStatus == preflight.StatusFail && got.detail == "" {
				t.Errorf("a FAIL verdict must carry a refuse-with-remediation detail")
			}
		})
	}
}

// TestEvalRagSmokeNegativeControlFirst is the false-green guard: when the egress
// negative-control fails (either the probe errored OR an external host was reachable), the
// upload drive MUST NOT run. If it did, a host that never actually blocked egress could
// still produce a green by completing the RAG path — exactly the false-green PRIV-05
// forbids. A spy records whether uploadCite was invoked.
func TestEvalRagSmokeNegativeControlFirst(t *testing.T) {
	const wantFact = "fact"

	t.Run("probe error short-circuits before upload", func(t *testing.T) {
		uploadRan := false
		egressBlocked := func() (bool, error) { return false, errors.New("probe could not run") }
		uploadCite := func() (string, bool, error) { uploadRan = true; return wantFact, true, nil }
		got := evalRagSmoke(egressBlocked, uploadCite, wantFact)
		if got.status != preflight.StatusFail {
			t.Fatalf("status = %v, want FAIL when the negative control could not run", got.status)
		}
		if uploadRan {
			t.Fatalf("uploadCite ran despite a failed negative control — false-green hazard")
		}
	})

	t.Run("egress reachable short-circuits before upload", func(t *testing.T) {
		uploadRan := false
		egressBlocked := func() (bool, error) { return false, nil } // external host WAS reachable
		uploadCite := func() (string, bool, error) { uploadRan = true; return wantFact, true, nil }
		got := evalRagSmoke(egressBlocked, uploadCite, wantFact)
		if got.status != preflight.StatusFail {
			t.Fatalf("status = %v, want FAIL when egress is not blocked", got.status)
		}
		if uploadRan {
			t.Fatalf("uploadCite ran despite egress not being blocked — false-green hazard")
		}
	})
}

// TestRunVerifyMemoryGate drives runVerifyMemory over the injectable seam to lock the three
// load-bearing behaviours: (1) memory OFF exits 0 without ever running the proof (nothing to
// verify — NOT the silent-skip hazard); (2) memory ON + a FAIL verdict returns exitBlocked
// with the remediation detail on stderr; (3) memory ON + a PASS returns exitPass. The proof
// seam is injected so no live container or host egress is needed (off-hardware).
func TestRunVerifyMemoryGate(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{}
		c.SetContext(context.Background())
		return c
	}

	t.Run("memory off exits 0 without running the proof", func(t *testing.T) {
		proofRan := false
		deps := verifyMemoryDeps{
			loadedMemoryEnabled: func() bool { return false },
			loadedConfig:        func() config.VillaConfig { return config.DefaultVillaConfig() },
			ragSmokeFn: func(context.Context, ragSmokeInput) memoryProof {
				proofRan = true
				return memoryProof{status: preflight.StatusPass}
			},
		}
		cmd := newCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if code := runVerifyMemory(cmd, nil, deps); code != exitPass {
			t.Errorf("memory-off exit = %d, want exitPass (%d)", code, exitPass)
		}
		if proofRan {
			t.Errorf("the proof must NOT run when memory is off")
		}
	})

	t.Run("memory on FAIL returns exitBlocked with remediation", func(t *testing.T) {
		deps := verifyMemoryDeps{
			loadedMemoryEnabled: func() bool { return true },
			loadedConfig:        func() config.VillaConfig { return config.DefaultVillaConfig() },
			ragSmokeFn: func(_ context.Context, in ragSmokeInput) memoryProof {
				// The drive must target the loopback PublishPort (no new host port, D-11).
				if in.owuiAddr != verifyMemoryLoopbackAddr {
					t.Errorf("owuiAddr = %q, want loopback %q", in.owuiAddr, verifyMemoryLoopbackAddr)
				}
				return memoryProof{status: preflight.StatusFail, detail: "egress is NOT blocked"}
			},
		}
		cmd := newCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runVerifyMemory(cmd, nil, deps); code != exitBlocked {
			t.Errorf("memory-on FAIL exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		if errOut.Len() == 0 {
			t.Errorf("a FAIL must print a remediation to stderr")
		}
	})

	t.Run("memory on PASS returns exitPass", func(t *testing.T) {
		deps := verifyMemoryDeps{
			loadedMemoryEnabled: func() bool { return true },
			loadedConfig:        func() config.VillaConfig { return config.DefaultVillaConfig() },
			ragSmokeFn: func(context.Context, ragSmokeInput) memoryProof {
				return memoryProof{status: preflight.StatusPass, detail: "document upload retrieved + cited with zero outbound"}
			},
		}
		cmd := newCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if code := runVerifyMemory(cmd, nil, deps); code != exitPass {
			t.Errorf("memory-on PASS exit = %d, want exitPass (%d)", code, exitPass)
		}
	})
}
