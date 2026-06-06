# VillaStraylight

## What This Is

VillaStraylight is a self-hosted, local AI server stack for privacy-conscious power users who want a ChatGPT/Claude-class experience running entirely on their own hardware. A single Go CLI (`villa`) auto-detects the host hardware, recommends suitable models and configuration, generates Podman Quadlet units, and orchestrates a stack of OSS AI services (local inference, chat UI, and a control dashboard) — initially tuned for AMD Strix Halo on Fedora Workstation 44+, with macOS/Apple Silicon planned later.

## Core Value

**Run a capable local AI workspace that "just works" after install** — hardware-aware setup that picks the right models and config so inference, chat, and the control dashboard come up healthy on the user's machine, with zero data leaving the box.

## Current State

**v1.0 MVP shipped 2026-06-05** (tag `v1.0`, PR #1 → `main`, merge `57f8ef7`). All 5 phases complete and live-verified on real AMD Strix Halo (gfx1151) hardware: `villa` detects the host, recommends a memory-fitting model, installs a rootless Podman Quadlet stack (llama.cpp Vulkan inference + Open WebUI chat), and serves a read-only control dashboard — strictly local, zero telemetry. 110 Go files, 430-test suite green, Phases 4 & 5 STRIDE-secured.

**v1.1 in progress:** Phase 6 (ROCm backend + `BackendFor` resolver + HIP residency proof) and Phase 7 (ROCm Quadlet render delta + refuse-with-remediation ROCm preflight + detect-readiness fields) complete — all off-hardware, byte-goldens frozen, 481-test suite green. The two "is this unit/host valid" pieces the Phase 8 switch verb gates on now exist. On-hardware validation of the switch/throughput lands in Phases 8–9.

## Current Milestone: v1.1 ROCm Opt-In Backend (Throughput)

**Goal:** Add an opt-in ROCm/HIP inference backend for higher throughput on Strix Halo (gfx1151), gated hard enough to preserve the "just works" bar, switchable on a running install, with benchmarking to prove the win — while Vulkan RADV remains the safe default.

**Target features:**
- ROCm backend behind the existing v1.0 `Backend` interface (`rocm-7.2.4` image, `HSA_OVERRIDE_GFX_VERSION=11.5.1`), offload-asserting like Vulkan — a silent CPU fallback is a FAIL.
- `villa backend set rocm|vulkan` — swap the inference unit on a running install, fit-guarded + readiness-polled, rollback-safe (no full reinstall).
- Full ROCm preflight + detect — confirm `rocminfo`/gfx1151, kernel floor, block firmware-20251125, require HSA override; refuse-with-remediation rather than silently degrade.
- `villa bench` — A/B tok/s comparison (Vulkan vs ROCm) on the loaded model; throughput delta is a success criterion.
- Backend-aware `recommend`/`detect` — `detect` reports ROCm readiness; `recommend` advises whether ROCm is viable/worth it; Vulkan stays default.
- Dashboard + `villa status` show the active backend and live tok/s.

**Key constraints:** Pin `rocm-7.2.4` stable (avoid `rocm7-nightlies` 64 GB cap bug); strictly-local / zero-telemetry posture unchanged; phases continue from Phase 6.

**Candidate later themes (deferred):** RAG (Qdrant) + search (SearXNG), macOS/Apple-Silicon (Metal) backend, authenticated remote/multi-user access, OpenCode coding-agent wiring, voice (Whisper/Kokoro).

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
- [x] Full ROCm preflight + detection (rocminfo/gfx1151, kernel floor, firmware-20251125 block, HSA override) — refuse-with-remediation — *Validated off-hardware in Phase 7: `villa preflight --backend rocm` refuses only on positively-known-bad with named remediation (degrades unevaluable signals to WARN, exit 2 never a false exit 1), driven by a `go:embed` `rocm-policy.json`; `villa detect --json` appends a `rocm_readiness` block (schema 1→2, append-only, undetectable signals serialize UNSET never false-green). On-hardware confirmation deferred to Phase 8.*

### Active

<!-- v1 / Milestone 1 — "Core platform". These are hypotheses until shipped. -->

- [ ] ROCm/HIP inference backend, opt-in, behind the existing `Backend` interface, offload-asserting (no false-green CPU fallback)
- [ ] `villa backend set rocm|vulkan` switches the inference backend on a running install (fit-guarded, readiness-polled, rollback-safe)
- [ ] `villa bench` proves the Vulkan-vs-ROCm throughput delta
- [ ] Backend-aware `detect`/`recommend` (ROCm readiness + advice; Vulkan stays default)
- [ ] Dashboard + `villa status` surface the active backend and live tok/s

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
*Last updated: 2026-06-06 — Phase 7 complete (ROCm render delta + preflight + detect, off-hardware; ROCM-03/PRE-06/DET-04 validated, 481-test suite green). 2026-06-05 — started milestone v1.1 (ROCm Opt-In Backend). Builds on the v1.0 `Backend` interface seam (Phase 2, D-11): ROCm becomes a second, opt-in backend with full preflight gating, a switchable `villa backend set` verb, `villa bench` to prove the throughput delta, and backend-aware detect/recommend/dashboard. Vulkan RADV stays the default. v1.0: all 5 phases shipped and merged to `main` (tag `v1.0`, PR #1), Phases 4 & 5 SECURED (12/12 + 19/19 STRIDE). Phases continue from Phase 6.*
