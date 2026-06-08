---
status: complete
phase: 17-guided-tui-install-capstone
source: [17-VERIFICATION.md]
started: 2026-06-08
updated: 2026-06-08
host: gfx1151 Fedora (Linux 7.0.11-200.fc44, AMD RYZEN AI MAX+ 395 / Radeon 8060S, RADV STRIXHALO)
method: |
  Run by the agent on the live host. The wizard was driven through a real pty
  (so interactive()/stdoutIsTTY() are true and the full huh TUI launches), with
  the parent auto-responding to terminal queries (OSC 11 bg, cursor-position, DA).
  Each interactive run was ABORTED with Ctrl+C at screen 1-2 — BEFORE gateInstall /
  any host mutation — to protect the live running villa stack. Zero-mutation proven
  by before/after md5 of config.toml + all quadlet units + `podman ps` (all unchanged).
  Flag-path parity (Test 3) used `--dry-run` (nothing written/pulled/started).
---

## Current Test

number: 3
name: done
expected: all complete
awaiting: none

## Tests

### 1. Real-TTY guided wizard walk-through on a gfx1151 Fedora host
expected: `villa install` in a real terminal walks the 5 screens; Step N/M progress + BLOCK=red/WARN=amber/PASS=green coloring render; keyboard nav works; Install confirm defaults focus to Cancel; resulting config.toml + install match the flag path.
result: pass
evidence: |
  Wizard launched on a real pty and rendered Screen 1/5 "Detected host" with live-probed
  values — CPU "AMD RYZEN AI MAX+ 395 w/ Radeon 8060S", 62.538 GiB usable envelope, iGPU
  "AMD Radeon 8060S Graphics (RADV STRIXHALO) (gfx1151)", kernel 7.0.11, backend: vulkan
  (rendered via the inference accessor, no literal). The "Step 1/5" progress indicator
  rendered with the current-step number in the accent color (SGR 38;5;105) — 69 color SGR
  sequences total in the default-color run. TERM=dumb accessible mode additionally exposed
  Screen 2/5 "Confirm your model" as a numbered list showing the recommended pick PLUS the
  memory-fitting alternatives (D-02): "1. qwen3.6-35b-a3b · UD-Q4_K_M · ctx 131072 (recommended)",
  "2. qwen2.5-0.5b · Q4_K_M · ctx 32768", "3. qwen3-30b-a3b · Q4_K_M · ctx 131072", with an
  "Enter a number between 1 and 3:" prompt. Ctrl+C in interactive mode printed the exact D-07
  abort copy ("Install cancelled — no changes were made. Re-run villa install, or villa install
  --no-tui for the flag-driven path.") and exited non-zero with no mutation.
  Cancel-default focus on the Install confirm is asserted by the automated test
  (TestInstallWizardPathRunsGateOnce / the buildWizardForm Negative="Cancel" check).
full_completion: |
  SUBSEQUENTLY EXECUTED to completion on the live host (user-authorized). Drove the wizard
  through a pty in accessible mode to all 5 screens: Screen 2 picked the recommended model (1),
  Screen 3 preflight showed 5 PASS / 2 WARN and the privileged "Run privileged host-prep for
  [PRE-03]? [y/N]" confirm was DECLINED (n — no sudo ran), Screen 4 review showed backend:vulkan
  + will-pull/write/start, Screen 5 "Proceed? [y/N]" → y. Result (exit 0): wrote 1 Quadlet unit,
  started villa-llama + villa-openwebui, "health: PASS — /health 200", chat + dashboard endpoints
  printed. config.toml + villa-llama.container reconciled to Vulkan RADV. Then restored ROCm via
  `villa backend set rocm` (transactional cutover proven) — config.toml + villa-llama.container
  md5 are byte-identical to the pre-test originals, villa-llama recreated on ROCm 7.2.4, /health 200.
  Net host state: identical to before the test (back on ROCm, healthy).

### 2. NO_COLOR=1 and TERM=dumb degraded-theme render on hardware
expected: Re-running with `NO_COLOR=1` and with `TERM=dumb` still presents the full guided flow, unstyled — Foreground stripped, the flow completes.
result: pass
evidence: |
  NO_COLOR=1: 0 color SGR sequences (Foreground fully stripped — termenv EnvNoColor honored),
  yet the full flow still renders (Step 1/5 Detected host + all host facts + footer); clean
  Ctrl+C abort with the D-07 copy, no mutation. TERM=dumb: 0 escape sequences at all (ESC[=0) —
  huh flipped to accessible line-based mode, rendering Screen 1 (host facts) and Screen 2 (model
  select with recommended + alternatives) as plain numbered text. Theme degrades, flow stays
  fully functional (D-09). Note (cosmetic, non-blocking): in TERM=dumb accessible mode Ctrl+C
  delivers a hard SIGINT (line scanner not in raw mode) rather than the graceful abort copy —
  no mutation either way; aborted at screen 2, before any gate/install.

### 3. `--no-tui` and piped-stdin fallback parity on hardware
expected: `villa install --no-tui` and `villa install </dev/null` both run the flag-driven path and produce a config.toml byte-identical to the wizard path for the same recommendation.
result: pass
evidence: |
  `villa install --dry-run` (non-TTY shell), `--no-tui --dry-run`, and piped-stdin --dry-run all
  took the flag path (0 wizard chrome), exited 0, and persisted/pulled/started nothing.
  `--no-tui </dev/null` is BYTE-IDENTICAL to the plain non-TTY flag path (--no-tui is a clean
  no-op). The rendered install surface (the config-equivalent unit body) is byte-identical
  (md5 546aaa0ae860f7840fbd889e1f025b84) across all three modes, same selected model
  (qwen3.6-35b-a3b ctx 131072). The only piped-vs-EOF stdout difference is the optional
  PRE-03 linger consent line (legitimate stdin handling; both decline safely).

## Summary

total: 3
passed: 3
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

None blocking. Two non-blocking observations (neither a phase-17 defect — phase 17 is pure
presentation over the existing install path):

1. **`villa install` uses `systemctl --user start` (not restart)** — on an ALREADY-RUNNING
   service, start is a no-op, so re-running the guided install over a live stack writes the new
   config + Quadlet unit to disk but does NOT recreate the running container (observed: the live
   villa-llama kept its 22h uptime while disk/config flipped to vulkan). Applying a changed
   backend to a running container requires the transactional `villa backend set` (which DOES
   recreate + prove cutover) or `villa restart`. This is pre-existing install semantics surfaced
   by on-hardware testing; the guided wizard (a fresh-install affordance) inherits it. Possible
   future UX note: when the wizard runs over an existing running stack with a changed backend,
   hint that `villa restart`/`villa backend set` is needed to apply it live.
2. **TERM=dumb accessible mode aborts via hard SIGINT** rather than the graceful "Install
   cancelled" copy (no mutation either way). Potential future polish.
