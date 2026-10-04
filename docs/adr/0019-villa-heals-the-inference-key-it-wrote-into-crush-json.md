---
status: accepted
---

# Villa heals the inference key it wrote into crush.json, and nothing else

AGENT-04 says villa surfaces crush.json drift and never overwrites the file: the
operator may hand-edit it, and a silent rewrite would destroy their edits. v1.14
made llama-server require an api key (GHSA-qxg9, ADR-0011) and changed
`agent.Render` to write `cfg.InferenceSecret` into `providers.villa.api_key`, where
it had written the placeholder `local`. A host whose crush.json predates that change
keeps the placeholder. Every Crush call then gets `unauthorized: Invalid API Key`,
`tools-mode enter` fails its proof and rolls back, and doctor reports a drift whose
remediation ("re-render") names no verb (#277). The drift is villa's own: it changed
a value only villa writes, and the operator never edited anything.

## Decision

- **A key-only drift is healed, not reported.** `agent.KeyOnlyDrift(onDisk,
  rendered)` is a pure predicate. It is true when the two documents differ, and
  substituting the rendered `providers.villa.api_key` into the on-disk document
  makes them semantically equal (the same canonical compare `DetectDrift` uses). In
  that case, and only then, villa writes its rendering over the file. Any other
  difference, alone or together with a stale key, is drift as before: reported,
  never written.

- **The previous file is kept.** Before a heal, the on-disk bytes are written to
  `crush.json.bak` beside it, 0600, so the heal is reversible by hand.

- **Every stack apply heals, and so does `villa code`.** `stackapply.Apply` runs the
  heal after the inference-secret heal and before it renders, through a
  `HealAgentConfig` seam that is a no-op when the coding agent is off or the file
  is absent. Up, restart and every swap therefore repair an upgraded host,
  `tools-mode enter` included, whose proof runs Crush. `agent.Run` heals in the same
  place it already writes a first-run config, and reports it as a warning. An
  I/O error during the heal fails the apply before any unit is written.

- **Doctor stays read-only.** It names the key-only case in its finding and says
  that any stack apply or `villa code` heals it, instead of the generic "review your
  edits or re-render".

- **AGENT-04 is narrowed, not dropped.** It now reads: villa never overwrites the
  operator's edits to crush.json. A value villa alone writes, and that changed under
  the operator, is villa's to correct.

## Rejected

**An explicit `villa code --rerender`.** It keeps AGENT-04 absolute, but every host
upgraded from before v1.14 stays broken until its operator reads doctor and runs
it. The inference secret itself was introduced the other way: generated and healed
without operator action (ADR-0013).

**Heal only the literal placeholder `local`.** Narrower, but it encodes one
release's history. A future change to a villa-owned field would need another
special case; the sole-difference rule covers it.

**Heal from doctor.** Doctor is the read-only gate (ADR-0017). A gate that writes
cannot be run to find out what is wrong.

## Consequences

- An operator who hand-set `api_key` to some other value, and changed nothing else,
  has it replaced on the next apply. The value was already refused by llama-server,
  and the old file is in `crush.json.bak`.
- A rotation of `inference_secret`, if one is ever added, propagates to Crush on the
  next apply with no extra code.
