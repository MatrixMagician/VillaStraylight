---
phase: 27-install-addon-preflight-gates-villa-verify-agent
plan: 04
subsystem: acceptance
tags: [on-hardware, acceptance, crush-run, tool-call, egress, llama-down, gfx1151, checkpoint, priv-06, install-03, partial]

# Dependency graph
requires:
  - phase: 27-install-addon-preflight-gates-villa-verify-agent
    plan: 01
    provides: "install_agent.go evalAgentProof readiness + liveAgentToolCallProbe crush-run driver + agentProbePrompt/Token{A,B} payload constants"
  - phase: 27-install-addon-preflight-gates-villa-verify-agent
    plan: 03
    provides: "verify_agent.go evalAgentVerify two-control core + liveAgentVerify (egress negative-control + llama-down) — agentTask reuses liveAgentToolCallProbe (DRY)"
  - phase: 24-coder-fit-math
    provides: "recommend coder pick (qwen3-coder-next-q4, residency swap); D-13 build-9496-vulkan-radv qualification scope"
provides:
  - "On-hardware confirmation (Open Q1 RESOLVED): the deterministic crush run planted-file read->edit->result payload forces a verifiable tool-call edit on the live served model + restrictive-tools crush.json with NO TTY prompt, exit 0, deterministic across 2 runs"
  - "Confirmation that agentProbePrompt/agentProbeTokenA/agentProbeTokenB need NO tuning — the placeholder constants are confirmed verbatim; shared byte-identically by both drivers (install readiness + verify agent) via liveAgentToolCallProbe"
affects: [phase-28-surfacing, milestone-v1.4-close]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "On-hardware payload confirmation without source mutation: the placeholder constants from Plans 01/03 were proven verbatim, so Task 2 produced acceptance evidence rather than a code commit"

key-files:
  created:
    - .planning/phases/27-install-addon-preflight-gates-villa-verify-agent/27-04-SUMMARY.md
  modified: []

key-decisions:
  - "No payload tuning: agentProbePrompt + TOKEN_A/TOKEN_B confirmed deterministic verbatim on the live ROCm 7.2.4 served model with permissions.allowed_tools auto-accept (no --yolo, no TTY) — Open Q1 resolved with the placeholder constants unchanged"
  - "Tasks 1 and 3 surfaced as a genuine blocking-human checkpoint: applying a real host outbound egress block (sudo/firewall) and the 46.2 GiB coder-GGUF install + transactional backend swap acceptance are operator-gated and were NOT fabricated"

requirements-completed: []
requirements-partial: [INSTALL-03, PRIV-06]

# Metrics
duration: ~25min
completed: 2026-06-14
status: PARTIAL — Task 2 accepted on-hardware; Tasks 1 + 3 operator-gated (blocking-human checkpoint)
---

# Phase 27 Plan 04: On-Hardware Acceptance Summary (PARTIAL — operator checkpoint open)

**Open Q1 is RESOLVED on the live gfx1151 box: the deterministic `crush run` planted-file read→edit→result payload (`VILLA_PROBE_TOKEN_A` → `VILLA_PROBE_TOKEN_B`) forces a verifiable tool-call edit with NO TTY prompt, exit 0, deterministically across two independent runs — confirming the placeholder payload constants verbatim (no tuning). The two proofs that require host privileges I do not have — the operator-applied egress block (Task 1) and the full `villa install --coding-agent` readiness + `villa verify agent` egress/llama-down acceptance (Task 3) — remain a genuine blocking-human checkpoint and were NOT simulated.**

## Host context (live box, as found)

- Host: `neurodev` (gfx1151 AMD Strix Halo), user `oliverh`.
- Stack up: `villa-llama.service` **running on ROCm 7.2.4 (HIP)** — note the D-13 backend caveat: the coder qualification + `cache_reuse_safe` claim are **build-9496-vulkan-radv-scoped**; this box currently serves the chat model on ROCm. `villa-openwebui`, `villa-dashboard`, `villa-qdrant`, `villa-embed` all active.
- Crush binary staged at `~/.local/share/villa/bin/crush` — `crush version v0.76.0` (the pinned, checksum-verified release).
- Loopback inference endpoint `http://127.0.0.1:8080/v1/models` **reachable (exit 0)**, currently serving the **chat** model `Qwen3.6-35B-A3B-UD-Q4_K_M.gguf` (the coder `qwen3-coder-next-q4` is residency: swap — not yet swapped in; that is Task 3).
- `agent_enabled` is NOT set in `~/.config/villa/config.toml` (addon not yet installed since Plan 01 landed) → `villa verify agent` would currently exit 0 "nothing to verify". `--coding-agent` (Task 3) flips + persists it.

## Task outcomes

### Task 2 — CONFIRMED on-hardware (Open Q1 RESOLVED) ✅

The deterministic `crush run` tool-call payload was exercised against the **live served model** using a Phase-27-rendered `crush.json` (restrictive tools present: `options.disabled_tools` = fetch/agentic_fetch/download/sourcegraph AND `permissions.allowed_tools` = view/edit/write).

**Exact payload (the confirmed constants — `install_agent.go:258-263`, verbatim, NO tuning):**

- `agentProbeTokenA` = `VILLA_PROBE_TOKEN_A`
- `agentProbeTokenB` = `VILLA_PROBE_TOKEN_B`
- `agentProbePrompt` = `Open the file villa-readiness-probe.txt, replace the text VILLA_PROBE_TOKEN_A with VILLA_PROBE_TOKEN_B, and save it. Reply DONE when finished.`
- `permissions.allowed_tools` = `["view","edit","write"]` (auto-accept; **no `--yolo`** — rejected with `run` in v0.76.0, per 24-03/26-03)

**Procedure run (mirrors `liveAgentToolCallProbe` exactly):** plant `villa-readiness-probe.txt` containing `VILLA_PROBE_TOKEN_A` in a fresh temp working dir → `crush run --quiet "<agentProbePrompt>"` with **stdin from `/dev/null`** (proving no interactive TTY prompt is required) → assert the file now contains `VILLA_PROBE_TOKEN_B` and `crush run` exited 0.

**Results:**

| Run | crush run exit | File before | File after | TTY prompt | Verdict |
|-----|---------------|-------------|------------|------------|---------|
| 1 | 0 (printed `DONE`) | `VILLA_PROBE_TOKEN_A` | `VILLA_PROBE_TOKEN_B` | none (stdin /dev/null) | EDIT CONFIRMED |
| 2 | 0 (printed `DONE`) | `VILLA_PROBE_TOKEN_A` | `VILLA_PROBE_TOKEN_B` | none (stdin /dev/null) | EDIT CONFIRMED |

A real read→edit→result loop (the tool-bearing analog of Phase-26's PONG), deterministic across two independent runs, bounded by a 240 s timeout (no timeout occurred — a timeout would be a FAIL). Backend: **ROCm 7.2.4** (D-13 caveat noted; the payload mechanism is model/backend-agnostic).

**Payload identity across both drivers (DRY, by construction):** the constants are defined ONCE in `install_agent.go` (`agentProbePrompt`, `agentProbeTokenA/B`) and consumed by `liveAgentToolCallProbe`. `verify_agent.go` wires `agentTaskFn: liveAgentToolCallProbe` (`liveVerifyAgentDeps`), so the install readiness driver and the verify-agent `agentTask`/`llamaDownTask` drive the **identical** tool-call round. No source edit was needed; nothing diverged to keep in sync.

**`make check`:** GREEN across all 24 packages (vet + `go test ./...`).

### Task 1 — OPERATOR-GATED (blocking-human checkpoint) ⛔

Applying a **real host outbound egress block** is a privileged operator network action (the same precondition `villa verify memory` uses) with **no villa CLI/API seam** (D-07 forbids new cap-root tooling). I confirmed the block is currently **NOT applied** and do not have host firewall/sudo privileges to apply it, so I cannot complete this task and must not fabricate it.

**Current (block NOT applied) — captured for the operator baseline:**

- Container egress probe on `villa.network` (the exact `runProbeCurl` mechanism `villa verify agent` uses):
  `podman run --rm --network villa --entrypoint curl docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…ac7aad -sf --max-time 5 https://huggingface.co/`
  → currently **SUCCEEDS** (egress OPEN — block not yet applied).
- Loopback `curl -sf --max-time 5 http://127.0.0.1:8080/v1/models` → **SUCCEEDS** (exit 0).

**What the operator must do (and capture):** apply the host outbound egress block the same way the Phase-20 / `verify memory` acceptance did, then confirm:
1. the container probe above **FAILS** (non-zero) under the block, AND
2. loopback `/v1/models` **still succeeds**.
**Capture the EXACT operator block command in this SUMMARY** (the Phase-20 artifacts did not record one — T-27-20; this gap is still open pending the operator-supplied command).

### Task 3 — OPERATOR-GATED (blocking-human checkpoint) ⛔

The full addon acceptance depends on Task 1's live block AND requires actions I cannot/should not perform autonomously:
- `villa install --coding-agent` pre-stages a **46.2 GiB** coder GGUF (`qwen3-coder-next-q4`, residency: swap) and performs a **transactional backend swap** to serve the coder on the loopback endpoint — a heavy, state-mutating operation that must be operator-initiated on the live box.
- `villa verify agent` ctrl2 **stops `villa-llama`** then restores it — must run with the egress block live and be observed end-to-end.
- An agent that answers with inference down, a simulated egress block, or a skipped llama-down control would be a FAIL, not a pass (honesty-by-construction).

**What the operator must run (with Task 1's block applied) and record here:**
1. **INSTALL READINESS (INSTALL-03 / D-05):** `villa install --coding-agent` → confirm it stages exactly the picked coder GGUF (presence-skip on a 2nd run), installs the pinned checksum-verified crush binary, renders `crush.json` (one loopback provider, kill switches, `villa-` model id, restrictive tools), and PASSES readiness via a **real planted-file edit** (NOT a health-200).
2. **VERIFY AGENT ctrl1 (PRIV-06 egress, negative-control-FIRST):** `villa verify agent` → the external egress probe FAILS under the block FIRST, THEN the real `crush run` task COMPLETES while egress is blocked.
3. **VERIFY AGENT ctrl2 (no cloud fallback):** with `villa-llama` stopped the SAME task FAILS (no answer — no silent cloud fallback), and `villa-llama` is RESTORED (running) afterward.
4. Overall `villa verify agent` verdict PASS only when ctrl1 passes AND ctrl2 fails-as-expected; exit 0 on PASS.
5. Record the exact operator egress-block command, the restored-`villa-llama` confirmation, and the **backend the acceptance ran on** (note ROCm 7.2.4 vs the build-9496-vulkan-radv coder qualification scope, exactly as 26-03 did).

## Deviations from Plan

**1. [Rule 3 — Blocking, resolved] Task 2 run order vs the Task 1 checkpoint.**
The plan orders Task 1 (operator egress block) as a blocking-human checkpoint BEFORE Task 2. Because Task 2 (payload confirmation) does NOT depend on egress being blocked — it needs only the live served model + crush binary + restrictive-tools config — and the orchestrator is in AUTO mode instructing me to "attempt every step I genuinely can run," I executed Task 2 first to resolve Open Q1 and de-risk the operator's Task 3 run. This does not weaken any proof: the egress block is strictly required only for the Task 3 controls, which remain operator-gated.

**2. Box left as found.** To run Task 2, a Phase-27 `crush.json` was rendered to `~/.config/crush/crush.json` (derived artifact; Task 3's install re-renders it). The pre-existing pre-Phase-27 `crush.json` was backed up to `/tmp/crush.json.pre27.bak` and **restored** afterward; no villa service state was mutated; `agent_enabled` was NOT flipped; no coder GGUF was pulled; no backend was swapped. A one-off render driver placed under `internal/_task2render/` was removed (not committed).

## Self-Check: PASSED
- Created file verified on disk: `.planning/phases/27-install-addon-preflight-gates-villa-verify-agent/27-04-SUMMARY.md`.
- Task 2 evidence is from real on-hardware `crush run` invocations (two independent runs, both exit 0 + TOKEN_B edit, no TTY).
- No source files were modified (payload confirmed verbatim) → no per-task code commit for Task 2; no fabricated commit is claimed.
- Tasks 1 & 3 are honestly reported as operator-gated (blocking-human), not simulated.
