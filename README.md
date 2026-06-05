<!-- generated-by: gsd-doc-writer -->
# VillaStraylight

A single Go CLI (`villa`) that stands up a private, local AI workspace on your own hardware — auto-detecting an AMD Strix Halo (gfx1151) Fedora host, recommending a memory-fitting model/quant/context, generating rootless Podman Quadlet units, and orchestrating llama.cpp (Vulkan) inference plus an Open WebUI chat front-end behind a loopback-only control dashboard. Strictly local, zero telemetry.

![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)

VillaStraylight is for privacy-conscious power users who want a ChatGPT/Claude-class experience that runs entirely on their own machine, with no data leaving the box. `villa` is the **control plane only** — the AI services (llama.cpp `llama-server`, Open WebUI) are integrated OSS containers, not rebuilt.

> Status: v1.0 shipped (tag `v1.0`). v1.1 (opt-in ROCm backend) is in progress.

## Requirements

- **Go 1.26+** — required to build the `villa` binary (see `go.mod`).
- **Fedora Workstation 44+** on **AMD Strix Halo (gfx1151)** — the only supported host platform for v1. The architecture leaves room for a future macOS/Apple-Silicon backend, but it is not yet implemented.
- **Podman v5 (rootless)** with the user socket enabled (`systemctl --user enable --now podman.socket`) — `villa` drives the AI stack through rootless Podman Quadlet/systemd units, not Docker.
- A **Vulkan RADV** capable GPU stack (Mesa) for the default inference backend.

`villa preflight` checks these host requirements (Vulkan ICD + iGPU enumeration, Podman rootless readiness, SELinux/linger state, and disk/memory floors) and tells you what is missing before you install anything.

## Installation

Build the static `villa` binary from source:

```bash
git clone https://github.com/MatrixMagician/VillaStraylight.git
cd VillaStraylight
make build       # builds ./villa
```

This produces a single static binary at `./villa`. Move it onto your `PATH` if you like:

```bash
install -m 0755 ./villa ~/.local/bin/villa
```

## Quick start

The shortest path from a clean host to a running local AI workspace:

```bash
# 1. Check the host is ready (read-only; reports pass / warn / block).
./villa preflight

# 2. See what model/quant/context fits this machine's memory envelope.
./villa recommend

# 3. Detect, recommend, gate, pull the model, generate units, and bring the stack up.
./villa install
```

`villa install` runs the full managed bring-up: detect the host, recommend a fitting model, gate on a safe host (offering privileged host-prep with per-step consent), download the recommended GGUF model, persist the selection to `config.toml`, render rootless Podman Quadlet units, start inference and Open WebUI, and poll until the inference endpoint is healthy. Re-running with unchanged config is a true no-op. Use `villa install --dry-run` to print the rendered units without writing, pulling, or starting anything.

After install, open the chat UI (Open WebUI, published on the configured chat port — default `3000`) and the read-only control dashboard:

```bash
./villa dashboard        # serves on 127.0.0.1:8888 (loopback only)
```

## Usage

`villa` is a Cobra-based CLI. Every subcommand accepts the global flags `--json` (structured output), `-v`/`--verbose` (per-value provenance), and `--force` (override blocking preflight checks, auditable).

**Inspect the host and pick a model:**

```bash
villa detect                          # print a hardware profile (CPU/arch, iGPU,
                                      # Vulkan/ROCm availability, RAM, usable GTT envelope)
villa recommend --alternatives        # show the fit math and other fitting picks
villa recommend --save                # persist the pick to ~/.config/villa/config.toml
```

**Validate inference before committing to a full install:**

```bash
villa inference run <name>            # run a model and assert GPU offload + a chat completion
villa inference validate <name>       # full end-to-end: offload proof + chat + context ceiling probe
```

**Manage models:**

```bash
villa model list                      # list catalog models and the currently loaded one
villa model pull <name>               # download and verify a GGUF model into the local models dir
villa model swap <name>               # fit-guard, auto-pull, persist config, restart inference
```

**Run the stack lifecycle:**

```bash
villa up [service]                    # reconcile config into units and start (whole stack or one service)
villa status                          # aggregated health: unit + container + /health + GPU-offload proof
villa logs [service]                  # show (and optionally follow) journald logs
villa restart [service]               # re-render units from config and restart
villa down [service]                  # stop without removing units
```

**Configuration and teardown:**

```bash
villa config show                     # print the effective config.toml
villa config set model=<id>           # set a key (model, quant, ctx, backend, catalog_path); applies on next up/restart
villa uninstall                       # tear down units, non-model volumes, and linger — keeps config.toml
```

## Configuration

`villa` reads a single TOML config at `$XDG_CONFIG_HOME/villa/config.toml` (typically `~/.config/villa/config.toml`), written with `0600` permissions. When the file is absent, `villa` uses typed defaults — it is read-only by default and only writes config via `villa recommend --save`, `villa config set`, or `villa install`.

Key fields (`internal/config/villaconfig.go`):

| Key | Default | Description |
|-----|---------|-------------|
| `model` | (from `recommend`) | Chosen catalog model id. |
| `quant` | (from `recommend`) | Chosen quantization (e.g. `UD-Q4_K_M`). |
| `ctx` | (from `recommend`) | Context length in tokens. |
| `backend` | `vulkan` | Inference backend (Vulkan RADV by default for gfx1151). |
| `catalog_path` | (embedded) | Optional path to an external catalog JSON override. |
| `dashboard_addr` | `127.0.0.1` | Loopback-only bind address for the control dashboard. Never widened to a routable interface. |
| `dashboard_port` | `8888` | Host port the control dashboard listens on. |
| `chat_port` | `3000` | Host port Open WebUI is published on (the dashboard's chat link target). |

Inspect or change config with `villa config show` and `villa config set key=value`.

## Development

Common tasks are wired into the `Makefile`:

```bash
make run        # run the villa CLI via `go run ./cmd/villa`
make build      # build ./villa
make test       # go test ./...
make vet        # go vet ./...
make fmt        # gofmt -w .
make lint       # golangci-lint if installed, else go vet
make check      # vet + test
make tidy       # go mod tidy
make clean      # remove build artifacts
```

The CLI entry point is `cmd/villa/main.go`; the control-plane libraries live under `internal/` (`detect`, `recommend`, `catalog`, `preflight`, `download`, `inference`, `orchestrate`, `modelswap`, `status`, `dashboard`, `metrics`, `config`).

> Note: an earlier exploratory scaffold (`cmd/villastraylight`, `internal/{llm,server}`, `web/`, and `.env.example`) remains in the tree as a reference-only parts bin. It is superseded by the `villa` control plane and is not the current architecture.

## License

MIT — see [LICENSE](LICENSE).
