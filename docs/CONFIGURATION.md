<!-- generated-by: gsd-doc-writer -->
# Configuration

`villa` is **config-as-source-of-truth**: a single TOML file, `config.toml`, is the
authoritative input that lifecycle commands (`villa install`, `villa up`,
`villa restart`) render Podman Quadlet units from. The control plane never edits
units by hand — it regenerates them from `config.toml` and reconciles the result.

The configuration surface has three layers:

1. **`config.toml`** — the persisted selection (model, quant, context, backend,
   ports, dashboard bind). This is the only file you edit.
2. **Global CLI flags** — runtime-only switches (`--json`, `--verbose`, `--force`,
   `--catalog`) that do not persist.
3. **Generated/managed container env** — the llama-server runtime flags and the
   Open WebUI environment block. These are **derived constants**, not user
   settings; they are documented here so you know what the rendered units contain,
   but you change them by changing `config.toml` (or upgrading `villa`), not by
   editing the units.

> Note: a legacy `.env.example` (with `VS_*` variables) exists at the repository
> root. It belongs to the superseded reference scaffold (`cmd/villastraylight`)
> and is **not** read by the `villa` CLI. Ignore it for `villa` configuration.

## Environment variables

The first-party `villa` CLI is **not** configured through environment variables —
its settings live in `config.toml`. Only a small number of standard XDG base-directory
variables influence where `villa` reads and writes files.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `XDG_CONFIG_HOME` | Optional | `~/.config` | Base dir for the config file (`$XDG_CONFIG_HOME/villa/config.toml`) and the generated Quadlet units (`$XDG_CONFIG_HOME/containers/systemd/`). Resolved via Go's `os.UserConfigDir`. |
| `XDG_DATA_HOME` | Optional | `~/.local/share` | Base dir for downloaded model weights (`$XDG_DATA_HOME/villa/models`). When unset, falls back to `~/.local/share/villa/models`, and if the home dir cannot be resolved, `/var/tmp/villa/models`. |

The Open WebUI **container** sets its own environment block (telemetry kill-set,
OpenAI base URL, auth) — see [Managed container environment](#managed-container-environment).
Those values are emitted into the generated Quadlet unit by `villa`; they are not
read from your shell.

## Config file format

`villa` stores its configuration as TOML at:

```
$XDG_CONFIG_HOME/villa/config.toml      (default: ~/.config/villa/config.toml)
```

The file is **read-only by default**: when it is absent, every command runs against
typed defaults. It is created/written only by `villa recommend --save` and edited
only by `villa config set`. Both writers go through a single traversal-guarded
writer that refuses to write outside the `villa` config dir and sets file mode
`0600` (directory `0700`).

A minimal `config.toml` looks like this:

```toml
model = "qwen3-30b-a3b"
quant = "UD-Q4_K_M"
ctx = 131072
backend = "vulkan"
catalog_path = ""
dashboard_addr = "127.0.0.1"
dashboard_port = 8888
chat_port = 3000
```

### Keys

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `model` | string | _(empty until recommended/set)_ | The chosen catalog model id. Resolved through the catalog, never treated as a filesystem path. |
| `quant` | string | _(empty until recommended/set)_ | The chosen quantization label (e.g. `UD-Q4_K_M`). |
| `ctx` | int | _(empty/0 until recommended/set)_ | Context length in tokens. Rendered into the llama-server `-c` flag. |
| `backend` | string | `vulkan` | Inference backend. **`vulkan` is the only value accepted in v1**; any other value is rejected by `config set` (see [Backend selection](#backend-selection)). |
| `catalog_path` | string | _(empty → embedded seed catalog)_ | Optional path to an external catalog JSON. Empty means "use the embedded seed catalog". |
| `dashboard_addr` | string | `127.0.0.1` | Loopback bind address for the control dashboard. **Only loopback values are permitted** (see [Required vs optional settings](#required-vs-optional-settings)). |
| `dashboard_port` | int | `8888` | Host port the control dashboard listens on. |
| `chat_port` | int | `3000` | Host port Open WebUI is published on; also the target of the dashboard's "chat" link. |

### Inspecting and editing the config

Two commands read and write `config.toml` safely:

```bash
# Print the effective config (typed defaults when the file is absent)
villa config show
villa config show --json     # stable lowercase JSON: model, quant, ctx, backend, catalog_path

# Set a single key (validated, then persisted via the 0600 writer)
villa config set ctx=32768
villa config set model=qwen3-30b-a3b
```

`config set` accepts only the keys `model`, `quant`, `ctx`, `backend`,
`catalog_path`. An unknown key, a non-positive `ctx`, or an unsupported `backend`
value is rejected with a clear error and **nothing is written**. After a successful
`set`, `villa` reminds you that the change applies on the next
`villa up` / `villa restart` (reconcile).

> Note: `config set` does not expose `dashboard_addr`, `dashboard_port`, or
> `chat_port`. Those carry their loopback/port defaults and are validated on load;
> to change them, edit `config.toml` directly.

## Required vs optional settings

Nothing in `config.toml` is required for `villa` to **start** — an absent file
yields typed defaults, and the read-only commands (`detect`, `recommend`,
`config show`) run with no config at all. Requirements only apply at the point a
setting is *used*:

- **`model` / `quant` / `ctx`** — required before you can install or run inference.
  `villa recommend --save` populates them from the host's memory envelope; lifecycle
  commands need a resolved model to render the inference unit.
- **`backend`** — defaults to `vulkan`. `config set backend=` **refuses** any value
  other than `vulkan`:

  ```
  config set: unsupported backend "rocm" — only "vulkan" is supported in v1
  ```

  This is deliberate: only backends the render path actually honors may be
  persisted, so the setting can never silently no-op.
- **`dashboard_addr`** — must denote loopback. The dashboard server **refuses** to
  start on a non-loopback address; only `127.0.0.1`, `::1`, `localhost`, or empty
  (treated as `127.0.0.1`) are allowed. A tampered config cannot make the dashboard
  bind all interfaces — this is enforced by construction, not just by the default.
- **`dashboard_port` / `chat_port`** — a value of `0` is treated as "unset" and
  self-healed back to the default (`8888` / `3000`) on the next load, because port
  `0` is never a valid intended value for a long-running service.

## Defaults

Defaults are defined in a single place in the source (`internal/config/villaconfig.go`,
`defaultConfig()`), so they cannot drift between writers and readers.

| Setting | Default | Where it comes from |
|---------|---------|---------------------|
| `backend` | `vulkan` | `defaultConfig()` (Vulkan RADV is the only v1 backend) |
| `dashboard_addr` | `127.0.0.1` | `defaultConfig()` (loopback-only) |
| `dashboard_port` | `8888` | `defaultConfig()` |
| `chat_port` | `3000` | `defaultConfig()` |
| `catalog_path` | _(empty)_ → embedded seed catalog | `internal/catalog` falls back to the compiled-in `seed.json` |
| Models directory | `$XDG_DATA_HOME/villa/models` → `~/.local/share/villa/models` | `cmd/villa/model.go` `modelsDir()` |
| Config file path | `$XDG_CONFIG_HOME/villa/config.toml` → `~/.config/villa/config.toml` | `internal/config` `Path()` |
| Quadlet units directory | `$XDG_CONFIG_HOME/containers/systemd/` → `~/.config/containers/systemd/` | `cmd/villa/install.go` `quadletUnitDir()` |

`model`, `quant`, and `ctx` have **no static default** — they are zero/empty until
`villa recommend --save` (or `villa config set`) populates them from the detected
hardware.

### Catalog (model list) configuration

The list of selectable models comes from a **catalog**. By default `villa` uses an
embedded seed catalog compiled into the binary (`internal/catalog/seed.json`,
`schema_version` 2). You can point `villa recommend` at an external catalog:

```bash
villa recommend --catalog /path/to/catalog.json --save
```

When `--save` is used, the resolved `catalog_path` is persisted so future runs
reuse it without re-passing the flag. The external file is validated: a bad path,
a symlink, a directory, a file over 1 MiB, malformed JSON, or a mismatched
`schema_version` causes `villa` to emit a warning and **fall back to the embedded
seed** rather than failing.

### Managed container environment

The generated Quadlet units embed runtime configuration that is **not** exposed as
user settings. It is recorded here for transparency.

**Inference (llama-server) runtime flags** — fixed for Strix Halo stability and
sourced from the backend seam (`internal/inference/backend_vulkan.go`):

| Flag | Purpose |
|------|---------|
| `-ngl 999` | Offload all layers to the iGPU (free on unified memory). |
| `-fa 1` | Flash attention on (stability + KV-cache memory). |
| `--no-mmap` | Keep weights resident in unified memory (no mmap). |
| `-c <ctx>` | Context length, from `config.toml` `ctx`. |
| `--host 0.0.0.0` / `--port 8080` | Container-internal bind only; the host side is published loopback-only at `127.0.0.1:8080`. |
| `-lv 4` | Raises llama-server log verbosity enough for the offload-residency assertion. |
| `--metrics` | Exposes the Prometheus `/metrics` endpoint for the dashboard perf panel. |

The inference container also receives `--device /dev/dri`, `--group-add keep-groups`,
`--security-opt seccomp=unconfined`, and a read-only model bind mount
(`<models-dir>:/models:ro,z`). The container image is a digest-pinned Vulkan RADV
image (`docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:...`).

**Open WebUI environment block** — emitted as ordered `Environment=` entries in the
generated unit (`internal/orchestrate/openwebui.go`). The order is fixed and
load-bearing:

| Variable | Value | Purpose |
|----------|-------|---------|
| `OPENAI_API_BASE_URL` | `http://villa-llama:8080/v1` | Reaches inference over the `villa` network by container DNS, at its internal port 8080. |
| `ENABLE_OPENAI_API` | `True` | Use the OpenAI-compatible llama-server endpoint. |
| `ENABLE_OLLAMA_API` | `False` | Ollama is not the engine. |
| `OPENAI_API_KEY` | `sk-no-key-required` | Required-but-ignored placeholder; llama.cpp performs no auth. Not a secret. |
| `ANONYMIZED_TELEMETRY` | `False` | Telemetry kill-set. |
| `DO_NOT_TRACK` | `True` | Telemetry kill-set. |
| `SCARF_NO_ANALYTICS` | `True` | Telemetry kill-set. |
| `OFFLINE_MODE` | `True` | Telemetry kill-set / offline. |
| `ENABLE_VERSION_UPDATE_CHECK` | `False` | No update phone-home. |
| `HF_HUB_OFFLINE` | `1` | No Hugging Face network access from the UI. |
| `WEBUI_AUTH` | `True` | Local admin account, persisted in the durable volume. |

Open WebUI is published loopback-only at `127.0.0.1:3000` (container-internal port
`8080`) and stores data in a named volume mounted at `/app/backend/data`. The image
is digest-pinned (`ghcr.io/open-webui/open-webui:main@sha256:...`).

## Per-environment overrides

`villa` does **not** use a `NODE_ENV`-style environment switch or per-environment
config files (`.env.development`, etc.). It targets a single local host. The ways
configuration varies per machine are:

- **XDG base directories** — Setting `XDG_CONFIG_HOME` / `XDG_DATA_HOME` relocates
  the config file, the generated Quadlet units, and the models directory. This is
  the primary mechanism for running an isolated `villa` instance (for example, in a
  test harness): every host-touching path is derived from these, and the test code
  paths (`LoadVillaFrom` / `SaveVillaTo`, the injectable `configDeps` and lifecycle
  seams) point them at a temporary directory.
- **Per-host recommendation** — `villa recommend` reads the detected hardware
  (memory envelope, GPU) and produces a model/quant/context that fits *that* host;
  `--save` writes it to `config.toml`. The same binary therefore produces a
  different `config.toml` on a 64 GB vs a 128 GB machine.
- **External catalog override** — `catalog_path` (or `--catalog`) lets a host use a
  curated model list different from the embedded seed.

### Backend selection

The `backend` key is the forward-looking seam for alternative GPU backends. In v1:

- **`vulkan`** (Vulkan RADV) is the only accepted value, and `config set` rejects
  anything else.

ROCm is admitted by the backend interface but **not wired** in v1, so it cannot be
selected. <!-- VERIFY: the v1.1 backend config field (vulkan default | rocm opt-in) selecting image/env/devices is described in project planning but is not present in the current repository source -->

Per the project plan, a future v1.1 adds a backend config field that selects
`vulkan` (default) or `rocm` (opt-in), each mapping to a different image, env, and
device passthrough. Until that lands, treat `backend` as effectively fixed to
`vulkan`. <!-- VERIFY: v1.1 ROCm backend image, env, and device-mapping details are not yet implemented in this repository -->
