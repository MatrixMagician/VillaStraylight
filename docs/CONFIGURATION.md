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
> root. It belongs to the superseded reference scaffold (the `internal/llm` + `web/`
> remnants) and is **not** read by the `villa` CLI. Ignore it for `villa` configuration.

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
| `backend` | string | `vulkan` | Inference backend: `vulkan` (Vulkan RADV, default) or `rocm` (ROCm 7.2.4, opt-in). `config set backend=` only accepts `vulkan`; switch to `rocm` with the transactional `villa backend set rocm` command (see [Backend selection](#backend-selection)). |
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

> Note: `config set backend=` only accepts `vulkan`. Switching to ROCm is a
> stateful cutover (re-fit, ROCm preflight, regenerate, restart, prove, rollback),
> so it is driven by `villa backend set rocm` rather than a plain config write —
> see [Backend selection](#backend-selection).

To inspect the active backend and its resolved container image:

```bash
villa backend show          # active backend + resolved image tag
villa backend show --json   # { "backend": "...", "image": "..." }
```

## Required vs optional settings

Nothing in `config.toml` is required for `villa` to **start** — an absent file
yields typed defaults, and the read-only commands (`detect`, `recommend`,
`config show`) run with no config at all. Requirements only apply at the point a
setting is *used*:

- **`model` / `quant` / `ctx`** — required before you can install or run inference.
  `villa recommend --save` populates them from the host's memory envelope; lifecycle
  commands need a resolved model to render the inference unit.
- **`backend`** — defaults to `vulkan`. Valid persisted values are `vulkan` and
  `rocm`; the inference resolver (`internal/inference/backend.go` `BackendFor`)
  **fails closed** on any other value rather than silently coercing it to a default.
  The plain `config set backend=` writer is intentionally restricted to `vulkan`:

  ```
  config set: unsupported backend "rocm" — only "vulkan" is supported in v1
  ```

  Switching to ROCm is not a plain key write — it is the transactional cutover
  `villa backend set rocm`, which re-fits the preserved model, runs the ROCm
  preflight, regenerates only the inference unit, restarts, proves the cutover, and
  rolls back on any failure. The cutover is the only writer that persists
  `backend = "rocm"`.
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
| `backend` | `vulkan` | `defaultConfig()` (Vulkan RADV default; `rocm` is the opt-in alternative) |
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
(`<models-dir>:/models:ro,z`). The container-internal server binds `0.0.0.0:8080`,
but only the loopback host publish `127.0.0.1:8080:8080` is reachable from the host.

**Backend-specific image, devices, and env.** The image, device passthrough, and
env are the only differences between the two backends; both are owned exclusively by
the backend seam (`internal/inference/backend_vulkan.go` / `backend_rocm.go`).

| Backend | Image (digest-pinned) | Devices | Extra env |
|---------|-----------------------|---------|-----------|
| `vulkan` (default) | `docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e5…` | `/dev/dri` | _(none)_ |
| `rocm` (opt-in) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150…` | `/dev/kfd` **and** `/dev/dri` | `HSA_OVERRIDE_GFX_VERSION=11.5.1` then `ROCBLAS_USE_HIPBLASLT=1` (order preserved) |

The two ROCm env vars are required for ROCm on gfx1151: `HSA_OVERRIDE_GFX_VERSION=11.5.1`
makes the HIP runtime target RDNA 3.5, and `ROCBLAS_USE_HIPBLASLT=1` enables the
hipBLASLt path (the long-context throughput win). Both backends share the same
mandatory llama-server flags, the loopback host publish, the read-only model bind,
and `--group-add keep-groups` (which is what grants the rootless user's render/video
groups access to the GPU devices — never combine it with another `--group-add`).
The ROCm nightly tag is **never** used (it carries the 64 GB allocation-cap bug);
the denied tag is enforced by policy — see [ROCm bring-up policy](#rocm-bring-up-policy).

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

The `backend` key selects the GPU backend the inference unit renders against. Two
values are honored by the inference resolver (`BackendFor`):

- **`vulkan`** (Vulkan RADV) — the default. Stable and compatible across model
  sizes; the value `config set` and the empty/absent config resolve to.
- **`rocm`** (ROCm 7.2.4 / HIP) — the opt-in performance backend. It maps to a
  different digest-pinned image, adds the `/dev/kfd` device, and sets the ordered
  `HSA_OVERRIDE_GFX_VERSION` / `ROCBLAS_USE_HIPBLASLT` env (see
  [Managed container environment](#managed-container-environment)).

Switching backend is a stateful operation, not a plain config edit:

```bash
villa backend show            # inspect the active backend + image
villa backend set rocm        # transactional cutover to ROCm
villa backend set rocm --dry-run   # preview target/fit/preflight, mutate nothing
villa backend set vulkan      # switch back
```

`villa backend set <backend>` re-checks the **preserved** model against the target
memory envelope (refuse-with-remediation if it no longer fits), runs the ROCm
preflight when the target is `rocm`, captures the prior unit verbatim, persists
`config.toml` and regenerates **only** the inference unit, restarts it, and **proves**
the cutover with a real generation probe plus a GPU-residency check within a bounded
timeout. Any mutate error or a non-passing proof rolls the switch back verbatim — a
failed switch is a no-op to the running stack. `--dry-run` previews the target, the
fit verdict, and the preflight without writing, regenerating, or restarting anything.

### ROCm bring-up policy

The ROCm version floors, denylists, and required runtime override live as **data**
in `internal/preflight/rocm-policy.json`, embedded into the binary at build time
(so a malformed policy is a build-time error, never an attacker-controlled runtime
parse). Both the `villa backend set rocm` cutover and the standalone
`villa preflight --backend rocm` gate read this policy. A floor or denylist entry is
corrected in this one file without reshaping any check.

| Key | Value (current) | Meaning |
|-----|-----------------|---------|
| `kernelFloor` | `6.18.4` | Minimum kernel with the gfx1151 stability fix; below it, ROCm bring-up is **refused**. |
| `kernelTested` | `6.18.9` | Validated kernel baseline (named in remediation text). |
| `mesaFloor` | `25.0.0` | Minimum Mesa/RADV version. Carried for parity; **not yet wired** to a check. |
| `firmwareFloor` | `20260110` | Minimum linux-firmware date stamp; below it (but not denied) is a WARN advisory. |
| `firmwareDeny` | `["20251125"]` | linux-firmware builds documented to break ROCm on Strix Halo; a match is a hard **refusal**. |
| `imageDeny` | `["rocm7-nightlies"]` | ROCm image tags that reintroduce the 64 GB allocation cap; a requested image matching one is **refused**. |
| `requiredHSAOverride` | `11.5.1` | The `HSA_OVERRIDE_GFX_VERSION` value ROCm needs on gfx1151; a wrong/unset known value is **refused**. |

The gate is biased against over-blocking: a signal only **fails** (refuses) on a
positively-detected known-bad fact. Anything it cannot evaluate (a host fact that is
Unknown, or a probe not run off-hardware) degrades to a WARN, never a false refusal.
Of these signals, the linux-firmware date is probed on-host (from `rpm`) for the
ROCm-readiness sub-tree of `villa detect`, while the running `HSA_OVERRIDE_GFX_VERSION`
env is not read from the host environment — the cutover sets it inside the container
rather than depending on the user's shell. <!-- VERIFY: kernel version, linux-firmware date, and gfx1151 device id reflect the actual host; these host facts are probed at runtime and are not discoverable from the repository alone -->

The `mesaFloor`/`firmwareFloor`/`firmwareDeny`/`kernelFloor`/`kernelTested` values
are also the source for the version-floor data the non-ROCm host preflight uses
(`Floors()`), so the two surfaces never disagree.
