---
status: accepted
---

# Vision follows the swapped-in model

`vision` in config.toml is the persisted decision to start llama-server with the
served entry's projector. `villa recommend --save` and `villa install` wrote it, from
recommend's answer for the pick: on only when the entry ships a projector and it fits
beside the model. `villa model swap` changed the model and left `vision` alone. So a
stack on `qwen3.6-35b-a3b` with vision on could not swap to any text-only entry: the
render refused, because a projector-less entry under `vision = true` is a decision the
render cannot honour (#299). The refusal named `recommend --save`, which re-picks the
recommended model, and `install`, a whole-stack operation for one field. No verb
turned vision off, and `config set` does not accept the key.

The #297 vet hit this on every swap off the dev host's default and worked around it
by hand-editing `vision = false`. The field is `omitempty`, so the next config write
dropped the line, and a later `sed` to turn it back on matched nothing.

Three shapes were on the table: the swap decides vision for its target, a `villa
vision on|off` verb, or `config set vision=false`.

## Decision

- **`villa model swap` writes the target's vision answer.** The swap's fit guard
  already runs recommend's fit math for the target, and that run already answers
  vision the same way `recommend --save` does. The swap writes that answer beside the
  model and quant, so the decision always describes the model it renders.
- **The swap says when vision changed.** The operator named a model, not a vision
  setting, so a change is printed after the swapped line ("vision turned off: …" or
  "vision turned on: …"). A swap that leaves vision where it was says nothing about it.
- **A swap that turns vision on pulls the projector.** The swap's on-disk check now
  covers every file the entry declares, projector and draft included, so weights
  fetched before an entry gained a projector are pulled again for the missing file
  only. The dashboard's on-disk column uses the same check.
- **No new verb.** A `vision on|off` verb would let an operator hold vision off on a
  model with a projector. Nobody has asked for that, and it would be a second writer
  of a field whose truth depends on a file being on disk.
- **The render's refusal stays.** `vision = true` on a projector-less entry is now
  reachable only by a hand edit or a catalog change, and it still refuses rather than
  rendering text-only. Its remediation names `villa model swap <model>`, which
  re-decides vision for the model it serves.
- **The swap's capture does not refuse that config.** The swap frame snapshots the
  units the prior config renders before it changes anything. When the render refuses
  the prior config, capture takes every `villa*` unit on disk instead, so the
  remediation the refusal names can run.

## Consequences

- A swap to a vision model turns vision on even when the operator had hand-set it off
  for that model. That matches `recommend --save`, which has always done the same.
- The dashboard's switch response does not carry the vision change; `status` shows
  the `vision` row after the switch.
- The dashboard may now show a model as not on disk when its weights are present but
  a sidecar is missing. That is the truthful reading: the swap would pull.
