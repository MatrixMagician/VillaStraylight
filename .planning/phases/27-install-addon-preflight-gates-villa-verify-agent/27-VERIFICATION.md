---
phase: 27-install-addon-preflight-gates-villa-verify-agent
verified: 2026-06-14T00:00:00Z
status: gaps_found
score: 2/5 must-haves verified
overrides_applied: 0
gaps:
  - truth: "`villa install --coding-agent` stages the coder GGUF + renders crush.json + proves readiness against the CODER model the addon exists for (SC#1 / INSTALL-03)"
    status: failed
    reason: >-
      runInstall sets cfg.AgentEnabled = true but never sets cfg.CodingMode /
      cfg.CoderModel / cfg.CoderAgentCtx. The render() call passes no CodingMode,
      so internal/orchestrate/render.go (CodingMode == nil) keeps villa-llama
      serving the CHAT model on :8080 at the default 32768 ctx. agent.Render's
      servedModelID falls back to cfg.Model (chat) because CoderModel is empty,
      so crush.json advertises the chat model. The staged coder GGUF (coderShard)
      is downloaded and then never referenced — dead-staged disk. Net: the
      install readiness proof (step 10c) and `villa verify agent` both PASS
      against the CHAT model, not the coder — confirmed on-hardware (the chat
      model unit served and `crush run` exercised the chat model). The headline
      addon configures crush.json + proves readiness against the wrong model.
    artifacts:
      - path: "cmd/villa/install.go"
        issue: "lines 446-448 set only cfg.AgentEnabled; CodingMode/CoderModel/CoderAgentCtx never set"
      - path: "cmd/villa/install.go"
        issue: "lines 459-464 render() passes no CodingMode field, so the coder GGUF is never served"
      - path: "internal/agent/render.go"
        issue: "servedModelID (lines 149-154) falls back to cfg.Model (chat) since CoderModel is empty — crush.json + ctx target the chat model"
    missing:
      - "On the --coding-agent path, set cfg.CoderModel/CoderQuant/CoderAgentCtx/CodingMode from the SAME rec.Coder the disk/envelope gates and the staged shard derive from (single source)"
      - "Thread a non-nil CodingMode descriptor (+ coder ModelFile) into the orchestrate.RenderInput so villa-llama loads the staged coder GGUF"
      - "Add a test asserting the rendered RenderInput / crush.json served id == rec.Coder.Model when --coding-agent is set"
  - truth: "`villa verify agent` proves egress is ACTIVELY blocked (negative-control-FIRST) — a broken probe environment must NOT false-green PRIV-06 (SC#3)"
    status: failed
    reason: >-
      egressBlocked returns `err != nil, nil`: ANY non-zero podman/curl exit
      (missing/unpullable image, missing villa network, podman daemon error,
      curl absent in the helper image, a typo'd host) is interpreted as
      "egress blocked = good". Because the negative control is the FIRST gate and
      the whole PRIV-06 verdict is gated behind it, a broken probe environment
      yields a PASSING control — exactly the false-green the negative-control-
      first design exists to forbid.
    artifacts:
      - path: "cmd/villa/verify_agent.go"
        issue: "lines 129-134: `return err != nil, nil` treats any probe failure as blocked=true"
    missing:
      - "Distinguish 'probe ran, host unreachable (blocked)' from 'probe could not run' — require curl connection/timeout exit semantics (6/7/28) and surface infrastructure errors as an egressBlocked() err (→ FAIL 'could not run the negative-control probe'), not blocked=true"
      - "Optionally run a positive loopback sanity probe inside the same network first, so a wholesale-broken probe environment fails the control rather than passing it"
  - truth: "Install/verify readiness proves a REAL replacement edit, not mere presence of TOKEN_B (SC#1 honesty / D-05)"
    status: failed
    reason: >-
      Success is `strings.Contains(string(edited), agentProbeTokenB)` only. The
      prompt names both tokens; if the agent appends rather than replaces, or a
      tool writes a transcript/confirmation containing TOKEN_B into the workdir,
      Contains reports success without a real semantic replace — a false-green on
      the readiness contract.
    artifacts:
      - path: "cmd/villa/install_agent.go"
        issue: "line 298: `strings.Contains(string(edited), agentProbeTokenB)` with no `!Contains(TOKEN_A)` replacement assertion"
    missing:
      - "Require TOKEN_B present AND TOKEN_A absent: `strings.Contains(s, TOKEN_B) && !strings.Contains(s, TOKEN_A)` to assert the replace happened, not mere presence (this driver is shared by the readiness proof and verify agentTask — fixing it once closes both)"
  - truth: "The llama-down control never silently leaves villa-llama stopped (T-27-16 'never left stopped')"
    status: failed
    reason: >-
      The deferred restore discards the Start error: `defer func() { _ =
      deps.systemd.Start(installServiceName) }()`. If Start fails (transient
      systemd error), the operator gets a PASS/FAIL verdict with villa-llama
      silently left DOWN and no surfaced error or remediation — for a verb that
      deliberately stops a core service, a restore failure is exactly what the
      operator must hear about.
    artifacts:
      - path: "cmd/villa/verify_agent.go"
        issue: "line 149: deferred `_ = deps.systemd.Start(installServiceName)` swallows the restore error"
    missing:
      - "Capture and surface the restore error — fold it into the verdict detail or print a prominent stderr warning with the manual `systemctl --user start villa-llama.service` remediation"
---

# Phase 27: Install Addon, Preflight Gates & `villa verify agent` Verification Report

**Phase Goal:** The coding agent is an optional `villa install` addon that comes up ready (real tool-call round-trip), gated honestly by preflight, removable by uninstall, and PROVEN strictly local at runtime — egress and cloud-fallback negative controls, never flag-trusted.
**Verified:** 2026-06-14
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

This verification incorporates the deep code review (`27-REVIEW.md`, 1 critical + 6 warnings) and the on-hardware acceptance. The team has decided gap-closure is required before the phase is marked complete. The blocker (CR-01) and three honesty warnings (WR-01/05/06) are recorded as gaps below; the genuinely-solid surfaces (INSTALL-04, the PRIV-06 structure, the egress mechanism, seam discipline) are recorded as VERIFIED so the gap-closure plan stays tightly scoped.

### Observable Truths (ROADMAP Success Criteria)

| # | Truth (SC) | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Coding-agent addon: gate → pre-stage coder GGUF + binary in the sanctioned window → render → readiness proof with a REAL tool-call round-trip (INSTALL-03) | ✗ FAILED | Gate/pre-stage/binary/render wiring all present (`install.go:446-572`), BUT the addon stages a coder GGUF that is never served: `CodingMode`/`CoderModel`/`CoderAgentCtx` are never set, so `render()` (install.go:459-464, no CodingMode → orchestrate/render.go:101) serves the CHAT model and `servedModelID` (render.go:149-154) puts the chat model in crush.json. Readiness PASSES against the wrong model. On-hardware confirmed chat-model served. (CR-01 BLOCKER) |
| 2 | Preflight gates the addon honestly: disk BLOCK, post-coder envelope BLOCK (from rec.Coder), cloud-credential WARN, typed-Unknown → WARN (INSTALL-04) | ✓ VERIFIED | `cmd/villa/preflight_agent.go`: `runAgentChecks` builds disk `TierBlock` (line 109), envelope `TierBlock` from `rec.Coder.Fits` (line 120, never re-derived), cloud-cred WARN over `cloudCredentialAllowlist` (lines 50-62), typed-Unknown→WARN. Folded into the install gate only when the addon is enabled. Tests green. |
| 3 | `villa verify agent` proves zero outbound, negative-control-FIRST: egress-open must FAIL the gate; egress-blocked run completes the real agent task (PRIV-06) | ✗ FAILED | Structure is correct and PASS-on-hardware was captured (rootless-netns nft FORWARD drop, T-27-20), BUT the negative control false-greens: `verify_agent.go:129-134` treats ANY probe failure as `blocked=true`, so a broken probe environment passes the control. Ordering/structure VERIFIED; the control's honesty is FAILED. (WR-01) |
| 4 | Llama-down negative control proves no silent cloud fallback: with villa-llama stopped the agent must NOT answer; the smoking-gun answer FAILS verification (PRIV-06) | ✓ VERIFIED (control logic) / ⚠️ see Gap-4 | `evalAgentVerify` (verify_agent.go:91-100): `answered == true` → FAIL is correct; on-hardware the llama-down task failed as expected. The control LOGIC is sound and was accepted on-hardware. The associated WR-06 restore-error-swallowing defect is recorded as a separate gap (availability/honesty), not a failure of the cloud-fallback control itself. |
| 5 | `villa uninstall` removes the agent binary, rendered config, and addon artifacts (INSTALL-04) | ✓ VERIFIED | `cmd/villa/uninstall.go`: `removeAgentBinary`/`removeCrushConfig` seams (lines 87-88), invoked in the ordered teardown (lines 191/196), live-wired (lines 338-339), traversal-guarded + idempotent; staged GGUF follows keep/remove-models; config.toml left. Ordered-teardown + config-left tests green. |

**Score:** 2/5 truths verified (SC#2, SC#5). SC#1 and SC#3 FAILED; SC#4 control-logic sound but carries the WR-06 honesty gap.

### Required Artifacts

| Artifact | Expected | Status | Details |
| --- | --- | --- | --- |
| `internal/config/villaconfig.go` | `AgentEnabled` gate field (omitempty, not self-healed) | ✓ VERIFIED | Present; agent-off encode omits the key (D-01). |
| `internal/agent/render.go` | restrictive `permissions.allowed_tools` + `options.disabled_tools` | ✓ VERIFIED | `disabledTools = [fetch, agentic_fetch, download, sourcegraph]` (line 58), rendered unconditionally (line 188); `allowed_tools = view/edit/write` (lines 138-140). BUT `servedModelID` resolves the CHAT model (lines 149-154) because the caller never sets `CoderModel` — see CR-01. |
| `cmd/villa/install_agent.go` | coderShardFor, presence-skip, ensure-download, agent.Install compose, render, evalAgentProof | ✓ EXISTS / ⚠️ WR-05 | All seams present; the readiness probe (line 298) is presence-only, not a replacement assertion. |
| `cmd/villa/install.go` | `--coding-agent` flag + gate block + pre-stage/install/render/readiness | ⚠️ WIRED-WRONG | Flag, gate, FSL notice, pre-stage, binary install, render, readiness all wired — but the coder render inputs are never set, so the staged coder is dead disk (CR-01). |
| `cmd/villa/preflight_agent.go` | runAgentChecks (disk/envelope BLOCK, cloud-cred WARN, typed-Unknown WARN) | ✓ VERIFIED | All four check tiers present and folded only when enabled. |
| `cmd/villa/uninstall.go` | removeAgentBinary + removeCrushConfig ordered seams | ✓ VERIFIED | Present, ordered, idempotent, traversal-guarded. |
| `cmd/villa/verify_agent.go` | evalAgentVerify (negative-control-first) + live seam + drivers | ✓ EXISTS / ⚠️ WR-01,WR-06 | Pure core ordering correct; live egress probe false-greens (WR-01); restore error swallowed (WR-06). |
| `cmd/villa/verify.go` | newVerifyAgent registered under the verify parent | ✓ VERIFIED | Registered; addon-off exits 0 with a clear message. |

### Key Link Verification

| From | To | Via | Status | Details |
| --- | --- | --- | --- | --- |
| `install.go` --coding-agent path | `orchestrate.RenderInput.CodingMode` | should serve the staged coder | ✗ NOT_WIRED | CodingMode never threaded; the unit serves the chat model (CR-01 root cause). |
| `install.go` | `coderShardFor(rec, cat)` | pre-stage gated on AgentEnabled | ✓ WIRED | Resolved + staged (install.go:540-556) — but result never feeds the render. |
| install readiness | `evalAgentProof(toolCall)` | real crush-run round-trip | ⚠️ PARTIAL | Wired, but the toolCall asserts presence-only, not replace (WR-05). |
| `evalAgentVerify` | egressBlocked → agentTask → llamaDownTask | negative-control-first | ⚠️ PARTIAL | Ordering correct; egressBlocked false-greens on infra failure (WR-01). |
| `liveAgentVerify` | systemd Stop/Start(villa-llama) | deferred restore | ⚠️ PARTIAL | Restore runs but the Start error is discarded (WR-06). |
| `runUninstall` | removeAgentBinary / removeCrushConfig | ordered teardown | ✓ WIRED | Invoked at a deterministic asserted position. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Seam grep-gate (no leaked backend/image literal) | `go test ./internal/inference/ -run TestSeamGrepGate` | 1 passed | ✓ PASS |
| INSTALL-04 + PRIV-06 structural tests | `go test ./cmd/villa/ -run 'TestUninstall\|TestAgentPreflight\|TestEvalAgentVerify\|TestVerify\|TestCoderShard\|TestEvalAgentProof'` | 49 passed | ✓ PASS |
| Full pre-commit gate | `make check` | all packages ok | ✓ PASS |

Note: the green test suite reflects the unit contracts AS WRITTEN; it does not catch CR-01 because no test asserts the rendered served id == `rec.Coder.Model` on the --coding-agent path (a gap the closure plan must add). The on-hardware acceptance (27-04) recorded a PASS, but that PASS was against the chat model — the confirmation that surfaced CR-01.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| --- | --- | --- | --- | --- |
| INSTALL-03 | 27-01, 27-04 | Coding agent as optional install addon with real tool-call readiness | ✗ BLOCKED | Addon wiring present but stages a coder that is never served; readiness proves the chat model (CR-01). Plus WR-05 presence-only readiness. REQUIREMENTS.md marks `[x]` — premature given the open BLOCKER. |
| INSTALL-04 | 27-02 | Honest preflight gates + uninstall removes binary/config/artifacts | ✓ SATISFIED | preflight_agent.go gates (disk/envelope/cloud-cred/typed-Unknown); uninstall.go ordered teardown. Tests green. |
| PRIV-06 | 27-03, 27-04 | `villa verify agent` zero-outbound negative-control-first + llama-down no-cloud-fallback | ✗ BLOCKED | Structure + on-hardware egress/llama-down controls ran PASS, but the egress negative control false-greens on broken probe infra (WR-01) and the restore error is swallowed (WR-06); the agentTask shares the presence-only readiness probe (WR-05). |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| --- | --- | --- | --- | --- |
| `cmd/villa/install.go` | 528-556 | Resolve+stage coderShard that is then never referenced (dead-staged disk) | 🛑 Blocker | Multi-GB coder weight downloaded, never served; gate basis diverges from runtime (CR-01) |
| `cmd/villa/verify_agent.go` | 129-134 | `err != nil` interpreted as success (false-green negative control) | 🛑 Blocker | Broken probe env passes the PRIV-06 egress gate (WR-01) |
| `cmd/villa/install_agent.go` | 298 | `Contains(TOKEN_B)` presence-only success (no replace assertion) | ⚠️ Warning | Append/echo false-greens the readiness/verify contract (WR-05) |
| `cmd/villa/verify_agent.go` | 149 | Discarded deferred `Start` error | ⚠️ Warning | villa-llama may be left stopped silently (WR-06) |

(Additional review INFO/WARNINGs — WR-02 missing HTTP timeout, WR-03 no HTTPS assert, WR-04 dead size guard, IN-01..IN-04 — are not folded into the gap list per the team's scoping decision; they remain documented in 27-REVIEW.md for optional follow-up.)

### Human Verification Required

None outstanding — the on-hardware acceptance (27-04) was already run; it is the source of the CR-01 confirmation. No new human-verification items are introduced by this verdict.

### Gaps Summary

Phase 27 delivers INSTALL-04 (honest preflight gates + uninstall coverage) solidly, and the PRIV-06 verify-agent structure, seam discipline (grep-gate, traversal guards, no-shell-interpolation, restrictive-tools render, helper image from the seam accessor), and config byte-identical-off coverage are sound; `make check` is green and the on-hardware egress mechanism (rootless-netns nft FORWARD drop) was captured.

The phase goal is NOT achieved because of one BLOCKER plus three honesty defects, all confirmed at the cited lines:

- **CR-01 (BLOCKER):** `--coding-agent` stages a coder GGUF that is never served — `CodingMode`/`CoderModel`/`CoderAgentCtx` are never set, so the inference unit + crush.json + both proofs target the CHAT model. The headline feature configures and proves readiness against the wrong model. Fix: single-source the coder render inputs from `rec.Coder` and thread `CodingMode` into the render; add a served-id assertion test.
- **WR-01:** the egress negative control treats any probe failure as "blocked" — a broken probe environment false-greens PRIV-06. Fix: distinguish "host unreachable" from "probe could not run".
- **WR-05:** readiness asserts `Contains(TOKEN_B)` only — an append/echo false-greens. Fix: also require `!Contains(TOKEN_A)`.
- **WR-06:** the llama-down restore discards the `Start` error — villa-llama can be left stopped silently. Fix: surface the restore error + remediation.

These four are scoped for `/gsd-plan-phase 27 --gaps`. CR-01 + WR-05 share the crush-run driver, so fixing the served model and the replace-assertion can be planned together; WR-01 + WR-06 are both in `verify_agent.go`.

---

_Verified: 2026-06-14_
_Verifier: Claude (gsd-verifier)_
