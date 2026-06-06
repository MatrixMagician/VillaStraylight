# Phase 8: `villa backend set` Switch Verb + Rollback - Pattern Map

**Mapped:** 2026-06-06
**Files analyzed:** 4 new (2 source + 2 test)
**Analogs found:** 4 / 4 (all exact or strong role+data-flow matches; this is a composition phase)

This is a COMPOSITION phase. Every primitive already exists behind injectable `Deps` seams.
The planner should treat each new file as a near-mechanical clone of the named analog, plus
the ONE genuinely new mechanism: the capture→prove→rollback transactional frame.

## File Classification

| New File | Role | Data Flow | Closest Analog | Match Quality |
|----------|------|-----------|----------------|---------------|
| `internal/backendswap/backendswap.go` | service (pure orchestration core) | transactional / event-driven (capture→mutate→prove→rollback state machine) | `internal/modelswap/modelswap.go` | exact (sibling package, same `Deps`+`Result` idiom) |
| `internal/backendswap/backendswap_test.go` | test | table-driven over fake `Deps` (ordering + rollback asserts) | `internal/modelswap/modelswap_test.go` | exact (same `recorder` + `callOrder` ordering-contract style) |
| `cmd/villa/backend.go` | command (cobra noun) | request-response (RunE → os.Exit; body returns code) | `cmd/villa/model.go` (`newModel`/`newModelSwap`/`runModelSwap`/`liveSwapDeps`) | exact (same noun+subcommand+live-Deps wiring) |
| `cmd/villa/backend_test.go` | test | exit-code mapping + dry-run + show + JSON, fake `Deps` | `cmd/villa/model_test.go` (`newTestCmd`, `newSwapStub`, `TestModelSwapExitMapping`) | exact |

## Pattern Assignments

### `internal/backendswap/backendswap.go` (service, transactional)

**Analog:** `internal/modelswap/modelswap.go` (clone the skeleton; do NOT generalize `modelswap.Run`)

**Imports pattern** — keep the package literal-free of backend tokens (`ROCm0`/HSA/image
all live behind seams; `TestSeamGrepGate` will fail otherwise). Add `context` + `os` (capture)
over the modelswap import set:
```go
// modelswap.go:17-20 (extend with context + the inference/preflight/detect Result types
// the prove step needs — but NEVER the ROCm literals)
import (
	"context"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	// prove-verdict types come through a ProveVerdict the cmd layer fills; backendswap
	// stays free of inference/detect imports if ProveVerdict is locally defined.
)
```

**Deps struct pattern** (clone `internal/modelswap/modelswap.go:25-47`) — add the
capture/restore + prove fields the transactional frame needs:
```go
// EXTEND the modelswap.Deps shape (RESEARCH Pattern 1). Each host touch is a field.
type Deps struct {
	LoadConfig        func() (config.VillaConfig, error)
	FitsModel         func(cfg config.VillaConfig) (bool, string)   // recommend.Pick(Overrides{Model: cfg.Model})
	PreflightROCm     func(cfg config.VillaConfig) (ok bool, reason string) // wraps preflight.RunROCm; ok=false on any Fail
	CaptureUnit       func() ([]byte, error)                        // ★ os.ReadFile(<quadletDir>/villa-llama.container) — BEFORE mutate
	SaveConfig        func(c config.VillaConfig) error
	ReconcileAndWrite func(c config.VillaConfig) (changed bool, err error)
	RestoreUnit       func(b []byte) error                          // ★ atomic write of captured bytes (WriteUnits)
	DaemonReload      func() error
	Restart           func(service string) error
	Prove             func(ctx context.Context, target string) ProveVerdict // pollHealth + chatProbe + RunningOffloadVerdict
	InstallServiceName string                                       // "villa-llama.service"
}
```

**Result struct pattern** (clone `modelswap.go:51-77`) — add `RolledBack`, `FromBackend`,
`ToBackend`, `Prove`:
```go
type Result struct {
	Refused, Switched, RolledBack, NoOp bool
	Reason     string
	Err        error
	FailedStep string   // "capture"/"persist config"/"regenerate units"/"restart"/"prove"/"rollback"
	FromBackend, ToBackend string
	Prove      ProveVerdict
}
```

**Core ordering pattern** (the security contract) — `modelswap.go:85-142` is the forward
skeleton (resolve→guard→persist-FIRST→reconcile→restart-inference-only). Phase 8 WRAPS it:
```
(1) LoadConfig → cfg; from = cfg.Backend; same-backend → NoOp (modelswap.go:134 no-op style)
(2) FitsModel(cfg)          → !fits → Result{Refused} (modelswap.go:96-98, fit-guard FIRST)
(3) PreflightROCm(cfg)      → !ok  → Result{Refused} (target==rocm only)
(4) ★CAPTURE★ priorUnit,err := CaptureUnit(); err → Result{Refused, FailedStep:"capture"}  // BEFORE any mutate (Pitfall 4)
    priorCfg := cfg  // value copy; VillaConfig has no pointers (villaconfig.go:31-51)
(5) MUTATE: cfg.Backend = target; SaveConfig (modelswap.go:122 persist-FIRST);
            ReconcileAndWrite; (DaemonReload happens inside ReconcileAndWrite, see liveSwapDeps:383);
            Restart(InstallServiceName)   // any err here → ROLLBACK
(6) ★PROVE★ v := Prove(ctx, target); v.Status != StatusPass → ROLLBACK
            ROLLBACK: RestoreUnit(priorUnit); SaveConfig(priorCfg); DaemonReload; Restart; best-effort re-ready
                      → Result{RolledBack, FromBackend:from, ToBackend:target, Prove:v}
    PASS → Result{Switched, FromBackend:from, ToBackend:target}
```

**Restart-inference-only invariant** (copy verbatim from `modelswap.go:137`):
```go
if err := d.Restart(d.InstallServiceName); err != nil { ... }  // ONLY villa-llama.service
```

---

### `internal/backendswap/backendswap_test.go` (test, ordering + rollback)

**Analog:** `internal/modelswap/modelswap_test.go` (the `swapRecorder` + `callOrder` discipline)

**Recorder + call-order pattern** (clone `modelswap_test.go:18-79`):
```go
const installService = "villa-llama.service"
type swapRecorder struct {
	callOrder   []string
	saved       config.VillaConfig
	restored    [][]byte
	restarted   []string
	capturedAt  int          // index in callOrder when CaptureUnit fired
	proveFail   bool
}
// Each seam appends to callOrder so tests assert ordering (modelswap_test.go:64-77 style):
//   CaptureUnit → rec.callOrder = append(rec.callOrder, "capture")
//   SaveConfig  → "save:"+c.Backend ; ReconcileAndWrite → "write" ; Restart → "restart:"+svc
//   RestoreUnit → "restore" ; Prove → "prove"
```

**Required test cases** (map to RESEARCH §Phase Requirements → Test Map, lines 432-440):
| Test | Asserts | Modeled on |
|------|---------|-----------|
| `TestCaptureBeforeMutate` | `capture` index < `save`/`write` index in callOrder (Pitfall 4) | `modelswap_test.go:119-156` (pull<save<write<restart) |
| `TestRollbackVerbatim` | prove-fail → `restore` called with byte-equal `priorUnit`; `RolledBack` true | new (the transactional frame) |
| `TestProveGate` | `Prove` returns non-Pass → no `Switched`, rollback fires | new |
| `TestActiveNotSuccess` | a Prove verdict that is "ready+200 but residency FAIL" → rollback (SC#3) | new |
| `TestRefuse...` (fit / preflight) | refuse → ZERO side-effect seams in callOrder | `modelswap_test.go:96-114` (TestSwapFitGuardFirst) |
| `TestSwapInferenceOnly` | `restarted == ["villa-llama.service"]` only | `modelswap_test.go:152-155` |

---

### `cmd/villa/backend.go` (command, request-response)

**Analog:** `cmd/villa/model.go` — `newModel` (noun), `newModelSwap` (subcommand),
`runModelSwap` (Result→exit mapping), `liveSwapDeps` (host wiring)

**Noun + subcommand pattern** (clone `model.go:34-43` + `:239-254`):
```go
// RESEARCH Code Examples lines 322-335
func newBackend() *cobra.Command {
	backend := &cobra.Command{Use: "backend", Short: "...", Args: cobra.NoArgs}
	backend.AddCommand(newBackendShow(), newBackendSet())
	return backend
}
// newBackendSet: Args: cobra.ExactArgs(1); a --dry-run local bool flag (install.go:179 style)
//   RunE: code := runBackendSet(cmd, args[0], dryRun, liveBackendSwapDeps()); os.Exit(code); return nil
// Register in root.go AddCommand list alongside newModel(), newStatus().
```

**RunE → return-code (NOT os.Exit in the body) pattern** — copy `model.go:248-252` exactly;
the `runBackendSet` body RETURNS the int so tests assert output + code without a subprocess
(`model.go:68` doc). Exit constants live at `cmd/villa/preflight.go:19-21`:
`exitPass=0`, `exitWarn=2`, `exitBlocked=1`.

**Result→exit mapping pattern** (clone `runModelSwap` `model.go:260-305`): switch on the
typed `backendswap.Result` — `Refused`→`exitBlocked`+remediation; `Err`→`exitBlocked`+per-`FailedStep`
message; `RolledBack`→`exitBlocked`+"rolled back; prior backend restored"; `Switched`→`exitPass`.

**`--dry-run` pattern** (mirror `install.go:259-270`): preview {target, fit verdict, preflight
verdict}; write NOTHING — no SaveConfig/WriteUnits/Restart; return `exitPass`.

**`backend show` pattern** — active backend = `cfg.Backend` (source of truth); image tag via
`inference.BackendFor(cfg.Backend)` then `backend.Image()`. Mirror `internal/status/status.go:179-184`
which already does `inference.BackendFor(cfg.Backend)`. JSON via `json.NewEncoder` w/ indent
(copy `model.go:208-216`).

**Live Deps wiring** (clone `liveSwapDeps` `model.go:311-390`) — `liveBackendSwapDeps()`:
```go
sys := orchestrate.NewSystemd()
return &backendswap.Deps{
	InstallServiceName: installServiceName,                 // install.go:145 = "villa-llama.service"
	LoadConfig:         config.LoadVilla,                   // villaconfig.go:124
	SaveConfig:         config.SaveVilla,                   // villaconfig.go:150
	FitsModel: func(cfg config.VillaConfig) (bool, string) {// reuse model.go:323-337 verbatim, keyed on cfg.Model
		cat, _, err := catalog.Load(modelCatalogPath); if err != nil { return false, "catalog load failed" }
		rec := recommend.Pick(detect.Probe(), cat, recommend.Overrides{Model: cfg.Model})
		if rec.Fits { return true, "" }
		return false, fmt.Sprintf("needs %d bytes vs %d usable", rec.TotalBytes, rec.UsableEnvelopeBytes)
	},
	PreflightROCm: func(cfg config.VillaConfig) (bool, string) {
		if cfg.Backend != "rocm" { return true, "" }        // gate only applies to the rocm target
		for _, c := range preflight.RunROCm(detect.Probe()) { // checks_rocm.go:38
			if c.Status == preflight.StatusFail { return false, c.Detail } // preflight.go:94
		}
		return true, ""
	},
	CaptureUnit: func() ([]byte, error) {                   // RESEARCH lines 352-355
		dir, err := quadletUnitDir(); if err != nil { return nil, err } // install.go:749
		return os.ReadFile(filepath.Join(dir, "villa-llama.container"))
	},
	ReconcileAndWrite: /* copy model.go:351-387 verbatim — Render+Reconcile+WriteUnits+DaemonReload */,
	RestoreUnit: func(b []byte) error {                     // RESEARCH lines 356-361
		dir, err := quadletUnitDir(); if err != nil { return err }
		plan := orchestrate.Plan{Changed: []orchestrate.Unit{{Name: "villa-llama.container", Text: string(b)}}}
		return orchestrate.WriteUnits(plan, dir)            // reconcile.go:52 atomic temp-then-rename
	},
	DaemonReload: sys.DaemonReload,                         // systemd.go:93
	Restart:      sys.Restart,                              // systemd.go:131
	Prove:        liveProve,                                // see Shared Patterns / Prove
}
```

---

### `cmd/villa/backend_test.go` (test)

**Analog:** `cmd/villa/model_test.go` — `newTestCmd` (`:19-25`), `newSwapStub` (`:142-185`),
`TestModelSwapExitMapping` (`:229-...`)

**Captured-buffer command harness** (copy `model_test.go:19-25`):
```go
func newTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "set"}; var out, errOut bytes.Buffer
	cmd.SetOut(&out); cmd.SetErr(&errOut); return cmd, &out, &errOut
}
```

**Fake-Deps stub** (clone `newSwapStub` `model_test.go:142-185`) returning `*backendswap.Deps`
with closures over a recorder. **Required tests** (RESEARCH lines 438-439): `TestBackendShow`
(active backend + image tag), `TestBackendSetDryRun` (mutates nothing — assert no save/write/restart
in recorder), plus exit-mapping cases modeled on `TestModelSwapExitMapping` subtests
(`model_test.go:230-321`): refuse→exit 1, switched→exit 0, rolled-back→exit 1.

## Shared Patterns

### Prove composition (the ONE new wiring — pollHealth + chatProbe + RunningOffloadVerdict)
**Source:** `internal/inference/probe.go:52` (`pollHealth`), `:97` (`chatProbe`),
`internal/inference/running_offload.go:305` (`RunningOffloadVerdict`)
**Apply to:** the `Prove` Deps field's live impl (`liveProve`) — lives in `cmd/villa/backend.go`
(keeps backendswap literal-free). Bounded by `context.WithTimeout` (reuse the 5m
`defaultReadyTimeout`, `validate.go:25`, as the `load_tensors`-hang guard — Pitfall 2).
```go
// Compose (RESEARCH Pattern 3, lines 209-236). Three gates within a bounded deadline:
// (a) pollHealth ready  (b) chatProbe tokens>0  (c) RunningOffloadVerdict == StatusPass
v := inference.RunningOffloadVerdict(inference.RunningOffloadInput{   // running_offload.go:40-72
	JournalText:    journal,                       // sys.ResidencyJournal("villa-llama.service") systemd.go:199 (F-3 scoped)
	GTTUsedBytes:   detect.GTTUsedBytes(),          // memory.go:124
	GPUBusyPercent: detect.GPUBusyPercent(),        // gpu_amd.go:460 ← LIVE decode-time read (D-07, deferred to THIS phase)
	WeightBytes:    liveWeightBytes(cfg),
	ConfigModel:    modelFile, ConfigContext: cfg.Ctx,
	Markers:        backend.ResidencyProof(),        // backend_rocm.go:99 — ROCm0/"- ROCm"/fault; NEVER hardcoded in callers
})
// StatusPass/StatusFail are inference.Status (inference.go:121-129). is-active/200 alone NEVER counts (SC#3).
```
**Critical:** `BackendFor(target).ResidencyProof()` is the ONLY source of `ROCm0`/HSA/fault
literals (`backend.go:21` resolver, fail-closed). backendswap and backend.go must stay
literal-free — `TestSeamGrepGate` (`seam_test.go:34`) walks `internal/` and fails on any leak.

### Capture-before-mutate / verbatim restore (the transactional frame — NEW)
**Source:** `os.ReadFile` (capture) + `orchestrate.WriteUnits`/`atomicWrite` (`reconcile.go:52,68`)
**Apply to:** backendswap steps 4 (capture) + rollback. Capture is the LAST act before mutation;
an uncapturable prior unit → refuse, do not mutate (Pitfall 4). Restore reuses the existing
fsync + traversal-guarded atomic temp-then-rename writer — do NOT hand-roll (RESEARCH §Don't Hand-Roll).

### Fit re-check (preserve model — never re-pick)
**Source:** `recommend.Pick(detect.Probe(), cat, recommend.Overrides{Model: cfg.Model})` —
verbatim from `liveSwapDeps` `model.go:323-337`
**Apply to:** the `FitsModel` Deps field. Keyed on `cfg.Model` (the PRESERVED model), not a
swap target. `Fits=false` → refuse-with-remediation, zero side effects. Mutate ONLY `cfg.Backend`.

### systemd lifecycle seam (reload / restart / residency journal)
**Source:** `internal/orchestrate/systemd.go` — `NewSystemd()` (`:60`), `DaemonReload` (`:93`),
`Restart` (`:131`, fixed-arg `systemctl --user restart <svc>`), `ResidencyJournal` (`:199`,
invocation-scoped F-3). Fixed-arg, no shell (T-03-01). Do NOT `os/exec` directly.
**Apply to:** every backendswap systemd touch (mutate-restart, rollback daemon-reload/restart),
and the Prove journal read.

### Config source-of-truth (load / save)
**Source:** `config.LoadVilla` (`villaconfig.go:124`), `config.SaveVilla` (`:150`, 0600
traversal-guarded). `VillaConfig` (`:31-51`) is a flat value type (no pointers) → a plain
`priorCfg := cfg` is a safe deep snapshot for rollback.
**Apply to:** LoadConfig/SaveConfig Deps fields; mutate only `cfg.Backend`, persist BEFORE unit work.

## No Analog Found

None. Every new file has a strong in-repo analog; the only genuinely new code (capture/restore
frame + Prove composition) is assembled entirely from existing seams. No file needs to fall back
to RESEARCH.md generic patterns.

## Metadata

**Analog search scope:** `internal/modelswap/`, `internal/inference/`, `internal/orchestrate/`,
`internal/preflight/`, `internal/config/`, `internal/detect/`, `internal/status/`, `cmd/villa/`
**Files scanned (read in this session):** `modelswap.go`, `modelswap_test.go`,
`cmd/villa/model.go`, `cmd/villa/model_test.go`, `inference/backend.go`, `inference/backend_rocm.go`,
`inference/probe.go`, `inference/running_offload.go`, `inference/seam_test.go`,
`orchestrate/systemd.go`, `preflight/checks_rocm.go`, `config/villaconfig.go`,
plus targeted greps over `install.go`, `status.go`, `reconcile.go`, `inference.go`, `validate.go`.
**Pattern extraction date:** 2026-06-06
**Seam grep-gate to extend:** `inference/seam_test.go:34` (`TestSeamGrepGate`) — its walk already
covers `internal/backendswap/` (it walks all of `internal/` minus the seam), so the new package
is asserted literal-free automatically once created.
