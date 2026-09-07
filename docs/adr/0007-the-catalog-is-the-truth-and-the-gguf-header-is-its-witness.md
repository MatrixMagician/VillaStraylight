---
status: accepted
---

# The catalog is the truth and the GGUF header is its witness

`internal/catalog/seed.json` hand-carries `n_layers`, `n_kv_heads`, `head_dim` and
`kv_bytes_per_elem` for every entry. Those four numbers are the whole KV term of
the fit inequality villa refuses or accepts a context length on. Nothing read them
back off the weights, so a typo or a re-quant published under the same name
produced a confident, wrong fit that no gate could see.

The file already carries the same facts. A GGUF header is a magic, a version, a
tensor count and a metadata section of key-value pairs, and the keys villa needs
are `general.architecture`, `<arch>.block_count`,
`<arch>.attention.head_count_kv` and `<arch>.attention.key_length`. Reading them
costs a few kilobytes off the front of a 20 GB file.

The first entry checked was already wrong. `qwen3.6-35b-a3b` carried 48 / 4 / 128
against a header that says 10 / 2 / 256, which overstated its KV term by about
4.8x and refused contexts this host can serve.

## What the header is allowed to decide

| option | what it means | verdict |
|--------|---------------|---------|
| A. parse the GGUF and fit from it | the file is the source of truth; the catalog becomes a download manifest | rejected |
| B. catalog only | keep hand-carried numbers, add nothing | rejected |
| C. catalog is truth, header is witness | the catalog still drives the fit; a disagreement is a finding | **chosen** |

**A is rejected** because it makes an unvetted file able to move the fit. Every
number in the catalog was vetted against a measurement on this hardware; a header
is whatever the publisher wrote. Parsing it into the fit means a re-upload
silently changes what villa will run, with no review and no record, which is the
opposite of what the pin discipline exists for.

**B is rejected** because it is the state that produced the bug. A hand-carried
number nothing verifies is a number that drifts, and a 4.8x error in the KV term
is invisible in a fit that reports "fits: true" either way.

**C is chosen.** The vetted catalog still drives every decision. The header is
read only to answer one question, "does this entry still describe this file?", and
a disagreement is reported to the operator rather than resolved by villa. Nothing
is auto-corrected: villa cannot know which of the two is wrong, and guessing would
turn a visible fault into a silent one.

This is the same shape as a pin. The vetted value is compiled in and the observed
value is read at runtime, they are compared, and the disagreement is the product.

## Where the check lives

| option | verdict |
|--------|---------|
| a preflight check that `villa doctor` composes | **chosen** |
| a new `villa verify catalog` verb | rejected |
| inside `catalog.Load` | rejected |

**A preflight check composed by doctor** is chosen because the finding already has
a shape here. `preflight.CheckResult` carries the tier, the status, a detail, a
remediation and a provenance; `doctor.findingFromCheck` folds one into a
`Finding` and ranks it worst-wins with everything else. `RunCatalogGeometry`
therefore adds a check and no new vocabulary, and the BLOCK/WARN/FAIL semantics it
inherits are exactly the ones this question needs: a mismatch is a confident FAIL,
a header villa could not parse is an unevaluable WARN, and a model that is not on
this host is not a finding at all.

**A `villa verify catalog` verb** is rejected on what the verify family means
here. Every `villa verify` proves something about a RUNNING stack by driving it,
with a negative control that has to be able to fail. This is a fact about two
files on disk, provable with the stack down, and putting it under `verify` would
blur a distinction the privacy claims lean on.

**Inside `catalog.Load`** is rejected twice over. It would put file I/O into a
pure loader that today decodes an embedded blob and never touches the filesystem,
and it would make one bad entry refuse the whole catalog, because `Load` is
all-or-nothing by design. A typo in one seed entry would leave villa with no
catalog at all.

## The pull-time refusal keeps the file

`villa model pull` runs the same comparison after the shard loop, so a bad entry
is caught at acquisition rather than at the next `doctor`. It runs after the
already-on-disk short-circuit too, so a rerun refuses again instead of reporting a
clean pull the second time.

The refusal does **not** delete the file. Its SHA-256 matched the catalog pin, so
the bytes are exactly what was asked for; the disagreement is between two pieces
of villa's own metadata. Deleting a 20 GB download because villa's catalog is
wrong would punish the operator for villa's fault, and the re-pull would fetch the
identical bytes and refuse identically.

## The hybrid trap

`Geometry` counts KV-BEARING layers, not blocks. Two of the entries on the dev
host are hybrid architectures where most blocks hold a fixed-size recurrent
(Gated-DeltaNet / SSM) state and only every `<arch>.full_attention_interval`-th
block holds a growing KV cache. `qwen35moe` reports 40 blocks and serves 10
attention layers; `qwen3next` reports 48 and serves 12.

On a dense architecture the key is absent and the two counts are equal, which is
precisely why the field is named `KVLayers` and not `BlockCount`. A witness that
compared block counts would have reported a mismatch on every hybrid entry and a
pass on the dense one, and the fix would have been to "correct" the catalog in the
wrong direction.
