# Codebase Structure

**Analysis Date:** 2026-06-07

## Directory Layout

```
VillaStraylight/
├── cmd/villa/              # Cobra command tier — one file per subcommand
│   ├── main.go             # Entry: newRoot().Execute()
│   ├── root.go             # Command tree + global persistent flags (--json/-v/--force)
│   ├── <subcommand>.go     # detect/recommend/preflight/model/inference/install/...
│   ├── <subcommand>_test.go
│   └── testdata/           # Byte-frozen CLI goldens (*.golden, *.json.golden)
├── internal/               # ~16 pure cores + the one impure orchestrate module
│   ├── config/             # config.toml store — SINGLE SOURCE OF TRUTH (VillaConfig)
│   ├── detect/             # host probe → HostProfile; gpu_amd.go = backend seam
│   ├── catalog/            # embedded model catalog (go:embed seed.json) + override
│   ├── recommend/          # pure Pick() → memory-fitting Recommendation
│   ├── preflight/          # host-prep gate; go:embed rocm-policy.json
│   ├── download/           # model weight pull + shard handling
│   ├── inference/          # BACKEND-NEUTRAL SEAM (BackendFor, Backend iface, offload/prove)
│   ├── orchestrate/        # THE impure module: render(pure)/reconcile(pure)/systemd(host)
│   │   └── quadlet/        # *.tmpl Quadlet unit templates (go:embed)
│   ├── backendswap/        # transactional `villa backend set` core
│   ├── bench/              # pure A/B throughput core (composes backendswap)
│   ├── modelswap/          # guarded `villa model swap` core (shared CLI + dashboard)
│   ├── status/             # read-model aggregation → Report (shared CLI + dashboard)
│   ├── metrics/            # llama.cpp /metrics scrape (pp/tg)
│   ├── dashboard/          # loopback chi server + embedded SPA (go:embed assets)
│   │   └── assets/         # no-build single-page UI
│   ├── llm/                # LEGACY (reference-only) — OpenAI/SSE client parts bin
│   └── */testdata/         # per-package fixtures + goldens
├── web/                    # LEGACY (reference-only) — old React/Vite scaffold
├── docs/
├── bin/                    # built binary output
└── CLAUDE.md               # GSD source-of-truth pointer + prescriptive stack
```

## Directory Purposes

**`cmd/villa/`:**
- Purpose: cobra command tier — flag parsing, exit codes, table/JSON rendering, `live*Deps` wiring.
- Contains: one `<subcommand>.go` + `<subcommand>_test.go` per command; `main.go`; `root.go`.
- Key files: `cmd/villa/root.go` (tree in `newRoot`), `cmd/villa/install.go` (largest — install flow), `cmd/villa/backend.go` (`liveProve`, `liveBackendSwapDeps`), `cmd/villa/bench.go` (`liveBenchDeps`, `--ab` Switch/Restore).

**`internal/inference/` (the seam):**
- Purpose: backend-neutral run + offload proof; the ONLY package allowed to hold backend image/device/marker literals.
- Contains: `backend.go` (`BackendFor`, `Backend`/`ResidencyMarkers` interfaces), `backend_vulkan.go`, `backend_rocm.go`, `offload.go`/`running_offload.go` (dual offload assert), `prove.go` (exported running-server probes), `validate.go`, `probe.go`, `runner_podman.go`, `seam_test.go` (grep gates).
- Key files: `internal/inference/backend.go`, `internal/inference/seam_test.go`.

**`internal/orchestrate/` (the one impure module):**
- Purpose: render Quadlet units (pure), reconcile vs disk (pure), write atomically + drive systemd (impure).
- Contains: `orchestrate.go` (types), `render.go` (pure render + `WriteUnits`), `reconcile.go` (pure sha256 diff), `systemd.go` (fixed-arg host seam), `openwebui.go`, `dashboard_unit.go`, `quadlet/*.tmpl`.
- Key files: `internal/orchestrate/render.go`, `internal/orchestrate/systemd.go`.

**`internal/config/`:**
- Purpose: the single source of truth — XDG `config.toml` load/save (`VillaConfig`), 0600/0700 modes, self-healing defaults.
- Key files: `internal/config/villaconfig.go`.

**`internal/detect/`:**
- Purpose: host probe → typed-Unknown `HostProfile`; `gpu_amd.go` is the second allowed home for backend literals (the GPU seam).
- Key files: `internal/detect/detect.go` (`Probe`), `internal/detect/gpu_amd.go` (seam).

## Key File Locations

**Entry Points:**
- `cmd/villa/main.go`: binary entry → `newRoot().Execute()`.
- `cmd/villa/root.go`: cobra tree + global flags.
- `internal/dashboard/server.go`: `NewServer` (loopback chi server, `villa-dashboard.service`).

**Configuration:**
- `internal/config/villaconfig.go`: `VillaConfig` + load/save (source of truth).
- `internal/catalog/seed.json`: embedded default model catalog (`go:embed`).
- `internal/preflight/rocm-policy.json`: embedded ROCm kernel/firmware policy (`go:embed`).

**Core Logic:**
- `internal/recommend/recommend.go`: pure `Pick()`.
- `internal/inference/backend.go`: `BackendFor` + `Backend` interface.
- `internal/backendswap/backendswap.go`: transactional switch `Run`.
- `internal/bench/bench.go`: A/B core (composes `backendswap`).
- `internal/orchestrate/render.go`: pure unit render.

**Testing:**
- `cmd/villa/testdata/*.golden` + `*.json.golden`: byte-frozen CLI/contract goldens.
- `internal/orchestrate/testdata/*.golden`: rendered Quadlet unit goldens (incl. `villa-llama-rocm.container.golden`).
- `internal/inference/seam_test.go`: `TestSeamGrepGate` + `TestROCmMarkerPresence`.
- `internal/*/testdata/`: per-package fixtures (e.g. `internal/inference/testdata/sysfs`).

## Naming Conventions

**Files:**
- One subcommand per file in `cmd/villa/` (`backend.go`, `bench.go`, `install.go`).
- Tests co-located as `<file>_test.go`.
- Backend implementations: `backend_<name>.go` (`backend_vulkan.go`, `backend_rocm.go`).
- Golden fixtures: `*.golden` (CLI/units) and `*.json.golden` (frozen JSON contracts).
- Embedded data: `seed.json`, `rocm-policy.json`, `quadlet/*.tmpl`.

**Live seam wiring:**
- `live*Deps()` closures in `cmd/villa/` wire a pure core's `Deps` to the real host: `liveBackendSwapDeps`, `liveBenchDeps`, `liveSwapDeps`, `liveStatusDeps`, `liveDashboardDeps`, `liveConfigDeps`, plus `liveProve`/`liveMeasure` injected gates.
- Live host paths are package-level `live*` consts in cores (e.g. `internal/detect/detect.go`: `liveProcMeminfo`, `liveDRMRoot`).

**Directories:**
- `internal/<domain>/` one package per bounded concern; `testdata/` for fixtures; `quadlet/`/`assets/` for embedded payloads.

**Unit identities (stable contract, NOT backend literals):**
- `villa-llama` / `villa-openwebui` containers, `villa` network, `villa-models` volume, `villa-dashboard.service` (`internal/orchestrate/render.go`).

## Where to Add New Code

**New subcommand:**
- Implementation: `cmd/villa/<name>.go` with a `new<Name>()` cobra constructor + `run<Name>` function; register it in `cmd/villa/root.go` `newRoot`.
- Tests: `cmd/villa/<name>_test.go`; frozen output → `cmd/villa/testdata/<name>.json.golden`.

**New pure decision/aggregation logic:**
- Implementation: a new `internal/<domain>/` package with a `Deps` struct + `Run()`/`Pick()` returning a typed `Result`/value — never `os.Exit`, never print.
- Wire it from `cmd/villa` via a `live<Domain>Deps()` closure.

**New inference backend (e.g. Metal/macOS):**
- Implementation: `internal/inference/backend_<name>.go` implementing `Backend` (Image/ContainerArgs/ResidencyProof); add the case to `BackendFor` (`internal/inference/backend.go`).
- Keep ALL its literals in that file; add a golden `internal/orchestrate/testdata/villa-llama-<name>.container.golden`. Do NOT add literals to callers (`TestSeamGrepGate` will fail).

**New Quadlet unit:**
- Template: `internal/orchestrate/quadlet/<name>.tmpl`; render wiring in `internal/orchestrate/render.go`; golden in `internal/orchestrate/testdata/`.

**New config field:**
- Add a TOML-tagged field to `VillaConfig` in `internal/config/villaconfig.go` with a default in `defaultConfig()` (append-only; self-heal type-zero in `normalizeVilla`).

**New dashboard endpoint:**
- Handler in `internal/dashboard/api.go`, route in `internal/dashboard/server.go`; mutations MUST go under the same-origin-guarded `/api` subrouter; fold the shared `status`/`modelswap` core, never a fork.

## Special Directories

**`internal/*/testdata/` and `cmd/villa/testdata/`:**
- Purpose: golden fixtures and host-output samples (sysfs, journal, JSON contracts).
- Generated: goldens are regenerated via the standard `-update` test flow.
- Committed: Yes — the `*.json.golden` files are the byte-frozen contract.

**`internal/orchestrate/quadlet/` and `internal/dashboard/assets/`:**
- Purpose: `go:embed` payloads (unit templates; no-build SPA).
- Generated: No (authored). Committed: Yes.

**`internal/llm/` (LEGACY — reference-only):**
- Purpose: parts bin from the superseded scaffold (OpenAI-compatible / SSE streaming client). May be cannibalized for the gateway/generation probe; do NOT extend as the app.
- Committed: Yes. Not part of the current control-plane architecture.

**`web/` (LEGACY — reference-only):**
- Purpose: old React/Vite chat UI scaffold. Superseded by Open WebUI; a custom chat UI is explicitly out of scope.
- Committed: Yes (`node_modules`/`dist` present locally). Do NOT build new product UI here.

---

*Structure analysis: 2026-06-07*
