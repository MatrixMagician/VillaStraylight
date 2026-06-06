# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Current state & source of truth

VillaStraylight is being built through the **GSD workflow**. The canonical,
up-to-date project context lives in `.planning/` and in the GSD-managed sections
below (Project, Technology Stack, etc.):

- `.planning/PROJECT.md` — what this is, core value, constraints, key decisions
- `.planning/ROADMAP.md` — milestone-grouped phase plan and per-phase success criteria
- `.planning/MILESTONES.md` — shipped milestone history (v1.0, v1.1); `.planning/RETROSPECTIVE.md` — lessons
- `.planning/milestones/` — archived per-milestone ROADMAP/REQUIREMENTS (a fresh `.planning/REQUIREMENTS.md` is created per active milestone via `/gsd-new-milestone`)
- Per-phase research/specs live under `.planning/phases/NN-*/` (e.g. `NN-RESEARCH.md`)

**In one line:** a single Go CLI (`villa`) that auto-detects an AMD Strix Halo
(gfx1151) Fedora host, recommends a memory-fitting model/quant/context, generates
rootless **Podman Quadlet** units, and orchestrates **llama.cpp (Vulkan)**
inference + **Open WebUI** chat + a control dashboard — strictly local, zero
telemetry. Go is the **control plane only**; AI services are integrated OSS
containers, not rebuilt.

**Shipped:** v1.0 MVP and v1.1 (ROCm Opt-In Backend) are complete and tagged on `main`. The `villa` control plane is implemented under `cmd/villa/` + `internal/`. Start the next cycle with `/gsd-new-milestone`.

## Legacy scaffold (reference-only — NOT the current architecture)

An earlier exploratory scaffold left reference-only remnants in the repo —
`internal/llm` (an OpenAI-compatible SSE/streaming client) and `web/` (an embedded
React UI), plus the root `.env.example`. **This is superseded** — the product
integrates Open WebUI for chat and a `villa` control plane for orchestration; a
custom chat UI is explicitly out of scope (see PROJECT.md → Out of Scope). Treat
this code as a parts bin: its `internal/llm` SSE streaming / OpenAI-compatible
client may be cannibalized for the gateway, but do not extend it as the app.
Don't let its layout constrain the architecture.

## Build, run & test

```bash
make build   # go build -o ./villa ./cmd/villa
make run     # go run ./cmd/villa
make test    # go test ./...
make check   # vet + test (pre-commit gate)
make lint    # golangci-lint if installed, else go vet
```

Go 1.26+. Single module, single static binary built from `./cmd/villa`.

## Working in this codebase

**Code map** (Go is the control plane only — AI services are OSS containers):

- `cmd/villa/` — cobra CLI, one file per subcommand (detect, recommend, preflight,
  install, up/down/restart/logs, status, model, backend, bench, dashboard, uninstall).
  Host effects live behind injectable `live*Deps` seams (`grep -rn "func live" cmd/villa`).
- `internal/` — `detect` (host probe → typed-Unknown HostProfile; AMD seam in `gpu_amd.go`),
  `recommend` (pure memory-fit `Pick`), `preflight` (reusable BLOCK/WARN gate + `go:embed`
  `rocm-policy.json`), `inference` (`BackendFor` resolver + Backend/Runner/ResidencyProof
  seam; Vulkan + ROCm), `orchestrate` (Quadlet Render/Reconcile/WriteUnits — the only impure
  module), `backendswap` (transactional switch), `bench` (pure A/B core), plus `status`,
  `dashboard`, `metrics`, `config`, `catalog`, `download`, `modelswap`, `llm`.
  Deeper detail: `docs/ARCHITECTURE.md`, `docs/DEVELOPMENT.md`.

**Conventions & gotchas (non-obvious — read before editing):**

- **Config is the single source of truth.** Quadlet units are regenerated from config,
  never hand-edited.
- **Dashboard binary trap:** `villa status`/`recommend` run fresh from `./villa`, but
  `villa-dashboard.service` is long-lived — after `make build` you MUST
  `systemctl --user restart villa-dashboard.service` for dashboard code changes to take effect.
- **Inference seam grep-gate (`TestSeamGrepGate`):** backend marker strings (`ROCm0`,
  `Vulkan0`, `HSA_OVERRIDE…`, image tags) must stay behind `internal/inference` +
  `internal/orchestrate`. The gate walks both `internal/` and `cmd/villa` — a leaked literal
  fails the build.
- **`--json`/dashboard contracts are byte-frozen by golden tests** (`testdata/*.golden*`).
  Evolve append-only + schema-bump; refreeze intentionally with `go test … -update`.
- **Offload is offload-asserting, never liveness:** a silent/partial CPU fallback is a FAIL
  (`ResidencyProof`), never a false-green.
- **Vulkan RADV is the default; ROCm is strictly opt-in** (`villa backend set rocm`).

<!-- GSD:project-start source:PROJECT.md -->

## Project

**VillaStraylight**

VillaStraylight is a self-hosted, local AI server stack for privacy-conscious power users who want a ChatGPT/Claude-class experience running entirely on their own hardware. A single Go CLI (`villa`) auto-detects the host hardware, recommends suitable models and configuration, generates Podman Quadlet units, and orchestrates a stack of OSS AI services (local inference, chat UI, and a control dashboard) — initially tuned for AMD Strix Halo on Fedora Workstation 44+, with macOS/Apple Silicon planned later.

**Core Value:** **Run a capable local AI workspace that "just works" after install** — hardware-aware setup that picks the right models and config so inference, chat, and the control dashboard come up healthy on the user's machine, with zero data leaving the box.

### Constraints

- **Tech stack**: Go for all first-party code (CLI, detection, orchestration, dashboard backend, gateway) — single-language, single static binary, easy self-hosted distribution.
- **Orchestration**: Podman (rootless) via Quadlet/systemd units — native to Fedora; no Docker dependency.
- **Platform (v1)**: Fedora Workstation 44+ on AMD Strix Halo only. Architecture must not hard-code assumptions that block a later macOS/Apple-Silicon/Metal backend.
- **Inference**: llama.cpp `llama-server`, Vulkan backend primary (ROCm optional) — OpenAI-compatible API as the integration contract.
- **Privacy/Security**: Strictly local by default; no telemetry from first-party components; outbound limited to image/model pulls.
- **Performance**: Setup must produce a configuration that actually runs on the detected hardware (right model size/quant/context for the memory envelope) — "runs healthy after install" is the bar.
- **Integration-first**: Reuse mature OSS (Open WebUI, llama.cpp, later Qdrant/SearXNG); build only the control plane.

<!-- GSD:project-end -->

<!-- GSD:stack-start source:research/STACK.md -->

## Technology Stack

## TL;DR — Prescriptive Stack

| Layer | Choice | Confidence |
|-------|--------|------------|
| Inference engine | llama.cpp `llama-server` (OpenAI-compatible) | HIGH |
| **GPU backend (v1 default)** | **Vulkan RADV (Mesa)** | HIGH |
| GPU backend (optional/perf) | ROCm 7.2.4 (HIP) | MEDIUM |
| Inference image | `docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv` (primary) / `ghcr.io/ggml-org/llama.cpp:server-vulkan` (fallback) | HIGH |
| Chat UI | Open WebUI — `ghcr.io/open-webui/open-webui:main` | HIGH |
| Orchestration | Podman Quadlet (`.container`/`.network`/`.volume`), rootless, user systemd | HIGH |
| Control plane language | Go (single static binary) | HIGH (constraint) |
| Quadlet generation | Hand-rolled template/text writer (+ `containers/podman/v5/pkg/systemd/parser` for validation) | HIGH |
| Podman control | **Direct REST API over Unix socket using stdlib `net/http`** (NOT the full bindings module) | HIGH |
| Hardware detection | `github.com/jaypipes/ghw` + targeted `/sys` + `vulkaninfo`/`rocminfo` parsing | HIGH |
| HTTP/dashboard backend | `chi` router (already chosen) + stdlib | HIGH |

## The Strix Halo (gfx1151) GPU-Backend Decision — the crux

### Reality on the ground

- **gfx1151 is the RDNA 3.5 iGPU. Two real backends exist: Vulkan and ROCm/HIP.** Both work today; neither is "fire and forget."
- **Vulkan RADV is the stable, compatible default.** The kyuz0 reference labels it "Most stable and compatible. Recommended for most users and all models." It loads large models cleanly, including past the 64 GB mark where ROCm has had problems. (HIGH)
- **ROCm 7.2.4 (HIP) is the performance option** and is now genuinely usable on gfx1151 with `HSA_OVERRIDE_GFX_VERSION=11.5.1`, but it carries sharp edges: a `rocm7-nightlies` 64 GB allocation-cap bug, sensitivity to kernel/firmware versions, and historical large-model hangs needing batch limiting. (MEDIUM)
- **AMDVLK Vulkan is the fastest Vulkan path but has a ≤2 GiB single-buffer allocation limit** — some large models won't load. Not safe as a default. (HIGH)

### Decision for v1

### How the iGPU is exposed (concrete)

# env:

### Unified / GTT memory configuration (host kernel params)

| Param | Effect |
|-------|--------|
| `amd_iommu=off` | 5–12% faster and more stable than `iommu=pt` on Strix Halo (benchmarked). |
| `amdgpu.gttsize=126976` | Caps GTT at 124 GiB (126976 MiB ÷ 1024). |
| `ttm.pages_limit=32505856` | Caps pinned pages at 124 GiB (32505856 × 4 KiB). Must match gttsize. |

### Mandatory llama-server runtime flags on Strix Halo

- `-ngl 999` — offload all layers to iGPU (unified memory makes this free).
- `-fa 1` — flash attention on (required for stability + KV memory).
- `--no-mmap` — avoid mmap; load weights resident in unified memory.

## Recommended Models & Quantizations (unified-memory tiers)

| RAM tier | Seed default | Quant | Context | Rationale |
|----------|--------------|-------|---------|-----------|
| 64 GB | Qwen3.x 35B-A3B (MoE) | UD-Q4_K_M | 128K | MoE: fast (~3B active) + fits comfortably; headroom for KV at long ctx. |
| 96 GB | 70B-class dense (e.g. DeepSeek-R1-Distill-Llama-70B) | Q4_K_M | 32K | Dense 70B Q4 fits; ctx trimmed to 32K to stay in envelope. |
| 124/128 GB | Qwen3.x 35B-A3B (MoE) at 128K, or larger MoE (e.g. gpt-oss-120b / GLM-4.5-Air-class) | UD-Q4_K_M | 128K | Either max context on the fast MoE, or step up to a 100B+ MoE for frontier capability. |

## Go Control-Plane Libraries

### Podman REST API — do NOT vendor the full bindings module

| Recommended | Alternative |
|-------------|-------------|
| **Direct REST over Unix socket with stdlib `net/http`** (custom `http.Transport` dialing `unix:///run/user/$UID/podman/podman.sock`) | `github.com/containers/podman/v5/pkg/bindings` |

### Quadlet generation — generate text, manage via systemd

| Need | Approach |
|------|----------|
| Write Quadlet unit files | Go `text/template` — render `.container` etc. from the recommended config. Simple, dependency-free, fully controllable. |
| (Optional) validate generated units | `github.com/containers/podman/v5/pkg/systemd/parser` — lightweight INI/unit parser, much lighter than full bindings, useful to round-trip-validate generated files. |
| Manage user units | Shell to `systemctl --user` (daemon-reload / start / enable / status), or use `github.com/coreos/go-systemd/v22/dbus` for programmatic D-Bus control of the user manager. |

### Hardware / GPU detection

| Library | Use |
|---------|-----|
| `github.com/jaypipes/ghw` | Primary: CPU, memory (total/usable), PCI, GPU, baseboard/DMI — works **without root**, never hard-errors on missing perms. Covers "CPU/arch, GPU, total memory" cleanly. |
| `github.com/jaypipes/pcidb` | (transitive via ghw) PCI ID → human names, to identify the Strix Halo iGPU device. |
| Direct `/sys` reads + small parsers | gfx1151 specifics ghw won't surface: confirm `gfx1151` via `rocminfo`, confirm Vulkan device via `vulkaninfo --summary`, read current `amdgpu.gttsize`/`ttm` state, read DMI product string to recognize "Ryzen AI MAX". |

### HTTP control plane / dashboard backend

## Open WebUI integration

## Container Images to Standardize On

| Purpose | Image | Notes |
|---------|-------|-------|
| Inference (Vulkan, primary) | `docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv` | Strix-Halo-tuned, auto-rebuilt on llama.cpp master; the community reference. Includes router/`models.ini` support. |
| Inference (Vulkan, vendor-neutral fallback) | `ghcr.io/ggml-org/llama.cpp:server-vulkan` | Official upstream; less Strix-Halo-specific but trustworthy provenance. |
| Inference (ROCm, optional/perf) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4` | Opt-in. Avoid `rocm7-nightlies` (64 GB cap bug). |
| Chat UI | `ghcr.io/open-webui/open-webui:main` | Pin a digest in prod. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| **Docker / docker-compose** | Project constraint; Podman rootless + Quadlet is Fedora-native, boots via systemd, no daemon. | Podman Quadlet + user systemd. |
| **`containers/podman/v5` full bindings as a hard dep** | Massive dependency, build-time system libs, binary bloat — breaks the single-static-binary goal. | Direct REST over the Podman user socket with stdlib. |
| **AMDVLK Vulkan image as default** | ≤2 GiB single-buffer limit — large models silently fail to load. | RADV (Mesa) Vulkan. |
| **ROCm `rocm7-nightlies` for large models** | Known bug caps allocation at 64 GB → can't load 70B+/long-context. | `rocm-7.2.4` stable, or just Vulkan RADV. |
| **ROCm as the *default* backend on gfx1151** | More fragile: firmware/kernel-version sensitive, large-model hangs, override env required. Conflicts with "just works." | Vulkan RADV default; ROCm opt-in. |
| **`linux-firmware-20251125`** | Documented to break ROCm on Strix Halo (instability/crashes). | Newer firmware (e.g. 20260110); detection should warn if this exact version is present. |
| **Kernels < 6.18.4** | gfx1151 stability bug. | Fedora 43/44 with kernel ≥ 6.18.4 (kyuz0 baseline: 6.18.9). |
| **`iommu=pt`** | 5–12% slower than disabling IOMMU on Strix Halo. | `amd_iommu=off`. |
| **mmap / no flash-attention on Strix Halo** | Crashes and slowdowns. | Always `--no-mmap -fa 1 -ngl 999`. |
| **Auto-selecting correctness-flagged models on unified memory** | Documented wrong-output issues on these backends (e.g. Qwen3 Coder Next per DreamServer). | Catalog `unified_memory_safe` flag; route around them. |
| **Ollama as the engine** | Vendored llama.cpp lags upstream (missing Wave32 FA / graphics-queue fixes → ~56% t/s gap on AMD Vulkan); extra abstraction over the chosen llama-server contract. | llama.cpp `llama-server` directly. |

## Stack Patterns by Variant

- Offer ROCm 7.2.4 backend (opt-in) for higher throughput.
- Because HIP+hipBLASLt+rocWMMA-FA beats Vulkan on token generation at long context — but only when the environment is exactly right.
- Prefer a 70B-class dense Q4 at reduced context (32K), or a fast MoE at full context.
- Because 96 GB fits 70B Q4 but not with huge KV; pick one of width-vs-context, don't try both.
- Fall back to Vulkan RADV.
- Because the nightly 64 GB allocation cap blocks large models.

## Version Compatibility

| Component | Known-good baseline | Notes |
|-----------|---------------------|-------|
| Fedora | 43 / 44+ | kyuz0 tested on 42/43; project targets 44+. |
| Linux kernel | ≥ 6.18.4 (6.18.9 tested) | < 6.18.4 has gfx1151 stability bug. |
| linux-firmware | ≥ 20260110 | Avoid 20251125 (breaks ROCm). |
| ROCm (if used) | 7.2.4 stable | Needs `HSA_OVERRIDE_GFX_VERSION=11.5.1`; avoid nightlies for >64 GB. |
| llama.cpp | master (auto-rebuilt images) | MTP merged to master — don't use deprecated `-mtp` images. |
| Open WebUI | `:main` (pin digest) | Internal port 8080, data `/app/backend/data`. |
| Podman | v5 (rootless socket) | `systemctl --user enable --now podman.socket`. |

## Sources

- `github.com/kyuz0/amd-strix-halo-toolboxes` (README + benchmark viewer) — de-facto Strix Halo reference: backend recommendation, images, kernel params, runtime flags, firmware/kernel baselines. **HIGH**
- `llm-tracker.info/_TOORG/Strix-Halo` — Vulkan vs HIP performance/stability, build flags, memory envelope, hipBLASLt tuning. **HIGH**
- `github.com/ggml-org/llama.cpp` docs (docker.md) — official image tags (server-vulkan / server-rocm) on ghcr.io. **HIGH**
- `github.com/Light-Heart-Labs/DreamServer` README — tier/model map (64/96/124 GB), UD-Q4_K_M defaults, MoE preference, unified-memory correctness-routing pattern. **HIGH** (prior art, model picks MEDIUM — volatile)
- `unsloth.ai/docs` (Dynamic 2.0 GGUFs) — UD-Q4_K_M adaptive quantization rationale. **HIGH**
- `docs.openwebui.com` + `pkg.go.dev` (open-webui env config) — OPENAI_API_BASE_URL, ENABLE_OLLAMA_API, ANONYMIZED_TELEMETRY, OFFLINE_MODE, WEBUI_AUTH, port 8080, data dir. **HIGH**
- `pkg.go.dev/github.com/containers/podman/v5/pkg/bindings` + `pkg/systemd/quadlet`/`parser`; podman.io REST API docs — bindings weight, REST-over-socket alternative, quadlet packages. **HIGH**
- `github.com/jaypipes/ghw` (+ `go-hardware/ghw` fork), `jaypipes/pcidb` — hardware detection without root. **HIGH**
- `github.com/ollama/ollama` issues #15601 — Vulkan/AMD t/s gap vs standalone llama.cpp (why not Ollama). **MEDIUM**
- `github.com/ggml-org/llama.cpp` issues #15018 — ROCm slow weight loading >64 GB vs Vulkan. **MEDIUM**

<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->

## Conventions

Conventions not yet established. Will populate as patterns emerge during development.
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->

## Architecture

Architecture not yet mapped. Follow existing patterns found in the codebase.
<!-- GSD:architecture-end -->

<!-- GSD:skills-start source:skills/ -->

## Project Skills

No project skills found. Add skills to any of: `.claude/skills/`, `.agents/skills/`, `.cursor/skills/`, `.github/skills/`, or `.codex/skills/` with a `SKILL.md` index file.
<!-- GSD:skills-end -->

<!-- GSD:workflow-start source:GSD defaults -->

## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:

- `/gsd-quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd-debug` for investigation and bug fixing
- `/gsd-execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->

<!-- GSD:profile-start -->

## Developer Profile

> Profile not yet configured. Run `/gsd-profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
