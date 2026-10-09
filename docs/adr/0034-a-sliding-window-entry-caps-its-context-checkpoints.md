---
status: accepted
---

# A sliding-window entry caps its context checkpoints

ADR-0029 put the sliding-window KV cache into the fit. llama-server keeps a second
sliding-window structure the fit did not see: context checkpoints. A slot saves a
copy of its sliding layers' cache at a few points of every prompt it processes (three
per prompt on the dev host) and keeps up to `--ctx-checkpoints` of them, default 32,
in host memory. `--cache-ram` bounds only the prompt cache an idle slot is saved into;
an active slot's checkpoints sit outside it. villa rendered no cap (#323).

A checkpoint holds the sliding layers' cells for the tokens it covers, at most one
window. On Gemma 4 31B (50 sliding layers x 16 KV heads x 256 dims x K+V x f16 =
819,200 bytes per cell, window 1024) the side server logged
`created context checkpoint 18 of 32 (pos_min = 3873, pos_max = 8480, n_tokens = 8481, size = 800.013 MiB)`:
every checkpoint past the first 1024 tokens is 1024 cells plus about 13 KB. Three per
prompt fill the default 32 in eleven turns, 25 GiB in one slot, and four slots can
hold 100 GiB that nothing villa rendered or counted.

## Decision

- **villa renders `--ctx-checkpoints 1` for a unit whose model carries a `swa`
  block, and for no other unit.** The value is `inference.SWACtxCheckpoints`. The
  primary unit and each resident slot are marked by their own catalog entry
  (`stackapply` sets `SlidingWindow` from `SWA != nil`), so a Qwen unit renders no
  flag and stays byte-identical.
- **The fit counts the cap.** `recommend` adds
  `4 slots x SWACtxCheckpoints x min(ctx, window)` cells at the sliding layers'
  per-cell size to the entry's KV term, through the `kvCacheBytes` every reader
  uses, so chat, coder, swap sizing, resident admission, install and status see it.
  `--json` keeps its schema: `kv_cache_bytes` is the sum. On Gemma 4 31B the term is
  3.36 GB (3,355,443,200 bytes) at any ctx at or above the window.
- **The render carries a bool, not a number.** `RunSpec`, `RenderInput` and
  `ResidentUnit` carry `SlidingWindow bool`, and the seam renders the constant, so
  a caller cannot render a cap the fit did not count.
- **1 is the smallest value that kept every turn's prefix under load.** The method
  and the numbers follow.

## How the value was chosen

A side llama-server on port 8081 from the pinned ROCm 10.0 image (b11430), the unit's
device and flag shape, Gemma 4 31B at `-c 32768`, one `--ctx-checkpoints` value per
run, with `0` as the negative control. The criterion: `cache_n > 0` on every request
after the first, and no `forcing full prompt re-processing` in the log. Each cell
below is `cache_n / prompt tokens`.

The first two workloads could not choose, because the `0` control passed them:

| workload | `--ctx-checkpoints` | turns 2-6 | regenerate | edit |
|----------|---------------------|-----------|------------|------|
| six turns, thinking off, reply echoed back | 0, 1, 2, 4, default | all kept (turn 6 7073/8485) | | |
| six turns, thinking on, reasoning dropped from history, then a regenerate and an edit of turn 6's user message at its first token | default | all kept | 8495/8496 | 7103/8496 |
| same | 0 | all kept | 8480/8481 | 7088/8481 |

With one conversation running, the unified sliding cache (4 x 1024 + 512 = 4608
cells) holds about four and a half windows of that one sequence, so a rollback of
up to about 3500 tokens finds its cells still there and restores no checkpoint.
Checkpoints matter when other slots are active and each sequence is held to about
one window. The third workload adds that load: three background clients keep three
slots generating on distinct ~3000-token prompts while the conversation runs in the
fourth.

| under load, `--ctx-checkpoints` | turn 2 | turn 3 | turn 4 | turn 5 | turn 6 | regenerate | edit | forced re-processing |
|------|------|------|------|------|------|------|------|------|
| 0 | 0/2829 | 2829/4242 | 0/5655 | 5655/7068 | 7068/8481 | 0/8481 | 0/8481 | 13 (incl. background) |
| 1 | 1411/2829 | 2824/4242 | 4237/5655 | 5650/7068 | 7063/8481 | 8476/8481 | 0/8481 | 1 (the edit) |
| 2 | 1416/2829 | 2824/4242 | 4242/5655 | 5650/7068 | 7063/8481 | 8476/8481 | 0/8481 | 1 (the edit) |
| 4 | 1416/2829 | 2824/4242 | 4237/5655 | 5655/7068 | 7063/8481 | 8480/8481 | 19/8481 | 0 |
| default (32) | 1416/2829 | 2824/4242 | 4242/5655 | 5650/7068 | 7063/8481 | 8476/8481 | 7084/8481 | 0 |

A turn and a regenerate roll back to the end of the previous prompt, and the newest
checkpoint sits five tokens before that end, so one checkpoint serves them. An edit
of the last user message rolls back to the start of that message, to a checkpoint
three or more creations old, which 1, 2 and 4 have already evicted. Keeping it
needs more than 4, at 3.36 GB of fit per step on Gemma 4 31B. Losing it costs one
prompt's prefill (8481 tokens at about 200 tok/s here), not a wrong answer. Turns
are the common case and the edit is the rare one, so the cap is 1.

## Alternatives

| option | verdict |
|--------|---------|
| render the cap only | rejected: a rendered flag nobody counts still admits an entry into memory it will take |
| count the worst case at the default of 32 | rejected: 107 GB on Gemma 4 31B refuses the entry on every host, for memory a cap makes unnecessary |
| a cap large enough to keep a last-message edit (more than 4, default 32 measured) | rejected for now: each step is 3.36 GB of fit on Gemma 4 31B, for a prefill saving on a rare request; one constant changes it |
| measure a peak and count that | rejected: a peak is one workload's number; the next workload is not bounded by it |
| a per-entry cap in the catalog | rejected: the cap is a property of how villa serves, not of the model; one constant keeps the render and the fit in step |
| `CtxCheckpoints int` on the render inputs | rejected: an int lets a caller render a value the fit never counted |
| cap the transient validate and ceiling runs too | not done: they serve one or two requests and never accumulate checkpoints |

## Consequences

- A sliding-window entry's KV term has three parts: the global layers' cache at ctx,
  the bounded sliding cache (ADR-0029), and the checkpoint cap. Every Qwen entry's
  term is unchanged.
- `TestContainerArgsLeaveSWASizingToDefaults` still keeps `-np`, `-ub`, `-kvu` and
  `--swa-full` out of every unit, so the 4 slots the term multiplies by stays the
  default. `--ctx-checkpoints` is the one sliding-window flag villa renders, and its
  short form and old alias stay forbidden.
- A host that fit Gemma 4 31B with a small margin may now refuse it, or `model swap`
  falls back to the entry's `default_ctx` (ADR-0024). That is memory the server can
  take.
- Editing the last message of a long conversation on a busy server re-processes the
  prompt.
- The about 13 KB per checkpoint above the cell count is left to the 12% headroom.
