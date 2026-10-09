---
status: accepted
---

# A sliding-window layer is not a KV-bearing layer

The catalog's KV term is `2 x n_layers x n_kv_heads x head_dim x ctx x bytes`, and
ADR-0007 made the GGUF header its witness. The witness counted every block as a
KV-bearing layer unless the header carried `<arch>.full_attention_interval`, the
key the Qwen hybrids use, and it refused a per-layer `head_count_kv` array as
unsupported.

The first two catalog entries from outside the Qwen family (#315) break both
assumptions. Gemma 4 31B (`gemma4`) and Muse Glimmer 30B (`muse-glimmer`)
interleave local and global attention: `<arch>.attention.sliding_window_pattern`
is a per-block bool array, `true` for a sliding-window block, and Gemma 4 also
carries `head_count_kv` as a per-block int array (16 heads of 256 on its sliding
blocks, 4 heads of 512 on its global ones). Read off the files on the dev host:

| arch | blocks | pattern | global blocks | head_count_kv | key_length |
|------|--------|---------|---------------|---------------|------------|
| gemma4 | 60 | 5 sliding, 1 global, repeating | 10 | [16 x5, 4] x10 | 512 (swa 256) |
| muse-glimmer | 52 | 3 sliding, 1 global, repeating | 13 | 2 | 128 |

llama.cpp bounds a sliding-window block's cache at the window (1024 and 2048
tokens here), so only the global blocks grow with the context. With the old rule
Gemma 4 could not be witnessed at all, which the pull path turns into a refusal
and `doctor` into a permanent CAT-01 WARN, and Muse would have been witnessed at
52 layers when 13 grow, a 4x overstatement of its KV term.

## Decision

- **A sliding-window layer is not KV-bearing.** When the header carries
  `sliding_window_pattern`, `KVLayers` is the count of its `false` entries, and the
  catalog's `n_layers` for such an entry is that count: 10 for Gemma 4 31B, 13 for
  Muse Glimmer.
- **The sliding-window cache is bounded, not free, and the fit counts it.** llama.cpp
  allocates a second cache for the sliding layers of
  `GGML_PAD(min(n_ctx, n_swa x n_seq_max + n_ubatch), 256)` cells when the KV is
  unified (`src/llama-kv-cache-iswa.cpp`). villa renders none of `-np`, `-kvu`, `-ub`
  or `--swa-full`, so llama-server's defaults apply, `n_parallel = 4` (unified) and
  `n_ubatch = 512`, and the Gemma unit's journal shows the result at ctx 131072:
  `ROCm0 KV buffer size = 10240.00 MiB` for the global layers and a second
  `ROCm0 KV buffer size = 3600.00 MiB` for the sliding ones, 4608 cells at 819,200
  bytes per cell (50 layers x 16 KV heads x 256 dims x K+V x f16). The first draft of
  this ADR called that "under 1 GB" and left it to the headroom; measured, it is 3.77
  GB, more than the slack the fit had on a 33 GB envelope at `--ctx 16384`. An entry
  therefore carries a `swa` block (`n_layers`, `n_kv_heads`, `head_dim`, `window`,
  schema 5), the header witnesses it (`SWALayers`, `SWAHeadCountKV`, `SWAKeyLength`
  from `key_length_swa`, `SWAWindow` from `sliding_window`), and `recommend` adds
  `2 x n_layers x n_kv_heads x head_dim x cells x bytes` with
  `cells = pad256(min(ctx, window x 4 + 512))` to the KV term. The two constants are
  llama-server's defaults, and an inference test keeps villa from rendering the flags
  that would change them.
- **A per-layer `head_count_kv` is read at the KV-bearing layers and must agree
  there.** Gemma 4's global blocks all carry 4, so its `n_kv_heads` is 4 and its
  `head_dim` is the scalar `key_length` of 512, which is the global blocks' value.
  A header whose KV-bearing layers disagree is an error naming the distinct
  values, and the operator sees the same unevaluable WARN as before.
- **Small integer and bool arrays are retained; nothing else changes.** The reader
  keeps integer and bool arrays of at most 4096 elements and still discards every
  larger, string, float or negative-element array behind a marker, so a vocabulary
  never lands in memory. Every other geometry key still refuses an array.

## Alternatives

| option | verdict |
|--------|---------|
| carry `n_layers` = block count and leave the witness alone | rejected: Gemma 4 is unwitnessable (a permanent WARN, and `villa model pull` refuses it), and Muse over-reserves 4x |
| full per-layer geometry in the catalog | rejected: two layer classes describe every architecture llama.cpp bounds this way, and the scalar triple plus one block says which is which |
| treat the sliding caches as headroom | rejected after measuring: 3.77 GB on Gemma 4 31B exceeds the slack the fit keeps on small envelopes, so an entry could be admitted into an OOM |
| render `-np` and `-ub` explicitly so the bound is known | rejected: it adds flags to every unit for a number the defaults already fix; the test that pins the defaults gives the same guarantee without changing a rendered unit |
| a separate JSON field for the sliding term | rejected: it is KV cache, so it folds into `kv_cache_bytes`, and the `--json` contract keeps its schema |

## Consequences

- `internal/gguf` gains `Header.Uints` and `Header.Bools`, and
  `FixtureWithArraysForTest` for other packages' tests. CAT-01 and the pull-time
  refusal are unchanged in shape; they now PASS on both architectures, and a Gemma
  entry without its `swa` block FAILs CAT-01 with the sliding values in the detail.
- The catalog schema is 5. An entry without a `swa` block reserves nothing for
  sliding layers, which is exact for every Qwen entry (their headers carry no
  `sliding_window_pattern`, and their witness values and fit terms are unchanged).
- llama-server's SWA context checkpoints (`--ctx-checkpoints`, 32 per slot, about
  0.78 MiB per token on Gemma 4 31B, host memory, outside `--cache-ram` while a slot
  is active) are not in the fit. They are measured in the vet record and tracked as #323;
  ADR-0034 caps them and counts the cap.
- A future entry whose global layers are heterogeneous, or whose pattern length
  disagrees with `block_count`, degrades to the unevaluable WARN rather than a
  confident mismatch, as ADR-0007 requires.
- The Qwen hybrids keep the `full_attention_interval` rule; a header carrying both
  keys is read by the pattern.
