# Getting started

This guide takes you from a clean Fedora host to a running local AI workspace
(chat in your browser, inference on your iGPU, a control dashboard) using the
`villa` CLI. The happy path is four commands: `make build`, `villa preflight`,
`villa recommend`, `villa install`.

`villa` is the **control plane only**. The AI services it brings up (llama.cpp
`llama-server` for inference, Open WebUI for chat) are integrated OSS containers,
orchestrated through rootless Podman Quadlet units. Nothing leaves your machine.

The default first-run path uses the **ROCm/HIP** backend. `villa preflight
--backend rocm` gates it, and `villa recommend` automatically falls back to the
**Vulkan RADV** backend if your host is confidently not ROCm-ready, so the happy
path below works either way. You can switch between them at any time with a
transactional cutover and compare them with an honest A/B benchmark; see
[Switching backends](#switching-backends).

## Prerequisites

You need the build toolchain to compile `villa`, and a supported host for it to
manage. `villa preflight` (step 1 below) checks the *host* requirements for you and
tells you exactly what is missing, so you do not have to verify them all by hand.

**To build the binary:**

- **Go 1.26+** to compile `villa` from source (`go.mod` pins `go 1.26.2`).
- **`make`**, which drives the build through the `Makefile`. You can also invoke
  `go` directly (see [Build without make](#build-without-make)).

**To run the stack (the host `villa` manages):**

- **Fedora Workstation 44+** on **AMD Strix Halo (gfx1151)**, the only supported
  host platform for v1. The architecture leaves room for a future
  macOS/Apple-Silicon backend, but it is not yet implemented.
- **Podman v5 (rootless)** with the user socket enabled. Enable it once with:
  ```bash
  systemctl --user enable --now podman.socket
  ```
  `villa` drives the AI stack through rootless Podman Quadlet/systemd units; there
  is no Docker dependency.
- **A ROCm/HIP GPU stack** for the default inference backend, which also needs
  `HSA_OVERRIDE_GFX_VERSION=11.5.1` in the runtime environment and a kernel and
  `linux-firmware` at or above the pinned floors. Check it with
  `./villa preflight --backend rocm`, which refuses with a concrete remediation on a
  confidently-bad host rather than letting bring-up fail later.
- **A Vulkan RADV (Mesa) GPU stack** for the fallback inference backend, worth
  having even on ROCm: it is where `villa recommend` and `villa backend set vulkan`
  land you if ROCm is not viable. If the RADV ICD or `/dev/dri` nodes are missing,
  `villa preflight` will tell you (check `PRE-01`) and suggest
  `sudo dnf install mesa-vulkan-drivers`.

You do **not** need to manually verify kernel params, SELinux booleans, or user
lingering up front: `villa preflight` classifies all of these and `villa install`
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
go build -ldflags "-X main.version=$(git describe --tags --always --dirty)" \
  -o villa ./cmd/villa            # same as `make build` (which stamps the version)
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

`villa preflight` is read-only. It runs the host-prep gate: Vulkan ICD + iGPU
enumeration (`PRE-01`), Podman rootless readiness (`PRE-02`), user lingering
(`PRE-03`), free disk/memory (`PRE-04`), the SELinux `container_use_devices`
boolean (`PRE-05`), two WARN-tier version floors, the kernel (`PRE-06`) and
`linux-firmware` (`PRE-07`), and compute device access, `/dev/kfd` and a render
node, as your user (`PRE-08`). With the workspace agent on it also checks the
sandbox runtime (`PRE-09`): the `krun` binary, the `libkrun`, `libkrunfw` and
`crun-krun` packages, `/dev/kvm` opened read-write as your user, and one real
`podman run --runtime=krun` whose kernel differs from the host's. Each result is
classified as a **BLOCK** or **WARN**. It maps the worst result to an exit code:

| Exit code | Meaning |
|-----------|---------|
| `0` | All checks passed. |
| `2` | Passed with warnings (or a `--force`-overridden block). |
| `1` | A blocking check failed. Fix it (or re-run with `--force` to override, auditable). |
| `130` | Interrupted with Ctrl-C. Applies to every `villa` command, not just preflight. |

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
   runs a privileged command: declined or non-interactive gaps are printed and
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

All three bind to loopback only, never to a routable interface. The chat and
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

## Switching backends

The default backend is **ROCm/HIP 7.2.4**, which `villa install` brings up for you.
**You do not need to do anything in this section to have a working stack**: this
section is for moving between backends after the fact.

**Vulkan RADV** is the fallback. It is less sensitive to kernel/firmware versions and
needs no runtime override, so it is the right target if ROCm is unhealthy on your host
(`villa recommend` will already have suggested it if the host is confidently not
ROCm-ready). Two additional digest-pinned variants, `rocm-6.4.4` and
`rocm-6.4.4-rocwmma`, are also selectable; benchmark them rather than assuming a win.

Every switch is **transactional** (capture → mutate → prove → rollback): a failed
switch is a no-op to the running stack. The previous setup is restored verbatim, so
trialing a backend cannot leave you worse off.

### 1. Inspect the active backend

```bash
./villa backend show     # active backend (from config) + resolved digest-pinned image
```

### 2. Preview the switch without changing anything

`--dry-run` reports the target, the fit verdict (the preserved model re-checked
against the target envelope), and the ROCm preflight. It writes nothing:

```bash
./villa backend set vulkan --dry-run
```

### 3. Switch

```bash
./villa backend set vulkan     # to the RADV fallback
./villa backend set rocm       # back to the ROCm default
```

This re-fit-guards the preserved model, runs the ROCm preflight gate when the target
is a ROCm backend, regenerates **only** the inference unit, restarts it, and
**proves** the cutover with a real generation probe plus a GPU-residency check inside
a bounded timeout. If any step fails or the preflight refuses (e.g. a too-old
kernel, a denied linux-firmware build, or a missing `HSA_OVERRIDE_GFX_VERSION`), the
switch rolls back verbatim and the stack you were already running keeps running.

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
**separately**, never as a blended number. `--ab` always restores the original
backend on exit. Tuning flags: `--reps`/`-n` (counted runs per side, default `5`),
`--warmup` (discarded warm-up runs, default `1`), and `--n-predict` (fixed
`max_tokens` per run, default `128`).

### 5. Getting back to a known-good state

```bash
./villa backend set vulkan
```

The same transactional guarantees apply. Vulkan RADV has the fewest host
requirements, so it is always a safe place to return to if a ROCm backend misbehaves.

## Turning speculation on

`villa recommend` prints a `speculation` line with the mode it resolved for the
picked model: `draft` when the catalog entry ships a draft sidecar and it fits,
`ngram` when the entry carries a measurement that qualified it, `off` otherwise.
`--save` and `villa install` persist whatever it resolved, so a fresh install
already runs the mode the recommendation showed you. On an install that predates
the setting the key is simply absent, which renders speculation off and leaves
the unit unchanged. To turn it on afterwards:

```bash
./villa speculation show               # the persisted mode (off when unset)
./villa speculation set ngram --dry-run   # preview the target and the fit
./villa speculation set ngram          # transactional cutover, rolls back on failure
```

`ngram` is llama-server's `ngram-mod`: it drafts tokens from n-grams of the context
itself, so it downloads nothing and costs no memory. It is roughly neutral on a
prompt the server has not seen and up to 2.8x on repeated output, which is what
`villa code` produces. Asking for it on a model with no qualified measurement is a
refusal rather than a silent downgrade.

`draft` is the sidecar draft. On `qwen3.8-27b` it is the model's own
multi-token-prediction head, a 1 GiB file pulled and verified with the weights,
and it roughly doubles token generation on a prompt the server has not seen
(11 to 20 tokens per second on the dev host). The entry is also `ngram_safe`,
so `draft` there runs ngram-mod alongside the head and reaches 34 to 78 on
repeated output. `villa recommend` shows the draft's weight and KV as their own
rows beside the projector and resolves `draft` when it fits; to switch a running
stack:

```bash
./villa speculation set draft --dry-run   # preview the target and the fit
./villa speculation set draft          # transactional cutover, rolls back on failure
```

The swap never downloads. If the draft file is not on disk the set refuses and
names `villa model pull`. See
[CONFIGURATION.md](CONFIGURATION.md#speculation) for the vocabulary.

## Attaching an image in chat

Some catalog entries ship a vision projector, a companion file pulled and verified
with the model. When it fits the envelope alongside the model and the KV cache,
`villa recommend` shows it as its own `+ vision projector` row and prints
`Vision: yes`; `--save` and `villa install` persist that and pull the projector with
the weights. Run `./villa status` and read the `vision` row: on `yes`, attach a
screenshot or photo to a message in Open WebUI and ask about it. When the projector
did not fit, the recommendation says so in a note and `Vision: no` — the model still
runs, text-only, and an attached image will not be looked at.

## Running a task on your own files

The workspace agent runs one instruction against one folder. The folder is the
only thing the task can write; a libkrun microVM is the boundary; the served
chat model is the only thing the VM can reach. Everything below is off until you
opt in, and the install you already did is byte-identical without it.

### 1. Turn it on

```bash
./villa install --workspace-agent     # persists workspace_agent and tools_mode, renders villa-sandbox.network
```

On a stack that is already installed you can instead run
`./villa tools-mode enter`, which serves the chat unit with the model's own chat
template under a transactional cutover; without it, tool calls fail at the
server and `villa work` refuses. Coding mode already implies tools mode.

Two things the install does not do. It does not install the sandbox runtime:
`PRE-09` names the packages (`libkrun`, `libkrunfw`, `crun-krun`) and the
`/dev/kvm` access it needs, and refuses with that remediation until they are
there. And it does not build the sandbox image. The image villa runs a task in
is `localhost/villa-sandbox:office`, pinned by the digest of a build of
`build/sandbox/Containerfile`. A build on your own host produces a different
digest, because `dnf` is not byte-reproducible, and villa has no verb yet that
records that digest as the effective pin. Until it does, a task runs only on a
host that holds the pinned build. On any other host `PRE-09` reports the probe
as unevaluable, and the task is refused once podman fails to find the image:
`refused: the bridge exited before it was ready`.

Crush, villa's pinned coding agent, is the harness inside the VM. It is the same
binary `villa install --coding-agent` places under the villa data root, so that
addon must be installed too. Villa does not check for it before launch; a
missing binary is the same refusal, once the mount fails.

### 2. Register a folder

```bash
./villa workspace add ~/Documents/quarterly
./villa workspace list
```

Registration is the grant. `add` refuses a relative path, your home directory
itself, anything outside it, anything overlapping villa's own config or data
root, a folder nested with a grant you already have, and a path that is missing
or not a directory. `remove` forgets the grant and touches no file.

### 3. Run a task and answer the prompt

```bash
./villa work ~/Documents/quarterly "Reconcile invoices.csv against bank.csv and write reconciliation.md"
```

The terminal follows the task over the dashboard service's loopback API and
prints its narration. Reads are allowed without asking. A write parks the task
and asks:

```
[queued]
[running]
allowed: read view invoices.csv
allowed: read view bank.csv
[awaiting_approval]
approval #1: write wants to write reconciliation.md
allow? [y once / a for the rest of this task / N deny]: y
approval #1 answered allow by dashboard
[running]
created reconciliation.md
run complete
[flagged]
created reconciliation.md
flagged reconciliation.md: "Invoice 1043 was paid on 3 March" is unsupported (no source names a payment date)
20260910-141500-a1b2 flagged (exit 2)
```

`y` allows once, `a` allows the rest of this task's requests, and any other
answer denies; deny is the default. `by dashboard` names the API the answer
went through, which is the same one for the terminal and the panel. A denial is
told to the model, recorded, and does not change the exit code.

`--auto` makes this task's writes need no approval. Deletion still asks in every
mode. Villa decides that from a command pattern (`rm`, `rmdir`, `unlink`,
`shred`, `trash`, `git clean`, `find -delete`, including behind `sudo`, `env`,
`xargs` and shell chaining), and a pattern is not a proof: the grant bounds what
a miss could reach. In `--auto` mode a command runs without asking only when it
is on Crush's own read-only list or is `python3`, `soffice`, `mv` or `cp` with
every path argument inside the workspace.

An instruction of `-` reads the prompt from stdin. `--detach` submits, prints
the task id, and returns; a detached task that needs an approval parks in
`awaiting_approval` with no timeout until you answer it. Villa owns no timer.
If you want a task on a schedule, drive `villa work --detach` from your own
systemd timer.

Before submitting, `villa work` refuses three things and exits `1`:

```
work: refused. "/home/you/notes" is not a registered workspace. Grant it with: villa workspace add /home/you/notes
work: refused. tools mode is off, so the chat unit is not served with the model's own template and the agent has no tool loop. Turn it on with: villa tools-mode enter
work: refused. dashboard service is not answering on http://127.0.0.1:8888. Start it with: systemctl --user start villa-dashboard.service
```

### 4. Read the record

```bash
./villa task list                     # ID  WORKSPACE  STATE  SUBMITTED  EXIT
./villa task show 20260910-141500-a1b2
./villa task show 20260910-141500-a1b2 --json   # the record verbatim (schema 1)
```

`show` prints the state and exit code, the harness and its version, every file
the task created or modified, every approval with its answer, and one
`grounding` line per document the audit checked, followed by a `flagged` line
per unsupported claim. A task that did not finish shows `-` in the exit column.
Records and logs live under `~/.local/share/villa/tasks/` and are never deleted.

A detached task is answered from any terminal:

```bash
./villa task approve 20260910-141500-a1b2         # once
./villa task approve 20260910-141500-a1b2 --all   # the rest of this task, the prompt's `a`
./villa task deny 20260910-141500-a1b2
./villa task cancel 20260910-141500-a1b2          # drop it if queued, kill it if running
```

`cancel` keeps whatever the task already wrote; undoing partial work is a new
task. One task runs at a time; the rest queue in submission order.

### 5. The dashboard

`http://127.0.0.1:8888` gains three panels. **Workspaces** lists the grants.
**Tasks** lists the records with their state. Clicking a row opens **the task
detail**: the record, its live narration over the same event stream the terminal
reads, approve and deny buttons while the task is parked, and a cancel button
while it has not finished. Submitting a task from the dashboard is deferred; the
terminal is the only way in.

### Exit codes

| Exit | State | Meaning |
|------|-------|---------|
| `0` | `done` | The task finished and the audit found nothing to flag. |
| `2` | `flagged` | The task finished, and either a document carries a claim the audit could not support or the audit could not run. The document is left as written. |
| `1` | `failed`, `refused`, `cancelled`, `interrupted` | The harness failed; villa declined before the sandbox started; you cancelled it; or the dashboard service restarted while it ran. |
| `1` | (no record) | `villa work` refused before submitting: an unregistered folder, tools mode off, or the service not answering. |

### What to know before trusting it

- **Root inside the VM.** krun ignores `--user`, so the guest runs as uid 0 and
  the files it writes come out owned by you. The VM is the boundary, not the
  user.
- **Deletion is a pattern.** The rule that makes deletion always ask matches
  command names. It is stated as a pattern, not claimed as a proof.
- **The audit is the same model.** After the run, one chat completion per
  document asks the model that wrote it to list its claims against the files the
  session read. A clean audit means the auditor found none, and nothing more. On
  the prototype's outputs it flagged 10 of 19 claims in a memo that invented
  facts, 5 of 23 in a reconciliation with a wrong totals row, and 1 of 17 in an
  action-items list that was correct.
- **`--verbose` is host-local.** `villa work -v` replays the task's full
  transcript after the summary, from the log file under this host's villa data
  root. There is no log route: the transcript never crosses the API, so it only
  works on the machine that ran the task.
- **Bring your own timer.** `--detach` and the queue ship now; a villa-owned
  timer does not. `villa work --detach` from your own systemd timer is the
  supported shape.
- **`villa doctor`** gains `SBX-01` (the read-only twin of `PRE-09`), `SBX-02`
  (`villa-sandbox.network` is on disk and the inference unit is joined to it)
  and `TMD-01` (the served unit carries the tool-calling flag exactly when tools
  mode is on). `villa status` prints `mode  tools` on the inference line and, once
  a task has run, a `last task  <id> <state>` row.

## Common setup issues

Most first-run problems are exactly the things `villa preflight` flags. Run it
first: the table tells you which check failed and prints the fix.

- **Podman socket / rootless not ready (`PRE-02`).** If `villa preflight` reports
  that Podman rootless readiness could not be verified, enable the user socket and
  confirm subuid/subgid ranges exist:
  ```bash
  systemctl --user enable --now podman.socket
  ```
  A present Podman with missing `/etc/subuid` or `/etc/subgid` ranges is a hard
  fail; the remediation line prints the exact `usermod --add-subuids …` command.

- **ROCm bring-up refused (`ROCM-PRE-*`).** Since ROCm is the default backend,
  `./villa preflight --backend rocm` is worth running before you install. It refuses
  with a concrete remediation on a confidently-bad host: a sub-floor kernel, a denied
  `linux-firmware` build, a non-gfx1151 device, or an unset
  `HSA_OVERRIDE_GFX_VERSION`. Either fix what it names, or use the Vulkan fallback:
  ```bash
  ./villa preflight --backend rocm
  ./villa backend set vulkan     # if ROCm is not viable on this host
  ```

- **Vulkan RADV not detected (`PRE-01`).** llama.cpp silently falls back to CPU (or
  fails to load) without a working Vulkan backend. This matters even on the ROCm
  default, since Vulkan is the fallback you would switch to. Install the Mesa RADV
  drivers and confirm the iGPU exposes device nodes:
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

- **Cannot open `/dev/kfd` or the render node (`PRE-08`).** The invoking user
  needs read+write access to the compute device nodes themselves, not just their
  presence — a user outside the `render`/`video` groups passes every other check
  and then watches the ROCm unit fail to start. Add your user to both groups and
  log in again (the new membership does not apply to your current session):
  ```bash
  sudo usermod -aG render,video "$USER"
  # then log out and log back in, and confirm with:
  id -nG
  ```
  If `/dev/kfd` itself is reported absent rather than denied, the amdgpu kernel
  module is not loaded (no group fix will help) — check `lsmod | grep amdgpu` and
  `dmesg | grep -i kfd` instead. On the Vulkan fallback, `/dev/kfd` access is
  informational only; only the render node blocks.

- **Services do not survive reboot (`PRE-03`).** Without user lingering, the
  user-systemd units stop when you log out. `villa install` offers to enable it;
  the manual command is:
  ```bash
  loginctl enable-linger "$USER"
  ```

- **It installed clean, but something is wrong now.** `villa preflight` answers
  "can this host install?"; it is not the tool for a stack that already came up.
  For that, run the read-only runtime twin:
  ```bash
  ./villa doctor
  ```
  It reports host conditions, per-service health, the GPU-offload proof, and
  config-vs-disk drift (a unit on disk that no longer matches `config.toml` is
  usually a hand-edit, which `villa up` will overwrite on the next reconcile;
  the finding names the units that differ).
  With web search on it also reports `websafe-binary`: a WARN when the villa you
  are running is not the binary `villa-websafe` bind-mounts, which happens after
  a rebuild somewhere else or a move. The container keeps serving the old path
  until `villa install` rewrites the unit.
  It also reports one `CAT-01` finding per model on this host, comparing the
  catalog's fit dimensions against the GGUF header of the file they describe; a
  FAIL there means villa's own catalog entry is wrong, not your install
  ([ADR-0007](adr/0007-the-catalog-is-the-truth-and-the-gguf-header-is-its-witness.md)).

- **No fitting configuration / recommend refused.** If `villa install` reports the
  memory envelope is undeterminable, run `./villa detect` to confirm the GPU and
  memory envelope are visible, then `./villa recommend` to inspect the fit math.

- **A blocking check you accept the risk on.** Any blocking preflight gap can be
  overridden with the global `--force` flag (`villa preflight --force`,
  `villa install --force`). The override prints an auditable summary of exactly what
  was bypassed and degrades the verdict to a warning, it never reports a clean pass.

## Next steps

### Docs

- [README.md](../README.md) is the full command reference: model management,
  inference validation, and the lifecycle verbs `up` / `down` / `restart` / `logs`.
- [CONFIGURATION.md](CONFIGURATION.md) covers the `config.toml` surface (model,
  quant, ctx, backend, dashboard/chat ports, the resident set), where it lives
  (`~/.config/villa/config.toml`), how to inspect or change it with
  `villa config show` / `villa config set`, the backend-selection rules, and the
  ROCm bring-up policy (version floors, denylists, required override).
- [MEMORY.md](MEMORY.md) covers the memory and knowledge stack in full.
- [ARCHITECTURE.md](ARCHITECTURE.md) shows how the control plane, the generated
  Quadlet units, and the integrated OSS containers fit together.

### What else the stack can do

Everything below is **off by default and opt-in**: the install you just did is
byte-identical whether or not these exist, and each is proven at runtime rather than
assumed. How you turn one on differs by subsystem: the "Start here" column is the
actual enable path, not a suggestion. What none of them are is a `villa config set`
key: its allowlist is `model` / `quant` / `ctx` / `backend` / `catalog_path`, and the
guided install deliberately does not prompt for subsystems either.

| Subsystem | What it adds | Start here |
|-----------|--------------|------------|
| **Memory & knowledge** | Cross-chat memory plus cited answers from your own documents. Embeddings are computed and held locally in Qdrant; no cloud embedding API. | Set `memory_enabled=true` in `config.toml`, then `villa install` (the one gate with no flag). See [MEMORY.md](MEMORY.md) |
| **Recall** | Indexes your past conversations so new chats retrieve what you discussed weeks ago, by meaning, with citations. | `villa recall index` / `villa recall status` |
| **Coding agent** | A strictly-local terminal coding agent (a locked-down Crush) talking to your own model. `villa code --agent claude` launches Claude Code against the same endpoint instead, if you already have `claude` on PATH — coding mode is a hard gate for that target, and villa never installs `claude` itself. | `villa install --coding-agent` to install it (persists the gate), then `villa code` |
| **Coding mode** | Flips the running stack to a tool-calling configuration tuned for that agent, and back. The cutover is transactional, so a failed flip is a no-op. | `villa coding-mode enter` / `exit` |
| **Web search** | Grounded answers from the web through a local SearXNG, with a guard that sanitizes fetched pages and **flags** prompt-injection patterns (it never claims content is safe). | `villa install --web-search` (persists the gate) |
| **Resident set** | Holds several models loaded at once, each on its own loopback port, instead of restarting inference to swap between them. | `villa model resident ls` / `add` |
| **Workspace agent** | Runs one instruction against one folder you registered, inside a per-task microVM that reaches the served model and nothing else, asking before it writes. | `villa install --workspace-agent`, then `villa workspace add` and `villa work`. See [Running a task on your own files](#running-a-task-on-your-own-files) |
| **Backup & restore** | The whole workspace (config, Open WebUI data, the usage store, and the bench store) to one local `.tar`, and back transactionally. | `villa backup` / `villa restore <archive>` |

### Keeping it current

`villa update` moves the digest pins the stack runs, one subsystem at a time,
proving each **before and after** it changes it. It is on-command only: nothing
polls, and `status` and `doctor` show the last recorded check and its age rather
than triggering a new one.

```bash
villa update --check      # read-only; works on a stopped stack
villa update --dry-run    # the ordered plan, the download total and the snapshot disk
villa update              # apply, halting on the first failure
```

Chat and memory keep their state in a data volume, so those two are stopped while
their data is copied before they are changed, and a rollback restores that data
alongside the pin; `--dry-run` states the disk this needs before it is spent.

Until a signed pin manifest is published, `--check` honestly reports that it
**could not check** and the stack runs the pins compiled into the binary. That is
deliberately not the same as reporting you are up to date. See
[RELEASING.md](RELEASING.md).

### Proving it, rather than trusting it

The privacy claims are runtime-asserted, not install-time assumptions. Each of
these drives the real path and fails honestly: an unevaluable result is a
failure, never a false green:

```bash
./villa doctor           # is this installed stack still healthy?
./villa verify memory    # the RAG path retrieves and cites with ZERO outbound
./villa verify search    # web-search outbound is BOUNDED to the sanctioned allowlist
./villa verify agent     # the coding agent runs with no silent cloud fallback
```

`villa verify memory` and `villa verify search` are negative-control-first: they
require host egress to be blocked for the run, and a control that cannot fail
proves nothing. See [MEMORY.md](MEMORY.md#proving-zero-outbound-with-villa-verify-memory).
