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

<!-- GSD:stack-start source:codebase/STACK.md -->

## Technology Stack

## Languages

- Go 1.26.2 - All first-party code: the `villa` CLI (`cmd/villa/`), hardware detection, recommendation engine, Podman/Quadlet orchestration, dashboard backend, and the OpenAI-compatible inference client. Single-language by constraint (single static binary).
- HTML / CSS / JavaScript - The no-build, embedded control-dashboard single-page UI (`internal/dashboard/assets/dashboard.html`, `dashboard.css`, `dashboard.js`). Served verbatim via `go:embed`; there is no JS toolchain/bundler in the `villa` path.
- TOML - Persisted CLI configuration format (`$XDG_CONFIG_HOME/villa/config.toml`).
- JSON - The embedded model catalog (`internal/catalog/seed.json`), the ROCm pin policy (`internal/preflight/rocm-policy.json`), and golden test fixtures.

## Runtime

- Go 1.26.2 (from `go.mod`). Compiles to a single static binary `villa`.
- Target host OS: Fedora Workstation 44+ (Linux kernel >= 6.18.4) on AMD Strix Halo (gfx1151). The binary is the control plane; AI workloads run as rootless Podman containers under the user systemd manager.
- Go modules (`go.mod` / `go.sum`).
- Lockfile: present (`go.sum`).
- Module path: `github.com/MatrixMagician/VillaStraylight`.

## Frameworks

- `github.com/spf13/cobra` v1.10.2 - CLI command tree for `villa` (`cmd/villa/root.go` + per-verb files). Subcommands: `detect`, `recommend`, `preflight`, `install`, `uninstall`, `up`, `down`, `restart`, `status`, `logs`, `config`, `dashboard`, `model` (`list` / `pull` / `show` / swap), `backend`, `inference`, `bench`.
- `github.com/go-chi/chi/v5` v5.3.0 - HTTP router + middleware for the loopback-only control-dashboard backend (`internal/dashboard/server.go`). Middleware chain: RequestID, RealIP, Logger, Recoverer, plus a custom `requireSameOrigin` guard on `/api`.
- Go standard `testing` package - The only test framework. Table-driven tests, `httptest` servers, and byte-for-byte golden fixtures (`cmd/villa/testdata/*.golden.json`, `internal/orchestrate` rendered-unit goldens, `internal/metrics/testdata/slots.json`). No third-party assertion or mocking library — seams are injected `func` fields.
- `go build` / `go test` / `go vet` / `gofmt` via `Makefile`.
- `golangci-lint` (optional; config `.golangci.yml`) - `make lint` runs it if installed, else falls back to `go vet`.

## Key Dependencies

- `github.com/spf13/cobra` v1.10.2 - CLI framework (see above).
- `github.com/go-chi/chi/v5` v5.3.0 - Dashboard HTTP router (see above).
- `github.com/jaypipes/ghw` v0.24.0 - Root-less hardware detection: CPU/arch (`ghw.CPU()` in `internal/detect/cpu.go`) and total physical memory (`ghw.Memory()` in `internal/detect/memory.go`). Never hard-errors on missing perms.
- `github.com/BurntSushi/toml` v1.6.0 - Marshal/unmarshal of `config.toml` (`internal/config/villaconfig.go`). No string interpolation (mitigates injection on write).
- `github.com/jaypipes/pcidb` v1.1.1 - PCI ID -> human name (transitive via ghw).
- `github.com/spf13/pflag` v1.0.9 - flag parsing (via cobra).
- `github.com/inconshreveable/mousetrap` v1.1.0 - cobra Windows helper.
- `github.com/go-ole/go-ole` v1.2.6, `github.com/yusufpapurcu/wmi` v1.2.4 - ghw Windows backends (not exercised on the Fedora target).
- `golang.org/x/sys` v0.25.0 - low-level syscalls (via ghw).
- `gopkg.in/yaml.v3` v3.0.1, `howett.net/plist` - ghw transitive parsers.

## Configuration

- TOML file at `$XDG_CONFIG_HOME/villa/config.toml` (resolved via `os.UserConfigDir`). Defined by `VillaConfig` in `internal/config/villaconfig.go`.
- Fields: `model`, `quant`, `ctx`, `backend` (default `vulkan`; `rocm` opt-in), `catalog_path`, `dashboard_addr` (default `127.0.0.1`, loopback-only by construction), `dashboard_port` (default `8888`), `chat_port` (default `3000`).
- Read-only by default: `LoadVilla` returns typed defaults when the file is absent; `SaveVilla` (invoked by `recommend --save` / model swap) writes strictly under the XDG dir with mode `0600`, dir `0700`, and a path-traversal guard. Self-heals zeroed dashboard/chat fields on load (never widens the bind off loopback).
- `internal/catalog/seed.json` - the seed model catalog (`//go:embed seed.json` in `internal/catalog/load.go`). Catalog has a schema version window; an external override path may be supplied via `catalog_path`.
- `internal/preflight/rocm-policy.json` - ROCm pin policy: image-tag allow/deny, kernel floor, firmware floor/deny, required `HSA_OVERRIDE_GFX_VERSION` (`//go:embed rocm-policy.json` in `internal/preflight/floors.go`).
- `internal/orchestrate/quadlet/*.tmpl` - Quadlet unit `text/template`s (`//go:embed quadlet/*.tmpl` in `internal/orchestrate/render.go`): `container.tmpl`, `network.tmpl`, `volume.tmpl`, `openwebui.container.tmpl`, `openwebui.volume.tmpl`.
- `internal/dashboard/assets/` - embedded dashboard UI (`//go:embed all:assets` in `internal/dashboard/embed.go`); `dashboard.html` is parsed as an `html/template` shell (chat-link port injected), css/js served verbatim.
- `Makefile` targets: `help`, `run`, `build` (-> `./villa`), `test`, `vet`, `fmt`, `lint`, `check` (vet+test), `tidy`, `clean`.
- `.golangci.yml` - linter config (used by `make lint`).

## Platform Requirements

- Go 1.26.2 toolchain.
- For end-to-end runtime testing: a Fedora host with rootless Podman, `systemctl --user`, and the AMD GPU stack (`/dev/dri`, optionally `/dev/kfd` for ROCm). Host probe tools used when present: `vulkaninfo`, `rocminfo`, `rpm`, `setsebool`, `loginctl`, `journalctl`.
- Fedora Workstation 44+ on AMD Strix Halo (gfx1151), kernel >= 6.18.4, linux-firmware >= 20260110 (firmware 20251125 explicitly denied for ROCm).
- Rootless Podman v5 with the user socket/manager; user lingering enabled (`loginctl enable-linger`) so Quadlet services survive logout/reboot.
- Strictly local; no telemetry from first-party components.

## Container Images Standardized On

| Purpose | Image | Source file |
|---------|-------|-------------|
| Inference (Vulkan RADV, v1 default) | `docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…ac7aad` | `internal/inference/backend_vulkan.go` |
| Inference (ROCm 7.2.4, opt-in/perf) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1…531a89` | `internal/inference/backend_rocm.go` |
| Inference (ROCm 6.4.4, TG-tuned, opt-in) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4@sha256:c81f30a7…f150ec62` | `internal/inference/backend_rocm.go` |
| Inference (ROCm 6.4.4 rocWMMA, opt-in) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4-rocwmma@sha256:9a97129a…43c0141` | `internal/inference/backend_rocm.go` |
| Chat UI (Open WebUI) | `ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a…a9184e` | `internal/orchestrate/openwebui.go` |
<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->

## Conventions

## Naming Patterns

- Lowercase, no underscores for source: `value.go`, `backend.go`, `running_offload.go`
- Tests mirror their source file with `_test.go`: `backend.go` → `backend_test.go`,
- Topic-grouped check files in `internal/preflight`: `checks_gpu.go`,
- Standard Go `CamelCase` (exported) / `camelCase` (unexported).
- **`live*Deps` constructors** wire a pure core's `Deps` struct to the real host.
- **`*ForTest` helpers** expose an internal seam to tests in another package
- **`fake*Deps` types** are test doubles for a command's `Deps`:
- Short receiver names (`b backendVulkan`, `r CheckResult`); descriptive locals.
- The golden `-update` flag is a package-level `var update = flag.Bool(...)`
- Typed `Optional` wrappers (`Bytes`/`Str`/`Int`/`Bool`) instead of bare
- Interface seams named for the role: `Backend`, `Deps`, `RenderInput`,

## Code Style

- `gofmt` (`make fmt` runs `gofmt -w .`). Tabs, standard Go layout.
- `goimports` enforced via `.golangci.yml` — imports are grouped and ordered.
- Enabled linters: `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`,
- `run.timeout: 3m`.
- `errcheck` is disabled for `_test.go` files (the only exclude rule).
- `make lint` falls back to `go vet` if `golangci-lint` is not installed; CI

## Import Organization

## Core Architectural Conventions

### Typed-Unknown degradation (never bare 0 / never panic)

### Config is the single source of truth

### Pure-core + injectable-seam

- Pure logic lives in `internal/*` cores (no host I/O): `detect`, `recommend`,
- Host effects (exec, Unix sockets, `/sys`, filesystem) are injected via a `Deps`
- `internal/orchestrate` is the **only intentionally impure** module (it shells to
- Consequence: every command is testable off-hardware by passing a `fake*Deps`.

### Backend interface seam + fail-closed resolver

### Backend marker strings stay behind the seam

### Byte-frozen output contracts (golden, append-only)

### Offload-asserting (silent CPU fallback = FAIL)

## Error Handling

- Return errors up; wrap with context using `fmt.Errorf(... %w ...)` (~60 of ~96
- **Fail closed** on untrusted input (hand-edited config): error, never a silent
- **Refuse-with-remediation** in preflight: every non-PASS `CheckResult` carries a

## Logging

## Comments

- Every file opens with a package/file-level doc comment stating its role and the
- Decision/requirement IDs (`D-NN`, `REQ-*`, `SC#N`, `T-6-03`) are the canonical
- Test functions carry a doc comment explaining the invariant being guarded and

## Function & Module Design

- **`Deps` struct injection**: a command's host dependencies are a struct of
- **Thin cobra callers**: `cmd/villa/*.go` commands are thin wrappers that call
- **Single polymorphism point**: choose a concrete backend only via `BackendFor`.
- Exports: package APIs are deliberately narrow; test-only access goes through

## GOTCHA — dashboard restart after rebuild

<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->

## Architecture

> Full layered system diagram: `.planning/codebase/ARCHITECTURE.md` (System Overview).

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| Command tier | Cobra surface, flag parsing, exit codes, rendering, `live*Deps` wiring | `cmd/villa/*.go` |
| detect | Probe host → typed-Unknown `HostProfile` (CPU, memory envelope, iGPU, kernel, ROCm readiness) | `internal/detect/detect.go` |
| recommend | Pure `Pick()` → memory-fitting `Recommendation` (model/quant/ctx/backend) | `internal/recommend/recommend.go` |
| catalog | Embedded model catalog (`go:embed seed.json`) + external override w/ fallback | `internal/catalog/catalog.go`, `load.go` |
| preflight | Reusable host-prep gate → `[]CheckResult` (BLOCK/WARN tiers, fail-soft) | `internal/preflight/preflight.go` |
| inference | Backend-neutral seam: `BackendFor`, `Backend` iface, offload/residency proof | `internal/inference/*.go` |
| orchestrate | Render Quadlet units (pure) + reconcile + host-touching systemd seam | `internal/orchestrate/*.go` |
| backendswap | Transactional `villa backend set` (capture→prove→cutover→rollback) | `internal/backendswap/backendswap.go` |
| bench | Pure A/B throughput core; `--ab` composes `backendswap.Run` | `internal/bench/bench.go` |
| modelswap | Guarded `villa model swap` ordering core (shared by CLI + dashboard) | `internal/modelswap/modelswap.go` |
| status | Read-model aggregation → frozen `Report` (shared by CLI + dashboard) | `internal/status/status.go` |
| dashboard | Loopback-only chi server folding `status` core + embedded SPA | `internal/dashboard/server.go`, `api.go` |
| metrics | llama.cpp `/metrics` scrape (pp/tg timings) | `internal/metrics/llamacpp.go` |
| download | Model weight pull + shard handling | `internal/download/download.go` |
| config | Single source of truth: XDG `config.toml` load/save (`VillaConfig`) | `internal/config/villaconfig.go` |

## Pattern Overview

- **Pure cores, impure edges.** Cores never call `os.Exit` and never print. They return typed values (`Recommendation`, `[]CheckResult`, `Verdict`, `Result`, `Report`); the command tier maps those to exit codes and tables/JSON.
- **Single polymorphism point for backends.** `inference.BackendFor(name)` is the only place a backend string becomes a concrete implementation; everything else depends on the `Backend` interface.
- **Config is the single source of truth.** `config.toml` drives recommend → orchestrate; Quadlet units are regenerated from config, never hand-edited as the authority.
- **Honesty-by-construction.** Every probe degrades to a typed `Unknown` (`detect.Bool`/`detect.Bytes`) → WARN, which is DISTINCT from a confident negative → FAIL. CPU fallback is never reported as success.
- **Composition over re-implementation.** `bench --ab` composes `backendswap.Run`; `dashboard` composes `status` and `modelswap`; nothing forks a proven core.

## Layers

- Purpose: cobra command tree, flag parsing, exit-code mapping, rendering, `live*Deps` wiring.
- Location: `cmd/villa/*.go`, one file per subcommand (`detect.go`, `install.go`, `backend.go`, …); tree assembled in `cmd/villa/root.go` (`newRoot`), entry `cmd/villa/main.go`.
- Depends on: every pure core via `live*Deps()` closures.
- Used by: end user (the `villa` binary).
- Purpose: all decision logic and host-state aggregation, behind injectable seams.
- Location: `internal/detect`, `recommend`, `catalog`, `preflight`, `inference`, `backendswap`, `bench`, `modelswap`, `status`, `metrics`, `download`, `config`.
- Depends on: each other along the pipeline (recommend → catalog+detect; status → detect+inference+orchestrate); never on cobra.
- Used by: the command tier and (for status/modelswap) the dashboard.
- Purpose: turn config + proven `Backend` into rootless Podman Quadlet units, reconcile against disk, write atomically, drive user systemd.
- Location: `internal/orchestrate/`. `render.go`/`reconcile.go` are PURE; only `systemd.go` and `WriteUnits` (in `render.go`) touch the host.
- Depends on: `config`, `inference`.
- Used by: install/lifecycle commands, `backendswap`, `status`.
- Purpose: run integrated OSS AI containers (`villa-llama`, `villa-openwebui`) plus `villa-dashboard.service`, networked over `villa.network`, models on `villa-models.volume`.

## Data Flow

### Primary install path (detect → recommend → preflight → orchestrate → systemd → proof)

### Backend switch (transactional)

### A/B benchmark

- Persistent state lives in `config.toml` (the single source of truth) and on-disk Quadlet units (regenerated from config). Cores hold no global mutable state; the dashboard server guards its one cached value with a `sync` mutex.

## Key Abstractions

- Purpose: which GPU backend applies (image, runtime flags, device args, residency markers).
- Examples: `internal/inference/backend_vulkan.go`, `backend_rocm.go`.
- Pattern: every backend literal lives behind it; callers depend on the interface only.
- Purpose: map a config `backend` string → `Backend`; fail-closed on unknown values.
- Examples: `internal/inference/backend.go:21`.
- Pattern: `"" | "vulkan"` → Vulkan RADV; `"rocm"` → ROCm; anything else → actionable error, NEVER silent fallback.
- Purpose: each backend owns its log/journal marker literals (`Vulkan0`/`ROCm0`, device label, fault string); both offload scrapes are parameterized by it.
- Examples: `internal/inference/backend.go:80`, `offload.go`, `running_offload.go`.
- Pattern: a future backend slots in without re-rolling offload math; CPU fallback = FAIL, never false-green.
- Purpose: drive host-touching flows from tests without a live host.
- Examples: `internal/backendswap/backendswap.go`, `internal/bench/bench.go`, `internal/modelswap/modelswap.go`, `internal/status/status.go`.
- Pattern: every host action is a `func` field; the live wiring is a `live*Deps()` closure in `cmd/villa`.

## Entry Points

- Location: `cmd/villa/main.go` → `newRoot().Execute()`.
- Triggers: user CLI invocation.
- Responsibilities: build the cobra tree (`cmd/villa/root.go`), dispatch to the per-subcommand `run*` function, map returned error to exit 1.
- Location: `internal/dashboard/server.go` (`NewServer`), launched as a user systemd unit (`villa-dashboard.service`).
- Triggers: `villa dashboard` / boot via systemd.
- Responsibilities: loopback-only chi server folding the shared `status` read-model + embedded SPA.

## Architectural Constraints

- **Backend literals are seam-locked.** Container image/device/`podman`/marker literals MUST live in `internal/inference/` (and `internal/detect/gpu_amd.go`). Enforced by `TestSeamGrepGate` (`internal/inference/seam_test.go`) over both `internal/` and `cmd/villa`.
- **orchestrate is the ONLY impure first-party module.** Filesystem + `os/exec` touch is confined to `internal/orchestrate/systemd.go` + `WriteUnits`. Render/Reconcile must stay pure.
- **No silent CPU fallback.** Offload assert requires BOTH log-scrape AND sysfs GTT-delta; an unevaluable signal → WARN, a confident absence → FAIL.
- **Loopback-only binds.** Dashboard binds `127.0.0.1` via `net.JoinHostPort`; never `:port`/`0.0.0.0` (PRIV-01, `internal/dashboard/server.go`).
- **No shell interpolation.** All host commands are fixed-arg `exec.Command`; model names are catalog-resolved, never shell-interpolated.
- **`--json`/dashboard contracts are byte-frozen.** Evolve append-only + bump schema version; golden tests guard them (`cmd/villa/testdata/*.json.golden`).
- **No telemetry.** First-party components emit none; outbound limited to image/model pulls (asserted in `status`).
- **Single static binary.** No Podman full-bindings dependency; Podman is controlled via fixed-arg CLI / REST-over-socket.

## Anti-Patterns

### Re-typing a backend literal in a caller

### Silently defaulting an unknown backend string

### Treating health-200 / is-active as success

### Re-implementing backend switching inside bench

### Editing a Quadlet unit as the source of truth

## Error Handling

- Typed-Unknown degradation: missing tool / unparseable output → `Unknown` → WARN, never a false hard block (`internal/preflight`, `internal/detect`).
- Typed tool errors: `orchestrate.ErrToolNotFound` (missing binary → soft) vs `ErrCommandFailed` (ran non-zero with no output → hard) (`internal/orchestrate/systemd.go`).
- Transactional rollback: any mutate error or non-pass prove → verbatim restore, with honest rollback-incomplete reporting (`internal/backendswap/backendswap.go`).

## Cross-Cutting Concerns

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
