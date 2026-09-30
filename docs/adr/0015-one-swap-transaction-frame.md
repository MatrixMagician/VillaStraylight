---
status: accepted
---

# One swap transaction frame owns the lock, restart set, proof and rollback

A read-only spike at `2fd9314` found the capture → save → write → restart → prove →
rollback frame copied three times: `backendswap.transact` (backend, speculation,
tools mode), `codingmode.Run` and `modelswap.Run`. The ADR-0010 stack lock was taken
by the cobra caller, and only five callers took it, so `villa bench --ab` flipped the
backend through `backendswap.Run` unlocked (#250), and `up`/`restart` (the
inference-secret heal writes config), `restore`, `update`, `install` and the resident
verbs mutated the stack unlocked too. Each copy restarted villa-llama alone, while a
backend switch also rewrites every resident unit, which shares the backend image
(#251). The cutover proof after a swap was keyed to the chat model even when
villa-llama served the coder (#261). This ADR records the seam #256 chose.

## Decision

- **`stackapply.Transact(TxDeps, Change) Outcome` is the frame.** It lives beside
  `Apply` and `Restore` in the stack-apply module (ADR-0013), in
  `internal/stackapply/transact.go`. In order it:
  1. takes the stack lock (`TxDeps.Lock`), then loads the config;
  2. runs the verb's `Change`, which returns the target config or an `Outcome` that
     stops the transaction (a refusal, a no-op, a failed pull) before anything is
     captured;
  3. captures the prior config and the on-disk bytes of every unit the prior config
     renders;
  4. saves the target and applies it; no unit changed is a `NoOp`, nothing restarted
     or proven;
  5. restarts every changed service that is running, plus the proven service
     (`TxDeps.Service`, villa-llama) whether or not it was, since the proof needs it
     serving;
  6. proves, and switches only on `prove.StatusPass`.

  A save, write, restart or proof failure restores every captured unit verbatim,
  re-saves the prior config, reloads, and restarts exactly the services the cutover
  restarted or tried to. Every rollback step that fails is named, and the `Outcome`
  still says `RolledBack`.
- **`TxDeps` is a struct of funcs** with one live binding, `liveTxDeps` in
  `cmd/villa/backend.go`: `Lock`, `LoadConfig`, `SaveConfig`, `Capture`, `Apply` and
  `Restore` (this module's own, over `liveStackDeps`), `DaemonReload`, `IsActive`,
  `Restart`, `Prove` (`liveProve`) and `Service`. The swap cores' tests fake it without
  faking a stack.
- **The lock binding is the caller's.** The CLI binds `acquireStackLock`
  (`stacklock.Acquire`, blocking). The dashboard's switch handler binds `tryStackLock`
  (`stacklock.TryAcquire`) on its copy of the deps, keeps `swapMu`, and answers
  `stacklock.ErrBusy` with the same 409 body as before.
- **The swap cores shrink to a Change.** `backendswap` (`Run`, `RunSpeculation`,
  `RunTools`), `codingmode.Run` and `modelswap.Run` keep the no-op test, the guards
  (fit, ROCm preflight, the coding-mode refusal, catalog resolution, pull) and the
  config fields they write. Each `Result` embeds `stackapply.Outcome`. The proof choice
  is the `TxDeps.Prove` binding: `liveProve` everywhere, `liveToolsProve` for tools mode.
- **One proof target.** `proveTarget` resolves the model, file, ctx and weight
  footprint from `stackapply.ServedTarget`, the same served target the render used,
  and counts a draft's weight exactly when the render serves one. `liveCodingProve`
  was the only copy that did this; it is deleted.
- **The flows that are not swaps keep their own shape and take the same lock** in
  their cobra caller, before their first config read: `up` and `restart` (not on
  `--dry-run`), `model resident add|rm`, `restore` (after the skew prompt), `update`'s
  apply half (after `--dry-run`), and `install` (not on `--dry-run`). Their stopped
  windows, data volumes and restart choices differ from a swap's; forcing them
  through a `Change` would move their decisions, not remove a copy.
- **A structural guard.** `TestEveryStackMutationHoldsTheLock` (`cmd/villa`) fails
  the build when a caller of `stackapply.Apply`/`Restore` outside the module is not a
  registered writer run by a verb that calls `acquireStackLock`, when code reaches a
  frame's host directly (`x.Tx.Apply(...)`), or when `runInstall` stops taking the lock.

## Rejected

**Take the lock in `runBackendSwap` and in each unlocked verb.** The smallest fix for
#250, and the one the issue named first. It leaves three copies of the frame and a
lock every new caller must remember, which is how bench forgot it.

**Export `backendswap.Transact` for `codingmode` and `modelswap` to call.** No new
file, but a model swap and a coding-mode cutover would import the backend-switch
package for its frame, and the frame would still not know which units changed.

**A new `internal/swaptx` package.** The cleanest name, but a second module beside
stack-apply that both own "a config reaches the stack". Keeping the frame next to
`Apply` means the guard reads one module's callers.

**Give the frame `stackapply.Deps` and call `Apply`/`Restore` directly.** Every swap
core's tests would then fake a catalog, a renderer and a unit dir to test a no-op
check. The frame takes `Apply`/`Capture`/`Restore` as funcs; `liveTxDeps` is the one
place they are bound to this module.

**Restart every unit on a swap, as `restart` does.** A backend switch would bounce the
chat UI and the vector store for nothing. **Restart changed units whether or not they
run, as `update` does for its subsystem.** A swap would start a resident the operator
stopped.

## Consequences

- #250: `bench --ab` waits for a concurrent mutation
  (`TestBenchABSwitchWaitsForTheStackLock`), and a transaction holds the lock until it
  returns (`TestTransactHoldsTheStackLock`).
- #251: a backend switch restarts the running residents it rewrites and restores and
  restarts them on rollback (`TestSwitchRestartsTheChangedResidentUnit`). The
  restarted set is ordered as the render orders units.
- #261: the proof in swap-residency coding mode drives the coder
  (`TestProveTargetIsTheServedCoder`), and shared residency with speculation renders
  (`TestSharedResidencySpeculationResolvesTheChatModel`). An unset speculation mode
  no longer counts a draft the unit does not load into the GTT floor.
- Behaviour that changed: a lock failure is `FailedStep "lock"` from inside the core,
  not a message printed before it; a model swap now resolves, fits and pulls after
  the lock and the config read rather than before (the lock was already held across
  them); a swap whose target renders the units already on disk is a `NoOp` for every
  swap verb, not only `model swap`; a save or write failure rolls back without
  restarting anything, because nothing running was touched. `runModelSwap` and the
  dashboard used to report a proof-triggered rollback as a switch (exit 0, HTTP 200);
  they now report it (exit 1, HTTP 500).
- The three rollback clones and their ordering suites are deleted; the ordering lives
  in `internal/stackapply/transact_test.go`. Deleted: `backendswap.transact`, the
  `CaptureUnit(s)`, `ReconcileAndWrite`, `RestoreUnit(s)`, `DaemonReload`, `Restart`,
  `Prove` and `InstallServiceName` fields of the three `Deps`, `liveCodingProve`,
  `codingModelFile`, `codingWeightBytes`, and the five `acquireStackLock` calls in the
  swap verbs.
- ADR-0010 is amended to say where the lock is taken. ADR-0013's "the swap cores'
  capture/rollback frames stay in `cmd/villa`" no longer holds.
- Config writers and service stops (#267): the frame's rollback restores the whole
  captured `config.toml`, so any config write that lands inside a swap's window is
  silently reverted, and a service stopped inside it fails the swap's proof for a
  reason the swap did not cause. `config set`, `workspace add|remove`,
  `recommend --save`, `verify agent` (around the whole proof; `update apply` runs the
  same proof already holding the lock, and flock does not nest) and `backup` now take
  the blocking stack lock from their first config read
  (`TestConfigSetWaitsForTheStackLock` and its siblings hold the real flock). The
  teardown verbs lock too: `down` (around the stop), `uninstall` (around the whole
  teardown, model-weights prompt included) and `verify search` (around the proof, so
  the transient nft bound it puts in the shared rootless netns and its deferred
  teardown sit inside one locked window; `update apply` runs that proof already
  holding the lock, so the lock is in `runVerifySearch`, not `liveSearchVerify`).
  They capture nothing, but a swap in flight when they run would fail its proof and
  roll back for a reason it did not cause. `verify search` stops no service; its
  mutation is the nft bound, so the guard also lists `applySearchBound` as a local
  sink (`localSinks`).
- The lock guard is two checks over the sources, each self-tested
  (`TestLockGuardCatchesEveryShape`, `TestLockGuardCatchesNestedAcquires`). Every
  reference to `stackapply.Apply/Restore`, `orchestrate.WriteUnits`,
  `config.SaveVilla` or `orchestrate.NewSystemd`, as a call or as a function value,
  must sit in a function `lockRules` names, with the verbs that lock it or the
  reason it needs none; a read-only entry may not reference a systemd mutator.
  `liveTxDeps` must be wired with `acquireStackLock`, a `TxDeps` is built nowhere
  else, and a `Tx.Lock` override must be `tryStackLock`. A verb that takes the lock
  and reaches another function that takes it fails the build.
- The restart set now matches this ADR: the proven service is restarted whether or
  not its unit changed (last, when no changed unit named it), and `running()` counts
  an unreadable `IsActive` as not running, so a stopped resident is never started by
  a state the frame could not read (`TestTransactRestartsTheProvenServiceWhenOnlyAnotherUnitChanged`,
  `TestTransactNeverStartsAServiceItCannotReadTheStateOf`).
