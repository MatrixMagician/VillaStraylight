package main

import (
	"context"
	"fmt"

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

// liveAgentVerify is the production runtime strictly-local agent proof seam (on-hardware by
// nature: it needs the live villa-llama + crush binary + a host-egress precondition supplied
// by the verification wave). It mirrors liveRagSmoke's four-layer shape, composing the
// negative-control egress probe with the host-side crush-run drivers, then calls the pure
// evalAgentVerify.
//
//   - egressBlocked: a negative-control external probe via the EXISTING runProbeCurl (fixed-
//     arg `podman run --rm --network villa --entrypoint curl <helperImage> curl -sf
//     --max-time 5 https://huggingface.co/`). The helper image is sourced from
//     orchestrate.EmbedImage() — NO re-typed image literal (T-27-15, TestSeamGrepGate green).
//     `blocked := err != nil`: a REACHABLE external host means egress is NOT blocked.
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
		_, err := runProbeCurl(ctx, helperImage, "-sf", "--max-time", "5", egressNegativeControlHost)
		// A reachable external host (err == nil) means egress is NOT blocked → blocked=false.
		// An unreachable host (err != nil) is the EXPECTED, desired outcome → blocked=true.
		return err != nil, nil
	}

	// The crush-run read→edit round-trip; reused verbatim for both controls (DRY).
	task := deps.agentTaskFn(ctx)
	agentTask := func() (bool, error) { return task() }

	llamaDownTask := func() (bool, error) {
		// Stop villa-llama, run the SAME task, then ALWAYS restore (deferred Start). The
		// restore runs regardless of the task outcome so the service is never left stopped.
		if err := deps.systemd.Stop(installServiceName); err != nil {
			// Could not even stop the service — we cannot run a meaningful control. Treat as
			// not-answered (no false cloud-fallback claim); the stop error is surfaced by the
			// service-restore inspection on-hardware (Plan 04).
			return false, fmt.Errorf("stop %s: %w", installServiceName, err)
		}
		defer func() { _ = deps.systemd.Start(installServiceName) }()
		answered, _ := task()
		return answered, nil
	}

	return evalAgentVerify(egressBlocked, agentTask, llamaDownTask)
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
