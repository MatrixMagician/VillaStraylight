---
status: proposed
---

# A draft sidecar is proven as a second model, not trusted as a flag

ADR-0006 shipped `ngram` and measured a draft sidecar losing on every catalog
entry, because every entry was a mixture-of-experts model with about 3B active
parameters. It said a draft would ship when the catalog carried a dense entry.
`qwen3.8-27b` is that entry, and the 2026-09-07 probe on the dev host (pinned
rocm-7.2.4 image, llama.cpp b9536, greedy, 256 tokens) reads:

| mode | cold tg | warm tg | accepted |
|------|---------|---------|----------|
| none | 11.2 | 10.9 | n/a |
| ngram-mod | 11.1 | 21.3-44.4 | 57-80% |
| draft-simple, Qwen3.5-0.8B Q8_0 | 14.0 | 14.0 | 76% |
| draft-mtp, head embedded in the main GGUF | 18.5 | 18.8 | 69% |
| draft-mtp, separate `mtp-Qwen3.8-27B-Q4_0.gguf` | 20.3 | 20.3 | 71% |
| ngram-mod + draft-mtp file | 21.6 | 34.1-78.2 | 71-81% |

On a dense target a draft pays, and the model's own multi-token-prediction head
pays most. That closes #123 and #132 with one shape rather than two.

## The draft is a sidecar with a spec type

**A `Draft` type embeds `catalog.Sidecar`** (shards, weight bytes, provenance)
and adds what a draft needs and a projector does not: the KV fit inputs,
`spec_type`, `n_max` and `p_min`. The chosen option. The projector never carries
fields it cannot honour, and `AllShards` plus `validateSidecars` gain a second
branch rather than a second concept. `spec_type` is a fail-closed allowlist of
what was measured here, `draft-simple` and `draft-mtp`; a value outside it
refuses the catalog.

**Widening `Sidecar` with optional fields**, rejected. A projector with a
`spec_type` key is a shape that lies about what it can do.

**An `mtp` mode of its own**, rejected. The MTP head is a file pulled and verified
with the model, rendered through `--spec-draft-model` like any draft, and proven
the same way. Only the spec type differs, and that is catalog data.

## The head ships as a separate file

Both the embedded head and the separate file were measured. The separate file is
faster (20.3 against 18.8) and it is the only form the residency proof can see: a
draft loaded from its own file emits its own `load_tensors` block, and an embedded
head emits nothing. Villa proves offload, so the form it cannot prove does not
ship.

## The draft is proven as a second model

The residency proof reads the journal as a sequence of model blocks, each opened
by `llama_model_loader: loaded meta data`. When a draft is expected the second
block must show a device buffer above zero; a block with only CPU buffers is a
FAIL, and no second block is Unknown, which WARNs. The same rule the target has
always had, applied per block. One helper serves the start-time scrape and the
running proof, so `install`, `bench`, `prove`, `doctor` and every swap read the
same answer.

The sysfs signal folds the draft's weight into the band it is checked against,
through a `DraftBytes` recommendation term beside `ProjectorBytes`. ADR-0001's
two signals hold for the draft.

## What `draft` renders

`speculation = draft` renders `--spec-type <spec_type>` with the draft file, the
backend's own residency device token, all draft layers on the device, `n_max` and
`p_min` from the catalog. When the entry is also `ngram_safe` the spec type is
`ngram-mod,<spec_type>`, because the pinned build accepts the list and the
combination won every cell. `draft` therefore means the sidecar draft, and ngram
riding along is licensed by the entry's own ngram qualification, never assumed.

## Resolution and fit

An unset `speculation` resolves draft when the entry carries one and it fits, else
ngram when qualified, else off, each with a note. An explicit `draft` that does
not fit or is not declared is a refusal, the contract `ResolveSpeculation` already
has for `ngram`. The projector is reserved before the draft, so a tight envelope
loses the speedup before it loses vision. The draft's KV is sized at the served
context, with no draft context flag.

The draft is pulled and verified with the model unconditionally, as the projector
is. `villa speculation set draft` with no draft on disk refuses and names
`villa model pull`; the swap never downloads.

## Consequences

- Catalog stays schema 4; `draft` is an optional block like `projector`.
- `Recommendation` gains `DraftBytes` and `DraftKVBytes`, append-only, schema
  bump. `villa status` shows the mode only.
- One provenance per entry, measured on ROCm; Vulkan inherits, as `ngram_safe`.
- `CONTEXT.md` defines `draft` under Speculation and drops "draft mode" from its
  avoid list; the Sidecar entry stops naming the projector as the only one.
