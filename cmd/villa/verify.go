package main

// verify.go is the thin cobra caller for the `villa verify` verb tree and its first
// subcommand `villa verify memory` — the runtime firewalled zero-outbound document-upload
// RAG smoke proof (D-10/D-11/PRIV-05, SC#4). The decision logic (negative-control-first
// PASS/FAIL) lives in the pure evalRagSmoke core (verify_memory.go); this file keeps ONLY
// the cobra wiring + exit-code mapping (reusing the AUTHORITATIVE preflight constants —
// exitPass/exitWarn/exitBlocked), the printing, and the live host wiring (liveRagSmoke +
// the config seams).
//
// ON-HARDWARE BY NATURE (D-10): unlike the pure core, this verb needs the LIVE OWUI
// container reachable over its loopback PublishPort AND a host-egress precondition (the
// public-internet outbound blocked for the duration) supplied by the verification wave —
// the negative-control probe proves that block is real. It is therefore a dedicated verb
// rather than a per-install gate (RESEARCH §"Wiring").
//
// It is GATED on the PERSISTED memory_enabled (config.LoadVilla().MemoryEnabled). When
// memory is OFF there is nothing to verify and the command exits 0 with a clear message —
// this is NOT the silent-skip hazard (the hazard is skipping the proof while memory IS on;
// that path runs the proof and refuses-with-remediation on FAIL, never a false-green).

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// verifyMemoryLoopbackAddr is the host side of the OWUI PublishPort (127.0.0.1:<chat_port>,
// openWebUIPublishPort = "127.0.0.1:3000:8080"). The RAG drive reaches OWUI over this
// EXISTING loopback bind — no new host port is opened (D-11). It is a loopback constant,
// not a backend/image literal; the port itself is config-resolved (cfg.ChatPort).
const verifyMemoryLoopbackAddr = "127.0.0.1"

// verifyMemoryDeps are the injectable host seams for `villa verify memory`, so the run path
// is testable off-hardware (mirrors doctor.Deps / the install memoryProofFn seam). The live
// wiring is liveVerifyMemoryDeps.
type verifyMemoryDeps struct {
	// loadedMemoryEnabled is the AUTHORITATIVE memory gate source — the PERSISTED
	// config.LoadVilla().MemoryEnabled (live: liveLoadedMemoryEnabled, failing soft to
	// false so a broken config never silently claims memory is on). Reused from install.
	loadedMemoryEnabled func() bool
	// loadedConfig resolves the loopback OWUI port + the question/fact (live:
	// liveLoadedConfig). The planted doc/fact + question are the on-hardware seed the
	// verification wave supplies; here they are resolved-but-overridable defaults.
	loadedConfig func() config.VillaConfig
	// ragSmokeFn drives the runtime RAG smoke proof (live: liveRagSmoke). Injecting it
	// makes the gated run path unit-testable without a live container or host egress.
	ragSmokeFn func(ctx context.Context, in ragSmokeInput) memoryProof
}

// liveVerifyMemoryDeps wires verifyMemoryDeps to the real host: the persisted memory gate,
// the persisted config, and the production liveRagSmoke seam.
func liveVerifyMemoryDeps() verifyMemoryDeps {
	return verifyMemoryDeps{
		loadedMemoryEnabled: liveLoadedMemoryEnabled,
		loadedConfig:        liveLoadedConfig,
		ragSmokeFn:          liveRagSmoke,
	}
}

// newVerify builds the `villa verify` parent command — validate a RUNNING villa stack.
func newVerify() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Validate a running villa stack (runtime proofs)",
		Long: "Run runtime proofs against the RUNNING stack. Subcommands drive real workloads " +
			"and assert honest, offload-/egress-proven outcomes (never a false-green).",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newVerifyMemory())
	return cmd
}

// newVerifyMemory builds `villa verify memory`: the runtime firewalled zero-outbound
// document-upload RAG smoke proof (D-10/D-11/PRIV-05). It is gated on the persisted
// memory_enabled and refuses-with-remediation (exitBlocked) on FAIL. The exit-code mapping
// lives ENTIRELY in runVerifyMemory (return-not-Exit body; cobra RunE calls os.Exit).
func newVerifyMemory() *cobra.Command {
	return &cobra.Command{
		Use:   "memory",
		Short: "Prove the document-upload RAG path retrieves + cites with ZERO outbound (runtime, firewalled)",
		Long: "Drive a REAL document upload through the Open WebUI RAG path (upload → chunk → embed " +
			"via villa-embed → store in Qdrant → retrieve → cite) over the existing loopback port " +
			"(no new host port) WHILE host egress is blocked, paired with a negative-control external " +
			"probe that MUST fail. Asserting zero-outbound by absence alone is a false-green; the " +
			"negative control proves egress is actually blocked. On-hardware by nature: needs the live " +
			"OWUI container and a host-egress precondition supplied by the verification wave. Gated on " +
			"the persisted memory_enabled; exits 0 (passed, or memory off — nothing to verify) or 1 " +
			"(a blocking FAIL with remediation). Mutates nothing in config.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runVerifyMemory(cmd, args, liveVerifyMemoryDeps()))
			return nil
		},
	}
}

// runVerifyMemory gates on the persisted memory_enabled, builds the ragSmokeInput from the
// resolved config, runs the injected proof, and RETURNS the exit code (no os.Exit) so
// verify_test.go can drive it deterministically. A memory-OFF stack exits 0 (nothing to
// verify — not the silent-skip hazard). A memory-ON FAIL prints the refuse-with-remediation
// detail to stderr and returns exitBlocked; a PASS prints the proof detail and returns
// exitPass.
func runVerifyMemory(cmd *cobra.Command, _ []string, deps verifyMemoryDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	if !deps.loadedMemoryEnabled() {
		fmt.Fprintln(out, "verify memory: the memory stack is not enabled (memory_enabled=false) — nothing to verify. Enable it with `villa install` after opting in, then re-run.")
		return exitPass
	}

	cfg := deps.loadedConfig()

	// The planted question + the fact whose ONLY source is the uploaded document (so a
	// correct answer can only come from retrieval, not the base model's priors). The
	// verification wave seeds the matching doc; these defaults are the seam contract.
	const (
		ragSmokeQuestion = "What is the VillaStraylight runtime RAG smoke verification token?"
		ragSmokeWantFact = "VILLA-RAG-SMOKE-TOKEN-7741"
	)

	in := ragSmokeInput{
		owuiAddr: verifyMemoryLoopbackAddr,
		owuiPort: cfg.ChatPort,
		question: ragSmokeQuestion,
		wantFact: ragSmokeWantFact,
	}

	proof := deps.ragSmokeFn(cmd.Context(), in)
	if proof.status == preflight.StatusFail {
		fmt.Fprintf(errOut, "verify memory: runtime zero-outbound RAG proof FAILED: %s\n", proof.detail)
		return exitBlocked
	}
	fmt.Fprintf(out, "verify memory: %s\n", proof.detail)
	return exitPass
}
