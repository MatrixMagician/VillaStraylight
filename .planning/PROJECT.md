# VillaStraylight

## What This Is

VillaStraylight is a self-hosted, local AI server stack for privacy-conscious power users who want a ChatGPT/Claude-class experience running entirely on their own hardware. A single Go CLI (`villa`) auto-detects the host hardware, recommends suitable models and configuration, generates Podman Quadlet units, and orchestrates a stack of OSS AI services (local inference, chat UI, and a control dashboard) — initially tuned for AMD Strix Halo on Fedora Workstation 44+, with macOS/Apple Silicon planned later.

## Core Value

**Run a capable local AI workspace that "just works" after install** — hardware-aware setup that picks the right models and config so inference, chat, and the control dashboard come up healthy on the user's machine, with zero data leaving the box.

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
- [x] Strictly local operation: all services bind to localhost/LAN; no telemetry; only outbound traffic is image/model downloads during install/update — *Validated through Phase 4: PRIV-01/02/03 loopback + no-telemetry gates, `villa status` loopback-only assertion, SECURED audit 12/12*

### Active

<!-- v1 / Milestone 1 — "Core platform". These are hypotheses until shipped. -->

- [ ] Control dashboard showing service health, performance metrics, token/throughput usage, and available/loaded models — *Phase 5 (next)*
- [ ] Runs on Fedora Workstation 44+ on AMD Strix Halo — *continuously exercised on the live gfx1151 host through Phase 4*

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
| Go control plane; AI services are OSS containers | Avoid rebuilding mature tools; focus effort on hardware-aware orchestration | — Pending |
| llama.cpp + Vulkan as primary inference backend | Most reliable Strix Halo iGPU path on Linux; OpenAI-compatible API; matches DreamServer's llama.cpp choice | ✅ Validated (Phase 2) — RADV offload proven on live gfx1151 (log device_info + sysfs GTT delta), loopback /v1, dual-assert verdict |
| Inference behind a `Backend` interface; offload is offload-asserting (D-11), not liveness | De-risk the project's biggest unknown early; no Vulkan/Linux leak to callers; never false-green a silent CPU fallback | ✅ Validated (Phase 2) — `internal/inference` seam + grep-gate; CPU-fallback = FAIL |
| Podman Quadlets / systemd for orchestration | Native, rootless, boot-on-startup on Fedora; no Docker | — Pending |
| Integrate Open WebUI for chat | Mature, multi-model, RAG-ready; don't reinvent the UI | — Pending |
| Single `villa` Go CLI as install + control entry point | Cohesive self-hosted UX; one static binary | — Pending |
| Strictly local, no telemetry, no outbound (except pulls) | Privacy is a stated core value | — Pending |
| v1 = "Core platform"; defer memory/search/OpenCode/voice/agents | Nail one platform end-to-end before breadth | — Pending |
| AMD Strix Halo / Fedora first; macOS/Metal later | Reduce surface area; one hardware target to optimize | — Pending |
| Treat repo as greenfield; earlier Go chat scaffold is reference-only | New vision is far larger than the scaffold; don't let it constrain design | — Pending |

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
*Last updated: 2026-06-05 — Phase 4 (Chat Integration) complete: Open WebUI runs as a managed 5th/2nd container wired to local inference by container DNS, telemetry killed, durable `:Z` volume, default model auto-pulled at install. Verified 4/4 on the live gfx1151 host (zero-config browser chat, streaming, restart persistence, loopback-only posture) and SECURED (12/12 STRIDE threats, threats_open=0). Only Phase 5 (Control Dashboard) remains in milestone v1.0. Prior: Phase 2 de-risked iGPU offload; Phase 3 shipped the idempotent install + full lifecycle verb set.*
