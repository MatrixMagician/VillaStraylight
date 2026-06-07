# VillaStraylight

## What This Is

VillaStraylight is a self-hosted, local AI server stack for privacy-conscious power users who want a ChatGPT/Claude-class experience running entirely on their own hardware. A single Go CLI (`villa`) auto-detects the host hardware, recommends suitable models and configuration, generates Podman Quadlet units, and orchestrates a stack of OSS AI services (local inference, chat UI, and a control dashboard) — initially tuned for AMD Strix Halo on Fedora Workstation 44+, with macOS/Apple Silicon planned later.

## Core Value

**Run a capable local AI workspace that "just works" after install** — hardware-aware setup that picks the right models and config so inference, chat, and the control dashboard come up healthy on the user's machine, with zero data leaving the box.

## Current State

**v1.1 ROCm Opt-In Backend shipped 2026-06-06** (tag `v1.1`). All 6 phases (6–11) complete; milestone audit `tech_debt` (13/13 requirements satisfied, 0 critical blockers, highest-value debt closed inline by Phase 11). ROCm now ships as a strictly opt-in second inference backend behind the `BackendFor` resolver with a HIP residency proof (P6), a byte-frozen Quadlet render delta + refuse-with-remediation preflight + detect-readiness fields (P7), a transactional `villa backend set` switch with verbatim rollback (P8, 4/4 on-hardware UAT), an honest A/B `villa bench` (P9, live Δpp +4.84 / Δtg −11.15), and backend-aware `recommend`/`status`/dashboard surfacing (P10). Phase 11 made the `rocm_readiness` firmware/HSA detect probes real (live-verified on the gfx1151 host — `villa status` badge reads `ready`, backend `rocm`) and reconciled documentation drift. Vulkan RADV remains the default. 129 Go files, ~26.3k LOC, full suite green (16 packages).

**v1.0 MVP shipped 2026-06-05** (tag `v1.0`, PR #1 → `main`, merge `57f8ef7`). All 5 phases live-verified on real AMD Strix Halo (gfx1151) hardware: `villa` detects the host, recommends a memory-fitting model, installs a rootless Podman Quadlet stack (llama.cpp Vulkan inference + Open WebUI chat), and serves a read-only control dashboard — strictly local, zero telemetry. Phases 4 & 5 STRIDE-secured.

> **Note (release hygiene):** v1.1 reached `main` via PR #2 (merge `2e22d1f`) and is tagged `v1.1` on that merge commit — mirroring v1.0's PR #1 / tag-on-`main` pattern. Both shipped milestones are on `main`.

## Current Milestone: v1.2 Operability

**Goal:** Harden VillaStraylight into an operable, recoverable daily-driver — self-diagnosis, backup/restore, comparative benchmarking, usage history, a guided install, and a TG-tuned ROCm option — without weakening the v1.0 "just works" bar or the strictly-local posture.

**Target features:**
- **`villa doctor` (DOCTOR-01)** — one-shot health/diagnostics: re-run preflight against a running install, surface drift/faults with remediation.
- **Backup / restore (BAK-01)** — back up and restore config + Open WebUI data volume (chats, settings) for recovery/migration.
- **`villa bench --compare` + saved reports (BENCH-03)** — persist bench runs; compare over time / across models, building on the Phase 9 honest A/B core.
- **Cumulative usage tracking (USAGE-01)** — track token/throughput usage over time, surfaced in `status`/dashboard (not just live).
- **Guided TUI install (INSTALL-01)** — interactive terminal UI for first-run install/setup alongside the flag-driven CLI.
- **`rocm-6.4.4` alt image (ROCM-ALT-01)** — alternate ROCm image option tuned for token-generation-heavy models (addresses the v1.1 Δtg −11.15 regression).

**Deferred (not this milestone):**
- **Milestone 2 — Memory & Search:** Qdrant persistent memory, SearXNG search, OpenCode coding-agent wiring.
- **Future:** macOS / Apple-Silicon (Metal) backend, authenticated remote/multi-user access, voice (Whisper/Kokoro), agents/orchestration, image generation, ROCm perf-tuning knobs (hipBLASLt/rocWMMA-FA/batch).

## Requirements

### Validated

<!-- Shipped and confirmed valuable. -->

- [x] Hardware auto-detection: identifies CPU/arch, GPU, and total/unified memory on the host — *Validated in Phase 1: `villa detect` reads ghw + amdgpu sysfs on the live Strix Halo host; usable envelope = GTT `mem_info_gtt_total` (62.5 GiB), not MemTotal*
- [x] Recommendation engine: maps detected hardware to a recommended model + quantization + context length + service config — *Validated in Phase 1: `villa recommend` shows the `model_bytes + KV@ctx + headroom ≤ envelope` fit math over a versioned go:embed catalog*
- [x] Non-optional preflight gate refuses/warns before unsafe install (reusable `internal/preflight`, exit 0/2/1) — *Validated in Phase 1*
- [x] llama.cpp `llama-server` inference running on the AMD Strix Halo iGPU via the Vulkan backend, exposing an OpenAI-compatible API — *Validated in Phase 2: real Vulkan-RADV iGPU offload (log + sysfs dual-assert), loopback OpenAI API, live chat completion*
- [x] Single `villa` Go CLI installs and controls the whole stack (install, up/down, status, model management) — *Validated in Phase 3: full lifecycle verb set, idempotent install, config-as-source-of-truth*
- [x] Podman Quadlet (systemd) orchestration: Go generates and manages `.container`/`.network`/`.volume` units, rootless, start-on-boot — *Validated in Phase 3*
- [x] Open WebUI integrated as the chat front end, wired to the local inference API — *Validated in Phase 4: container-DNS wiring to `http://villa-llama:8080/v1`, telemetry killed, durable `:Z` volume; live browser chat + restart persistence confirmed on the gfx1151 host (UAT 4/4)*
- [x] Strictly local operation: all services bind to localhost/LAN; no telemetry; only outbound traffic is image/model downloads during install/update — *Validated through Phase 5: PRIV-01/02/03 loopback + no-telemetry gates, `villa status` loopback-only assertion, SECURED audits (Phase 4 12/12, Phase 5 19/19 STRIDE, threats_open=0)*
- [x] Control dashboard showing service health, performance metrics, token/throughput usage, and available/loaded models — *Validated in Phase 5: read-only dashboard as a read-model over the same internal API as `villa status`; amdgpu-sysfs GPU panel, 409-guarded model switch, chat link; all 5 UAT items live-verified on the gfx1151 host*
- [x] Runs on Fedora Workstation 44+ on AMD Strix Halo — *Validated v1.0: continuously exercised on the live gfx1151 host through all 5 phases, incl. reboot boot-survival*
- [x] Full ROCm preflight + detection (rocminfo/gfx1151, kernel floor, firmware-20251125 block, HSA override) — refuse-with-remediation — *Validated Phase 7 (off-hardware) + Phase 11 (on-hardware): `villa preflight --backend rocm` refuses only on positively-known-bad with named remediation, driven by a `go:embed` `rocm-policy.json`; `villa detect --json` appends a `rocm_readiness` block (schema 1→2, append-only). Phase 11 made the firmware/HSA probes real — live gfx1151 host reports `ready`.*
- [x] ROCm/HIP inference backend, opt-in, behind the existing `Backend` interface, offload-asserting (no false-green CPU fallback) — *Validated v1.1 (Phase 6): `backend_rocm.go` (digest-pinned `rocm-7.2.4`, kfd+dri, render group, HSA→hipBLASLt env, ROCm0 residency markers) selected via the single `BackendFor()` resolver that fails closed; a silent/partial CPU fallback is a FAIL. Vulkan stays the default, byte-identical.*
- [x] `villa backend set rocm|vulkan` switches the inference backend on a running install (fit-guarded, readiness-polled, rollback-safe) — *Validated v1.1 (Phase 8): transactional `internal/backendswap` capture→mutate→prove→rollback; cutover gated on a real generation-probe + residency proof; auto-rolls back verbatim on any failure. 4/4 on-hardware UAT incl. forced CPU-fallback rollback + bounded 5m timeout.*
- [x] `villa bench` proves the Vulkan-vs-ROCm throughput delta — *Validated v1.1 (Phase 9): honest A/B reports prompt-processing and token-generation tok/s separately (never blended), warmup-discarded, N-rep median+stddev, residency-void-gated; `--ab` composes the Phase-8 switch. Live proof-of-value Δpp +4.84 / Δtg −11.15 — ROCm wins pp, regresses tg.*
- [x] Backend-aware `detect`/`recommend` (ROCm readiness + advice; Vulkan stays default) — *Validated v1.1 (Phase 10): `recommend` derives honest ROCm advice ("worth trying / verify with bench / withheld") purely from `rocm_readiness`, never reassigns the backend, never promises a speed-up.*
- [x] Dashboard + `villa status` surface the active backend and live tok/s — *Validated v1.1 (Phase 10): active backend + image tag, live tok/s labeled by backend, tri-state ROCm-readiness badge — append-only, schema-bumped, goldens re-frozen once; `status`'s previously-hardcoded `VulkanBackend()` now reflects the configured backend.*
- [x] `rocm-6.4.4` alternate ROCm image option for TG-heavy models (ROCM-ALT-01) — *Validated v1.2 (Phase 12): two digest-pinned, fail-closed, seam-locked backends (`rocm-6.4.4` + `-rocwmma`) selectable via `BackendFor`, gated by `rocm-policy.json`, honestly benchable via `bench --ab-target`. On-hardware UAT (gfx1151, 2026-06-07): SC#1–4 all PASS as engineering deliverables (incl. a correct offload-asserting residency FAIL + verbatim rollback for `-rocwmma`, and surviving a same-day rolling-tag drift via the content-addressed pin). **Honest perf outcome: `rocm-6.4.4` does NOT recover the v1.1 Δtg −11.15 — Vulkan still leads tg by ~11.68 tok/s; Vulkan stays the tg default, never auto-switched.** The capability + honest measurement shipped; the perf premise it tested is false on this host/model.*

### Active

<!-- v1.2 Operability — scoped requirements defined in REQUIREMENTS.md, mapped in ROADMAP.md. -->

- [ ] `villa doctor` — one-shot health/diagnostics against a running install, with remediation (DOCTOR-01)
- [ ] Backup / restore of config + Open WebUI data volume (BAK-01)
- [ ] `villa bench --compare` + persisted/saved benchmark reports (BENCH-03)
- [ ] Cumulative token/throughput usage tracking over time (USAGE-01)
- [ ] Guided TUI install flow (INSTALL-01)

### Out of Scope

<!-- Explicit boundaries with reasoning. -->

- **macOS / Apple Silicon / Metal backend** — deferred to a future milestone; v1 focuses on AMD Strix Halo on Linux to nail one platform first
- **Qdrant persistent memory** — deferred to Milestone 2 (memory & search)
- **SearXNG search** — deferred to Milestone 2
- **OpenCode (local-model coding agent) wiring** — deferred to Milestone 2
- **Voice (Whisper STT / Kokoro TTS)** — explicitly future
- **Agents / agent orchestration (e.g. n8n, custom)** — explicitly future
- **Image generation (ComfyUI) and other DreamServer extras** — not requested for this product
- **Building a custom chat UI** — superseded by Open WebUI; the earlier Go chat scaffold is reference/dev-only, reused opportunistically (e.g. gateway pieces) at most
- **Remote access / multi-user auth** — out of v1; strictly-local posture, designed so authenticated remote access can be added later without rework
- **Rebuilding AI services in Go** — Go is the control plane only; inference/chat/etc. are existing OSS containers

## Context

- **Hardware target:** AMD Strix Halo (Ryzen AI Max) — RDNA 3.5 integrated GPU (gfx1151), large unified/shared memory pool (commonly 64–128 GB). Inference performance hinges on unified-memory configuration and GPU backend choice.
- **GPU access:** On Fedora/Linux today, the Vulkan backend of llama.cpp is the most reliable path for Strix Halo's iGPU; ROCm/HIP for gfx1151 is newer and less stable, kept as an optional alternate backend.
- **Reference prior art:** DreamServer (Light-Heart-Labs) — a similar Strix Halo stack, but Docker-Compose-based with a `dream` CLI and Open WebUI + Qdrant + SearXNG + Perplexica + Whisper/Kokoro + agents. VillaStraylight's distinct angle: **Go control plane, Podman Quadlets, Fedora-native, privacy + performance first.** DreamServer's model/memory-envelope recommendations (e.g. UD-Q4_K_M quants for unified memory; model selection by 64/96/124 GB tiers) are a useful starting reference for the recommendation engine.
- **Existing code:** A small single-binary Go chat-proxy scaffold exists in this repo from earlier this session (OpenAI-compatible streaming + embedded React UI). The project is treated as greenfield; the scaffold may be cannibalized for gateway/streaming code but does not constrain the architecture.
- **Prior decision:** Earlier session chose Go + chi, OpenAI-compatible model connection — consistent with this larger design.

## Constraints

- **Tech stack**: Go for all first-party code (CLI, detection, orchestration, dashboard backend, gateway) — single-language, single static binary, easy self-hosted distribution.
- **Orchestration**: Podman (rootless) via Quadlet/systemd units — native to Fedora; no Docker dependency.
- **Platform (v1)**: Fedora Workstation 44+ on AMD Strix Halo only. Architecture must not hard-code assumptions that block a later macOS/Apple-Silicon/Metal backend.
- **Inference**: llama.cpp `llama-server`, Vulkan backend primary (ROCm optional) — OpenAI-compatible API as the integration contract.
- **Privacy/Security**: Strictly local by default; no telemetry from first-party components; outbound limited to image/model pulls.
- **Performance**: Setup must produce a configuration that actually runs on the detected hardware (right model size/quant/context for the memory envelope) — "runs healthy after install" is the bar.
- **Integration-first**: Reuse mature OSS (Open WebUI, llama.cpp, later Qdrant/SearXNG); build only the control plane.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Go control plane; AI services are OSS containers | Avoid rebuilding mature tools; focus effort on hardware-aware orchestration | ✅ Validated (v1.0) — single static `villa` binary orchestrates llama.cpp + Open WebUI containers; no AI service rebuilt |
| llama.cpp + Vulkan as primary inference backend | Most reliable Strix Halo iGPU path on Linux; OpenAI-compatible API; matches DreamServer's llama.cpp choice | ✅ Validated (Phase 2) — RADV offload proven on live gfx1151 (log device_info + sysfs GTT delta), loopback /v1, dual-assert verdict |
| Inference behind a `Backend` interface; offload is offload-asserting (D-11), not liveness | De-risk the project's biggest unknown early; no Vulkan/Linux leak to callers; never false-green a silent CPU fallback | ✅ Validated (Phase 2 + v1.1) — `internal/inference` seam + grep-gate; the v1.1 ROCm backend exercised the seam for the first time with zero caller leakage |
| Podman Quadlets / systemd for orchestration | Native, rootless, boot-on-startup on Fedora; no Docker | ✅ Validated (v1.0) — `.container`/`.network`/`.volume` units generated + reconciled; rootless, boot-survival confirmed on reboot |
| Integrate Open WebUI for chat | Mature, multi-model, RAG-ready; don't reinvent the UI | ✅ Validated (Phase 4) — container-DNS wired to local /v1, telemetry killed, durable volume; live browser chat + restart persistence |
| Single `villa` Go CLI as install + control entry point | Cohesive self-hosted UX; one static binary | ✅ Validated (v1.0) — full lifecycle verb set, idempotent install, config-as-source-of-truth |
| Strictly local, no telemetry, no outbound (except pulls) | Privacy is a stated core value | ✅ Validated (through v1.0) — loopback-only PRIV gates, STRIDE-secured (Phase 4 12/12, Phase 5 19/19); v1.1 posture unchanged |
| v1 = "Core platform"; defer memory/search/OpenCode/voice/agents | Nail one platform end-to-end before breadth | ✅ Good — v1.0 + v1.1 shipped on the focused scope; deferred themes remain deferred |
| AMD Strix Halo / Fedora first; macOS/Metal later | Reduce surface area; one hardware target to optimize | ✅ Good — exercised continuously on the live gfx1151 host; Metal kept behind the `Backend` interface |
| Treat repo as greenfield; earlier Go chat scaffold is reference-only | New vision is far larger than the scaffold; don't let it constrain design | ✅ Good — scaffold not extended; clean `cmd/villa` + `internal/*` architecture |
| ROCm is opt-in; Vulkan RADV stays default; `recommend` advises, never auto-switches | Preserve the v1.0 "just works" bar; ROCm is fragile (firmware/kernel-sensitive); honesty over hype | ✅ Validated (v1.1) — `recommend` never reassigns backend nor promises a speed-up; live Δtg −11.15 proves the honesty constraint was warranted |
| Single polymorphic `BackendFor(cfg.Backend)` resolver, fail-closed | One resolution point so backend choice is honored across install/lifecycle/status/bench/dashboard; never silently fall back to Vulkan | ✅ Validated (Phase 6) — all runtime sites route through it; unknown config string → actionable error, not a silent default |
| Pin `rocm-7.2.4` stable; never `rocm7-nightlies` | Nightlies have a 64 GB allocation-cap bug that blocks large models | ✅ Good — digest-pinned in `backend_rocm.go`; preflight refuses a nightly image request |
| `villa backend set` is transactional (capture→prove→cutover→rollback) | A failed ROCm bring-up must be a no-op to the running stack — the "just works" bar | ✅ Validated (Phase 8) — 4/4 on-hardware UAT incl. forced CPU-fallback rollback + bounded timeout |
| `bench --ab` composes the Phase-8 switch; never re-implements switching | One switch implementation; bench measures, it doesn't orchestrate | ✅ Validated (Phase 9) — `--ab` delegates the flip to `backendswap.Run` |
| Surfacing (Phase 10) lands last; `--json`/goldens re-freeze exactly once | Append-only, schema-bumped, never reordered — protect the frozen dashboard contracts | ✅ Validated (Phase 10) — status/recommend/detect goldens re-frozen once as pure-addition diffs |
| ROCM-ALT-01: ship the alt image as a selectable, honestly-benched capability; never auto-switch; adopt only the digest the A/B proves recovers Δtg | Honesty over hype — prove the perf claim on-hardware, don't promise an unbenchmarked speed-up | ✅ Validated (Phase 12), premise disproven — on-hardware A/B shows `rocm-6.4.4` does NOT recover Δtg (Vulkan leads tg ~11.68 tok/s); capability shipped, Vulkan stays tg default. The honest A/B did exactly its job: prove, don't assume |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-06-07 after Phase 12 — ROCM-ALT-01 shipped, verified (UAT 7/7), and secured (10/10 threats closed). Honest on-hardware verdict: `rocm-6.4.4` does NOT recover the v1.1 Δtg −11.15; Vulkan stays the tg default (never auto-switched). v1.2 (Operability) remaining: `villa doctor` (DOCTOR-01, next), saved bench reports + `--compare` (BENCH-03), cumulative usage tracking (USAGE-01), backup/restore (BAK-01), guided TUI install (INSTALL-01). v1.1 (Phases 6–11) + v1.0 (Phases 1–5) shipped + tagged. Requirements in REQUIREMENTS.md; phases in ROADMAP.md.*
