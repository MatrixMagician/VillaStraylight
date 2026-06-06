---
phase: 08-villa-backend-set-switch-verb-rollback
verified: 2026-06-06T00:00:00Z
status: human_needed
score: 4/4 must-haves verified (off-hardware logic); 2/4 on-hardware UAT items now PASS, 2 residual (failure-path)
re_verification:
  previous_status: none
  previous_score: n/a
human_verification:
  - test: "Force a bad ROCm config (e.g. a host that silently CPU-falls-back, or a load_tensors hang) and run `villa backend set rocm`."
    expected: "liveProve classifies it FAIL (gpu_busy 0% / not-ready-before-timeout / no tokens) within proveTimeout (5m); the switch auto-rolls back to the verbatim prior vulkan unit+config and re-readies villa-llama; exit 1 with a 'rolled back; prior backend restored' message; the running stack is unchanged."
    why_human: "Silent-CPU-fallback detection, the load_tensors-hang deadline, the allocation-cap / firmware-fault paths, and the live transactional restore all depend on real ROCm runtime behavior unavailable off-host. RESIDUAL — requires deliberately breaking a ROCm config; not exercised in the 2026-06-06 happy-path on-hardware session."
  - test: "Confirm the bounded proveTimeout (5m) actually fires on a never-ready ROCm server (an unbounded load_tensors hang)."
    expected: "The cutover prove returns FAIL at the deadline (not an infinite wait) and rolls back."
    why_human: "Requires a real hung llama-server load on the target hardware; the deadline context is wired but its trip can only be observed live. RESIDUAL — not exercised in the 2026-06-06 session (no induced hang)."
human_verification_closed: 2026-06-06T22:00:00Z
human_verification_resolved:
  - test: "On a running install, `villa backend set rocm` performs a real ROCm bring-up that proves healthy and cuts over."
    resolution: "PASS (on-hardware, 2026-06-06). `villa backend set rocm` → exit 0, 'cutover proven'; `villa backend show` reports rocm + rocm-7.2.4 digest; ROCm0 residency PASS (20583.34 MiB resident); model qwen3.6-35b-a3b preserved; only villa-llama regenerated/restarted. Cross-checked by `bench --ab` flipping vulkan↔rocm and restoring."
  - test: "`villa backend set rocm --dry-run` and `villa backend show` against a real configured install."
    resolution: "PASS (on-hardware, 2026-06-06). Dry-run printed {target rocm, fit PASS, preflight PASS} and wrote nothing ('no config persisted, no units regenerated, no restart'); `villa backend show` reported the real active backend + image tag."
---

# Phase 8: `villa backend set` Switch Verb + Rollback — Verification Report

**Phase Goal:** A user can flip the inference backend on a *running* install with one command and never end up with a broken stack — the switch captures the prior working unit, gates cutover on a real generation-probe + ROCm residency proof, and auto-rolls back verbatim on any failure. (The on-hardware risk concentration; the v1.0 Phase-2 analog.)
**Verified:** 2026-06-06
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

The off-hardware logic that the goal rests on — the transactional capture→mutate→prove→rollback state machine, the prove-gate mapping (only `StatusPass` succeeds; `gpu_busy==0` FAILs), the refuse-with-remediation paths, dry-run-mutates-nothing, and the `show` output — is implemented and unit-proven. The runtime behaviors that can only be observed on a real gfx1151 host (actual ROCm offload, HSA-override, load_tensors-hang deadline trip, silent-CPU-fallback detection, the live transactional restore) are classified as human verification per the phase's on-hardware research flag, not failed.

### Observable Truths

| # | Truth (Success Criterion) | Status | Evidence |
| - | ------------------------- | ------ | -------- |
| 1 | SC#1 — `villa backend set rocm` swaps ONLY villa-llama (save-before-restart, regenerate, daemon-reload, restart the inference unit only), preserving model/quant/context, refuses-with-remediation on fit OR ROCm-preflight failure (never re-picks). | VERIFIED (logic) | `backendswap.Run` ordering: load→fit-guard FIRST (`backendswap.go:160`)→ROCm preflight (`:172`, **target-pinned** snapshot)→capture (`:179`)→mutate (`:234-243`)→prove. Restart is `d.Restart(d.InstallServiceName)` only — `TestSwapInferenceOnly` asserts restarted == `["villa-llama.service"]`. Fit/preflight refuse with zero seams: `TestRefuseFitGuard`, `TestRefuseProveFlightROCm` assert `len(callOrder)==0`. Model preserved: FitsModel/Pick keyed on `cfg.Model` (`backend.go:387-397`), never re-picked. **CR-01 fix present** (`backend.go` is fed a `preflightCfg` with `.Backend=target`, `backendswap.go:170-172`); guarded by `TestPreflightSeesTargetBackend`. |
| 2 | SC#2 — A failed bring-up auto-rolls back to the verbatim captured prior unit/config and re-readies it (a failed switch is a no-op to the running stack). | VERIFIED (logic) | Capture strictly before mutate (`backendswap.go:179`, `TestCaptureBeforeMutate` asserts capIdx<saveIdx,writeIdx). `rollback` closure restores `priorUnit` bytes → `SaveConfig(priorCfg)` → `DaemonReload` → `Restart` (`:191-210`), best-effort with honest incomplete reporting (`:225-229`). Triggers: ANY mutate error (`:235-243`) and non-pass prove (`:249-250`). `TestRollbackVerbatim` asserts byte-equal restore; `TestMutateErrorRollsBack`, `TestRollbackIncompleteReported` cover the paths. cmd maps RolledBack→exit 1 + "rolled back; prior backend restored". |
| 3 | SC#3 — Cutover succeeds ONLY after a real generation-probe readiness check + residency proof passes within a bounded timeout — `is-active` alone never counts. | VERIFIED (logic) | Core switches only on `v.Status == ProveStatusPass` (`backendswap.go:249`); any other verdict rolls back — `TestActiveNotSuccess` ("ready+200 but residency FAIL"→rollback), `TestProveGate`. `liveProve` composes (a) bounded `inference.PollHealth` (`backend.go:100`), (b) real `inference.GenerationProbe` tokens>0 (`:112-149`), (c) `RunningOffloadVerdict` over `BackendFor(target).ResidencyProof()` markers + GTT floor + during-decode `gpu_busy` (`:158-167`); maps ONLY `inference.StatusPass`→`ProveStatusPass` (`:171-174`). `gpuBusyFloor` FAILs on `gpu_busy==0` during a claimed-healthy decode (`running_offload.go:273-277`). Whole prove bounded by `proveTimeout` (5m) deadline context. |
| 4 | SC#4 — `villa backend show` reports the active backend; `set --dry-run` previews {target, fit, preflight} without mutating config or units. | VERIFIED (logic) | `runBackendShow` reports `BackendFor(cfg.Backend).Name()` + `.Image()` (`backend.go:232-263`), `--json` supported. Dry-run branch is FIRST in `runBackendSet` (`:303-330`): prints target + fit + preflight (against a would-be-target cfg, `:320-322`) and returns before any `backendswap.Run`. `TestBackendSetDryRun` asserts zero saved/written/restarted/captured/proved seams; `TestBackendShow` asserts backend+image in human & JSON output. |

**Score:** 4/4 truths verified at the off-hardware logic level (on-hardware runtime → human verification).

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/backendswap/backendswap.go` | Deps/Result/ProveVerdict + Run capture→mutate→prove→rollback | VERIFIED | 254 lines; `Run` present; literal-free of backend tokens (grep clean). |
| `internal/backendswap/backendswap_test.go` | State-machine suite over fake Deps | VERIFIED | 13 test funcs incl. capture-ordering, verbatim rollback, prove-gate, active-not-success, fit/preflight refuse, mutate-error rollback, rollback-incomplete, `TestPreflightSeesTargetBackend` (CR-01 RED-proven guard). |
| `internal/inference/prove.go` | Exported PollHealth + GenerationProbe wrappers | VERIFIED | Both present, thin delegations over private pollHealth/chatProbe; no `--rm` container. |
| `internal/inference/seam_test.go` | TestSeamGrepGate extended to walk cmd/villa | VERIFIED | Second walk rooted at `../../cmd/villa`; gate green. |
| `cmd/villa/backend.go` | newBackend/show/set, runBackendSet exit map, liveProve, liveBackendSwapDeps | VERIFIED | 475 lines; literal-free (grep clean); all seams wired to in-repo primitives. |
| `cmd/villa/backend_test.go` | TestBackendShow/SetDryRun/SetExitMapping over fake Deps | VERIFIED | All present + TestBackendRegistered. |
| `cmd/villa/root.go` | newBackend() registered | VERIFIED | `root.go:35` AddCommand list includes `newBackend()`. |

### Key Link Verification

| From | To | Via | Status |
| ---- | -- | --- | ------ |
| `backendswap.Run` | `config.VillaConfig` | Deps.LoadConfig/SaveConfig | WIRED |
| `backendswap.Run` rollback | `Deps.RestoreUnit(priorUnit)` | verbatim bytes restore | WIRED |
| `inference.prove.go` | private pollHealth/chatProbe | exported delegating wrappers | WIRED |
| `liveProve` | `inference.PollHealth` + `GenerationProbe` | exported non-container primitives | WIRED |
| `liveProve` | `BackendFor(target).ResidencyProof()` | RunningOffloadVerdict markers | WIRED |
| `liveProve` | `detect.GPUBusyPercent()` | during-decode goroutine+ticker max-keeper | WIRED |
| `runBackendSet` | `backendswap.Run` | `liveBackendSwapDeps()` | WIRED |
| `root.go` | `newBackend()` | AddCommand | WIRED |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| backendswap + backend cmd state machine | `go test ./internal/backendswap/... ./cmd/villa/ -run '...'` | 42 passed (2 pkgs) | ✓ PASS |
| seam grep-gate (walks internal/ + cmd/villa) | `go test ./internal/inference/ -run 'TestSeamGrepGate\|TestROCmMarkerPresence'` | 2 passed | ✓ PASS |
| full repo suite (no regression) | `go test ./... -count=1` | 505 passed, 18 pkgs | ✓ PASS |
| static analysis | `go vet ./...` | No issues | ✓ PASS |
| literal-free cmd noun | `grep -REn 'ROCm0\|HSA_OVERRIDE\|Memory access fault' cmd/villa/backend.go` | no match | ✓ PASS |
| literal-free core | `grep ... internal/backendswap/ \| grep -v _test.go` | no match | ✓ PASS |
| live ROCm cutover / rollback / hang-deadline | (on-hardware) | n/a off-host | ? SKIP → human |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
| ----------- | -------------- | ----------- | ------ | -------- |
| BSET-01 | 08-01, 08-02 | `villa backend set` swaps the inference unit on a running install (save-before-restart, regenerate, daemon-reload, restart only villa-llama.service), fit-guarded, refuses-with-remediation. | SATISFIED (logic) | Truth #1 — Run ordering + restart-inference-only + fit/preflight refuse; CR-01 fix makes the ROCm-preflight-refuse path genuinely reachable on vulkan→rocm. On-hardware restart/regenerate behavior → human. |
| BSET-02 | 08-01, 08-02 | Transactional + rollback-safe: capture prior unit/config before mutating, gate cutover on real generation-probe + residency proof, auto-roll-back verbatim on any failure. | SATISFIED (logic) | Truths #2 + #3 — capture-before-mutate, verbatim restore, prove-gate (StatusPass only), bounded timeout. On-hardware bring-up/rollback → human. |
| BSET-03 | 08-02 | `villa backend show` reports active backend; model/quant/context preserved (refuse, don't re-pick); `--dry-run` previews without mutating. | SATISFIED (logic) | Truth #4 + Truth #1 (model=config) — show output, dry-run zero-seam, FitsModel keyed on cfg.Model. |

No orphaned requirements: REQUIREMENTS.md maps exactly BSET-01/02/03 to Phase 8; all three appear in plan frontmatter and are accounted for.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| — | — | — | — | No debt markers (TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER) found in the phase's modified files. No stub returns, no hardcoded-empty render data. |

### CR-01 Fix Confirmation

The orchestrator's fix for the BSET-01 preflight-bypass (commit `ce18a08`) is present and effective:
- `backendswap.go:170-172` builds `preflightCfg := cfg; preflightCfg.Backend = target` and passes it to `d.PreflightROCm` — so on a vulkan→rocm switch the live closure (`backend.go:401-411`, gated on `cfg.Backend != "rocm"`) now actually invokes `preflight.RunROCm`, making SC#1's "refuses when ROCm preflight blocks" path genuinely reachable.
- The RED-proven regression test `TestPreflightSeesTargetBackend` (`backendswap_test.go:305`) asserts the gate receives `Backend=="rocm"`; the stub records `preflightSawBE`. Test passes. The dry-run path (`backend.go:320-322`) already pinned the target and now agrees with the live switch.

### Review WARNINGs / INFO (advisory — not gaps)

The 08-REVIEW.md left 5 WARNINGs (WR-01 fit-check is backend-agnostic vs the documented ROCm 64 GB cap; WR-02 liveProve omits the /props drift overlay status.go includes; WR-03 GTT floor is a host-wide counter; WR-04 WARN-vs-FAIL collapsed to a flat "fail" message; WR-05 double config-load TOCTOU) and 3 INFO (IN-01 rollback detail overwrites earlier failures; IN-02 proveTimeout magic-mirrors an unexported const; IN-03 empty generation-probe detail). These are advisory hardening notes, not goal blockers — recorded here for the developer; none invalidate a success criterion. WR-01 and WR-04 in particular are worth revisiting when the ROCm fit-envelope and residency-instrumentation maturity land on-hardware.

### Human Verification Required

See the `human_verification` frontmatter for the four on-hardware UAT items (real ROCm cutover, forced-bad rollback, proveTimeout hang trip, live dry-run/show). These cannot be confirmed off this host per the phase's on-hardware research flag and are deferred to UAT rather than failed.

### Gaps Summary

No off-hardware gaps. The transactional core, prove-gate, refuse-with-remediation paths, dry-run-mutates-nothing, and show output are all implemented and unit-proven (505 tests green, vet clean, seam gate green, literal-free discipline intact). The CR-01 Critical is fixed and regression-guarded. The remaining verification surface is purely runtime behavior on real gfx1151 ROCm hardware, surfaced as human verification.

---

_Verified: 2026-06-06_
_Verifier: Claude (gsd-verifier)_
