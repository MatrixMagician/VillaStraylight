<!-- generated-by: gsd-doc-writer -->
# Architecture

## System overview

VillaStraylight is a single static Go CLI — `villa` (`cmd/villa`) — that acts as the
**control plane** for a strictly-local AI stack on an AMD Strix Halo (gfx1151) /
Fedora host. It detects the host hardware, recommends a memory-fitting
model/quant/context from a versioned catalog, gates installs behind a host-readiness
preflight, renders rootless **Podman Quadlet** units from a single config source of
truth, and orchestrates two integrated OSS containers — **llama.cpp `llama-server`**
(OpenAI-compatible inference, **Vulkan RADV by default with an opt-in ROCm 7.2.4
backend**) and **Open WebUI** (chat) — plus a native, loopback-only Go **control
dashboard**. The Go code is the orchestrator only; the AI services are integrated
upstream images, never rebuilt. The architectural style is a **layered pipeline of pure
cores behind injectable host seams**: every package that makes a decision (`detect`,
`recommend`, `preflight`, `inference.BackendFor`, `orchestrate.Render`,
`orchestrate.Reconcile`, `status`, `modelswap`, `backendswap`, `bench`) is a pure,
table-testable library that returns a typed value; all host-touching effects (sysfs
reads, `podman`, `systemctl`, HTTP probes, downloads, file writes) are injected as
function seams or confined to a small number of clearly-marked impure files. The command
layer (`cmd/villa`) is the only place that prints, maps verdicts to exit codes, and
calls `os.Exit`.

As of **v1.1**, backend choice is a first-class, polymorphic seam. A single resolver,
`inference.BackendFor(cfg.Backend)`, maps the persisted `backend` string
(`""`/`"vulkan"` → Vulkan RADV, `"rocm"` → ROCm 7.2.4) to a `Backend` implementation
and is the **only** place a concrete backend is chosen. It **fails closed**: an
unknown/typo'd value returns an actionable error rather than silently defaulting to a
privileged backend. Every inference call site depends on the `Backend` interface, so
flipping `backend = "rocm"` in `config.toml` re-routes image, device-passthrough args,
runtime flags, and offload-residency markers with no other change.

A defining cross-cutting contract is **typed-Unknown**: every detected signal is a
`detect.Bytes`/`detect.Bool`/`detect.Int`/`detect.Str` carrying a `Known` flag and
provenance, so "could not measure" is always distinct from a legitimate zero. This
propagates into the PASS / WARN / FAIL verdict vocabulary shared by `preflight`,
`inference`, `status`, `backendswap`, and `bench` — an unevaluable check degrades to
WARN rather than a false pass or a false hard-block. A defining v1.1 guarantee is that
this verdict is **backend-neutral**: offload-assert keys on the active backend's
`ResidencyProof()` markers, so CPU fallback is positively classified FAIL regardless of
backend — never a false-green.

## Component diagram

```mermaid
graph TD
    CLI["cmd/villa (cobra CLI)<br/>detect · recommend · preflight · model · install<br/>up · down · restart · logs · status · dashboard<br/>backend · bench · uninstall"]

    CLI --> detect["internal/detect<br/>HostProfile probe (typed-Unknown)<br/>+ ROCm readiness"]
    CLI --> catalog["internal/catalog<br/>embedded model catalog + fit dims"]
    CLI --> recommend["internal/recommend<br/>Pick: memory-fitting model (pure)"]
    CLI --> preflight["internal/preflight<br/>host-readiness checks (pure)<br/>+ ROCm gate (rocm-policy.json)"]
    CLI --> download["internal/download<br/>verified resumable GGUF pull"]
    CLI --> config["internal/config<br/>config.toml store (0600)"]
    CLI --> orchestrate["internal/orchestrate<br/>Render → Reconcile → WriteUnits + systemd seam"]
    CLI --> modelswap["internal/modelswap<br/>guarded swap core"]
    CLI --> backendswap["internal/backendswap<br/>transactional capture→prove→rollback"]
    CLI --> bench["internal/bench<br/>honest A/B core (pure)"]
    CLI --> status["internal/status<br/>read-model aggregation"]
    CLI --> dashboard["internal/dashboard<br/>loopback chi dashboard"]

    recommend --> detect
    recommend --> catalog
    preflight --> detect
    CLI --> resolver["inference.BackendFor(name)<br/>single fail-closed resolver"]
    resolver --> bvk["backendVulkan<br/>(RADV, default)"]
    resolver --> brc["backendROCm<br/>(7.2.4, opt-in)"]
    bvk -.implements.-> inference["internal/inference<br/>Backend / Runner / ResidencyProof seam"]
    brc -.implements.-> inference
    orchestrate --> inference
    orchestrate --> config
    modelswap --> catalog
    modelswap --> config
    modelswap --> download
    modelswap --> orchestrate
    backendswap -.cmd composes.-> orchestrate
    backendswap -.cmd composes.-> config
    bench -.--ab composes.-> backendswap
    status --> orchestrate
    status --> inference
    status --> detect
    dashboard --> status
    dashboard --> modelswap
    dashboard --> metrics["internal/metrics<br/>llama-server /metrics + /slots scrape"]

    orchestrate -.writes.-> quadlet["~/.config/containers/systemd<br/>*.container/.network/.volume"]
    orchestrate -.systemctl --user.-> systemd["rootless user systemd"]
    inference -.podman run.-> llama["llama-server (Vulkan/ROCm, gfx1151)"]
    systemd -.-> owui["Open WebUI"]
    systemd -.-> dashsvc["villa dashboard service"]
```

Note on the GPU backend seam: backend selection is funneled through the single
`inference.BackendFor(name)` resolver (`internal/inference/backend.go`), which is
fail-closed — every other site depends on the `Backend` interface, never on a concrete
`backendVulkan`/`backendROCm`. The only files permitted to hold imperative backend
literals (the container image digest, `/dev/dri` device passthrough, `--group-add
keep-groups`, the loopback publish, the mandatory `llama-server` flags, and the ROCm
`HSA_OVERRIDE_GFX_VERSION` override) are the per-backend implementations
`internal/inference/backend_vulkan.go` and `internal/inference/backend_rocm.go`, plus
the AMD detection seam `internal/detect/gpu_amd.go`. A future Metal backend slots in as
a third sibling `Backend` implementation behind `BackendFor` without touching callers.

## Data flow

The canonical flow is the `villa install` lifecycle (`cmd/villa/install.go`,
`runInstall`), which composes the pure cores in order:

1. **Detect** — `detect.Probe()` reads `/proc/meminfo`, `/sys/class/drm`,
   `/sys/module/ttm/parameters/pages_limit`, `/proc/sys/kernel/osrelease`, and the AMD
   GPU seam to assemble a `detect.HostProfile` (CPU/arch, iGPU name + gfx id, Vulkan
   ICD, DRI nodes, ROCm presence, total RAM, and the usable GTT/unified-memory
   envelope). It never errors and never panics — every field degrades to a typed
   Unknown.
2. **Recommend** — `recommend.Pick(profile, catalog, overrides)` (a pure function)
   chooses the largest auto-eligible model whose `weight_bytes + kv_cache@ctx +
   headroom ≤ usable_envelope`. It skips bootstrap and `unified_memory_safe:false`
   entries, defaults the backend to `vulkan`, re-validates manual overrides, and
   degrades to a conservative RAM-fraction floor (or refuses) when the envelope is
   Unknown. Every term of the fit inequality is surfaced on the `Recommendation`.
3. **Preflight gate** — `preflight.RunWithResources(profile, req)` runs the
   host-readiness checks against the concrete model's `weights + KV + headroom`
   requirement: Vulkan iGPU present, podman rootless ready, user lingering, SELinux
   container-device boolean, free disk/memory floors, kernel and firmware baselines.
   Each check is BLOCK- or WARN-tier; a BLOCK-tier check that cannot be evaluated
   downgrades to WARN. BLOCK gaps are offered as **per-step consented privileged
   host-prep** (`setsebool`, `loginctl enable-linger`) — never run silently —
   overridable with `--force`.
4. **Ensure model + persist config** — the recommended GGUF is auto-pulled if absent
   via `internal/download` (HEAD-verify → resumable `.part` → per-shard SHA256 →
   atomic rename), then the chosen `model/quant/ctx/backend` plus the
   dashboard/chat defaults are written to `config.toml` via the 0600 traversal-guarded
   `config.SaveVilla` — **before** any unit work, so config is the single source of
   truth.
5. **Render** — `orchestrate.Render(RenderInput)` (pure) builds five Quadlet units —
   the inference `.container`, `villa.network`, `villa-models.volume`, and the Open
   WebUI `.container` + `.volume`. The inference unit's imperative fields are obtained
   **through** `Backend.Image()` and `Backend.ContainerArgs(spec)` and mapped to
   Quadlet keys, never re-typed.
6. **Reconcile** — `orchestrate.Reconcile(units, unitDir)` (pure) does a sha256
   render-vs-disk diff, classifying each unit Changed or Unchanged. An empty Changed
   slice is a true no-op — the idempotency core.
7. **Write + start** — `orchestrate.WriteUnits` writes each changed unit atomically
   (sibling temp + fsync + `os.Rename`, refusing any path outside the unit dir), then
   the `orchestrate.Systemd` seam runs `systemctl --user daemon-reload`, starts
   `villa-llama.service`, then `villa-openwebui.service`. The native control-dashboard
   unit is reconciled separately into `~/.config/systemd/user`, enabled for
   boot-survival, and started.
8. **Readiness poll** — the command layer polls the loopback inference endpoint's
   `/health` (503 = still loading → keep polling; timeout → WARN, never a confident
   FAIL), then prints the inference endpoint, the chat URL, and the dashboard URL.

The day-to-day verbs (`up`/`down`/`restart`/`logs` in `cmd/villa/lifecycle.go`) reuse
the same Render→Reconcile→WriteUnits→Systemd core, so hand-editing `config.toml` and
re-running `up`/`restart` converges exactly the changed units. `villa status`
(`internal/status`) and the dashboard (`internal/dashboard`) fold the **same** status
read-model — never a fork — to report per-service active state, mapped `/health`, and
the running-server GPU-offload verdict (keyed on the active backend's residency markers),
with a worst-wins overall PASS/WARN/FAIL.

A second v1.1 flow is the **transactional backend switch** (`villa backend set
<vulkan|rocm>`, `cmd/villa/backend.go`), driven by the pure `backendswap.Run(Deps,
target)` state machine (`internal/backendswap/backendswap.go`). It clones the proven
`modelswap` forward skeleton (fit-guard first, persist-before-unit-work,
restart-inference-only) and wraps it in a transactional frame:

1. **Capture** — the verbatim prior `villa-llama.container` bytes and prior
   `VillaConfig` are snapshotted **strictly before** any mutation.
2. **Fit + ROCm preflight guard** — the preserved model is re-checked against the
   target backend's envelope, and (for ROCm) the `preflight.RunROCm*` bring-up gate
   runs; a refuse-with-remediation aborts with zero mutation.
3. **Mutate** — persist the new backend to config, then reconcile/write the inference
   unit and restart only the inference service.
4. **Prove** — the cutover is gated on an injected `Prove` verdict: a real
   generation-probe **and** a positive residency proof over the now-running server
   (`inference.PollHealth` + `GenerationProbe` + `RunningOffloadVerdict`).
5. **Rollback** — any mutate error or non-pass verdict restores the verbatim captured
   unit + config and re-readies best-effort, so a failed or degraded switch is a no-op
   to the running stack.

`backendswap` is deliberately literal-free of backend marker tokens and imports neither
`internal/inference` nor `internal/detect`; the prove verdict is a local value type
(`ProveVerdict`/`ProveStatusPass`) and the real markers arrive only through the injected
`Prove` seam wired in `cmd/villa`.

The **honest A/B benchmark** (`villa bench [--ab]`, `cmd/villa/bench.go`) is the pure
`bench.Run(ctx, Deps, BenchSpec)` core (`internal/bench/bench.go`). Without `--ab` it
measures the current backend non-disruptively; with `--ab` it **composes**
`backendswap.Run` to flip backends rather than re-implementing any switching — the
switching logic stays locked in the one transactional core. Each measured run is gated
on a residency proof (`RunningOffloadVerdict`) so only runs that genuinely executed on
the GPU count toward the median/stddev `Stats` and the comparative `ABResult`.

## Key abstractions

- **`detect.HostProfile`** + the typed `Bytes`/`Bool`/`Int`/`Str` optionals
  (`internal/detect/detect.go`, `internal/detect/value.go`) — the read-only host
  description and the typed-Unknown spine that every downstream decision consumes.
- **`recommend.Pick`** (`internal/recommend/recommend.go`) — the pure
  memory-fit selector; returns a `Recommendation` exposing every term of
  `weight + KV + headroom ≤ envelope`.
- **`preflight.CheckResult` / `preflight.Run` / `RunWithResources`**
  (`internal/preflight/preflight.go`) — the reusable host-readiness gate; pure,
  returns typed BLOCK/WARN-tier PASS/WARN/FAIL results with remediation + provenance.
- **`inference.BackendFor`** (`internal/inference/backend.go`) — the single,
  fail-closed polymorphism point mapping the config `backend` string to a `Backend`.
  `""`/`"vulkan"` → `backendVulkan`, `"rocm"` → `backendROCm`; any other value is an
  error, never a silent default. Every backend call site routes through here.
- **`inference.Backend`** and **`inference.Runner`** (`internal/inference/inference.go`)
  — the backend-neutral seam. `Backend` (`Name`/`Image`/`ContainerArgs`/`ResidencyProof`)
  and `Runner` (start/stop/health/endpoint/logs) isolate every GPU/podman literal; the
  Vulkan RADV implementation lives in `backend_vulkan.go`, the opt-in ROCm 7.2.4
  implementation in `backend_rocm.go`, the podman runner in `runner_podman.go`.
- **`inference.ResidencyMarkers`** (`internal/inference/inference.go`) — the
  backend-owned, log-shape-only descriptor returned by `Backend.ResidencyProof()`. It
  carries only marker literals (device token e.g. `Vulkan0`/`ROCm0`, device label,
  start-log prefix, fault string, software-renderer-reject flag), so both offload-assert
  scrape paths (`offload.go`, `running_offload.go`) are parameterized by it — a new
  backend supplies its own markers without re-rolling the offload math.
- **`inference.Verdict`** + `RunningOffloadVerdict` (`internal/inference/inference.go`,
  `running_offload.go`) — the dual-assert GPU-offload result (log-scrape keyed on
  `ResidencyMarkers` + sysfs GTT delta) that catches silent CPU fallback; the shared,
  backend-neutral PASS/WARN/FAIL value the CLI, dashboard, `backendswap`, and `bench`
  render.
- **`orchestrate.Render` / `Reconcile` / `WriteUnits` / `Systemd`**
  (`internal/orchestrate/render.go`, `reconcile.go`, `systemd.go`) — the pure Quadlet
  renderer, the sha256 idempotency reconciler, the atomic unit writer, and the
  fixed-arg `systemctl`/`loginctl`/`journalctl` lifecycle seam.
- **`config.VillaConfig`** + `LoadVilla`/`SaveVilla` (`internal/config/villaconfig.go`)
  — the persisted `config.toml` selection (model/quant/ctx/backend + dashboard/chat
  ports), written 0600 with a path-traversal guard; the single source of truth the
  units render from.
- **`catalog.Catalog` / `CatalogModel`** (`internal/catalog/catalog.go`) — the embedded,
  schema-versioned model catalog carrying the per-model KV-fit dimensions, the
  `unified_memory_safe` flag, and per-shard download metadata.
- **`status.Report` / `status.Run` / `status.Aggregate`** (`internal/status/status.go`)
  — the JSON-neutral read-model the CLI and dashboard share; folds per-service
  active/health/offload into a worst-wins overall verdict and records loopback posture.
- **`preflight.RunROCm` / `RunROCmWithPolicy`** (`internal/preflight/checks_rocm.go`) —
  the opt-in ROCm bring-up gate (gfx1151 confirm, kernel/firmware floors,
  `HSA_OVERRIDE_GFX_VERSION` viability), driven by policy data in the `go:embed`-ed
  `rocm-policy.json` (`internal/preflight/floors.go`) so deny-lists are data, not code.
- **`detect.computeROCmReadiness` / `ROCmReadiness`** (`internal/detect/readiness_rocm.go`)
  — the pure, typed-Unknown ROCm-viability summary (gfx id, kernel/firmware baselines,
  override viability) surfaced by `detect` and the backend-set preflight.
- **`modelswap.Run`** (`internal/modelswap/modelswap.go`) — the guarded swap core where
  ordering is the security contract: resolve-through-catalog → fit-guard refuse →
  auto-pull → persist config → reconcile/write → restart only the inference service.
- **`backendswap.Run`** + `Deps` / `ProveVerdict` / `Result`
  (`internal/backendswap/backendswap.go`) — the pure, Deps-injected transactional
  capture→mutate→prove→rollback state machine for `villa backend set`. Imports neither
  `inference` nor `detect`; markers and the real prove verdict arrive only through the
  injected `Prove` seam wired in `cmd/villa/backend.go`.
- **`bench.Run`** + `BenchSpec` / `Stats` / `ABResult` / `Result`
  (`internal/bench/bench.go`) — the pure honest-A/B benchmark core. `--ab` composes
  `backendswap.Run` (never re-implements switching); each kept run is residency-proven so
  CPU-fallback runs are excluded from the median/stddev comparison.
- **`dashboard.Server`** (`internal/dashboard/server.go`) — the loopback-only `chi`
  control dashboard; constructed to refuse any non-loopback bind, serves a read-only
  JSON API over the shared `status` core plus the `metrics` perf scrape, with the one
  sanctioned mutation (`POST /api/models/switch`) routed through `modelswap.Run`.

## Directory structure rationale

The repository follows the standard Go `cmd/` + `internal/` split: every package under
`internal/` is a pure or seam-injected library with no CLI behavior, and `cmd/villa`
is the only consumer that prints and exits. This keeps decision logic exhaustively
table-testable and lets the same cores back both the CLI and the dashboard. The v1.1
`backendswap` and `bench` packages follow the same discipline — pure, Deps-injected
cores with their live host wiring confined to `cmd/villa`.

```
cmd/
  villa/              The cobra CLI: one file per subcommand (detect, recommend,
                      preflight, model, install, up/down/restart/logs, status,
                      dashboard, backend, bench, uninstall) plus root.go and the
                      live-wiring of each package's injectable seams.
internal/
  detect/             Host probe → typed-Unknown HostProfile; AMD GPU seam in
                      gpu_amd.go; ROCm-viability summary in readiness_rocm.go.
  catalog/            Embedded, schema-versioned model catalog + KV-fit dimensions.
  recommend/          Pure memory-fit model selector (Pick) over detect + catalog.
  preflight/          Pure, reusable host-readiness gate (BLOCK/WARN-tier checks);
                      opt-in ROCm gate driven by embedded rocm-policy.json.
  download/           Verified, resumable, per-shard-checksummed GGUF downloader.
  config/             config.toml store (0600, traversal-guarded); the source of truth.
  inference/          BackendFor resolver + Backend/Runner/ResidencyProof seam:
                      Vulkan + ROCm backends, podman runner, offload assert.
  orchestrate/        Pure Quadlet Render + sha256 Reconcile + atomic WriteUnits +
                      systemd seam; Open WebUI managed-service render path.
  modelswap/          Guarded swap core (ordering-is-the-security-contract).
  backendswap/        Transactional backend-switch core (capture→prove→rollback).
  bench/              Pure honest-A/B benchmark core (--ab composes backendswap).
  status/             Shared read-model aggregation (CLI + dashboard, never forked).
  metrics/            Bounded llama-server /metrics + /slots scrape for the perf panel.
  dashboard/          Loopback-only chi control dashboard backend + embedded UI.
  llm/                Reference-only scaffold remnant (OpenAI-compatible SSE client).
web/                  Legacy embedded React UI (reference-only).
```

The `internal/llm` and `web/` trees are remnants of an earlier exploratory scaffold
(an embedded-UI OpenAI-compatible proxy). They are superseded by the `villa` control
plane and integrated Open WebUI; they are kept as a parts bin and are not part of the
current architecture.
