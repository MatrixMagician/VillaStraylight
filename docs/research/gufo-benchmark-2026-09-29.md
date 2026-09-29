# gufo versus llama.cpp, measured on this host

Measured 2026-09-29 on the gfx1151 dev box (AMD RYZEN AI MAX+ 395, Radeon 8060S, 125 GiB unified), against [the evaluation](gufo-inference-engine.md). Every number below was produced here, not read from a write-up. The per-run evidence is committed in [`gufo-benchmark-artifacts/`](gufo-benchmark-artifacts/); the harness that produced it is throwaway and lives outside the tree in `~/.jcode/scratch/gufo-bench/`.

Both engines serve the same file, `Qwen3.8-27B-UD-Q4_K_XL.gguf`, the one already in `~/.local/share/villa/models/`. Identical rootless-Podman posture on both sides: same devices, same `keep-groups`, same loopback publish, same read-only model bind. Only the image and the server invocation differ.

- gufo `0.2.0`, `ghcr.io/gufo-org/toolboxes/gufo-runtime@sha256:39698a66…`
- llama.cpp, villa's own vetted default, `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1…`, started with villa's own mandatory flags (`-ngl 999 -fa 1 --no-mmap -lv 4 --metrics`)

Methodology mirrors `internal/bench/bench.go`: a warmup measured then discarded, pp and tg carried separately, every run residency-checked with a non-resident run void rather than counted as a slow pass, acceptance computed only over runs that actually drafted. Five kept reps per configuration, greedy, thinking off on both sides, ctx 16,384 (villa's coding-mode agent context). No run was void in any configuration.

## Headline

| Configuration | pp tok/s | tg tok/s | Cold load |
|---|---:|---:|---:|
| llama.cpp rocm-7.2.4, AR | 317.11 ±2.54 | 11.54 ±0.19 | 2.73 s |
| llama.cpp rocm-7.2.4, ngram | 318.64 ±0.88 | 47.36 ±31.32 | 2.71 s |
| llama.cpp rocm-7.2.4, ngram+draft-mtp | 308.19 ±2.50 | 55.38 ±23.12 | 3.24 s |
| **gufo 0.2.0, AR** | **566.24 ±8.06** | 12.06 ±0.07 | **1.18 s** |
| **gufo 0.2.0, DFlash2** | 530.86 ±2.45 | 21.45 ±0.38 | 2.27 s |

Median of five kept runs, sample stddev. The llama.cpp rows are villa's shipped configurations: `ngram` is what `config.toml` runs today, and `ngram+draft-mtp` is what ADR-0009 qualified for this entry.

## Prompt processing: the win is real, and bigger than gufo claims

**gufo does 566.24 tok/s against llama.cpp's 317.11, a 78.6% gain**, on a 3,100-token prefill at ctx 16,384. The stddev is under 1.5% on both sides across five runs.

This reproduces gufo's published +83.6% at depth 0 closely enough to trust the direction. It also holds at the context villa's agent path actually uses, which is the gap in gufo's tables (they publish 4,096 and 131,072, not 16,384).

Speculation costs gufo a little prefill (530.86 with DFlash2) and costs llama.cpp about the same in relative terms. The ordering does not change.

## Cold load: 2.3x, not the published 4x

**1.18 s against 2.73 s.** gufo's own table claims 5.87 s against 23.33 s, a 4x gain. On this host llama.cpp loads far faster than gufo measured it, so the real gain is 2.3x, not 4x. Both are cold in the sense that the container is new; the host page cache is warm for a file that has been on this disk since September 7, which is the honest caveat on both sides.

1.55 seconds saved per load is real but small. It does not, on its own, change the cost basis of `model swap` the way the published 17-second gap would have.

## Token generation: llama.cpp wins, and it is not close

This is the finding that inverts the recommendation, and it took a controlled prompt to see.

| Configuration | tg per run, sorted |
|---|---|
| llama.cpp AR | 11.1, 11.4, 11.5, 11.6, 11.6 |
| llama.cpp ngram | 18.4, 21.8, **47.4**, 51.7, 96.5 |
| llama.cpp ngram+draft-mtp | 23.0, 34.8, **55.4**, 61.8, 82.0 |
| gufo AR | 11.9, 12.0, 12.1, 12.1, 12.1 |
| gufo DFlash2 | 20.8, 21.4, **21.4**, 21.6, 21.8 |

Autoregressive, the two engines are a wash: 12.06 against 11.54, a 4.5% edge to gufo. Every meaningful difference comes from speculation, and **villa's shipped speculation beats gufo's by a factor of 2.6** (55.38 against 21.45).

The acceptance rates say why. Villa's `ngram-mod` accepted 86.9% of drafts on this workload and `ngram+draft-mtp` 68.0%, while gufo's DFlash2 accepted 35.2%. DFlash2 is a separately-trained draft model; `ngram-mod` builds its drafts from the prompt itself, and villa's agent workloads are exactly the repetitive, quote-heavy text n-gram drafting is best at.

The variance is the other half of the story, and it cuts against llama.cpp. The ngram rows swing from 18.4 to 96.5 tok/s run to run, so the median is not a number you can promise a user. gufo's DFlash2 sits in a 1-tok/s band. Villa is faster in the median and much less predictable; gufo is slower and steady.

**On a repetitive prompt this gap is even wider.** My first spec used one paragraph repeated 40 times, which flatters n-gram drafting. I rebuilt the filler from varied sentences and re-ran, which is the table above. The repetitive run is in `spec-ctx16k-results.json` and shows llama.cpp ngram+draft-mtp at 36.52 against gufo's 21.86.

## The agent turn: gufo wins the long prefix, loses the short one

The pp and tg tables point opposite ways, so neither settles the question. An agent turn does both. This probe prices one `villa work` shaped turn end to end: a large per-turn prefix, a 400-token answer, five reps after a warmup, both engines in their production configuration.

Raw wall-clock is not the comparison, because the two engines stop at different points. gufo hit EOS around 130 tokens where llama.cpp ran to the 400-token cap, so raw timing would credit gufo for answering less. The figures below price both for the same work: the measured prefill, plus a full 400-token answer at each engine's measured decode rate.

| Prefix | Engine | Turn cost | Prefill | Decode |
|---|---|---:|---:|---:|
| ~8,800 tokens | **gufo DFlash2** | **36.24 s** | 15.87 s | 20.41 s |
| ~8,800 tokens | llama.cpp ngram+draft-mtp | 50.58 s (+39.6%) | 29.51 s | 21.05 s |
| ~2,270 tokens | **llama.cpp ngram+draft-mtp** | **21.53 s** | 7.48 s | 14.08 s |
| ~2,270 tokens | gufo DFlash2 | 24.26 s (+12.7%) | 4.16 s | 20.10 s |

**There is a crossover, and it sits inside villa's operating range.** At an 8,800-token prefix gufo finishes a turn 39.6% sooner. At 2,270 tokens llama.cpp wins by 12.7%. Prefill is where gufo buys its lead (15.87 s against 29.51 s at the long prefix) and decode is where it gives it back.

Where the crossover falls depends on llama.cpp's n-gram acceptance, which is workload-dependent and unstable. Solving the measured rates: when llama.cpp's decode runs at 19.0 tok/s there is no crossover at all and gufo wins at every prefix length. When it runs at 28.4 tok/s, as it did on the shorter prompt, the crossover lands near 4,100 prefix tokens for a 400-token answer, scaling linearly with answer length.

That instability is itself the finding. Villa's speed on an agent turn is a function of how quotable the context is; gufo's is not.

## Concurrency: gufo wins decisively, on an axis villa does not use

Aggregate decode throughput summing each request's own rate, matched per-slot context, one client.

| Users | gufo aggregate | llama.cpp aggregate | Gain | gufo wall-clock | llama.cpp wall-clock | Gain |
|---:|---:|---:|---:|---:|---:|---:|
| 1 | 12.08 | 11.55 | +4.6% | 7.97 | 6.04 | +32.0% |
| 2 | 22.94 | 19.81 | +15.8% | 11.28 | 7.67 | +47.1% |
| 4 | 40.54 | 23.40 | +73.2% | 13.90 | 8.60 | +61.6% |
| 8 | **66.64** | **22.56** | **+195.4%** | 15.47 | 9.19 | +68.3% |

llama.cpp's aggregate throughput flattens completely past 4 users, at 23.40 then 22.56. gufo scales to 66.64. This is a much larger gap than gufo's own tables publish (+48.5% at 8 users), and it is measured here with autoregressive decoding on both sides.

Two configuration facts fell out of building this probe, both worth recording.

**llama.cpp divides `-c` across `-np` slots.** `-c 16384 -np 8` gives each slot 2,048 tokens, and a 3,140-token prompt is refused with `exceed_context_size_error`. gufo's `--sessions 8` gives each session the full `--context`. The table above scales llama.cpp's `-c` by the slot count so both hold the same per-request context. Villa passes no `-np` today, so this does not affect the current stack, but any future multi-slot work has to budget `ctx × slots`, not `ctx`.

**gufo caps queued requests per client IP at 4 and answers the fifth with HTTP 429.** `--max-pending-per-client` defaults to 4. Open WebUI is one client IP, so a gufo-backed chat unit would refuse a fifth concurrent request out of the box.

## What this changes

**The recommendation stands, and the agent-path question is now answered.** I recommended gufo as an opt-in fourth backend on the strength of its published numbers. The prefill win reproduces and is larger than published at villa's real context. The generation win does not exist: villa's `ngram+draft-mtp` is 2.6x faster than gufo's DFlash2 on the same model, because n-gram drafting suits villa's workloads and DFlash2 accepted only 35% of its drafts here.

So gufo is not a faster engine for villa. It is a **different tradeoff**: much faster prefill, much better concurrency, steadier generation, and less than half the peak generation speed.

That places it precisely:

- **It does not earn the chat unit.** Interactive chat is decode-bound, single-user, and short-prefix. Villa would lose on the number the user watches stream.
- **It earns the agent path above roughly 4,000 prefix tokens.** At an 8,800-token prefix gufo finishes a turn 39.6% sooner. Below that llama.cpp wins. `villa work` on a small folder sits below the line; on a large one it sits above. That is a per-task property, not a per-host one, which argues for `backend` staying a config value rather than becoming a recommendation.
- **It would earn a multi-user unit** if villa ever grows one. llama.cpp does not scale past 4 concurrent requests on this hardware, and gufo reaches 66.64 tok/s at 8.

**What I got wrong in the evaluation.** I wrote that single-user generation was "a wash, roughly +2.6% on AR" and left it there. That is true of autoregressive decoding and irrelevant, because neither engine runs autoregressive in production. Villa runs speculation, and comparing the two engines' *shipped* speculation is the comparison that matters. Gufo's benchmark tables compare its DFlash2 against llama.cpp's DFlash2, never against `ngram-mod`, so its published numbers cannot show this gap.

**What to do next, in order.** Nothing here justifies touching the chat unit. If the agent path is worth 40% on large folders, the work is a `backendGufo` case with its own `ResidencyMarkers` keyed on `event=load_completed`, a `gufo_safe` catalog flag limiting it to `qwen3.8-27b`, and `--max-pending-per-client` raised above its default of 4. The dashboard needs a typed-Unknown path for `requests_processing` and `requests_deferred`, which gufo does not emit, before any unit runs on it.

## Reproducing

The harness takes a JSON spec naming both engines' full `podman run` argument vectors and applies the identical measurement spec to each.

```sh
cd ~/.jcode/scratch/gufo-bench
python3 gufobench.py spec-varied.json          # the five-configuration table
python3 concurrency.py gufo:0 spec-varied.json 1,2,4,8
python3 concurrency.py llamacpp:0 spec-varied.json 1,2,4,8
python3 agentturn.py spec-agent.json 12000 400  # and 3000 400
```

Three measurement traps it guards, all found by hitting them:

- **A repeated prompt is not a measured prefill.** gufo's prompt cache answers an identical repeat with `prompt_n=0, cache_n=58, prompt_per_second=0.0`. Villa's bench uses one fixed prompt for reproducibility, so a naive port would have recorded gufo's pp as zero. The harness gives each run a unique nonce prefix at a fixed token budget, and voids any run that reports `prompt_n=0`.
- **An early EOS is not a comparable generation.** gufo stopped at ~43 tokens on my first question while llama.cpp ran to 128, so the tg figures covered different amounts of work. The throughput harness voids a run below `min_predicted` and asks a question long enough that both engines fill the budget. The agent-turn harness cannot do that, because the answer length is the thing under test, so it prices both engines for a fixed answer budget at their measured decode rate instead of timing them raw.
- **A repetitive prompt flatters n-gram drafting.** My first filler repeated one paragraph 40 times, which is close to the best case for `ngram-mod` and the worst possible control. The varied filler is generated from a shuffled sentence grammar.

Both engines emit llama.cpp's `timings` block with identical field names (`prompt_per_second`, `predicted_per_second`, `draft_n`, `draft_n_accepted`), verified in `src/cli/serve/generation_metrics.hpp`, so one parser reads both and neither gets a home-field advantage.

Residency was proven per ADR-0001 on every run, not assumed: `load_tensors: ROCm0 model buffer size` for llama.cpp, `event=load_completed … gpu_device_used_mib>0` for gufo. Every configuration reported PASS with zero void runs.
