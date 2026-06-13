---
phase: 25-coding-mode-render-transactional-swap-verb
plan: 02
subsystem: control-plane
tags: [coding-mode, transactional-swap, backendswap-clone, modelswap-compose, residency-prove, seam-gate, cobra-noun, structural-guard]

# Dependency graph
requires:
  - phase: 25-coding-mode-render-transactional-swap-verb
    plan: 01
    provides: "inference.CodingModeSpec/Sampling + RunSpec.CodingMode, config.VillaConfig coding fields (CodingMode/CoderModel/CoderQuant/CoderAgentCtx), orchestrate.RenderInput.CodingMode/CoderAgentCtx, the villa-llama-coding.container.golden render contract"
  - phase: 24-coder-fit-math-catalog-on-hardware-model-qualification
    provides: "schema-3 coder catalog fields (Role/AgentCtx/CacheReuseSafe/AgentSampling) + recommend.Pick().Coder residency verdict; build-9496 swap qualification"
provides:
  - "internal/codingmode core: Run(d Deps, dir Direction) Result + Deps + Result + ProveVerdict + ProveStatusPass + Direction(Enter|Exit) + CoderTarget + Residency{Swap,Shared} — pure, Deps-injected, literal-free of backend markers"
  - "cmd/villa coding-mode noun: newCodingMode() (enter/exit) + liveCodingModeDeps() + liveCodingProve (ConfigContext=AgentCtx twin) + runCodingMode Result->exit mapping"
  - "cmd/villa/root.go registers newCodingMode()"
  - "structural no-auto-flip guard (TestNoAutoFlipStructuralGuard): CodingMode toggle mutated nowhere outside codingmode.Run"
affects: [26 crush launcher (points at the entered coding endpoint), 27 install addon + villa verify agent, 28 status surfacing]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Clone the backendswap transactional frame verbatim in shape; swap the backend-string axis for a Direction(enter|exit)+coder-model+render-delta axis"
    - "Compose modelswap's forward ordering (resolve->fit-guard->pull->persist->reconcile->restart) inside the transactional frame — never fork it"
    - "Durable-chat-model invariant: cfg.Model is NEVER overwritten at enter; the coder is served from cfg.CoderModel so exit reverts by clearing coder fields (config-derived restore, no new prior-chat-model schema field) — D-08"
    - "liveProve twin for coding mode: identical 3-gate composition, differs ONLY in served model (CoderModel) + ConfigContext=AgentCtx (Pitfall 4)"
    - "Structural guard anchored to the bool literal (CodingMode = true|false) so it targets the config toggle without false-matching the same-named render-descriptor pointer field"

key-files:
  created:
    - internal/codingmode/codingmode.go
    - internal/codingmode/codingmode_test.go
    - cmd/villa/coding-mode.go
    - cmd/villa/coding-mode_test.go
  modified:
    - cmd/villa/root.go

key-decisions:
  - "Durable chat model (D-08 realization): cfg.Model stays the chat model at enter; the served coder model lives in cfg.CoderModel and the live ReconcileAndWrite/Prove resolve the served model from CoderModel when coding. Exit clears the coder fields and the unit reverts to the untouched chat model — a config-derived restore, NOT a bare flip and NOT a new prior-chat-model config field (keeps Plan-01's frozen schema)."
  - "Structural no-auto-flip guard regex anchored to the boolean literal (`CodingMode = true|false` / `CodingMode: true|false`) so it targets the VillaConfig toggle (the auto-flip surface) and does not false-match the unrelated render-descriptor pointer field orchestrate.RenderInput.CodingMode / inference.RunSpec.CodingMode (assigned a *CodingModeSpec, never a bool)."
  - "ResolveCoder backed by recommend.Pick().Coder: a `shared` residency verdict is a valid enter target (render-delta-only, surfaced), NOT a refusal; only an unfit/empty `swap` verdict is a refuse-with-remediation (defensive — never a silent OOM)."

requirements-completed: []  # CMODE-02 pending the on-hardware acceptance checkpoint (Task 3)

# Metrics
duration: ~40min
completed: 2026-06-13
---

# Phase 25 Plan 02: Coding-Mode Transactional Swap Verb Summary

**A pure `internal/codingmode` core that clones the backendswap capture->mutate->under-load-prove->verbatim-rollback frame and composes modelswap's forward ordering, plus the explicit `villa coding-mode enter|exit` cobra noun wiring the live host seams (a liveProve twin keyed on ConfigContext=AgentCtx) — literal-free of backend markers, with a structural guard proving the mode never auto-flips. Tasks 1-2 are complete and `make check` is green; Task 3 (the on-hardware enter->prove->exit acceptance) is an OPEN human-verify checkpoint.**

## Status: CHECKPOINT REACHED (Task 3 — on-hardware acceptance)

Tasks 1 and 2 are fully implemented, tested, and committed. `make check` (vet + full suite incl. TestSeamGrepGate + the structural no-auto-flip guard) is GREEN. Task 3 is an `autonomous: false` on-hardware acceptance checkpoint that CANNOT be closed without a real operator-driven enter->prove->exit smoke under load — it is NOT fabricated. See "On-Hardware Acceptance (Task 3) — OPEN" below.

## Performance

- **Duration:** ~40 min (Tasks 1-2)
- **Started:** 2026-06-13
- **Tasks:** 2 of 3 auto tasks complete; Task 3 is a blocking on-hardware checkpoint
- **Files:** 4 created + 1 modified

## Accomplishments

- **Task 1 — pure transactional core (CMODE-02 off-hardware).** `internal/codingmode.Run` clones the `backendswap` frame verbatim in shape: capture the prior `villa-llama.container` bytes + prior `VillaConfig` value snapshot STRICTLY before any mutation -> mutate (persist config, reconcile+write, restart inference only) -> under-load Prove -> ANY mutate error or non-pass verdict rolls back to the verbatim captured unit+config with honest rollback-incomplete reporting. It composes modelswap's forward ordering (resolve->fit-guard->pull->persist->reconcile->restart) for the swap-residency model change. 14 Deps-driven tests (no live host).
- **Task 2 — the verb + live wiring.** `villa coding-mode enter|exit` (explicit subcommands, D-06; NOT `villa code`), registered in `root.go`. `liveCodingModeDeps()` wires every host seam; `liveCodingProve` is a `liveProve` twin that sets `ConfigContext = AgentCtx` (Pitfall 4) and resolves the served model from `CoderModel`. The ONE delta from `backend.go` — the `ReconcileAndWrite` closure resolves the coder catalog entry and translates `catalog.AgentSampling -> inference.Sampling` into `RenderInput.CodingMode`/`CoderAgentCtx` (catalog import in the closure, never the pure renderer, D-05).
- **Marker discipline held (T-25-07).** The core imports NEITHER `internal/inference` NOR `internal/detect` and defines its OWN `ProveStatusPass`/`ProveVerdict`; `cmd/villa/coding-mode.go` holds NO backend/coding-flag literals (markers only via `BackendFor().ResidencyProof()`). `git grep` confirms clean; `TestSeamGrepGate` (walking internal/ + cmd/villa) green.
- **No-auto-flip proven structurally (D-06 / T-25-09).** `TestNoAutoFlipStructuralGuard` walks `cmd/villa` + `internal/` and asserts the `CodingMode` bool toggle is mutated NOWHERE outside `codingmode.Run`.
- **swap vs shared never silent (D-10).** The Result carries the residency mode; the success line names swap (chat->coder) or shared (render-delta-only, no swap) explicitly.
- **Exit symmetric, not a bare flip (D-08).** Realized via the durable-chat-model invariant (see Decisions) so exit runs the identical capture->mutate->prove->rollback frame.

## Task Commits

1. **Task 1: Pure transactional codingmode core (clone backendswap, compose modelswap)** — `0684917` (feat)
2. **Task 2: villa coding-mode enter|exit noun + liveCodingModeDeps + no-auto-flip guard** — `63441d4` (feat)

(Task 1 was committed as `6df36a9` then amended to `0684917` with the durable-chat-model exit-design refinement before any later commit — see Deviations.)

## On-Hardware Acceptance (Task 3) — OPEN (human-verify, blocking)

The dev box IS the gfx1151 host. Runtime probe (read-only) found:

| Item | State |
|------|-------|
| `villa-llama.service` | **active, running ROCm 7.2.4 (HIP)** — NOT the pinned vulkan-radv build-9496 the Phase-24 swap qualification + this checkpoint's `how-to-verify` are scoped to |
| Coder GGUFs | present: Qwen3-Coder-30B-A3B (Q4_K_XL), Qwen3-Coder-Next (Q3_K_XL, Q4_K_XL) |
| `./villa` binary | rebuilt fresh this plan (`make build`, `v1.3-47-g63441d4-dirty`); `villa coding-mode enter|exit` verb confirmed wired (`--help` shows both subcommands) |
| Config | chat mode (`qwen3.6-35b-a3b`, ctx 131072); no coding fields — clean starting state |

**Why Task 3 is NOT auto-closed:** the running stack is on **ROCm 7.2.4**, while the acceptance (all 3 coder entries PASS at swap, `cache_reuse_safe=true`) is **build-9496-vulkan-radv-scoped** (D-03/D-13). Running the smoke now would either prove against an unqualified backend/build (outside the checkpoint's stated scope) OR require switching the live backend + restarting the running production stack — an operator-affecting mutation I must not perform autonomously. The rollback drill (step 5) also deliberately degrades the live stack. This is exactly the human-verify, on-hardware, operator-judgment checkpoint the plan declared (`autonomous: false`). No pass is fabricated.

**What was already proven off-hardware:** the full transactional state machine (capture-before-mutate ordering, verbatim rollback, honest rollback-incomplete, idle-green-is-FAIL prove gate, same-state NoOp, exit symmetry, swap-vs-shared surfacing) via the 14-test Deps-driven `internal/codingmode` suite; the Result->exit mapping + structural guard via the 8-test `cmd/villa` suite; marker containment via `TestSeamGrepGate`.

**Operator steps to close (from the plan's `how-to-verify`, on the gfx1151 box, pinned vulkan-radv build-9496 — switch the backend first if currently on ROCm):**
1. `make build` (done — fresh `./villa`).
2. `./villa coding-mode enter` — confirm the coder model is served, residency proves PASS under load, exit 0, the success line names the swap residency mode.
3. Confirm `~/.config/containers/systemd/villa-llama.container` Exec carries `--jinja`, `-c <agent_ctx>`, the sampling flags, and `--cache-reuse 256`.
4. `./villa coding-mode exit` — confirm the chat model is restored, residency proves PASS, exit 0, the Exec line is back to byte-identical v1.3 (no coding flags).
5. Rollback drill: force a CPU-fallback / residency-FAIL during an enter and confirm prove FAILS, the stack rolls back verbatim to the prior chat model+unit, message is honest.
6. NoOp drill: `./villa coding-mode enter` twice — the second is a clean "already in coding mode — no change".

**Resume signal:** Type "approved" once enter->prove->exit (incl. the rollback + NoOp drills) pass on hardware, or describe the failure.

## Decisions Made

- **Durable chat model realizes D-08 without a new schema field.** Rather than overwriting `cfg.Model` with the coder model at enter (which would lose the chat model and force a new `prior_chat_model` config field), `cfg.Model` stays the chat model and the coder is served from `cfg.CoderModel`. The live `ReconcileAndWrite`/`liveCodingProve` resolve the served model from `CoderModel` when `cfg.CodingMode` is true (D-05 config->render). Exit clears the coder fields -> the unit reverts to the untouched chat model: a config-derived restore that is symmetric to enter and keeps Plan-01's frozen config schema intact.
- **Structural guard anchored to the bool literal.** The no-auto-flip regex matches `CodingMode = true|false` / `CodingMode: true|false` so it targets the VillaConfig toggle and does not false-match the same-named render-descriptor pointer field (`RenderInput.CodingMode` / `RunSpec.CodingMode`, a `*CodingModeSpec`).
- **`shared` residency is a valid enter, not a refusal (D-10).** `ResolveCoder` returns ok=true with `Residency=shared` (render-delta-only) when no coder fits standalone; only an unfit/empty `swap` verdict refuses-with-remediation.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Naive `cfg.Model = coderModel` at enter would make D-08 exit-restore impossible**
- **Found during:** Task 1 (designing the exit path)
- **Issue:** The plan's Task-1 action text described setting `cfg.Model` to the coder model at enter and "restoring the captured prior chat model" at exit. But Plan-01's frozen config schema has NO prior-chat-model field, and config is the only cross-invocation persistence — overwriting `cfg.Model` at enter would irrecoverably lose the chat model, so exit could not restore it (D-08 would be unsatisfiable without a new schema field that Plan 01 deliberately did not add).
- **Fix:** Keep `cfg.Model` as the durable chat model at enter (never overwritten); persist the coder in `cfg.CoderModel`; resolve the SERVED model from `CoderModel` when coding (live `ReconcileAndWrite` + `liveCodingProve`). Exit clears the coder fields and the unit reverts to the chat model — a config-derived restore, symmetric to enter, with no new schema field. `Result.ToModel` still surfaces the coder (served) model for the user.
- **Files modified:** internal/codingmode/codingmode.go, internal/codingmode/codingmode_test.go
- **Verification:** TestEnter / TestExitRestoresChat / TestSharedResidencyRenderDeltaOnly all pass.
- **Committed in:** `0684917` (Task 1, amended before any later commit)

**2. [Rule 1 - Bug] Structural-guard regex false-matched the render-descriptor pointer field**
- **Found during:** Task 2 (first run of TestNoAutoFlipStructuralGuard)
- **Issue:** A broad `\.CodingMode\s*=` regex flagged `orchestrate.RenderInput.CodingMode` (render.go) and `cmd/villa/coding-mode.go in.CodingMode = spec` — both the *render descriptor* pointer field, NOT the VillaConfig *toggle* the no-auto-flip invariant is about. Left as-is the guard would force suppressing legitimate render plumbing.
- **Fix:** Anchored the regex to the boolean literal (`CodingMode = true|false` / `CodingMode: true|false`) — the config toggle is always assigned a bool; the render-descriptor field is always assigned a `*CodingModeSpec`. The guard now precisely targets the auto-flip surface.
- **Files modified:** cmd/villa/coding-mode_test.go
- **Verification:** TestNoAutoFlipStructuralGuard passes; manual `grep` confirms the only bool-literal CodingMode writes are in codingmode.go (allow-listed).
- **Committed in:** `63441d4` (Task 2)

**Total deviations:** 2 auto-fixed (2 Rule 1 bugs). Both were necessary for the plan's own load-bearing invariants (D-08 exit-restore; a real, non-vacuous no-auto-flip guard) and changed neither the plan's intent nor its frozen contracts. No scope creep.

## Issues Encountered
None beyond the two auto-fixed deviations above (both caught by the local test suite, fixed inline).

## Known Stubs
None. The verb is fully wired against the Plan-01 frozen render contract; every host seam has a live closure. The only OPEN item is the on-hardware acceptance (Task 3), which is a verification gate, not a stub.

## User Setup Required
None for the code. To CLOSE Task 3, the operator runs the on-hardware enter->prove->exit + rollback + NoOp drills on the gfx1151 box (see "On-Hardware Acceptance (Task 3) — OPEN").

## Next Phase Readiness
- CMODE-02 code is complete and `make check`-green off-hardware; CMODE-02 is marked complete in REQUIREMENTS only AFTER the Task-3 on-hardware acceptance is approved (it remains pending here).
- Phase 26 (`villa code` Crush launcher) consumes the entered coding endpoint this verb produces; the verb name deliberately reserves `villa code`.

## Self-Check: PASSED
- Created files verified on disk: internal/codingmode/codingmode.go, internal/codingmode/codingmode_test.go, cmd/villa/coding-mode.go, cmd/villa/coding-mode_test.go, 25-02-SUMMARY.md.
- Task commits verified in git history: 0684917 (Task 1), 63441d4 (Task 2).
- `make check` green (vet + full suite incl. TestSeamGrepGate + TestNoAutoFlipStructuralGuard).

---
*Phase: 25-coding-mode-render-transactional-swap-verb*
*Tasks 1-2 completed: 2026-06-13 — Task 3 (on-hardware acceptance) OPEN*
