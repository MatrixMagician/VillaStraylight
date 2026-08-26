# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Current state & source of truth

The canonical project context lives in this file (Project, Technology Stack,
Conventions, Architecture below) and in `docs/`:

- `docs/ARCHITECTURE.md` — layered system design and component responsibilities
- `docs/DEVELOPMENT.md` — build, test, and contribution workflow
- `docs/CONFIGURATION.md` — every `config.toml` field and its effect
- `docs/GETTING-STARTED.md`, `docs/MEMORY.md`, `docs/TESTING.md`
- `docs/RELEASING.md` — how a release is cut and how the signed pin manifest is
  published; the signing key is offline by design and must never reach CI
- `docs/spec/v1.8-villa-update.md` — the accepted, not-yet-implemented `villa
  update` design: read it before touching pins, and note §7's two migration
  hazards (most `EmbedImage()` callers are probe helpers, not pins)
- `CONTEXT.md` — the domain glossary (ubiquitous language); use its terms in
  issues, tests and proposals rather than drifting to synonyms
- `docs/adr/` — accepted architecture decisions; read the ones touching your area
  before changing it, and surface a contradiction rather than silently overriding

Historical planning artifacts (milestone history, retrospectives, per-phase
research) were removed from the working tree; they remain in git history.

**In one line:** a single Go CLI (`villa`) that auto-detects an AMD Strix Halo
(gfx1151) Fedora host, recommends a memory-fitting model/quant/context, generates
rootless **Podman Quadlet** units, and orchestrates **llama.cpp (ROCm)**
inference + **Open WebUI** chat + a control dashboard — strictly local, zero
telemetry. Go is the **control plane only**; AI services are integrated OSS
containers, not rebuilt.

**Shipped:** v1.0 MVP, v1.1 (ROCm Opt-In Backend), v1.2 (Operability), v1.3 (Memory & Knowledge), v1.4 (Coding Agent), v1.5 (Web Search — Grounded & Guarded), v1.6 (structural consolidation + a transactional install), and v1.7 (the resident set, a lint gate that can fail, and docs that match the tree) are complete and tagged on `main`. The `villa` control plane is implemented under `cmd/villa/` + `internal/`.

## Build, run & test

```bash
make build         # go build -o ./villa ./cmd/villa (version-stamped)
make build-static  # CGO-free static build — the SC#4 gate CI enforces
make run           # go run ./cmd/villa
make test          # go test ./...
make test-race     # go test -race ./... (cgo test-only; the binary stays CGO-free)
make check         # vet + test + test-race (pre-commit gate)
make lint          # pinned golangci-lint on THIS branch's new issues (LINT_ALL=1 for the whole tree)
```

CI runs `make check`'s equivalents plus `CGO_ENABLED=0 go build`, `go mod verify`,
and a grep asserting no TUI dependency returns. `make check` alone does NOT cover
the CGO-free build — run `make build-static` before pushing if you touched imports.

Go 1.26+. Single module, single static binary built from `./cmd/villa`.

## Working in this codebase

**Code map** (Go is the control plane only — AI services are OSS containers):

- `cmd/villa/` — cobra CLI, one file per subcommand. The tree is assembled in one
  place, `newRoot` in `root.go`: detect, recommend, preflight, model, inference,
  install, up/down/restart/logs, config, status, doctor, verify, recall, dashboard,
  websafe, backend, coding-mode, code, bench, backup, restore, uninstall.
  Host effects live behind injectable `live*Deps` seams (`grep -rn "func live" cmd/villa`).

- `internal/` — `detect` (host probe → typed-Unknown HostProfile; AMD seam in `gpu_amd.go`),
  `recommend` (pure memory-fit `Pick`), `preflight` (reusable BLOCK/WARN gate + `go:embed`
  `rocm-policy.json`), `inference` (`BackendFor` resolver + Backend/Runner/ResidencyProof
  seam; ROCm default + Vulkan fallback), `orchestrate` (Quadlet Render/Reconcile/WriteUnits — the
  `podman`/`systemctl` seam), `backendswap` (transactional switch), `bench` (pure A/B core),
  `residentset` (pure admission control for holding several models loaded at once), plus `status`,
  `dashboard`, `metrics`, `config`, `catalog`, `download`, `modelswap`, `llm`.

  The v1.3–v1.5 packages follow the same pure-core shape: `memory` + `recall`
  (memory-stack decision spine and the chat-index plan/diff algebra), `agent` +
  `codingmode` (the `villa code` delivery spine and the transactional
  enter/exit state machine), `websafe` (the web-search injection guard —
  sanitize/normalize/fence/classify; it reduces and FLAGS, and never claims safe),
  `doctor` (read-only runtime twin of preflight), `backup` (pure manifest-skew
  comparison), `usage` (reset-aware Fold over llama.cpp's monotonic token totals),
  and the persistence trio `pathsafe` / `jsonstore` / `benchstore` + `verifystate`.
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

- **ROCm 7.2.4 is the default inference backend; Vulkan RADV is the fallback** (`villa backend set vulkan`). `BackendFor("")` and `IsROCmFamily("")` must BOTH mean ROCm, or an unset config runs ROCm while skipping the ROCm preflight gate.

## Project

**VillaStraylight**

VillaStraylight is a self-hosted, local AI server stack for privacy-conscious power users who want a ChatGPT/Claude-class experience running entirely on their own hardware. A single Go CLI (`villa`) auto-detects the host hardware, recommends suitable models and configuration, generates Podman Quadlet units, and orchestrates a stack of OSS AI services (local inference, chat UI, and a control dashboard) — initially tuned for AMD Strix Halo on Fedora Workstation 44+, with macOS/Apple Silicon planned later.

**Core Value:** **Run a capable local AI workspace that "just works" after install** — hardware-aware setup that picks the right models and config so inference, chat, and the control dashboard come up healthy on the user's machine, with zero data leaving the box.

### Constraints

- **Tech stack**: Go for all first-party code (CLI, detection, orchestration, dashboard server, gateway) — single-language, single static binary, easy self-hosted distribution.
- **Orchestration**: Podman (rootless) via Quadlet/systemd units — native to Fedora; no Docker dependency.
- **Platform (v1)**: Fedora Workstation 44+ on AMD Strix Halo only. Architecture must not hard-code assumptions that block a later macOS/Apple-Silicon/Metal inference backend.
- **Inference**: llama.cpp `llama-server`, ROCm inference backend primary (Vulkan RADV fallback) — OpenAI-compatible API as the integration contract.
- **Privacy/Security**: Strictly local by default; no telemetry from first-party components; outbound limited to image/model pulls.
- **Performance**: Setup must produce a configuration that actually runs on the detected hardware (right model size/quant/context for the memory envelope) — "runs healthy after install" is the bar.
- **Integration-first**: Reuse mature OSS (Open WebUI, llama.cpp, later Qdrant/SearXNG); build only the control plane.

## Technology Stack

### Languages

- Go 1.26.2 - All first-party code: the `villa` CLI (`cmd/villa/`), hardware detection, recommendation engine, Podman/Quadlet orchestration, dashboard server, and the OpenAI-compatible inference client. Single-language by constraint (single static binary).
- HTML / CSS / JavaScript - The no-build, embedded control-dashboard single-page UI (`internal/dashboard/assets/dashboard.html`, `dashboard.css`, `dashboard.js`). Served verbatim via `go:embed`; there is no JS toolchain/bundler in the `villa` path.
- TOML - Persisted CLI configuration format (`$XDG_CONFIG_HOME/villa/config.toml`).
- JSON - The embedded model catalog (`internal/catalog/seed.json`), the ROCm pin policy (`internal/preflight/rocm-policy.json`), and golden test fixtures.

### Runtime

- Go 1.26.2 (from `go.mod`). Compiles to a single static binary `villa`.
- Target host OS: Fedora Workstation 44+ (Linux kernel >= 6.18.4) on AMD Strix Halo (gfx1151). The binary is the control plane; AI workloads run as rootless Podman containers under the user systemd manager.
- Go modules (`go.mod` / `go.sum`).
- Lockfile: present (`go.sum`).
- Module path: `github.com/MatrixMagician/VillaStraylight`.

### Frameworks

- `github.com/spf13/cobra` v1.10.2 - CLI command tree for `villa` (`cmd/villa/root.go` + per-verb files). Subcommands: see the code map above — `newRoot` in `cmd/villa/root.go` is the single authoritative list.
- Go standard `testing` package - The only test framework. Table-driven tests, `httptest` servers, and byte-for-byte golden fixtures (`cmd/villa/testdata/*.golden*`, `internal/orchestrate` rendered-unit goldens, `internal/metrics/testdata/slots.json`). No third-party assertion or mocking library — seams are injected `func` fields.
- `go build` / `go test` / `go vet` / `gofmt` via `Makefile`.
- `golangci-lint` v2 (config `.golangci.yml`, v2 format) - run by CI on PULL REQUESTS ONLY, gated to NEW issues. The pull_request restriction is load-bearing: `only-new-issues` has no base to diff against on a push event, silently degrades to linting the whole tree, and then fails on that same backlog. `make lint` mirrors that gate locally at the SAME pinned version (`.golangci-version`), diffing against `LINT_BASE` (default `origin/main`); `make LINT_ALL=1 lint` lints the whole tree — as of PR #61 that is **0 issues**, so the
  new-issues gate is a floor to hold, not a workaround around a backlog. Do not reintroduce one.

### Key Dependencies

Four direct dependencies, and five indirect. Everything else the control plane
needs comes from the standard library: the dashboard routes on `net/http`'s mux,
host detection reads procfs and sysfs, and the guided install is a stdin prompt
loop.

- `github.com/spf13/cobra` v1.10.2 - CLI framework (see above).
- `github.com/BurntSushi/toml` v1.6.0 - Marshal/unmarshal of `config.toml` (`internal/config/villaconfig.go`). No string interpolation (mitigates injection on write).
- `github.com/microcosm-cc/bluemonday` v1.0.27 - HTML sanitiser for the web-search guard layer (`internal/websafe/sanitize.go`): strips all markup from fetched, untrusted page content before it reaches the model.
- `golang.org/x/text` v0.23.0 - Unicode normalisation (NFKC) in the same guard layer, so an injection cannot hide behind confusable or zero-width characters.
- Indirect: `spf13/pflag` (via cobra), `inconshreveable/mousetrap` (cobra Windows helper), `aymerick/douceur` + `gorilla/css` + `golang.org/x/net` (via bluemonday).

### Configuration

- TOML file at `$XDG_CONFIG_HOME/villa/config.toml` (resolved via `os.UserConfigDir`). Defined by `VillaConfig` in `internal/config/villaconfig.go`.
- Core fields: `model`, `quant`, `ctx`, `backend` (default `rocm`; `rocm-6.4.4`, `rocm-6.4.4-rocwmma`, `vulkan` also valid — note `internal/catalog/seed.json`'s per-entry `backend_default` OVERRIDES `recommend.defaultBackend`, so the two must be kept in step), `catalog_path`, `dashboard_port` (default `8888`), `chat_port` (default `3000`). The subsystem fields (`memory_enabled`/`embedding_*`, `coding_mode`/`coder_*`, `agent_enabled`, `web_search_*`) and `resident []ResidentModel` are all `omitempty` — `VillaConfig` in `internal/config/villaconfig.go` is the list, not this line.
- Read-only by default: `LoadVilla` returns typed defaults when the file is absent; `SaveVilla` (invoked by `recommend --save` / model swap) writes strictly under the XDG dir with mode `0600`, dir `0700`, and a path-traversal guard. Self-heals zeroed dashboard/chat fields on load (never widens the bind off loopback).
- `internal/catalog/seed.json` - the seed model catalog (`//go:embed seed.json` in `internal/catalog/load.go`). Catalog has a schema version window; an external override path may be supplied via `catalog_path`.
- `internal/preflight/rocm-policy.json` - ROCm pin policy: image-tag allow/deny, kernel floor, firmware floor/deny, required `HSA_OVERRIDE_GFX_VERSION` (`//go:embed rocm-policy.json` in `internal/preflight/floors.go`).
- `internal/orchestrate/quadlet/*.tmpl` - Quadlet unit `text/template`s (`//go:embed quadlet/*.tmpl` in `internal/orchestrate/render.go`): one per service — `ls internal/orchestrate/quadlet/` is the list, not this line.
- `internal/dashboard/assets/` - embedded dashboard UI (`//go:embed all:assets` in `internal/dashboard/embed.go`); `dashboard.html` is parsed as an `html/template` shell (chat-link port injected), css/js served verbatim.
- `Makefile` targets: `help`, `run`, `build` (-> `./villa`), `build-static` (SC#4 CGO-free gate), `test`, `test-race`, `vet`, `fmt`, `lint`, `check` (vet+test+test-race), `tidy`, `clean`.
- `.golangci.yml` - linter config (used by `make lint`).

### Platform Requirements

- Go 1.26.2 toolchain.
- For end-to-end runtime testing: a Fedora host with rootless Podman, `systemctl --user`, and the AMD GPU stack (`/dev/dri`, optionally `/dev/kfd` for ROCm). Host probe tools used when present: `vulkaninfo`, `rocminfo`, `rpm`, `setsebool`, `loginctl`, `journalctl`.
- Fedora Workstation 44+ on AMD Strix Halo (gfx1151), kernel >= 6.18.4, linux-firmware >= 20260110 (firmware 20251125 explicitly denied for ROCm).
- Rootless Podman v5 with the user socket/manager; user lingering enabled (`loginctl enable-linger`) so Quadlet services survive logout/reboot.
- Strictly local; no telemetry from first-party components.

### Container Images Standardized On

| Purpose | Image | Source file |
|---------|-------|-------------|
| Inference (Vulkan RADV, fallback) | `docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…ac7aad` | `internal/inference/backend_vulkan.go` |
| Inference (ROCm 7.2.4, DEFAULT) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1…531a89` | `internal/inference/backend_rocm.go` |
| Inference (ROCm 6.4.4, TG-tuned) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4@sha256:c81f30a7…f150ec62` | `internal/inference/backend_rocm.go` |
| Inference (ROCm 6.4.4 rocWMMA) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4-rocwmma@sha256:9a97129a…43c0141` | `internal/inference/backend_rocm.go` |
| Chat UI (Open WebUI) | `ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a…a9184e` | `internal/orchestrate/openwebui.go` |

## Conventions

### Naming Patterns

- Tests mirror their source file: `backend.go` → `backend_test.go`. Topic-grouped
  check files in `internal/preflight`: `checks_gpu.go`, `checks_memory.go`,
  `checks_podman.go`, `checks_linger.go`, `checks_resources.go`.
- **`live*Deps` constructors** wire a pure core's `Deps` struct to the real host;
  they live in `cmd/villa` and are the only place host I/O is bound.
- **`fake*Deps` types** are test doubles for those same `Deps` structs — the reason
  every command is testable off-hardware.
- **`*ForTest` helpers** (`GTTUsedBytesForTest`, `rocmMarkersForTest`) expose one
  internal seam to tests in another package, rather than widening the real API.
- Typed `Optional` wrappers instead of bare zero values: `detect.Bytes`/`Str`/`Int`/
  `Bool` are aliases of `Optional[T]` (`internal/detect/value.go`), so "unknown" is
  a distinct state from "zero" — this is what makes typed-Unknown degradation work.
- The golden `-update` flag is a package-level `var update = flag.Bool("update", …)`
  per test package (`cmd/villa/detect_test.go`, `internal/orchestrate/render_test.go`).

### Code Style

- `gofmt` (`make fmt` runs `gofmt -w .`). Tabs, standard Go layout.
- `goimports` enforced via `.golangci.yml` — imports are grouped and ordered.
- Linters: the golangci-lint v2 defaults (`errcheck`, `govet`, `ineffassign`,
  `staticcheck`, `unused`) plus `misspell` and `revive`. `revive` is the noisiest
  of them; the tree is currently clean — new code is expected to satisfy it.
- Two exclusion rules, both scoped to `_test.go`: `errcheck` is disabled there,
  and so is `revive`'s `unused-parameter` (test doubles must keep the seam's
  parameter names to satisfy a fixed `Deps` signature).
- `make lint` runs the pinned linter via `go run …@$(GOLANGCI_VERSION)`, where the
  version comes from `.golangci-version` — the single place the pin lives, read by
  the CI workflow too, so a contributor's distro package cannot disagree with CI
  about what is clean. There is deliberately NO fall back to `go vet`: the old
  target was `command -v … && golangci-lint run || (echo "not found" && go vet)`,
  and in `A && B || C` a FAILURE of B runs C, so any finding printed "golangci-lint
  not found" and exited 0 behind a passing vet. The gate could never fail and it
  misreported why. A gate that silently degrades is worse than one that is absent.
  Do not reintroduce that shape.

### Core Architectural Conventions

#### Pure-core + injectable-seam

- Pure logic lives in `internal/*` cores that do no host I/O of their own — they
  take typed input and return typed values, never printing and never calling `os.Exit`.
- Host effects (exec, Unix sockets, `/sys`, filesystem) are injected via a `Deps`
  struct of `func` fields, wired to the real host by a `live*Deps()` closure in `cmd/villa`.
- `internal/orchestrate` is the **intentionally impure orchestration module** — it
  shells to `podman`/`systemctl` and writes Quadlet units. It is no longer the only
  first-party code that touches the filesystem: `internal/pathsafe` is the shared
  filesystem seam (path containment, XDG data-root resolution, atomic writes) and
  `internal/jsonstore` the JSON-document persistence layer on top of it, used by
  `benchstore`, `verifystate` and the memory stores. Everything else routes through
  those two rather than calling `os` directly.
- Consequence: every command is testable off-hardware by passing a `fake*Deps`.

### Error Handling

- Return errors up; wrap with context using `fmt.Errorf("...: %w", err)`. Only the
  command tier turns an error into an exit code.
- **Fail closed** on untrusted input (a hand-edited config, an unknown backend
  string): return an actionable error, never a silent default or fallback.
- **Refuse-with-remediation** in preflight: every non-PASS `CheckResult` carries a
  `Remediation` hint and a `Provenance` string, so a refusal always tells the user
  what to do next and where the finding came from.

### Comments

- Every file opens with a package- or file-level doc comment stating its role and
  the invariant it upholds. Match that density when adding a file.
- Decision/requirement IDs (`D-NN`, `REQ-*`, `SC#N`, `GUARD-NN`, `PRIV-NN`) are the
  canonical cross-reference between code, tests and docs — carry them through.
- Test functions carry a doc comment naming the invariant being guarded, so a
  failure reads as a broken promise rather than a broken assertion.

### Function & Module Design

- **`Deps` struct injection**: a command's host dependencies are a struct of `func`
  fields, not an interface hierarchy — one implementation live, one fake in tests.
- **Thin cobra callers**: `cmd/villa/*.go` commands parse flags, call one core, and
  render. Decision logic in a cobra `RunE` is a smell.
- **Single polymorphism point**: choose a concrete backend only via `BackendFor`.
- Exports: package APIs are deliberately narrow; test-only access goes through a
  `*ForTest` helper rather than exporting the real symbol.

## Architecture

> Full layered system diagram: `docs/ARCHITECTURE.md`.

### Component Responsibilities

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
| residentset | Pure `Admit()` → `Plan`/`Refusal` for the resident model set (LRU evict, no host I/O) | `internal/residentset/admit.go` |
| modelswap | Guarded `villa model swap` ordering core (shared by CLI + dashboard) | `internal/modelswap/modelswap.go` |
| status | Read-model aggregation → frozen `Report` (shared by CLI + dashboard) | `internal/status/status.go` |
| dashboard | Loopback-only stdlib-mux server folding `status` core + embedded SPA | `internal/dashboard/server.go`, `api.go` |
| metrics | llama.cpp `/metrics` scrape (pp/tg timings) | `internal/metrics/llamacpp.go` |
| download | Model weight pull + shard handling | `internal/download/download.go` |
| config | Single source of truth: XDG `config.toml` load/save (`VillaConfig`) | `internal/config/villaconfig.go` |
| prove | The ONE cutover verdict the three transactional cores gate on | `internal/prove/prove.go` |
| residency | The residency-proof drive protocol (idle + under-load), seamed for tests | `internal/residency/residency.go`, `underload.go` |
| openwebui | The Open WebUI HTTP protocol, seamed at the transport; endpoint paths live here and nowhere else | `internal/openwebui/*.go` |
| subsystem | The four optional-subsystem gates: is this subsystem on? | `internal/subsystem/subsystem.go` |
| verify | The verify family's shape: gate → drive → resolve → exit code | `internal/verify/verify.go` |
| install | Install's decisions, its mutate-and-start ordering, and its transaction | `internal/install/*.go` |

This table covers the v1.0–v1.2 spine plus the v1.6 consolidation modules. The
v1.3–v1.5 packages (`memory`, `recall`, `agent`, `codingmode`, `websafe`, `doctor`,
`backup`, `usage`, `pathsafe`, `jsonstore`, `benchstore`, `verifystate`) follow the
same pure-core + `Deps` shape — see the code map above and `docs/ARCHITECTURE.md`.

### Pattern Overview

- **Pure cores, impure edges.** Cores never call `os.Exit` and never print. They return typed values (`Recommendation`, `[]CheckResult`, `Verdict`, `Result`, `Report`); the command tier maps those to exit codes and tables/JSON.
- **Single polymorphism point for inference backends.** `inference.BackendFor(name)` is the only place a config `backend` string becomes a concrete implementation; everything else depends on the `Backend` interface.
- **Config is the single source of truth.** `config.toml` drives recommend → orchestrate; Quadlet units are regenerated from config, never hand-edited as the authority.
- **Honesty-by-construction.** Every probe degrades to a typed `Unknown` (`detect.Bool`/`detect.Bytes`) → WARN, which is DISTINCT from a confident negative → FAIL. CPU fallback is never reported as success.
- **Composition over re-implementation.** `bench --ab` composes `backendswap.Run`; `dashboard` composes `status` and `modelswap`; nothing forks a proven core. v1.6 applied this to the five shapes that HAD been forked: the residency proof (five copies), the Open WebUI protocol (twelve renamed seams), the subsystem gates (read directly in 20+ files), the verify shape (three copies), and install's decisions.
- **A gate is answered once.** `subsystem.MemoryOn`/`WebSearchOn`/`AgentOn`/`CodingModeOn` are the only places a subsystem flag is read as a predicate; a test fails the build if that is bypassed. Enablement is a pure function of an already-loaded config, so one command cannot observe two answers in a single run.
- **Every stack-mutating flow is transactional.** The three swap cores AND `villa install` (ADR-0003) capture before mutating and restore on failure, reporting honestly when a rollback could not complete.

### Layers, data flow & key abstractions

`docs/ARCHITECTURE.md` carries these properly — component diagram, data flow, key
abstractions, and the directory-structure rationale. The one-line shape: command
tier (`cmd/villa/*.go`) → pure cores (`internal/*`) → orchestration
(`internal/orchestrate`) → the running OSS containers plus
`villa-dashboard.service`, networked over `villa.network` with models on
`villa-models.volume`. The unit set is `villa-llama` (plus one `villa-llama-<slug>`
per resident model, named by `orchestrate.ResidentUnitName`), `villa-openwebui`,
`villa-qdrant` + `villa-embed` (v1.3 RAG), and `villa-searxng` + `villa-websafe`
(v1.5 web search) — the last of which bind-mounts the `villa` binary into a
distroless container, which is why the CGO-free build gate is load-bearing.

Persistent state lives in `config.toml` (the single source of truth) and in on-disk
Quadlet units regenerated from it. Cores hold no global mutable state; the dashboard
server guards its one cached value with a `sync` mutex.

### Entry Points

- Location: `cmd/villa/main.go` → `newRoot().Execute()`.
- Triggers: user CLI invocation.
- Responsibilities: build the cobra tree (`cmd/villa/root.go`), dispatch to the per-subcommand `run*` function, map returned error to exit 1.
- Location: `internal/dashboard/server.go` (`NewServer`), launched as a user systemd unit (`villa-dashboard.service`).
- Triggers: `villa dashboard` / boot via systemd.
- Responsibilities: loopback-only `net/http` server folding the shared `status` read-model + embedded SPA.

### Architectural Constraints

- **Backend literals are seam-locked.** Container image/device/`podman`/marker literals MUST live in `internal/inference/` (and `internal/detect/gpu_amd.go`). Enforced by `TestSeamGrepGate` (`internal/inference/seam_test.go`) over both `internal/` and `cmd/villa`.
- **Impurity is confined to named seams.** `os/exec` touch lives in `internal/orchestrate/systemd.go`; unit writing in `WriteUnits`; all other filesystem access goes through `internal/pathsafe` (containment + atomic writes) and `internal/jsonstore`. Render/Reconcile must stay pure, and a core must not reach for `os` directly.
- **No silent CPU fallback.** Offload assert requires BOTH log-scrape AND sysfs GTT-delta; an unevaluable signal → WARN, a confident absence → FAIL.
- **Loopback-only binds.** Dashboard binds `127.0.0.1` via `net.JoinHostPort`; never `:port`/`0.0.0.0` (PRIV-01, `internal/dashboard/server.go`).
- **No shell interpolation.** All host commands are fixed-arg `exec.Command`; model names are catalog-resolved, never shell-interpolated.
- **`--json`/dashboard contracts are byte-frozen.** Evolve append-only + bump schema version; golden tests guard them (`cmd/villa/testdata/*.golden*` — most end `.golden`, a couple `.golden.json`; nothing ends `.json.golden`).
- **No telemetry.** First-party components emit none; outbound limited to image/model pulls (asserted in `status`).
- **Single static binary.** No Podman full-bindings dependency; Podman is controlled via fixed-arg CLI / REST-over-socket.

### Error Handling

- Typed-Unknown degradation: missing tool / unparseable output → `Unknown` → WARN, never a false hard block (`internal/preflight`, `internal/detect`).
- Typed tool errors: `orchestrate.ErrToolNotFound` (missing binary → soft) vs `ErrCommandFailed` (ran non-zero with no output → hard) (`internal/orchestrate/systemd.go`).
- Transactional rollback: any mutate error or non-pass prove → verbatim restore, with honest rollback-incomplete reporting (`internal/backendswap/backendswap.go`).

## Agent skills

### Issue tracker

GitHub Issues on `MatrixMagician/VillaStraylight`, via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical roles, each label string equal to its name. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
