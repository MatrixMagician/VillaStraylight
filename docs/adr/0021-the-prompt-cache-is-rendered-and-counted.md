---
status: accepted
---

# The prompt cache is rendered explicitly and counted in the fit

llama-server keeps a RAM prompt cache of finished prompts so a returning
conversation does not re-prefill. The pinned image's default cap is `--cache-ram
8192` (MiB; `-1` is no limit, `0` disables it). Villa rendered no `--cache-ram`, so
the cap was whatever the image happened to default to, and neither `recommend.Pick`
nor `residentset.Admit` counted it (#274).

Measured on the dev host on 2026-10-05 (SQ-48): qwen3.6-35b-a3b UD-Q4_K_M, ctx
131072, the pinned ROCm 7.2.4 image (llama-server build 9536), driven through Open
WebUI and then by 24 distinct ~7.6k-token prompts.

| point | prompt cache | VmRSS / VmHWM | GTT used |
|-------|--------------|---------------|----------|
| 37 min of ordinary chat | 22 prompts, 3.1 GiB | 5.0 / 5.0 GiB | 27.70 GB |
| +12 long prompts | 32 prompts, 6.0 GiB | 8.0 / 8.0 GiB | 27.71 GB |
| +8 more | 8.0 GiB, then `cache size limit reached, removing oldest entry` | 9.9 / 10.3 GiB | 27.71 GB |
| +4 more (16 evictions in all) | 28 prompts, 8.0 GiB | 10.0 / 10.3 GiB | n/a |

The cache fills to its cap and then holds there by evicting the oldest entry. It
overshoots by less than one entry (peak about 8.4 GiB). It is host RSS, not GTT:
RSS grew by 3.0 GiB while the cache grew by 2.9 GiB and GTT stayed flat. On unified
memory that RSS comes out of the same DRAM the envelope has already promised to the
weights and KV. Only the 12% headroom could absorb it, and that already has to cover
the OS, the compute buffers and context growth. The headroom is smaller than one
cache on any envelope under 66.7 GiB, which is every 64 GB host, and each resident
unit is another llama-server with another cache.

## Decision

- **One constant owns the cap.** `inference.PromptCacheMiB = 8192`, with
  `inference.PromptCacheBytes` derived from it, live in the inference seam. The
  `--cache-ram` literal stays there too (`TestSeamGrepGate`).

- **It is rendered explicitly, on every chat unit.** `llamaServerFlags` appends
  `--cache-ram 8192` for both backends, so the primary `villa-llama` and every
  resident `villa-llama-<slug>` get it (resident units render through the same
  `ContainerArgs`). `villa-embed` does not: it was measured with no prompt cache
  and a footprint of about 301 MB. The value is never `-1`: an unbounded cache is
  the one setting that has no cost the fit could count.

- **The fit counts it, per server.** `recommend` adds `inference.PromptCacheBytes`
  to the inequality in `pickBest` and `buildRecommendation`, so the fit is `weights
  + KV + headroom + prompt cache (+ projector + draft) <= envelope`.
  `Recommendation` gains `prompt_cache_bytes` (append-only, schema 7 to 8) and its
  `TotalBytes` includes the term. Each resident slot's `Bytes` is its weights + KV +
  its own `PromptCacheBytes`, so N resident units cost N caches, while the headroom
  stays one reserve for the whole set. The table prints it as a `+ prompt cache` row.

## Why 8192

It is the behaviour hosts already have, so rendering it changes nothing a running
stack does, and it is what the measurement above describes. The cache earns it: the
organic log had 46 `found better prompt` hits in 37 minutes, many at similarity 1.000.

## Rejected

**`--cache-ram 0`.** On a hybrid model the in-slot reuse often fails (`forcing full
prompt re-processing ... hybrid/recurrent memory`), so disabling the cache means a
full re-prefill (about 800 tok/s aggregate here) on every returning conversation.
The saved 8 GiB is not worth that.

**Leave the image default and only count it.** The count would then be true only for
as long as the image's default is. A rebuilt tag could change the cost with nothing
in villa's tree to show it. Stating the flag makes the cost ours.

**Count it in the headroom.** Raising the headroom fraction would charge every
envelope for a per-server cost, and a resident set would still not pay per server.

**4096.** Halves the 64 GB-host penalty and still holds about 12 long or 30 short
conversations, but it changes behaviour on every host and was not measured.

## Consequences

- A 64 GB host reserves 8 GiB more than before, and a pick on a tight envelope may
  be a smaller model or context than it was. On the 123.5 GiB dev host the pick
  does not change: 8.0 GiB of a 14.8 GiB headroom was already being spent silently.
- A resident set that fit only because its caches were not counted is refused by
  `villa model resident add`; units already configured keep running and are
  re-sized the same way on the next add.
- `recommend --json` is schema 8 with `prompt_cache_bytes`; `total_bytes` is 8 GiB
  higher than the v1.15 value for the same pick.
- Not covered: the coder fit (`recommend.CoderFit`), which sizes the model that
  replaces the primary in coding mode, does not carry the term yet, and neither does
  the `install` minimum-memory floor. Both predate this ADR and are separate changes.
- Untested: a resident-set host, a non-hybrid model (more KV per token, so it reaches
  the MiB cap sooner) and a `villa work` task.
