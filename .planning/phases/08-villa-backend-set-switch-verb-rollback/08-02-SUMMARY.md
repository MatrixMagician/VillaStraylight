---
phase: 08-villa-backend-set-switch-verb-rollback
plan: 02
subsystem: cmd-villa
tags: [go, cobra, backend-set, liveProve, gpu-busy-decode, rollback, prove-gate, seam-grep-gate]

# Dependency graph
requires:
  - phase: 08-villa-backend-set-switch-verb-rollback
    provides: "internal/backendswap.Run transactional core (Deps/Result/ProveVerdict/ProveStatusPass) + EXPORTED inference.PollHealth/GenerationProbe + the cmd/villa-walking TestSeamGrepGate"
  - phase: 06-rocm-backend-resolver-spine
    provides: "inference.RunningOffloadVerdict + BackendFor(target).ResidencyProof() markers + detect.GPUBusyPercent()"
  - phase: 03-install-lifecycle
    provides: "liveSwapDeps render/reconcile/write closure + liveModelFile + liveWeightBytes + installServiceName/quadletUnitDir seams cloned here"
provides:
  - "cmd/villa `backend` cobra noun: `backend set <vulkan|rocm> [--dry-run]` + `backend show [--json]`, registered on root"
  - "liveProve — the bounded PollHealth + GenerationProbe + RunningOffloadVerdict cutover gate with the live detect.GPUBusyPercent() decode-time read (D-07)"
  - "liveBackendSwapDeps — full live host wiring driving the Plan-01 transactional core"
  - "runBackendSet Result→exit mapping (Refused/Err/RolledBack→1, Switched/NoOp→0; body returns the int)"
affects: [09-villa-bench, 10-surfacing]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Concurrent during-decode sampling: GenerationProbe runs in a goroutine while a ticker loop re-reads detect.GPUBusyPercent(), keeping the max — closes the D-07 silent-CPU-fallback window (a single post-probe read can miss a short decode)"
    - "Cmd-tier prove composition: compose the EXPORTED non-container PollHealth/GenerationProbe (never inference.Validate's --rm container) + RunningOffloadVerdict over BackendFor(target).ResidencyProof() markers, fed the SAME concrete liveWeightBytes/liveModelFile seams status.go uses"
    - "Literal-free cmd noun: all backend markers arrive only via ResidencyProof(); even the doc comment naming forbidden tokens is reworded to keep the cmd/villa-walking seam grep-gate green"

key-files:
  created:
    - cmd/villa/backend.go
    - cmd/villa/backend_test.go
  modified:
    - cmd/villa/root.go

key-decisions:
  - "proveTimeout is a named cmd-tier const (5m) mirroring inference's UNEXPORTED defaultReadyTimeout, documented as the load_tensors-hang bound (Pitfall 2 / T-8-07): a never-ready server trips the shared deadline context and is a PROVE FAIL → rollback, never an infinite wait."
  - "gpu_busy is sampled DURING the decode via a goroutine+ticker max-keeper (D-07 / Assumption A2), not a single post-probe read — a short completion would otherwise miss the busy window and let a silent CPU fallback pass."
  - "liveProve maps ONLY inference.StatusPass → backendswap.ProveStatusPass; any FAIL/WARN (incl. ready+health-200-but-residency-FAIL) is a 'fail' so the core rolls back (SC#3) — is-active/200 alone is never success."
  - "backend show reports the RESOLVED backend.Name() (not the raw cfg.Backend string) so an empty-config default surfaces as 'vulkan'; image tag via BackendFor(cfg.Backend).Image() mirrors status.go."
  - "PreflightROCm short-circuits ok=true for any non-rocm target and otherwise refuses on the FIRST preflight.StatusFail with that check's Detail as the remediation; the dry-run evaluates it against the WOULD-BE target backend (cfg with Backend=target), not the current one."

patterns-established:
  - "During-decode corroborator sampling (goroutine probe + ticker max-read) as the live realization of the Phase-6 fixture-driven gpu_busy fold."
  - "Cmd-tier transactional-core wiring cloned verbatim from liveSwapDeps (render/reconcile/write/daemon-reload closure) so the backend switch reuses the proven model-swap host seams with no new orchestration."

# Metrics
duration: 18min
completed: 2026-06-06
---

# Phase 8 Plan 02: villa backend Noun + liveProve Cutover Gate Summary

**Built the `villa backend set <vulkan|rocm> [--dry-run]` / `villa backend show` cobra surface and `liveProve` — the bounded `inference.PollHealth` + `inference.GenerationProbe` + `RunningOffloadVerdict` cutover gate that samples `detect.GPUBusyPercent()` DURING the decode (D-07) — wiring the Plan-01 transactional core to the real host while keeping `cmd/villa/backend.go` literal-free of backend markers.**

## Performance

- **Duration:** ~18 min
- **Tasks:** 2 of 2
- **Files created:** 2
- **Files modified:** 1

## Accomplishments

- `liveProve(ctx, target)` composes THREE required gates inside one bounded deadline (`proveTimeout`, 5m): (a) bounded readiness via the EXPORTED `inference.PollHealth` (never-ready → FAIL, the Pitfall-2 load_tensors-hang bound); (b) a REAL generation probe via the EXPORTED `inference.GenerationProbe` (tokens==0 / !OK → FAIL); (c) residency via `inference.RunningOffloadVerdict` fed `BackendFor(target).ResidencyProof()` markers + the SAME concrete `liveWeightBytes(cfg)` / `liveModelFile(cfg)` / `cfg.Ctx` seams `status.go` uses. Maps ONLY `inference.StatusPass` → `backendswap.ProveStatusPass` (SC#3).
- D-07 closed: `detect.GPUBusyPercent()` is sampled DURING the generation probe — the probe runs in a goroutine while a 100ms ticker loop re-reads gpu_busy and keeps the max, so a silent CPU fallback (0% busy during a claimed-healthy decode) is caught and rolled back, not missed by a single post-probe read.
- `villa backend` cobra noun: `set <backend> [--dry-run]` (ExactArgs(1)) + `show [--json]`, registered on root. `runBackendSet` previews target/fit/preflight on `--dry-run` (zero side effects) and otherwise maps `backendswap.Run`'s typed Result to exit codes (Refused/Err/RolledBack→1 with per-step/rollback messages, Switched/NoOp→0); the body RETURNS the int so tests assert output+code without a subprocess.
- `liveBackendSwapDeps` wires every host seam to the proven in-repo primitives: `config.LoadVilla/SaveVilla`, the `recommend.Pick` fit-math against the PRESERVED model, the ROCm preflight gate, verbatim unit capture/restore through the traversal-guarded `orchestrate.WriteUnits`, the render/reconcile/write closure cloned from `liveSwapDeps`, `sys.DaemonReload/Restart`, and `Prove: liveProve`.
- `cmd/villa/backend.go` stays literal-free of backend markers — the 08-01-03-extended `TestSeamGrepGate` (now walking `cmd/villa`) is green as a committed regression test.

## Task Commits

1. **Task 08-02-01: liveProve composition + live gpu_busy decode read (D-07)** — `d1c0ae5` (feat)
2. **Task 08-02-02: backend cobra noun (set/show/--dry-run), exit mapping, live Deps, tests, root registration** — `be5e690` (feat)

## Files Created/Modified

- `cmd/villa/backend.go` — `proveTimeout`/`busySampleInterval` consts; `liveProve` (3-gate bounded cutover prove with the during-decode gpu_busy sampling); `newBackend`/`newBackendShow`/`newBackendSet` cobra surface; `runBackendShow` + `runBackendSet` (Result→exit mapping); `liveBackendSwapDeps` live host wiring.
- `cmd/villa/backend_test.go` — `backendRecorder` + `newBackendStub` fake-`*backendswap.Deps`; `TestBackendRegistered`, `TestBackendShow` (human + `--json`), `TestBackendSetDryRun` (asserts zero mutate/capture/prove seams), `TestBackendSetExitMapping` (refused→1, switched→0, prove-fail rollback→1+restored, no-op→0, write-error rollback→1).
- `cmd/villa/root.go` — `newBackend()` added to the root `AddCommand` list.

## Decisions Made

See `key-decisions` in frontmatter — the load-bearing choices: the cmd-tier `proveTimeout` mirror, the during-decode goroutine+ticker gpu_busy sampling, the StatusPass-only mapping, the resolved-Name() show output, and the non-rocm-short-circuit/would-be-target preflight evaluation.

## Deviations from Plan

None — plan executed exactly as written. One faithful judgement call: the doc comment in `backend.go` that explains the backend-marker discipline was reworded to DESCRIBE the forbidden tokens (the per-backend residency device token / the HSA override env var / the GPU-fault abort string) rather than spell them, because the cmd/villa-walking `TestSeamGrepGate` matches its pattern anywhere in a file including comments — naming the literals verbatim would have failed the very gate the comment documents. The plan's intent (literal-free `backend.go`, markers only via `ResidencyProof()`) is preserved exactly.

## Issues Encountered

The initial `backend.go` header comment listed the forbidden marker literals verbatim to document the discipline; this would have tripped the cmd/villa-walking seam grep-gate (it scans comments too). Resolved by rewording the comment to describe the markers without the literal tokens — confirmed `grep -REn 'ROCm0|HSA_OVERRIDE|Memory access fault' cmd/villa/backend.go` returns nothing and `TestSeamGrepGate` is green.

## Verification

- `go test ./cmd/villa/ -run 'Backend' -count=1` — green (14 tests: registration, show human/json, dry-run-mutates-nothing, exit mapping incl. both rollback paths).
- `go test ./internal/inference/ -run 'TestSeamGrepGate|TestROCmMarkerPresence' -count=1` — green (the cmd/villa walk confirms `backend.go` is literal-free).
- `go test ./... -count=1` — green (504 tests, 18 packages; no regression — modelswap/status goldens byte-stable).
- `go vet ./...` — clean.
- `grep -REn 'ROCm0|HSA_OVERRIDE|Memory access fault' cmd/villa/backend.go` — no match.

## Known Stubs

None. `liveProve` and `liveBackendSwapDeps` are fully wired to the in-repo primitives; the only deferred validation is on-hardware UAT (per 08-VALIDATION.md Manual-Only): a real `villa backend set rocm` cutover, a forced-bad ROCm config rollback within `proveTimeout`, and a CPU-fallback bring-up classified FAIL — none of which are stubs, they are live-host assertions.

## Threat Flags

None — no new network endpoints, auth paths, file access patterns, or schema changes beyond the threat register. The `<backend>` arg crosses only into `inference.BackendFor` (fail-closed allowlist) and the fixed-arg orchestrate seams; capture/restore stay inside `quadletUnitDir()` via the traversal-guarded `orchestrate.WriteUnits`.

## Self-Check: PASSED

- FOUND: cmd/villa/backend.go
- FOUND: cmd/villa/backend_test.go
- FOUND: cmd/villa/root.go (modified — newBackend() registered)
- FOUND commit: d1c0ae5
- FOUND commit: be5e690
