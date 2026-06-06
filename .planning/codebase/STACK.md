# Technology Stack

**Analysis Date:** 2026-06-07

## Languages

**Primary:**
- Go 1.26.2 - All first-party code: the `villa` CLI (`cmd/villa/`), hardware detection, recommendation engine, Podman/Quadlet orchestration, dashboard backend, and the OpenAI-compatible inference client. Single-language by constraint (single static binary).

**Secondary:**
- HTML / CSS / JavaScript - The no-build, embedded control-dashboard single-page UI (`internal/dashboard/assets/dashboard.html`, `dashboard.css`, `dashboard.js`). Served verbatim via `go:embed`; there is no JS toolchain/bundler in the `villa` path.
- TOML - Persisted CLI configuration format (`$XDG_CONFIG_HOME/villa/config.toml`).
- JSON - The embedded model catalog (`internal/catalog/seed.json`), the ROCm pin policy (`internal/preflight/rocm-policy.json`), and golden test fixtures.

> Note: a legacy `web/` React scaffold and `.env.example` exist but are reference-only (superseded by Open WebUI + the `villa` control plane). They are NOT part of the v1.1 architecture.

## Runtime

**Environment:**
- Go 1.26.2 (from `go.mod`). Compiles to a single static binary `villa`.
- Target host OS: Fedora Workstation 44+ (Linux kernel >= 6.18.4) on AMD Strix Halo (gfx1151). The binary is the control plane; AI workloads run as rootless Podman containers under the user systemd manager.

**Package Manager:**
- Go modules (`go.mod` / `go.sum`).
- Lockfile: present (`go.sum`).
- Module path: `github.com/MatrixMagician/VillaStraylight`.

## Frameworks

**Core:**
- `github.com/spf13/cobra` v1.10.2 - CLI command tree for `villa` (`cmd/villa/root.go` + per-verb files). Subcommands: `detect`, `recommend`, `preflight`, `install`, `uninstall`, `up`, `down`, `restart`, `status`, `logs`, `config`, `dashboard`, `model` (`list` / `pull` / `show` / swap), `backend`, `inference`, `bench`.
- `github.com/go-chi/chi/v5` v5.3.0 - HTTP router + middleware for the loopback-only control-dashboard backend (`internal/dashboard/server.go`). Middleware chain: RequestID, RealIP, Logger, Recoverer, plus a custom `requireSameOrigin` guard on `/api`.

**Testing:**
- Go standard `testing` package - The only test framework. Table-driven tests, `httptest` servers, and byte-for-byte golden fixtures (`cmd/villa/testdata/*.golden.json`, `internal/orchestrate` rendered-unit goldens, `internal/metrics/testdata/slots.json`). No third-party assertion or mocking library — seams are injected `func` fields.

**Build/Dev:**
- `go build` / `go test` / `go vet` / `gofmt` via `Makefile`.
- `golangci-lint` (optional; config `.golangci.yml`) - `make lint` runs it if installed, else falls back to `go vet`.

## Key Dependencies

**Critical (direct, from `go.mod`):**
- `github.com/spf13/cobra` v1.10.2 - CLI framework (see above).
- `github.com/go-chi/chi/v5` v5.3.0 - Dashboard HTTP router (see above).
- `github.com/jaypipes/ghw` v0.24.0 - Root-less hardware detection: CPU/arch (`ghw.CPU()` in `internal/detect/cpu.go`) and total physical memory (`ghw.Memory()` in `internal/detect/memory.go`). Never hard-errors on missing perms.
- `github.com/BurntSushi/toml` v1.6.0 - Marshal/unmarshal of `config.toml` (`internal/config/villaconfig.go`). No string interpolation (mitigates injection on write).

**Infrastructure (indirect, from `go.mod`):**
- `github.com/jaypipes/pcidb` v1.1.1 - PCI ID -> human name (transitive via ghw).
- `github.com/spf13/pflag` v1.0.9 - flag parsing (via cobra).
- `github.com/inconshreveable/mousetrap` v1.1.0 - cobra Windows helper.
- `github.com/go-ole/go-ole` v1.2.6, `github.com/yusufpapurcu/wmi` v1.2.4 - ghw Windows backends (not exercised on the Fedora target).
- `golang.org/x/sys` v0.25.0 - low-level syscalls (via ghw).
- `gopkg.in/yaml.v3` v3.0.1, `howett.net/plist` - ghw transitive parsers.

> Deliberately NOT vendored: the full `containers/podman/v5` bindings module. Podman is driven via the rootless `podman` CLI (fixed-arg `exec.Command`) and systemd via `systemctl --user`, keeping the single-static-binary goal intact.

## Configuration

**CLI config (the `villa` control plane):**
- TOML file at `$XDG_CONFIG_HOME/villa/config.toml` (resolved via `os.UserConfigDir`). Defined by `VillaConfig` in `internal/config/villaconfig.go`.
- Fields: `model`, `quant`, `ctx`, `backend` (default `vulkan`; `rocm` opt-in), `catalog_path`, `dashboard_addr` (default `127.0.0.1`, loopback-only by construction), `dashboard_port` (default `8888`), `chat_port` (default `3000`).
- Read-only by default: `LoadVilla` returns typed defaults when the file is absent; `SaveVilla` (invoked by `recommend --save` / model swap) writes strictly under the XDG dir with mode `0600`, dir `0700`, and a path-traversal guard. Self-heals zeroed dashboard/chat fields on load (never widens the bind off loopback).

**Embedded assets (`go:embed`):**
- `internal/catalog/seed.json` - the seed model catalog (`//go:embed seed.json` in `internal/catalog/load.go`). Catalog has a schema version window; an external override path may be supplied via `catalog_path`.
- `internal/preflight/rocm-policy.json` - ROCm pin policy: image-tag allow/deny, kernel floor, firmware floor/deny, required `HSA_OVERRIDE_GFX_VERSION` (`//go:embed rocm-policy.json` in `internal/preflight/floors.go`).
- `internal/orchestrate/quadlet/*.tmpl` - Quadlet unit `text/template`s (`//go:embed quadlet/*.tmpl` in `internal/orchestrate/render.go`): `container.tmpl`, `network.tmpl`, `volume.tmpl`, `openwebui.container.tmpl`, `openwebui.volume.tmpl`.
- `internal/dashboard/assets/` - embedded dashboard UI (`//go:embed all:assets` in `internal/dashboard/embed.go`); `dashboard.html` is parsed as an `html/template` shell (chat-link port injected), css/js served verbatim.

**Build:**
- `Makefile` targets: `help`, `run`, `build` (-> `./villa`), `test`, `vet`, `fmt`, `lint`, `check` (vet+test), `tidy`, `clean`.
- `.golangci.yml` - linter config (used by `make lint`).

## Platform Requirements

**Development:**
- Go 1.26.2 toolchain.
- For end-to-end runtime testing: a Fedora host with rootless Podman, `systemctl --user`, and the AMD GPU stack (`/dev/dri`, optionally `/dev/kfd` for ROCm). Host probe tools used when present: `vulkaninfo`, `rocminfo`, `rpm`, `setsebool`, `loginctl`, `journalctl`.

**Production / deployment target (v1.1):**
- Fedora Workstation 44+ on AMD Strix Halo (gfx1151), kernel >= 6.18.4, linux-firmware >= 20260110 (firmware 20251125 explicitly denied for ROCm).
- Rootless Podman v5 with the user socket/manager; user lingering enabled (`loginctl enable-linger`) so Quadlet services survive logout/reboot.
- Strictly local; no telemetry from first-party components.

## Container Images Standardized On

(Digest-pinned in source; tag is silently rebuilt, digest is not.)

| Purpose | Image | Source file |
|---------|-------|-------------|
| Inference (Vulkan RADV, v1 default) | `docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555…ac7aad` | `internal/inference/backend_vulkan.go` |
| Inference (ROCm 7.2.4, opt-in/perf) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1…531a89` | `internal/inference/backend_rocm.go` |
| Chat UI (Open WebUI) | `ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a…a9184e` | `internal/orchestrate/openwebui.go` |

Pin policy explicitly denies `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm7-nightlies` (64 GB allocation-cap bug), enforced in `internal/preflight` and `internal/detect/gpu_amd.go`.

---

*Stack analysis: 2026-06-07*
