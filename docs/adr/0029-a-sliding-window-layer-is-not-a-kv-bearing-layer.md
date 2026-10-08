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
  Muse Glimmer. The bounded sliding caches are a constant the headroom absorbs
  (under 1 GB on either model at their windows).
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
| a schema 5 with per-layer geometry in the catalog | rejected: it rewrites the fit for a term the headroom already covers; the scalar triple still describes what grows |
| read `key_length_swa` and size the sliding caches exactly | rejected: a second term for a bounded constant adds a field the fit would never refuse on |

## Consequences

- `internal/gguf` gains `Header.Uints` and `Header.Bools`, and
  `FixtureWithArraysForTest` for other packages' tests. CAT-01 and the pull-time
  refusal are unchanged in shape; they now PASS on both architectures.
- A future entry whose global layers are heterogeneous, or whose pattern length
  disagrees with `block_count`, degrades to the unevaluable WARN rather than a
  confident mismatch, as ADR-0007 requires.
- The Qwen hybrids keep the `full_attention_interval` rule; a header carrying both
  keys is read by the pattern.
