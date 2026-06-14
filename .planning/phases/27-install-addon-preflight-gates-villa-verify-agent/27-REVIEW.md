---
phase: 27-install-addon-preflight-gates-villa-verify-agent
reviewed: 2026-06-14T00:00:00Z
depth: deep
files_reviewed: 9
files_reviewed_list:
  - cmd/villa/install.go
  - cmd/villa/install_agent.go
  - cmd/villa/install_test.go
  - cmd/villa/install_agent_test.go
  - cmd/villa/verify_agent.go
  - cmd/villa/install_memory.go
  - internal/orchestrate/endpoint.go
  - cmd/villa/verify_agent_test.go
  - internal/orchestrate/endpoint_test.go
findings:
  critical: 0
  warning: 3
  info: 4
  total: 7
status: issues_found
---

# Phase 27: Code Review Report (Gap-Closure Delta)

**Reviewed:** 2026-06-14
**Depth:** deep
**Files Reviewed:** 9
**Status:** issues_found

## Summary

This is an adversarial deep review of the phase-27 gap-closure delta since `ce5da7e`
(plans 27-05 and 27-06), which were submitted to close prior findings CR-01 (BLOCKER:
the `--coding-agent` install path served the chat model instead of the staged coder),
WR-01 (egress negative-control could false-green on a broken probe), WR-05 (readiness
probe asserted mere token presence, not a real replacement), and WR-06 (a failed
villa-llama restore was swallowed).

**Verdict on the four prior findings: all four are correctly and convincingly closed.**

- **CR-01 (serve the coder):** `runInstall` now enters coding-mode (`cfg.CodingMode`,
  `cfg.CoderModel/Quant/CoderAgentCtx`) on `--coding-agent` for a real swap-residency
  coder fit, and threads a non-nil `RenderInput.CodingMode` + the coder model file
  (`codingServedTarget`/`codingModelFile`/`codingDescriptor`) so the unit serves the
  staged coder. The served `-m`, the pre-staged shard (`coderShardFor`), and the persisted
  config all single-source from `rec.Coder`. Covered by `install_test.go:1919` (serves the
  coder) and `:1973` (chat-only off-path byte-identical). Sound.
- **WR-01 (egress honesty):** `classifyEgressProbe` distinguishes a genuinely-blocked host
  (curl 6/7/28) from an infrastructure failure (sanity-probe break, unclassified exit,
  never-started container), and a broken probe environment returns an ERROR (→ FAIL),
  never `blocked=true`. The new two-layer probe (in-network sanity FIRST via
  `orchestrate.LlamaInNetworkEndpoint()`, then external negative control) is the right
  shape. `TestClassifyEgressProbe` locks the full truth table including the cardinal
  "infra failure must never read as blocked" invariant. Sound.
- **WR-05 (real replacement):** `agentProbeReplaced` requires TOKEN_B present AND TOKEN_A
  absent; `TestAgentProbeReplaced` proves an append, a transcript echoing both tokens, and
  the unedited file all return false. Sound.
- **WR-06 (surface restore failure):** `runLlamaDownControl` uses a named-return + deferred
  `restoreErr = start()` so the Start error is captured and surfaced; `liveAgentVerify`
  downgrades a would-be PASS to FAIL and appends the manual remediation. `TestLlamaDownRestore`
  and `TestRestoreLlamaWarning` lock the behavior. Sound.

The honesty/security invariants from CLAUDE.md hold: no leaked backend/host literal in
`cmd/villa` (the new in-network endpoint is sourced from `orchestrate.LlamaInNetworkEndpoint()`
+ `inference.ServerPort()`; `TestSeamGrepGate` passes), the negative control runs FIRST and
fails-closed, the llama-down control FAILs on an answer (no silent cloud fallback), and all
exec is fixed-arg with no shell interpolation. `go build`, `go vet`, the seam gate, and the
48 changed-package tests all pass.

The findings below are residual quality/robustness issues, none of which reopen the prior
findings or constitute a correctness defect I can prove.

## Warnings

### WR-01: `runProbeCurlCode` exit-code extraction is untested on the honesty-critical path

**File:** `cmd/villa/install_memory.go:384-415`
**Issue:** `runProbeCurlCode` is the new exec helper whose entire reason to exist (WR-01) is
to surface curl's numeric exit code so `classifyEgressProbe` can tell a real block (6/7/28)
from "the probe could not run". The classifier is well tested with *synthetic* exit codes,
but the extraction itself — `errors.As(runErr, &exitErr)` returning curl's code vs. the `-1`
never-started fallback — has no test. The load-bearing assumptions (podman propagates the
container process's curl exit code unchanged; a context-cancel/timeout surfaces as a
non-`*exec.ExitError` or an ExitError with code `-1`) are exactly the kind of host-runtime
behavior that can drift and silently miscategorize a timeout as a block or vice-versa,
defeating the negative control. The whole WR-01 honesty fix rests on this mapping being
correct on the real host.
**Fix:** Add a focused test for `runProbeCurlCode` against a stubbed/fixed-arg command that
exits with a known non-zero code (e.g. run `sh -c 'exit 7'` or a tiny helper binary) to
assert `exitCode == 7, err != nil`, plus a never-exists-binary case to assert `exitCode == -1`.
Even one real-exec case anchors the `errors.As` extraction so a future podman/exec change is
caught here rather than producing a false-green at runtime.

### WR-02: Egress external probe uses `-f`, so a reachable-but-erroring host is classified as infra-failure, not "not blocked"

**File:** `cmd/villa/verify_agent.go:199` (and the shared `egressNegativeControlHost`)
**Issue:** The external negative control runs `curl -sf --max-time 5 https://huggingface.co/`.
With `-f` (fail on HTTP >= 400), if egress is actually OPEN but huggingface returns a 403/429/5xx
(rate-limit, captcha, transient outage), curl exits 22 — which falls into the `default` branch of
`classifyEgressProbe` and is reported as an infrastructure FAIL ("the probe could not run"),
NOT `blocked=false`. The intent of the negative control is "an external host MUST be unreachable";
a host that answered with an HTTP error is demonstrably *reachable* (egress is open), which should
fail the verification as "egress is NOT blocked", not be excused as a probe-infra problem. As
written, an open-egress host can escape the "not blocked" verdict under the wrong remote status,
weakening the very control PRIV-06 depends on. This is a false-negative on the security assertion
(egress-open mistaken for probe-broken).
**Fix:** Treat a non-connection/timeout exit from the EXTERNAL probe as "reachable ⇒ not blocked"
rather than infra-failure, OR drop `-f` for the external probe so any HTTP response (even 4xx/5xx)
returns exit 0 ⇒ `blocked=false`. Reserve the infra-failure classification for codes that
genuinely mean "curl/podman could not run" (e.g. 127, or the `-1` never-started case). The
in-network sanity probe already proves the probe environment works, so an external HTTP-error
response should be read as a reachable host (egress open), which the gate must FAIL.

### WR-03: `--coding-agent` with only shared-residency coder fit hard-blocks the install with a misleading "no coder fits" message

**File:** `cmd/villa/install.go:456-462`, `:579-583`; `cmd/villa/install_agent.go:44-54`
**Issue:** When `--coding-agent` is passed and a coder DOES fit but only in *shared* residency
(`rec.Coder.Fits == true`, `rec.Coder.Residency == "shared"`, `rec.Coder.Model == ""`),
`cfg.CodingMode` is never set (line 456 guards on `rec.Coder.Model != ""`), and step 6c's
`coderShardFor` returns `(zero, false)` for the empty id, so install BLOCKS at line 581 with
"no coder model fits the detected memory envelope". That message is factually wrong for the
shared-residency case (a coder *does* fit), and the addon is rejected outright on a host the
recommender considers viable. The code comments (line 455) acknowledge shared residency is a
"future" path, so this is a deliberate v1.4 swap-only limitation — but surfacing it as
"no coder fits" misdirects the operator (who may have ample memory) toward "free memory / use a
larger host", which will not help. The `agentEnabledForGate` preflight at step 3a' also runs the
agent disk/envelope checks for this case, doing wasted work before the step-6c block.
**Fix:** Detect `rec.Coder.Fits && rec.Coder.Residency == "shared"` explicitly and emit a
distinct refusal ("the coding-agent addon currently requires a swap-residency coder fit; this
host only supports shared residency, which is not yet served") so the operator is not told to
free memory they do not lack. If shared residency is meant to be unsupported, the message should
say so rather than reuse the no-fit copy.

## Info

### IN-01: `liveCoderModelPresent` / `liveEmbedModelPresent` size-only guard accepts a same-size tampered file

**File:** `cmd/villa/install_agent.go:62-70`; `cmd/villa/install_memory.go:94-103`
**Issue:** The integrity guard is stat-size-only (`uint64(fi.Size()) == sh.SizeBytes`). A file
that was tampered/corrupted *in place* but kept the exact byte length passes the presence check
and is never re-pulled/re-verified, so the container serves an unverified weight. The code comments
explicitly accept this tradeoff ("a cheap stat-only guard; it does NOT re-hash on every install"),
which is reasonable for the common truncation case, but it is worth recording that the SHA256 is
only ever asserted at download time, never on a present-file re-install.
**Fix:** Acceptable as-is for performance; if stronger assurance is wanted, opportunistically
re-hash when the file's mtime is newer than the install record, or document that integrity rests
on the download-time verify + filesystem trust.

### IN-02: `existingAncestorDir` lacks a symlink-loop / unbounded-walk guard

**File:** `cmd/villa/install.go:1387-1402`
**Issue:** The loop walks `filepath.Dir(p)` upward until a `Stat` succeeds or the parent equals
itself (root). This is correct for normal paths, but on a pathological input it relies entirely on
`filepath.Dir` converging to `/`. It does converge for all real absolute/relative paths, so this
is not a live bug — noted only because it is an unbounded loop with no explicit iteration cap.
**Fix:** None required; optionally add a small iteration ceiling as defense-in-depth.

### IN-03: `evalAgentVerify` discards the llama-down task error, relying on a comment to justify it

**File:** `cmd/villa/verify_agent.go:94`, `:212-220`
**Issue:** `answered, _ := llamaDownTask()` drops the error, and inside `liveAgentVerify` the
`llamaDownTask` closure always returns `(answered, nil)`. The design is correct (an inference-down
error is the EXPECTED outcome), but the contract is enforced only by convention/comments: a future
edit that makes `llamaDownTask` return a meaningful error would have it silently ignored. The
restore error is correctly captured via the separate `restoreErr` closure variable.
**Fix:** None required; consider a named blank (`answered, _ /*expected inference-down*/`) or a
typed result to make the "error is intentionally expected" contract self-documenting.

### IN-04: `liveAgentToolCallProbe` ignores `crush run` stdout/stderr, surfacing only an opaque exit-code error

**File:** `cmd/villa/install_agent.go:288-292`
**Issue:** On a non-zero `crush run`, the driver returns `fmt.Errorf("crush run: %w", runErr)`
without capturing the command's stdout/stderr (unlike `runProbeCurl`, which folds stderr into the
error). When the readiness/verify proof FAILs, the operator gets "crush run: exit status 1" with no
clue from the agent's own output, making remediation harder.
**Fix:** Capture `cmd.Stderr`/`cmd.Stdout` into buffers and include a bounded tail in the returned
error (mirroring `runProbeCurl`'s stderr-folding) so a failed round-trip carries actionable detail.

---

_Reviewed: 2026-06-14_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
