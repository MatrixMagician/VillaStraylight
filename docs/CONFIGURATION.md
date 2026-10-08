# Configuration

`villa` is **config-as-source-of-truth**: a single TOML file, `config.toml`, is the
authoritative input that lifecycle commands (`villa install`, `villa up`,
`villa restart`) render Podman Quadlet units from. The control plane never edits
units by hand: it regenerates them from `config.toml` and reconciles the result.

The configuration surface has three layers:

1. **`config.toml`**: the persisted selection (model, quant, context, backend,
   ports, dashboard bind). This is the only file you edit.
2. **Global CLI flags**: runtime-only switches (`--json`, `--verbose`, `--force`,
   `--catalog`) that do not persist.
3. **Generated/managed container env**: the llama-server runtime flags and the
   Open WebUI environment block. These are **derived constants**, not user
   settings; they are documented here so you know what the rendered units contain,
   but you change them by changing `config.toml` (or upgrading `villa`), not by
   editing the units.

## Environment variables

The first-party `villa` CLI is **not** configured through environment variables;
its settings live in `config.toml`. Only a small number of standard XDG base-directory
variables influence where `villa` reads and writes files.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `XDG_CONFIG_HOME` | Optional | `~/.config` | Base dir for the config file (`$XDG_CONFIG_HOME/villa/config.toml`) and the generated Quadlet units (`$XDG_CONFIG_HOME/containers/systemd/`). Resolved via Go's `os.UserConfigDir`. |
| `XDG_DATA_HOME` | Optional | `~/.local/share` | Base dir for downloaded model weights (`$XDG_DATA_HOME/villa/models`). When unset, falls back to `~/.local/share/villa/models`, and if the home dir cannot be resolved, `/var/tmp/villa/models`. |

The Open WebUI **container** sets its own environment block (telemetry kill-set,
OpenAI base URL, auth); see [Managed container environment](#managed-container-environment).
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
model = "qwen3.6-35b-a3b"
quant = "UD-Q4_K_M"
ctx = 131072
backend = "rocm"
catalog_path = ""
dashboard_port = 8888
chat_port = 3000
```

### Keys

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `model` | string | _(empty until recommended/set)_ | The chosen catalog model id. Resolved through the catalog, never treated as a filesystem path. |
| `quant` | string | _(empty until recommended/set)_ | The chosen quantization label (e.g. `UD-Q4_K_M`). |
| `ctx` | int | _(empty/0 until recommended/set)_ | Context length in tokens. Rendered into the llama-server `-c` flag; in tools mode it is raised to the served entry's `agent_ctx` when that is larger. `villa model swap` sizes its target at this ctx and keeps it when the target fits; when the target is over the envelope at it, the swap writes the target's `default_ctx` instead and prints `ctx reset to …`, and it refuses only when the target does not fit at its default either (ADR-0024). |
| `backend` | string | `rocm` | Inference backend: `rocm` (ROCm 10.0, **default**), `rocm-10.0` (the same image by name), `rocm-7.2.4` (the former default), `rocm-6.4.4`, `rocm-6.4.4-rocwmma`, or `vulkan` (Vulkan RADV, the fallback). `config set backend=` only accepts `vulkan`; every ROCm target must go through the transactional `villa backend set` command (see [Backend selection](#backend-selection)). |
| `catalog_path` | string | _(empty → embedded seed catalog)_ | Optional path to an external catalog JSON. Empty means "use the embedded seed catalog". |
| `dashboard_port` | int | `8888` | Host port the control dashboard listens on. |
| `chat_port` | int | `3000` | Host port Open WebUI is published on; also the target of the dashboard's "chat" link. |
| `speculation` | string | _(absent → off)_ | Speculative-decoding mode of the inference unit: `off`, `ngram` for llama-server's `ngram-mod`, or `draft` for the entry's draft sidecar. An absent key renders speculation off, which is what every install predating the key carries. `config set` does not accept it: turning it on is a stateful cutover driven by `villa speculation set` (see [Speculation](#speculation)). A hand-edited value outside this vocabulary is refused on load. |
| `vision` | bool | _(absent → false)_ | Whether the inference unit is started with the model's vision projector, so an image attached in Open WebUI is answered. True only when a `villa recommend --save`, a `villa install` or a `villa model swap` resolved a projector that fits the envelope and pulled it — the projector file must be on disk before a unit can reference it, which is why this is persisted rather than read back from the catalog. `model swap` writes it for the target, judged at the ctx the swap serves it at (ADR-0024), so swapping to a text-only entry turns it off and swapping to one whose projector fits turns it on, and the swap prints the change (ADR-0023). `config set` does not accept the key. An absent key renders no projector flag, which is what every install predating the key carries. |
| `reranker` | bool | _(absent → false)_ | The memory stack's reranker gate, read as `memory_enabled && reranker` (ADR-0028). Written true by `villa install` when memory is on, after the reranker's weights are staged; omitted when memory is off. With it on, the render adds the `villa-rerank` unit, Open WebUI's hybrid search and external-reranker env, and a 2 GiB `reranker` reservation. Set it to `false` by hand to opt out until the next install. See [Memory](MEMORY.md#hybrid-search-and-the-reranker). |
| `extractor` | bool | _(absent → false)_ | The memory stack's document-extractor gate, read as `memory_enabled && extractor` (ADR-0033). Written true by `villa install` when memory is on; omitted when memory is off. With it on, the render adds the `villa-extract` unit (Apache Tika with OCR), Open WebUI's `CONTENT_EXTRACTION_ENGINE=tika` env, and a 2 GiB `extractor` reservation, so a scanned PDF uploaded to a knowledge base is read instead of refused. Set it to `false` by hand to opt out until the next install. See [Memory](MEMORY.md#document-extraction). |
| `resident` | array of tables | _(absent)_ | Zero or more `[[resident]]` slots: secondary models held loaded alongside `model`. Absent until `villa model resident add` writes one. See [The resident set](#the-resident-set). |
| `tools_mode` | bool | _(absent → false)_ | Whether the chat unit is served with the model's own chat template, so tool calls parse. Written by `villa tools-mode enter`, `villa tools-mode exit` and `villa install --workspace-agent`, never by `config set`: flipping it restarts the inference unit under a transactional cutover. Coding mode implies it, so `coding_mode = true` answers the gate on without this key. See [The workspace agent](#the-workspace-agent). |
| `workspace_agent` | bool | _(absent → false)_ | The workspace agent's gate. Written only by `villa install --workspace-agent`, which also sets `tools_mode`. `villa-sandbox.network` itself renders unconditionally (like `villa.network`); with the gate on, the render additionally starts a `villa-inferproxy` unit joined to BOTH networks (never the inference unit itself — GHSA-gvp9, ADR-0011), and `villa preflight` runs `PRE-09`. |
| `inference_secret` | string | _(generated on first render)_ | The `LLAMA_API_KEY`/`OPENAI_API_KEY` bearer every rendered llama-server instance (the primary unit, every resident slot, Open WebUI, and villa-inferproxy) requires (GHSA-qxg9, ADR-0011). Generated once via `config.GenerateInferenceSecret` (crypto/rand) and persisted on `villa install`'s first render, or self-healed on the next `villa up`/`restart` for a config.toml that predates it; reused verbatim thereafter, never rotated. It is NEVER rendered into a `.container` unit — every unit references it only by the path of a 0600 `EnvironmentFile=` (`internal/orchestrate.InferenceSecretEnvFilePath`). `config set` does not accept it. |
| `workspace` | array of strings | _(absent)_ | The registered workspace grants: absolute, symlink-resolved folders a task may be run against. Written by `villa workspace add` and `villa workspace remove`; `villa work` refuses any path not on this list. |
| `sandbox_memory` | string | _(absent → `4g`)_ | The `--memory` limit of a task's microVM, in podman's syntax. Edit by hand; read at task launch. The default lives in `internal/orchestrate/sandbox.go`, not in `defaultConfig()`, so an absent key writes nothing to the file. |
| `sandbox_cpus` | int | _(absent → `4`)_ | The `--cpus` limit of a task's microVM. Edit by hand; read at task launch; zero or negative renders the default. |
| `voice_enabled` | bool | _(absent → false)_ | The voice subsystem's gate (ADR-0030): speech-to-text (`villa-stt`, whisper.cpp on Vulkan) plus text-to-speech (`villa-tts`, Kokoro-FastAPI on the CPU), proven together by one round trip. Written by `villa install --voice`, which also pre-stages the whisper model into the models dir. With it on, Open WebUI's voice input and read-aloud point at the two units by container DNS, `villa recommend` reserves the `stt` and `tts` rows off the envelope, and `villa verify voice` speaks a sentence and transcribes it back. Nothing leaves the box: the Kokoro image bakes its weights in and was proven to make no outbound call. |
| `image_enabled` | bool | _(absent → false)_ | The image-generation gate (`subsystem.Image`, ADR-0029). Written by `villa install --image`; nothing on the command line turns it off. With it on, the render appends the `villa-image` sd-server unit, Open WebUI's image generation is wired to it, and the image model's measured footprint is reserved off the envelope before the chat-model fit. See [Image generation](#image-generation). |
| `image_model` | string | _(absent → `z-image-turbo`)_ | The image-table id `villa-image` serves: `z-image-turbo` (Z-Image-Turbo Q8_0) or `z-image-turbo-q4` (Q4_K, about 2.6 GiB less resident). Edit by hand, then `villa install`; `catalog_path` never overrides the image table. Self-healed to the default on load and omitted from the file while `image_enabled` is off. |

### The resident set

`model` names the **primary** model, the one `villa-llama.service` runs. A stack may
also hold **resident** models: extra `llama-server` instances kept loaded at the same
time, each in its own container on its own host loopback port, so switching between
them in the chat UI costs no cold load. Every resident slot is one `[[resident]]` table:

```toml
model = "qwen3.6-35b-a3b"
quant = "UD-Q4_K_M"
ctx = 131072
backend = "rocm"

[[resident]]
model = "qwen3.5-0.8b"
quant = "Q8_0"
ctx = 4096
port = 8081
```

| Field | Type | Description |
|-------|------|-------------|
| `model` | string | The slot's catalog model id. Resolved through the catalog, never treated as a filesystem path. It is also the sole source of the slot's unit name, so two slots may not share one. |
| `quant` | string | The slot's quantization label. Omitted when empty. |
| `ctx` | int | The slot's own context length in tokens (its single llama-server `-c`). Independent of the primary's `ctx`. Omitted when zero. |
| `port` | int | The **host** loopback port this slot publishes on (`127.0.0.1:<port>`). Stated explicitly rather than derived from list position, so removing a middle slot does not renumber, and therefore rewrite and restart, every slot after it. Omitted when zero. |

Each slot renders one extra Quadlet unit, `villa-llama-<slug>.container`, where
`<slug>` is the model id lowercased with every character outside `[a-z0-9-]` folded to
`-`. The chat UI's connection env lists the primary endpoint plus every resident one,
so the resident set is visible in Open WebUI as additional models.

The key is **append-only and optional**: a config with no resident slot carries no
`resident` key at all and renders a byte-identical stack to one that never had the
feature.

#### Managing slots

Edit the resident set through the CLI rather than by hand: the commands are the only
writers that check the set actually fits:

```bash
villa model resident ls              # primary + every slot, with port, unit and state
villa model resident ls --json       # machine-readable, schema-versioned
villa model resident add <model-id>  # fit-guard, allocate a port, auto-pull, start
villa model resident rm <model-id>   # drop the slot, regenerate, stop the orphan
```

`add` sizes the candidate with the same memory-fit math `villa recommend` uses,
asks the admission core whether the whole set still fits the usable envelope, and
**refuses with a remediation** when it does not: nothing is written, downloaded or
started on a refusal. It allocates the lowest free host port at or above `8081`,
skipping the primary's port and every port a slot already claims, so a removal's gap
is reused before the range grows.

`rm` refuses the primary: it is not a resident slot, and changing it is
`villa model swap <name>`.

Both are transactional in the sense the rest of the stack is (ADR-0003): the prior
config and unit files are captured before the first mutation and restored verbatim on
any later failure, and a restore that could not itself complete is reported as
incomplete rather than as a clean rollback.

> A hand-edited `resident` block is untrusted input and fails closed. Two slots
> sharing a host port, a slot claiming the primary's port, two slots slugging to the
> same unit name, or a model id with no usable unit-name characters are each refused
> at render time with an actionable error, never rendered into units that podman
> would start and immediately kill.

### The workspace agent

Five keys drive `villa work`. Each has exactly one writer, and none of them is a
`config set` key.

| Key | Type | Default | Written by | Read by |
|-----|------|---------|------------|---------|
| `tools_mode` | bool | `false` | `villa tools-mode enter` / `exit`, `villa install --workspace-agent` | the inference render (the chat template flag), `villa work`, `villa doctor` (`TMD-01`), `villa status` (`mode: tools`) |
| `workspace_agent` | bool | `false` | `villa install --workspace-agent` | the inference unit's second network line (`villa-sandbox.network` itself always renders), `villa preflight` (`PRE-09`), `villa doctor` (`SBX-01`, `SBX-02`) |
| `workspace` | array of strings | absent | `villa workspace add` / `remove` | `villa work`, the runner, the dashboard's Workspaces panel |
| `sandbox_memory` | string | `4g` | you, by hand | the task launch (`podman run --memory`) |
| `sandbox_cpus` | int | `4` | you, by hand | the task launch (`podman run --cpus`) |

```toml
tools_mode = true
workspace_agent = true
workspace = ["/home/you/Documents/quarterly"]
sandbox_memory = "4g"
sandbox_cpus = 4
```

`tools_mode` is a stateful cutover, the same shape as `speculation`: `villa
tools-mode enter` checks that the context floor tools mode serves still fits the
memory envelope, captures the inference unit, persists the key, regenerates only
that unit, restarts it, and proves the cutover with the residency proof plus one
real read-then-edit tool call; any failure rolls back verbatim. `exit` runs the
same transaction with the residency proof alone. The gate villa answers is
`tools_mode || coding_mode`, so `villa tools-mode show` reports `on` with
`implied by coding mode` on a stack in coding mode, and `exit` does not clear
coding mode.

`workspace_agent` is written by `villa install --workspace-agent`, which also
writes `tools_mode = true`. Nothing on the command line turns it off; edit the
file and re-run `villa install` to drop the inference unit's join to
`villa-sandbox.network`. The network unit itself is never removed by a gate flip
(issue #199): it renders unconditionally, like `villa.network`, and an unjoined
`.network` unit starts no service, so there is nothing to clean up.

`workspace` holds absolute paths after symlink resolution. `villa workspace add`
refuses a relative path, the home directory itself, a path outside it, a path
overlapping villa's own XDG config or data root, a path nested with an existing
grant in either direction, and a path that is missing or not a directory.
Re-adding a granted path is a no-op. `remove` resolves the argument the same way
and touches no file. A hand-edited entry that no longer resolves is not a grant:
`villa work` resolves the argument and looks it up, so a stale entry refuses.

`sandbox_memory` and `sandbox_cpus` are the two tunables a task's microVM takes
from config. The defaults are constants in `internal/orchestrate/sandbox.go`
rather than `defaultConfig()`, so an absent key writes nothing to the file and
renders `4g` and `4`. `4g` was frozen by measurement on 2026-09-10 (spec
§13, item 4): under krun, `villa-recalc` on the reconciliation workbook peaked at
157 MB RSS and completed at 2g, 4g and 8g alike, and a whole task VM (Crush, the
bridge, the audit's reads) peaked at 420 MB. `4g` is eight times that and the
value the prototype ran at; set `sandbox_memory` higher for a larger workbook.

### Image generation

Two keys drive local image generation (ADR-0029). Neither is a `config set` key.

| Key | Type | Default | Written by | Read by |
|-----|------|---------|------------|---------|
| `image_enabled` | bool | `false` | `villa install --image` | the render (the `villa-image` unit and Open WebUI's image env group), the reservation registry (`villa recommend`), `villa status` (the `villa-image.service` row), `villa doctor` (`IMG-DOC-residency` while on, `IMG-DOC-stale` when off but the service is still active), `villa update image` |
| `image_model` | string | `z-image-turbo` | you, by hand | the render, the reservation row, the pre-stage, the offload proof |

```toml
image_enabled = true
image_model = "z-image-turbo"
```

`villa install --image` reserves the image model's measured footprint
(`weight_bytes + compute_bytes` from `internal/catalog/images.json`) before the
chat-model fit, pulls the entry's three weight files (the diffusion model, the
Qwen3-4B text encoder and the FLUX VAE, each verified by size and SHA-256) into
the models dir, renders `villa-image.container`, wires Open WebUI's
`automatic1111` engine to `http://villa-image:1234`, starts the unit after the
web-search stack, and proves offload with one real 512x512 generation before it
reports success: the params placement line in the unit's journal must show
every byte on `VRAM` and none in `RAM`, the GTT floor must clear the footprint,
and `gpu_busy_percent` must be non-zero during the drive. A partial CPU
placement is a FAIL that rolls the install back.

The unit eager-loads, so the footprint is held from the moment the service
starts, which is what the reservation row claims. The model is picked from the
compiled-in image table, not from `seed.json` or a `catalog_path` override, so
changing the entry is a code change carrying a fresh on-hardware measurement.
To turn image generation off safely, do all four steps. Reconcile never
deletes a unit, so after the first two `villa-image.container` is still on disk
with `WantedBy=default.target`, and the next reboot eager-loads its 9 GB again
outside every fit (`villa doctor` reports this as `IMG-DOC-stale`):

```bash
# 1. by hand in ~/.config/villa/config.toml: image_enabled = false
# 2. re-render Open WebUI without the image group
villa up
# 3. stop the unit and drop it from the boot set
systemctl --user disable --now villa-image.service
# 4. remove the unit file reconcile left behind, and let systemd forget it
rm ~/.config/containers/systemd/villa-image.container
systemctl --user daemon-reload
```

sd-server ignores SIGTERM, so step 3 waits podman's 10 s SIGKILL fallback.

### Inspecting and editing the config

Two commands read and write `config.toml` safely:

```bash
# Print the effective config (typed defaults when the file is absent)
villa config show
villa config show --json     # stable lowercase JSON: model, quant, ctx, backend, catalog_path

# Set a single key (validated, then persisted via the 0600 writer)
villa config set ctx=32768
villa config set model=qwen3.6-35b-a3b
```

`config set` accepts only the keys `model`, `quant`, `ctx`, `backend`,
`catalog_path`. The `resident` array is not among them: a slot carries a port and a
memory cost, so it is written only by `villa model resident add` / `rm`, which check
the fit first. Nor is `speculation`, for the same reason `backend` is restricted:
turning it on is a cutover, so it is written by `villa speculation set` (see
[Speculation](#speculation)). The workspace agent's keys are not among them either:
`tools_mode` is a cutover (`villa tools-mode enter` / `exit`), `workspace_agent` is
persisted by `villa install --workspace-agent`, and `workspace` is checked on every
`villa workspace add` (see [The workspace agent](#the-workspace-agent)). An unknown key, a non-positive `ctx`, or an unsupported `backend`
value is rejected with a clear error and **nothing is written**. After a successful
`set`, `villa` reminds you that the change applies on the next
`villa up` / `villa restart` (reconcile).

> Note: `config set` does not expose `dashboard_port` or `chat_port`. Those carry
> their port defaults and are validated on load; to change them, edit `config.toml`
> directly.
>
> The dashboard's bind address and the in-network service addresses and ports are
> **not settings**: they are constants in `internal/config`. Nothing could set
> them (the `set` allowlist never accepted them, and the loader healed any
> hand-edited value straight back), and widening them off loopback or off the
> private container network is a privacy violation rather than a preference. A
> config file still carrying the old keys loads fine; they are ignored.

> Note: `config set backend=` only accepts `vulkan`. That is not a claim about
> which backend is preferred (ROCm is the default); it is about which writes are
> safe as a plain key write. Selecting any ROCm backend is a stateful cutover
> (re-fit, ROCm preflight, regenerate, restart, prove, rollback), so it is driven by
> `villa backend set <name>`; see [Backend selection](#backend-selection).

To inspect the active backend and its resolved container image:

```bash
villa backend show          # active backend + resolved image tag
villa backend show --json   # { "backend": "...", "image": "..." }
```

## Required vs optional settings

Nothing in `config.toml` is required for `villa` to **start**: an absent file
yields typed defaults, and the read-only commands (`detect`, `recommend`,
`config show`) run with no config at all. Requirements only apply at the point a
setting is *used*:

- **`model` / `quant` / `ctx`**, required before you can install or run inference.
  `villa recommend --save` populates them from the host's memory envelope; lifecycle
  commands need a resolved model to render the inference unit.
- **`backend`**, defaults to `rocm`. Valid persisted values are `rocm`,
  `rocm-6.4.4`, `rocm-6.4.4-rocwmma`, and `vulkan`; the inference resolver
  (`internal/inference/backend.go` `BackendFor`) **fails closed** on any other value
  rather than silently coercing it to a default. An absent or empty value resolves to
  the `rocm` default, and is gated by the ROCm preflight exactly as the explicit name
  would be. The plain `config set backend=` writer is intentionally restricted to
  `vulkan` (the one target with no bring-up gate to skip):

  ```text
  config set: refusing to persist backend "rocm" — only "vulkan" may be set here;
  switch to a ROCm backend (the default) with the transactional `villa backend set rocm`
  ```

  Selecting a ROCm backend is the transactional cutover
  `villa backend set <name>`, which re-fits the preserved model, runs the ROCm
  preflight, regenerates only the inference unit, restarts, proves the cutover, and
  rolls back on any failure. The cutover is the only writer that persists a ROCm
  backend name.
- **The dashboard bind address** is the loopback constant `127.0.0.1`, and the
  server additionally **refuses** to start on a non-loopback address. A config can
  no longer express a bind address at all, so it cannot make the dashboard bind
  all interfaces.
- For **`dashboard_port` / `chat_port`**, a value of `0` is treated as "unset" and
  self-healed back to the default (`8888` / `3000`) on the next load, because port
  `0` is never a valid intended value for a long-running service.

## Defaults

Defaults are defined in a single place in the source (`internal/config/villaconfig.go`,
`defaultConfig()`), so they cannot drift between writers and readers.

| Setting | Default | Where it comes from |
|---------|---------|---------------------|
| `backend` | `rocm` | `defaultConfig()` (ROCm 10.0 default, ADR-0022; `vulkan` is the RADV fallback) |
| `dashboard_port` | `8888` | `defaultConfig()` |
| `chat_port` | `3000` | `defaultConfig()` |
| `catalog_path` | _(empty)_ → embedded seed catalog | `internal/catalog` falls back to the compiled-in `seed.json` |
| Models directory | `$XDG_DATA_HOME/villa/models` → `~/.local/share/villa/models` | `cmd/villa/model.go` `modelsDir()` |
| Config file path | `$XDG_CONFIG_HOME/villa/config.toml` → `~/.config/villa/config.toml` | `internal/config` `Path()` |
| Quadlet units directory | `$XDG_CONFIG_HOME/containers/systemd/` → `~/.config/containers/systemd/` | `cmd/villa/install.go` `quadletUnitDir()` |
| `sandbox_memory` | `4g` | `internal/orchestrate/sandbox.go`, applied at task launch when the key is absent |
| `sandbox_cpus` | `4` | `internal/orchestrate/sandbox.go`, applied at task launch when the key is absent or not positive |
| Task records and logs | `$XDG_DATA_HOME/villa/tasks/` → `~/.local/share/villa/tasks/` | `internal/pathsafe` `DataRoot()` + `internal/taskstore` |

`model`, `quant`, and `ctx` have **no static default**: they are zero/empty until
`villa recommend --save` (or `villa config set`) populates them from the detected
hardware.

### Catalog (model list) configuration

The list of selectable models comes from a **catalog**. By default `villa` uses an
embedded seed catalog compiled into the binary (`internal/catalog/seed.json`,
`schema_version` 3). You can point `villa recommend` at an external catalog:

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

**Voice (`voice_enabled = true`).** `villa-stt` runs `whisper-server` from the pinned
ggml-org Vulkan image with `Entrypoint=` set to the server binary (the image's own
entrypoint is a shell that would drop every argument after the first) and
`-m /models/ggml-large-v3-turbo.bin --host 0.0.0.0 --port 8081 --inference-path /v1/audio/transcriptions --convert`:
the inference path puts whisper's one handler on the OpenAI transcription route Open
WebUI calls, and `--convert` lets the image's ffmpeg turn the browser's webm into WAV.
The unit carries only `AddDevice=/dev/dri`, read from the Vulkan backend seam, and
mounts the models volume read-only. `villa-tts` runs the pinned Kokoro-FastAPI CPU
image with `DOWNLOAD_MODEL=false`, `HF_HUB_OFFLINE=1` and `HF_HUB_DISABLE_TELEMETRY=1`,
so its entrypoint never re-runs the weight download. Open WebUI gets
`AUDIO_STT_ENGINE=openai`, `AUDIO_STT_OPENAI_API_BASE_URL=http://villa-stt:8081/v1`,
`AUDIO_STT_MODEL=whisper-1`, `AUDIO_TTS_ENGINE=openai`,
`AUDIO_TTS_OPENAI_API_BASE_URL=http://villa-tts:8880/v1`, `AUDIO_TTS_MODEL=kokoro` and
`AUDIO_TTS_VOICE=af_heart`; both API keys carry the no-auth sentinel, because neither
unit checks one and both are reachable only on `villa.network`.

**Inference (llama-server) runtime flags** are fixed for Strix Halo stability and
sourced from the backend seam (`internal/inference/backend_rocm.go` /
`backend_vulkan.go`):

| Flag | Purpose |
|------|---------|
| `-ngl 999` | Offload all layers to the iGPU (free on unified memory). |
| `-fa 1` | Flash attention on (stability + KV-cache memory). |
| `--load-mode none` | Keep weights resident in unified memory (no mmap). The `rocm` default and `rocm-6.4.4-rocwmma` images predate this flag and render `--no-mmap`, the same setting in its older spelling; `vulkan` and `rocm-6.4.4` render `--load-mode none`. |
| `-c <ctx>` | Context length, from `config.toml` `ctx`. |
| `--host 0.0.0.0` / `--port 8080` | Container-internal bind only; the host side is published loopback-only at `127.0.0.1:8080`. |
| `-lv 4` | Raises llama-server log verbosity enough for the offload-residency assertion. |
| `--metrics` | Exposes the Prometheus `/metrics` endpoint for the dashboard perf panel. |
| `--cache-ram 8192` | Caps the RAM prompt cache at 8 GiB (ADR-0021). Rendered, not configurable: there is no `config.toml` key, and the fit counts exactly this value, once per chat unit, so each resident model adds one. `villa-embed` does not render it. |

The inference container also receives `--device /dev/dri`, `--group-add keep-groups`,
`--security-opt seccomp=unconfined`, and a read-only model bind mount
(`<models-dir>:/models:ro,z`). The container-internal server binds `0.0.0.0:8080`,
but only the loopback host publish `127.0.0.1:8080:8080` is reachable from the host.

**Backend-specific image, devices, and env.** The image, device passthrough, and
env are the only differences between the backends; all are owned exclusively by
the backend seam (`internal/inference/backend_rocm.go` / `backend_vulkan.go`).

| Backend | Image (digest-pinned) | Devices | Extra env |
|---------|-----------------------|---------|-----------|
| `rocm` / `rocm-10.0` (default) | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-10.0@sha256:3893b3…` | `/dev/kfd` **and** `/dev/dri` | `HSA_OVERRIDE_GFX_VERSION=11.5.1` then `ROCBLAS_USE_HIPBLASLT=1` (order preserved) |
| `rocm-7.2.4` | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150…` | `/dev/kfd` **and** `/dev/dri` | same ordered ROCm env |
| `rocm-6.4.4` | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4@sha256:c81f30…` | `/dev/kfd` **and** `/dev/dri` | same ordered ROCm env |
| `rocm-6.4.4-rocwmma` | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4-rocwmma@sha256:9a9712…` | `/dev/kfd` **and** `/dev/dri` | same ordered ROCm env |
| `vulkan` (fallback) | `docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e5…` | `/dev/dri` | _(none)_ |

The two ROCm env vars are required for ROCm on gfx1151: `HSA_OVERRIDE_GFX_VERSION=11.5.1`
makes the HIP runtime target RDNA 3.5, and `ROCBLAS_USE_HIPBLASLT=1` enables the
hipBLASLt path (the long-context throughput win). Both backends share the same
mandatory llama-server flags, the loopback host publish, the read-only model bind,
and `--group-add keep-groups` (which is what grants the rootless user's render/video
groups access to the GPU devices, never combine it with another `--group-add`).
The ROCm nightly tag is **never** used (it carries the 64 GB allocation-cap bug);
the denied tag is enforced by policy; see [ROCm bring-up policy](#rocm-bring-up-policy).

The **Open WebUI environment block** is emitted as ordered `Environment=` entries in the
generated unit (`internal/orchestrate/openwebui.go`). The order is fixed and
load-bearing:

| Variable | Value | Purpose |
|----------|-------|---------|
| `OPENAI_API_BASE_URL` | `http://villa-llama:8080/v1` | Reaches inference over the `villa` network by container DNS, at its internal port 8080. |
| `ENABLE_OPENAI_API` | `True` | Use the OpenAI-compatible llama-server endpoint. |
| `ENABLE_OLLAMA_API` | `False` | Ollama is not the engine. |
| `OPENAI_API_KEY` | the `inference_secret` bearer | Set via the SAME 0600 `EnvironmentFile=` (`internal/orchestrate.InferenceSecretEnvFilePath`) villa-llama itself references — never a literal in this 0644 unit — because llama-server now requires `Authorization: Bearer <inference_secret>` on every route but `/health` (GHSA-qxg9, ADR-0011). With a resident set the value is a literal `Environment=` line instead (the singular EnvironmentFile can't carry OWUI's per-slot connection list), still the same real secret, never the old `sk-no-key-required` sentinel. |
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

With `image_enabled`, ONE further ordered group is appended after the web-search
group, and the trailing `ENABLE_PERSISTENT_CONFIG=False` gate covers it:

| Variable | Value | Purpose |
|----------|-------|---------|
| `ENABLE_IMAGE_GENERATION` | `True` | Turn Open WebUI's image generation on. |
| `IMAGE_GENERATION_ENGINE` | `automatic1111` | The engine whose verify is a real `GET /sdapi/v1/options`, whose model list names the real file stem, and whose generate is `POST /sdapi/v1/txt2img`. |
| `AUTOMATIC1111_BASE_URL` | `http://villa-image:1234` | sd-server over the `villa` network by container DNS, at its own `--listen-port`. |
| `IMAGE_SIZE` | `1024x1024` | The entry's preset, the same value the unit's `-W`/`-H` carry. |
| `IMAGE_STEPS` | `8` | The entry's preset, the same value the unit's `--steps` carries. |

`IMAGE_GENERATION_MODEL` stays unset so Open WebUI never POSTs a checkpoint
switch to a single-model server, and `AUTOMATIC1111_PARAMS` stays unset because
`cfg_scale` is sd-server's argv default.

**The image server (`villa-image`)** runs `stable-diffusion.cpp`'s `sd-server`
(`ghcr.io/leejet/stable-diffusion.cpp:master-vulkan@sha256:367ccc…`, a rolling
tag pinned by digest, `internal/orchestrate/image.go`) with the entrypoint
`/sd-server`, the same `/dev/dri`, `keep-groups` and `seccomp=unconfined` device
access the Vulkan chat unit reads from the seam, the read-only models mount, and no
host port. Its flags come from `internal/inference/image_server.go`:

| Flag | Purpose |
|------|---------|
| `--listen-ip 0.0.0.0 --listen-port 1234` | Container-internal bind only; reachable from `villa.network` alone. |
| `--diffusion-model`, `--llm`, `--vae` | The entry's three files under `/models`, each under its own flag. |
| `--backend Vulkan0 --params-backend Vulkan0` | Explicit placement on the Vulkan device, which disables sd-server's auto-fit (the default places params on the GPU, RAM or disk by free memory, a silent CPU fallback). |
| `--eager-load` | Load every param at start, so the footprint is held from the first moment and the placement line is in the journal before the proof's first request. |
| `--vae-tiling` | Keeps the 1024x1024 VAE decode at a 416 MB buffer instead of a 5.8 GB one the device refuses. |
| `--diffusion-fa` | Flash attention in the diffusion model. |
| `--cfg-scale 1 --steps 8 -W 1024 -H 1024` | The entry's preset as argv defaults; Open WebUI's request carries no `cfg_scale`, so this is the value used. |

**The task sandbox** is not a unit. Each `villa work` task is one `podman run`,
rendered by `orchestrate.RenderSandboxRun` (`internal/orchestrate/sandbox.go`) as a
fixed argument list, never a shell. The flags are the boundary, so they are frozen
by a test and listed here in full:

| Argument | Purpose |
|----------|---------|
| `--rm -i --init` | One container per task, removed on exit; stdio is the only channel across the boundary. |
| `--name villa-task-<id>` | Named by task id, so `villa task cancel` kills it by name. |
| `--runtime=krun` | A libkrun microVM with its own guest kernel. krun ignores `--user`: the guest runs as root, and files it writes come out owned by you. |
| `--network villa-sandbox` | The internal network (`Internal=true`): the task reaches the served model and nothing else. |
| `--read-only --tmpfs /tmp` | A read-only root; `/tmp` is the only scratch space, and it is where Crush's config and data live. |
| `--memory <sandbox_memory> --cpus <sandbox_cpus>` | From `config.toml`; defaults `4g` and `4`. |
| `--volume <workspace>:/workspace:Z` | The one read-write mount: the registered grant. |
| `--volume <villa>:/usr/local/bin/villa:ro,z` | The running `villa` binary, read-only, so the guest can run `villa sandbox-bridge`. This is why the CGO-free build gate is load-bearing. |
| `--volume <crush>:/usr/local/bin/crush:ro,z` | The pinned Crush binary, read-only. |
| `-w /workspace -e HOME=/tmp` | Working directory and home. |
| `-e CRUSH_GLOBAL_CONFIG=/tmp/crushcfg -e CRUSH_GLOBAL_DATA=/tmp/crushdata` | Crush's config on the tmpfs. Its data directory is set by the bridge on the workspace-create call, not by this env var; the var is kept as the global default. |
| `-e CRUSH_DISABLE_METRICS=1 -e DO_NOT_TRACK=1 -e CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1` | The telemetry and auto-update kill set, the same one `villa code` renders. |
| `<image> villa sandbox-bridge [--model <model>] [--ctx <ctx>]` | The pinned image, then the bridge as entrypoint, told the served model and context window. |

There is no `--device` and no models volume: a task has no GPU and no weights.
The image is `localhost/villa-sandbox:office`, pinned by the digest of the build
`build/sandbox/Containerfile` produced (`internal/orchestrate/sandbox.go`). It is
the one pin villa builds rather than pulls, so its registry is `localhost`, and a
`dnf` build is not byte-reproducible: a rebuild yields a new digest, which
`villa update --check` reports as a rebuild, the way it does for the ROCm image.
`villa sandbox build` is the rebuild: it builds the image from the build context
embedded in the binary and records the digest it produced as this host's
effective pin, which is the reference the render above receives.

## Per-environment overrides

`villa` does **not** use a `NODE_ENV`-style environment switch or per-environment
config files (`.env.development`, etc.). It targets a single local host. The ways
configuration varies per machine are:

- **XDG base directories.** Setting `XDG_CONFIG_HOME` / `XDG_DATA_HOME` relocates
  the config file, the generated Quadlet units, and the models directory. This is
  the primary mechanism for running an isolated `villa` instance (for example, in a
  test harness): every host-touching path is derived from these, and the test code
  paths (`LoadVillaFrom` / `SaveVillaTo`, the injectable `configDeps` and lifecycle
  seams) point them at a temporary directory.
- **Per-host recommendation.** `villa recommend` reads the detected hardware
  (memory envelope, GPU) and produces a model/quant/context that fits *that* host;
  `--save` writes it to `config.toml`. The same binary therefore produces a
  different `config.toml` on a 64 GB vs a 128 GB machine. The fit is `weights + KV
  + headroom + prompt cache (+ projector + draft) <= envelope`; the prompt cache
  is the 8 GiB `--cache-ram` cap every chat `llama-server` unit renders (see
  [Managed container environment](#managed-container-environment)), so a 64 GB host
  reserves 8 GiB more than it did before v1.16. `villa recommend --json` reports it
  as `prompt_cache_bytes` (schema 8, appended to the schema 7 draft fields below),
  and the table prints it as a `+ prompt cache` row. Before that fit, the envelope
  shrinks by every reservation for a service beside the chat model: the embedding
  model when `memory_enabled`, the reranker when `reranker` is also set
  (ADR-0028), the extractor when `extractor` is also set (ADR-0033), the
  injection budget when `web_search_enabled` (ADR-0027), and the image model's
  measured footprint when `image_enabled` (ADR-0027, ADR-0032). `--json` lists
  them as `reservations`, an array of `{name, bytes}`
  (schema 9), and the table prints one `− <name> reservation` row for each.
- **External catalog override.** `catalog_path` (or `--catalog`) lets a host use a
  curated model list different from the embedded seed.

### Backend selection

The `backend` key selects the GPU backend the inference unit renders against. Four
values are honored by the inference resolver (`BackendFor`):

- **`rocm`** (ROCm 10.0 / HIP, ADR-0022) is **the default**, and what an empty or absent
  config resolves to. `rocm-10.0` names the same image explicitly, and `rocm-7.2.4`
  keeps the former default (llama.cpp build 9536, which cannot load `qwen4exp`
  models) reachable by name. It adds the `/dev/kfd` device and sets the ordered
  `HSA_OVERRIDE_GFX_VERSION` / `ROCBLAS_USE_HIPBLASLT` env (see
  [Managed container environment](#managed-container-environment)), so it requires a
  host that passes the ROCm bring-up gate.
- **`rocm-6.4.4`** and **`rocm-6.4.4-rocwmma`** are additive digest-pinned ROCm 6.4.4
  variants, identical to `rocm` apart from the image. Benchmark them with
  `villa bench --ab --ab-target <name>` rather than assuming a win.
- **`vulkan`** (Vulkan RADV) is the fallback. Stable and compatible across model
  sizes, with no ROCm host requirements; the only value `config set` will write.

`villa recommend` recommends `rocm` and only falls back to `vulkan` when the host is
**confidently** not ROCm-ready: every readiness signal known and at least one known-bad
(for example a denied `linux-firmware` build or a sub-floor kernel). An unevaluable
signal never triggers the fallback, so an unprobed host is not silently downgraded; the
accompanying note names the blocker. That fallback only annotates a *recommendation*;
it never rewrites a `config.toml` you already chose.

Switching backend is a stateful operation, not a plain config edit:

```bash
villa backend show            # inspect the active backend + image
villa backend set vulkan      # transactional cutover to the Vulkan RADV fallback
villa backend set rocm --dry-run   # preview target/fit/preflight, mutate nothing
villa backend set rocm        # switch back to the ROCm default
```

`villa backend set <backend>` re-checks the **preserved** model against the target
memory envelope (refuse-with-remediation if it no longer fits), runs the ROCm
preflight when the target is any ROCm-family backend, captures the prior unit verbatim, persists
`config.toml` and regenerates **only** the inference unit, restarts it, and **proves**
the cutover with a real generation probe plus a GPU-residency check within a bounded
timeout. Any mutate error or a non-passing proof rolls the switch back verbatim: a
failed switch is a no-op to the running stack. `--dry-run` previews the target, the
fit verdict, and the preflight without writing, regenerating, or restarting anything.

### Speculation

The `speculation` key selects the speculative-decoding mode the inference unit is
started with. Three values are honored, and an absent key means off:

- **`off`** renders no speculation flag at all.
- **`ngram`** renders llama-server's `--spec-type ngram-mod`. It downloads nothing
  and costs no memory: the drafts come from n-grams of the context itself.
- **`draft`** renders the entry's draft sidecar, a companion GGUF pulled and
  verified with the model: `--spec-type <spec_type>` with the draft file, the
  backend's own residency device, every draft layer on the device, and the
  `n_max` and `p_min` the catalog measured. When the entry is also `ngram_safe`
  the spec type is `ngram-mod,<spec_type>`, so `draft` on such an entry is both.

`ngram` is neutral on a prompt the server has not seen and up to 2.8x on repeated
output, which is what `villa code` produces. It is offered only for a catalog entry
carrying a measurement that licensed it (`ngram_safe` plus an `ngram_provenance`
naming the probe); asking for it on an entry without one is a refusal, never a
silent downgrade to off. `villa recommend` shows the mode it resolved, and
`--save` and `villa install` persist it exactly as they persist the backend.

`draft` pays on a dense target, where the draft is a small fraction of a target
step; the catalog carries one only for an entry measured on this hardware. It is
a term of the fit in its own right, reserved after the vision projector, so a
tight envelope loses the speedup before it loses vision. An unset `speculation`
resolves down a ladder: `draft` when the entry carries one and it fits, else
`ngram` when the entry is qualified, else `off`, each with a note. A dropped
draft is noted in the recommendation. An explicit `draft` on an entry that has
no draft, or whose draft does not fit, is a refusal. `villa speculation set draft`
also refuses when the draft file is not on disk and names `villa model pull`,
because the swap never downloads. `villa recommend --json` reports the two
reserved terms as `draft_bytes` and `draft_kv_bytes` (schema 7), both `0` when
no draft is served. The numbers and the reasoning are in
[ADR-0009](adr/0009-a-draft-sidecar-is-proven-as-a-second-model.md).

Switching the mode is a stateful operation on the same transaction as a backend
switch:

```bash
villa speculation show              # the persisted mode (off when unset)
villa speculation show --json       # { "speculation": "..." }
villa speculation set ngram --dry-run   # preview target/fit, mutate nothing
villa speculation set ngram         # transactional cutover
villa speculation set draft         # the draft sidecar; refuses when it is not on disk
villa speculation set off           # turn it back off
```

`villa speculation set <mode>` re-checks the served model against the target mode,
captures the prior unit verbatim, persists `config.toml` and regenerates **only** the
inference unit, restarts it, and proves the cutover. Any mutate error or a
non-passing proof rolls back verbatim.

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
| `firmwareFloor` | `20260110` | Minimum linux-firmware date stamp; below it (but not denied) is a WARN advisory. |
| `firmwareDeny` | `["20251125"]` | linux-firmware builds documented to break ROCm on Strix Halo; a match is a hard **refusal**. |
| `imageDeny` | `["rocm7-nightlies"]` | ROCm image tags that reintroduce the 64 GB allocation cap; a requested image matching one is **refused**. |
| `requiredHSAOverride` | `11.5.1` | The `HSA_OVERRIDE_GFX_VERSION` value ROCm needs on gfx1151; a wrong/unset known value is **refused**. |

The gate is biased against over-blocking: a signal only **fails** (refuses) on a
positively-detected known-bad fact. Anything it cannot evaluate (a host fact that is
Unknown, or a probe not run off-hardware) degrades to a WARN, never a false refusal.
Of these signals, the linux-firmware date is probed on-host (from `rpm`) for the
ROCm-readiness sub-tree of `villa detect`, while the running `HSA_OVERRIDE_GFX_VERSION`
env is not read from the host environment: the cutover sets it inside the container
rather than depending on the user's shell.

The `firmwareFloor`/`firmwareDeny`/`kernelFloor`/`kernelTested` values
are also the source for the version-floor data the non-ROCm host preflight uses
(`Floors()`), so the two surfaces never disagree.
