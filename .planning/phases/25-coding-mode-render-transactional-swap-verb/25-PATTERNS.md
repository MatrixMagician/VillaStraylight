# Phase 25: Coding-Mode Render & Transactional Swap Verb - Pattern Map

**Mapped:** 2026-06-13
**Files analyzed:** 9 (3 new, 6 modified)
**Analogs found:** 9 / 9 (every file has an exact in-repo analog — this is a composition phase)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/codingmode/codingmode.go` (NEW) | service (transactional core) | event-driven (capture→mutate→prove→rollback) | `internal/backendswap/backendswap.go` | exact (clone the frame) |
| `internal/codingmode/codingmode_test.go` (NEW) | test | event-driven | `internal/backendswap/backendswap_test.go` | exact (clone shape) |
| `cmd/villa/coding-mode.go` (NEW) | route (cobra noun) + provider (live wiring) | request-response | `cmd/villa/backend.go` (noun + `liveProve` + `liveBackendSwapDeps`) | exact |
| `internal/inference/inference.go` (MODIFY) | model (type defn) | transform | existing `RunSpec` struct (this file, lines 104-117) | exact (append field) |
| `internal/inference/backend_vulkan.go` (MODIFY) | service (backend seam) | transform | this file's `ContainerArgs` (lines 81-103) + `llamaServerFlags` (line 53) | exact (extend in place) |
| `internal/inference/backend_rocm.go` (MODIFY) | service (backend seam) | transform | `backend_vulkan.go` `ContainerArgs` (symmetric ROCm sibling) | exact |
| `internal/orchestrate/render.go` / `orchestrate.go` (MODIFY) | utility (pure renderer) | transform | existing `Render`/`RenderInput` (config→RunSpec→ContainerArgs) | exact |
| `internal/orchestrate/testdata/villa-llama-coding.container.golden` (NEW) | test fixture | — | `internal/orchestrate/testdata/villa-llama.container.golden` | exact (append-only sibling) |
| `internal/config/villaconfig.go` (MODIFY) | model (config schema) | CRUD | `MemoryEnabled` fields + `marshalVilla` omit-when-off (this file) | exact |

## Pattern Assignments

### `internal/codingmode/codingmode.go` (transactional core, event-driven) — CMODE-02

**Analog:** `internal/backendswap/backendswap.go` (read in full; CLONE verbatim in shape, swap the "backend string" axis for a "coding-mode target" axis enter|exit).

**Package doc + literal-free import discipline** (`backendswap.go:1-27`): the new core MUST import neither `internal/inference` nor `internal/detect` and define its OWN prove sentinel — that is what keeps it backend-marker-free (`TestSeamGrepGate` walks `internal/`).

```go
// backendswap.go:36 — clone this LOCAL success sentinel (do NOT import inference.StatusPass)
const ProveStatusPass = "pass"

// backendswap.go:43-50 — ProveVerdict is defined here, not imported
type ProveVerdict struct {
    Status string  // cutover succeeds ONLY when == ProveStatusPass
    Detail string
}
```

**Deps seam set to clone** (`backendswap.go:56-93`): `LoadConfig`, `CaptureUnit` (verbatim prior bytes STRICTLY before mutate), `SaveConfig`, `ReconcileAndWrite`, `RestoreUnit`, `DaemonReload`, `Restart`, `Prove`, `InstallServiceName`. For coding mode swap the `FitsModel`/`PreflightROCm` fields for a coder-fit guard composed from `modelswap` (see below) + the residency-mode (`swap`/`shared`) selector.

**Result type to clone** (`backendswap.go:97-127`): `Refused`/`Switched`/`RolledBack`/`NoOp`/`Reason`/`Err`/`FailedStep` + carry `Prove`. Rename `FromBackend`/`ToBackend` → from/to model (or a `Direction enter|exit`).

**Run() ordering — the frame skeleton** (`backendswap.go:145-253`):
- (1) `LoadConfig`; same-state target → clean `NoOp` zero side effects (`:148-155`). Mirror for "already in coding mode → no change" (Open Question 3).
- (2) fit-guard FIRST, refuse-with-remediation BEFORE any capture/mutate (`:160-162`) — compose the coder fit at `AgentCtx` (Pitfall 4).
- (4) **CAPTURE strictly before mutate** (`:179-183`): `priorUnit, _ := d.CaptureUnit()`; `priorCfg := cfg` (flat value snapshot, no pointers).
- **rollback() helper** (`:191-210`): accumulates errors across RestoreUnit/SaveConfig/DaemonReload/Restart, returns `(ok, detail)` — clone VERBATIM (Pitfall 5).
- **rolledBack() helper** (`:214-231`): folds honest "rolled back, but the restore did not fully complete (…)" into `Reason` when `!ok`.
- (5) MUTATE (`:233-243`): `SaveConfig` → `ReconcileAndWrite` → `Restart` (inference-only); ANY error → `rolledBack(...)`.
- (6) PROVE (`:248-252`): `v := d.Prove(...)`; `if v.Status != ProveStatusPass { rolledBack("prove", v.Detail, nil, v) }`.

**Exit symmetry (D-08):** exit path captures the prior chat model (restored from before enter) and runs the SAME (4)→(6) frame — not a bare `coding_mode=false` write.

**Compose `modelswap` forward ordering (D-07, do NOT fork):** `internal/modelswap/modelswap.go` `Run` is `(1) resolve through catalog, (2) fit-guard refuse, (3) auto-pull if absent, (4) persist-before-unit-work, (5) reconcile, (6) restart-inference-only` (`modelswap.go:79-85`). Reuse its `Fits`/`Pull` closures for the model change inside the transactional frame.

---

### `cmd/villa/coding-mode.go` (cobra noun + live wiring, request-response) — CMODE-02

**Analog:** `cmd/villa/backend.go` (read in full).

**Backend-marker discipline header** (`backend.go:28-33`): this file must stay LITERAL-FREE of backend markers; they arrive ONLY through `inference.BackendFor(target).ResidencyProof()`. `TestSeamGrepGate` walks `cmd/villa`.

**Live `Prove` closure to REUSE essentially unchanged** (`backend.go:65-175`) — the single genuinely new composition; clone as the codingmode `Deps.Prove`:
- (a) bounded readiness `inference.PollHealth(deadlineCtx, endpoint, proveTimeout)` (`:100-106`); never-ready → fail.
- (b) REAL generation probe `inference.GenerationProbe` with `detect.GPUBusyPercent()` sampled DURING decode, keeping max (`:112-149`).
- (c) residency proof `inference.RunningOffloadVerdict(...)` fed `backend.ResidencyProof()` markers (`:158-167`). **For coding mode set `ConfigContext` = resolved `AgentCtx`** (the rendered `-c`), not `cfg.Ctx`, so residency fit-math matches.
- map ONLY `inference.StatusPass` → local `ProveStatusPass`; everything else → `"fail"` (`:171-174`).

```go
// backend.go:159-167 — the verdict assembly (set ConfigContext = resolved AgentCtx for coding mode)
v := inference.RunningOffloadVerdict(inference.RunningOffloadInput{
    JournalText:    journal,                 // orchestrate.NewSystemd().ResidencyJournal(installServiceName)
    GTTUsedBytes:   detect.GTTUsedBytes(),
    GPUBusyPercent: maxBusy,
    WeightBytes:    liveWeightBytes(cfg),
    ConfigModel:    modelFile,
    ConfigContext:  cfg.Ctx,                 // coding mode: pass resolved AgentCtx instead
    Markers:        backend.ResidencyProof(),
})
```

**Cobra noun shape** (`backend.go:187-289`): `newBackend()`/`newBackendSet()` — build the `coding-mode` noun with `enter`/`exit` subcommands. `RunE` calls a body that RETURNS the int, then `os.Exit(code)` (so tests assert output+code without a subprocess) (`:281-285`).

**Result→exit mapping** (`runBackendSet`, `backend.go:296-370`): clone the `switch` over `res.Refused`/`res.RolledBack`/`res.Err`/`res.NoOp`/default(Switched) → `exitBlocked`/`exitPass` with mirrored status/rollback messaging (Claude's-discretion: mirror `backend set` rendering).

**Live Deps wiring** (`liveBackendSwapDeps`, `backend.go:378-484`) — clone as `liveCodingModeDeps()`:
- `CaptureUnit` reads `villa-llama.container` from `quadletUnitDir()` (`:424-430`).
- `ReconcileAndWrite` renders via `orchestrate.Render(orchestrate.RenderInput{Backend, Cfg, ModelFile, ModelsDir})` → `Reconcile` → `WriteUnits` → `sys.DaemonReload()` (`:433-469`). NEEDS NO CHANGE beyond passing the cfg that now carries `coding_mode` — descriptor derivation lives in `orchestrate.Render` (D-05).
- `RestoreUnit` writes verbatim bytes through `orchestrate.WriteUnits` (`:472-479`).
- `Restart: sys.Restart`, `Prove: liveProve` (`:481-482`).

**Register** in `cmd/villa/root.go` alongside `newBackend()`.

---

### `internal/inference/inference.go` (RunSpec type, transform) — CMODE-01

**Analog:** the existing `RunSpec` struct (`inference.go:104-117`).

Append an OPTIONAL pointer field (Claude's-discretion name/shape) — pointer-nil = off path BY CONSTRUCTION (D-02):
```go
// inference.go:107-117 — RunSpec gains a new optional descriptor (zero/nil ⇒ byte-identical off)
type RunSpec struct {
    ContainerName string
    ModelFile     string
    ModelsDir     string
    ContextLen    int
    CodingMode    *CodingModeSpec // NEW (nil = off): --jinja / sampling / cache-reuse-safe gate
}
```
Define `CodingModeSpec` here carrying the sampling preset + `CacheReuseSafe bool`. Source the sampling shape from `internal/catalog/catalog.go:130-134` (`AgentSampling{Temperature float64; TopP float64; TopK int; RepeatPenalty float64}`). The agent ctx is NOT a separate field — it is delivered via `RunSpec.ContextLen = AgentCtx` (Pitfall 1: do not emit a second `-c`).

---

### `internal/inference/backend_vulkan.go` + `backend_rocm.go` (ContainerArgs append, transform) — CMODE-01

**Analog:** `backend_vulkan.go` `ContainerArgs` (`:81-103`) + `llamaServerFlags` (`:53`). The seam header (`:8-13`) states this is the ONLY file allowed backend/flag literals — the new `--jinja`/`--cache-reuse`/sampling literals MUST land HERE (seam-locked; `TestSeamGrepGate` stays green).

Existing assembly point to extend (the single `-c` already carries context — line 97):
```go
// backend_vulkan.go:101-102 — append the coding-mode delta AFTER llamaServerFlags
args = append(args, llamaServerFlags...)   // unchanged base: -ngl 999 -fa 1 --no-mmap -lv 4 --metrics
if cm := spec.CodingMode; cm != nil {      // nil ⇒ byte-identical off path (D-02)
    args = append(args, "--jinja")
    if s := cm.Sampling; s != nil {
        args = append(args, "--temp", ..., "--top-p", ..., "--top-k", ..., "--repeat-penalty", ...)
    }
    if cm.CacheReuseSafe {                 // fail-closed: false unless catalog declares it (D-03)
        args = append(args, "--cache-reuse", "256")
    }
}
return args
```
> Assumption A1 (RESEARCH §Assumptions): confirm `--temp`/`--top-p`/`--top-k`/`--repeat-penalty` + `--cache-reuse 256` against `llama-server --help` on the build-9496 dev box BEFORE freezing the on-path golden. `--jinja` and `--cache-reuse 256` are Phase-24-evidence-confirmed; sampling spellings are not yet grep-confirmed.

**ROCm symmetry:** apply the IDENTICAL delta block at the ROCm `ContainerArgs` assembly point (`backend_rocm.go`) so both backends render the delta behind the seam.

---

### `internal/orchestrate/render.go` / `orchestrate.go` (derive descriptor, transform) — CMODE-01

**Analog:** the existing `Render`/`RenderInput` flow (config→`RunSpec`→`in.Backend.ContainerArgs(spec)`), as exercised by `liveBackendSwapDeps().ReconcileAndWrite` (`cmd/villa/backend.go:446-451`).

**D-05 single point:** `Render` builds `RunSpec.CodingMode` from `in.Cfg` — when `cfg.CodingMode==true`, populate the descriptor (sampling/`CacheReuseSafe` resolved from the catalog coder entry at enter) AND set `spec.ContextLen = cfg.CoderAgentCtx` (Pitfall 1). When off, leave `CodingMode=nil` ⇒ off-path goldens unchanged. `ReconcileAndWrite(cfg)` stays the single config→unit point — no caller change.

---

### `internal/orchestrate/testdata/villa-llama-coding.container.golden` (NEW fixture) — CMODE-01

**Analog:** `internal/orchestrate/testdata/villa-llama.container.golden` (read in full). The on-path golden is a sibling with the `Exec=` line extended by `--jinja [sampling] [--cache-reuse 256]` and `-c` set to the agent ctx. The off-path goldens (`villa-llama.container.golden`, `villa-llama-rocm*.container.golden`) MUST stay byte-identical. Freeze the new golden intentionally with `go test ... -update` on the ON variant only.

Reference off-path `Exec` line (golden line 15) — the on-path delta extends exactly this:
```
Exec=llama-server -m /models/<coder>.gguf -c <agent_ctx> --host 0.0.0.0 --port 8080 -ngl 999 -fa 1 --no-mmap -lv 4 --metrics --jinja [--temp … --top-p … --top-k … --repeat-penalty …] [--cache-reuse 256]
```

---

### `internal/config/villaconfig.go` (append-only coding fields, CRUD) — CMODE-01/02 (D-04)

**Analog:** the `MemoryEnabled` field block + `marshalVilla` omit-when-off path (this file).

Field precedent (`villaconfig.go:60-79`):
```go
MemoryEnabled  bool   `toml:"memory_enabled,omitempty"`
EmbeddingModel string `toml:"embedding_model,omitempty"`
// … companions, all omitempty
```
Add append-only: `CodingMode bool` `toml:"coding_mode,omitempty"` + resolved `CoderModel`/`CoderQuant`/`CoderAgentCtx` (resolved AT ENTER, never re-picked), all `omitempty`.

Omit-when-off marshal precedent (`villaconfig.go:226-236`) — extend identically so a non-coding install is byte-identical on disk:
```go
func marshalVilla(c VillaConfig) ([]byte, error) {
    if !c.MemoryEnabled { c.EmbeddingModel = ""; /* …zero the 6 memory fields */ }
    if !c.CodingMode {     // NEW — same discipline (D-04)
        c.CoderModel = ""; c.CoderQuant = ""; c.CoderAgentCtx = 0
    }
    return toml.Marshal(c)
}
```
Extend the existing marshal-omit test (`TestMarshalOmitWhenOff`) to assert no coding keys when off.

## Shared Patterns

### Capture-before-mutate + honest rollback (Pitfall 5)
**Source:** `internal/backendswap/backendswap.go:179-231` (`CaptureUnit` before any mutation; `rollback()`/`rolledBack()` accumulate errors and fold an honest incomplete message).
**Apply to:** `internal/codingmode/codingmode.go` (both enter and exit) — clone verbatim.

### Backend-marker seam containment (`TestSeamGrepGate`)
**Source:** `internal/inference/backend_vulkan.go:8-13` (seam header); `internal/backendswap/backendswap.go:1-36` (local `ProveStatusPass`, no inference/detect import); `cmd/villa/backend.go:28-33` (markers only via `ResidencyProof()`).
**Apply to:** the new `--jinja`/`--cache-reuse`/sampling literals (must live in `internal/inference` only); `internal/codingmode` (no inference/detect import, own sentinel); `cmd/villa/coding-mode.go` (no marker literals).

### Under-load residency Prove (offload-asserting; idle-green = FAIL)
**Source:** `cmd/villa/backend.go:65-175` (`liveProve`: PollHealth + GenerationProbe + RunningOffloadVerdict; only `StatusPass`→pass).
**Apply to:** the codingmode cutover gate (`Deps.Prove`) — reuse essentially unchanged; set `ConfigContext` = resolved `AgentCtx`.

### Config-is-truth, omit-when-off, byte-identical-on-disk
**Source:** `internal/config/villaconfig.go:60-79` + `:226-236`.
**Apply to:** the new coding-mode config fields.

### Result→exit mapping (typed Result, no os.Exit in body)
**Source:** `cmd/villa/backend.go:296-370` (`runBackendSet` switch).
**Apply to:** `cmd/villa/coding-mode.go` enter/exit handlers.

### Fit-guard FIRST at agent ctx (Pitfall 4), forward ordering composed not forked
**Source:** `internal/modelswap/modelswap.go:79-85` (resolve→fit→pull→persist→reconcile→restart); `internal/recommend/coder.go` (`CoderFit.Residency` `swap`/`shared`, fit evaluated at `AgentCtx`).
**Apply to:** the codingmode enter path — compose `modelswap`'s fit/pull closures; key the enter path on `recommend.Pick(...).Coder.Residency` (D-10: `swap` does the model change, `shared` applies render-delta-only, never silently degrade).

## No Analog Found

None. Every file maps to an exact in-repo analog — Phase 25 is a composition/clone phase, not a discovery phase.

## Metadata

**Analog search scope:** `internal/backendswap`, `internal/modelswap`, `internal/inference`, `internal/orchestrate`, `internal/config`, `internal/catalog`, `internal/recommend`, `cmd/villa`.
**Files scanned (read this session):** `backendswap.go`, `cmd/villa/backend.go`, `backend_vulkan.go`, `inference.go`, `villaconfig.go` (marshal block), `villa-llama.container.golden`; `modelswap.go`, `catalog.go`, `recommend/coder.go` (targeted grep).
**Pattern extraction date:** 2026-06-13
