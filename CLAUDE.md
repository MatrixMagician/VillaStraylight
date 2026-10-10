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
- `docs/spec/v1.11-workspace-agent.md` — the workspace agent (`villa work`), now
  built: read it before touching a task, an approval, the sandbox or tools mode.
  Its status block names the open items; the gap that was not in §13 is closed by
  `villa sandbox build`
- `docs/spec/v1.8-villa-update.md` — the `villa update` design, now implemented:
  read it before touching pins. Note §7.1's migration hazard — most
  `EmbedImage()` callers are probe helpers, NOT pins, and a mechanical rewrite of
  that accessor would drag the probe machinery into the update path
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

**Shipped:** v1.0 through v1.18, each tagged on `main`; the tags, their GitHub
release notes and `docs/adr/` are the history. The compiled-in pins were re-vetted on hardware on 2026-09-11 (#205; the rebuilt `rocm-7.2.4` tag was refused, see `docs/RELEASING.md`) and the first signed pin manifest is published (#207: serial 2, valid until 2027-09-11, first attached to the v1.11 release and carried forward by the release workflow), so `--check` returns a real verdict. The fetch URL names the *latest* release, so the release workflow carries `pins.json` and `pins.json.sig` forward to each new tag (`docs/RELEASING.md` § 2); a new manifest is signed offline and uploaded over that copy only when a pin moved. The `villa` control plane is implemented under `cmd/villa/` + `internal/`.

## Build, run & test

```bash
make build         # go build -o ./villa ./cmd/villa (version-stamped)
make build-static  # CGO-free static build — the SC#4 gate CI enforces
make run           # go run ./cmd/villa
make test          # go test ./...
make test-race     # go test -race ./... (cgo test-only; the binary stays CGO-free)
make check         # vet + test + test-race (pre-commit gate)
make dev-deploy    # dev host only: build-static, restart villa-dashboard.service, villa doctor
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
  install (`--coding-agent`, `--web-search`, `--workspace-agent`, `--voice`, `--image`),
  up/down/restart/logs, config, status, doctor, verify, recall, dashboard,
  websafe, inferproxy (the hidden `inferproxy-serve` subcommand mirrors websafe-serve's
  bind-mounted-binary shape — GHSA-gvp9, ADR-0011), backend, speculation, coding-mode,
  code (Crush, or Claude Code via --agent claude), tools-mode, workspace, work, task,
  sandbox build (builds the task image on this host and records its digest as the
  effective pin), sandbox-bridge (the in-VM half of a task; never run by hand), bench,
  eval (the capability suite against the served model's eval baseline, ADR-0018),
  backup, restore, update, uninstall.
  Host effects live behind injectable `live*Deps` seams (`grep -rn "func live" cmd/villa`).

- `internal/` — `detect` (host probe → typed-Unknown HostProfile; AMD seam in `gpu_amd.go`),
  `recommend` (pure memory-fit `Pick`), `preflight` (reusable BLOCK/WARN gate + `go:embed`
  `rocm-policy.json`), `inference` (`BackendFor` resolver + Backend/Runner/ResidencyProof
  seam; ROCm default + Vulkan fallback), `orchestrate` (Quadlet Render/Reconcile/WriteUnits — the
  `podman`/`systemctl` seam), `stackapply` (the one path from a target config to written units:
  every render input, the inference-secret heal, then crush.json's stale copy of that key through
  `HealAgentConfig` (ADR-0019), write, stop and remove the orphaned units + reload, ADR-0013,
  ADR-0035; and `Transact`,
  the swap transaction frame that owns the stack lock, restart set, proof and rollback,
  ADR-0015), `backendswap` (the backend / speculation / tools-mode swap changes), `bench` (pure A/B core),
  `residentset` (pure admission control for holding several models loaded at once), plus `status`,
  `dashboard`, `metrics`, `config`, `catalog`, `download`, `modelswap`, `llm`, and `inprobe` (the one
  home for the in-network curl-probe doctrine — exit-code mapping and typed-Unknown health mapping —
  shared by `status`'s memory/web-search health checks and `install`'s memory probe).
  `eval` (ADR-0018) is the pure capability-suite core: embedded capability cases
  (`cases.json`, sha256 pinned beside `SuiteVersion`), a grader table keyed by kind,
  and `Compare` onto `verify.Status`; `evalstore` holds the eval baselines in
  `eval-baselines.json` (jsonstore, one per model + quant + suite version); it is a
  backup entry, restored verbatim with a WARN naming the baselines that drops (ADR-0020).

  The v1.3–v1.5 packages follow the same pure-core shape: `memory` + `recall`
  (memory-stack decision spine and the chat-index plan/diff algebra), `agent` +
  `codingmode` (the `villa code` delivery spine and the enter/exit swap
  change; crush.json drift is reported and never written over the operator's edits, AGENT-04,
  except a drift that is only villa's inference key, which `villa code` and every stack apply
  rewrite after keeping `crush.json.bak`, ADR-0019), `websafe` (the web-search injection guard —
  sanitize/normalize/fence/classify; it reduces and FLAGS, and never claims safe),
  `doctor` (read-only runtime twin of preflight; it decides from the loaded config,
  ADR-0017, and its seams are raw reads), `backup` (the one ordered entry
  registry, ADR-0020, and the pure manifest-skew comparison), `usage` (reset-aware Fold over llama.cpp's monotonic token totals),
  and the persistence trio `pathsafe` / `jsonstore` / `benchstore` + `verifystate`.
  `voice` (ADR-0030) is the voice subsystem's pure core: the two reservation
  footprints, the two services' network identities (every voice URL villa composes
  comes from `voice.STT` / `voice.TTS`), and the round-trip proof `voice.Prove`
  shared by `install --voice`, `verify voice` and `update voice`; the curl legs live
  in `cmd/villa/voice.go`.

  The v1.11 workspace-agent packages: `workspace` (the grant list and its
  refusals), `approval` (the Action × Mode table; deletion asks in every mode, as a
  pattern, not a proof), `taskstore` (the record, its state machine, the log; never
  deletes), `crushapi` (the Crush server client + the bridge's stdio protocol),
  `grounding` (the post-run claim audit; it flags, never edits, and a clean audit is
  "the auditor found none") and `taskrun` (the runner, hosted by the dashboard service;
  `villa work` and `villa task` are its loopback HTTP clients).

  Image generation (#312, ADR-0032) is `subsystem.Image`: `catalog.Image(id)` over the
  compiled-in image table (`internal/catalog/images.json`, never `catalog_path`),
  the `image` reservation row, `stackapply.ImageServe` (the one catalog-to-renderer
  translation), `orchestrate/image.go` + `image.container.tmpl` (the `villa-image`
  sd-server unit and Open WebUI's `automatic1111` env group), and in `inference`
  `ImageServerArgs` (the sd-server flags; explicit `--backend/--params-backend` on the
  Vulkan device token, `--eager-load`), `VulkanGPUAccess` (the one device-access set
  the chat unit and the image unit both render) and `ImageOffloadVerdict` (the
  placement-line fold: RAM > 0 is a FAIL). `cmd/villa/install_image.go` holds the
  pre-stage and THE image proof install, doctor (`IMG-DOC-residency`) and
  `update image` share.
  Deeper detail: `docs/ARCHITECTURE.md`, `docs/DEVELOPMENT.md`.

**Conventions & gotchas (non-obvious — read before editing):**

- **Config is the single source of truth.** Quadlet units are regenerated from config,
  never hand-edited.

- **A new `.container` unit joins the subsystem unit registry** (`internal/subsystem/units.go`)
  under the subsystem it moves with. `TestSubsystemUnitsMatchTheRenderedUnits` fails the build
  on a rendered `.container` the registry does not name, and the registry is also the removal
  set: a stack apply stops and removes a registry unit the config no longer renders, and only
  those (`orchestrate.Orphans`, ADR-0035).

- **Dynamic binary trap:** `make build` links `./villa` dynamically, and `villa-websafe`,
  `villa-inferproxy`, and every task VM bind-mount that file into a distroless image, so
  after a plain `make build` a websafe/inferproxy restart crash-loops with "No such file
  or directory". On the dev host build with `make build-static`; it is the same gate CI
  enforces.

- **Dashboard binary trap:** `villa status`/`recommend` run fresh from `./villa`, but
  `villa-dashboard.service` is long-lived — after `make build` you MUST
  `systemctl --user restart villa-dashboard.service` for dashboard code changes to take effect.
  The unit's `ExecStart` is this working tree's `./villa`, so a `git checkout` plus a rebuild
  silently changes what the live stack serves: know which branch is checked out before you
  rebuild. `make dev-deploy` runs the whole sequence (static build, dashboard restart, doctor).

- **After a kernel or linux-firmware upgrade, run `villa bench` once.** The report fingerprints
  `kernel_version`, so `bench-reports.jsonl` carries one comparable datapoint per kernel.

- **Inference seam grep-gate (`TestSeamGrepGate`):** backend marker strings (`ROCm0`,
  `Vulkan0`, `HSA_OVERRIDE…`, image tags) must stay behind `internal/inference` +
  `internal/detect/gpu_amd.go`; a managed-service image literal in `internal/orchestrate`
  is allowed only in the files the gate's allowlist names. The gate walks both `internal/`
  and `cmd/villa` — a leaked literal fails the build.

- **Inference client gate (`TestInferenceReachedOnlyThroughClient`, ADR-0014):** call
  llama-server only through `inferenceClient(cfg)` / `inNetworkInferenceClient(cfg)`
  (`cmd/villa/inference.go`). A read of `InferenceSecret`, an address accessor or a
  route literal elsewhere fails the build unless the gate lists the file with a reason.

- **`--json`/dashboard contracts are byte-frozen by golden tests** (`testdata/*.golden*`).
  Evolve append-only + schema-bump; refreeze intentionally with `go test … -update`, and read
  the diff: a golden that changed in a way you cannot explain is a regression. `internal/status`
  (the read-model the CLI and dashboard both fold) must not import a store package; projection
  from a store into a `status` type happens in `cmd/villa` and is wired through `Deps`.

- **Offload is offload-asserting, never liveness:** a silent/partial CPU fallback is a FAIL
  (`ResidencyProof`), never a false-green.

- **ROCm 10.0 is the default inference backend (ADR-0022); Vulkan RADV is the fallback** (`villa backend set vulkan`). `"rocm"` is the default's name, not a version: it follows the default, and `rocm-7.2.4` keeps the old image reachable by name. `BackendFor("")` and `IsROCmFamily("")` must BOTH mean ROCm, or an unset config runs ROCm while skipping the ROCm preflight gate.

## Project

**VillaStraylight**

VillaStraylight is a self-hosted, local AI server stack for privacy-conscious power users who want a ChatGPT/Claude-class experience running entirely on their own hardware. A single Go CLI (`villa`) auto-detects the host hardware, recommends suitable models and configuration, generates Podman Quadlet units, and orchestrates a stack of OSS AI services (local inference, chat UI, and a control dashboard) — initially tuned for AMD Strix Halo on Fedora Workstation 44+, with macOS/Apple Silicon planned later.

**Core Value:** **Run a capable local AI workspace that "just works" after install** — hardware-aware setup that picks the right models and config so inference, chat, and the control dashboard come up healthy on the user's machine, with zero data leaving the box.

### Constraints

- **Tech stack**: Go for all first-party code (CLI, detection, orchestration, dashboard server, and the bind-mounted `websafe-serve` / `inferproxy-serve` helpers) — single-language, single static binary, easy self-hosted distribution.
- **Orchestration**: Podman (rootless) via Quadlet/systemd units — native to Fedora; no Docker dependency.
- **Platform (v1)**: Fedora Workstation 44+ on AMD Strix Halo only. Architecture must not hard-code assumptions that block a later macOS/Apple-Silicon/Metal inference backend.
- **Inference**: llama.cpp `llama-server`, ROCm inference backend primary (Vulkan RADV fallback) — OpenAI-compatible API as the integration contract.
- **Privacy/Security**: Strictly local by default; no telemetry from first-party components; outbound limited to image/model pulls.
- **Performance**: Setup must produce a configuration that actually runs on the detected hardware (right model size/quant/context for the memory envelope) — "runs healthy after install" is the bar.
- **Integration-first**: Reuse mature OSS (Open WebUI, llama.cpp, Qdrant, SearXNG); build only the control plane.

## Technology Stack

### Languages

- Go 1.26.9 - All first-party code: the `villa` CLI (`cmd/villa/`), hardware detection, recommendation engine, Podman/Quadlet orchestration, dashboard server, and the OpenAI-compatible inference client. Single-language by constraint (single static binary).
- HTML / CSS / JavaScript - The no-build, embedded control-dashboard single-page UI (`internal/dashboard/assets/dashboard.html`, `dashboard.css`, `dashboard.js`). Served verbatim via `go:embed`; there is no JS toolchain/bundler in the `villa` path.
- TOML - Persisted CLI configuration format (`$XDG_CONFIG_HOME/villa/config.toml`).
- JSON - The embedded model catalog (`internal/catalog/seed.json`), the ROCm pin policy (`internal/preflight/rocm-policy.json`), and golden test fixtures.

### Runtime

- Go 1.26.9 (from `go.mod`). Compiles to a single static binary `villa`.
- Target host OS: Fedora Workstation 44+ (Linux kernel >= 6.18.4) on AMD Strix Halo (gfx1151). The binary is the control plane; AI workloads run as rootless Podman containers under the user systemd manager.
- Go modules (`go.mod` / `go.sum`).
- Lockfile: present (`go.sum`).
- Module path: `github.com/MatrixMagician/VillaStraylight`.

### Frameworks

- `github.com/spf13/cobra` v1.10.2 - CLI command tree for `villa` (`cmd/villa/root.go` + per-verb files). Subcommands: see the code map above — `newRoot` in `cmd/villa/root.go` is the single authoritative list.
- Go standard `testing` package - The only test framework. Table-driven tests, `httptest` servers, and byte-for-byte golden fixtures (`cmd/villa/testdata/*.golden*`, `internal/orchestrate` rendered-unit goldens, `internal/metrics/testdata/slots.json`). No third-party assertion or mocking library — seams are injected `func` fields.
- `go build` / `go test` / `go vet` / `gofmt` via `Makefile`.
- `golangci-lint` v2 (config `.golangci.yml`, v2 format) - run by CI on PULL REQUESTS ONLY, gated to NEW issues. The pull_request restriction is load-bearing: `only-new-issues` has no base to diff against on a push event, silently degrades to linting the whole tree, and then fails on that same backlog. `make lint` mirrors that gate locally at the SAME pinned version (`.golangci-version`), diffing against `LINT_BASE` (default `origin/main`); `make LINT_ALL=1 lint` lints the whole tree, and the tree is clean, so the
  new-issues gate is a floor to hold, not a workaround around a backlog. Do not reintroduce one.

### Key Dependencies

Four direct dependencies, and five indirect. Everything else the control plane
needs comes from the standard library: the dashboard routes on `net/http`'s mux,
host detection reads procfs and sysfs, and the guided install is a stdin prompt
loop. The count is deliberate: a fifth direct dependency needs an argument, not a
convenience.

- `github.com/spf13/cobra` v1.10.2 - CLI framework (see above).
- `github.com/BurntSushi/toml` v1.6.0 - Marshal/unmarshal of `config.toml` (`internal/config/villaconfig.go`). No string interpolation (mitigates injection on write).
- `github.com/microcosm-cc/bluemonday` v1.0.27 - HTML sanitiser for the web-search guard layer (`internal/websafe/sanitize.go`): strips all markup from fetched, untrusted page content before it reaches the model.
- `golang.org/x/text` v0.39.0 - Unicode normalisation (NFKC) in the same guard layer, so an injection cannot hide behind confusable or zero-width characters.
- Indirect: `spf13/pflag` (via cobra), `inconshreveable/mousetrap` (cobra Windows helper), `aymerick/douceur` + `gorilla/css` + `golang.org/x/net` (via bluemonday).

### Configuration

- TOML file at `$XDG_CONFIG_HOME/villa/config.toml` (resolved via `os.UserConfigDir`). Defined by `VillaConfig` in `internal/config/villaconfig.go`.
- Core fields: `model`, `quant`, `ctx` (a `model swap` resets it to the target's `default_ctx` when the target does not fit at it, ADR-0024), `speculation` (`off`/`ngram`/`draft`, unset renders
  off; written by `villa speculation set`, never `config set`), `vision` (bool,
  absent renders no projector flag; written by `recommend --save`/`install` and by
  `model swap` for its target, ADR-0023, each of which pulls the projector first), `backend` (default `rocm`, which is ROCm 10.0 per ADR-0022; `rocm-10.0`, `rocm-7.2.4`, `rocm-6.4.4`, `rocm-6.4.4-rocwmma`, `vulkan` also valid — note `internal/catalog/seed.json`'s per-entry `backend_default` OVERRIDES `recommend.defaultBackend`, so the two must be kept in step), `catalog_path`, `dashboard_port` (default `8888`), `chat_port` (default `3000`). The subsystem fields (`memory_enabled`/`embedding_*`/`reranker` (the memory stack's reranker gate, read as `subsystem.RerankOn`, written by `install` once the weights are staged, ADR-0028), `coding_mode`/`coder_*`, `agent_enabled`, `web_search_*`), `resident []ResidentModel`, and the v1.11 fields (`tools_mode` written by `tools-mode enter|exit` and `install --workspace-agent`; `workspace_agent` by `install --workspace-agent`; `workspace []string` by `workspace add|remove`; `sandbox_memory` default `4g` and `sandbox_cpus` default `4`, hand-edited, read at task launch) are all `omitempty` — `VillaConfig` in `internal/config/villaconfig.go` is the list, not this line. `inference_secret` (GHSA-qxg9, ADR-0011) is the one exception to "opt-in only": it is generated once via `config.GenerateInferenceSecret` and persisted UNCONDITIONALLY (inference has no opt-in gate, unlike `web_loader_secret`/`searxng_secret`), on `install`'s first render and self-healed, before the render, by every stack apply (`internal/stackapply`, ADR-0013) for an existing config.toml that predates it — see `internal/orchestrate.InferenceSecretEnvFilePath`.
- Read-only by default: `LoadVilla` returns typed defaults when the file is absent; `SaveVilla` (invoked by `recommend --save` / model swap) writes strictly under the XDG dir with mode `0600`, dir `0700`, and a path-traversal guard. Self-heals zeroed dashboard/chat fields on load (never widens the bind off loopback).
- `internal/catalog/seed.json` - the seed model catalog (`//go:embed seed.json` in `internal/catalog/load.go`). Catalog has a schema version window; an external override path may be supplied via `catalog_path`.
- `internal/preflight/rocm-policy.json` - ROCm pin policy: image-tag allow/deny, kernel floor, firmware floor/deny, required `HSA_OVERRIDE_GFX_VERSION` (`//go:embed rocm-policy.json` in `internal/preflight/floors.go`).
- `internal/orchestrate/quadlet/*.tmpl` - Quadlet unit `text/template`s (`//go:embed quadlet/*.tmpl` in `internal/orchestrate/render.go`): one per service — `ls internal/orchestrate/quadlet/` is the list, not this line.
- `internal/dashboard/assets/` - embedded dashboard UI (`//go:embed all:assets` in `internal/dashboard/embed.go`); `dashboard.html` is parsed as an `html/template` shell (chat-link port injected), css/js served verbatim.
- `Makefile` targets: `help`, `run`, `build` (-> `./villa`), `build-static` (SC#4 CGO-free gate), `test`, `test-race`, `vet`, `fmt`, `lint`, `check` (vet+test+test-race), `dev-deploy` (dev host only: build-static, dashboard restart, doctor), `govulncheck`, `ci`, `tidy`, `clean`.
- `.golangci.yml` - linter config (used by `make lint`).

### Platform Requirements

- Go 1.26.9 toolchain.
- For end-to-end runtime testing: a Fedora host with rootless Podman, `systemctl --user`, and the AMD GPU stack (`/dev/dri`, optionally `/dev/kfd` for ROCm). Host probe tools used when present: `vulkaninfo`, `rocminfo`, `rpm`, `setsebool`, `loginctl`, `journalctl`.
- Fedora Workstation 44+ on AMD Strix Halo (gfx1151), kernel >= 6.18.4, linux-firmware >= 20260110 (firmware 20251125 explicitly denied for ROCm).
- Rootless Podman v5 with the user socket/manager; user lingering enabled (`loginctl enable-linger`) so Quadlet services survive logout/reboot.
- Strictly local; no telemetry from first-party components.

### Container Images Standardized On

| Purpose | Image | Source file |
|---------|-------|-------------|
| Inference (Vulkan RADV, fallback) | `docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:521fd599…3ecfc5ab` | `internal/inference/backend_vulkan.go` |
| Inference (ROCm 10.0, DEFAULT, ADR-0022) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-10.0@sha256:3893b3e5…fc79bb1` | `internal/inference/backend_rocm.go` |
| Inference (ROCm 7.2.4, former default, `rocm-7.2.4`) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1…531a89` | `internal/inference/backend_rocm.go` |
| Inference (ROCm 6.4.4, TG-tuned) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4@sha256:1c655ca0…05053947` | `internal/inference/backend_rocm.go` |
| Inference (ROCm 6.4.4 rocWMMA) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4-rocwmma@sha256:9a97129a…43c0141` | `internal/inference/backend_rocm.go` |
| Chat UI (Open WebUI) | `ghcr.io/open-webui/open-webui:main@sha256:1a6399d2…8dc8b924` | `internal/orchestrate/openwebui.go` |
| Speech-to-text (whisper.cpp, Vulkan; ADR-0030) | `ghcr.io/ggml-org/whisper.cpp:main-vulkan@sha256:8bbf6a98…7bc88955` | `internal/orchestrate/voice.go` |
| Text-to-speech (Kokoro-FastAPI, CPU; ADR-0030) | `ghcr.io/remsky/kokoro-fastapi-cpu:v0.9.0@sha256:7f9a2569…7d9f2985` | `internal/orchestrate/voice.go` |

The table is not exhaustive: the memory, web-search, extractor and image units pin their
images in `internal/orchestrate/{memory,searxng,websafe,extract,image}.go`, and
`pins.Table()` (`internal/pins/pins.go`) is the enumerable list of every pinned component.

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
| gguf | The GGUF header + KV section reader, never the tensors: the witness the catalog's fit dimensions are cross-checked against (ADR-0007) | `internal/gguf/gguf.go` |
| preflight | Reusable host-prep gate → `[]CheckResult` (BLOCK/WARN tiers, fail-soft) | `internal/preflight/preflight.go` |
| inference | Backend-neutral seam: `BackendFor`, `Backend` iface, offload/residency proof; `Client`, the one authenticated caller of llama-server (address, api key, every route; ADR-0014) | `internal/inference/*.go`, `client.go` |
| orchestrate | Render Quadlet units (pure) + reconcile + host-touching systemd seam | `internal/orchestrate/*.go` |
| stackapply | Stack apply: derive every render input from the config (served model, coding descriptor, resident slots), heal the inference secret and then crush.json's stale copy of it (`HealAgentConfig`, ADR-0019), render, write what changed, stop and remove the registry units it no longer renders (`orchestrate.Orphans`, ADR-0035), reload; every unit-writing verb but install goes through it (ADR-0013). `Transact` is the swap transaction frame: stack lock, capture, apply, restart of every changed running unit, proof, rollback (ADR-0015) | `internal/stackapply/stackapply.go`, `transact.go` |
| backendswap | The `backend set` / `speculation set` / `tools-mode` swap changes: no-op test, guards, the field written; run through `stackapply.Transact` | `internal/backendswap/backendswap.go` |
| bench | Pure A/B throughput core; `--ab` composes `backendswap.Run` | `internal/bench/bench.go` |
| residentset | Pure `Admit()` → `Plan`/`Refusal` for the resident model set (LRU evict, no host I/O) | `internal/residentset/admit.go` |
| modelswap | Guarded `villa model swap` ordering core (shared by CLI + dashboard) | `internal/modelswap/modelswap.go` |
| status | Read-model aggregation → frozen `Report` (shared by CLI + dashboard) | `internal/status/status.go` |
| doctor | Read-only health verdict: `Aggregate(cfg, Deps)` decides from the loaded config which subsystems it reports on, the unit drift plan, SBX-02 and tools/agent drift; its seams are raw reads plus the four proofs and the preflight gates (ADR-0017) | `internal/doctor/doctor.go`, `decide.go` |
| dashboard | Loopback-only stdlib-mux server folding `status` core + embedded SPA | `internal/dashboard/server.go`, `api.go` |
| metrics | Parsers for llama.cpp `/metrics` + `/slots` (pp/tg gauges, usage counters); the keyed scrape is `inference.Client`'s | `internal/metrics/llamacpp.go` |
| inprobe | The in-network curl-probe doctrine: exit-code mapping, typed-Unknown health mapping, TTL-bounded pair cache | `internal/inprobe/inprobe.go` |
| download | Model weight pull + shard handling | `internal/download/download.go` |
| config | Single source of truth: XDG `config.toml` load/save (`VillaConfig`) | `internal/config/villaconfig.go` |
| prove | The ONE cutover verdict the three transactional cores gate on | `internal/prove/prove.go` |
| residency | The residency-proof drive protocol (idle + under-load), seamed for tests | `internal/residency/residency.go`, `underload.go` |
| openwebui | The Open WebUI HTTP protocol, seamed at the transport; endpoint paths live here and nowhere else | `internal/openwebui/*.go` |
| subsystem | The optional-subsystem gates (`On` and its named `*On` accessors): is this subsystem on? Also the unit registry (`units.go`) | `internal/subsystem/subsystem.go` |
| verify | The verify family's shape: gate → drive → resolve → exit code | `internal/verify/verify.go` |
| install | The whole install flow behind `Run(ctx, Deps, Opts) Result`: decisions, ordering, transaction, narration via `Emit` | `internal/install/*.go` |
| pins | The compiled-in, enumerable pin registry: schema, allowlist, fallback, serial floor | `internal/pins/pins.go` |
| pinstate | What THIS host runs: effective pins, retained tuples, serial, CheckedAt | `internal/pinstate/store.go` |
| pinresolve | The one answer to "what should this component run?" — effective, else vetted | `internal/pinresolve/resolve.go` |
| manifest | The signed pin-manifest wire format; signature over VERBATIM published bytes | `internal/manifest/manifest.go` |
| manifestverify | Signature + monotonic serial + expiry: a signature proves authorship, not currency | `internal/manifestverify/*.go` |
| updatecheck | The read-only report, and the Reject that must not read as up-to-date | `internal/updatecheck/check.go` |
| updatefetch | The ONE outbound request a check makes; strictly on-command | `internal/updatefetch/fetch.go` |
| updateflow | The per-subsystem transaction: prove current → capture → mutate → prove → commit | `internal/updateflow/updateflow.go` |
| prune | Reference-counted image removal — the only image deletion in this project | `internal/prune/prune.go` |
| snapshotprune | Data-snapshot retention — the only snapshot deletion in this project | `internal/snapshotprune/snapshotprune.go` |
| workspace | The registered grant list: `Register`/`Remove`/`Registered`, typed refusals, fail-closed at registration | `internal/workspace/workspace.go` |
| approval | Pure Action × Mode table for a task's tool call: allow, ask, deny; deletion asks in every mode (a pattern, not a proof) | `internal/approval/approval.go` |
| taskstore | The task record, its state machine (`Exit(State)` is the one state → exit-code map), the append-only log; never deletes | `internal/taskstore/*.go` |
| crushapi | The Crush server client + the bridge's one-object-per-line stdio protocol | `internal/crushapi/*.go` |
| grounding | The post-run claim audit: one completion per document, reports, never edits; a clean audit is "the auditor found none" | `internal/grounding/grounding.go` |
| backendswap.RunTools | The `tools-mode enter` / `exit` swap: a boolean axis, the ctx-floor fit guard, one real tool call in the proof (`liveToolsProve`) | `internal/backendswap/backendswap.go` |
| taskrun | The runner: one task at a time, hosted by the dashboard service; every decision through `approval`, every terminal state through `taskstore` | `internal/taskrun/*.go` |
| catalog.Image + inference.ImageOffloadVerdict | Image generation (ADR-0032): the compiled-in image table, the eager-loaded `villa-image` sd-server unit rendered from the seam's flags and device access, and the two-signal placement proof (any param byte in RAM is a FAIL) | `internal/catalog/image.go`, `internal/orchestrate/image.go`, `internal/inference/image_server.go`, `internal/inference/image_offload.go` |

Packages missing from the table (`memory`, `recall`, `agent`, `codingmode`, `websafe`,
`backup`, `usage`, `pathsafe`, `jsonstore`, `benchstore`, `verifystate`, `voice`, `eval`,
`evalstore`, `llm`, `stacklock`, `uninstall`) follow the same pure-core + `Deps` shape — see the code map above and `docs/ARCHITECTURE.md`.

### Pattern Overview

- **Pure cores, impure edges.** Cores never call `os.Exit` and never print. They return typed values (`Recommendation`, `[]CheckResult`, `Verdict`, `Result`, `Report`); the command tier maps those to exit codes and tables/JSON.
- **Single polymorphism point for inference backends.** `inference.BackendFor(name)` is the only place a config `backend` string becomes a concrete implementation; everything else depends on the `Backend` interface.
- **Config is the single source of truth.** `config.toml` drives recommend → orchestrate; Quadlet units are regenerated from config, never hand-edited as the authority.
- **Honesty-by-construction.** Every probe degrades to a typed `Unknown` (`detect.Bool`/`detect.Bytes`) → WARN, which is DISTINCT from a confident negative → FAIL. CPU fallback is never reported as success.
- **Composition over re-implementation.** `bench --ab` composes `backendswap.Run`; `dashboard` composes `status` and `modelswap`; nothing forks a proven core. The residency proof, the Open WebUI protocol, the subsystem gates, the verify shape and install's decisions each have one home; extend it rather than copying it.
- **A gate is answered once.** The `subsystem` package's `*On` predicates (`MemoryOn`, `WebSearchOn`, `ImageOn`, …) are the only places a subsystem flag is read as a predicate; a test fails the build if that is bypassed. Enablement is a pure function of an already-loaded config, so one command cannot observe two answers in a single run.
- **Every stack-mutating flow is transactional and holds the stack lock.** Every swap runs in one frame, `stackapply.Transact` (ADR-0015), which takes the lock (ADR-0010) and restarts every changed running unit; `villa install` (ADR-0003) AND `villa update` capture before mutating and restore on failure, reporting honestly when a rollback could not complete. `update` adds a step the swaps never needed: it proves the CURRENT state first, so a pre-existing failure is a refusal rather than an update failure villa did not cause.

- **The image is not always the state being changed.** Chat and memory own a mutable data volume (`subsystem.OwnsPersistentState`), so their update is a stopped window — stop → snapshot → mutate → start — and their rollback restores the data as well as the pin. The stop is load-bearing: a volume exported from under a running service is a torn copy. A failed capture REFUSES, unlike the failed prune/cleanup that WARNs, because a capture failure happens before any mutation while cleanup happens after the update already succeeded.

- **A pin is two values.** The VETTED pin (compiled into `pins.Table`, a build-time fact that cannot be absent) and the EFFECTIVE pin (in `pinstate`, what this host runs, a runtime fact that routinely is). They are separate packages because they FAIL differently; `pinresolve` is where they meet. Rendered units derive their image through `livePinnedRender`, never a constant — a test fails the build if any cmd verb calls `orchestrate.Render` directly.

- **Checks are strictly on-command.** Nothing polls for updates and no other verb checks opportunistically; `status` and `doctor` read the LAST RECORDED check and its age. This is what keeps "zero telemetry" unqualified — if checks ever become automatic, that claim must change with them.

### Layers, data flow & key abstractions

`docs/ARCHITECTURE.md` carries these properly — component diagram, data flow, key
abstractions, and the directory-structure rationale. The one-line shape: command
tier (`cmd/villa/*.go`) → pure cores (`internal/*`) → orchestration
(`internal/orchestrate`) → the running OSS containers plus
`villa-dashboard.service`, networked over `villa.network` with models on
`villa-models.volume`. The unit set is `villa-llama` (plus one `villa-llama-<slug>`
per resident model, named by `orchestrate.ResidentUnitName`), `villa-openwebui`,
`villa-qdrant` + `villa-embed` (v1.3 RAG) + `villa-rerank` (the reranker on the embedder's pin, only with `reranker = true`, ADR-0028) + `villa-extract` (Apache Tika with OCR on its own `extractor` pin, only with `extractor = true`, ADR-0033), `villa-searxng` + `villa-websafe`
(v1.5 web search), and `villa-stt` + `villa-tts` (voice, ADR-0030: whisper-server on
Vulkan with its model pre-staged in the models volume, and Kokoro on the CPU; one
gate, `voice_enabled`, one round-trip proof) — the web-search pair bind-mounts the `villa` binary into a
distroless container, which is why the CGO-free build gate is load-bearing — and,
when `subsystem.ImageOn`, `villa-image` (sd-server on Vulkan RADV, ADR-0032),
rendered after the web-search block and before `villa-inferproxy`. The
v1.11 workspace agent adds one long-lived unit, `villa-sandbox.network`
(`Internal=true`), rendered UNCONDITIONALLY like `villa.network` — and, when
`subsystem.SandboxOn`, a managed `villa-inferproxy` unit (issue #199 / GHSA-gvp9,
ADR-0011): `villa-llama` joins `villa.network` ONLY (never the sandbox network,
so a compromised task VM has no route through it to the internet), and
`villa-inferproxy` joins BOTH networks, forwarding ONLY `POST
/v1/chat/completions` and `GET /v1/models` to `villa-llama`'s bare root and
injecting the real `LLAMA_API_KEY` bearer on that outbound leg — so a task's
own request never needs a valid key of its own. `villa-closed.network`
(`Internal=true`, ADR-0036) also renders unconditionally, last: `villa-qdrant`,
`villa-embed`, `villa-rerank`, `villa-extract`, `villa-stt`, `villa-tts` and
`villa-image` join it and nothing else, so they have no route off-box; Open WebUI
joins both it and `villa.network`. An in-network probe runs on its target's
network (`probeCurl` takes it), and doctor's `networks` finding compares the
running containers and networks with the rendered units. No container unit for the task
itself: each task is one `podman run --runtime=krun` named `villa-task-<id>`,
rendered by `orchestrate.RenderSandboxRun`, which bind-mounts the `villa` binary
the same way (the second reason the CGO-free gate is load-bearing).

Persistent state lives in `config.toml` (the single source of truth) and in on-disk
Quadlet units regenerated from it. Cores hold no global mutable state; the dashboard
server holds two `sync` mutexes: `swapMu` serializes model switches and `usageMu` guards
the cumulative-usage write.

### Entry Points

- Location: `cmd/villa/main.go` → `newRoot().Execute()`.
- Triggers: user CLI invocation.
- Responsibilities: build the cobra tree (`cmd/villa/root.go`), dispatch to the per-subcommand `run*` function, map returned error to exit 1.
- Location: `internal/dashboard/server.go` (`NewServer`), launched as a user systemd unit (`villa-dashboard.service`).
- Triggers: `villa dashboard` / boot via systemd.
- Responsibilities: loopback-only `net/http` server folding the shared `status` read-model + embedded SPA.

### Architectural Constraints

- **Backend literals are seam-locked.** Container image/device/`podman`/marker literals MUST live in `internal/inference/` (and `internal/detect/gpu_amd.go`), except in the files the gate's allowlist names (managed-service image literals in `internal/orchestrate`, fixed-arg podman lifecycle calls). Enforced by `TestSeamGrepGate` (`internal/inference/seam_test.go`) over both `internal/` and `cmd/villa`.
- **Impurity is confined to named seams.** Host commands run through the `Deps` seams wired in `cmd/villa` and the few internal edges that own one (`orchestrate/systemd.go`, `inference/runner_podman.go`, `detect/gpu_amd.go`, `preflight/exec.go`, `inprobe`); a pure core never imports `os/exec`. Unit writing in `WriteUnits`; all other filesystem access goes through `internal/pathsafe` (containment + atomic writes) and `internal/jsonstore`. Render/Reconcile must stay pure, and a core must not reach for `os` directly.
- **No silent CPU fallback.** Offload assert requires BOTH log-scrape AND sysfs GTT-delta; an unevaluable signal → WARN, a confident absence → FAIL.
- **Loopback-only binds.** Dashboard binds `127.0.0.1` via `net.JoinHostPort`; never `:port`/`0.0.0.0` (PRIV-01, `internal/dashboard/server.go`).
- **No shell interpolation.** All host commands are fixed-arg `exec.Command`; model names are catalog-resolved, never shell-interpolated.
- **`--json`/dashboard contracts are byte-frozen.** Evolve append-only + bump schema version; golden tests guard them (`cmd/villa/testdata/*.golden*` — most end `.golden`, a couple `.golden.json`; nothing ends `.json.golden`).
- **No telemetry.** First-party components emit none; outbound limited to image/model pulls (asserted in `status`).
- **Single static binary.** No Podman full-bindings dependency; Podman is controlled only through its fixed-arg CLI.

### Error Handling

- Typed-Unknown degradation: missing tool / unparseable output → `Unknown` → WARN, never a false hard block (`internal/preflight`, `internal/detect`).
- Typed tool errors: `orchestrate.ErrToolNotFound` (missing binary → soft) vs `ErrCommandFailed` (ran non-zero with no output → hard) (`internal/orchestrate/systemd.go`).
- Transactional rollback: any mutate error or non-pass prove → verbatim restore, with honest rollback-incomplete reporting (`internal/stackapply/transact.go`).

## Agent skills

### Issue tracker

GitHub Issues on `MatrixMagician/VillaStraylight`, via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical roles, each label string equal to its name. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
