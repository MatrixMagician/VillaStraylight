# VillaStraylight

A single Go CLI (`villa`) that stands up a private, local AI workspace on your own hardware — auto-detecting an AMD Strix Halo (gfx1151) Fedora host, recommending a memory-fitting model/quant/context, generating rootless Podman Quadlet units, and orchestrating llama.cpp (ROCm) inference plus an Open WebUI chat front-end behind a loopback-only control dashboard. Inference is local, and there is zero telemetry: outbound happens only when you run a command that fetches something (a model pull, an image pull, an update check), never on a timer and never in the background.

![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)

VillaStraylight is for privacy-conscious power users who want a ChatGPT/Claude-class experience that runs entirely on their own machine, with no data leaving the box. `villa` is the **control plane only** — the AI services (llama.cpp `llama-server`, Open WebUI, the optional Qdrant + local-embeddings memory stack, and the optional SearXNG web-search service) are integrated OSS components, not rebuilt; the optional coding agent is the pinned, checksum-verified Crush binary.

> Status: **v1.8 shipped** (tag `v1.8`). Built milestone by milestone, each addition keeping the zero-telemetry, loopback-only posture:
> - **v1.0** — Vulkan-only MVP: detect → recommend → install → Open WebUI chat → control dashboard.
> - **v1.1** — the **ROCm/HIP backend** with a transactional `backend set` switch and an honest A/B `bench`. (ROCm shipped opt-in here and became the default in a later revision; Vulkan RADV remains selectable as the fallback.)
> - **v1.2** — operability: `villa doctor`, saved bench `--compare`, cumulative usage, `backup`/`restore`, and a guided TUI install.
> - **v1.3** — strictly-local **memory & knowledge**: a Qdrant + local-embeddings stack wired into Open WebUI Memory/RAG, plus conversational `recall`.
> - **v1.4** — an optional strictly-local **terminal coding agent** (Crush) wired over loopback to a fit-guarded coding model, proven zero-outbound at runtime.
> - **v1.5** — opt-in **web-search grounding**: a containerized SearXNG wired into Open WebUI's native web search, with grounded fetch → embed → cite through an SSRF-guarded, injection-guarding `villa-websafe` loader. Strictly opt-in / default-OFF (byte-identical to v1.4 when disabled); outbound is provably bounded (`villa verify search`) and prompt injection is reduced-and-flagged, never claimed eliminated.
> - **v1.6** — **structural consolidation, and a transactional install.** The residency proof, the Open WebUI protocol, the subsystem gates, the verify shape and install's decisions each became one module instead of being re-implemented per caller. The behavioural change: `villa install` now captures before mutating and rolls back on failure (ADR-0003), so a failed install no longer leaves a running-but-unproven stack — the one stack-mutating flow that was outside the capture-prove-rollback discipline. Config-load seams that silently defaulted now fail closed.
> - **v1.7** — **the resident set, a lint gate that can fail, and docs that match the tree.** `villa model resident` holds several models loaded at once, each on its own loopback port, so switching between them costs no cold load — the alternative to `model swap` rather than a variant of it, fit-guarded across the whole set and transactional (ADR-0003). The dashboard was rebuilt around a status strip. `make lint` was repaired: the old target ran its `go vet` fallback when the LINT failed, so the gate could never fail and misreported why; the linter is now pinned in one place and the standing backlog is 0. Every document was audited against the tree — the claims that had quietly stopped being true are gone, the enumerations that go stale now point at their authority, and a docs grep-gate fails the build on a dead reference or a retired claim.
> - **v1.8** — **`villa update`: the transactional check → fetch → prove → prune lifecycle.** Every pin was a compile-time constant, so "a newer version exists" had no representation anywhere in the tree — and eight of ten pins had already drifted. A pin is now two values: the VETTED pin villa shipped and the EFFECTIVE pin this host runs. New pins arrive in an ed25519-signed manifest that may supply values only, never a new component, registry or URL, and that is refused below a serial it has already seen. Applying proves each subsystem **before and after** it changes it, one subsystem at a time: a pre-existing failure is a refusal rather than an update failure villa did not cause, and an unprovable new image rolls back with villa saying it *may* be fine rather than claiming it is broken. A live incident then proved that for chat and memory the **image is not the state being changed** — their data is — so those two are stopped while their volume is snapshotted, and a rollback restores the data alongside the pin.

## Requirements

- **Go 1.26+** — required to build the `villa` binary (see `go.mod`).
- **Fedora Workstation 44+** on **AMD Strix Halo (gfx1151)** — the only supported host platform for v1. The architecture leaves room for a future macOS/Apple-Silicon backend, but it is not yet implemented.
- **Podman v5 (rootless)** with the user socket enabled (`systemctl --user enable --now podman.socket`) — `villa` drives the AI stack through rootless Podman Quadlet/systemd units, not Docker.
- A **ROCm/HIP** capable GPU stack for the default inference backend, with `HSA_OVERRIDE_GFX_VERSION=11.5.1` in the runtime environment — the ROCm preflight gate refuses bring-up if it is absent. ROCm also expects a kernel and `linux-firmware` at or above the pinned floors (`villa preflight --backend rocm` reports both).
- A **Vulkan RADV** capable GPU stack (Mesa) for the fallback backend. Keep it installed even on ROCm: it is the safe landing spot `villa backend set vulkan` switches to, and `villa recommend` recommends it automatically when the host is confidently not ROCm-ready.

`villa preflight` checks these host requirements (Vulkan ICD + iGPU enumeration, Podman rootless readiness, SELinux/linger state, and disk/memory floors) and tells you what is missing before you install anything.

## Prerequisites: Kernel Parameters

VillaStraylight targets AMD Strix Halo's **unified memory** — the CPU and iGPU share one physical RAM pool. To let llama.cpp offload large models onto the iGPU, the host kernel must allow the GPU to address that pool dynamically.

### Why custom kernel parameters?

Many guides statically partition memory between the CPU and iGPU (e.g. locking 32 GB for video). That is a waste. With **unified dynamic memory**, the GPU can access nearly all system RAM (up to ~124 GB) on demand, while keeping the flexibility to use it for the CPU when needed.

> **Performance note:** Benchmarking by Lars Urban shows a **5–12% performance increase** from setting `amd_iommu=off` instead of the previously recommended pass-through mode.

### Apply the parameters

Add the parameters to `GRUB_CMDLINE_LINUX` in `/etc/default/grub`, then regenerate the GRUB config and reboot:

```bash
# Edit GRUB_CMDLINE_LINUX in /etc/default/grub
sudo vim /etc/default/grub
#   GRUB_CMDLINE_LINUX="... amd_iommu=off amdgpu.gttsize=126976 ttm.pages_limit=32505856"

# Regenerate the GRUB config, then reboot for the parameters to take effect
sudo grub2-mkconfig -o /boot/grub2/grub.cfg
sudo reboot
```

| Parameter | How it enables unified memory |
|-----------|-------------------------------|
| `amd_iommu=off` | **Disable AMD IOMMU** entirely, which can improve GPU memory-access performance on Strix Halo unified-memory setups. |
| `amdgpu.gttsize=126976` | **GTT size (Graphics Translation Table):** explicitly sets the maximum unified memory addressable by the GPU to ~124 GB (126976 MB), overriding default driver limits. |
| `ttm.pages_limit=32505856` | **Pinned-memory limit:** allows the TTM (Translation Table Manager) to pin up to ~124 GB of pages in high-speed system RAM, ensuring the GPU has direct access without swapping. |

After rebooting, `villa detect` and `villa recommend` report the enlarged GTT envelope (the usable memory pool is the GTT total, `mem_info_gtt_total`), so the recommended model/quant/context can fit the full unified-memory budget.

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
                                      # (recommend reports the backend and an honesty-bounded
                                      #  ROCm hint; the recommended backend is rocm, falling
                                      #  back to vulkan only on a confidently not-ROCm-ready host)
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

**Hold several models loaded at once:**

```bash
villa model resident ls               # the primary plus every resident slot: port, unit, state
villa model resident add <name>       # fit-guard the whole set, allocate a loopback port,
                                      # auto-pull, render one more unit, start it
villa model resident rm <name>        # drop the slot, regenerate, stop the orphaned unit
```

A resident slot is a second `llama-server` kept loaded on its own host loopback port, so
switching between models in the chat UI costs no cold load — the alternative to `model
swap`, not a variant of it. `add` sizes the candidate with the same fit math `villa
recommend` uses, asks whether the whole set still fits the envelope, and **refuses with a
remediation** when it does not: nothing is written, downloaded, or started on a refusal.
Both verbs are transactional (ADR-0003) and the chat UI lists every resident endpoint
alongside the primary. See [CONFIGURATION.md](docs/CONFIGURATION.md#the-resident-set).

**Switch and benchmark the inference backend (v1.1):**

```bash
villa backend show                    # active backend (from config) + resolved digest-pinned image tag
villa backend set vulkan              # transactional switch to the fallback backend: re-fit-guard,
                                      # restart, prove GPU residency in a bounded timeout —
                                      # rolls back verbatim on any failure
villa backend set rocm --dry-run      # preview target/fit/ROCm preflight without mutating anything
villa bench                           # honest throughput of the running backend: separate
                                      # prompt-processing (pp) and token-generation (tg) tok/s
villa bench --ab                      # also flip to the other backend, bench it identically,
                                      # restore the original, and report the per-metric A/B delta
```

`villa backend set <rocm|rocm-6.4.4|rocm-6.4.4-rocwmma|vulkan>` is transactional (capture → mutate → prove → rollback): a failed switch is a no-op to the running stack. ROCm 7.2.4 is the default; Vulkan RADV is the fallback and is always a safe target. `villa bench` flags include `--reps`/`-n` (counted runs per side, default 5), `--warmup` (discarded warm-up runs, default 1), and `--n-predict` (fixed `max_tokens` per run, default 128).

**Diagnose, measure, back up (v1.2):**

```bash
villa doctor                          # one-shot read-only health diagnosis (preflight + status +
                                      # GPU-residency proof + config-vs-disk drift; 0/1/2 exit)
villa bench --compare                 # compare saved bench reports — per-metric deltas, comparability-guarded
villa bench --list                    # list saved bench reports
villa backup [archive]                # self-describing local archive (config + Open WebUI/Qdrant volumes +
                                      # usage + bench; model weights EXCLUDED, identities recorded)
villa restore <archive>               # transactional restore (capture → quiesce → import → prove → rollback)
```

**Strictly-local memory & knowledge (v1.3):**

```bash
villa recall index                    # semantically index past chats into an Open WebUI Knowledge collection
villa recall status                   # show recall-index freshness (typed-Unknown when unevaluable, never silently stale)
villa verify memory                   # negative-control-first runtime proof: a real upload→cited answer with zero outbound
```

The memory stack — a digest-pinned Qdrant plus a dedicated local-embeddings `llama-server`, wired into Open WebUI's native Memory/RAG by environment only — is an optional addon enabled during `villa install`. It adds **zero new outbound**; `villa verify memory` proves that under a real egress block.

**Local coding agent (v1.4):**

```bash
villa install --coding-agent          # optional addon: pin + SHA-256-verify the Crush binary, pre-stage the
                                      # recommended coder GGUF, render crush.json (kill switches, loopback-only)
villa coding-mode enter               # transactionally swap the running stack into a tool-calling coding mode
villa coding-mode exit                # restore the chat model (explicit verb — coding mode never auto-flips)
villa code                            # launch the locked-down Crush agent (telemetry/autoupdate killed) over loopback
villa verify agent                    # negative-control-first proof of zero outbound + no silent cloud fallback
```

`villa recommend` also computes a coder fit at agent-profile context and reports an honest residency mode (`swap` or `shared`); the coder is qualified agent-in-the-loop on the gfx1151 box before it ships in the catalog. Codebase memory is agent-native (LSP + ripgrep + `AGENTS.md`/`CRUSH.md` context files), not a vector index.

**Opt-in web-search grounding (v1.5):**

```bash
villa install --web-search             # opt into the web-search addon: render the SearXNG search service + the
                                       # SSRF-guarded villa-websafe loader, generate their secrets, wire Open
                                       # WebUI's native web search, and prove SearXNG readiness with a real
                                       # format=json query. Persists web_search_enabled=true (default off), so a
                                       # later bare `villa install` keeps it on.
villa verify search                    # negative-control-first, inverse-framed proof that outbound is BOUNDED
                                       # under a real egress block (an ineffective block is REJECTED, never a
                                       # fabricated PASS); also asserts planted injections are stripped+fenced+flagged
```

Web search is **strictly opt-in and default-OFF** — with it disabled the install renders byte-identical to v1.4 and the zero-outbound posture is unchanged. When enabled, a query reaches SearXNG's upstream engines and result pages are fetched, so outbound is no longer zero; that outbound is **bounded and provable** (`villa verify search`) and surfaced honestly in `villa status`/`doctor`/the dashboard (the outbound-bounded indicator derives from the real verify result, never a config flag). Fetched pages flow through `villa-websafe` — the sole producer of Open WebUI `page_content` — which SSRF-guards every fetch and runs an injection-guard pass (sanitize → Unicode-normalize → provenance-fence → heuristic classify). The guard **reduces and flags** prompt injection; it is never claimed to eliminate it, and the browser-side markdown-image exfiltration channel is a documented residual, not closed.

**Run the stack lifecycle:**

```bash
villa up [service]                    # reconcile config into units and start (whole stack or one service)
villa status                          # aggregated health: unit + container + /health + GPU-offload proof,
                                      # plus the active backend and its resolved image tag
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

## Keeping the stack current

Every component the stack runs is pinned by digest — the backend images, Open
WebUI, Qdrant, the embedder, SearXNG, the websafe base, and the checksum-verified
Crush binary. `villa update` is the transactional verb that moves those pins
forward.

```bash
villa update --check                  # read-only: what is current, what has moved; works on a stopped stack
villa update --dry-run                # the ordered plan, the download total and the snapshot disk; changes nothing
villa update                          # apply, one subsystem at a time, each proven before it commits
villa update <subsystem>              # apply to one of: inference, chat, memory, search, agent
```

Each subsystem is proven **before and after** it changes: villa refuses to start
against an already-unhealthy subsystem (that is a refusal, not an update failure),
and rolls back verbatim if the new version cannot be proven. An unprovable
component is not treated as a broken one — villa says the new version *may* be
fine and that it cannot show that it is, then restores what was running. One
known-good previous is retained per subsystem so a rollback has somewhere to land;
see [ADR-0004](docs/adr/0004-villa-update-prunes-images-that-install-never-would.md).

**Chat and memory keep their state in a data volume, so for them the image is not
the thing being changed.** An update can migrate a schema forward, after which the
old image can no longer read it — so those two are stopped, their volume is
exported, and only then are they mutated. The snapshot is part of the rollback
target: a rollback restores the data alongside the pin, and then re-proves. A
volume villa could not snapshot is not updated at all, because mutating data with
nothing to go back to is the failure this exists to prevent. `--dry-run` states the
disk each snapshot needs before it is spent.

### What update sends, in two parts

These are two different operations with two different footprints, so they are
stated separately rather than averaged into one comfortable sentence.

**The check** is one HTTPS GET to this project's release endpoint, made by `villa`
itself, to fetch a signed manifest of current pins. No request body, no cookies,
no credentials, no identifier — nothing that describes your host, your models, or
your usage. If the manifest is absent, expired, or fails its signature check,
villa reports that it **could not check** and falls back to the pins compiled into
the binary. That is deliberately not the same as reporting that you are up to
date, and villa will not say the latter when it means the former.

**The fetch** is `podman pull` against the registries named in the compiled-in
allowlist — gigabytes, several hosts, and performed by Podman on villa's
instruction rather than by villa itself. What bounds it is the allowlist: a
manifest may supply new *values* for components villa already knows, and can never
introduce a component, a registry host, or a URL template. Villa can promise the
set of hosts it will contact; it cannot promise the volume, and does not pretend
the fetch is as small as the check.

**Neither happens unless you ask.** There is no timer, and no command checks
opportunistically on your behalf — `villa status`, `villa doctor` and the
dashboard display the *last recorded* check and its age, and never trigger a new
one. The trade is deliberate and has a cost worth knowing: villa will not tell you
an update exists unless you run the command, so "last checked 146 days ago" is a
prompt you have to act on yourself.

`villa update --check --from-registries` asks each registry directly instead of
using the manifest. It is opt-in because it contacts one endpoint per installed
component, which reveals to those registries which addons you have enabled. The
manifest check does not.

`villa` itself is reported but never self-applied: `--check` will tell you a newer
release exists and print the command to install it, and will not replace the
binary it is running from.

## Configuration

`villa` reads a single TOML config at `$XDG_CONFIG_HOME/villa/config.toml` (typically `~/.config/villa/config.toml`), written with `0600` permissions. When the file is absent, `villa` uses typed defaults — it is read-only by default and only writes config via `villa recommend --save`, `villa config set`, or `villa install`.

Key fields (`internal/config/villaconfig.go`):

| Key | Default | Description |
|-----|---------|-------------|
| `model` | (from `recommend`) | Chosen catalog model id. |
| `quant` | (from `recommend`) | Chosen quantization (e.g. `UD-Q4_K_M`). |
| `ctx` | (from `recommend`) | Context length in tokens. |
| `backend` | `rocm` | Inference backend: `rocm` (ROCm 7.2.4, default for gfx1151), `rocm-6.4.4`, `rocm-6.4.4-rocwmma`, or the `vulkan` (RADV) fallback. Switch it transactionally with `villa backend set` — `villa config set backend=` only accepts `vulkan`, since every ROCm target must pass the bring-up gate. |
| `catalog_path` | (embedded) | Optional path to an external catalog JSON override. |
| `dashboard_port` | `8888` | Host port the control dashboard listens on. |
| `chat_port` | `3000` | Host port Open WebUI is published on (the dashboard's chat link target). |

When the optional memory (v1.3), coding-agent (v1.4), and web-search (v1.5) addons are enabled, `villa install` persists their own append-only fields into the same `config.toml`, which stays the single source of truth — the rendered Quadlet units, `crush.json`, and the SearXNG `settings.yml` are regenerated from it, never hand-edited. Web search keys off the deliberate `web_search_enabled` bool (default false, never self-healed on) — set it with `villa install --web-search` (which persists the gate). With it off, every field is omitted and the render is byte-identical to v1.4. When on, `villa install` generates the SearXNG `secret_key` and the `villa-websafe` bearer via `crypto/rand` into `0600` files (never logged, never in a `0644` unit).

Inspect or change config with `villa config show` and `villa config set key=value`.

## Development

Common tasks are wired into the `Makefile`:

```bash
make run        # run the villa CLI via `go run ./cmd/villa`
make build      # build ./villa (version-stamped)
make build-static # the CGO-free static build CI enforces (SC#4)
make test       # go test ./...
make test-race  # the race gate (CR-01): CGO_ENABLED=1 go test -race ./...
make vet        # go vet ./...
make fmt        # gofmt -w .
make lint       # golangci-lint at the version pinned in .golangci-version,
                # fetched via `go run` — nothing needs to be on PATH. Reports this
                # branch's NEW issues; LINT_ALL=1 lints the whole tree.
make check      # vet + test + test-race (the pre-commit gate)
make tidy       # go mod tidy
make clean      # remove build artifacts
```

The CLI entry point is `cmd/villa/main.go`; the control-plane libraries live under `internal/` — `ls internal/` is the list, and the code map in [CLAUDE.md](CLAUDE.md) says what each one owns in a line. [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) carries the layering, and [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) the build/test/lint loop in full.

## License

MIT — see [LICENSE](LICENSE).
