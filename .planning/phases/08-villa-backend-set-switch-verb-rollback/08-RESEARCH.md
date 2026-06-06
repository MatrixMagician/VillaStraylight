# Phase 8: `villa backend set` Switch Verb + Rollback — Research

**Researched:** 2026-06-06
**Domain:** Transactional on-host backend switching (Podman Quadlet + rootless systemd) with capture→prove→cutover→rollback state-machine, ROCm/HIP bring-up failure detection, and real generation-probe + residency-proof gating
**Confidence:** HIGH (the phase is a near-mechanical clone of proven in-repo patterns; the only MEDIUM/LOW items are live ROCm log-line shapes that need on-hardware confirmation)

## Summary

Phase 8 is explicitly the "v1.0 Phase-2 analog" and the on-hardware risk concentration of the milestone. The good news from research: **almost every primitive it needs already exists in the codebase and is already wired through injectable `Deps` seams.** This is not a greenfield build — it is a *composition* of `internal/modelswap` (the ordering-is-the-security-contract skeleton), `internal/inference` (the residency/generation-probe engine, already backend-parameterized in Phase 6), `internal/preflight.RunROCm` (the Phase-7 ROCm fit/host gate), `internal/orchestrate` (render/reconcile/atomic-write + the systemd seam), and `internal/config` (config-as-source-of-truth).

The single genuinely *new* mechanism Phase 8 introduces is the **transactional rollback envelope**: capture the verbatim prior `villa-llama.container` bytes + the prior `VillaConfig` BEFORE mutating, then on ANY failure in the prove/cutover sequence, restore those exact bytes, daemon-reload, restart, and re-ready. The model-swap skeleton already proves the *forward* sequence (save→regenerate→daemon-reload→restart-inference-only); Phase 8 wraps it in a capture+restore frame and inserts a hard **generation-probe + residency-proof gate** between "restart" and "success" — where model-swap simply trusted the restart.

**Primary recommendation:** Build `internal/backendswap` as a sibling of `internal/modelswap`, structured as `capture() → mutate(target) → prove(new backend, bounded timeout) → on-fail restore(captured)`. Reuse `inference.RunningOffloadVerdict` (with `BackendFor(target).ResidencyProof()` markers) for the residency proof and `inference`'s `chatProbe` shape for the generation probe. Reuse `preflight.RunROCm` for the preflight gate and `recommend.Pick(Overrides{Model: cfg.Model})` for the fit re-check (NEVER re-pick the model). Wire the live decode-time `detect.GPUBusyPercent()` read that Phase 6 deferred to Phase 8 (D-07) so the ROCm busy-corroborator actually fires during the prove step. Add a `villa backend` cobra noun with `set <backend>`, `show`, and `set --dry-run`. No new external dependencies.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| `villa backend set/show` CLI parsing, exit-code mapping, human/JSON output | CLI (`cmd/villa`) | — | Matches every existing verb: cobra RunE → `os.Exit`, body RETURNS code; printing lives only here |
| Transactional capture→mutate→prove→rollback orchestration | Control library (`internal/backendswap`, NEW) | CLI (live `Deps` wiring) | Pure, `Deps`-injected core mirroring `internal/modelswap`; testable with no live host |
| Fit re-check (model preserved, refuse if no longer fits) | `internal/recommend` (`Pick` fit-math) | backendswap | Same fit-math `model swap` reuses; NO new envelope math |
| ROCm preflight gate (refuse-with-remediation) | `internal/preflight` (`RunROCm`) | backendswap | Phase-7 verdict; backendswap consumes `[]CheckResult` |
| Unit regenerate (render the new backend's `villa-llama.container`) | `internal/orchestrate` (`Render`) | inference (`BackendFor`) | Config→units derivation already exists; backend flips via `BackendFor(cfg.Backend)` |
| Atomic unit write + verbatim capture/restore | `internal/orchestrate` (`WriteUnits`/`Reconcile`) + filesystem snapshot | backendswap | `atomicWrite` exists; capture is a raw `os.ReadFile` of the on-disk unit before mutate |
| systemd daemon-reload + restart-inference-only | `internal/orchestrate` (`Systemd`) | backendswap | `DaemonReload`/`Restart` seams exist, fixed-arg, fail-typed |
| Generation-probe readiness (bounded timeout, real tokens) | `internal/inference` (`chatProbe` + `pollHealth`) | backendswap | Reuse the probe engine; do NOT rebuild the OpenAI client |
| Residency proof for the NEW backend (ROCm0 buffer + gpu_busy + no fault) | `internal/inference` (`RunningOffloadVerdict`) | `internal/detect` (journald/gpu_busy/GTT reads) | Phase-6 backend-parameterized engine; markers from `BackendFor(target).ResidencyProof()` |
| Live decode-time `gpu_busy_percent` read (D-07, deferred to here) | `internal/detect` (`GPUBusyPercent`) | backendswap prove-step | The "wire the live read" task Phase 6 explicitly deferred to Phase 8 |
| `backend show` active-backend report | CLI + `internal/config` (`LoadVilla`) | inference (`BackendFor` for image tag) | Active backend = `cfg.Backend` (source of truth); image tag from the resolved backend |

## Standard Stack

This phase adds **zero new external libraries.** It is built entirely from the Go standard library and existing in-repo internal packages. Below is the inventory of what to reuse (the real "stack" for this phase).

### Core (existing in-repo seams to reuse — do NOT reimplement)

| Package / symbol | File | Purpose for Phase 8 | Provenance |
|------------------|------|---------------------|------------|
| `modelswap.Run` / `modelswap.Deps` | `internal/modelswap/modelswap.go` | The ordering-is-security skeleton to CLONE (resolve→guard→persist-FIRST→regenerate→restart-inference-only); typed `Result` not exit code | [VERIFIED: codebase] |
| `inference.RunningOffloadVerdict` | `internal/inference/running_offload.go:305` | The residency proof for the NEW backend (journald `<DeviceToken>` buffer line + GTT floor + gpu_busy corroborator + fault-void) | [VERIFIED: codebase] |
| `inference.BackendFor(name)` | `internal/inference/backend.go:21` | Resolve target backend → `ResidencyProof()` markers + image; fails closed on unknown | [VERIFIED: codebase] |
| `backendROCm.ResidencyProof()` | `internal/inference/backend_rocm.go:99` | ROCm markers: `DeviceToken="ROCm0"`, `DeviceLabel="- ROCm"`, `FaultString="Memory access fault by GPU node"` | [VERIFIED: codebase] |
| `inference.chatProbe` (shape) | `internal/inference/probe.go:97` | Real generation probe via reused `llm.OpenAIClient` — bounded `chatProbeTimeout=60s`; tokens>0 = OK | [VERIFIED: codebase] |
| `inference.pollHealth` / `pollRunnerHealth` | `internal/inference/probe.go:52,219` | Bounded readiness poll (200/503-aware); the gate, never the verdict | [VERIFIED: codebase] |
| `preflight.RunROCm(profile)` | `internal/preflight/checks_rocm.go:38` | Refuse-with-remediation ROCm host gate (gfx1151/kernel/firmware/HSA/image-deny) | [VERIFIED: codebase] |
| `recommend.Pick(p, cat, Overrides{Model: cfg.Model})` | `internal/recommend/recommend.go:74` | Fit re-check for the PRESERVED model against the envelope; `Fits=false` → refuse | [VERIFIED: codebase] |
| `orchestrate.Render(RenderInput)` | `internal/orchestrate/render.go:76` | Regenerate `villa-llama.container` from config + resolved backend | [VERIFIED: codebase] |
| `orchestrate.Reconcile` / `WriteUnits` / `atomicWrite` | `internal/orchestrate/reconcile.go:26,52,68` | Hash idempotency + atomic temp-then-rename unit write (traversal-guarded) | [VERIFIED: codebase] |
| `orchestrate.Systemd` (`DaemonReload`/`Restart`/`IsActive`/`ResidencyJournal`) | `internal/orchestrate/systemd.go` | Fixed-arg user-manager seam; `ResidencyJournal` scopes to current invocation (F-3 fix) | [VERIFIED: codebase] |
| `config.LoadVilla` / `config.SaveVilla` | `internal/config/villaconfig.go:124,150` | Config-as-source-of-truth; 0600 traversal-guarded writer | [VERIFIED: codebase] |
| `detect.GPUBusyPercent()` | `internal/detect/gpu_amd.go:460` | LIVE decode-time gpu_busy read — the D-07 read deferred to THIS phase | [VERIFIED: codebase] |
| `detect.GTTUsedBytes` | `internal/detect/gpu_amd.go` | Point-in-time GTT floor for residency corroboration | [VERIFIED: codebase] |

### Supporting (stdlib)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `os` (`ReadFile`/`Rename`/`WriteFile`) | go 1.26.2 stdlib | Capture verbatim prior unit bytes; restore via the existing atomic-write path | Capture + rollback |
| `time` (`time.Duration`/deadline) | go 1.26.2 stdlib | Bounded prove-timeout (the `load_tensors` hang guard) | Prove step |
| `context` | go 1.26.2 stdlib | Cancellation/deadline threading into the probe (mirrors `Validate(ctx, ...)`) | Prove step |
| `github.com/spf13/cobra` v1.10.2 | (already a dep) | `villa backend` noun + `set`/`show` subcommands | CLI wiring |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Cloning `modelswap` into a new `backendswap` package | Generalizing `modelswap.Run` to also swap backends | REJECTED: model-swap's contract (catalog-resolve + auto-pull) is wrong for a backend switch (no model resolution, no pull); the rollback frame is new. A sibling package keeps each contract clean and the model-swap golden/tests byte-stable. Reuse via shared lower seams (`orchestrate`, `inference`, `recommend`), not by overloading `Run`. |
| Capture = raw `os.ReadFile` of the on-disk unit | Re-render the prior backend from the prior config | Re-render is *almost* verbatim but risks a template drift between capture and restore. SC#2 demands the **verbatim captured** unit. Capture the actual on-disk bytes (and the prior `VillaConfig`) — restore is then byte-exact regardless of template changes. |
| `RunningOffloadVerdict` for the prove step | A fresh `Validate()` run (`internal/inference/validate.go`) | `Validate` spins a NEW `--rm` container at a transient name; the switch must prove the ACTUAL restarted `villa-llama.service`, which is the running-server case `RunningOffloadVerdict` is built for (WR-05/CR-03). Use the running-server verdict, not the cold-start validator. |
| Restart only `villa-llama.service` | Restart the whole stack | Open WebUI / dashboard are backend-agnostic; restarting them is needless churn and risks unrelated failures masking the switch outcome. SC#1 mandates inference-unit-only (mirrors `model swap`). |

**Installation:** No new packages. Confirm the existing toolchain:
```bash
go version   # go1.26.2 (verified) — no go.mod changes expected for this phase
```

## Package Legitimacy Audit

**No external packages are added by this phase.** Every dependency is either the Go standard library or an existing in-repo `internal/` package already vetted in Phases 1–7. `go.mod` is expected to be **unchanged**.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| *(none added)* | — | — | — | — | — | N/A — phase adds no external deps |

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

*If a plan unexpectedly proposes a new external dependency, that is a red flag for this phase — the milestone charter ("Go is the control plane only; build only the control plane") and every prior phase built switch/probe/render logic from stdlib + in-repo seams. Gate any new dep behind a `checkpoint:human-verify`.*

## Architecture Patterns

### System Architecture Diagram

```
`villa backend set rocm`  (cmd/villa/backend.go — cobra RunE → os.Exit(code))
        │
        ▼
runBackendSet(name, deps)  ── --dry-run? ──► preview: {target, fit verdict, preflight verdict}; MUTATE NOTHING ──► exit 0
        │ (real run)
        ▼
backendswap.Run(deps, target)  (internal/backendswap — pure, Deps-injected)
   │
   ├─(1) LoadConfig ─────────────► cfg (source of truth); fromBackend = cfg.Backend
   │        │
   │        └─ same-backend? ────► no-op refuse/notice (already on target), exit 0
   │
   ├─(2) FIT-GUARD (preserve model) ─► recommend.Pick(Overrides{Model: cfg.Model})
   │        │                          Fits=false ──► REFUSE-with-remediation (zero side effects) ──► exit 1
   │        ▼
   ├─(3) PREFLIGHT GATE (target=rocm) ─► preflight.RunROCm(profile)
   │        │                            any FAIL ──► REFUSE-with-remediation (zero side effects) ──► exit 1
   │        ▼
   ├─(4) ★CAPTURE★ (verbatim, BEFORE any mutation)
   │        ├─ priorUnitBytes = os.ReadFile(<quadletDir>/villa-llama.container)
   │        └─ priorConfig    = cfg (deep copy)
   │        ▼
   ├─(5) MUTATE: cfg.Backend = target ; SaveConfig(cfg) ; Render(new backend) ; WriteUnits ; DaemonReload ; Restart(villa-llama.service)
   │        │  (any error here ──► ROLLBACK, see ▼)
   │        ▼
   ├─(6) ★PROVE★ (bounded timeout — the load_tensors-hang guard)
   │        ├─ pollHealth(endpoint, deadline)  ── never-ready before deadline ──► PROVE FAIL
   │        ├─ generationProbe(endpoint)       ── 0 tokens / error ──► PROVE FAIL
   │        └─ RunningOffloadVerdict{ Markers: BackendFor(target).ResidencyProof(),
   │                                  JournalText: ResidencyJournal(svc),
   │                                  GPUBusyPercent: detect.GPUBusyPercent(),  ← live decode-time read (D-07)
   │                                  GTTUsedBytes, WeightBytes, Props }
   │                 Status==FAIL (CPU fallback / 0% busy / fault / 0-MiB buffer) ──► PROVE FAIL
   │        │
   │        ├── PROVE PASS ──────────────────────────────────────────────► Switched ✓ exit 0
   │        │
   │        └── PROVE FAIL ──► ★ROLLBACK★
   │                              ├─ WriteUnits(priorUnitBytes)  (verbatim restore)
   │                              ├─ SaveConfig(priorConfig)
   │                              ├─ DaemonReload ; Restart(villa-llama.service)
   │                              └─ re-ready (pollHealth)  ──► RolledBack (failed switch = no-op to stack) exit 1
   │
   └─ returns typed Result{Switched|Refused|RolledBack|Err, FromBackend, ToBackend, ProveVerdict, Reason}
```

### Recommended Project Structure
```
internal/backendswap/
└── backendswap.go        # NEW — Run(Deps, target) capture→mutate→prove→rollback; pure, Deps-injected (sibling of modelswap)
cmd/villa/
└── backend.go            # NEW — `villa backend` noun: set <backend> [--dry-run], show; liveBackendSwapDeps(); exit-code mapping
```
Tests: `internal/backendswap/backendswap_test.go` (drives the whole state-machine with fake Deps — asserts capture-before-mutate ordering, prove-fail→verbatim-restore, fit-refuse zero-side-effects), `cmd/villa/backend_test.go` (exit-code + dry-run + show + JSON output).

### Pattern 1: Typed `Deps` + `Run() → Result` (NOT exit code)
**What:** The pure core takes an injectable `Deps` struct (every host touch is a field) and returns a typed `Result`; the cobra caller maps `Result` → exit code + messages.
**When to use:** Every backendswap function. This is THE established repo idiom (modelswap, install, lifecycle, status all do it) and is what makes the state-machine testable with no live host.
**Example:**
```go
// Source: internal/modelswap/modelswap.go (the skeleton to extend) — VERIFIED codebase
type Deps struct {
    LoadConfig        func() (config.VillaConfig, error)
    Profile           func() detect.HostProfile                 // for fit re-check + preflight
    FitsModel         func(cfg config.VillaConfig) (bool, string) // recommend.Pick(Overrides{Model: cfg.Model})
    PreflightROCm     func(p detect.HostProfile) []preflight.CheckResult
    CaptureUnit       func() ([]byte, error)                    // os.ReadFile(<quadletDir>/villa-llama.container)
    SaveConfig        func(c config.VillaConfig) error
    ReconcileAndWrite func(c config.VillaConfig) (changed bool, err error)
    RestoreUnit       func(bytes []byte) error                  // atomic write of captured bytes
    DaemonReload      func() error
    Restart           func(service string) error
    Prove             func(ctx context.Context, target string) ProveVerdict // pollHealth+gen-probe+residency
    InstallServiceName string                                    // "villa-llama.service"
}
type Result struct {
    Refused, Switched, RolledBack, NoOp bool
    Reason     string
    Err        error
    FailedStep string
    FromBackend, ToBackend string
    Prove      ProveVerdict
}
```

### Pattern 2: Capture-before-mutate, verbatim restore (the transactional frame — NEW)
**What:** Snapshot the exact on-disk `villa-llama.container` bytes AND the prior `VillaConfig` BEFORE the first mutation. On any prove/cutover failure, restore those exact bytes via the existing `atomicWrite`, restore config, daemon-reload, restart, and re-ready.
**When to use:** The whole switch is wrapped in this frame. Capture must be the LAST thing before mutation and must succeed (an uncapturable prior unit → refuse, do not mutate).
**Why verbatim (not re-render):** SC#2 requires the **verbatim captured prior unit**. Re-rendering the prior backend from the prior config is *almost* identical but couples rollback fidelity to template stability across the capture window. Capturing raw bytes is byte-exact and template-drift-proof.
**Example:**
```go
// Capture (step 4) — VERIFIED: orchestrate.atomicWrite path already exists; this is a raw read
priorUnit, err := d.CaptureUnit() // os.ReadFile(filepath.Join(quadletUnitDir(), "villa-llama.container"))
if err != nil {
    return Result{Refused: true, Reason: "cannot capture prior unit for rollback safety", Err: err}
}
priorCfg := cfg // value copy of VillaConfig (no pointers inside)
// ... mutate + prove ...
// Rollback on prove fail:
if proveFailed {
    _ = d.RestoreUnit(priorUnit)   // reuses WriteUnits/atomicWrite → temp-then-rename
    _ = d.SaveConfig(priorCfg)
    _ = d.DaemonReload()
    _ = d.Restart(d.InstallServiceName)
    // best-effort re-ready poll; a failed switch is a NO-OP to the running stack
    return Result{RolledBack: true, Reason: prove.Detail, FromBackend: priorCfg.Backend, ToBackend: target}
}
```

### Pattern 3: Prove = readiness gate AND real generation AND residency proof (bounded)
**What:** `systemctl is-active` / `/health 200` alone NEVER counts as success (SC#3). The prove step requires three things within a bounded deadline: (a) `pollHealth` returns ready, (b) a real generation probe returns ≥1 token, (c) `RunningOffloadVerdict` is `StatusPass` for the TARGET backend's markers.
**When to use:** Immediately after the restart in the mutate step; its outcome decides Switched-vs-Rollback.
**Example:**
```go
// Source: composing internal/inference probe.go + running_offload.go — VERIFIED codebase
func liveProve(ctx context.Context, target string) ProveVerdict {
    cfg, _ := config.LoadVilla()
    backend, err := inference.BackendFor(target) // fail-closed
    if err != nil { return ProveVerdict{Status: inference.StatusFail, Detail: err.Error()} }
    endpoint := inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint()

    // (a) bounded readiness — never-ready before deadline IS the load_tensors-hang signal
    deadlineCtx, cancel := context.WithTimeout(ctx, proveTimeout) // bounded
    defer cancel()
    if r := pollHealthBounded(deadlineCtx, endpoint); !r { return ProveVerdict{Status: inference.StatusFail, Detail: "not ready before timeout (possible load_tensors hang or CPU-fallback stall)"} }

    // (b) real generation
    if !generationProbeOK(deadlineCtx, endpoint, cfg.Model) { return ProveVerdict{Status: inference.StatusFail, Detail: "generation probe returned no tokens"} }

    // (c) residency proof for the NEW backend — markers from the target backend
    sys := orchestrate.NewSystemd()
    journal, _ := sys.ResidencyJournal("villa-llama.service") // current-invocation scoped (F-3)
    v := inference.RunningOffloadVerdict(inference.RunningOffloadInput{
        JournalText:    journal,
        GTTUsedBytes:   detect.GTTUsedBytes(),
        GPUBusyPercent: detect.GPUBusyPercent(), // ← live decode-time read (D-07, deferred to Phase 8)
        WeightBytes:    liveWeightBytes(cfg),
        Props:          liveProps(endpoint),
        ConfigModel:    modelFile, ConfigContext: cfg.Ctx,
        Markers:        backend.ResidencyProof(), // ROCm0 / "- ROCm" / fault string
    })
    return ProveVerdict{Status: v.Status, Detail: v.Detail, Verdict: v}
}
```

### Anti-Patterns to Avoid
- **Re-picking the model on a backend switch.** Model = config (BSET-03). The switch preserves model/quant/context verbatim; it only re-CHECKS that the preserved model still fits the target backend's envelope. If it no longer fits → refuse-with-remediation, do NOT silently swap to a smaller model.
- **`systemctl is-active`/`/health 200` as success.** Explicitly forbidden by SC#3. A ROCm container can be `active` and answer `/health` while silently running on CPU. Only the dual residency proof + real generation counts.
- **Unbounded readiness wait.** A `load_tensors` hang must surface as a bounded-timeout PROVE FAIL → rollback, NOT an infinite wait. Thread a `context.WithTimeout` deadline through the prove poll (mirrors `defaultReadyTimeout` but with rollback on expiry, not a soft WARN).
- **Re-rendering for rollback instead of restoring captured bytes.** SC#2 says verbatim. Restore the captured on-disk bytes.
- **Generalizing `modelswap.Run` to also switch backends.** Keeps two clean contracts apart; protects the model-swap golden/tests. Compose shared lower seams instead.
- **Mutating config/units under `--dry-run`.** `--dry-run` previews {target, fit verdict, preflight verdict} and writes nothing — no `SaveConfig`, no `WriteUnits`, no restart (mirrors `install --dry-run`).
- **Restarting Open WebUI / dashboard on a backend switch.** Inference-unit-only (SC#1).
- **Hardcoding `ROCm0`/HSA/fault literals in backendswap.** They live ONLY behind `BackendFor(target).ResidencyProof()` (the seam grep-gate `TestSeamGrepGate` will fail otherwise). backendswap must stay literal-free.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Atomic unit write / temp-then-rename | A bespoke writer | `orchestrate.WriteUnits` / `atomicWrite` | Already fsync + traversal-guarded + rename-atomic (T-03-02/03) |
| Residency proof / CPU-fallback detection | A new log scraper | `inference.RunningOffloadVerdict` + `ResidencyProof()` | Phase-6 backend-parameterized; handles ROCm0 buffer line, 0-MiB FAIL, fault-void, gpu_busy fold |
| Real generation probe | A new OpenAI client | `inference.chatProbe` / `llm.OpenAIClient` | D-04: reuse the gateway, never rebuild; bounded timeout already there |
| Readiness poll (200/503-aware) | A new poller | `inference.pollHealth` | 503=keep-polling, bounded, typed-Unknown on timeout |
| Fit re-check / envelope math | New memory math | `recommend.Pick(Overrides{Model: cfg.Model})` | Single source of fit-math; `Fits=false` carries the reason |
| ROCm host gate | New checks | `preflight.RunROCm` | Phase-7 externalized policy (kernel/firmware/HSA/image-deny), refuse-with-remediation |
| systemd lifecycle (reload/restart/is-active/journal) | `os/exec` calls | `orchestrate.Systemd` | Fixed-arg (no shell), typed not-found degradation, invocation-scoped journal (F-3) |
| Config read/write (source of truth) | TOML plumbing | `config.LoadVilla`/`SaveVilla` | 0600, traversal-guarded, self-healing normalize |
| Live gpu_busy / GTT reads | sysfs parsing | `detect.GPUBusyPercent` / `detect.GTTUsedBytes` | Typed-Unknown discipline already implemented |

**Key insight:** Phase 8's *only* genuinely new code is (1) the capture/restore transactional frame, (2) the `Prove` composition wiring the live `gpu_busy` read Phase 6 deferred, and (3) the `villa backend` cobra noun. Everything else is wiring established seams. Treat any task that proposes a new scraper, poller, fit-calc, or systemd shell-out as a smell.

## Runtime State Inventory

> This is a switch-on-running-install phase, so runtime state IS the subject. Inventory of state the switch must capture/restore/re-prove (not a rename audit, but the same discipline):

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data (config) | `~/.config/villa/config.toml` — the `backend` field is the mutated value; model/quant/ctx must be preserved | Capture `priorConfig`; restore verbatim on rollback. Mutate ONLY `cfg.Backend` |
| Live service config (units) | `~/.config/containers/systemd/villa-llama.container` — the Quadlet unit regenerated for the new backend (image/devices/env differ Vulkan↔ROCm) | Capture `priorUnitBytes` via `os.ReadFile` BEFORE mutate; restore verbatim via `WriteUnits`/`atomicWrite` on rollback |
| OS-registered state (systemd) | `villa-llama.service` (Quadlet-generated). A `daemon-reload` is required after every unit write (and after the restore) so systemd re-reads the changed/restored unit | `DaemonReload` after both mutate-write AND restore-write; restart inference unit only |
| Live container/process | The running `villa-llama` container holds a per-process KV cache that CANNOT migrate across a backend switch (it resets on restart — explicitly out of scope per REQUIREMENTS.md). The restart tears down the old container | Accept KV reset; "preserve model+config" = the *configured* selection, not the live cache |
| Secrets/env vars | ROCm requires `HSA_OVERRIDE_GFX_VERSION=11.5.1` + `ROCBLAS_USE_HIPBLASLT=1` — these live IN the rendered unit (backend_rocm.go), NOT in user env or secrets | None — env is unit-embedded and round-trips with the captured/rendered unit |
| Build artifacts | None — no compiled artifact carries backend state | None |

**Nothing found in category — Secrets:** Confirmed: the only backend-distinguishing env (`HSA_OVERRIDE_*`, `ROCBLAS_*`) is rendered into the `.container` unit by `backendROCm.ContainerArgs`/the Phase-7 renderer, captured/restored as unit bytes. No separate secret store touched.

## Common Pitfalls

### Pitfall 1: Treating "container active / /health 200" as a successful ROCm bring-up
**What goes wrong:** ROCm silently falls back to CPU (wrong/missing HSA override, allocation-cap, partial init) yet the container is `active` and `/health` returns 200. The switch reports success; the user gets a slow CPU stack.
**Why it happens:** `is-active`/`/health` test liveness, not offload. This is the exact silent-CPU-fallback the whole milestone exists to catch (governing invariant carried from v1.0 D-11).
**How to avoid:** The PROVE step REQUIRES `RunningOffloadVerdict == StatusPass` (ROCm0 model-buffer line with N>0 MiB + non-zero `gpu_busy_percent` during the decode + no `Memory access fault` + GTT floor met) AND ≥1 real generated token. A CPU buffer-only journal, a 0-MiB ROCm0 buffer, or `gpu_busy_percent==0` during the generation probe is a FAIL → rollback.
**Warning signs:** Journal shows `load_tensors: CPU_Mapped model buffer size` and no `ROCm0` buffer line; `gpu_busy_percent` stays 0 during the probe.

### Pitfall 2: `load_tensors` hang causing an infinite/long prove wait
**What goes wrong:** ROCm bring-up wedges during weight load (`load_tensors`) — `/health` never reaches 200. An unbounded poll hangs the CLI; the user can't tell a slow load from a dead one.
**Why it happens:** ROCm large-model loads can hang on gfx1151 under the wrong firmware/kernel/allocation conditions; there is no clean error, just no progress.
**How to avoid:** Bound the prove readiness poll with a `context.WithTimeout` deadline. Deadline expiry = PROVE FAIL → rollback (NOT a soft WARN like cold-start `Validate`). Pick a generous-but-finite timeout (the existing `defaultReadyTimeout` is 5m — reuse or expose as the prove bound). Document the chosen value as a constant.
**Warning signs:** `/health` stuck on 503 / transport-refused past the deadline with no `load_tensors ... buffer size` line in the journal.

### Pitfall 3: ROCm allocation-cap / firmware fault mid-load
**What goes wrong:** `rocm7-nightlies` 64 GB allocation cap (denied by Phase-7 policy, but a hand-edited image or a large model could still trip an alloc failure), or a firmware fault, aborts the load.
**Why it happens:** Known gfx1151 ROCm sharp edges (CLAUDE.md "What NOT to Use"). The `Memory access fault by GPU node` marker is the KFD/HIP abort signal.
**How to avoid:** `RunningOffloadVerdict` already FAILs on `FaultString="Memory access fault by GPU node"` (voids residency BEFORE any buffer-line PASS — `running_offload.go:121`). The Phase-7 `RunROCm` image-deny gate refuses the nightlies tag up front. An allocation failure surfaces as either a fault string or a never-ready timeout (Pitfall 2) → both rollback.
**Warning signs:** `Memory access fault by GPU node` in the journal; `failed to allocate` / `out of memory` lines (the `ceilingOOMMarkers` set in `probe.go:156` documents the phrasings).

### Pitfall 4: Capturing AFTER mutation (rollback restores the wrong unit)
**What goes wrong:** If capture runs after `SaveConfig`/`WriteUnits`, the "prior" bytes are already the NEW backend's unit — rollback is a no-op and the user is stuck on the broken backend.
**Why it happens:** Easy ordering mistake when adapting the model-swap skeleton (which has no capture step).
**How to avoid:** Capture is step 4, strictly BEFORE step 5 (mutate). Add an explicit ordering test (fake `Deps` records call order; assert `CaptureUnit` precedes `SaveConfig`/`ReconcileAndWrite`). Mirror modelswap's "ordering IS the security contract" test discipline.
**Warning signs:** Rollback test passes a captured unit byte-equal to the post-mutation unit.

### Pitfall 5: Rollback partial failure leaving an inconsistent stack
**What goes wrong:** Restore-write succeeds but the daemon-reload or restart fails, leaving config/unit restored but the service down or running the new backend.
**Why it happens:** Any host op can fail; rollback is itself a multi-step sequence.
**How to avoid:** Make rollback best-effort-but-reported: attempt each restore step, accumulate errors, and surface a CLEAR "rollback incomplete — manual remediation" message with the exact `villa restart` / unit path if a step fails. Do NOT claim a clean no-op if rollback itself errored. (This is the one place where "a failed switch is a no-op" can break; report honestly per the project's typed-Unknown discipline.)
**Warning signs:** Rollback returns success while `IsActive` ≠ `active` afterward.

### Pitfall 6: ROCm device-init log prefix uncertainty (`ggml_cuda_init:`)
**What goes wrong:** The start-time scrape's `StartLogDevicePrefix` for ROCm is set to `ggml_cuda_init:` (HIP reuses the CUDA-init prefix), tagged A2/MEDIUM in `backend_rocm.go:94`. If the kyuz0 ROCm build emits a different prefix, the start-time device line is missed.
**Why it happens:** Training/assumption-based marker; not yet confirmed against a live ROCm decode on gfx1151.
**How to avoid:** The LOAD-BEARING residency signal for a RUNNING server is the journald `load_tensors: ROCm0 model buffer size = N MiB` line (`DeviceToken="ROCm0"`), NOT the start-time device-init prefix — and that buffer line is the same shape across backends. So the prove step is robust even if `ggml_cuda_init:` is wrong. STILL: confirm the exact ROCm0 buffer-line and gpu_busy behavior on-hardware during Phase 8 UAT and correct the markers if needed (the grep-gate keeps them in `backend_rocm.go`).
**Warning signs:** Live ROCm journal shows the buffer line as e.g. `ROCm0` vs `ROCm_Host` (the code already excludes `ROCm_Host` via exact `Contains` — Pitfall 2 in running_offload.go).

## Code Examples

### `villa backend` cobra noun (mirrors `newModel`)
```go
// Source: pattern from cmd/villa/model.go newModel — VERIFIED codebase
func newBackend() *cobra.Command {
    backend := &cobra.Command{
        Use:   "backend",
        Short: "Inspect and switch the inference backend (vulkan default / rocm opt-in)",
        Args:  cobra.NoArgs,
    }
    backend.AddCommand(newBackendShow(), newBackendSet())
    return backend
}
// newBackendSet: `villa backend set <vulkan|rocm> [--dry-run]`
//   RunE → code := runBackendSet(cmd, args[0], dryRun, liveBackendSwapDeps()); os.Exit(code)
// Register in root.go AddCommand list alongside newModel(), newStatus(), ...
```

### Fit re-check seam (preserve model, refuse if it no longer fits)
```go
// Source: liveSwapDeps Fits closure, model.go:323 — VERIFIED codebase (reuse verbatim)
FitsModel: func(cfg config.VillaConfig) (bool, string) {
    cat, _, err := catalog.Load(modelCatalogPath)
    if err != nil { return false, "catalog load failed" }
    rec := recommend.Pick(detect.Probe(), cat, recommend.Overrides{Model: cfg.Model})
    if rec.Fits { return true, "" }
    return false, fmt.Sprintf("needs %d bytes vs %d usable", rec.TotalBytes, rec.UsableEnvelopeBytes)
},
```

### Capture / restore seams
```go
// Source: composing quadletUnitDir (install.go:749) + orchestrate.WriteUnits — VERIFIED codebase
CaptureUnit: func() ([]byte, error) {
    dir, err := quadletUnitDir(); if err != nil { return nil, err }
    return os.ReadFile(filepath.Join(dir, "villa-llama.container"))
},
RestoreUnit: func(b []byte) error {
    dir, err := quadletUnitDir(); if err != nil { return err }
    // reuse the atomic temp-then-rename writer via a single-unit Plan
    plan := orchestrate.Plan{Changed: []orchestrate.Unit{{Name: "villa-llama.container", Text: string(b)}}}
    return orchestrate.WriteUnits(plan, dir)
},
```

## State of the Art

| Old Approach (v1.0) | Current Approach (v1.1 Phase 8) | When Changed | Impact |
|--------------------|--------------------------------|--------------|--------|
| `model swap`: save→regenerate→restart, trust the restart | `backend set`: + capture-before + prove-after + verbatim-rollback | This phase | First time the stack switches a backend on a running install with a real cutover gate |
| Offload markers hardcoded `Vulkan0` | Backend-parameterized `ResidencyProof()` markers | Phase 6 | ROCm slots in; backendswap stays literal-free |
| `gpu_busy_percent` wired as fixture-only (input present, never read live) | LIVE `detect.GPUBusyPercent()` read during the prove decode | This phase (D-07 deferred read) | The busy-corroborator finally fires on a real decode — closes the silent-CPU-fallback gap for ROCm |

**Deprecated/outdated:**
- Cold-start `inference.Validate()` for proving the switch — wrong tool (spins a transient `--rm` container, not the running `villa-llama.service`). Use `RunningOffloadVerdict`.
- `rocm7-nightlies` image — denied by Phase-7 `RunROCm` image policy (64 GB cap).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | ROCm start-time device-init prefix is `ggml_cuda_init:` | Pitfall 6 / backend_rocm.go (carried A2) | LOW — the running-server prove uses the `ROCm0` journald buffer line (backend-neutral shape), not the start prefix; mis-set prefix only affects the cold-start scrape, not the switch gate. Confirm on-hardware. |
| A2 | A non-zero `gpu_busy_percent` is observable during the short generation probe window on a healthy ROCm decode | Pattern 3 / Pitfall 1 | MEDIUM — if the probe decode is too brief to register busy %, the corroborator could read 0 and falsely FAIL. Mitigate: the generation probe should elicit enough tokens to overlap a busy sample, OR sample gpu_busy during (not after) the decode. Validate timing on-hardware (the D-07 live-read task). |
| A3 | `villa-llama.container` is the exact on-disk filename to capture/restore in the Quadlet dir | Runtime State Inventory / Code Examples | LOW — consistent with `installServiceName`/render across the codebase; verify the rendered unit filename in the plan (grep the renderer) before locking the capture path. |
| A4 | The prove timeout can reuse/derive from `defaultReadyTimeout` (5m) as the bounded `load_tensors`-hang guard | Pitfall 2 | LOW — value is a tunable constant; on-hardware UAT may suggest a different bound for large ROCm loads. Externalize as a named constant. |
| A5 | No new external Go dependency is required | Package Legitimacy Audit / Standard Stack | LOW — every needed capability exists in stdlib or in-repo; flagged so the planner treats any proposed new dep as a checkpoint. |

## Open Questions (RESOLVED)

1. **Same-backend `backend set vulkan` when already on vulkan — refuse, no-op, or re-prove?**
   - What we know: `model swap` treats an unchanged target as a clean no-op (WR-06).
   - What's unclear: whether a redundant `backend set <current>` should be a silent no-op (consistent) or a useful "re-prove the running backend" action.
   - RESOLVED: Treat as a no-op (exit 0, "already on <backend>") for consistency with `model swap` and `up` no-op semantics; defer a separate `backend reprove`/`status`-driven re-check. Implemented in 08-01-01 (`TestNoOpSameBackend`).

2. **Rollback re-ready: how long to wait for the restored backend to come back up?**
   - What we know: the restored Vulkan backend was working before the switch, so it should re-ready quickly.
   - What's unclear: whether to block on a full re-ready poll or fire-and-report.
   - RESOLVED: best-effort bounded re-ready poll; report "rolled back; prior backend restored" on success and "rolled back; prior backend not yet ready — run `villa status`" if the re-ready poll times out (honest reporting, Pitfall 5). Implemented in 08-01-02 rollback helper.

3. **gpu_busy sampling window during the generation probe (ties to A2).**
   - What we know: `RunningOffloadInput.GPUBusyPercent` is a single point-in-time read; a healthy decode shows non-zero busy.
   - What's unclear: whether one read taken right after the probe reliably catches a busy sample on a short completion.
   - RESOLVED: sample `gpu_busy_percent` DURING the generation probe (or take a couple of samples and keep the max) rather than a single post-probe read; validate on-hardware. This is the heart of the D-07 live-read task — implemented in 08-02-01 (sampled during the decode), flagged for live UAT.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build | ✓ | go1.26.2 | — |
| Podman (rootless user socket) | the running stack the switch mutates | host-dependent (on-hardware) | v5 | — (switch operates on an already-installed stack; preflight gates) |
| `systemctl --user` / `journalctl --user` | daemon-reload/restart/residency journal | host-dependent | — | typed not-found degradation already in `orchestrate.Systemd` |
| AMD gfx1151 iGPU + ROCm 7.2.4 image | the actual ROCm bring-up being proved | on-hardware ONLY | — | off-hardware: prove step is unit-tested with fixture journals; the real ROCm decode is the live-UAT subject |
| `detect.GPUBusyPercent` sysfs (`gpu_busy_percent`) | live decode-time residency corroborator | on-hardware | — | typed-Unknown → busy fold skipped (never a false PASS) |

**Missing dependencies with no fallback:** none for off-hardware development/tests (all host touches are `Deps`-injected and fixture-driven). The ROCm real-decode behaviors (A1/A2/A3 in the Assumptions Log) are confirmable ONLY on the live gfx1151 host and are the explicit subject of Phase 8 on-hardware UAT (per STATE.md Blockers/Concerns).

**Missing dependencies with fallback:** sysfs `gpu_busy_percent` and journald reads degrade to typed-Unknown (never a false PASS) when absent.

## Validation Architecture

> nyquist_validation is enabled (config: `workflow.nyquist_validation: true`).

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table tests + fake `Deps`); the repo's established style |
| Config file | none (Go built-in) |
| Quick run command | `go test ./internal/backendswap/ ./cmd/villa/ -run Backend -count=1` |
| Full suite command | `go test ./... -count=1` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| BSET-01 | swap inference unit only; fit-guard refuse; preserve model/quant/ctx | unit | `go test ./internal/backendswap/ -run TestSwapInferenceOnly -count=1` | ❌ Wave 0 |
| BSET-01 | refuse-with-remediation when model no longer fits / ROCm preflight blocks | unit | `go test ./internal/backendswap/ -run TestRefuse -count=1` | ❌ Wave 0 |
| BSET-02 | capture-before-mutate ordering | unit | `go test ./internal/backendswap/ -run TestCaptureBeforeMutate -count=1` | ❌ Wave 0 |
| BSET-02 | prove-fail → verbatim restore + re-ready (failed switch = no-op) | unit | `go test ./internal/backendswap/ -run TestRollbackVerbatim -count=1` | ❌ Wave 0 |
| BSET-02 | cutover gated on generation-probe + residency proof within bounded timeout | unit | `go test ./internal/backendswap/ -run TestProveGate -count=1` | ❌ Wave 0 |
| BSET-02 | `is-active`/200 alone never counts as success | unit | `go test ./internal/backendswap/ -run TestActiveNotSuccess -count=1` | ❌ Wave 0 |
| BSET-03 | `backend show` reports active backend + image tag | unit | `go test ./cmd/villa/ -run TestBackendShow -count=1` | ❌ Wave 0 |
| BSET-03 | `--dry-run` previews (target/fit/preflight), mutates nothing | unit | `go test ./cmd/villa/ -run TestBackendSetDryRun -count=1` | ❌ Wave 0 |
| (seam) | backendswap holds no ROCm0/HSA/image literals | grep-gate | `go test ./internal/inference/ -run TestSeamGrepGate -count=1` (extend coverage to new pkg) | ✅ exists (extend) |

### Sampling Rate
- **Per task commit:** `go test ./internal/backendswap/ ./cmd/villa/ -count=1` + `go vet ./...`
- **Per wave merge:** `go test ./... -count=1`
- **Phase gate:** full suite green + on-hardware UAT (real ROCm bring-up success path AND a forced-failure → rollback path) before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/backendswap/backendswap_test.go` — covers BSET-01/02 state-machine (fake `Deps`, ordering + rollback)
- [ ] `cmd/villa/backend_test.go` — covers BSET-03 (`show`, `set --dry-run`, exit codes, JSON)
- [ ] Extend the inference seam grep-gate (or add an equivalent) so `internal/backendswap` is asserted literal-free of backend tokens
- [ ] No framework install needed (Go built-in `testing`)

## Security Domain

> `security_enforcement: true`, `security_asvs_level: 1`, `security_block_on: high`.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | Local-only CLI; no auth surface added |
| V3 Session Management | no | No sessions |
| V4 Access Control | no | Operates on the invoking user's own rootless units/config |
| V5 Input Validation | yes | `backend` arg validated through `BackendFor` (fail-closed allowlist: vulkan/rocm/empty only) — never interpolated; unit/config paths traversal-guarded (`assertInsideDir`) |
| V6 Cryptography | no | No crypto introduced (model SHA256 verify is the download path, untouched here) |
| V12 File handling | yes | Unit capture/restore writes stay inside the Quadlet dir (`assertInsideDir`); atomic temp-then-rename; 0600 config (`SaveVilla`) |
| V14 Configuration | yes | Backend env (`HSA_OVERRIDE_*`) lives in the rendered unit; rootless Podman; loopback-only publish preserved by the unchanged renderer |

### Known Threat Patterns for {Go CLI + rootless Podman/systemd}

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Command injection via the `<backend>` arg | Tampering | `BackendFor` allowlist (vulkan/rocm); fixed-arg `systemctl`/`journalctl` (no shell) — `orchestrate.Systemd` |
| Path traversal on unit capture/restore | Tampering | `assertInsideDir` on the Quadlet dir (reconcile.go); restore via `WriteUnits` which re-guards |
| Privilege/device escalation by selecting ROCm | Elevation | ROCm device passthrough (`/dev/kfd`,`/dev/dri`) is rooted in the digest-pinned unit + Phase-7 SELinux/`RunROCm` gate; backendswap never adds device args itself (they come from `BackendFor(target).ContainerArgs`/renderer) |
| Silent CPU fallback presented as success | Spoofing (of health) | The PROVE residency+generation gate (the core control of this phase) — a CPU-fallback ROCm run FAILs and rolls back |
| Broken-stack DoS from a failed switch | Denial of Service | Transactional verbatim rollback — a failed switch restores the prior working stack (SC#2) |
| Loopback→exposed port regression on re-render | Information disclosure | The renderer/PublishPort privacy posture is unchanged; `status` loopback assertion + the byte-frozen golden guard it (out of this phase's mutation surface) |

**Security note:** This phase's PRIMARY security property is *availability integrity* — guaranteeing a failed/degraded switch never leaves the user with a broken or silently-CPU stack. The residency-proof gate (anti-spoofing of health) and the verbatim rollback (anti-DoS) are the load-bearing controls and must be covered by the forced-failure UAT path, not just the happy path.

## Sources

### Primary (HIGH confidence)
- Codebase (read in full this session): `internal/modelswap/modelswap.go`, `internal/inference/{backend,inference,validate,probe,offload,running_offload,backend_rocm}.go`, `internal/orchestrate/{systemd,reconcile}.go`, `internal/preflight/checks_rocm.go`, `internal/config/villaconfig.go`, `internal/status/status.go`, `cmd/villa/{model,install,lifecycle,status}.go` — the seams this phase composes.
- `.planning/REQUIREMENTS.md` (BSET-01/02/03, out-of-scope KV-migration), `.planning/ROADMAP.md` (Phase 8 success criteria + depends-on), `.planning/STATE.md` (decisions [03-05] modelswap-clone, [06-01] D-07 gpu_busy live-read deferred to Phase 8, Blockers/Concerns on-hardware risk).
- `./CLAUDE.md` — Vulkan-default/ROCm-opt-in, Podman REST over Unix socket (stdlib), Quadlet via text/template, no full podman bindings, ROCm sharp edges (HSA override, alloc cap, firmware/kernel floors), images to standardize on.

### Secondary (MEDIUM confidence)
- `internal/inference/backend_rocm.go` self-documented A2 (`ggml_cuda_init:` ROCm start prefix) and the ROCm marker set — confirm on-hardware.

### Tertiary (LOW confidence)
- None relied upon. The ROCm live log-line shapes (A1/A2) are flagged as on-hardware-confirm items, not asserted as fact.

## Project Constraints (from CLAUDE.md)

- **Go only, single static binary** — no new external deps; no full `containers/podman/v5` bindings.
- **Podman control via direct REST over the user socket with stdlib** — but THIS phase touches Podman only indirectly through `systemctl --user`/Quadlet (the existing `orchestrate.Systemd` seam); do not introduce a REST client here.
- **Quadlet units are derived/regenerated from config, never hand-edited** — the switch regenerates `villa-llama.container`; capture/restore operate on rendered bytes.
- **Config is the single source of truth** — mutate `cfg.Backend`, persist via `SaveVilla` BEFORE unit work; rollback restores prior config.
- **Vulkan RADV default; ROCm opt-in, never auto-selected** — `backend set` is the explicit user action; never re-pick a backend.
- **Pin `rocm-7.2.4` digest, never `rocm7-nightlies`** — enforced by the rendered unit + Phase-7 `RunROCm` image-deny (backendswap does not choose the image).
- **Strictly local, zero telemetry, loopback-only** — unchanged; the switch must not alter the privacy posture (the renderer/PublishPort are untouched).
- **Offload is asserted, not assumed (D-11 governing invariant)** — the PROVE step's residency proof is the in-phase embodiment of this.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| BSET-01 | `villa backend set rocm\|vulkan` swaps inference unit on a running install (save-before-restart, regenerate, daemon-reload, restart only `villa-llama.service`), fit-guarded, refuses-with-remediation | Pattern 1/2 (modelswap clone + capture frame); fit re-check via `recommend.Pick(Overrides{Model})`; ROCm gate via `preflight.RunROCm`; restart-inference-only via `orchestrate.Systemd.Restart(installServiceName)` |
| BSET-02 | Transactional + rollback-safe: capture prior unit/config, gate cutover on real generation-probe + residency proof for the new backend, auto-rollback verbatim on any failure | Pattern 2 (verbatim capture/restore), Pattern 3 (prove = pollHealth+chatProbe+`RunningOffloadVerdict` bounded); Pitfalls 1–5; live `detect.GPUBusyPercent` read (D-07) |
| BSET-03 | `backend show` reports active backend; model/quant/ctx preserved (model=config, refuse don't re-pick); `--dry-run` previews without mutating | `backend show` = `cfg.Backend` + resolved `BackendFor` image tag; preserve model (mutate only `cfg.Backend`); `--dry-run` mirrors `install --dry-run` (preview target/fit/preflight, zero side effects) |

## Metadata

**Confidence breakdown:**
- Standard stack (reused seams): HIGH — every primitive read in source this session; signatures verified.
- Architecture (capture→prove→rollback state-machine): HIGH — directly maps onto the proven modelswap + inference + orchestrate seams; the only new code is the transactional frame and the prove wiring.
- ROCm live failure-detection markers: MEDIUM — the running-server `ROCm0` buffer-line proof is backend-neutral-shaped and robust; the start-time prefix (A1) and gpu_busy timing (A2/A3) need on-hardware confirmation (the explicit UAT subject).
- Pitfalls: HIGH — sourced from the code's own documented findings (WR-05, CR-03, D-06, fault-void, ceiling OOM markers) + CLAUDE.md ROCm sharp edges.

**Research date:** 2026-06-06
**Valid until:** 2026-07-06 (stable — in-repo seams; re-verify the `rocm-7.2.4` digest and ROCm log-line shapes at plan/UAT time per STATE.md Blockers/Concerns).
