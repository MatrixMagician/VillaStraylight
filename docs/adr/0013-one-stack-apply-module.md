---
status: accepted
---

# One stack-apply module renders, heals, writes and reloads

A read-only spike at `2fd9314` found the `orchestrate.RenderInput` assembly
(backend, model file, resident units, models dir, host villa path) hand-copied at
eight `cmd/villa` sites plus install's `Deps.Render`, and the Reconcile → write →
daemon-reload tail copied at seven. Each copy chose its own inputs, so only
`coding-mode enter` and `install` built the coding descriptor: `up`, `restart`,
`backend set`, `speculation set`, `tools-mode`, `model swap`, `restore`, `update`
and the resident verbs all re-rendered villa-llama out of coding mode while
config.toml still said it was on (#249). The inference-secret self-heal
(GHSA-qxg9, ADR-0011) had two routes, and the up/restart one ran after the render,
so a resident set's chat UI unit was written with an empty key. This ADR records
the seam #254 chose.

## Decision

- **`internal/stackapply` is the one home for turning a target config into unit
  files.** It is a pure core with a `Deps` struct of funcs: `Catalog`, `ModelsDir`,
  `HostVillaPath`, `Render` (the pinned render), `UnitDir`, `Reconcile`,
  `WriteUnits`, `DaemonReload`, `SaveConfig` and `WriteInferenceSecretEnv`. Its
  interface is four functions:
  - `Render(d, cfg)` derives every render input from the config and renders,
    writing nothing. The served model file comes from `ServedTarget(cfg)` (the coder
    in swap-residency coding mode, the chat model otherwise), the coding descriptor
    is translated from the served catalog entry, the agent ctx from
    `cfg.CoderAgentCtx`, the resident slots through `ResidentUnits`.
  - `Plan(d, cfg)` renders and reconciles against the unit dir, writing nothing:
    `up --dry-run`, a transaction's capture, and doctor's drift check.
  - `Apply(d, cfg)` heals the inference secret, then renders, reconciles, writes
    the changed units and daemon-reloads. It returns the changed units, and still
    returns them when the reload after the write fails, so a caller's rollback
    knows what is on disk. Nothing changed means no unit written and no reload
    (the heal still rewrites the env file).
  - `Restore(d, units)` writes captured unit bytes back verbatim.
- **The caller decides what to start or restart**, from the changed units Apply
  returns. `serviceUnits` stays in `cmd/villa` (ADR-0012), and so did the swap
  cores' capture/rollback frames until ADR-0015 moved them into one frame,
  `stackapply.Transact`, beside `Apply`.
- **One live adapter.** `liveStackDeps()` in `cmd/villa/lifecycle.go` wires the
  catalog, `livePinnedRender` (pinresolve plus the speculation, projector and
  tools-mode ctx floor it already resolved), the Quadlet unit dir,
  `orchestrate.WriteUnits`, systemd and the secret writers. Doctor overrides two
  fields of a copy (the read-only unit dir and the installed binary mount).
- **One secret-heal route, before the render.** A missing secret is generated and
  persisted with the target config, and the 0600 env file is rewritten on every
  Apply. Render and Plan never heal, which keeps `--dry-run` and every capture
  side-effect free by construction rather than by an early return.
- **Install renders through `stackapply.Render`.** `install.Deps.Render` now takes
  the assembled config; `ModelFile`, `ResidentUnits`, `HostVillaPath` and
  `CodingRender` are gone. The write half does not fit install: its transaction
  captures before its first mutation, and it generates the secret in memory and
  persists it with the flow's one `SaveConfig`, so a `--dry-run` persists nothing
  (ADR-0003). Install keeps its own write, reload and env-file step.

## Rejected

**Widen `livePinnedRender` to take a config, plus a `liveApply` beside it in
`cmd/villa`.** The smallest diff, but the derivation would stay testable only
against the live catalog and pin store, and `internal/install` could not reach it,
so install would keep its own `CodingRender` seam: the second copy that let #249
happen. It also keeps decision logic in the command tier, which ADR-0012 moves out.

**Put the derivation in `internal/orchestrate`.** The renderer is deliberately
catalog-free (the catalog-to-inference translation belongs to the caller), and
Apply's heal saves config. Both would turn the pure renderer into a second impure
hub.

**A `Stack` interface with a live struct behind it.** One implementation; the
project's shape is a `Deps` struct of funcs with one live wiring and one fake.

**Move speculation, the projector and the tools-mode ctx floor into
`stackapply` too.** They are already derived in one place, `livePinnedRender`,
which reaches them through the `Render` seam with their own tests. Moving them
widens the diff with no second caller to gain.

**Heal only when the plan changed, as `liveWriteUnits` did.** Knowing whether
anything changed needs the render, and the render needs the secret. Healing after
the render is the ordering bug; rendering twice to learn the answer first buys
nothing but a skipped idempotent env-file write.

## Consequences

- #249 is fixed at its root: every verb that regenerates units, and doctor's drift
  check, renders coding mode the way `coding-mode enter` does.
  `TestUpKeepsCodingModeTheWayEnterRendersIt` renders the stack the way `up` does
  for a swap- and a shared-residency coding-mode config and compares villa-llama
  with what enter wrote. A re-install without `--coding-agent` on a host already
  in coding mode now keeps it; install used to render coding mode only when this
  run's gate set it.
- A resident set's chat UI unit carries the inference secret in the same apply
  that generates it. The env file is rewritten on every apply, including one that
  changes no unit, which also restores one deleted by hand. `Restore` does not
  heal: it writes bytes that were on disk before the transaction's own Apply.
- `villa update` no longer daemon-reloads when no unit changed. The resident verbs
  still reload exactly once per run (Apply's, or their own when only an orphan unit
  was removed). `up` and `restart` narrate "wrote N changed unit(s)" without the
  unit dir.
- Deleted: `renderInferenceUnits`, `liveRenderUnits`, `renderAndWrite`,
  `liveWriteUnits`, `ensureInferenceSecretWith`/`File`,
  `lifecycleDeps.ensureInferenceSecret`, `applyReconcile`, `codingServedTarget`,
  `codingDescriptor`, `liveCodingRender`, and seven copies of the
  Reconcile → write → reload closure. `liveResidentUnits` delegates to
  `stackapply.ResidentUnits`.
- No golden bytes changed. `TestEveryRenderInputCarriesTheResidentSet` and
  `TestEveryRenderCallGoesThroughThePinnedEntryPoint` still hold: the module builds
  the one `RenderInput` and reaches `orchestrate.Render` only through the pinned
  seam.
- Not migrated: `internal/status` still assembles its own `RenderInput`. It reads
  only the service set and published ports from it, which coding mode does not
  change, so its answer is unaffected; it is the last hand-built input and a
  candidate to route through `stackapply.Render` when status is next touched. The
  cutover proof (`liveProve`, driven by `stackapply.Transact`, ADR-0015) and its
  served-model choice are unchanged (#255, #256). `liveCodingProve` was deleted by
  ADR-0015.
