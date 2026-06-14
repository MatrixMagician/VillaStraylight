---
phase: 27-install-addon-preflight-gates-villa-verify-agent
verified: 2026-06-14T00:00:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 2/5
  gaps_closed:
    - "CR-01: `villa install --coding-agent` now serves the staged coder (rec.Coder) — CodingMode/CoderModel/CoderQuant/CoderAgentCtx set + threaded into RenderInput"
    - "WR-05: install readiness asserts a REAL replacement (TOKEN_B present AND TOKEN_A absent) via agentProbeReplaced"
    - "WR-01: egress negative control FAILs on a broken probe (classifyEgressProbe distinguishes blocked=curl 6/7/28 from infra-failure); in-network sanity probe targets villa-llama via orchestrate.LlamaInNetworkEndpoint()"
    - "WR-06: llama-down restore (Start) failure is surfaced (verdict downgrade + `systemctl --user start villa-llama.service` remediation), never silently swallowed"
  gaps_remaining: []
  regressions: []
---

# Phase 27: Install Addon, Preflight Gates & `villa verify agent` Verification Report

**Phase Goal:** Make the Crush coding agent an OPTIONAL `villa install` addon with sanctioned-window coder pre-staging + a real tool-call readiness proof (INSTALL-03), honest preflight gates + uninstall coverage (INSTALL-04), and `villa verify agent` — the runtime strictly-local proof: negative-control-first egress block + no-silent-cloud-fallback (llama-down) controls (PRIV-06).
**Verified:** 2026-06-14
**Status:** passed
**Re-verification:** Yes — after gap closure (plans 27-05 + 27-06 closing CR-01, WR-01, WR-05, WR-06)

## Goal Achievement

The four findings from the prior verification (`gaps_found`, 2/5) are all closed in the
ACTUAL codebase (not just claimed in SUMMARY). I traced each fix to the cited lines, ran the
new off-hardware seam tests, and confirmed `make check` (vet + full suite) is green with no
regression. The two genuinely-solid surfaces from the prior pass (INSTALL-04 preflight +
uninstall) remain intact (quick regression check). All 5 ROADMAP success criteria are now
observably true.

### Observable Truths (ROADMAP Success Criteria)

| # | Truth (SC) | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Coding-agent addon: gate → pre-stage coder GGUF + binary → render → readiness proof with a REAL tool-call round-trip against the CODER (INSTALL-03) | ✓ VERIFIED | `install.go:446-461` sets `cfg.CoderModel/CoderQuant/CoderAgentCtx/CodingMode` from `rec.Coder` (single-source, gated on `rec.Coder.Model != ""`); `install.go:487-502` threads a non-nil `RenderInput.CodingMode` (via Phase-25 `codingServedTarget`/`codingModelFile`/`codingDescriptor`) + the coder `ModelFile`, so the unit + crush.json now serve `rec.Coder.Model` — staged disk is no longer dead disk (CR-01 closed). Readiness step 10c (`install.go:743-755` → `agentProofFn` → `evalAgentProof(liveAgentToolCallProbe(ctx))` at `:1340`) asserts a REAL replacement via `agentProbeReplaced` (WR-05 closed). `TestInstallCodingAgent*` (28 incl. served-id + chat-only off-path) + `TestAgentProbeReplaced` truth table pass. |
| 2 | Preflight gates the addon honestly: disk BLOCK, post-coder envelope BLOCK (from rec.Coder), cloud-credential WARN, typed-Unknown → WARN (INSTALL-04) | ✓ VERIFIED (regression-checked) | `preflight_agent.go`: `runAgentChecks` (line 90) builds disk `TierBlock` (line 109) + envelope `TierBlock` from `rec.Coder.Fits` (line 120, never re-derived) + cloud-cred WARN over `cloudCredentialAllowlist` (lines 43-50). Folded only when the addon is enabled. No regression from gap closure. Tests green. |
| 3 | `villa verify agent` proves egress is ACTIVELY blocked, negative-control-FIRST — a broken probe must FAIL, never false-green (PRIV-06) | ✓ VERIFIED | Pure core `evalAgentVerify` (`verify_agent.go:62-103`) gates `agentTask` behind `blocked` (negative-control-first preserved). The live `egressBlocked` closure (`:178-202`) now (a) runs a POSITIVE in-network sanity probe to `orchestrate.LlamaInNetworkEndpoint()` FIRST (no host-loopback false-FAIL on healthy hosts), then (b) classifies the external probe via `classifyEgressProbe` (`:131-148`): `blocked=true` ONLY for curl exit {6,7,28}; ANY sanity error or unclassified/never-started (-1) → ERROR → FAIL. The defective `return err != nil, nil` is gone. `runProbeCurlCode` (`install_memory.go:384-415`) extracts the exit code via `errors.As`. `TestClassifyEgressProbe` + `TestEvalAgentVerifyInfraErrorFails` pass (WR-01 closed). |
| 4 | Llama-down negative control proves no silent cloud fallback; restore never leaves villa-llama silently stopped (PRIV-06) | ✓ VERIFIED | `evalAgentVerify` (`:94-100`): `answered == true` with villa-llama stopped → FAIL (cloud-fallback smoking gun) — control logic intact. `runLlamaDownControl` (`:248-258`) ALWAYS attempts the deferred restore and CAPTURES the Start error (no more discarded `_ = Start(...)`); `liveAgentVerify` (`:228-234`) downgrades a would-be PASS to FAIL and surfaces `restoreLlamaWarning` (`:265-270`) carrying the literal `systemctl --user start villa-llama.service` remediation (WR-06 closed). `TestLlamaDownRestore` + `TestRestoreLlamaWarning` pass. |
| 5 | `villa uninstall` removes the agent binary, rendered config, and addon artifacts (INSTALL-04) | ✓ VERIFIED (regression-checked) | `uninstall.go`: `removeAgentBinary`/`removeCrushConfig` seams (lines 87-88), invoked in the ordered teardown (lines 191/196), live-wired (lines 338-339). No regression from gap closure. Tests green. |

**Score:** 5/5 truths verified. SC#1 and SC#3 (both prior FAILs) are now closed; SC#4's WR-06 honesty gap is closed; SC#2 and SC#5 remain intact (no regression).

### Required Artifacts

| Artifact | Expected | Status | Details |
| --- | --- | --- | --- |
| `cmd/villa/install.go` | `--coding-agent` sets coder render inputs from `rec.Coder` + threads non-nil `CodingMode` into RenderInput | ✓ VERIFIED | Lines 446-502; `cfg.CoderModel = rec.Coder.Model` present; chat-only path byte-identical (CodingMode nil). |
| `cmd/villa/install_agent.go` | `agentProbeReplaced` (TOKEN_B present AND TOKEN_A absent), wired into `liveAgentToolCallProbe` | ✓ VERIFIED | Predicate at lines 308-309; called at line 298. |
| `internal/orchestrate/endpoint.go` | `LlamaInNetworkEndpoint()` composing `containerName` + `inference.ServerPort()`, no re-typed host literal | ✓ VERIFIED | Line 28-30: `fmt.Sprintf("http://%s:%d/v1", containerName, inference.ServerPort())`. |
| `internal/inference/backend_vulkan.go` | `ServerPort()` one-line accessor | ✓ VERIFIED | Line 166: `func ServerPort() int { return serverPort }`. |
| `cmd/villa/install_memory.go` | `runProbeCurlCode` exit-code accessor (additive) | ✓ VERIFIED | Lines 384-415; `errors.As(*exec.ExitError)`, -1 never-started fallback; `runProbeCurl` unchanged. |
| `cmd/villa/verify_agent.go` | `classifyEgressProbe` + two-layer egress probe + `runLlamaDownControl`/`restoreLlamaWarning` | ✓ VERIFIED | classifyEgressProbe 131-148; egressBlocked closure 178-202; runLlamaDownControl 248-258; restoreLlamaWarning 265-270. |
| `cmd/villa/preflight_agent.go` | `runAgentChecks` (disk/envelope BLOCK, cloud-cred WARN) | ✓ VERIFIED | Intact, no regression. |
| `cmd/villa/uninstall.go` | `removeAgentBinary` + `removeCrushConfig` ordered seams | ✓ VERIFIED | Intact, no regression. |
| `cmd/villa/verify.go` | `newVerifyAgent` registered under verify parent | ✓ VERIFIED | Line 76. |

### Key Link Verification

| From | To | Via | Status | Details |
| --- | --- | --- | --- | --- |
| `install.go` --coding-agent path | `orchestrate.RenderInput.CodingMode` | serves the staged coder | ✓ WIRED | Non-nil CodingMode threaded (install.go:500); coder ModelFile resolved (`:499`). CR-01 root cause closed. |
| install readiness | `evalAgentProof(liveAgentToolCallProbe)` | real crush-run replace round-trip | ✓ WIRED | install.go:1340; `agentProbeReplaced` asserts TOKEN_B present + TOKEN_A absent. |
| `liveAgentVerify` egressBlocked | `classifyEgressProbe` → FAIL on infra | negative-control-first, infra→FAIL | ✓ WIRED | Infra/sanity error → ERROR → FAIL; never blocked=true on probe failure. |
| `liveAgentVerify` sanity probe | `orchestrate.LlamaInNetworkEndpoint()` | container-DNS + server port, no host literal | ✓ WIRED | No `villa-llama:8080`/`127.0.0.1` literal in verify_agent.go (grep clean). |
| `runLlamaDownControl` deferred restore | operator FAIL + `systemctl --user start` | captured Start error → verdict downgrade | ✓ WIRED | verify_agent.go:228-234, 265-270. |
| `runUninstall` | removeAgentBinary / removeCrushConfig | ordered teardown | ✓ WIRED | Intact. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Seam grep-gate (no leaked backend/image/host literal in cmd/villa) | `go test ./internal/inference/ -run TestSeamGrepGate` | passed (in 632-pass run) | ✓ PASS |
| Gap-closure unit contracts (CR-01/WR-01/WR-05/WR-06) | `go test ./cmd/villa/ -run 'TestClassifyEgressProbe\|TestAgentProbeReplaced\|TestLlamaDownRestore\|TestRestoreLlamaWarning\|TestEvalAgentVerifyInfraErrorFails\|TestInstallCodingAgent'` | 28 passed | ✓ PASS |
| cmd/villa + orchestrate + inference suites | `go test ./cmd/villa/ ./internal/orchestrate/ ./internal/inference/ -count=1` | 632 passed | ✓ PASS |
| Full pre-commit gate | `make check` (vet + full suite) | all packages ok, no FAIL | ✓ PASS |

Note: `villa verify agent`'s on-hardware controls (rootless-netns nft FORWARD drop egress block; llama-down stop/restore) require a live host + egress precondition and were exercised in the 27-04 on-hardware acceptance. The gap-closure delta is proven off-hardware by the new seam tests above; the live exec paths (`runProbeCurlCode` exit extraction) remain advisory-untested per WR-01 review note (see below).

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| --- | --- | --- | --- | --- |
| INSTALL-03 | 27-01, 27-04, 27-05 | Coding agent as optional install addon with real tool-call readiness | ✓ SATISFIED | Addon now SERVES the staged coder (CR-01 closed) and the readiness proof asserts a REAL replacement against that coder (WR-05 closed). `REQUIREMENTS.md` `[x]` is now earned (was premature at the prior pass). |
| INSTALL-04 | 27-02 | Honest preflight gates + uninstall removes binary/config/artifacts | ✓ SATISFIED | preflight_agent.go gates + uninstall.go ordered teardown intact (regression-checked). |
| PRIV-06 | 27-03, 27-04, 27-06 | `villa verify agent` zero-outbound negative-control-first + llama-down no-cloud-fallback | ✓ SATISFIED | Egress negative control FAILs on a broken probe (WR-01 closed); llama-down restore failure surfaced (WR-06 closed); negative-control-first ordering + cloud-fallback FAIL logic intact. |

All three phase requirement IDs (INSTALL-03, INSTALL-04, PRIV-06) are accounted for in
`.planning/REQUIREMENTS.md` (lines 36-38, status table lines 87-89 "Complete"). No orphaned
requirements: REQUIREMENTS.md maps exactly these three IDs to Phase 27, all claimed by plans.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| --- | --- | --- | --- | --- |
| — | — | No unreferenced TBD/FIXME/XXX debt markers in any gap-closure-modified file | — | Clean |
| `cmd/villa/verify_agent.go` | 199 | External egress probe `curl -sf`: a reachable-but-HTTP-erroring host (exit 22) classifies as infra-FAIL, not "not blocked" (WR-02) | ℹ️ Info (fail-safe) | Does NOT undermine PRIV-06: exit 22 falls to `classifyEgressProbe`'s `default` (line 146) → ERROR → FAIL verdict. It can NEVER produce `blocked=true` (line 142 is curl {6,7,28} only) and NEVER a PASS. Security direction is safe (egress-open → FAIL); only the FAIL message is less precise. Advisory in 27-REVIEW.md. |
| `cmd/villa/install_memory.go` | 384-415 | `runProbeCurlCode` exit-code extraction untested with a real-exec case (WR-01 review note) | ℹ️ Info | The synthetic classifier is fully tested; the live `errors.As` extraction is convention-anchored. Not a current correctness defect — recorded for optional follow-up. |
| `cmd/villa/install.go` | 456-462, 579-583 | `--coding-agent` with only shared-residency coder fit blocks with a "no coder fits" message (WR-03 review note) | ℹ️ Info | v1.4 swap-only limitation; misleading copy, not a false-green or a must_have failure. Optional follow-up. |

None of the three advisory items reaches BLOCKER or undermines an INSTALL/PRIV must_have:
each is fail-safe (a less-precise FAIL, an untested-but-correct extraction, or a v1.4 scope
boundary). They are documented in 27-REVIEW.md for optional follow-up.

### Human Verification Required

None outstanding. The on-hardware acceptance (27-04) was already run; the egress/llama-down
controls were exercised on the live gfx1151 host. The gap-closure delta is proven by the new
off-hardware seam tests; no new human-verification item is introduced by this verdict. (The
WR-01 review note suggests one real-exec test for `runProbeCurlCode` as optional follow-up,
not a blocking human check.)

### Gaps Summary

No gaps. All four prior findings are closed in the actual codebase and asserted by passing
tests:

- **CR-01 (was BLOCKER) — CLOSED:** `villa install --coding-agent` now enters coding-mode and
  serves `rec.Coder.Model` (install.go:446-502), single-sourced from the same `rec.Coder` the
  disk/envelope gates and the staged shard derive from. The staged coder GGUF is the served
  model; crush.json + the readiness proof + `villa verify agent` all target the coder. Proven
  by the served-id + chat-only off-path assertions in `TestInstallCodingAgent*`.
- **WR-01 — CLOSED:** the egress negative control FAILs on a broken probe environment
  (`classifyEgressProbe` returns an error for any sanity failure or unclassified/never-started
  exit; `blocked=true` only for curl 6/7/28), and the in-network sanity probe targets
  villa-llama via `orchestrate.LlamaInNetworkEndpoint()` (no host-loopback false-FAIL, no
  re-typed host literal). Proven by `TestClassifyEgressProbe` + `TestEvalAgentVerifyInfraErrorFails`.
- **WR-05 — CLOSED:** readiness asserts `Contains(TOKEN_B) && !Contains(TOKEN_A)` via the pure
  `agentProbeReplaced`, shared by the install proof and the verify agentTask. Proven by
  `TestAgentProbeReplaced`.
- **WR-06 — CLOSED:** the llama-down restore failure is captured and surfaced (verdict
  downgrade + `systemctl --user start villa-llama.service` remediation), never silently
  swallowed. Proven by `TestLlamaDownRestore` + `TestRestoreLlamaWarning`.

The honesty/seam invariants from CLAUDE.md hold throughout: TestSeamGrepGate green (no leaked
backend/image/host literal in cmd/villa), the negative control runs FIRST and fails-closed,
the llama-down control FAILs on an answer, all exec is fixed-arg with no shell interpolation,
and the chat-only render path is byte-identical (no golden refreeze). The phase goal is
achieved.

---

_Verified: 2026-06-14_
_Verifier: Claude (gsd-verifier)_
_Re-verification after gap closure (plans 27-05 + 27-06)_
