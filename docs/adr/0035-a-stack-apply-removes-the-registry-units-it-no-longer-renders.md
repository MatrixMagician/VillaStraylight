---
status: accepted
---

# A stack apply removes the registry units it no longer renders

The review of PR #324 found a gap older than the roadmap it reviewed (#330). When a
subsystem or an optional unit is turned off (memory, the reranker, web search, the
workspace agent, and from #306 on image generation, voice and extraction), the next
stack apply drops the unit from the render and `recommend.ReservationsFor` drops its
row, but `orchestrate.Plan` had no removal set. The old `.container` file stayed in
`~/.config/containers/systemd/` with `WantedBy=default.target`, kept running, and
came back on every boot, while every later fit treated its memory as free. For an
embedder or a reranker that is a few hundred MB to 2 GiB; for image generation it
is ~9.8 GB of GTT. `uninstall`, `down` and `logs` build their service set from the
render, so none of them saw the unit again.

A subsystem is turned off by editing `config.toml` and running `villa up` or
`villa restart`: `config set` takes no gate, and `install.ResolveGates` only ever
raises a gate (a persisted gate is kept, a flag adds one). So the verbs that meet
the orphan are the ones that apply through `internal/stackapply` (ADR-0013), plus
`doctor`, which should name it, and `uninstall`, which should tear it down.

## Decision

- **The removal set is a third partition of `orchestrate.Plan`.** `Plan.Removed`
  is every unit the subsystem unit registry declares (`subsystem.Every` x
  `Kind.EveryUnit`) that is on disk and that the render no longer produces, carrying
  its on-disk bytes as `Text`. `orchestrate.Orphans(rendered, read)` is the one pure
  computation of it, over a read seam; `Reconcile` fills `Plan.Removed` through it
  with the unit dir's reader, and doctor's `unitDrift` calls it with its own
  `ReadUnit`. A unit outside the registry (the operator's own files, a resident
  slot's `villa-llama-<slug>.container`) is never in the set, so it is never
  touched.
- **`orchestrate.RemoveUnits(plan, dir)` is the only unit-file remover the apply
  path has.** It is traversal-guarded like `WriteUnits`, tolerates an absent file,
  and is a separate function so `WriteUnits` keeps writing exactly `Changed`:
  install's own write path (ADR-0003, ADR-0013) calls `WriteUnits` and is unchanged.
- **`stackapply.Apply` stops, removes and reloads inside the one apply.** After the
  heal and the plan it stops each removed `.container` unit's service (recording
  which were running), writes `Changed`, removes `Removed`, and daemon-reloads once.
  It returns `Applied{Changed, Removed, Stopped}` instead of the written units, so a
  caller's rollback knows what was stopped as well as what is on disk. A Quadlet
  unit cannot be `disable`d (`systemctl --user is-enabled` reports `generated`):
  removing the file and reloading is its disable. `Deps` gains `Stop`, `IsActive`
  and `RemoveUnits`, bound once in `liveStackDeps`.
- **The swap transaction frame captures and restores it.** `captureUnits` captures
  the registry units on disk as well as the units the prior config renders, so the
  bytes of a unit the apply removes are in the capture even when the prior config
  did not render it. On a rollback, `Transact` restores the captured units, reloads,
  and restarts every service it restarted plus every service the apply stopped: a
  failed apply brings the unit file and its running state back. An apply whose only
  effect was a removal is still a `NoOp` to the frame: nothing it proves changed.
- **Doctor names an orphan until it is removed.** A new finding, `orphan-units`
  (WARN, report schema v11), lists the registry units on disk that the loaded config
  no longer renders and tells the operator to run `villa up`. It is separate from
  `drift` because the remediation differs: a re-install writes changed units but
  never removes one.
- **Uninstall tears down what the registry declares.** It reconciles the render
  against the unit dir and adds `Plan.Removed` to the units it removes and their
  services to the ones it stops first.
- **`villa-inferproxy.container` is declared under `Sandbox`.** It is the one unit
  the workspace agent renders and manages (GHSA-gvp9, ADR-0011), so turning
  `workspace_agent` off now removes it like any other gated unit. The sandbox
  network stays undeclared: a `.network` is not a service. `villa update` on the
  sandbox pin now captures and restarts the proxy with the subsystem, as every other
  subsystem's units already are.
- **Resident units are not in the removal set.** `villa model resident rm` already
  stops and removes the slot it orphans inside its own transaction, and a resident
  the config still lists is rendered, so it is never on the removal side. A resident
  removed by a hand edit of `config.toml` is left running, as before: the slot is a
  config entry the resident verbs own, and `model resident rm` refuses a slot the
  config does not list, so the fix for a hand edit is to restore the entry and run
  `villa model resident rm`.
- **Install is unchanged.** Its gates never lower, so it never produces a removal
  set of its own. A stale orphan a re-install meets is left for `villa up`, which
  doctor names.
- **`orphan-units` replaces `IMG-DOC-stale`.** Image generation (#324, ADR-0032)
  shipped a doctor WARN for `villa-image.service` running while the gate is off,
  with a four-step manual removal, because no apply removed a unit. The apply now
  does, so the image-only finding goes and `villa-image.container` is an orphan
  like any other registry unit.
- **The registry is the one list a new unit joins.** Voice (#322, ADR-0030) is
  already in it. Every unit a later subsystem adds (image generation, document
  extraction) is declared in `unitTable` under its subsystem and has its gate turned
  on in the full-stack test fixture (`statefulFixtureInput`), where
  `TestSubsystemUnitsMatchTheRenderedUnits` fails the build on a rendered
  `.container` the registry does not name. Being removable is then free.

## Rejected

**Compute the removal set in `stackapply` from a new `ListUnits` seam.** The reader
already exists in `Reconcile`, and doctor's drift check mirrors `Reconcile` over its
own seam; a second home for "what is on disk but not rendered" is the drift the
cross-package tests exist to catch.

**Remove in `WriteUnits`.** The smallest diff, but install calls `WriteUnits` and
would then remove a unit it never stopped, outside its captured prior state.

**Let the frame stop the removed services and keep `Apply` file-only.** Every other
`Apply` caller (`up`, `restart`, the resident verbs, `restore`, `update`) would
carry its own copy of the stop, which is how the resident verbs grew their own
orphan path.

**Restart every removed service on rollback.** ADR-0015 rejected starting a service
the operator stopped; `Applied.Stopped` records which ones were running so the
rollback brings back exactly those.

**Match resident orphans by the `villa-llama-*` prefix.** A prefix is not the
registry: a hand-written unit under villa's naming would be deleted, and the
resident verbs already own that removal with its rollback.

**Fold the orphan into the `drift` finding.** One finding with two remediations
is a branch inside a message; two findings read as the two facts they are.

## Consequences

- `villa up` and `villa restart` after a config edit that turns a subsystem off
  now stop and remove its units and say `removed N unit(s) no longer rendered`;
  `up --dry-run` lists them. A removal alone starts nothing, as an unchanged `up`
  never did. The removal is idempotent: a second run finds nothing on disk.
- `villa update` and `villa restore` apply through the same `Apply`, so they also
  remove an orphan they meet; their rollbacks do not bring it back running.
  `update` restores the units it captured (since ADR-0038, every unit the config
  renders, which leaves the orphan out), and `restore`
  re-applies the prior config, which writes the unit file again but starts only
  the services restore itself stopped. The gap is a stopped unit that
  `villa restart <service>` starts, never a unit running outside the fit.
- A swap that meets a stale orphan removes it and reports `NoOp` when nothing else
  changed; its rollback restores the orphan's file and restarts it if it was running.
- `villa doctor` on a host with an orphan reports `orphan-units` WARN; the doctor
  `--json` schema version is 11. The three doctor goldens change only in that
  number.
- `Apply`'s callers read `Applied.Changed` where they read the written units, and
  `Applied.Empty()` where they tested for a no-op.
- The resident verbs' capture reads `Plan.Removed` into the prior units, so a
  removal that happens inside `model resident add|rm` rolls back with the rest.
- What the operator could choose instead: a resident dropped by a hand edit could
  join the removal set (then `model resident rm`'s own orphan path should go, so the
  removal has one home); install could remove too, at the cost of carrying the
  stopped services through its own `Mutations` and `Prior`.

## Amendment (#344): removal fails closed

The restart gate reads an unreadable `IsActive` as not running, so a restart never
starts what the operator stopped. Removal has the opposite risk: reading it as not
running deletes the unit file under a container that keeps running, and
`orphan-units`, keyed on the file, can no longer name it. `stackapply` therefore
reads every removed service's state before it stops, writes or removes anything, and
refuses with the service name and the `systemctl --user` check when one cannot be
read; through `Transact` the refusal is an apply error and takes the normal rollback.
`orphan-units` also names a registry service that is active with its unit file absent
(the case `IMG-DOC-stale` covered), in the same finding, so the doctor JSON shape and
schema (12) are unchanged. `orchestrate.RemoveUnits` is a lock sink.
