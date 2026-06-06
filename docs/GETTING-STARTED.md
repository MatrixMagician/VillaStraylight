<!-- generated-by: gsd-doc-writer -->
# Getting Started

This guide takes you from a clean Fedora host to a running local AI workspace —
chat in your browser, inference on your iGPU, a control dashboard — using the
`villa` CLI. The happy path is four commands: `make build`, `villa preflight`,
`villa recommend`, `villa install`.

`villa` is the **control plane only**. The AI services it brings up (llama.cpp
`llama-server` for inference, Open WebUI for chat) are integrated OSS containers,
orchestrated through rootless Podman Quadlet units. Nothing leaves your machine.

The default first-run path uses the **Vulkan RADV** backend — stable and
compatible across model sizes. Once that stack is healthy, you can optionally
trial the **ROCm/HIP** backend with a transactional switch and an honest A/B
benchmark; see [Trying ROCm (optional, advanced)](#trying-rocm-optional-advanced).

## Prerequisites

You need the build toolchain to compile `villa`, and a supported host for it to
manage. `villa preflight` (step 1 below) checks the *host* requirements for you and
tells you exactly what is missing — so you do not have to verify them all by hand.

**To build the binary:**

- **Go 1.26+** — required to compile `villa` from source (`go.mod` pins `go 1.26.2`).
- **`make`** — the build is driven through the `Makefile`. You can also invoke
  `go` directly (see [Build without make](#build-without-make)).

**To run the stack (the host `villa` manages):**

- **Fedora Workstation 44+** on **AMD Strix Halo (gfx1151)** — the only supported
  host platform for v1. The architecture leaves room for a future
  macOS/Apple-Silicon backend, but it is not yet implemented.
- **Podman v5 (rootless)** with the user socket enabled. Enable it once with:
  ```bash
  systemctl --user enable --now podman.socket
  ```
  `villa` drives the AI stack through rootless Podman Quadlet/systemd units — there
  is no Docker dependency.
- **A Vulkan RADV (Mesa) GPU stack** — the default inference backend. If the RADV
  ICD or `/dev/dri` nodes are missing, `villa preflight` will tell you (check
  `PRE-01`) and suggest `sudo dnf install mesa-vulkan-drivers`.

You do **not** need to manually verify kernel params, SELinux booleans, or user
lingering up front — `villa preflight` classifies all of these and `villa install`
offers to fix the privileged ones with per-step consent.

## Installation steps

Clone the repository and build the single static binary:

```bash
git clone https://github.com/MatrixMagician/VillaStraylight.git
cd VillaStraylight
make build       # builds ./villa
```

This produces `./villa` in the repo root. Optionally move it onto your `PATH`:

```bash
install -m 0755 ./villa ~/.local/bin/villa
```

The walkthrough below uses `./villa` (running from the repo). If you put the binary
on your `PATH`, drop the `./` prefix.

### Build without make

The `Makefile` targets are thin wrappers around the Go toolchain. If you prefer not
to use `make`, the equivalents are:

```bash
go build -o villa ./cmd/villa     # same as `make build`
go run ./cmd/villa <subcommand>   # same as `make run`
```

## First run

Walk through the host check, the model recommendation, and the install. Each step
is read-only until `villa install`, so you can inspect freely before anything
touches your host.

### 1. Check the host is ready

```bash
./villa preflight
```

`villa preflight` is read-only. It runs the host-prep gate — Vulkan ICD + iGPU
enumeration (`PRE-01`), Podman rootless readiness (`PRE-02`), user lingering
(`PRE-03`), free disk/memory (`PRE-04`), and the SELinux `container_use_devices`
boolean (`PRE-05`) — and classifies each result as a **BLOCK** or **WARN**. It maps
the worst result to an exit code:

| Exit code | Meaning |
|-----------|---------|
| `0` | All checks passed. |
| `2` | Passed with warnings (or a `--force`-overridden block). |
| `1` | A blocking check failed — fix it (or re-run with `--force` to override, auditable). |

Each non-passing row prints a remediation command, so you can resolve gaps before
installing anything. Add `-v` to see the provenance (which tool or `/sys` path)
behind every result.

### 2. See what fits this machine

```bash
./villa recommend
```

`villa recommend` turns the detected hardware profile into a single memory-safe
model/quant/context/backend pick, and **shows the fit math**:

```
model_bytes + KV-cache @ ctx + headroom  ≤  usable envelope
```

It is read-only by default. Useful flags:

```bash
./villa recommend --alternatives   # also list other fitting picks
./villa recommend --save           # persist the pick to config.toml
```

If you want to inspect the raw hardware profile that drives the recommendation,
run `./villa detect` (CPU/arch, iGPU, Vulkan/ROCm availability, total RAM, and the
real usable GTT envelope).

### 3. Install and bring the stack up

```bash
./villa install
```

This is the full managed bring-up. In one command, `villa install`:

1. Detects the host and recommends a fitting model.
2. Gates on a safe host. For any blocking host-prep gap with an automated fix
   (SELinux `container_use_devices`, user lingering), it **offers** the exact
   privileged command and runs it only on your explicit `y`. `villa` never silently
   runs a privileged command — declined or non-interactive gaps are printed and
   treated as a block (overridable with `--force`).
3. Downloads and verifies the recommended GGUF model (skipped if already present).
4. Persists the selection to `config.toml`.
5. Renders rootless Podman Quadlet units, writes only what changed, and starts
   inference, then Open WebUI, then the control dashboard.
6. Polls until the inference endpoint reports healthy, then prints the URLs.

Re-running `villa install` with unchanged config is a true no-op. To preview the
rendered units without writing, pulling, or starting anything:

```bash
./villa install --dry-run
```

When the install completes, it prints your loopback endpoints:

| Service | URL |
|---------|-----|
| Chat (Open WebUI) | `http://127.0.0.1:3000` |
| Control dashboard | `http://127.0.0.1:8888` |
| Inference (OpenAI-compatible API) | printed by `villa install` (loopback) |

All three bind to loopback only — never a routable interface. The chat and
dashboard ports are configurable (`chat_port`, `dashboard_port`); see
[CONFIGURATION.md](CONFIGURATION.md).

### 4. Open the dashboard and chat

The dashboard is brought up as a managed, boot-surviving service by `villa
install`, so the link above is live immediately. If you want to run it in the
foreground (or it is not running), start it explicitly:

```bash
./villa dashboard        # serves on 127.0.0.1:8888 (loopback only)
```

Open `http://127.0.0.1:3000` for chat and `http://127.0.0.1:8888` for the
read-only control dashboard. Confirm everything is healthy with:

```bash
./villa status           # unit + container + /health + GPU-offload proof
```

`villa status` also reports the active backend and its resolved (digest-pinned)
container image, so you always know which backend the running stack is on.

## Trying ROCm (optional, advanced)

The default backend is **Vulkan RADV**, and it is the only backend exercised on
the v1.0 happy path above. Vulkan is stable and compatible across model sizes;
**you do not need to do anything in this section to have a working stack.**

v1.1 adds an **opt-in ROCm/HIP backend** as a performance option. ROCm can win on
token generation at long context, but it is more sensitive to kernel/firmware
versions and requires a runtime override — so it is strictly opt-in, never the
first-run default. Only trial it once your Vulkan stack is already healthy
(`./villa status` is green).

The switch is **transactional** (capture → mutate → prove → rollback): a failed
switch is a no-op to the running stack — your Vulkan setup is restored verbatim,
so trialing ROCm cannot leave you worse off.

### 1. Inspect the active backend

```bash
./villa backend show     # active backend (from config) + resolved digest-pinned image
```

### 2. Preview the switch without changing anything

`--dry-run` reports the target, the fit verdict (the preserved model re-checked
against the target envelope), and the ROCm preflight — and writes nothing:

```bash
./villa backend set rocm --dry-run
```

### 3. Switch to ROCm

```bash
./villa backend set rocm
```

This re-fit-guards the preserved model, runs the ROCm preflight gate, regenerates
**only** the inference unit, restarts it, and **proves** the cutover with a real
generation probe plus a GPU-residency check inside a bounded timeout. If any step
fails — or the preflight refuses (e.g. a too-old kernel, a denied linux-firmware
build, or a missing `HSA_OVERRIDE_GFX_VERSION`) — the switch rolls back verbatim
and your Vulkan stack keeps running. <!-- VERIFY: kernel version, linux-firmware date, and gfx1151 readiness are host facts probed at runtime and cannot be confirmed from the repository alone -->

The ROCm preflight is also available standalone (read-only):

```bash
./villa preflight --backend rocm
```

### 4. Compare the two backends honestly

```bash
./villa bench            # throughput of the RUNNING backend only (non-disruptive)
./villa bench --ab       # also flip to the other backend, bench it identically,
                         # restore the original, and report the per-metric delta
```

`villa bench` reports prompt-processing (pp) and token-generation (tg) throughput
**separately** — never a blended number. `--ab` always restores the original
backend on exit. Tuning flags: `--reps`/`-n` (counted runs per side, default `5`),
`--warmup` (discarded warm-up runs, default `1`), and `--n-predict` (fixed
`max_tokens` per run, default `128`).

### 5. Switch back to Vulkan

```bash
./villa backend set vulkan
```

The same transactional guarantees apply. Vulkan RADV is always a safe place to
return to.

## Common setup issues

Most first-run problems are exactly the things `villa preflight` flags. Run it
first — the table tells you which check failed and prints the fix.

- **Podman socket / rootless not ready (`PRE-02`).** If `villa preflight` reports
  that Podman rootless readiness could not be verified, enable the user socket and
  confirm subuid/subgid ranges exist:
  ```bash
  systemctl --user enable --now podman.socket
  ```
  A present Podman with missing `/etc/subuid` or `/etc/subgid` ranges is a hard
  fail — the remediation line prints the exact `usermod --add-subuids …` command.

- **Vulkan RADV not detected (`PRE-01`).** llama.cpp silently falls back to CPU (or
  fails to load) without a working Vulkan backend. Install the Mesa RADV drivers
  and confirm the iGPU exposes device nodes:
  ```bash
  sudo dnf install mesa-vulkan-drivers
  ls /dev/dri
  vulkaninfo --summary
  ```

- **SELinux blocks device access (`PRE-05`).** Rootless containers need the
  `container_use_devices` boolean to reach the iGPU. `villa install` offers to set
  this for you on consent; the manual command is:
  ```bash
  setsebool -P container_use_devices=true
  ```

- **Services do not survive reboot (`PRE-03`).** Without user lingering, the
  user-systemd units stop when you log out. `villa install` offers to enable it;
  the manual command is:
  ```bash
  loginctl enable-linger "$USER"
  ```

- **No fitting configuration / recommend refused.** If `villa install` reports the
  memory envelope is undeterminable, run `./villa detect` to confirm the GPU and
  memory envelope are visible, then `./villa recommend` to inspect the fit math.

- **A blocking check you accept the risk on.** Any blocking preflight gap can be
  overridden with the global `--force` flag (`villa preflight --force`,
  `villa install --force`). The override prints an auditable summary of exactly what
  was bypassed and degrades the verdict to a warning — it never reports a clean pass.

## Next steps

- **[README.md](../README.md)** — the full command reference (model management,
  inference validation, the lifecycle verbs `up` / `down` / `restart` / `logs`,
  and the v1.1 `backend` / `bench` verbs).
- **[CONFIGURATION.md](CONFIGURATION.md)** — the `config.toml` surface (model,
  quant, ctx, backend, dashboard/chat ports), where it lives
  (`~/.config/villa/config.toml`), how to inspect or change it with
  `villa config show` / `villa config set`, the backend-selection rules, and the
  ROCm bring-up policy (version floors, denylists, required override).
- **[ARCHITECTURE.md](ARCHITECTURE.md)** — how the control plane, the generated
  Quadlet units, and the integrated OSS containers fit together.
