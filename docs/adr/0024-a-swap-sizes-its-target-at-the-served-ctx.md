---
status: accepted
---

# A swap sizes its target at the ctx it will serve

`villa model swap` refuses a target that does not fit the memory envelope. Its fit
check ran `recommend.Pick` with only the model overridden, so it sized the target at
the catalog's `default_ctx` and with the speculation mode its qualification would
pick. The swap then kept the configured `ctx` and `speculation`, and the render served
those. On the dev host `ctx` is 131072. A swap to `qwen3.5-2b` (`default_ctx` 8192)
checked its KV cache at a sixteenth of the context the unit then served (#301). Since
ADR-0023 the same check also decides `vision`, so the gap now reaches a persisted
field.

The catalog has no per-model context ceiling, so the only question is the memory fit.

## Decision

- **The fit sizes the target the way the render serves it.** It uses the configured
  `ctx`, or the entry's `default_ctx` when that is unset, raised to the entry's
  `agent_ctx` in tools mode as `livePinnedRender` raises it. It reserves for the
  persisted speculation mode, with an unset mode counted as off, as `liveSpeculation`
  renders it. Shared-residency coding mode serves the chat model itself at
  `coder_agent_ctx`, with its own qualification picking the mode when a mode is
  persisted at all, so it is sized that way; with a separate coder, the chat model is sized for the render after coding
  mode exits.
- **A ctx that does not fit falls back to the target's default.** The rule, owned by
  `modelswap.Size`:
  1. Unset `ctx`: size at `default_ctx` and leave `ctx` unset.
  2. The target fits at the configured `ctx`: keep it.
  3. It is over the envelope at the configured `ctx`, and that ctx is above its
     `default_ctx`: size again at `default_ctx`. If it fits, the swap writes
     `ctx = default_ctx` and prints `ctx reset to <default>: <configured> does not fit
     <model>` after the swapped line.
  4. It fits at neither: refuse, naming the default's shortfall.
- **Only a memory shortfall is retried.** A configured ctx at or below the default
  would only grow. A requested draft that does not fit beside the model is a
  shortfall: its KV shrinks with the ctx. An unqualified speculation mode is not a
  shortfall, and no ctx cures it, so it is refused as itself (`model swap: refusing
  — speculation: …`) rather than as "won't fit". Before this, it was rendered and
  then rolled back.
- **Vision and the draft are judged at the chosen ctx.** The `Fit` that `Size`
  returns is the one the swap writes `vision` from.
- **The dashboard's fit column runs the same rule.** It folds the same `swapFit` and
  `modelswap.Size`, so it shows the verdict and the ctx a switch would get.
- **The pull is announced from inside the swap**, after the fit guard (`OnPull`), so
  a refused target is never announced as pulling.

## Consequences

- A swap can lower `ctx`. It says so, and swapping back to a model that fits at the
  larger ctx does not raise it again: the operator's earlier ctx is not remembered.
  `villa config set ctx` or `villa recommend --save` raises it.
- A swap with `speculation = ngram` to an entry not qualified for it is refused before
  anything is pulled or written. It is no longer applied and rolled back.
