<!-- refreshed: 2026-06-07 -->
# Architecture

**Analysis Date:** 2026-06-07

## System Overview

```text
┌─────────────────────────────────────────────────────────────────────────┐
│  COMMAND TIER — cmd/villa (cobra; one file per subcommand)                │
│  detect recommend preflight model inference install up/down/restart/logs  │
│  config status dashboard backend bench uninstall                          │
│  Owns: flag parsing, os.Exit codes, table/--json rendering, live*Deps     │
│  wiring. Holds NO inference-backend literal (TestSeamGrepGate walk 2).     │
└───────┬───────────────────────────────────────────────────────────────────┘
        │ injects live*Deps closures into pure cores; maps typed Result→exit
        ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  PURE CORES (no os.Exit, no print; Deps-injected host seams)              │
│  ┌──────────┐ ┌───────────┐ ┌───────────┐ ┌──────────┐ ┌──────────────┐ │
│  │ detect   │→│ recommend │→│ preflight │ │backendswap│ │ bench (A/B)  │ │
│  │ HostProf │ │ Pick()    │ │ Run()     │ │ Run() txn │ │ Run() COMPOSE│ │
│  └──────────┘ └─────┬─────┘ └───────────┘ └─────┬────┘ │ backendswap  │ │
│       catalog       │ config (source of truth)  │      └──────────────┘ │
│                     ▼                            │                        │
│  ┌──────────────────────────────────────┐       │   status (read-model)  │
│  │ inference (BACKEND-NEUTRAL SEAM)      │◄──────┘   modelswap (swap core)│
│  │ BackendFor() · Backend iface ·        │           metrics · download   │
│  │ ResidencyProof/Markers · offload/prove│                                │
│  └────────────────┬─────────────────────┘                                │
└───────────────────┼───────────────────────────────────────────────────────┘
                    │ Render (pure) + ContainerArgs()/Image() literals
                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  orchestrate (THE ONE IMPURE MODULE)                                      │
│  Render/Reconcile = PURE (sha256 diff). Only systemd.go + WriteUnits      │
│  touch the host (fixed-arg exec → systemctl/loginctl/journalctl).         │
└───────────────────┬───────────────────────────────────────────────────────┘
                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  HOST: rootless Podman Quadlet units + user systemd                       │
│  villa-llama (llama.cpp Vulkan/ROCm) · villa-openwebui · villa.network    │
│  · villa-models.volume · villa-dashboard.service                          │
└─────────────────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| Command tier | Cobra surface, flag parsing, exit codes, rendering, `live*Deps` wiring | `cmd/villa/*.go` |
| detect | Probe host → typed-Unknown `HostProfile` (CPU, memory envelope, iGPU, kernel, ROCm readiness) | `internal/detect/detect.go` |
| recommend | Pure `Pick()` → memory-fitting `Recommendation` (model/quant/ctx/backend) | `internal/recommend/recommend.go` |
| catalog | Embedded model catalog (`go:embed seed.json`) + external override w/ fallback | `internal/catalog/catalog.go`, `load.go` |
| preflight | Reusable host-prep gate → `[]CheckResult` (BLOCK/WARN tiers, fail-soft) | `internal/preflight/preflight.go` |
| inference | Backend-neutral seam: `BackendFor`, `Backend` iface, offload/residency proof | `internal/inference/*.go` |
| orchestrate | Render Quadlet units (pure) + reconcile + host-touching systemd seam | `internal/orchestrate/*.go` |
| backendswap | Transactional `villa backend set` (capture→prove→cutover→rollback) | `internal/backendswap/backendswap.go` |
| bench | Pure A/B throughput core; `--ab` composes `backendswap.Run` | `internal/bench/bench.go` |
| modelswap | Guarded `villa model swap` ordering core (shared by CLI + dashboard) | `internal/modelswap/modelswap.go` |
| status | Read-model aggregation → frozen `Report` (shared by CLI + dashboard) | `internal/status/status.go` |
| dashboard | Loopback-only chi server folding `status` core + embedded SPA | `internal/dashboard/server.go`, `api.go` |
| metrics | llama.cpp `/metrics` scrape (pp/tg timings) | `internal/metrics/llamacpp.go` |
| download | Model weight pull + shard handling | `internal/download/download.go` |
| config | Single source of truth: XDG `config.toml` load/save (`VillaConfig`) | `internal/config/villaconfig.go` |

## Pattern Overview

**Overall:** Layered, ports-and-adapters. A thin cobra command tier sits over pure `internal/` cores; every host effect (filesystem, `os/exec`, HTTP, sysfs) is hidden behind an injectable `Deps`/seam so cores are exhaustively table-testable without a live host.

**Key Characteristics:**
- **Pure cores, impure edges.** Cores never call `os.Exit` and never print. They return typed values (`Recommendation`, `[]CheckResult`, `Verdict`, `Result`, `Report`); the command tier maps those to exit codes and tables/JSON.
- **Single polymorphism point for backends.** `inference.BackendFor(name)` is the only place a backend string becomes a concrete implementation; everything else depends on the `Backend` interface.
- **Config is the single source of truth.** `config.toml` drives recommend → orchestrate; Quadlet units are regenerated from config, never hand-edited as the authority.
- **Honesty-by-construction.** Every probe degrades to a typed `Unknown` (`detect.Bool`/`detect.Bytes`) → WARN, which is DISTINCT from a confident negative → FAIL. CPU fallback is never reported as success.
- **Composition over re-implementation.** `bench --ab` composes `backendswap.Run`; `dashboard` composes `status` and `modelswap`; nothing forks a proven core.

## Layers

**Command tier (`cmd/villa/`):**
- Purpose: cobra command tree, flag parsing, exit-code mapping, rendering, `live*Deps` wiring.
- Location: `cmd/villa/*.go`, one file per subcommand (`detect.go`, `install.go`, `backend.go`, …); tree assembled in `cmd/villa/root.go` (`newRoot`), entry `cmd/villa/main.go`.
- Depends on: every pure core via `live*Deps()` closures.
- Used by: end user (the `villa` binary).

**Pure cores (`internal/*` except orchestrate):**
- Purpose: all decision logic and host-state aggregation, behind injectable seams.
- Location: `internal/detect`, `recommend`, `catalog`, `preflight`, `inference`, `backendswap`, `bench`, `modelswap`, `status`, `metrics`, `download`, `config`.
- Depends on: each other along the pipeline (recommend → catalog+detect; status → detect+inference+orchestrate); never on cobra.
- Used by: the command tier and (for status/modelswap) the dashboard.

**Impure orchestration (`internal/orchestrate/`):**
- Purpose: turn config + proven `Backend` into rootless Podman Quadlet units, reconcile against disk, write atomically, drive user systemd.
- Location: `internal/orchestrate/`. `render.go`/`reconcile.go` are PURE; only `systemd.go` and `WriteUnits` (in `render.go`) touch the host.
- Depends on: `config`, `inference`.
- Used by: install/lifecycle commands, `backendswap`, `status`.

**Host (Podman + systemd):**
- Purpose: run integrated OSS AI containers (`villa-llama`, `villa-openwebui`) plus `villa-dashboard.service`, networked over `villa.network`, models on `villa-models.volume`.

## Data Flow

### Primary install path (detect → recommend → preflight → orchestrate → systemd → proof)

1. **Detect.** `detect.Probe()` reads sysfs/`/proc`/GPU seam → typed `HostProfile` (`internal/detect/detect.go:17`).
2. **Recommend.** `recommend.Pick(profile, catalog)` → memory-fitting `Recommendation`; persisted to `config.toml` via `recommend --save` (`internal/recommend/recommend.go`).
3. **Preflight gate.** `preflight.Run(deps)` → `[]CheckResult`; BLOCK failures stop install unless `--force` (`internal/preflight/preflight.go`).
4. **Render (pure).** `orchestrate.Render(RenderInput{Backend, Cfg, ModelFile, ModelsDir})` builds `inference.RunSpec` and pulls every container literal through `Backend.Image()`/`Backend.ContainerArgs()` → `[]Unit` (`internal/orchestrate/render.go`).
5. **Reconcile (pure).** sha256 render-vs-disk diff → `Plan{Changed, Unchanged}`; empty `Changed` = true no-op idempotency (`internal/orchestrate/reconcile.go`).
6. **Write + systemd.** `WriteUnits` writes temp + `os.Rename` (atomic, traversal-guarded); `systemd.go` daemon-reloads / enables / starts user units (`internal/orchestrate/systemd.go`).
7. **Readiness + residency proof.** Command layer polls `/health` (readiness) then asserts offload via `inference` — log-scrape AND sysfs GTT-delta both required for PASS; CPU fallback = FAIL (`internal/inference/offload.go`, `prove.go`).

### Backend switch (transactional)

1. `LoadConfig`; same-backend target → clean NoOp.
2. Fit-guard FIRST: preserved model must still fit target envelope (`backendswap.Run` step 2).
3. ROCm preflight gate (target-pinned cfg) — zero side effects on refusal.
4. CAPTURE verbatim prior unit bytes + prior config BEFORE any mutation.
5. MUTATE: save config → reconcile/write → restart ONLY inference service.
6. PROVE via injected `Prove` (PollHealth + GenerationProbe + RunningOffloadVerdict); switch only on `ProveStatusPass`, else verbatim rollback (`internal/backendswap/backendswap.go:145`).

### A/B benchmark

1. Warmup run measured then DISCARDED.
2. Each measured run residency-checked via injected `Measure`; `resident==false` is VOID (excluded), never substituted as a slow pass.
3. `--ab`: identical `BenchSpec` to both sides; the flip DELEGATES to `backendswap.Run` via `Switch`/`Restore`; original backend always restored via defer registered before the flip (`internal/bench/bench.go`).

**State Management:**
- Persistent state lives in `config.toml` (the single source of truth) and on-disk Quadlet units (regenerated from config). Cores hold no global mutable state; the dashboard server guards its one cached value with a `sync` mutex.

## Key Abstractions

**`inference.Backend` (the seam interface):**
- Purpose: which GPU backend applies (image, runtime flags, device args, residency markers).
- Examples: `internal/inference/backend_vulkan.go`, `backend_rocm.go`.
- Pattern: every backend literal lives behind it; callers depend on the interface only.

**`inference.BackendFor(name)` (single polymorphism point):**
- Purpose: map a config `backend` string → `Backend`; fail-closed on unknown values.
- Examples: `internal/inference/backend.go:21`.
- Pattern: `"" | "vulkan"` → Vulkan RADV; `"rocm"` → ROCm; anything else → actionable error, NEVER silent fallback.

**`ResidencyProof()` / `ResidencyMarkers` (backend-neutral offload assert):**
- Purpose: each backend owns its log/journal marker literals (`Vulkan0`/`ROCm0`, device label, fault string); both offload scrapes are parameterized by it.
- Examples: `internal/inference/backend.go:80`, `offload.go`, `running_offload.go`.
- Pattern: a future backend slots in without re-rolling offload math; CPU fallback = FAIL, never false-green.

**`Deps` + `Run() → typed Result` (injected transactional cores):**
- Purpose: drive host-touching flows from tests without a live host.
- Examples: `internal/backendswap/backendswap.go`, `internal/bench/bench.go`, `internal/modelswap/modelswap.go`, `internal/status/status.go`.
- Pattern: every host action is a `func` field; the live wiring is a `live*Deps()` closure in `cmd/villa`.

## Entry Points

**`villa` binary:**
- Location: `cmd/villa/main.go` → `newRoot().Execute()`.
- Triggers: user CLI invocation.
- Responsibilities: build the cobra tree (`cmd/villa/root.go`), dispatch to the per-subcommand `run*` function, map returned error to exit 1.

**`villa-dashboard.service`:**
- Location: `internal/dashboard/server.go` (`NewServer`), launched as a user systemd unit (`villa-dashboard.service`).
- Triggers: `villa dashboard` / boot via systemd.
- Responsibilities: loopback-only chi server folding the shared `status` read-model + embedded SPA.

## Architectural Constraints

- **Backend literals are seam-locked.** Container image/device/`podman`/marker literals MUST live in `internal/inference/` (and `internal/detect/gpu_amd.go`). Enforced by `TestSeamGrepGate` (`internal/inference/seam_test.go`) over both `internal/` and `cmd/villa`.
- **orchestrate is the ONLY impure first-party module.** Filesystem + `os/exec` touch is confined to `internal/orchestrate/systemd.go` + `WriteUnits`. Render/Reconcile must stay pure.
- **No silent CPU fallback.** Offload assert requires BOTH log-scrape AND sysfs GTT-delta; an unevaluable signal → WARN, a confident absence → FAIL.
- **Loopback-only binds.** Dashboard binds `127.0.0.1` via `net.JoinHostPort`; never `:port`/`0.0.0.0` (PRIV-01, `internal/dashboard/server.go`).
- **No shell interpolation.** All host commands are fixed-arg `exec.Command`; model names are catalog-resolved, never shell-interpolated.
- **`--json`/dashboard contracts are byte-frozen.** Evolve append-only + bump schema version; golden tests guard them (`cmd/villa/testdata/*.json.golden`).
- **No telemetry.** First-party components emit none; outbound limited to image/model pulls (asserted in `status`).
- **Single static binary.** No Podman full-bindings dependency; Podman is controlled via fixed-arg CLI / REST-over-socket.

## Anti-Patterns

### Re-typing a backend literal in a caller

**What happens:** A command or core hardcodes an image tag, `--device /dev/dri`, a `podman` invocation, or a marker token (`ROCm0`, `HSA_OVERRIDE_GFX_VERSION`, `Memory access fault`).
**Why it's wrong:** Breaks backend-neutrality; a future ROCm/Metal/macOS backend would require editing callers; `TestSeamGrepGate` fails CI.
**Do this instead:** Obtain literals through `Backend.Image()` / `Backend.ContainerArgs()` / `ResidencyProof()` and the exported prove primitives in `internal/inference/prove.go`.

### Silently defaulting an unknown backend string

**What happens:** Coercing a typo'd `backend` value to `vulkan`.
**Why it's wrong:** Hides a misconfiguration and could select a device path the user didn't intend.
**Do this instead:** Let `inference.BackendFor` fail-closed with an actionable error and surface it (`internal/inference/backend.go:27`).

### Treating health-200 / is-active as success

**What happens:** Concluding a switch/install succeeded because the server is reachable.
**Why it's wrong:** A server can be up and 200-healthy while silently running on CPU.
**Do this instead:** Gate on the full prove verdict (readiness + generation + residency); only `ProveStatusPass` is success (`internal/backendswap/backendswap.go:249`).

### Re-implementing backend switching inside bench

**What happens:** Duplicating capture/cutover/rollback logic in the A/B path.
**Why it's wrong:** Drifts from the locked transactional contract; double the surface to audit.
**Do this instead:** Compose `backendswap.Run` through the injected `Switch`/`Restore` seams (`internal/bench/bench.go`).

### Editing a Quadlet unit as the source of truth

**What happens:** Hand-editing `villa-llama.container` instead of config.
**Why it's wrong:** The next reconcile rewrites it from `config.toml`; the edit is lost or causes drift.
**Do this instead:** Change `config.toml` and re-render (`internal/orchestrate/render.go`).

## Error Handling

**Strategy:** Cores return typed values, not exit codes; the command tier maps to exit codes (0 PASS, 1 BLOCK/FAIL, 2 WARN/can't-verify) and renders remediation.

**Patterns:**
- Typed-Unknown degradation: missing tool / unparseable output → `Unknown` → WARN, never a false hard block (`internal/preflight`, `internal/detect`).
- Typed tool errors: `orchestrate.ErrToolNotFound` (missing binary → soft) vs `ErrCommandFailed` (ran non-zero with no output → hard) (`internal/orchestrate/systemd.go`).
- Transactional rollback: any mutate error or non-pass prove → verbatim restore, with honest rollback-incomplete reporting (`internal/backendswap/backendswap.go`).

## Cross-Cutting Concerns

**Logging:** No first-party telemetry; the command tier prints tables / `--json`. Journald is read (bounded by `io.LimitReader`) only for the residency scrape.
**Validation:** Pure cores validate inputs (fit-guard, manual-override re-validation in recommend, traversal guard in `WriteUnits`); preflight is the host-readiness gate.
**Authentication:** Dashboard `/api` mutations guarded by `requireSameOrigin` middleware; loopback-only bind; config written 0600 under XDG.

---

*Architecture analysis: 2026-06-07*
