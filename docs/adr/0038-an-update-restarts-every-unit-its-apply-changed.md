---
status: accepted
---

# An update restarts every running unit its apply changed

The verification of #347 (ADR-0036) found that `villa update` could split the stack
(#354). An update's mutation runs `stackapply.Apply`, and Apply writes every unit
whose render changed, not only the updated subsystem's. The mutation then restarted
only that subsystem's services. On the first update after an upgrade to v1.18, with
no `villa up` in between, a memory pin move rewrites the seven closed-network units,
Open WebUI and `villa-closed.network`. Qdrant and the embedder restart onto
`villa-closed`. Open WebUI is rewritten on disk but keeps running on `villa` alone,
so chat loses RAG, memory and reranking. The memory proof probes over `villa-closed`,
passes, and the update commits. The capture held only the subsystem's units, so a
rollback could not put Open WebUI's file back either.

An upgrade is not the only way in. A resident unit renders the inference backend's
image (`renderResidentUnits` in `internal/orchestrate/resident.go`), so an inference pin move rewrites every
resident unit, and none of them was restarted or captured.

## Decision

- **An update restarts what the swap frame restarts.** The mutation restarts the
  updated subsystem's services whether or not their units changed, as before, plus
  every service whose unit the apply changed and that is running. A changed unit
  whose service the operator stopped stays stopped. This is ADR-0015's rule with
  the subsystem's services in the place of the proven service.
- **The rule is one function.** `stackapply.RestartChanged(applied, always,
  isActive, restart)` restarts the changed running services in render order, then
  each `always` service not yet restarted, and returns every service it restarted
  or tried to. `Transact` calls it with the proven service; the update's
  `liveMutate` calls it with the subsystem's services. #354 was a second restart
  rule that had drifted from the first.
- **The capture takes every unit the config renders.** `liveCapture` reads the
  bytes of each unit in the plan's `Changed` and `Unchanged` sets, so any unit the
  apply rewrites is in the rollback point. A registry unit the config does not
  render is left out: update never changes the config, so the apply's removal of it
  is housekeeping, and ADR-0035 already says an update's rollback keeps it. A config
  the render refuses now refuses the update at capture, before anything is mutated,
  instead of failing the apply.
- **The rollback restarts what the mutation restarted.** `updateflow.Deps.Mutate`
  returns the services it restarted or tried to, even with an error, and the core
  hands that set to `Restore`. Restore writes every captured unit back, reloads, and
  restarts exactly that set. A save or write failure before any restart rolls back
  with no restart, as in `Transact`.
- **The stopped window is unchanged.** A stateful subsystem still stops, snapshots,
  mutates and starts its own services. The extra restarts happen inside the
  mutation, while the window is open, and the rollback's data restore still runs
  before `Restore`.

## Considered options

- **Refuse with "run `villa up` first" when the apply would change units outside
  the subsystem.** It keeps the update's restart and capture to one subsystem. The
  check needs the plan the apply will write, which depends on the new pins, and the
  render reads the pin store, so the check needs a dry-run render with an injected
  pin set before the mutation. It also refuses work the update can do safely: every
  inference update on a host with a resident model changes the resident units, so
  it would always refuse. A refusal after the pins were written would be a mutation
  failure, rolled back and reported as one.
- **Derive the rollback's restart set at rollback time** from the units whose
  captured bytes differ from disk and whose service is running. No seam change, but
  a service the update restarted that then failed reads as not running and would be
  left down on its restored unit.
- **Carry the restart set from the live `Mutate` to the live `Restore` in closure
  state.** No core change, but the core's guarantee that `Restore` follows a
  `Mutate` of the same subsystem would be hidden in two closures.
- **Capture with the swap frame's `captureUnits`.** It also takes registry units the
  config does not render, so a rollback would bring back an orphan the apply
  removed.

## Consequences

- The first `villa update` after an upgrade restarts every running service whose
  unit the new templates changed, as `villa up` would, and a failed update puts all
  of them back on their prior units and restarts them.
- An inference update restarts the running resident units it rewrites. The
  inference proof still drives `villa-llama` alone.
- The retained tuple (`pinstate.Previous.Units`) holds every unit the config
  renders, as the spec's §5.5 says the swaps' capture does, not only the
  subsystem's. Nothing reads it back today; `pin-state.json` grows by the size of
  the unit files per retained subsystem.
- An update whose config does not render refuses at capture ("the rollback point
  could not be captured") instead of rolling back from a failed apply.
- The proof is still the updated subsystem's. A memory update that restarts Open
  WebUI does not run the chat proof.
- `TestAnUpdateRestartsEveryChangedRunningUnit`,
  `TestAFailedUpdateRestoresAndRestartsEveryUnitItChanged` and
  `TestARollbackRestartsWhatTheMutationRestarted` hold the behaviour.
