package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// verify_agent.go holds the v1.4 CODING-AGENT RUNTIME strictly-local proof — the headline
// PRIV-06 honesty gate. Install-time green is NOT sufficient: a coding agent could phone home
// at runtime (telemetry / outbound tools) OR silently resolve to a cloud provider when the
// local inference endpoint is down. `villa verify agent` proves BOTH clauses of PRIV-06 in
// one verb, negative-control-FIRST:
//
//  1. ctrl1 — an external host MUST be UNREACHABLE under the host egress block (proving the
//     block is real, not merely unused), and ONLY THEN is the real agent task (a `crush run`
//     tool-call read→edit round-trip over the loopback inference endpoint) required to
//     complete WHILE egress is blocked. Asserting zero-outbound by ABSENCE alone is a false-
//     green; the negative control proves egress is actually blocked (honesty-by-construction).
//  2. ctrl2 — with `villa-llama` STOPPED, the SAME agent task MUST FAIL. An answer with the
//     local model down is the smoking gun for a silent cloud-model fallback and FAILS
//     verification. This control is folded into the SAME verb so it runs routinely, not as a
//     manual drill.
//
// It mirrors verify_memory.go's four-layer seam EXACTLY:
//
//  1. Verdict type — reuses memoryProof ({status preflight.Status; detail string}); PASS/FAIL
//     only, no WARN — an unevaluable result (timeout) is a FAIL, never a silent skip.
//  2. Pure core — evalAgentVerify maps the (egressBlocked, agentTask, llamaDownTask) outcomes
//     to a verdict, asserting the negative control FIRST (unit-testable off-hardware).
//  3. Live seam — liveAgentVerify composes the egress negative-control probe + the host-side
//     crush-run drivers (the llama-down one stops/restores villa-llama via the systemd seam)
//     and calls evalAgentVerify.
//  4. Fixed-arg exec — reuses runProbeCurl (install_memory.go) for the negative control; the
//     agent task reuses the Plan-01 liveAgentToolCallProbe host-binary driver. No shell, no
//     re-typed image literal (the helper image comes ONLY from orchestrate.EmbedImage()).

// evalAgentVerify is the PURE runtime strictly-local agent proof core (unit-testable off-
// hardware via the three injected probes): it maps the negative-control egress outcome, the
// real agent task outcome, and the llama-down control outcome to a PASS/FAIL verdict. There
// is NO WARN and NO skip path — an unevaluable result (a context-bounded driver returning an
// error / a timeout) is a FAIL (honesty-by-construction, mirrors evalRagSmoke).
//
// Negative-control FIRST (PRIV-06): asserting zero-outbound by absence alone is a false-
// green, so egress must be proven actually blocked BEFORE the agent task is even trusted —
// agentTask is not invoked until the negative control passes. A probe that could not RUN
// (err) → FAIL refusing to declare zero-outbound; an external host that WAS reachable
// (blocked == false) → FAIL that egress is not blocked.
//
// Only after egress is proven blocked is agentTask invoked: an error or a task that did not
// complete → FAIL the agent task did not run under the block. Finally the llama-down control:
// if the agent ANSWERED with villa-llama stopped (answered == true), that is the silent
// cloud-fallback smoking gun → FAIL; an error from llamaDownTask is the EXPECTED inference-
// down outcome and is fine (it is what we WANT — the task should fail when the local model is
// down). All controls passing-as-designed → PASS. Every FAIL carries a refuse-with-
// remediation detail.
func evalAgentVerify(
	egressBlocked func() (bool, error),
	agentTask func() (completed bool, err error),
	llamaDownTask func() (answered bool, err error),
) memoryProof {
	// 1) Negative control FIRST — egress must be proven blocked before the agent is trusted.
	blocked, err := egressBlocked()
	if err != nil {
		return memoryProof{
			status: preflight.StatusFail,
			detail: fmt.Sprintf("could not run the egress negative-control probe (%v) — refusing to declare zero-outbound; verify the %q network and a reachable helper image, then re-run `villa verify agent`", err, memoryProofNetwork),
		}
	}
	if !blocked {
		return memoryProof{
			status: preflight.StatusFail,
			detail: "egress is NOT blocked: an external host was reachable during the test — block the host's outbound to the public internet for the duration, then re-run `villa verify agent`",
		}
	}

	// 2) Real agent task — only reached once egress is proven blocked.
	completed, err := agentTask()
	if err != nil || !completed {
		return memoryProof{
			status: preflight.StatusFail,
			detail: fmt.Sprintf("the agent task did not complete under the egress block (completed=%t, err=%v) — check `systemctl --user status %s` and the rendered crush.json loopback endpoint, then re-run `villa verify agent`", completed, err, installServiceName),
		}
	}

	// 3) Llama-down control — with villa-llama stopped, the SAME task MUST fail. An ANSWER is
	// the silent cloud-fallback smoking gun. An error here is the EXPECTED inference-down
	// outcome (the task SHOULD fail with the local model down) and is intentionally ignored.
	answered, _ := llamaDownTask()
	if answered {
		return memoryProof{
			status: preflight.StatusFail,
			detail: "the agent ANSWERED with villa-llama stopped — silent cloud-model fallback detected; FAILS verification. Remove any cloud-provider credentials/config from the agent's environment and the rendered crush.json, then re-run `villa verify agent`",
		}
	}

	return memoryProof{status: preflight.StatusPass, detail: "zero-outbound agent task completed; no cloud fallback (llama-down control failed as expected)"}
}

// curl exit codes that mean the external host was genuinely UNREACHABLE (a real egress block),
// as opposed to an infrastructure failure of the probe itself: 6 could-not-resolve-host,
// 7 failed-to-connect, 28 operation-timeout. Any OTHER non-zero exit (e.g. 127 curl-absent) or
// a container that never started is "the probe could not run", NOT "the host was blocked".
const (
	curlExitCouldNotResolve  = 6
	curlExitFailedToConnect  = 7
	curlExitOperationTimeout = 28
)

// classifyEgressProbe is the PURE WR-01 classifier the live egressBlocked closure delegates
// its verdict math to (the live podman/curl exec is not driveable off-hardware; this is). It
// distinguishes a genuinely-blocked external host from a probe that could not RUN — the core
// honesty fix for the negative-control-FIRST gate.
//
//   - sanityErr != nil: the in-network sanity probe to villa-llama failed, so the probe
//     environment is wholesale-broken (missing image/network, podman daemon error, curl
//     absent, villa-llama down). Return an ERROR (the pure evalAgentVerify maps it to FAIL
//     "could not run the egress negative-control probe"). NEVER blocked=true — that is exactly
//     the false-green this control exists to forbid.
//   - sanity ok + a curl CONNECTION/TIMEOUT exit (6/7/28): the external host is genuinely
//     unreachable → blocked=true (the desired, proven-blocked outcome).
//   - sanity ok + exit 0 (externalErr == nil): the external host was reachable → blocked=false
//     (egress is NOT blocked; evalAgentVerify FAILs with the unblocked remediation).
//   - sanity ok + an UNCLASSIFIED non-zero exit or a never-started container (externalErr !=
//     nil but the code is not 6/7/28): an infrastructure failure → ERROR, NOT blocked=true.
func classifyEgressProbe(sanityErr error, externalExitCode int, externalErr error) (blocked bool, err error) {
	return classifyReachabilityProbe(sanityErr, externalExitCode, externalErr,
		func(e error) error {
			return fmt.Errorf("egress negative-control probe environment is broken: the in-network sanity probe to %s failed (%w) — verify the %q network, a reachable helper image, and that villa-llama is up, then re-run", orchestrate.LlamaInNetworkEndpoint(), e, memoryProofNetwork)
		},
		func(code int, e error) error {
			return fmt.Errorf("egress negative-control external probe could not run (exit %d: %w) — this is NOT proof of a block; verify the helper image has curl and podman can reach %q, then re-run", code, e, egressNegativeControlHost)
		})
}

// classifyReachabilityProbe is the SINGLE source of truth for the curl-exit reachability
// taxonomy shared by classifyEgressProbe (verify agent) and classifySearchProbe (verify
// search): a broken sanity/positive control → error (never blocked=true — the false-green
// this forbids); exit 0 → reachable (blocked=false); a genuine connection/timeout exit
// (6/7/28) → blocked=true; any OTHER non-zero exit or never-started container → "could not
// run" error. Keeping the exit-code SET in one place means the two verify paths cannot drift
// on which curl codes count as a proven block. Callers supply their own remediation wording
// (broken/cantRun) — the only thing that legitimately differs between the two proofs.
func classifyReachabilityProbe(sanityErr error, externalExitCode int, externalErr error, broken func(error) error, cantRun func(int, error) error) (blocked bool, err error) {
	if sanityErr != nil {
		return false, broken(sanityErr)
	}
	if externalErr == nil {
		return false, nil
	}
	switch externalExitCode {
	case curlExitCouldNotResolve, curlExitFailedToConnect, curlExitOperationTimeout:
		return true, nil
	default:
		return false, cantRun(externalExitCode, externalErr)
	}
}

// liveAgentVerify is the production runtime strictly-local agent proof seam (on-hardware by
// nature: it needs the live villa-llama + crush binary + a host-egress precondition supplied
// by the verification wave). It mirrors liveRagSmoke's four-layer shape, composing the
// negative-control egress probe with the host-side crush-run drivers, then calls the pure
// evalAgentVerify.
//
//   - egressBlocked: a TWO-LAYER negative control (WR-01). FIRST a positive in-network sanity
//     probe to villa-llama by container DNS (orchestrate.LlamaInNetworkEndpoint()) proves the
//     probe environment works; if it fails the control FAILs ("could not run the probe"),
//     never false-greens as blocked. THEN a negative external probe (runProbeCurlCode) is
//     classified by curl exit code via classifyEgressProbe: a CONNECTION/TIMEOUT exit (6/7/28)
//     → genuinely blocked; exit 0 → not blocked; any other failure → infrastructure FAIL. The
//     helper image is sourced from orchestrate.EmbedImage() and the in-network URL from
//     orchestrate.LlamaInNetworkEndpoint() — NO re-typed image/host literal (T-27-15,
//     TestSeamGrepGate green).
//
//   - agentTask: the Plan-01 host-side crush-run read→edit round-trip driver
//     (liveAgentToolCallProbe) over the loopback inference endpoint, bounded by ctx (a
//     timeout → err → FAIL, never a hang masquerading as success). DRY — the same planted-
//     file mechanism the install readiness probe uses.
//
//   - llamaDownTask: STOPS villa-llama.service via the injected systemd Stop seam, runs the
//     SAME crush-run task, then RESTORES the service (Start) in a deferred restore regardless
//     of outcome (T-27-16 — the service is never left stopped), and reports whether the agent
//     answered. An answer with inference down is the cloud-fallback smoking gun.
func liveAgentVerify(ctx context.Context, deps verifyAgentDeps) memoryProof {
	helperImage := orchestrate.EmbedImage()

	egressBlocked := func() (bool, error) {
		// WR-01: the negative control is the FIRST gate, so a BROKEN probe environment must
		// FAIL the control, never false-green as "blocked". Two layers, the in-network sanity
		// probe being the primary robust mechanism:
		//
		// (1) Positive in-network sanity probe FIRST. Curl villa-llama by its CONTAINER DNS
		//     name + server port (orchestrate.LlamaInNetworkEndpoint()), INSIDE the same villa
		//     network the negative probe uses. CRITICAL: runProbeCurl runs in a --network villa
		//     helper container where 127.0.0.1 is the HELPER's OWN loopback — so the agent
		//     task's host-loopback providerBaseURL would ALWAYS fail here and false-FAIL the
		//     control on every healthy host. The sanity probe proves podman runs, the villa
		//     network exists, the helper image is present, curl works in it, AND villa-llama is
		//     reachable by DNS. If it fails, the probe environment is wholesale-broken → error
		//     (the pure core maps it to FAIL "could not run the probe"), NOT blocked=true.
		_, sanityErr := runProbeCurl(ctx, helperImage, "-sf", "--max-time", "5",
			orchestrate.LlamaInNetworkEndpoint()+"/models")

		// (2) Negative external probe, classified by curl exit semantics. A genuinely-blocked
		//     host fails with a curl CONNECTION/TIMEOUT code (6/7/28) → blocked=true; any other
		//     failure mode (container never started, curl absent, unclassified non-zero) is an
		//     infrastructure failure → error (→ FAIL). Exit 0 (reachable) → blocked=false.
		//
		//     WR-02: this probe deliberately OMITS curl's -f (fail-on-HTTP-error). The negative
		//     control's question is reachability, not HTTP status: a host that answers with a
		//     4xx/5xx (rate-limit, captcha, transient outage) is demonstrably REACHABLE, so egress
		//     is OPEN and the gate MUST fail as "not blocked". With -f, that response would be
		//     curl exit 22 → the classifier's default (infra-failure) branch, excusing an open-
		//     egress host as a probe problem (a false-negative on the PRIV-06 security assertion).
		//     Dropping -f makes ANY HTTP response (including 4xx/5xx) return exit 0 ⇒ blocked=false,
		//     so a reachable-but-erroring host correctly reads as egress-open. The fail-closed
		//     property is preserved: a true connection/timeout still exits 6/7/28 → blocked=true,
		//     and a broken probe environment still surfaces via sanityErr / an unclassified exit.
		_, externalExit, externalErr := runProbeCurlCode(ctx, helperImage, "-s", "--max-time", "5", egressNegativeControlHost)

		return classifyEgressProbe(sanityErr, externalExit, externalErr)
	}

	// The crush-run read→edit round-trip; reused verbatim for both controls (DRY).
	task := deps.agentTaskFn(ctx)
	agentTask := func() (bool, error) { return task() }

	// WR-06: capture the restore (Start) error so a failed restore is SURFACED, never swallowed
	// (T-27-16 "never left stopped"). The deferred Start inside runLlamaDownControl always
	// ATTEMPTS the restore; its error is recorded here and folded into the verdict below.
	var restoreErr error
	llamaDownTask := func() (bool, error) {
		answered, rerr := runLlamaDownControl(
			func() error { return deps.systemd.Stop(installServiceName) },
			func() error { return deps.systemd.Start(installServiceName) },
			task,
		)
		restoreErr = rerr
		return answered, nil
	}

	proof := evalAgentVerify(egressBlocked, agentTask, llamaDownTask)

	// WR-06: a restore failure means the verb that deliberately stopped a core service did NOT
	// cleanly pass — villa-llama may be left down. DOWNGRADE a would-be PASS to FAIL and surface
	// the manual remediation; if the proof already FAILed, append the warning so the operator
	// still hears that the service may be stopped.
	if restoreErr != nil {
		warning := restoreLlamaWarning(installServiceName, restoreErr)
		if proof.status == preflight.StatusPass {
			return memoryProof{status: preflight.StatusFail, detail: warning}
		}
		return memoryProof{status: proof.status, detail: proof.detail + " " + warning}
	}
	return proof
}

// runLlamaDownControl runs the llama-down negative control as a PURE orchestration over
// injected seams (so the restore-failure path is unit-testable off-hardware): STOP villa-llama,
// run the SAME crush-run task, then ALWAYS attempt to restore (deferred Start) regardless of
// the task outcome — and SURFACE the restore (Start) error instead of swallowing it (WR-06 /
// T-27-16). It reports whether the agent ANSWERED with inference down (the cloud-fallback
// smoking gun) and the captured restore error.
//
// A stop failure short-circuits: if the service could not even be stopped there is no
// meaningful control to run and nothing to restore (the service was never stopped), so the
// task is not run and no restore is attempted (answered=false, restoreErr=nil).
func runLlamaDownControl(stop, start func() error, task func() (bool, error)) (answered bool, restoreErr error) {
	if err := stop(); err != nil {
		// Could not stop the service — cannot run a meaningful control, and there is nothing to
		// restore. No false cloud-fallback claim (answered=false); no restore error to surface.
		return false, nil
	}
	// Restore is ATTEMPTED on every path (deferred), and its error is CAPTURED (never dropped).
	defer func() { restoreErr = start() }()
	answered, _ = task()
	return answered, restoreErr
}

// restoreLlamaWarning builds the WR-06 operator-facing warning for a FAILED villa-llama restore
// after the llama-down control. A nil error yields no warning (the clean path is silent). A
// non-nil error yields a message that names the service was left stopped and carries the
// LITERAL manual remediation `systemctl --user start <service>` so the operator can recover by
// hand — the verb that deliberately stopped a core service must never leave it down silently.
func restoreLlamaWarning(service string, rerr error) string {
	if rerr == nil {
		return ""
	}
	return fmt.Sprintf("WARNING: failed to restore %s after the llama-down control (%v) — villa-llama may be left STOPPED; run `systemctl --user start %s` to restore it.", service, rerr, service)
}

// verifyAgentDeps are the injectable host seams for `villa verify agent`, so the run path is
// testable off-hardware (mirrors verifyMemoryDeps). The live wiring is liveVerifyAgentDeps.
type verifyAgentDeps struct {
	// loadedAgentEnabled is the AUTHORITATIVE coding-agent gate source — the PERSISTED
	// config.LoadVilla().AgentEnabled (live: liveLoadedAgentEnabled, failing soft to false so
	// a broken config never silently claims the agent is on). Reused from install.
	loadedAgentEnabled func() bool
	// agentTaskFn builds the host-side crush-run read→edit round-trip driver (live:
	// liveAgentToolCallProbe). Injecting it makes the gated run path unit-testable without a
	// live crush binary or inference endpoint.
	agentTaskFn func(ctx context.Context) func() (bool, error)
	// systemd is the user-manager lifecycle seam used by the llama-down control to stop and
	// restore villa-llama.service (live: orchestrate.NewSystemd()).
	systemd orchestrate.Systemd
	// verifyFn drives the runtime strictly-local agent proof (live: liveAgentVerify). Injecting
	// it makes the gated cobra run path unit-testable without a host.
	verifyFn func(ctx context.Context, deps verifyAgentDeps) memoryProof
}

// liveVerifyAgentDeps wires verifyAgentDeps to the real host: the persisted agent gate, the
// production crush-run driver, the real systemd seam, and the production liveAgentVerify.
func liveVerifyAgentDeps() verifyAgentDeps {
	return verifyAgentDeps{
		loadedAgentEnabled: liveLoadedAgentEnabled,
		agentTaskFn:        liveAgentToolCallProbe,
		systemd:            orchestrate.NewSystemd(),
		verifyFn:           liveAgentVerify,
	}
}

// newVerifyAgent builds `villa verify agent`: the runtime strictly-local coding-agent proof
// (PRIV-06). It is gated on the persisted agent_enabled and refuses-with-remediation
// (exitBlocked) on FAIL. The exit-code mapping lives ENTIRELY in runVerifyAgent (return-not-
// Exit body; cobra RunE calls os.Exit), mirroring newVerifyMemory.
func newVerifyAgent() *cobra.Command {
	return &cobra.Command{
		Use:   "agent",
		Short: "Prove the coding agent runs with ZERO outbound and NO silent cloud fallback (runtime, firewalled)",
		Long: "Drive a REAL `crush run` tool-call round-trip (read → edit → result) over the local " +
			"loopback inference endpoint WHILE host egress is blocked, paired with a negative-control " +
			"external probe that MUST fail (proving the block is real, not merely unused). Then fold in " +
			"a llama-down control: with villa-llama STOPPED the same task MUST fail — an answer is a " +
			"silent cloud-model fallback and FAILS verification. Asserting zero-outbound by absence " +
			"alone is a false-green; the negative control runs FIRST. On-hardware by nature: needs the " +
			"live villa-llama service, the staged crush binary, and a host-egress precondition supplied " +
			"by the verification wave. Gated on the persisted agent_enabled; exits 0 (passed, or the " +
			"agent addon is off — nothing to verify) or 1 (a blocking FAIL with remediation). Mutates " +
			"nothing in config (villa-llama is stopped then restored).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runVerifyAgent(cmd, args, liveVerifyAgentDeps()))
			return nil
		},
	}
}

// runVerifyAgent gates on the persisted agent_enabled, runs the injected proof, and RETURNS
// the exit code (no os.Exit) so verify_agent_test.go can drive it deterministically. An
// agent-OFF stack exits 0 (nothing to verify — NOT the silent-skip hazard; the hazard is
// skipping the proof while the agent IS on). An agent-ON FAIL prints the refuse-with-
// remediation detail to stderr and returns exitBlocked; a PASS prints the proof detail and
// returns exitPass. It mirrors runVerifyMemory's gate + exit mapping.
func runVerifyAgent(cmd *cobra.Command, _ []string, deps verifyAgentDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	if !deps.loadedAgentEnabled() {
		fmt.Fprintln(out, "verify agent: the coding agent is not enabled (agent_enabled=false) — nothing to verify. Enable it with `villa install --coding-agent`, then re-run.")
		return exitPass
	}

	proof := deps.verifyFn(cmd.Context(), deps)
	if proof.status == preflight.StatusFail {
		fmt.Fprintf(errOut, "verify agent: runtime strictly-local agent proof FAILED: %s\n", proof.detail)
		return exitBlocked
	}
	fmt.Fprintf(out, "verify agent: %s\n", proof.detail)
	return exitPass
}
