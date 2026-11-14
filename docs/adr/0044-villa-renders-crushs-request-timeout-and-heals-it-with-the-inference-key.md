---
status: accepted
---

# Villa renders Crush's request_timeout and heals it with the inference key

Crush aborts a request that sends nothing for `options.request_timeout` seconds
(streaming requests use it as an idle timeout, non-streaming ones as a hard
deadline; 0 disables it, a negative value is invalid). Its default is 60 s at the
pinned v0.93.1. Every Crush session opens with its system prompt and tool list,
about 17k tokens. gemma-4-31b prefills those at 219 to 221 tok/s, so 71 to 77 s
pass before the first token, Crush hangs up at 60 s, and no round trip can start:
`villa code` and the `agent-tool-call` doctor proof both fail on that model (#351).
ADR-0031 diagnosed the cause and deferred this change as a separate decision, since
it changes crush.json, the reference AGENT-04 compares against.

## Decision

- **villa renders `options.request_timeout`, never omitted.** `agent.Render` and
  `agent.RenderSandbox` write it from the context window they already render
  (`CoderAgentCtx`, else 32768, and `Ctx`, else 32768). An omitted key is Crush's
  default, which is the bug.

- **One pure formula.** `agent.RequestTimeoutSeconds(ctx) = clamp(ceil(ctx / 200),
  120, 600)`, in `internal/agent/timeout.go`. 200 tok/s is a floor under the slowest
  prefill villa has vetted, so a whole-window prefill at that rate fits the bound.
  16384 gives 120 s, 32768 gives 164 s, 65536 gives 328 s, and 131072 or more gives
  600 s. The result is never zero or negative: an unset window reads as 32768.

- **The timeout joins the villa-owned set.** `agent.VillaOwnedDrift` replaces
  `KeyOnlyDrift`. It is true when the documents differ and substituting the rendered
  `providers.villa.api_key` and `options.request_timeout` into the on-disk document
  makes them semantically equal. Only then does a stack apply or `villa code`
  rewrite the file, after keeping the old bytes as `crush.json.bak` (ADR-0019).
  Any other difference, alone or beside these two, is reported and never written.
  `DriftReport.ConfigKeyOnly` is renamed `ConfigVillaOwned`.

- **Doctor stays read-only and stays truthful.** The drift finding names both
  values. The prefill remediation of `agent-tool-call` names the bound villa
  renders for the agent's window, and says a crush.json that predates it keeps
  Crush's 60 s default and is fixed by `villa up` or `villa code`.

## Rejected

**`request_timeout = 0`.** It disables the bound, so a dead server hangs Crush
forever on a non-streaming call.

**A measured per-model prefill rate.** None is stored anywhere (`bench` reports
are per run, `evalstore` holds none). Feeding a live reading into the pure,
golden-pinned `Render` would make the rendered file, and so drift, change with GPU
load, and would make doctor depend on a probe (ADR-0017). A stored rate needs a new
store and is a separate issue, to be raised if the floor proves wrong on another
model.

**Report-only drift for the new key.** Every existing host's crush.json lacks the
key, so each would sit in permanent drift and keep failing on Gemma until its
operator edited a generated file by hand.

## Consequences

- Every host upgraded from an older villa has a crush.json that differs only by
  this key. The first stack apply or `villa code` heals it, and `crush.json.bak`
  holds the previous file. A later heal replaces an earlier backup.
- An operator who hand-set `request_timeout`, or `api_key`, and changed nothing
  else has it replaced (the trade ADR-0019 made for the key). A change to the
  agent's window moves the bound and heals on the next apply.
- A model slower than 200 tok/s at prefill still fails when its prefill outlasts
  the bound, and doctor then names the prefill cause with the rendered number.
- The `$schema` hint (`https://charm.land/crush.json`) is stale: its `Options` has
  no `request_timeout` and sets `additionalProperties: false`, so an editor may flag
  the key. It is an editor hint only, and the `crushSchema` const is unchanged.
- Crush's default moved to 2 minutes on its main branch. villa renders the value
  itself, so the pinned version's default no longer matters to it.
