# Phase 25: Coding-Mode Render & Transactional Swap Verb - Research

**Researched:** 2026-06-13
**Domain:** Composition of shipped Go control-plane cores (transactional swap + render-delta over the inference seam)
**Confidence:** HIGH

## Summary

Phase 25 is a **composition phase, not a discovery phase.** Every mechanism it needs already ships and is proven on the gfx1151 box: the `backendswap` transactional capture→mutate→prove→rollback frame, the `modelswap` forward ordering, the `inference.Backend` `ContainerArgs` assembly point (the only place `llama-server` flags are emitted), the `orchestrate.Render` pure renderer that sources every literal through the backend seam, the `config.VillaConfig` `omitempty`/omit-when-off memory-stack precedent, and the live `liveProve` closure (`PollHealth` + `GenerationProbe` + `RunningOffloadVerdict`) wired in `cmd/villa/backend.go`. The Phase-24 catalog already carries the schema-3 coder fields (`Role`, `AgentCtx`, `CacheReuseSafe`, `AgentSampling`, `TemplateProvenance`) and `recommend.Pick` already emits the `Coder` block with a `swap`/`shared` residency verdict. All three coder entries qualified PASS at `swap` residency on the pinned `vulkan-radv` digest (build 9496).

The job is therefore to ground the plan in the **exact existing patterns** and identify where the new coding-mode descriptor threads through. The two deliverables map cleanly: **CMODE-01** adds an optional coding-mode descriptor to `inference.RunSpec` (and `orchestrate.RenderInput`) that `backendVulkan/backendROCm.ContainerArgs` appends `--jinja` / `-c <agent_ctx>` / sampling tokens / `--cache-reuse` (gated on `cache_reuse_safe`) behind the seam — zero new `Backend`, zero new `BackendFor` branch, byte-identical off-path. **CMODE-02** clones `backendswap.Run` into a new `internal/codingmode` core that composes `modelswap`'s forward ordering inside the transactional frame, gated on the same `liveProve` residency discipline, exit symmetric to enter, explicit-verb-only (`villa coding-mode enter|exit`).

**Primary recommendation:** Clone `internal/backendswap` → `internal/codingmode` verbatim in shape (swap the "backend" axis for a "model + render-delta" axis), add an optional `CodingMode *CodingModeSpec` field to `RunSpec`/`RenderInput` that is zero/nil in the off path, append the delta flags inside `ContainerArgs` behind the existing seam, persist `coding_mode` + resolved coder fields in `config.toml` following the memory-stack `omitempty` precedent, and reuse the `liveProve` closure unchanged as the cutover gate. Add ONE new coding-mode-ON render golden (append-only); never touch the v1.3 off-path goldens.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Coding-mode flag emission (`--jinja`, sampling, `-c`, `--cache-reuse`) | inference (backend seam) | — | TestSeamGrepGate locks all backend/llama-server flag literals inside `internal/inference`; the descriptor threads in via `RunSpec`, the flags append in `ContainerArgs` |
| Threading the descriptor config→unit | orchestrate (render) | config | `RenderInput`→`Render`→`ContainerArgs(spec)`; `ReconcileAndWrite(cfg)` is the single config→unit point (D-05) |
| Coding-mode state (is-active + resolved coder) | config | — | Config is the single source of truth; units regenerate from it; survives restart (D-04) |
| Transactional enter/exit state machine | new `internal/codingmode` core | modelswap | Clones `backendswap` frame; composes `modelswap` forward ordering for the model change (D-07) |
| Under-load residency prove | cmd/villa (live `Prove` closure) | inference | Markers arrive only via injected `Prove` seam; `liveProve` composes `PollHealth`+`GenerationProbe`+`RunningOffloadVerdict` (D-09) |
| Verb surface, exit mapping, live wiring | cmd/villa (cobra noun) | — | Thin caller maps typed `Result`→exit codes; `liveCodingModeDeps()` wires seams (mirrors `liveBackendSwapDeps`) |
| Residency-mode selection (swap vs shared) | recommend (Phase-24 output) | new core | `recommend.Pick(...).Coder.Residency` drives enter path: swap does model change, shared applies render-delta only (D-10) |

## Standard Stack

This phase introduces **no new third-party dependencies.** It composes shipped first-party Go packages. The "stack" is the existing module set already in `go.mod` (cobra, chi, ghw, BurntSushi/toml, stdlib `testing`).

### Core (existing packages composed)
| Package | Role in Phase 25 | Why Standard |
|---------|------------------|--------------|
| `internal/backendswap` | Transactional frame to CLONE | The proven capture→mutate→prove→rollback state machine with honest rollback-incomplete (Pitfall 5) |
| `internal/modelswap` | Forward ordering to COMPOSE | resolve→fit-guard→pull→persist-before-unit-work→reconcile→restart-inference-only (the swap security contract, D-09) |
| `internal/inference` | `RunSpec`/`ContainerArgs` assembly point; `BackendFor` | The single place llama-server flags are emitted; seam-locked by `TestSeamGrepGate` |
| `internal/orchestrate` | `RenderInput`→`Render`→`ReconcileAndWrite` | Pure renderer that sources every literal through the backend seam |
| `internal/config` | `VillaConfig` + memory-stack `omitempty` precedent | Single source of truth; byte-identical-when-off marshal path |
| `internal/catalog` | Schema-3 coder fields (`Role`/`AgentCtx`/`CacheReuseSafe`/`AgentSampling`) | Already frozen (Phase 24); the render delta reads these |
| `internal/recommend` | `Coder CoderFit` block (`Residency` swap/shared) | Already emits the residency verdict the enter path keys on |

**Installation:** None. `go build ./...` only; no `go get`.

**Version verification:** N/A — no external packages added in this phase. The `go.mod` set is unchanged; the package legitimacy audit is therefore vacuously satisfied.

## Package Legitimacy Audit

> Not applicable — Phase 25 installs **zero** external packages. It composes already-vendored first-party `internal/*` packages and the standard library. No npm/PyPI/crates lookup is required. The pinned container image set (`kyuz0/amd-strix-halo-toolboxes`) is unchanged from v1.1/v1.3 (D-13 toolbox KEEP; nothing lands at the inference seam on account of the toolbox decision).

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram

```
  `villa coding-mode enter`                       `villa coding-mode exit`
            │                                               │
            ▼ (cobra, thin caller — cmd/villa/coding-mode.go)
   ┌──────────────────────────────────────────────────────────────┐
   │  liveCodingModeDeps()  — wires host seams (mirror             │
   │  liveBackendSwapDeps): LoadConfig/SaveConfig/CaptureUnit/     │
   │  ReconcileAndWrite/RestoreUnit/Restart/Prove(=liveProve)     │
   └──────────────────────────────────────────────────────────────┘
            │ Deps + target (enter|exit)
            ▼
   ┌──────────────────────────────────────────────────────────────┐
   │  internal/codingmode.Run  (CLONE of backendswap.Run)          │
   │                                                               │
   │  (1) LoadConfig → read coding_mode state; same-state = NoOp   │
   │  (2) resolve target coder model from recommend.Coder /        │
   │      captured prior chat model (exit); fit-guard (modelswap)  │
   │  (3) CAPTURE prior villa-llama.container bytes + prior cfg ───┐│
   │  (4) MUTATE: SaveConfig(coding_mode=true, resolved coder)     ││
   │      → ReconcileAndWrite(cfg) → Restart(villa-llama) ─────────┘│
   │  (5) PROVE under load (injected Prove seam) ──────────────────┐│
   │      pass → Switched ; non-pass/any mutate err → rollback ────┘│
   │      to verbatim captured unit+config (honest incomplete)      │
   └──────────────────────────────────────────────────────────────┘
            │ cfg.coding_mode → render-delta descriptor (D-05)
            ▼
   ┌──────────────────────────────────────────────────────────────┐
   │  orchestrate.Render(RenderInput{Cfg, Backend, ...})           │
   │    builds RunSpec{..., CodingMode: derive(cfg)}               │
   │    → in.Backend.ContainerArgs(spec)                           │
   └──────────────────────────────────────────────────────────────┘
            │
            ▼  (SEAM — internal/inference; TestSeamGrepGate locked)
   ┌──────────────────────────────────────────────────────────────┐
   │  backendVulkan/backendROCm.ContainerArgs(spec)                │
   │    base v1.3 args (BYTE-IDENTICAL when spec.CodingMode==nil)  │
   │    + IF coding-mode: --jinja, -c <agent_ctx>, sampling tokens,│
   │      --cache-reuse 256 (ONLY if cache_reuse_safe)             │
   └──────────────────────────────────────────────────────────────┘
            │
            ▼
     rendered villa-llama.container  (off-path = v1.3 golden;
     on-path = NEW append-only coding-mode-ON golden)

   PROVE seam (cmd/villa liveProve, reused unchanged):
     PollHealth(bounded) → GenerationProbe(tokens>0) →
     RunningOffloadVerdict(journal + GTT + gpu_busy + markers)
     StatusPass ⇒ ProveStatusPass ; anything else ⇒ rollback
```

### Recommended Project Structure
```
internal/codingmode/
├── codingmode.go        # NEW: clone of backendswap.go (Deps/Result/ProveVerdict; Run)
└── codingmode_test.go   # NEW: drives enter/exit/rollback off-host (clone backendswap_test.go shape)

internal/inference/
├── inference.go         # MODIFY: RunSpec gains optional CodingMode *CodingModeSpec; new type
├── backend_vulkan.go    # MODIFY: ContainerArgs appends delta flags behind the seam (off = byte-identical)
├── backend_rocm.go      # MODIFY: same delta append (ROCm path symmetric)
└── seam_test.go         # unchanged (must stay green; --jinja/--cache-reuse/sampling are NEW literals HERE)

internal/orchestrate/
├── orchestrate.go       # MODIFY: RenderInput → derive RunSpec.CodingMode from cfg
├── render.go            # MODIFY: Render builds the descriptor from in.Cfg (D-05 single point)
└── testdata/
    ├── villa-llama.container.golden            # UNCHANGED (off-path byte-identical)
    ├── villa-llama-rocm*.container.golden      # UNCHANGED
    └── villa-llama-coding.container.golden     # NEW append-only (coding-mode-ON variant)

internal/config/
└── villaconfig.go       # MODIFY: append-only coding-mode fields (omitempty) + marshalVilla omit-when-off

cmd/villa/
├── coding-mode.go       # NEW: thin cobra noun (enter/exit) + liveCodingModeDeps() + Result→exit
└── root.go              # MODIFY: register newCodingMode()
```

### Pattern 1: Optional descriptor threaded through RunSpec, appended in ContainerArgs (CMODE-01)
**What:** Add `CodingMode *CodingModeSpec` (pointer ⇒ nil/absent = off) to `inference.RunSpec`. `ContainerArgs` appends the delta flags ONLY when non-nil. Pointer-nil is the byte-identical-off guarantee BY CONSTRUCTION (D-02).
**When to use:** The whole CMODE-01 render delta.
**Example (the seam append — these literals live ONLY here, seam-locked):**
```go
// Source: internal/inference/backend_vulkan.go (ContainerArgs assembly point, extended)
// base args identical to v1.3 ... then:
args = append(args, llamaServerFlags...)   // -ngl 999 -fa 1 --no-mmap -lv 4 --metrics (unchanged)
if cm := spec.CodingMode; cm != nil {
    // -c is ALREADY emitted above from spec.ContextLen; for coding mode the caller
    // sets spec.ContextLen = cm.AgentCtx so KV is sized at agent ctx (Pitfall: KV@agent_ctx).
    args = append(args, "--jinja")
    if s := cm.Sampling; s != nil {
        args = append(args,
            "--temp", fmt.Sprintf("%g", s.Temperature),
            "--top-p", fmt.Sprintf("%g", s.TopP),
            "--top-k", fmt.Sprintf("%d", s.TopK),
            "--repeat-penalty", fmt.Sprintf("%g", s.RepeatPenalty))
    }
    if cm.CacheReuseSafe {        // fail-closed: false unless catalog declares it (D-03)
        args = append(args, "--cache-reuse", "256")
    }
}
return args
```
> NOTE [ASSUMED]: the exact llama-server flag spellings (`--temp`/`--top-p`/`--top-k`/`--repeat-penalty`, `--cache-reuse 256`) should be confirmed against the pinned build-9496 `llama-server --help` on the dev box before freezing the on-path golden. Phase-24 evidence (`cache-reuse.txt`) shows `--cache-reuse 256` was the probed form and `--jinja` is confirmed present (the `tools param requires --jinja` guard). Sampling flag names are the standard llama.cpp long forms but are not yet grep-confirmed in this session.

**Critical:** because `-c <ContextLen>` is already in the base args, do NOT emit a second `-c`. The coding-mode caller sets `RunSpec.ContextLen = AgentCtx` (via render deriving it from `cfg`), so the single `-c` carries the agent context. This keeps the off-path arg list byte-identical and avoids a duplicate flag.

### Pattern 2: Clone the transactional frame, swap the axis (CMODE-02)
**What:** `internal/codingmode.Run` is `backendswap.Run` with the "backend string" axis replaced by a "coding-mode target" axis (enter→coder model + delta; exit→restore captured chat model). Same ordering: same-state NoOp → fit-guard → CAPTURE-before-mutate → MUTATE (SaveConfig/ReconcileAndWrite/Restart-inference-only) → PROVE → rollback-on-any-failure with honest incomplete reporting.
**When to use:** Both enter and exit.
**Example (the frame skeleton to clone — pure, Deps-injected, literal-free):**
```go
// Source: internal/backendswap/backendswap.go Run() — clone shape verbatim
func Run(d Deps, dir Direction) Result {              // dir = enter | exit
    cfg, err := d.LoadConfig()
    // same-state (already in target mode) → clean NoOp, zero side effects
    // fit-guard the resolved coder model (enter) via the modelswap fit closure
    priorUnit, _ := d.CaptureUnit()                   // STRICTLY before any mutation
    priorCfg := cfg                                   // flat value snapshot
    // MUTATE: set cfg.CodingMode + resolved coder fields (enter) / restore prior (exit);
    //         SaveConfig → ReconcileAndWrite → Restart(InstallServiceName)
    // any mutate error → rollback() to verbatim priorUnit+priorCfg
    v := d.Prove(ctx, target)                          // injected; markers stay behind it
    if v.Status != ProveStatusPass { /* rollback verbatim */ }
    return Result{Switched: true, ...}
}
```

### Pattern 3: Persist mode state in config (D-04), derive descriptor in render (D-05)
**What:** Append-only `coding_mode bool` + resolved `coder_model`/`coder_quant`/`coder_agent_ctx` (resolved AT ENTER, never re-picked) to `VillaConfig`, all `omitempty`, with `marshalVilla` zeroing them when `coding_mode==false` so a non-coding install is byte-identical on disk (exact memory-stack precedent). `Render` derives `RunSpec.CodingMode` from these fields — `ReconcileAndWrite(cfg)` is the single config→unit point.
**Example (the marshal omit-when-off precedent to follow):**
```go
// Source: internal/config/villaconfig.go marshalVilla() — extend identically
func marshalVilla(c VillaConfig) ([]byte, error) {
    if !c.MemoryEnabled { /* zero the 6 memory fields so omitempty drops them */ }
    if !c.CodingMode {     // NEW — same discipline
        c.CoderModel = ""; c.CoderQuant = ""; c.CoderAgentCtx = 0
    }
    return toml.Marshal(c)
}
```

### Anti-Patterns to Avoid
- **New `Backend` implementation or `BackendFor` branch for coding mode** — coding mode is a render delta over the resolved backend, NOT a new backend (D-01). The grep gate and the dual ROCm/Vulkan render both depend on a single polymorphism point.
- **Putting `--jinja`/`--cache-reuse`/sampling literals in `cmd/villa` or `orchestrate`** — `TestSeamGrepGate` walks `internal/` + `cmd/villa`; these flag literals MUST live in `internal/inference` (the same class as the existing `llamaServerFlags`). The new `codingmode` core must be literal-free of backend markers (markers arrive only via the injected `Prove` seam).
- **Mutating an existing off-path golden** — the v1.3 `villa-llama*.container.golden` files are byte-frozen. Add a NEW coding-mode-ON golden; refreeze it intentionally with `go test ... -update`.
- **Bare config flip on exit** — exit is symmetric to enter (capture→cutover→prove→rollback), not a plain `coding_mode=false` write (D-08).
- **Auto-flipping the mode** — mode changes ONLY via the explicit verb (ROCm `backend set` precedent; "Out of Scope" forbids auto-switch). No automatic revert on agent exit.
- **Idle-green prove** — health-200 / is-active alone is NEVER a pass; the prove requires a real generation probe AND a positive `RunningOffloadVerdict` under load (D-09).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Transactional capture/rollback | A new state machine in `cmd/villa` | Clone `internal/backendswap.Run` | Honest rollback-incomplete reporting (Pitfall 5) and ordering are already proven + tested off-host |
| Forward swap ordering | A re-implemented resolve/fit/pull/persist sequence | Compose `internal/modelswap.Run` | The ordering IS the security contract (D-09); forking it duplicates the fit-guard-before-side-effect invariant |
| Under-load residency prove | A health-200 check | Reuse `liveProve` (`PollHealth`+`GenerationProbe`+`RunningOffloadVerdict`) | Silent-CPU-fallback detection is the whole point; idle-green is a false-green |
| Off-path byte-identity | Conditional template branches in `.tmpl` | Pointer-nil descriptor in `RunSpec` ⇒ no extra args | Byte-identity by construction; the renderer is unchanged, the seam decides |
| Mode persistence | An in-memory flag or hand-edited unit | `config.toml` field + regenerate units | Config is the single source of truth; survives restart/reboot |
| Sampling/flag literals placement | Strings in render/cmd | Append in `ContainerArgs` | `TestSeamGrepGate` enforces seam containment |

**Key insight:** Phase 25's correctness is almost entirely about *not* re-inventing what's shipped. The risky parts (rollback honesty, prove-under-load, seam containment, byte-identity) are already solved; the plan's job is wiring, not invention.

## Runtime State Inventory

> This is a feature-addition phase, not a rename/migration. No existing stored data, OS-registered state, or secrets are renamed. The relevant "runtime state" is the new persisted mode and the on-disk unit it regenerates.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `config.toml` gains append-only `coding_mode`/coder fields; omitted-when-off so existing installs are byte-identical on disk | code edit (config schema, omit-when-off marshal) |
| Live service config | `villa-llama.container` Quadlet unit is regenerated from config on enter/exit (the delta unit); captured verbatim before mutate, restored on rollback | code edit (render delta + capture/restore through existing orchestrate seams) |
| OS-registered state | None new — the existing `villa-llama.service` user unit is restarted (inference-only), not re-registered. No new systemd unit, no Task Scheduler analog. | None — verified: enter/exit restart the existing service only (D-07) |
| Secrets/env vars | None — coding mode adds no secret/env key. ROCm `HSA_OVERRIDE_GFX_VERSION` is backend-owned and unchanged. | None |
| Build artifacts | New `internal/codingmode` package + new golden fixture; standard `go build`/`go test -update` | reinstall (`make build`); golden refreeze on the on-path only |

**Nothing found in category:** OS-registered state and Secrets/env vars — verified by the fact that enter/exit reuse the existing `installServiceName` restart seam (no new unit) and add no env to `ContainerArgs` (the delta is flags only; ROCm env block is untouched).

## Common Pitfalls

### Pitfall 1: Duplicate `-c` flag breaks the golden
**What goes wrong:** Emitting a second `-c <agent_ctx>` in the coding-mode delta when `-c <ContextLen>` is already in the base args.
**Why it happens:** Treating the agent context as an additive flag rather than an override of the existing context value.
**How to avoid:** Set `RunSpec.ContextLen = AgentCtx` in `Render` when coding mode is on; the existing single `-c` carries it. Do NOT append a second `-c` in the delta block.
**Warning signs:** The on-path golden shows two `-c` tokens; `llama-server` would take the last one but the arg list is non-canonical.

### Pitfall 2: `--cache-reuse` rendered without the `cache_reuse_safe` gate
**What goes wrong:** Appending `--cache-reuse` unconditionally (or trusting an upstream claim).
**Why it happens:** The catalog field defaults to `false` (Go zero) and absence MUST mean "not safe" (fail-closed, D-03).
**How to avoid:** Gate strictly on `spec.CodingMode.CacheReuseSafe`, sourced from the catalog entry's `CacheReuseSafe`. This claim is **build-9496-scoped** — the Phase-24 probe proved all three entries safe via DeltaNet context checkpoints (Next) / GQA (30B), NOT generic KV-shift compatibility. If the toolbox digest is ever re-pinned, the `24-TOOLBOX-DECISION.md` Check 3 re-probe MUST run before trusting the flag.
**Warning signs:** A re-pinned image + an unchanged `cache_reuse_safe=true`; a journal warning `cache reuse is not supported - ignoring n_cache_reuse` on a model whose entry claims safe.

### Pitfall 3: jinja / tool-call template provenance drift
**What goes wrong:** `--jinja` activates the model's embedded chat template; a repo re-upload under the same quant name is a *different* artifact with a possibly different template, silently changing tool-call behavior.
**Why it happens:** The embedded GGUF chat template is part of the qualified artifact, but the model is pinned by repo+revision and verified by shard SHA-256; the template provenance is recorded separately (`TemplateProvenance`).
**How to avoid:** Do not introduce template handling in this phase — `--jinja` uses the embedded template of the already-pinned, shard-verified GGUF. The provenance pin (`TemplateProvenance`) is the Phase-24 guard; Phase 25 just renders `--jinja`. Surface no new template path.
**Warning signs:** Tool calls leaking as raw XML prose, malformed `tool_calls`, HTTP 500 on a tool request — all of which Phase-24 qualification proved absent on build 9496.

### Pitfall 4: Agent-scale KV blows the fit math
**What goes wrong:** KV at agent context (~6–12 GiB at 64–128k ctx) is far larger than chat-context KV; sizing at the chat ctx would under-reserve and OOM at container start.
**Why it happens:** The agent profile uses `AgentCtx`, not `default_ctx`.
**How to avoid:** The enter path's fit-guard (composed from `modelswap`'s fit closure / `recommend.Pick(...).Coder`) already evaluates the coder entry at `AgentCtx` — reuse it; do not re-derive fit at chat ctx. The residency verdict (`swap`/`shared`) is the fit-math output, never a preference.
**Warning signs:** `swap` chosen for an entry that only fits at chat ctx; container OOM at load_tensors despite a "fits" recommendation.

### Pitfall 5: Half-completed rollback reported as a clean no-op
**What goes wrong:** A rollback step (RestoreUnit/SaveConfig/DaemonReload/Restart) errors, but the result claims the stack is cleanly restored.
**Why it happens:** Not accumulating rollback-step errors.
**How to avoid:** Clone `backendswap`'s `rollback()`/`rolledBack()` helpers verbatim — they accumulate across all four steps and fold an honest "rolled back, but the restore did not fully complete" message into `Reason`.
**Warning signs:** A user sees "rolled back" but `villa status` shows the new (coder) model still served.

### Pitfall 6: `shared`-residency silently degrading a swap
**What goes wrong:** On a small envelope where no coder entry fits standalone, silently applying the render delta to the chat endpoint without surfacing that it's not a real model swap.
**Why it happens:** Treating `shared` as a transparent fallback.
**How to avoid:** `swap` is the primary v1.4 path (all 3 entries qualified at swap on gfx1151). In `shared` residency, apply the tool-calling render delta to the EXISTING chat-served endpoint WITHOUT a model change, still transactional and proved — but surface it explicitly (never silently degrade swap→shared). The exact `shared` operator UX is a planning detail; recommend implementing swap fully and shared as render-delta-only.
**Warning signs:** A 128 GB box silently riding the chat model when it could swap; a small box claiming a coder swap it never performed.

## Code Examples

### The live Prove closure to reuse unchanged (cutover gate)
```go
// Source: cmd/villa/backend.go liveProve() — reuse VERBATIM as codingmode Deps.Prove
// (a) bounded readiness, (b) real generation probe (tokens>0), (c) RunningOffloadVerdict
//     with gpu_busy sampled DURING decode; ONLY inference.StatusPass → ProveStatusPass.
v := inference.RunningOffloadVerdict(inference.RunningOffloadInput{
    JournalText:    journal,                      // invocation-scoped ResidencyJournal
    GTTUsedBytes:   detect.GTTUsedBytes(),
    GPUBusyPercent: maxBusy,                       // sampled during the probe
    WeightBytes:    liveWeightBytes(cfg),
    ConfigModel:    modelFile,
    ConfigContext:  cfg.Ctx,                        // for coding mode: agent ctx (resolved)
    Markers:        backend.ResidencyProof(),       // markers stay behind the seam
})
if v.Status == inference.StatusPass {
    return backendswap.ProveVerdict{Status: backendswap.ProveStatusPass, Detail: v.Detail}
}
return backendswap.ProveVerdict{Status: "fail", Detail: v.Detail}
```
> The codingmode core must define its OWN `ProveStatusPass`/`ProveVerdict` (as backendswap does) so it imports neither `inference` nor `detect` and stays marker-free. `liveProve` (or a coding-mode twin) maps `inference.StatusPass` into that local sentinel. When coding mode targets the agent context, `ConfigContext` must be the resolved `AgentCtx` so the residency fit math matches the rendered `-c`.

### The live ReconcileAndWrite closure to clone (config→unit, daemon-reload inside)
```go
// Source: cmd/villa/backend.go liveBackendSwapDeps().ReconcileAndWrite — clone verbatim.
// Render derives RunSpec.CodingMode from cfg, so this closure needs NO change beyond
// passing the same cfg (which now carries coding_mode). The descriptor derivation lives
// in orchestrate.Render (D-05), not here.
units, err := orchestrate.Render(orchestrate.RenderInput{
    Backend:   backend, Cfg: c, ModelFile: modelFile, ModelsDir: modelsDir(),
})
plan, _ := orchestrate.Reconcile(units, dir)
if len(plan.Changed) == 0 { return false, nil }
orchestrate.WriteUnits(plan, dir); sys.DaemonReload()
```

### The capture-before-mutate seam to clone (rollback fidelity)
```go
// Source: cmd/villa/backend.go liveBackendSwapDeps().CaptureUnit / RestoreUnit
CaptureUnit: func() ([]byte, error) {            // STRICTLY before any mutation
    dir, _ := quadletUnitDir()
    return os.ReadFile(filepath.Join(dir, "villa-llama.container"))
},
RestoreUnit: func(b []byte) error {               // verbatim restore on rollback
    dir, _ := quadletUnitDir()
    plan := orchestrate.Plan{Changed: []orchestrate.Unit{{Name: "villa-llama.container", Text: string(b)}}}
    return orchestrate.WriteUnits(plan, dir)
},
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Coding mode as a separate co-resident `villa-coder` unit | Transactional swap-based coding mode composing `modelswap` | v1.4 research (2026-06-12) | Co-resident deferred to CODER-V2-01 (128 GB fit-gated stretch); swap is the realized path |
| Qdrant tracks the codebase | Agent-native retrieval (LSP + ripgrep/glob) | v1.4 research | `villa-qdrant`/`villa-embed` untouched by v1.4; no embedding path in Phase 25 |
| `--cache-reuse` assumed unsafe on hybrid Next | Proven safe on build 9496 via DeltaNet context checkpoints | Phase 24 (2026-06-13) | All 3 entries `cache_reuse_safe=true`, build-9496-scoped |

**Deprecated/outdated:**
- Auto-switching models when the agent connects: explicitly out of scope (violates the ROCm explicit-verb precedent).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Sampling flag spellings `--temp`/`--top-p`/`--top-k`/`--repeat-penalty` and `--cache-reuse 256` are the build-9496 `llama-server` forms | Pattern 1 | Wrong flag name → llama-server ignores or errors on the arg; on-path golden bakes in a wrong literal. Verify against `llama-server --help` on the dev box before freezing the golden. |
| A2 | `--jinja` activates the embedded GGUF chat template with no extra template path needed | Pitfall 3 | If a `--chat-template` arg is required, the delta is incomplete. Phase-24 evidence shows `--jinja` alone drove the qualified tool-call loop, so risk is LOW. |
| A3 | Setting `RunSpec.ContextLen = AgentCtx` (single `-c`) is the right way to size agent KV vs. a separate flag | Pattern 1, Pitfall 1 | If a distinct ctx flag is expected the off/on goldens diverge unexpectedly. Mitigated by the existing single-`-c` base-arg structure. |
| A4 | The `shared`-residency render-delta-only path is acceptable for v1.4 with explicit surfacing | Pitfall 6, D-10 | If operators expect a real swap in shared mode, UX is wrong. CONTEXT D-10 marks the shared UX a planning detail; swap is the realized path on gfx1151. |

**If this table is non-empty:** A1 is the only assumption that should be closed with a quick on-hardware `llama-server --help` grep before the on-path golden is frozen; the rest are low-risk and grounded in Phase-24 functional evidence.

## Open Questions

1. **Exact `shared`-residency operator UX**
   - What we know: swap is the primary path (all 3 entries qualified at swap); shared applies the render delta to the chat endpoint without a model change, still transactional + proved.
   - What's unclear: the precise CLI messaging/flagging when an envelope only supports shared.
   - Recommendation: implement swap fully; shared = render-delta-only with an explicit surfaced notice; refine in planning if swap leaves it ambiguous. Never silently degrade swap→shared.

2. **Sampling flag spelling on build 9496**
   - What we know: `--jinja` and `--cache-reuse 256` are confirmed in Phase-24 evidence.
   - What's unclear: the exact long-form sampling flag names accepted by this build.
   - Recommendation: one dev-box `llama-server --help | grep -E 'temp|top-p|top-k|repeat'` before freezing the on-path golden (cheap, closes A1).

3. **Whether `coding-mode` is a `NoOp` when already in the target mode**
   - What we know: `backendswap` treats same-backend as a clean NoOp.
   - What's unclear: enter-while-already-coding and exit-while-already-chat semantics.
   - Recommendation: mirror the NoOp path (same-state = clean no-op, zero side effects); surface "already in coding mode — no change" like `backend set`.

## Environment Availability

> The phase is first-party Go code + a render delta over an already-pinned image. The only runtime dependencies are those the existing stack already requires (and which the dev host satisfies).

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | 1.26.2 | — |
| Pinned `vulkan-radv` toolbox (build 9496) | on-hardware enter/prove | ✓ (D-13 KEEP) | `sha256:9a74e555…` | — (digest unchanged; re-probe gate if ever re-pinned) |
| rootless Podman + `systemctl --user` | live enter/exit/restart | ✓ (dev host) | v5 | off-host tests use injected `Deps` (no live host) |
| gfx1151 GPU (`/dev/dri`) | residency prove | ✓ (dev host) | — | off-host tests stub `Prove` |

**Missing dependencies with no fallback:** none
**Missing dependencies with fallback:** off-host unit tests need no live host — every host action is an injected `Deps` field (the whole core is driven from `codingmode_test.go` without hardware, exactly as `backendswap_test.go` does).

## Validation Architecture

> `nyquist_validation: true` in `.planning/config.json` — this section is required.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven; `httptest`; byte-for-byte golden fixtures). No third-party assertion/mock lib — seams are injected `func` fields. |
| Config file | none (Go convention; `go test ./...`) |
| Quick run command | `go test ./internal/codingmode/... ./internal/inference/... ./internal/orchestrate/... ./internal/config/...` |
| Full suite command | `make check` (`go vet ./...` + `go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CMODE-01 | Addon-OFF render is byte-identical to v1.3 goldens | unit/golden | `go test ./internal/orchestrate/ -run TestRender` | ✅ (existing off-path goldens) |
| CMODE-01 | Seam grep-gate stays green (no `--jinja`/`--cache-reuse`/sampling leak outside `internal/inference`) | unit | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ (existing gate; new literals land inside seam) |
| CMODE-01 | Addon-ON renders `--jinja` + `-c <agent_ctx>` + sampling; `--cache-reuse` ONLY when `cache_reuse_safe` | unit/golden | `go test ./internal/inference/ -run TestContainerArgs` ; new `villa-llama-coding.container.golden` | ❌ Wave 0 (new on-path golden + ContainerArgs cases) |
| CMODE-01 | `--cache-reuse` absent when `cache_reuse_safe=false` (fail-closed) | unit | `go test ./internal/inference/ -run TestCacheReuseGate` | ❌ Wave 0 |
| CMODE-02 | Enter → prove-pass → coder model served (swap residency) | unit (Deps-driven) | `go test ./internal/codingmode/ -run TestEnter` | ❌ Wave 0 (clone backendswap_test) |
| CMODE-02 | Prove-FAIL (CPU fallback / residency FAIL) → verbatim rollback, prior unit+config restored | unit | `go test ./internal/codingmode/ -run TestProveFailRollback` | ❌ Wave 0 |
| CMODE-02 | Mutate error (save/write/restart) → verbatim rollback with honest rollback-incomplete | unit | `go test ./internal/codingmode/ -run TestMutateRollback` | ❌ Wave 0 |
| CMODE-02 | Exit → chat model restored under same transactional discipline (symmetric) | unit | `go test ./internal/codingmode/ -run TestExitRestoresChat` | ❌ Wave 0 |
| CMODE-02 | Same-state enter/exit is a clean NoOp (zero side effects) | unit | `go test ./internal/codingmode/ -run TestNoOp` | ❌ Wave 0 |
| CMODE-02 | Mode never auto-flips (explicit verb only) | unit | grep/structural: no caller mutates `coding_mode` outside `codingmode.Run` | ❌ Wave 0 (structural guard) |
| CMODE-01/02 | Config off-path byte-identical on disk (no coding keys when off) | unit | `go test ./internal/config/ -run TestMarshalOmitWhenOff` | ❌ Wave 0 (extend memory-omit test) |

### Sampling Rate
- **Per task commit:** `go test ./internal/codingmode/... ./internal/inference/... ./internal/orchestrate/... ./internal/config/...`
- **Per wave merge:** `make check`
- **Phase gate:** Full `make check` green (incl. `TestSeamGrepGate`, all goldens) before `/gsd-verify-work`; on-hardware enter→prove→exit smoke on the gfx1151 box as the acceptance checkpoint (swap residency, real generation + residency proof).

### Wave 0 Gaps
- [ ] `internal/codingmode/codingmode_test.go` — covers CMODE-02 (enter/exit/rollback/NoOp), clone of `backendswap_test.go`
- [ ] `internal/inference/` ContainerArgs coding-mode cases — covers CMODE-01 on-path flags + `cache_reuse_safe` gate
- [ ] `internal/orchestrate/testdata/villa-llama-coding.container.golden` — NEW append-only on-path golden (CMODE-01)
- [ ] `internal/config/` marshal-omit-when-off extension — covers byte-identical-on-disk for coding fields
- [ ] Structural guard: no `coding_mode` mutation outside `codingmode.Run` (explicit-verb-only invariant)
- [ ] Framework install: none — `go test` is already the suite

## Security Domain

> `security_enforcement` is not disabled in config. The phase's security surface is narrow (no new external input, no new bind, no new secret) but the privacy/seam invariants are load-bearing.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | No auth surface added (loopback-only inference, unchanged) |
| V3 Session Management | no | No sessions |
| V4 Access Control | no | No new access surface |
| V5 Input Validation | yes | Model name is catalog-resolved (never shell-interpolated); coder fields come from the frozen catalog, not user input; `--cache-reuse` gated on a validated catalog bool |
| V6 Cryptography | no | No crypto in this phase (GGUF shard SHA-256 verification is the unchanged download path) |
| V12 (file handling / path) | yes | Unit capture/restore goes through the traversal-guarded `orchestrate.WriteUnits`; config writes through `assertInsideDir` (unchanged seams) |

### Known Threat Patterns for the Go control plane

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Backend marker / image literal leaking into `cmd/villa` or the new core | Tampering / Information Disclosure | `TestSeamGrepGate` (walks `internal/` + `cmd/villa`); the codingmode core defines its own `ProveStatusPass` and imports neither `inference` nor `detect` |
| Shell injection via model/flag args | Tampering | Fixed-arg `exec.Command` only; catalog-resolved model name; no string interpolation (existing invariant, preserved) |
| Routable bind regression | Information Disclosure | Loopback-only host publish is unchanged (the delta is flags, not publish/bind); `normalizeVilla` never widens binds |
| Silent CPU fallback presented as success | Spoofing (false-green) | Offload-asserting prove: real generation + `RunningOffloadVerdict`; idle-green = FAIL → rollback |
| Stale `cache_reuse_safe` after a toolbox re-pin | Tampering (silent capability drift) | build-9496-scoped claim; `24-TOOLBOX-DECISION.md` Check 3 re-probe gate on any re-pin |
| Mode change without operator intent | Elevation / unexpected behavior | Explicit-verb-only (no auto-flip); out-of-scope list forbids auto-switch |

## Sources

### Primary (HIGH confidence)
- `internal/backendswap/backendswap.go` — transactional frame (Deps/Result/ProveVerdict/Run; rollback honesty) — read in full this session
- `internal/modelswap/modelswap.go` — forward ordering (resolve→fit→pull→persist→reconcile→restart) — read in full
- `internal/inference/inference.go`, `backend.go`, `backend_vulkan.go`, `backend_rocm.go`, `seam_test.go` — Backend iface, RunSpec, ContainerArgs assembly, BackendFor, grep gate — read in full
- `internal/orchestrate/orchestrate.go`, `render.go` — RenderInput/Render/parseContainerArgs; config→unit single point — read in full
- `internal/config/villaconfig.go` — VillaConfig, memory-stack omit-when-off marshal precedent — read in full
- `cmd/villa/backend.go` — `liveProve`, `liveBackendSwapDeps`, Result→exit mapping (the verb template) — read in full
- `cmd/villa/model.go` (liveSwapDeps) — forward-swap live wiring — read
- `internal/catalog/catalog.go`, `internal/recommend/coder.go` — schema-3 coder fields + Coder residency block — read
- `internal/orchestrate/testdata/villa-llama.container.golden` — the off-path byte-frozen contract — read
- `.planning/phases/25-.../25-CONTEXT.md` (D-01..D-10) — locked decisions — read in full
- `.planning/phases/24-.../24-TOOLBOX-DECISION.md` (D-13, Check 3 re-probe gate) — read in full
- `.planning/REQUIREMENTS.md` (CMODE-01/02), `.planning/STATE.md` §Blockers — read

### Secondary (MEDIUM confidence)
- Phase-24 qualification evidence (`cache-reuse.txt` / verdicts) as summarized in `24-TOOLBOX-DECISION.md` — `--jinja` present, `--cache-reuse 256` probed, all 3 entries safe on build 9496

### Tertiary (LOW confidence)
- Exact llama-server sampling flag spellings (A1) — from training knowledge of llama.cpp; flagged for a dev-box `--help` confirmation before golden freeze

## Metadata

**Confidence breakdown:**
- Standard stack (composed packages): HIGH — every source file read in full this session; no external deps added
- Architecture (descriptor threading + transactional clone): HIGH — directly mirrors shipped `backendswap`/`modelswap`/`ContainerArgs` patterns with explicit CONTEXT decisions
- Pitfalls: HIGH — sourced from STATE.md blockers, the toolbox decision record, and the seam/golden invariants in the code
- Render flag spellings: MEDIUM — `--jinja`/`--cache-reuse 256` confirmed via Phase-24 evidence; sampling flag names LOW (A1, verify on dev box)

**Research date:** 2026-06-13
**Valid until:** 2026-07-13 (stable — first-party composition; the only volatility is a toolbox re-pin, which would re-open the `cache_reuse_safe` claim via the Check 3 gate)

## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Tool-calling delta carried by an OPTIONAL coding-mode descriptor threaded through `RunSpec` (and `orchestrate` `RenderInput`), populated only when coding mode is active: agent ctx (→ `-c`), catalog sampling-preset tokens, `--jinja` on, `--cache-reuse` gated on `cache_reuse_safe`. Appended in the existing `ContainerArgs` behind the seam. NO new `Backend`, NO new resolver branch.
- **D-02:** Addon-off byte-identical BY CONSTRUCTION: zero/absent descriptor ⇒ args + rendered unit emitted exactly as v1.3; existing goldens MUST NOT change off-path. Flag literals live in `internal/inference`; `TestSeamGrepGate` stays green. A new coding-mode-ON golden is append-only, never a mutation of an existing one.
- **D-03 (carried, Phase 24):** `--cache-reuse` appended ONLY where the catalog entry declares `cache_reuse_safe=true` (absent ⇒ false, fail-closed). build-9496-scoped — re-probe per `24-TOOLBOX-DECISION.md` Check 3 if the toolbox is re-pinned.
- **D-04:** "Coding mode active" persisted in `config.toml` as append-only OPTIONAL fields (memory-stack precedent): `coding_mode` bool + resolved active coder model/quant/agent_ctx (resolved AT ENTER). All `omitempty`/omit-when-off ⇒ byte-identical on disk. Units regenerate from config; survives restart/reboot. Never a hand-edited unit, never an ephemeral flag.
- **D-05:** Render/reconcile derives the descriptor from the persisted config fields; `ReconcileAndWrite(cfg)` is the single point that turns "coding_mode=true" into the rendered tool-calling unit.
- **D-06:** Verb shape `villa coding-mode enter` / `villa coding-mode exit` (two explicit subcommands). NOT `villa code` (reserved for Phase 26). Mode changes ONLY via this verb; nothing auto-flips. Core returns a typed `Result`; cobra maps to exit code + messages.
- **D-07:** New pure, `Deps`-injected core (e.g. `internal/codingmode`) that CLONES the `backendswap` frame verbatim in shape: capture prior unit bytes + prior config STRICTLY before any mutation → mutate (persist, reconcile+write, restart inference only) → under-load Prove → ANY mutate error/non-pass rolls back verbatim with honest rollback-incomplete. Composes `modelswap`'s forward ordering. LITERAL-FREE of backend markers. Do NOT fork `modelswap`; do NOT inline into `cmd/villa`.
- **D-08:** Exit restores the chat model (captured prior model from before enter) under the SAME transactional discipline. Symmetric to enter, not a bare config flip.
- **D-09:** Cutover Prove reuses the `backendswap` residency discipline: real generation probe AND positive `RunningOffloadVerdict` under load; success ONLY on `ProveStatusPass`. Silent/partial CPU fallback or ready+health-200-but-residency-FAIL ⇒ rollback. Idle-green / is-active / health-200 alone is NEVER success. Real tool-call round-trip DEFERRED to Phase 27.
- **D-10:** Residency mode (Phase-24 output) drives the enter path: `swap` ⇒ perform the model swap (realized path on gfx1151, all 3 entries PASS at swap); `shared` ⇒ apply render delta to the existing chat endpoint WITHOUT a model change (still transactional, still proved). `swap` primary for v1.4; never silently degrade swap→shared.

### Claude's Discretion
- Exact Go package name (`internal/codingmode` vs `internal/modeswap`); field/JSON/TOML key names for the new config fields (follow memory-stack `omitempty` precedent; append-only).
- Exact `RunSpec`/`RenderInput` descriptor field names and shape for the coding-mode delta.
- Golden test organization for the coding-mode-ON render variant (`-update` discipline; off-path goldens untouched).
- Human-readable CLI output of `villa coding-mode enter|exit` (status lines, rollback messaging) — mirror `backend set`.
- Precise composition of the live `Prove` closure (PollHealth + GenerationProbe + RunningOffloadVerdict) in `cmd/villa`.

### Deferred Ideas (OUT OF SCOPE)
- Real tool-call round-trip as a readiness gate, egress-zero + llama-down negative controls — Phase 27 (`villa verify agent`). This phase's prove is residency+generation only.
- Crush delivery / SHA-256 pin policy / `crush.json` render / `villa code` launcher / env lockdown — Phase 26.
- `status`/dashboard/doctor/backup surfacing of the coding feature (`status.Report` 3→4) — Phase 28.
- Co-resident `villa-coder` Quadlet unit (no swap) — CODER-V2-01 (v2 stretch, deferred).
- Exact `shared`-residency operator UX beyond render-delta-only — refine in planning if the swap path leaves it ambiguous; do not silently degrade swap→shared.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CMODE-01 | Coding mode renders a tool-calling-ready llama-server unit delta (`--jinja`, agent ctx, sampling preset, `--cache-reuse` where model-compatible) behind the inference/orchestrate seams; addon-off renders byte-identical to v1.3 | Pattern 1 (optional `RunSpec.CodingMode` descriptor appended in `ContainerArgs` behind the seam; pointer-nil = byte-identical off); Pattern 3 (config-derived descriptor via `Render`/`ReconcileAndWrite`); seam containment via `TestSeamGrepGate`; new append-only on-path golden; `cache_reuse_safe` gate (Pitfall 2, D-03) |
| CMODE-02 | Enter/exit coding mode via a transactional verb composing `modelswap` (capture → under-load residency prove → cutover → verbatim rollback), chat model restored on exit | Pattern 2 (clone `backendswap.Run`, compose `modelswap` forward ordering); the `liveProve` cutover gate reused unchanged (Code Examples); capture-before-mutate + honest rollback (Pitfall 5); explicit-verb-only noun mirroring `cmd/villa/backend.go`; exit symmetric to enter (D-08); residency-driven enter path swap/shared (D-10) |

## Project Constraints (from CLAUDE.md)
- **Pure-core + injectable-seam:** the new `codingmode` core is pure and `Deps`-injected; live wiring lives in `cmd/villa` via a `liveCodingModeDeps()` closure. No host I/O in the core.
- **Config is the single source of truth:** mode state in `config.toml`; Quadlet units regenerated from config, never hand-edited.
- **Seam-locked backend literals (`TestSeamGrepGate`):** `--jinja`/`--cache-reuse`/sampling tokens and all backend markers stay inside `internal/inference`; the gate walks `internal/` + `cmd/villa`. The new core must import neither `inference` nor `detect` and define its own prove sentinel.
- **Byte-frozen goldens evolve append-only:** off-path `villa-llama*.container.golden` untouched; add a NEW coding-mode-ON golden, refreeze intentionally with `go test ... -update`.
- **Offload-asserting (silent CPU fallback = FAIL):** the prove requires real generation + `RunningOffloadVerdict`; idle-green is never green → rollback.
- **No shell interpolation:** fixed-arg `exec.Command`; model name catalog-resolved.
- **Loopback-only binds:** unchanged; the delta is flags, not publish/bind.
- **Dashboard restart trap:** not relevant this phase (no dashboard surfacing — Phase 28), but note `make build` + `systemctl --user restart villa-dashboard.service` for any future dashboard code change.
- **Single static binary; no new external deps.**
