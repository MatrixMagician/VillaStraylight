---
phase: 26-agent-delivery-core-lockdown-launcher
plan: 03
completed: 2026-06-13
requirements-completed: [AGENT-01, AGENT-04]
requirements-touched: [AGENT-03]
affects: [27 install addon + villa verify agent, 28 status/doctor/backup surfacing]
status: COMPLETE — on-hardware acceptance PASSED (with one finding found+fixed)
---

# 26-03 SUMMARY — On-hardware pin + `villa code` launch acceptance

**Completed:** 2026-06-13 (gfx1151 dev box; operator authorized the live stack as a test target)

## Status: COMPLETE — on-hardware acceptance PASSED

Both tasks done. Task 1 (pin the real binary hash) is committed; Task 2 (live launch
acceptance) PASSED on-hardware and **surfaced a real D-12 defect that was fixed and
re-verified on-hardware**. `make check` green across all packages.

## Task 1 — Pinned the extracted-binary SHA-256 (Q2 / Pitfall 6) — commit `29bc674`

Fetched the pinned release on the box and verified BEFORE extraction (D-03), then
derived the binary hash from the verified artifact:

- **Tarball** `crush_0.76.0_Linux_x86_64.tar.gz`: size **25155696** ✓, sha256
  **`0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9`** ✓ (== policy; verified before extract)
- **Extracted binary** (`crush_0.76.0_Linux_x86_64/crush`, ELF x86-64, `crush version v0.76.0`):
  sha256 **`4fd811f68c05da6c8d11fd1d5b6298a75ecc38a6c105a342b74e080cce8342b4`** — pinned into `crush-policy.json` `binarySha256` (sentinel replaced).

With a real policy hash, binary-drift (D-14a / AGENT-04) is now a **confident** signal
instead of a typed-Unknown WARN. `TestPolicyLoad` flipped to assert the pinned hash;
the sentinel-degradation path stays covered via constructed `DriftInput`s
(`TestBinaryDrift`). Run-flow fakes (install_test.go, code_test.go) carry the pinned
hash on non-drift paths so clean/first-run/coding-off paths exercise a non-drifted binary.

**Tarball-vs-binary path note (verified):** the binary lives at the nested
`crush_0.76.0_Linux_x86_64/crush` entry, not top-level. The install seam matches the tar
entry by **base name** (`install.go` `filepath.Base(cleanName) == "crush"`) and flattens
to `binDir/crush`, so it installs correctly — confirmed on the real tarball.

## Task 2 — On-hardware `villa code` launch acceptance (AGENT-01/AGENT-03/AGENT-04 E2E)

Live environment: `villa status` → overall PASS, **backend `rocm`** (caveat below),
villa-llama active/ready serving `Qwen3.6-35B-A3B-UD-Q4_K_M.gguf` (chat model; coding
mode OFF), loopback-only on `127.0.0.1:8080`, "no telemetry".

Observations (binary installed to `~/.local/share/villa/bin/crush`, hash == pinned;
no pre-existing `~/.config/crush/crush.json`):

1. **First-run render (config-absent → render-then-launch, no false drift) — PASS.** First
   `villa code` rendered `~/.config/crush/crush.json` from `config.toml`: both kill
   switches (`disable_metrics`, `disable_provider_auto_update`) + `disable_default_providers:true`
   + `auto_lsp:false`; exactly one `openai-compat` **villa** provider at
   `http://127.0.0.1:8080/v1`; villa-unique model id `villa-qwen3.6-35b-a3b`; LSP block
   (go→gopls, python→pyright-langserver). No false config-drift, no binary-absent block.
2. **Real prompt round-trip wired to local inference — PASS.** `crush run "...PONG"` with the
   villa-rendered config + lockdown env answered **`PONG`** (exit 0). stderr clean — no
   telemetry / autoupdate fetch. `disable_default_providers` left only the villa loopback
   provider reachable. **Q1 (render-only model-id) CONFIRMED:** Crush requested
   `villa-qwen3.6-35b-a3b`, llama.cpp served the GGUF and answered — single-model leniency holds;
   no `--alias` / inference-seam delta needed.
3. **Second run (present + matching → no auto-correct) — PASS.** Re-ran `villa code`:
   `crush.json` was **not** re-written (identical sha256 + mtime) — present-but-matching is a
   clean no-write (D-14).
4. **Drift negative-control — PASS.** Hand-edited `crush.json` (flipped `disable_metrics`→false);
   `villa code` **refused** with remediation ("refusing — on-disk crush.json differs … villa
   surfaces drift but never overwrites your file automatically") and did **not** auto-correct
   (sha unchanged) — D-14 confirmed.

### FINDING (fixed) — D-12 coding-off WARN was lost across `syscall.Exec` — commit `ca7f598`

The first-run launch surfaced a real defect: `agent.Run` called `d.Launch` (`syscall.Exec`)
**internally** on the clean/first-run path, replacing the process **before** `runCode` printed
the advisory warnings that were sequenced after `Run`. So on a *real* launch the
coding-mode-off WARN (D-12), the first-run-rendered notice, and lsp-missing WARNs were never
shown — they appeared only on non-launching refusal paths and in the test seam (fake `Launch`
returns instead of exec'ing). A test-vs-reality gap that the off-hardware suite could not catch.

**Fix (architecture-aligned):** `Run` no longer execs — it resolves the lockdown env into
`Result.LaunchEnv` and sets `Result.ReadyToLaunch`; `runCode` prints `Warnings`, **then**
performs the single `d.Launch(LaunchEnv)`. Cores return typed values; the command tier does
the I/O + exec. Re-verified on the box: first `villa code` now prints `lsp_missing` +
`config_rendered` + **`coding_mode_off`** WARNs before Crush launches. `make check` green.

### Caveats (honest, Phase-25-Task-3 discipline)

- **Backend is ROCm, not vulkan-radv** on this box right now. The launcher/render/drift logic
  is backend-agnostic (it points at the loopback endpoint + a model id), so this does not affect
  AGENT-01..04, but the live round-trip was observed on ROCm — recorded for parity with the
  Phase-25 scoping note.
- **Interactive TUI not driven** (no TTY in this automation session): `villa code`'s exec into
  the Crush TUI attaches a terminal, which crashes under non-interactive stdin (expected). The
  end-to-end model round-trip + lockdown env + provider wiring were proven via non-interactive
  `crush run` plus the villa render/launch flow; a human attaching a terminal will get the TUI.
- **Runtime egress negative-control is Phase 27 (PRIV-06)** — here only observed: stderr clean,
  no telemetry/autoupdate fetch, default cloud providers disabled.

## Requirements

- **AGENT-01** (pin policy + checksum-before-install + autoupdate-off): COMPLETE — real binary
  hash pinned from a verified tarball; install seam verified against the real artifact.
- **AGENT-03** (env-lockdown launcher): verified on-hardware — three kill-switch env vars applied
  before exec; coding-off WARN now correctly surfaced (D-12 fix).
- **AGENT-04** (drift detected + surfaced, never auto-corrected): COMPLETE — binary-drift now
  confident; config-drift refusal + no-auto-correct proven via negative control.

## Verification

- `go test ./internal/agent/ ./cmd/villa/ -count=1` green; `make check` green across all packages.
- On-hardware: first-run render + round-trip + no-rewrite + drift-refusal + WARN-before-launch all observed.

## Commits

- `29bc674` feat(26-03): pin real Crush v0.76.0 binary SHA-256 — binary-drift now confident
- `ca7f598` fix(26-03): surface villa code WARNs before exec — D-12 coding-off WARN now shown
