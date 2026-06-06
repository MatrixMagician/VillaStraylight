---
phase: 08-villa-backend-set-switch-verb-rollback
plan: 01
subsystem: orchestration
tags: [go, backendswap, transactional-rollback, prove-gate, seam-grep-gate, rocm, vulkan]

# Dependency graph
requires:
  - phase: 03-install-lifecycle
    provides: "internal/modelswap forward swap skeleton (fit-guard-first, persist-before-unit, restart-inference-only) + config.VillaConfig flat value type"
  - phase: 06-rocm-backend-resolver-spine
    provides: "RunningOffloadVerdict residency proof + Backend interface seam that Plan-02 liveProve composes"
  - phase: 07-rocm-render-unit-preflight-detect
    provides: "inference probe.go pollHealth/chatProbe private running-server primitives + the seam grep-gate this plan extends"
provides:
  - "internal/backendswap: Deps/Result/ProveVerdict + Run transactional capture->mutate->prove->rollback state-machine (the heart of Phase 8)"
  - "ProveStatusPass package-local success sentinel keeping backendswap literal-free of backend tokens"
  - "internal/inference EXPORTED non-container running-server prove primitives PollHealth + GenerationProbe (closes the Plan-02 liveProve BLOCKER on the unexported pollHealth/chatProbe)"
  - "seam grep-gate extended to walk cmd/villa, making the cmd-tier backend-literal-free property a committed regression test"
affects: [08-02-cmd-villa-backend-noun, 09-villa-bench, 10-surfacing]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Transactional rollback frame wrapped around a cloned forward swap skeleton (capture-before-mutate, verbatim restore, best-effort bounded re-ready)"
    - "Local prove-verdict value type + own status sentinel to keep a pure core import-free of inference/detect and literal-free of backend markers"
    - "Two-walk seam grep-gate: internal/ (seam-allowlisted, full pattern set) + cmd/villa (backend-marker subset, podman-process pattern excluded as the legitimate OS-orchestration tier)"

key-files:
  created:
    - internal/backendswap/backendswap.go
    - internal/backendswap/backendswap_test.go
    - internal/inference/prove.go
  modified:
    - internal/inference/seam_test.go

key-decisions:
  - "ProveVerdict is defined LOCALLY in backendswap (not imported from inference) with its own ProveStatusPass='pass' sentinel, so the core imports neither inference nor detect and stays literal-free of backend markers; the cmd layer maps inference.StatusPass into it."
  - "The cutover switches ONLY on ProveStatusPass; any other verdict (including ready+health-200-but-residency-FAIL) rolls back — is-active/200 alone is never success (SC#3)."
  - "Rollback is best-effort with accumulated errors across RestoreUnit->SaveConfig->DaemonReload->Restart; an incomplete rollback is flagged honestly in Result.Reason rather than masked as a clean no-op (Pitfall 5)."
  - "The seam grep-gate's cmd/villa walk EXCLUDES the podman-invocation pattern: cmd/villa is the legitimate OS-orchestration tier (lifecycle/uninstall fixed-arg podman calls), so the cmd-tier gate enforces only backend-marker neutrality (GOOS/image/device/ROCm0-HSA-fault), which is what guards the Plan-02 backend.go noun."

patterns-established:
  - "Capture-before-mutate transactional core: capture verbatim prior unit bytes + value-snapshot config strictly before SaveConfig/ReconcileAndWrite/Restart, asserted by callOrder index ordering."
  - "Refuse-with-remediation with zero side-effect seams: fit-guard FIRST then ROCm preflight, both before any capture/mutate."

requirements-completed: [BSET-01, BSET-02]

# Metrics
duration: 14min
completed: 2026-06-06
---

# Phase 8 Plan 01: backendswap Transactional Core + Exported Prove Primitives Summary

**Built `internal/backendswap` — the pure, Deps-injected capture→mutate→prove→rollback state-machine for `villa backend set` — and exported the non-container `PollHealth`/`GenerationProbe` prove primitives that Plan-02's `liveProve` compiles against, while extending the seam grep-gate to permanently guard `cmd/villa` against backend-marker leaks.**

## Performance

- **Duration:** ~14 min
- **Tasks:** 3 of 3
- **Files created:** 3
- **Files modified:** 1

## Accomplishments

- New `internal/backendswap` package: `Deps`/`Result`/`ProveVerdict` + `Run(d Deps, target string) Result`, the transactional core that captures the verbatim prior `villa-llama.container` bytes and config STRICTLY before any mutation, gates cutover on an injected `Prove` verdict, and auto-rolls back verbatim on any mutate error or non-pass verdict (BSET-02). A failed switch is a no-op to the running stack.
- Fit-guard FIRST then ROCm preflight refuse-with-remediation against the PRESERVED model (model = config, never re-pick); same-backend is a clean `NoOp`; refusals leave zero side-effect seams (BSET-01).
- Exported `inference.PollHealth` + `inference.GenerationProbe` — thin delegations over the package-private `pollHealth`/`chatProbe` that probe the ALREADY-running server with no `--rm` container — closing the Plan-02 BLOCKER where `liveProve` cited the unexported primitives.
- Extended `TestSeamGrepGate` to walk `cmd/villa` with the backend-marker pattern subset, making the cmd-tier literal-free property a committed regression test.

## Task Commits

1. **Task 08-01-01: backendswap core types + Run capture-before-mutate forward path** — `f2db5e4` (feat, TDD)
2. **Task 08-01-02: rollback frame + prove-gate + refuse-with-remediation** — `eaab3d6` (feat, TDD)
3. **Task 08-01-03: export PollHealth/GenerationProbe + extend seam gate to cmd/villa** — `d6d982a` (feat)

## Files Created/Modified

- `internal/backendswap/backendswap.go` — `Deps`/`Result`/`ProveVerdict`/`ProveStatusPass` + the `Run` transactional state-machine (capture→mutate→prove→rollback).
- `internal/backendswap/backendswap_test.go` — Wave-0 fake-`Deps` `swapRecorder` + 10 state-machine tests (capture-before-mutate, verbatim rollback, prove-gate, active-not-success, fit/preflight refuse, mutate-error rollback, rollback-incomplete).
- `internal/inference/prove.go` — exported `PollHealth` + `GenerationProbe` thin wrappers over the private running-server probes.
- `internal/inference/seam_test.go` — `TestSeamGrepGate` factored into a shared `matchFile` closure; second walk over `cmd/villa` with the backend-marker pattern subset.

## Decisions Made

See `key-decisions` in frontmatter — the four load-bearing choices: local `ProveVerdict`/`ProveStatusPass`, switch-only-on-pass, honest incomplete-rollback reporting, and the podman-pattern exclusion for the cmd/villa seam walk.

## Deviations from Plan

None — plan executed exactly as written. The one judgement call (the `cmd/villa` seam walk excluding the `podman invocation` pattern) is faithful to the plan's stated intent ("a ROCm0/HSA/fault/image/GOOS leak in cmd/villa ... fails CI" — the backend-marker leak, NOT the legitimate cmd-tier podman orchestration). cmd/villa was scanned first to confirm only the legitimate `uninstall.go` `podman volume rm` matched the podman pattern (the three backend-marker patterns matched zero files), so the chosen subset both guards `backend.go` and avoids a false CI failure on pre-existing intentional orchestration.

## Issues Encountered

The naive "apply all four patterns to cmd/villa" reading would have failed CI on the pre-existing, legitimate `cmd/villa/uninstall.go:363` `exec.Command("podman", ...)` volume-removal. Resolved by scoping the cmd/villa walk to the backend-marker pattern subset (the inference-backend-neutrality guard), excluding the podman-process pattern since cmd/villa is precisely the OS-orchestration tier allowed to invoke podman. Verified the three backend-marker patterns match zero cmd/villa files today, so the new gate is green and meaningfully guards the future `backend.go`.

## Verification

- `go test ./internal/backendswap/... -count=1` — green (10 tests).
- `go test ./internal/inference/ -run 'TestSeamGrepGate|TestROCmMarkerPresence' -count=1` — green (the walk now covers cmd/villa; the exported wrappers build).
- `go test ./... -count=1` — green (493 tests, 18 packages; no regression).
- `go vet ./...` — clean.
- `grep -REn 'ROCm0|HSA_OVERRIDE|Memory access fault|rocm-7\.2\.4' internal/backendswap/ | grep -v '_test.go'` — no match (package literal-free of backend tokens).

## Known Stubs

None. The `Run` state-machine is complete; the live `Deps` wiring (`liveBackendSwapDeps`, `liveProve`) is Plan-02 scope by design, not a stub.

## Self-Check: PASSED

- FOUND: internal/backendswap/backendswap.go
- FOUND: internal/backendswap/backendswap_test.go
- FOUND: internal/inference/prove.go
- FOUND: internal/inference/seam_test.go (modified)
- FOUND commit: f2db5e4
- FOUND commit: eaab3d6
- FOUND commit: d6d982a
