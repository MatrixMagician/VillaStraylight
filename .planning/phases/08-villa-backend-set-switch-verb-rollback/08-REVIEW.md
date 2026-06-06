---
phase: 08-villa-backend-set-switch-verb-rollback
reviewed: 2026-06-06T00:00:00Z
depth: deep
files_reviewed: 7
files_reviewed_list:
  - cmd/villa/backend.go
  - cmd/villa/backend_test.go
  - cmd/villa/root.go
  - internal/backendswap/backendswap.go
  - internal/backendswap/backendswap_test.go
  - internal/inference/prove.go
  - internal/inference/seam_test.go
findings:
  critical: 1
  warning: 5
  info: 3
  total: 9
status: issues_found
---

# Phase 8: Code Review Report

**Reviewed:** 2026-06-06
**Depth:** deep
**Files Reviewed:** 7
**Status:** issues_found

## Summary

Reviewed the `villa backend set` transactional switch verb across the pure core
(`internal/backendswap`), the cmd-tier live wiring (`cmd/villa/backend.go`), the new
exported prove primitives (`internal/inference/prove.go`), and the seam grep-gate.
The deep pass traced the prove chain through `liveProve` →
`inference.PollHealth`/`GenerationProbe`/`RunningOffloadVerdict` →
`scrapeLoadTensorsResidency`/`gttFloor`/`gpuBusyFloor`/`combineOffload`, and the
rollback state machine in `backendswap.Run`.

**The capture→mutate→prove→rollback frame itself is well-built.** Capture is strictly
before mutation (verified by both code and `TestCaptureBeforeMutate`); rollback restores
the verbatim captured unit bytes and the prior config snapshot; the prove gate maps ONLY
`inference.StatusPass` → `ProveStatusPass`, and a Known-zero `gpu_busy_percent` during a
claimed-healthy decode correctly FAILs (`gpuBusyFloor`). The `liveProve` goroutine/ticker
is sound: the chat-result channel is buffered (cap 1) so a `deadlineCtx.Done()` return
does not leak the probe goroutine, and `maxBusy` is touched only by the main goroutine
(the worker only sends a `ChatResult`), so there is no data race.

**However, there is one BLOCKER:** the ROCm preflight gate — a core BSET-01 safety
requirement — never executes on the only path that matters (a `vulkan → rocm` switch),
because `Run` passes the un-mutated config (still on the source backend) to a live
`PreflightROCm` closure that short-circuits for any non-rocm backend. No test catches
this because every test stub ignores the config argument.

## Critical Issues

### CR-01: ROCm preflight gate is dead code on a vulkan→rocm switch (BSET-01 bypass)

**File:** `internal/backendswap/backendswap.go:167` + `cmd/villa/backend.go:401-411`

**Issue:** The transactional core calls the preflight gate with the **pre-mutation**
config:

```go
// backendswap.go
from := cfg.Backend                       // e.g. "vulkan"
...
if ok, reason := d.PreflightROCm(cfg); !ok {   // cfg.Backend is STILL "vulkan" here
    return Result{Refused: true, ...}
}
...
cfg.Backend = target                      // mutation happens LATER (line 229)
```

The live `PreflightROCm` closure decides whether to run the ROCm checks by reading
`cfg.Backend`:

```go
// backend.go liveBackendSwapDeps
PreflightROCm: func(cfg config.VillaConfig) (bool, string) {
    if cfg.Backend != "rocm" {     // sees the SOURCE backend ("vulkan") → short-circuits
        return true, ""
    }
    for _, c := range preflight.RunROCm(detect.Probe()) { ... }
    return true, ""
},
```

On a `vulkan → rocm` switch, `cfg.Backend == "vulkan"` at the gate, so the closure returns
`true, ""` and `preflight.RunROCm` is **never called**. Because a `rocm → rocm` target is
intercepted earlier as a clean `NoOp` (backendswap.go:153), there is **no reachable path**
on which the ROCm preflight actually runs. The kernel/firmware/HSA-override safety checks
that BSET-01 requires before committing to ROCm are silently skipped, and the switch
proceeds to mutate config, regenerate the unit, and restart the inference service on a host
that may not satisfy the ROCm floor — exactly the failure the gate exists to prevent.

The bug is masked by the test suite: both `newSwapStub` (backendswap_test.go:69) and
`newBackendStub` (backend_test.go:49) wire `PreflightROCm` to a knob that **ignores its
`cfg` argument**, so the real `cfg.Backend != "rocm"` branch is never exercised. Note the
dry-run path in `runBackendSet` (backend.go:320-322) gets this **right** — it explicitly
builds `cfgTarget` with `cfgTarget.Backend = target` before calling `PreflightROCm` —
which makes the live switch and its own dry-run preview disagree about whether preflight
runs.

**Fix:** Set the target backend on the config snapshot passed to the preflight gate so the
closure sees the backend it is about to switch TO. Do it in the core so the contract holds
for any Deps wiring:

```go
// backendswap.go, step (3)
preCfg := cfg
preCfg.Backend = target
if ok, reason := d.PreflightROCm(preCfg); !ok {
    return Result{Refused: true, Reason: reason, FromBackend: from, ToBackend: target}
}
```

Add a regression test whose `PreflightROCm` stub asserts on the received `cfg.Backend`
(expecting `target`, not `from`), so the suite would catch a re-introduction.

## Warnings

### WR-01: FitsModel "re-check against the TARGET envelope" does not vary by target backend

**File:** `cmd/villa/backend.go:387-397` (live `FitsModel`)

**Issue:** The doc contract (BSET-01, backendswap.go:60-63) says FitsModel "re-checks the
PRESERVED model against the TARGET envelope." The live closure computes
`recommend.Pick(detect.Probe(), cat, Overrides{Model: cfg.Model})` — the envelope is
derived purely from `detect.Probe()` (host hardware) and is **independent of the target
backend**. Switching vulkan→rocm vs rocm→vulkan yields the identical fit verdict. If the
two backends have different effective memory envelopes (e.g. ROCm's documented 64 GB
allocation-cap behavior vs Vulkan RADV loading past 64 GB — see CLAUDE.md), the fit guard
will not catch a model that fits under Vulkan but not under the target ROCm cap. The guard
is therefore weaker than its stated contract.

**Fix:** Thread the target backend into the fit computation (e.g. apply a backend-specific
allocation ceiling to the usable envelope before `Pick`, or pass the target backend through
`recommend.Overrides`). At minimum, document that the current fit check is
backend-agnostic so the contract comment does not overstate the guarantee.

### WR-02: liveProve omits the /props drift overlay that the status path includes

**File:** `cmd/villa/backend.go:159-167` vs `cmd/villa/status.go:162-164`

**Issue:** `status.go` builds `RunningOffloadInput` with `Props: liveProps` so a
config-identity drift (loaded model/ctx != config.toml) downgrades a PASS to WARN.
`liveProve` constructs `RunningOffloadInput` **without** `Props` (it stays nil), so the
cutover prove never performs the drift check. The cutover is precisely the moment a
freshly-restarted server could be serving a stale/divergent model, yet the prove that
gates the cutover is more lenient than the passive `villa status` view. Because /props is
corroboration-only (it can downgrade PASS→WARN, and a WARN fails the gate → rollback),
including it would make the gate **stricter**, not falsely fail it.

**Fix:** Wire the same `liveProps(endpoint)` seam into `liveProve`'s
`RunningOffloadInput.Props` so the cutover prove and `villa status` apply identical
residency+drift criteria.

### WR-03: GTT-used floor is a host-wide counter — can false-PASS the sysfs signal under contention

**File:** `internal/inference/running_offload.go:212-241` (`gttFloor`), consumed by
`cmd/villa/backend.go:161` (`GTTUsedBytes: detect.GTTUsedBytes()`)

**Issue:** `gttFloor` PASSes when `mem_info_gtt_used >= weightBytes`. As the file's own
CR-03 note acknowledges, `mem_info_gtt_used` is a **host-wide** counter. If another GPU
consumer (a second container, a desktop compositor, a prior leaked allocation) already
holds ≥ the model's weight footprint in GTT, the floor PASSes even if the model under test
loaded entirely on CPU. The dual-assert mitigates this — `combineOffload` requires the
journal residency scrape to ALSO pass — so this alone will not produce a false cutover
PASS. But it does mean one of the two "independent" signals is not independent of host
state, weakening the defense-in-depth on a busy Strix Halo host.

**Fix:** Out of v1 scope to fully solve, but document the limitation at the `liveProve`
call site, and consider gating the gttFloor PASS on the journal residency signal already
being PASS (i.e. treat the floor as confirmation, not an independent vote) so a stale
host-wide reading cannot stand in for residency.

### WR-04: prove gate cannot distinguish "residency proven" from "residency unevaluable" — both block the switch, but only one is reported as a fallback stall

**File:** `cmd/villa/backend.go:100-106, 171-174`

**Issue:** `liveProve` maps everything that is not `inference.StatusPass` to a flat
`"fail"`. A WARN verdict from `RunningOffloadVerdict` — which by design means "could not
evaluate residency" (empty/unreadable journal, unreadable sysfs, weight==0) — is reported
to the user with the SAME `rolled back ... possible CPU-fallback stall` framing as a
confirmed FAIL. On a host where journald or amdgpu sysfs is simply unreadable, a perfectly
healthy GPU-resident switch is rolled back and the user is told it may be a silent CPU
fallback. Conservative (rollback on uncertainty) is the right default for a safety gate,
but the message misattributes an instrumentation gap as a fallback, sending the user to
debug the wrong thing.

**Fix:** Carry the underlying `Verdict.Status` (WARN vs FAIL) into `ProveVerdict.Detail`
and have `runBackendSet` distinguish "switch rolled back: residency could not be PROVEN
(instrumentation unavailable)" from "switch rolled back: GPU residency FAILED (silent CPU
fallback)". The verdict already carries a precise `Detail`; surface its WARN/FAIL origin.

### WR-05: double config load creates a TOCTOU window between Deps.LoadConfig and liveProve

**File:** `cmd/villa/backend.go:75` (`config.LoadVilla()` inside `liveProve`) vs the
`Deps.LoadConfig` used by `backendswap.Run`

**Issue:** `backendswap.Run` reads the source-of-truth config once via `d.LoadConfig`
(the captured `priorCfg`), then later `liveProve` independently re-reads config via
`config.LoadVilla()` to resolve the probe model id (`cfg.Model`) and residency seams. Between
the core's load and the prove's re-read, `Run` has already called `SaveConfig(cfg)` with
`cfg.Backend = target`. If `cfg.Model`/`cfg.Ctx` were concurrently edited (another `villa`
invocation, a manual config.toml edit) the prove would validate against a different model
than the one captured/mutated, and the residency drift/weight reference could be computed
against the wrong model. The transactional core's whole point is a single consistent
snapshot; re-loading inside the prove seam reopens it.

**Fix:** Pass the resolved config (or at least the probe model id + ctx + weight reference)
into the Prove seam rather than re-loading. Either extend the `Prove` signature to receive
the in-flight config, or capture the needed fields in the closure when `liveBackendSwapDeps`
is built.

## Info

### IN-01: rollback "detail" only reports the LAST failed step, silently overwriting earlier ones

**File:** `internal/backendswap/backendswap.go:186-205`

**Issue:** The `rollback` closure accumulates `ok` across all four restore steps (good —
it does not abort on first error), but `detail` is **reassigned** on each failure, so if
both `RestoreUnit` and `Restart` fail, only the `Restart` failure text survives. The
honest-incomplete-rollback message (Pitfall 5) then reports just one of potentially several
failures, understating how broken the stack is. `ok=false` is still correct, so the switch
is honestly flagged incomplete; only the diagnostic detail is lossy.

**Fix:** Append failures into a slice and join them (`strings.Join(details, "; ")`) instead
of overwriting, so a multi-step rollback failure reports every failed step.

### IN-02: proveTimeout magic-mirrored from an unexported inference const with no compile-time link

**File:** `cmd/villa/backend.go:41`

**Issue:** `proveTimeout = 5 * time.Minute` is documented as "seeded from inference's
`defaultReadyTimeout` (5m)" but is a hand-copied literal because the source is unexported.
If `inference.defaultReadyTimeout` changes, this silently drifts out of sync with no test
or compile error linking them.

**Fix:** Export the inference timeout (e.g. `inference.DefaultReadyTimeout`) and reference
it, or add a test asserting the two stay equal, so a future change to one forces the other.

### IN-03: GenerationProbe error detail can be empty, yielding a generic message

**File:** `cmd/villa/backend.go:133-139`

**Issue:** When the probe returns `!chat.OK` with `chat.Detail == ""`, the message falls
back to `"generation probe returned no tokens"`. `chatProbe` does set a `Detail` for the
zero-token case, so this is largely defensive, but the branch ordering means a future
`ChatResult{OK:false, Tokens:0, Detail:""}` would surface a misleading "no tokens" reason
for what may actually be a transport failure.

**Fix:** Minor — when `!chat.OK`, prefer reporting the failure unconditionally and only
distinguish the token count in the detail text.

---

_Reviewed: 2026-06-06_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
