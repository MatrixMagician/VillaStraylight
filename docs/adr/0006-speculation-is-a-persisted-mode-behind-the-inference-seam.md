---
status: accepted
---

# Speculation is a persisted mode rendered behind the inference seam

Token generation on gfx1151 is memory-bandwidth bound, and the pinned llama-server
can speculate. Villa never passed a speculation flag. Measured on the dev host
against Qwen3.6-35B-A3B on the ROCm 7.2.4 image, greedy, 256 tokens:

| mode | cold tg | warm tg | drafts accepted |
|------|---------|---------|-----------------|
| none | 49-50 | 49-50 | n/a |
| ngram-simple | 44-50 | 44-50 | 12 of 219 |
| ngram-map-k | 47-49 | 47-49 | 2 of 49 |
| ngram-cache | 47-50 | 48-50 | 9 of 14 |
| ngram-mod | 49-50 | 62-140 | 81-207 of ~245 |

Only `ngram-mod` pays: it is neutral on a prompt the server has not seen and up
to 2.8x on repeated output, which is what `villa code` produces. It needs no
download and no memory. So villa's `ngram` mode is `ngram-mod`, and the other
ngram variants are not offered. The same probe on Qwen3-Coder-30B-A3B read 67-68
without speculation, 62-74 cold and 65-103 warm with `ngram-mod`.

A draft sidecar was measured too, and it loses on every entry in the catalog:

| target | draft | none tg | draft-simple tg | accepted |
|--------|-------|---------|-----------------|----------|
| Qwen3.6-35B-A3B UD-Q4_K_M | Qwen3.5-0.8B Q8_0 | 49-50 | 36-42 | 86-91% |
| Qwen3-Coder-30B-A3B UD-Q4_K_XL | Qwen3-0.6B Q8_0 | 67-68 | 50-67 | 90-93% |

Acceptance is high, so the draft is not the problem. Every seed entry is a
mixture-of-experts model with about 3B active parameters per token, so a dense
0.6B-0.8B draft costs a third to a quarter of a target step, and verifying its
tokens costs the target a step per batch besides. The reference project's 2x
came from a dense 27B target, where the draft is under a fortieth of a step.
Until the catalog carries a dense entry, `draft` is a mode that would make every
supported model slower, so it does not ship. The config vocabulary rejects it as
unknown rather than accepting a value that does nothing, and the sidecar shard
shape the draft would have needed lands with the vision projector instead, which
does pay.

## Where the mode lives

**Persist the resolved mode in `config.toml`**, the chosen option. `speculation`
takes `off`, `ngram`, `draft` or is unset. `villa recommend` resolves an unset
value from the catalog entry's qualifications and the fit, and `--save` and
`villa install` persist what was resolved, exactly as they persist the backend.
The render reads config only, and an unset value renders speculation off. That
keeps config the single source of truth, keeps every existing unit byte-identical
on upgrade (a v1.8 install has no `speculation` key, so nothing changes until the
operator asks), and gives `villa speculation set` one value to swap.

**Resolve at render time from the catalog**, rejected. Each render would decide
again, so `villa status` could report a mode the running unit was not started
with, and a catalog edit would silently change a running stack on the next
reconcile.

**Catalog-only, no config field**, rejected. The operator could not choose, and
an entry's qualification would become a command.

## How the flag reaches the unit

`RunSpec` gains an optional speculation descriptor, nil by default, the same
construction as the coding-mode descriptor. Both backends append the identical
delta through one shared helper inside `internal/inference`, so `--spec-type`,
`ngram-mod` and the draft device token stay behind the seam grep gate. The
draft's device is the backend's own residency device token, never a literal
elsewhere.

The translation from config plus catalog entry to descriptor happens once, in
`livePinnedRender`, the funnel every command render already goes through. A
config that asks for a mode the served entry is not qualified for is a refusal,
never a silent downgrade.

## The swap

`villa speculation set <mode>` composes the backend swap's transaction rather
than forking it: prove-current is the fit guard, then capture, mutate config,
re-render, restart, prove, else roll back verbatim. The frame in `backendswap`
is generalised with a mutate closure so the two verbs share one rollback.

## What the qualification means

A catalog entry carries `ngram_safe` only with a `ngram_provenance` naming the
measurement, and the load fails closed without it. The seed carries a
qualification only for an entry probed on this hardware. Absence never widens
what villa will do.
