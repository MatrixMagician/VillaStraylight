---
status: accepted
---

# An operator adds a model from a commit-pinned URL, and villa builds the entry

A model that is not in the embedded seed can only be used by hand-writing a
catalog entry: eighteen fields, four of them read off a GGUF header, one a sha256 of
a file that has to be downloaded first, all in a JSON file `catalog_path` points
at. The entry for `qwen3.6-35b-a3b-heretic` on the dev host was written that way
(#407). The fields are mechanical to get right and expensive to get wrong, because
the four geometry numbers are the whole KV term of the fit (ADR-0007).

The work is a verb, `villa catalog add <url>`, that pulls the file through the
existing model-pull path, hashes it, reads its header with `internal/gguf`, and
writes the entry into an external catalog. The maintainer decided to build it and
fixed the shape: the input is a commit-pinned URL, villa pulls the file, the entry
starts with speculation unqualified and no projector, and `model list`, `status`
and `doctor` mark the model as operator-supplied. This ADR settles the questions
the decision left open. Each option set lists the drafting defaults from the issue
and says whether they are confirmed.

Two facts about the code shape every answer below. First, an external catalog
**replaces** the embedded seed: `catalog.Load` returns the external file whole or,
when it is unusable, the seed whole, and never merges. A user who sets
`catalog_path` therefore owns a copy of the seed from that day, and the file has to
contain every seed entry the host still needs. Second, ADR-0007 made the catalog
the truth and the header its witness. Writing header values into an entry is
consistent with that: the operator's `add` is the act that makes them catalog
data, and CAT-01 (doctor, `model pull`) then keeps checking them against the file.

## What a source URL may be

| option | verdict |
|--------|---------|
| A. any https URL, villa notes whether it looks pinned | rejected |
| B. `https://huggingface.co/<org>/<repo>/resolve/<40 lowercase hex>/<path>.gguf` only | **chosen** |
| C. B plus other hosts and mirrors behind a flag | rejected for now |

**B is chosen.** A commit-pinned URL is the one input that makes the added entry
reproducible: the same URL names the same bytes on every host and on every later
`model pull`. The accepted shape is exactly one:

- scheme `https`, host `huggingface.co`, no userinfo, no port, no query, no fragment;
- path `/<org>/<repo>/resolve/<commit>/<file path>` where `<commit>` is 40
  lowercase hex digits, the git commit id Hugging Face resolves to that exact
  revision;
- every path segment matches `[A-Za-z0-9._+-]+` and none is `.` or `..`, so a
  percent-escape, a backslash or a separator trick has nothing to hide in;
- the last segment ends in `.gguf` and is not a numbered shard
  (`-00001-of-00003.gguf`). A split model needs one manifest entry per shard, and
  the pull path supports that, but this verb takes one file. A sharded URL is
  refused with that reason rather than half-added. Deriving the sibling shards is a
  follow-up.

Everything else is refused with a reason and, where one exists, the fix: a
`resolve/main/...` or any other ref names a moving target and the refusal says to
copy the commit id from the repository's history; a `blob/<commit>/...` URL is the
browser view and the refusal gives the `resolve` form; a `?download=true` suffix is
refused rather than stripped, because villa stores the canonical URL it was given
and does not rewrite input it did not validate.

**A is rejected** because "looks pinned" is a heuristic on an untrusted string.
**C is rejected for now** because each extra host needs its own statement of what
"pinned" means (a mirror may serve a branch under a hash-shaped name) and none has
been asked for. The accepted set is a table in one file, so a host is a row and a
test when someone needs it.

## How the file arrives and what is trusted

| option | verdict |
|--------|---------|
| A. a new download path inside `catalog add` | rejected |
| B. `internal/download` gains `Fetch`: the pull path with the checksum learned instead of given | **chosen** |
| C. require the operator to paste sha256 and size | rejected |

**B is chosen.** `download.Fetch` reuses the pull path's request shape, `.part`
resume, bare-filename confinement and atomic rename, so the only outbound request is
the same HEAD and GET `model pull` makes against the same host, and the privacy
wording does not change. The one difference is that no checksum is known in advance,
so there is nothing to verify against: villa hashes what arrives and records it.
That is trust on first use at a pinned commit, and the verb says so in its output.
When the HEAD response exposes `X-Linked-Etag` or `X-Linked-Size`, a disagreement
with the hashed file refuses and removes the `.part`, as `model pull` does. Every
later pull verifies against the recorded sha256, so the TOFU window is the one `add`.

A file already in the models dir under that name is hashed and used, not
downloaded again: weights are tens of gigabytes. A name collision with another
catalog entry's different source is refused before any download (see Merge below).
The weights stay on disk if the entry is then refused or the write fails; they are
valid, and `model pull` short-circuits on them.

**C is rejected** because the pull is the thing that produces a trustworthy sha, and
a pasted one is a second source that can disagree with the file.

## The verb

`villa catalog add <url> [--id ID] [--quant QUANT] [--name TEXT] [--ctx N]`

| flag | default | confirmed? |
|------|---------|-----------|
| `--id` | the file stem, lowercased, anything outside `[a-z0-9._-]` becomes `-` | confirmed (the issue's `--id`) |
| `--quant` | parsed from the file stem when it ends in a known quant token (`Q6_K`, `UD-Q4_K_XL`, `IQ4_XS`, `Q8_0`, `BF16`, `F16`); otherwise the flag is **required** | confirmed; required when not derivable, per the brief |
| `--name` | the file stem | added: `display_name` is free text, not derivable, and a default is not a guess about the model |
| `--ctx` | see the field table | confirmed |

No flag exists for `tier_gb`, `min_envelope_bytes`, `agent_ctx` or
`backend_default`: each is left at its absence value (table below), and an operator
who knows better edits the file, as they did before this verb. A flag is a promise
that villa will validate the value, and none of those four can be validated.

The verb does the multi-gigabyte pull **without** the stack lock and takes the lock
only for the read-extend-write of the catalog file and the config write (ADR-0010).
Holding it across a pull would block every swap on this host for the length of a
download, and nothing in the pull touches the stack.

## Which file, and what happens to `catalog_path`

| option | verdict |
|--------|---------|
| A. always `catalog-local.json`, set `catalog_path` to it | rejected |
| B. extend the file `catalog_path` names; else create `$XDG_CONFIG_HOME/villa/catalog-local.json` and set `catalog_path` | **chosen** (the issue's default, confirmed) |
| C. keep a second, additions-only file beside the seed | rejected |

**B is chosen.** A would orphan an operator's existing curated file. **C is
rejected** because it needs a second loader and a merge, and `Load` has none: the
external file replaces the seed by design and the whole stack reads one catalog.

The rules that follow from B, all fail-closed:

- `catalog_path` set and the file unusable (missing, a symlink, malformed, a schema
  other than this binary's, a failed validation) refuses. `Load` would fall back to
  the seed here with a warning, which is right for reading and wrong for writing:
  extending a file villa could not read would replace the operator's work. `catalog`
  gains `LoadFile`, the same checks as `Load` with the error returned.
- With no `catalog_path`, a new `catalog-local.json` is created as **a copy of the
  embedded seed plus the new entry**. Setting `catalog_path` to a file holding only
  the new entry would drop every seed model, including the configured one, and the
  next stack apply would fail closed.
- `catalog_path` is set only when it was empty, and only after the file is written.
  A config that names a file that does not exist is worse than a file nothing names.
- Everything else in the file is preserved. `add` changes one entry and never
  reorders, reformats the meaning of, or removes another; it writes through
  `pathsafe.WriteFileAtomic` at 0600, rooted at the file's own directory, and
  refuses a symlinked target as `Load` does.
- The merged catalog is validated with the same checks `Load` runs before it is
  written, so `add` cannot produce a file `Load` would reject.

## Re-adding

| situation | result |
|-----------|--------|
| the id is absent | added |
| the id is present with the same first-shard URL | `already present`, exit 0, nothing downloaded or written; the operator's edits to the entry are kept |
| the id is present with a different URL or a different sha256 | refused, nothing written |
| another entry already uses the same shard filename with a different URL | refused: the models dir is flat, two sources cannot share a name |

The id and the URL checks run before the download, so a refusal costs no bandwidth.
After the download the sha256 is compared once more inside the lock.

## When the seed gains entries

| option | verdict |
|--------|---------|
| A. every `add` re-merges the seed entries the file lacks | rejected |
| B. `add` seeds only a new file; `doctor` WARNs when the file lacks a seed entry | **chosen** |
| C. B plus a `villa catalog sync` verb | deferred |

An external catalog replaces the seed, so after a villa upgrade that adds seed
entries, a user with `catalog_path` set keeps their old list and does not see the
new models in `recommend`, `model list` or `swap`. Nothing says so today.

**A is rejected** because it resurrects entries an operator removed on purpose: a
curated subset is a legitimate file, and `add` was asked to add one model, not to
reconcile. **B is chosen**: a new check, CAT-03, WARNs when the loaded external
catalog lacks seed ids and names them, with the remediation of merging them from
the release's `seed.json` or clearing `catalog_path`. A WARN that a deliberate subset
can never silence would be noise, so the finding is the file lacking an entry the
seed ships, which an operator who curated on purpose reads once. **C** is the
natural follow-up and is not built: it is a second verb with its own conflict
rules, and the WARN stands without it.

An entry whose id is in the seed but whose content differs (a stale copy after a
re-pin, or a hand edit) is reported as `operator-edited` by the marking below, not
as drift.

## Is the catalog file a backup entry (ADR-0020)?

| option | verdict |
|--------|---------|
| A. no: the verb regenerates it | rejected |
| B. yes, for the managed file `catalog-local.json` only | **chosen** (the issue's default, narrowed) |
| C. yes, whatever file `catalog_path` names | rejected |

Losing the file leaves `config.toml` naming an operator model that is no longer in
any catalog, and every stack apply then fails closed (`liveModelFile`). **A is
rejected**: the file is regenerable only if the operator kept the URL, and the
config does not record it. **B is chosen** as a file row at the end of the registry,
source and destination both `config dir/catalog-local.json`, archived when the file
exists, restored through the same writer `crush.json` uses. The manifest schema goes
5 to 6, for the reason ADR-0020 gives for the eval row: a v5 villa verifies every
member and then silently drops one it does not know, so it must fail closed on a v6
archive.

**C is rejected** because restore would have to write to a path that the restored
`config.toml` names, which it has not yet read when destinations are fixed, and a
hand-maintained file elsewhere on disk is the operator's to back up. The cost is
stated: an `add` that extends a file `catalog_path` already names is not in the
archive. The verb says so in its output when it extends a file outside the config
dir.

## Where each field's value comes from

No value is guessed. A field with no formula in code or docs is either read from the
file, set to its documented absence value, or required of the operator.

| field | value | source |
|-------|-------|--------|
| `id` | file stem slug, or `--id` | the URL's file name; the operator |
| `display_name` | file stem, or `--name` | the URL's file name; the operator |
| `quant` | parsed, or `--quant` | the URL's file name; the operator. Required when the name has no quant token |
| `weight_bytes` | the file size | the pull. The seed's convention, and what `model pull` verifies |
| `n_layers` | KV-bearing layers | `gguf.Geometry.KVLayers`, from the header (hybrid-aware, ADR-0007) |
| `n_kv_heads` | KV head count | `gguf.Geometry.HeadCountKV` |
| `head_dim` | key length | `gguf.Geometry.KeyLength` |
| `swa` | the sliding-window block | `gguf.Geometry.SWA*`, written only when the header has sliding layers, so CAT-01 compares the same eight values |
| `kv_bytes_per_elem` | 2 | villa renders no `--cache-type-k/v`, so llama-server serves an f16 cache; the seed carries 2 on every entry |
| `default_ctx` | `--ctx`, else `min(header context_length, 32768)` | the header's trained context, capped. 32768 is a villa policy constant, not a measurement: it is the lowest default of any chat model the seed qualified for real use, and a smaller value fails safe, since the fit guard refuses an entry that does not fit and the operator raises it by editing or `--ctx`. A header with no `context_length` makes `--ctx` required. A `--ctx` above the trained length refuses |
| `min_envelope_bytes` | 0 | the field's own "no floor" value (`recommend.meetsEnvelopeFloor`); it gates auto-selection only, and the entry is not auto-selectable (below) |
| `tier_gb` | 0 | read by nothing outside tests; the seed uses 0 for entries with no tier |
| `agent_ctx` | absent | tools mode then serves at `default_ctx`; set by hand if a larger agent window was measured |
| `backend_default` | `""` | an empty value defers to `recommend`'s default (`cmp.Or`), so the entry follows ADR-0022 instead of freezing a copy of it |
| `bootstrap` | false | not a first-chat model |
| `unified_memory_safe` | false | see below |
| `role` | absent | a chat entry |
| `shards` | one: canonical URL, bare file name, computed sha256, size | the source and the pull |
| `ngram_safe`, `ngram_provenance` | absent | speculation is licensed only by a measurement (ADR-0006); `model swap` already runs an unqualified entry with speculation off |
| `projector`, `draft` | absent | out of scope; a vision projector needs its own file and an on-hardware exercise |

The `default_ctx` row is the one the maintainer may want to change: the alternative
is to make `--ctx` required for every add, trading a derived number for a flag.

## Auto-selection: `unified_memory_safe` is false

`recommend` picks the heaviest entry that fits, among the entries with
`unified_memory_safe` true. An added entry carries no hardware qualification, and
if the flag were true, the next `villa recommend --save` or guided install on a
large host could choose an unmeasured file over every seed model the project
vetted. So the entry is written with the flag false: it is never auto-selected, and
the operator chooses it with `villa model swap <id>`, which does not read the flag.
Setting it true by hand is the operator's licence to auto-select, as editing in
`ngram_safe` is for speculation.

The flag's documented meaning is "known to misbehave", so the `--model` override
note is reworded for an entry that is not from the seed: it says the entry is
operator-supplied and not auto-selected, instead of claiming a known defect.

## How "operator-supplied" is represented

| option | verdict |
|--------|---------|
| A. a new field on the entry (`operator_supplied`, or a `source` block) | rejected |
| B. derived: an entry's origin is its relation to the embedded seed | **chosen** |
| C. a sidecar file listing the added ids | rejected |

**B is chosen.** `catalog.OriginOf(entry)` returns `seed` when the entry equals the
embedded seed's entry of that id, `operator-edited` when the id is in the seed and
the content differs, and `operator-supplied` when the id is not in the seed. It
needs no schema change, marks the hand-written `qwen3.6-35b-a3b-heretic` entry and
every future one, and cannot be dropped by editing the file, which a stored flag
can.

**A is rejected** for three reasons. The catalog decoder uses
`DisallowUnknownFields` and `SupportedSchema` is an exact match, so any new key is
a schema bump; every external catalog on every host (the dev host's included) would
then fall back to the seed with a warning until it was rewritten, which silently
changes the running stack. A stored flag is also a claim an editor can leave behind
or omit. And the provenance an operator needs is already in the entry: the first
shard's URL carries the repository and the commit. **C is rejected** because it is a
second file that can disagree with the first.

The cost of B is that the label tracks the binary: after a villa release that
re-pins a seed entry, a stale copy of it in an external file reads
`operator-edited`. That is true to the operator (the file differs from what this
release vetted) and is the prompt to refresh it.

The marking is shown in four places, each append-only:

- `villa model list`: an origin word after the quant, and an `origin` key in
  `--json` that is omitted for a seed entry (the output is a bare array with no
  schema field to bump, so omission is what keeps every existing consumer and the
  seed listing unchanged);
- `villa status`: `model_origin` in the report, omitted for a seed model, with
  `schema_version` 13 to 14 (the report's own rule: any additive change bumps it;
  the tool server's rows took 13 first);
  the dashboard serves the same document;
- `villa doctor`: CAT-02 for the configured model, PASS with the origin and what is
  not vetted about it; CAT-03 the seed-drift WARN above; doctor's own `schema_version`
  15 to 16 (the tool server took 14 and 15 first), and both findings bind only for an
  external catalog. CAT-01's remediation, which
  tells the reader to fix `internal/catalog/seed.json`, names the operator's own file
  for an entry that is not from the seed.

## Consequences

- Adding a model is one command and one pull. The recorded entry is as good as its
  header and as honest as its absences, and CAT-01 re-checks it from then on.
- `model swap` to an added model is unchanged: it resolves through `catalog_path`
  (#369), pulls (a no-op, the file is on disk), fits and proves as for any entry.
- `catalog_path` becomes a file villa writes, not only one it reads. A host with a
  hand-managed catalog keeps working; `add` rewrites that file's formatting (key
  order and indentation follow the struct, comments cannot exist in JSON) and keeps
  every entry's content.
- The default ctx cap, the exact-match schema and the single-file source are
  deliberate limits, and each is one constant or one table row to move.
- Sharded sources, other hosts, a `catalog sync` verb, and qualifying an added
  entry for speculation or vision are out of scope and unblocked by this shape.

Accepted on 2026-11-14 by operator approval.
