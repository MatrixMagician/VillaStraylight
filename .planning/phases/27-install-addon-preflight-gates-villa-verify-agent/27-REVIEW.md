---
phase: 27-install-addon-preflight-gates-villa-verify-agent
reviewed: 2026-06-14T00:00:00Z
depth: deep
files_reviewed: 16
files_reviewed_list:
  - cmd/villa/install_agent.go
  - cmd/villa/install_agent_test.go
  - cmd/villa/install.go
  - cmd/villa/install_test.go
  - cmd/villa/preflight_agent.go
  - cmd/villa/preflight_agent_test.go
  - cmd/villa/uninstall.go
  - cmd/villa/uninstall_test.go
  - cmd/villa/verify_agent.go
  - cmd/villa/verify_agent_test.go
  - cmd/villa/verify.go
  - internal/agent/policy.go
  - internal/agent/render.go
  - internal/agent/render_test.go
  - internal/config/villaconfig.go
  - internal/config/villaconfig_test.go
findings:
  critical: 1
  warning: 6
  info: 4
  total: 11
status: issues_found
---

# Phase 27: Code Review Report

**Reviewed:** 2026-06-14
**Depth:** deep
**Files Reviewed:** 16
**Status:** issues_found

## Summary

Phase 27 wires the Crush coding-agent install addon, honest preflight gates, uninstall
teardown, and the `villa verify agent` runtime PRIV-06 proof. The seam discipline is
strong: pure cores (`evalAgentProof`, `evalAgentVerify`, `runAgentChecks`, `agent.Render`)
are cleanly unit-testable, the restrictive-tools render is asserted by golden + targeted
tests, traversal guards are present on every write/remove, no shell interpolation, no
leaked backend/image literals, and the config byte-identical-off guarantee is well covered.

However, the review surfaces one BLOCKER: **the `--coding-agent` install stages a coder
GGUF that is never served**, because the inference unit and the rendered `crush.json` both
resolve to the *chat* model — `cfg.CoderModel`/`CodingMode` are never set on this path. The
agent therefore drives the chat model, the staged coder weight is dead disk, and the
readiness/verify proofs can pass while silently NOT exercising the coder the addon claims to
install. Several WARNINGs concern false-green surfaces in the egress negative-control, a
missing HTTP timeout, and a dead size guard.

The note about `install.go` overwriting persisted `backend=rocm` (line 432) is already
tracked in a separate follow-up; corroboration is in IN-04 below, not re-filed as a blocker.

## Critical Issues

### CR-01: `--coding-agent` stages a coder GGUF that is never served — the agent drives the chat model

**File:** `cmd/villa/install.go:446-572` (flag→cfg), `cmd/villa/install.go:459-468` (render), `internal/agent/render.go:149-155` (servedModelID)

**Issue:** On a `--coding-agent` install, `runInstall` sets `cfg.AgentEnabled = true` (line 446-448) and stages the coder shard resolved from `rec.Coder` (lines 540-556), but it NEVER sets `cfg.CoderModel`, `cfg.CoderAgentCtx`, or `cfg.CodingMode`. Consequently:

1. The inference unit is rendered from `orchestrate.RenderInput{Backend, Cfg, ModelFile, ModelsDir}` (line 459) with **no `CodingMode`** field set. Per `internal/orchestrate/render.go:101` the coder model is only served when `in.CodingMode != nil`, so `villa-llama` serves the **chat** model on `:8080`, not the staged coder.
2. `liveRenderCrushConfig(cfg)` → `agent.Render(cfg, …)` derives the served model id from `servedModelID`, which uses `cfg.CoderModel` only if non-empty, else `cfg.Model` (the chat model). So `crush.json` advertises `villa-<chatModel>` with `context_window = defaultContextWindow (32768)`, not the coder's `AgentCtx`.
3. `coderShard` (declared line 528, assigned line 545) is downloaded to `modelsDir()` and then **never referenced again** — it does not influence the rendered unit's `-m` path. It is dead-staged disk.

The net effect: the headline feature ("install the local coding agent + coder model") downloads a multi-GB coder weight that is never loaded, and the agent talks to the general chat model. Worse for honesty-by-construction: the install readiness proof (step 10c, `crush run` round-trip on `:8080`) and `villa verify agent` both PASS against the chat model, so the green signal does NOT prove the coder model the addon staged actually serves. The disk-footprint preflight gate (`AGENT-PRE-disk`) and the envelope gate (`AGENT-PRE-envelope`, driven by `rec.Coder`) reserve memory/disk for a coder that is then not run — the gate basis and the runtime are divergent, which is exactly the drift the phase's "single source" design (D-02/D-04) set out to prevent.

**Fix:** Make the addon serve what it stages. On the `--coding-agent` path, set the coder render inputs from the SAME `rec.Coder` the disk/envelope gates and the staged shard derive from, and thread `CodingMode` into the render so the inference unit loads the coder GGUF:

```go
if opts.codingAgent {
    cfg.AgentEnabled = true
    // Serve the coder the addon staged — single-source from rec.Coder so the
    // gate basis, the staged GGUF, the served unit, and crush.json all agree.
    cfg.CoderModel    = rec.Coder.Model
    cfg.CoderQuant    = rec.Coder.Quant
    cfg.CoderAgentCtx = rec.Coder.AgentCtx
    cfg.CodingMode    = true
}
// ...
units, err := d.render(orchestrate.RenderInput{
    Backend:    backend,
    Cfg:        cfg,
    ModelFile:  modelFile,   // must resolve the coder shard's filename when CodingMode
    ModelsDir:  d.modelsDir(),
    CodingMode: codingModeDescriptorFor(rec.Coder), // non-nil so render serves the coder
})
```

If the intended design is genuinely "agent runs against the chat model and the coder GGUF is for a later `coding-mode enter`", then the staging, the disk gate, and the envelope gate are all mis-scoped for this verb and should be removed from the `--coding-agent` path (and the readiness proof should not be sold as proving the coder). Either way the current state — stage-and-ignore — is incorrect and must be resolved before ship. Add a test asserting the rendered `RenderInput`/`crush.json` served id equals `rec.Coder.Model` when `--coding-agent` is set.

## Warnings

### WR-01: Egress negative-control treats ANY probe failure as "egress blocked" — false-green on broken probe infra

**File:** `cmd/villa/verify_agent.go:129-134` (and the shared `cmd/villa/verify_memory.go:147-152`)

**Issue:** `egressBlocked` runs `runProbeCurl(... podman run --rm --network villa --entrypoint curl <helperImage> curl -sf --max-time 5 https://huggingface.co/)` and returns `blocked := err != nil`. ANY non-zero exit — image not present/pullable, the `villa` network missing, podman daemon error, OOM, curl absent in the helper image, a typo'd host — is interpreted as "external host unreachable → egress is blocked → good". Because the negative control is the FIRST gate (and the whole PRIV-06 verdict is gated behind it passing), a broken probe environment yields a *passing* negative control, after which a completing agent task produces an overall PASS. That is precisely the false-green the negative-control-first design exists to forbid: the control is meant to prove egress is *actively blocked*, not merely that a `podman run` failed for unknown reasons. While this pattern predates Phase 27 in `verify_memory.go`, Phase 27 newly makes it load-bearing for the headline PRIV-06 agent gate.

**Fix:** Distinguish "probe ran and the host was unreachable (blocked)" from "probe could not run". Have `runProbeCurl`/the egress closure surface curl's own exit semantics — e.g. require the container to start and curl to return a *connection/timeout* failure code (curl exit 6/7/28), and treat infrastructure errors (image/network/daemon) as an `err` from `egressBlocked` (→ FAIL "could not run the negative-control probe"), not as `blocked=true`. At minimum, run a positive sanity probe first (curl to the loopback endpoint inside the same network must SUCCEED) so a wholesale-broken probe environment fails the control rather than passing it.

### WR-02: `liveInstallAgentBinary` uses `http.DefaultClient` with no client timeout

**File:** `cmd/villa/install_agent.go:106-117`

**Issue:** The Crush tarball download uses `http.DefaultClient.Do(req)` with only the request `ctx`. `cmd.Context()` from cobra typically has no deadline, so a stalled/slow-loris server (headers sent, body trickled) can hang the install indefinitely; `http.DefaultClient` has no `Timeout`. The memory/model pulls go through `download.PullModel` which presumably bounds this, but the agent-binary path is a hand-rolled `http.DefaultClient` call.

**Fix:** Use a client with an explicit timeout and/or a context deadline:

```go
client := &http.Client{Timeout: 5 * time.Minute}
// or: ctx, cancel := context.WithTimeout(ctx, 5*time.Minute); defer cancel()
resp, err := client.Do(req)
```

### WR-03: `liveInstallAgentBinary` does not assert an HTTPS scheme on the policy-derived URL

**File:** `cmd/villa/install_agent.go:104-110`, `internal/agent/policy.go:36`

**Issue:** The download URL is `strings.ReplaceAll(policy.URLTmpl, "{asset}", asset.Name)` from the embedded policy. The tarball is checksum-verified before extraction (good), so a MITM cannot inject a different binary — but a plaintext `http://` template would still allow a network attacker to *stall/redirect* the fetch and, more importantly, the defense-in-depth contract for a security-sensitive pinned download is transport integrity + the checksum. There is no guard that `URLTmpl` is `https://`. Since this is build-time embedded data the risk is low, but a one-line assertion makes the security posture explicit and catches a future policy edit that drops to `http`.

**Fix:** After substitution, parse the URL and refuse a non-HTTPS scheme:
```go
if u, err := url.Parse(url); err != nil || u.Scheme != "https" {
    return "", fmt.Errorf("install: refusing a non-HTTPS Crush download URL %q", url)
}
```

### WR-04: `liveCoderModelPresent` integrity guard is a dead tautology (`fi.Size() >= 0`)

**File:** `cmd/villa/install_agent.go:69`

**Issue:** `return fi.Size() >= 0 && uint64(fi.Size()) == sh.SizeBytes`. `os.FileInfo.Size()` is always `>= 0` for a regular file, so the first conjunct is always true and adds nothing — it reads as a guard but guards nothing. More substantively, this presence check is **size-only**: a same-size but content-tampered/corrupt GGUF is treated as present and is NEVER re-pulled or re-hashed (the comment even says "does NOT re-hash on every install"). The doc claims this protects against "truncated/tampered" files, but a tamper that preserves byte length passes. The downstream `download.PullModel` only runs when this returns false, so a same-size corrupt weight silently skips verification.

**Fix:** Drop the dead `fi.Size() >= 0` conjunct. Either keep the size-only check but correct the comment to say it detects truncation/size-mismatch only (not arbitrary tampering), or, since the file was SHA-verified at pull time and a verified marker could be persisted, gate re-verification on a recorded checksum stamp rather than size alone.

### WR-05: `liveAgentToolCallProbe` cannot distinguish "agent edited" from "agent failed but left TOKEN_B-shaped output"

**File:** `cmd/villa/install_agent.go:272-299`

**Issue:** Success is `strings.Contains(string(edited), agentProbeTokenB)`. The probe plants `TOKEN_A`, asks the agent to replace it with `TOKEN_B`, and declares edited if the file now *contains* `TOKEN_B`. But the prompt itself names both tokens, and the file is left in the working dir. If a future Crush version (or a tool that writes a transcript/log into the workdir, or a partial write) deposits the literal `VILLA_PROBE_TOKEN_B` anywhere readable as the probe file, or if the agent appends rather than replaces, `Contains` reports success without a real semantic edit. The `cmd.Run()` error is checked first (good), but a zero-exit `crush run` that wrote a confirmation containing TOKEN_B without performing the replace would false-green.

**Fix:** Assert the replace happened, not mere presence: require TOKEN_B present AND TOKEN_A absent (`strings.Contains(s, TOKEN_B) && !strings.Contains(s, TOKEN_A)`), since the round-trip is a *replacement*. This closes the append/echo false-green and matches the "real edit" contract (D-05).

### WR-06: `liveAgentVerify` swallows the llama-down restore error — service may be left stopped on a Start failure

**File:** `cmd/villa/verify_agent.go:140-152`

**Issue:** The llama-down control stops `villa-llama.service`, runs the task, and restores via `defer func() { _ = deps.systemd.Start(installServiceName) }()`. The restore error is discarded. The doc (T-27-16) promises "the service is never left stopped", but if `Start` fails (e.g. transient systemd error), the operator gets a PASS/FAIL verdict with the inference service silently left DOWN and no surfaced error or remediation. For a verb that deliberately stops a core service, a restore failure is exactly the case the operator must hear about.

**Fix:** Capture and surface the restore error — either fold it into the verdict detail or print a prominent stderr warning with the manual `systemctl --user start villa-llama.service` remediation:

```go
defer func() {
    if rerr := deps.systemd.Start(installServiceName); rerr != nil {
        fmt.Fprintf(os.Stderr, "verify agent: WARNING — failed to restore %s (%v); run `systemctl --user start %s`\n",
            installServiceName, rerr, installServiceName)
    }
}()
```

## Info

### IN-01: `coderShard` assigned but only used inside a nested block — reads as dead at the outer scope

**File:** `cmd/villa/install.go:528,545`

**Issue:** `var coderShard catalog.Shard` is declared at the block top (528) and assigned `coderShard = sh` (545), but every subsequent use is within the same `if cfg.AgentEnabled` block (549-555). The outer-scope declaration suggests a later use that does not exist (see CR-01 — it *should* feed the render). As written it is a confusing hoist. If CR-01 is fixed it becomes load-bearing; if not, collapse it to a local `sh` inside the block.

### IN-02: `agentDiskCheck` PASS detail uses Unicode `≥` / `<` in `Detail` strings

**File:** `cmd/villa/preflight_agent.go:122,134`

**Issue:** Detail strings embed `≥` and `<` (U+2265). Other CheckResult copy in the codebase is ASCII; a non-UTF8 terminal or a downstream `--json` consumer comparing against ASCII fixtures could mis-render. Minor; flagged for consistency with the project's ASCII-leaning copy.

### IN-03: cloud-credential allowlist is duplicated between source and test as parallel literals

**File:** `cmd/villa/preflight_agent.go:50-62`, `cmd/villa/preflight_agent_test.go:193-197`

**Issue:** `TestAgentCloudCredAllowlistComplete` re-lists all 11 provider keys as a `want` literal and checks the source list contains each. This is a one-directional pin: a key ADDED to the source but not the test is not caught, and the two lists can drift. Acceptable as a regression pin, but a length assertion (`len(cloudCredentialAllowlist) == len(want)`) would make it bidirectional.

### IN-04: corroboration of the already-tracked persisted-backend overwrite (not re-filed)

**File:** `cmd/villa/install.go:428-432`

**Issue:** As noted in the task context, `cfg := d.loadedConfig()` then `cfg.Backend = rec.Backend` overwrites a persisted `backend=rocm` with the recommend default (Vulkan) on every `villa install`. Confirmed present; this is the same line. Nuance relevant to Phase 27: because the coding-agent readiness/verify proofs run against whatever backend the unit ends up rendered with, a silent backend downgrade here would also silently change which backend the agent's inference runs on. Already logged in the separate follow-up — recorded here only for cross-reference, not as a new finding.

---

_Reviewed: 2026-06-14_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
