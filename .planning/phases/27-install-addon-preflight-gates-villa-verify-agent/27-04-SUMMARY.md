---
phase: 27-install-addon-preflight-gates-villa-verify-agent
plan: 04
subsystem: acceptance
tags: [on-hardware, acceptance, crush-run, tool-call, egress, llama-down, gfx1151, priv-06, install-03, complete]

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
  - "INSTALL-03 readiness ACCEPTED on-hardware: `villa install --coding-agent` PASSES via a real tool-call round-trip (read->edit->result), NOT a health-200; coder GGUF presence-skipped, pinned crush binary verified, locked-down crush.json rendered"
  - "PRIV-06 ACCEPTED on-hardware: `villa verify agent` PASS (exit 0) under a REAL host egress block — ctrl1 (egress proven blocked first, then blocked crush-run task completes) AND ctrl2 (llama-down task fails => no silent cloud fallback, villa-llama restored)"
  - "T-27-20 RESOLVED: the exact host egress-block command for rootless podman is captured (rootless-netns nft FORWARD drop) — the Phase-20 gap is closed"
affects: [phase-28-surfacing, milestone-v1.4-close]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Rootless-podman egress block lives in the ROOTLESS NETWORK NAMESPACE, not the host main netns: container egress is proxied (pasta) and traverses FORWARD inside the rootless netns (podman3 -> enp191s0), so the operator block must be applied there via `podman unshare --rootless-netns nft ...`"
    - "On-hardware acceptance without source mutation: the Plan-01/03 payload + seams were exercised end-to-end on the live box; no code change was needed (placeholder constants confirmed verbatim)"

key-files:
  created:
    - .planning/phases/27-install-addon-preflight-gates-villa-verify-agent/27-04-SUMMARY.md
  modified: []

key-decisions:
  - "No payload tuning: agentProbePrompt + TOKEN_A/TOKEN_B confirmed deterministic verbatim on the live ROCm 7.2.4 served model with permissions.allowed_tools auto-accept (no --yolo, no TTY) — Open Q1 resolved with the placeholder constants unchanged"
  - "Host egress-block mechanism (T-27-20) for rootless podman: apply an nft FORWARD drop INSIDE the rootless network namespace (`podman unshare --rootless-netns`), matching the villa.network subnet (10.89.2.0/24). A host-main-netns FORWARD rule does NOT block rootless egress (verified — see Negative finding) because pasta proxies container traffic outside the host FORWARD path"
  - "FINDING (bug, follow-up filed): `villa install` reverts a persisted `backend=rocm` to Vulkan — install.go:432 `cfg.Backend = rec.Backend` and recommend.Pick always defaults Vulkan (ROCm is opt-in, never auto-recommended). On this box install rewrote the unit + config to Vulkan; the runtime stayed ROCm only because `systemctl start` no-op'd the already-active service. Config-is-source-of-truth violation; reversible via `villa backend set rocm`"

requirements-completed: [INSTALL-03, PRIV-06]
requirements-partial: []

# Metrics
duration: ~25min (executor, Task 2) + on-hardware acceptance (orchestrator-driven, operator-authorized)
completed: 2026-06-14
status: COMPLETE — all three tasks accepted on-hardware (INSTALL-03 readiness + PRIV-06 egress/llama-down), box restored to as-found
---

# Phase 27 Plan 04: On-Hardware Acceptance Summary (COMPLETE)

All three tasks are accepted on the live gfx1151 box. **Task 2 (Open Q1)** confirmed the deterministic `crush run` payload verbatim. **Task 1** applied a real host egress block (rootless-netns nft FORWARD drop — the T-27-20 gap is now closed with an exact, reproducible command). **Task 3** ran the full acceptance: `villa install --coding-agent` PASSED readiness via a real tool-call round-trip (not a health-200), and `villa verify agent` returned PASS (exit 0) under the real block — ctrl1 (egress proven blocked first, then the blocked crush-run task completed) and ctrl2 (llama-down task failed => no silent cloud fallback), with villa-llama restored. No green was fabricated; an earlier ineffective block was correctly REJECTED by the verb (see Negative finding). The box was restored to its exact as-found state.

## Host context (live box)

- Host: gfx1151 AMD Strix Halo, user `oliverh`, **rootless** Podman (user systemd manager).
- As-found: `villa-llama.service` running on **ROCm 7.2.4 (HIP)** image `@sha256:2da150c1…`, serving the chat model `Qwen3.6-35B-A3B-UD-Q4_K_M.gguf` on `127.0.0.1:8080`; `villa-openwebui`/`villa-qdrant`/`villa-embed` up; `agent_enabled` unset; coder GGUF `Qwen3-Coder-Next-UD-Q4_K_XL.gguf` (47310.3 MiB ≈ 46.2 GiB) already staged at `~/.local/share/villa/models/`.
- Crush binary at `~/.local/share/villa/bin/crush` — `crush version v0.76.0` (pinned, checksum-verified).
- `villa.network`: bridge `podman3`, subnet **10.89.2.0/24** (rootless netns: `podman3` 10.89.2.1 → `enp191s0` 192.168.1.98, default route via 192.168.1.1).

## Task outcomes

### Task 2 — CONFIRMED on-hardware (Open Q1 RESOLVED) ✅

The deterministic `crush run` tool-call payload was exercised against the live served model with a Phase-27 restrictive-tools `crush.json` (`options.disabled_tools` = fetch/agentic_fetch/download/sourcegraph; `permissions.allowed_tools` = view/edit/write).

**Confirmed payload constants (`install_agent.go`, verbatim — NO tuning):**
- `agentProbeTokenA` = `VILLA_PROBE_TOKEN_A`; `agentProbeTokenB` = `VILLA_PROBE_TOKEN_B`
- `agentProbePrompt` = `Open the file villa-readiness-probe.txt, replace the text VILLA_PROBE_TOKEN_A with VILLA_PROBE_TOKEN_B, and save it. Reply DONE when finished.`
- `permissions.allowed_tools` auto-accept (no `--yolo`, rejected with `run` in v0.76.0); stdin `/dev/null` (no TTY).

Two independent runs: `crush run` exit 0, file `VILLA_PROBE_TOKEN_A` → `VILLA_PROBE_TOKEN_B`, no TTY prompt, bounded by 240 s (no timeout). The constants are defined once in `install_agent.go` and consumed by `liveAgentToolCallProbe`; `verify_agent.go` wires `agentTaskFn: liveAgentToolCallProbe`, so the install readiness driver and the verify-agent `agentTask`/`llamaDownTask` drive the identical round (DRY). `make check` GREEN (24 packages).

### Task 1 — ACCEPTED: host egress block applied + proven (T-27-20 RESOLVED) ✅

**Mechanism (the exact operator commands — rootless podman):** rootless container egress is proxied by pasta and does NOT traverse the host main-netns FORWARD chain; it traverses FORWARD **inside the rootless network namespace** (`podman3` → `enp191s0`). The block is therefore applied there:

```sh
podman unshare --rootless-netns nft add table inet villa_egress_block
podman unshare --rootless-netns nft add chain inet villa_egress_block forward '{ type filter hook forward priority -1 ; policy accept ; }'
podman unshare --rootless-netns nft add rule  inet villa_egress_block forward ip saddr 10.89.2.0/24 ip daddr != 10.89.2.0/24 drop
# removal:
podman unshare --rootless-netns nft delete table inet villa_egress_block
```

**Proven real under the block:**
- Container egress probe on `villa.network` (the `runProbeCurl` mechanism): `podman run --rm --network villa --entrypoint curl <vulkan-radv image> -sf --max-time 8 https://huggingface.co/` → **HTTP_000, exit 28 (timeout — BLOCKED)**.
- Loopback `curl -sf --max-time 8 http://127.0.0.1:8080/v1/models` → **HTTP_200 (still reachable)** — the agent's only allowed path stays open (host loopback is in the main netns, untouched by the rootless-netns rule).

**Negative finding (honesty-by-construction proven):** an initial block applied via the HOST main-netns FORWARD chain did NOT block rootless egress (container probe still HTTP_200), and `villa verify agent` correctly **FAILED (exit 1)**: *"egress is NOT blocked: an external host was reachable during the test."* The negative-control-FIRST design rejected the ineffective block rather than false-greening — direct evidence the proof is real.

### Task 3 — ACCEPTED: install readiness + verify-agent controls ✅

**1. INSTALL READINESS (INSTALL-03 / D-05):** `./villa install --coding-agent --no-tui` → exit 0. Output:
- FSL-1.1-MIT notice surfaced (informational).
- coder GGUF **presence-skipped** (already staged — no re-pull; no download line emitted).
- `coding agent installed and verified at ~/.local/share/villa/bin/crush` (pinned, checksum-verified binary).
- `coding-agent config rendered (outbound tools disabled, loopback provider only)`.
- **`coding agent ready: tool-call round-trip (read→edit→result) completed against the local endpoint`** — readiness PASSED via a REAL planted-file edit, NOT a health-200 (the separate `health: PASS — /health 200` line is the inference liveness, distinct from the agent proof which drives an edit).
- Second run: presence-skip + `no changes — stack already matches config` + `health: PASS — unchanged` (idempotent — confirms no re-pull on re-run).

**2. VERIFY AGENT ctrl1 + ctrl2 (PRIV-06):** `./villa verify agent` under the block → **PASS, exit 0**: *"zero-outbound agent task completed; no cloud fallback (llama-down control failed as expected)."*
- ctrl1 (negative-control-FIRST): the external egress probe FAILED under the block FIRST (proving the block is real), THEN the real `crush run` tool-call task COMPLETED while egress was blocked.
- ctrl2 (no cloud fallback): with `villa-llama` stopped the same task FAILED (no answer — no silent cloud fallback); `villa-llama` was RESTORED (active) after the verb returned.
- Overall verdict PASS only because ctrl1 passed AND ctrl2 failed-as-expected; exit 0.

**Backend the acceptance ran on (D-13 caveat):** **ROCm 7.2.4** — `villa install`'s `systemctl start` no-op'd the already-active service, so the running container stayed the as-found ROCm 7.2.4 container through the install readiness proof and verify ctrl1; verify ctrl2's stop/restart recreated it (briefly on Vulkan, from the install-rewritten unit) before restore. The coder qualification is build-9496-vulkan-radv-scoped; the payload + egress mechanisms are backend-agnostic.

## Finding (bug — follow-up filed)

`villa install` reverts a persisted `backend=rocm` to Vulkan: `install.go:432` does `cfg.Backend = rec.Backend`, and `recommend.Pick` always defaults to Vulkan (ROCm is strictly opt-in, never auto-recommended). On this box, `villa install --coding-agent` rewrote the `villa-llama` unit to the Vulkan image and persisted `backend = "vulkan"`; the runtime stayed ROCm only because `systemctl start` no-op'd the already-active service. This is a config-is-source-of-truth violation (a re-install silently discards a `villa backend set rocm` opt-in). Reversible via `villa backend set rocm`. Tracked as a follow-up todo for a maintenance fix (out of Phase-27 scope).

## Restore to as-found (post-acceptance)

`villa backend set rocm` (cutover proven) → `config.toml` restored from the as-found backup (now byte-identical: `backend=rocm`, `agent_enabled` unset) → `crush.json` restored. Verified: `villa-llama` back on ROCm 7.2.4 serving the chat model (HTTP_200), egress reopened (HTTP_200), all services up, no lingering nft block (host + rootless netns both clean).

## Self-Check: PASSED
- INSTALL-03 readiness PASSED via a real tool-call round-trip on the live box (not a health-200).
- PRIV-06 `villa verify agent` PASS (exit 0): egress proven blocked (negative-control-first) + no cloud fallback (llama-down FAILS), villa-llama restored.
- The egress block is real (container probe HTTP_000/exit 28; loopback HTTP_200) and its exact command is captured (T-27-20 closed).
- An ineffective block was correctly REJECTED by the verb (exit 1) — no fabricated PASS.
- Box restored to as-found (config byte-identical, ROCm 7.2.4 serving chat, egress open, services up, no lingering firewall state).
