---
status: accepted
---

# The reranker is a memory service with a measured footprint

Open WebUI's knowledge retrieval ranks chunks by embedding similarity alone. A
chunk that answers the question in different words than the question uses ranks
below chunks that share its vocabulary, and only the top few reach the model. On
the dev host, nomic-embed-text-v1.5 ranked "The first cars crossed on 28 May 1937"
fourth of five for "When did the bridge open to traffic?", behind three passages
about the bridge's construction, tolls and walkway. llama-server serves a
cross-encoder reranker from the image villa already pins for the embedder, and
Open WebUI can send its candidate chunks to an external reranker and sort by the
scores that come back (#308). ADR-0027 made adding a service's footprint one row in
the reservation registry and left two calls to the first service that needed them:
where the footprint comes from, and what gates the service.

## Decision

- **The reranker is part of the memory subsystem, behind a derived gate.**
  `subsystem.RerankOn(cfg)` is `MemoryOn(cfg) && cfg.Reranker`, the `ToolsOn` shape,
  not a new `Kind`. It shares the embedder's image, its update restart set and its
  proof, so it moves with memory. The flag exists because of how Open WebUI fails:
  when the external reranker answers nothing, `RerankCompressor` returns no
  documents at all, so a memory-on host upgraded to this build must not re-render
  Open WebUI against a `villa-rerank` that is not staged. `villa install` stages the
  weights, then writes `reranker = true` beside `memory_enabled`; until it runs, a
  memory-on config renders the stack it always had. The key is omitted when memory
  is off, like `embedding_model`.

- **The footprint is a measured constant in `internal/memory`, not a catalog
  entry.** The reranker is not a model the operator chooses, and the catalog's fit
  dimensions describe chat models sized at a context. `memory.RerankFootprintBytes`
  is 2 GiB, from a measurement on the dev host on 2026-10-08: bge-reranker-v2-m3
  Q8_0 served by the pinned embedder image at `--rerank -c 8192 -b 1024 -ub 1024`
  reached a high-water mark of 1.84 GB resident (1.17 GB anonymous for the batch
  buffers plus the mapped 606 MB of weights) after a six-document request and a
  1009-token pair, and held there across a second run. This is the precedent for
  the services that follow (speech, images, extraction): a service villa pins
  carries a constant its own package owns, measured on hardware with the flags the
  unit renders, and the ADR that adds it records the measurement. A catalog entry
  with a GGUF witness (ADR-0007) is for a model the operator picks.

- **The unit runs the embedder's pin.** `villa-rerank.container` resolves its image
  under `ComponentEmbedder`, so there is no new pins component and the signed
  manifest is unchanged. It is the embedder's shape: no device passthrough, no host
  port, the models volume read-only, serving `bge-reranker-v2-m3-Q8_0.gguf`
  (635,676,416 bytes, from gpustack's GGUF repository), which install stages like the
  nomic shard.

- **The pair bound is 1024 tokens.** A rank-pooled model scores a query and a chunk
  as one sequence, which must fit the physical batch, and llama-server refuses a
  longer pair with a 500 that Open WebUI turns into an empty retrieval. The
  embedder's own default batch already bounds every chunk to 512 tokens, so a pair
  never exceeds 1024. At the default 512 the unit held 1.21 GB and refused pairs
  past 512; at 2048 it held 2.85 GB to cover chunks the embedder cannot produce.

- **Open WebUI gets hybrid search and the external engine, from the same gate.**
  With the reranker on, its unit gains `ENABLE_RAG_HYBRID_SEARCH=True`,
  `RAG_RERANKING_ENGINE=external`, `RAG_RERANKING_MODEL`,
  `RAG_EXTERNAL_RERANKER_URL=http://villa-rerank:8080/v1/rerank` and the no-auth key
  sentinel, after the memory block. The candidate pool and the reranked count keep
  Open WebUI's defaults (three each): raising `RAG_TOP_K` would change the
  web-search reservation's input and is a separate decision.

- **`villa eval` gains a `rerank` grader kind.** A case carries the query as its
  prompt, a list of documents and the index that must score highest; it is conducted
  through a reranker seam rather than a chat completion, and skipped with "reranker
  off" when the gate is off, the way a tool case is skipped without tools mode. The
  two shipped cases were chosen by measurement: on the dev host the embedder ranks
  their answers fourth and second, and the reranker ranks both first. The case
  grades the reranker only, because asserting the embedder's failure inside it would
  fail for an embedder change the reranker had nothing to do with.

- **A subsystem's unit list is answered from the config.** The reranker is the
  first optional unit inside a subsystem, and two consumers of memory's unit list
  each grew their own on-disk check before a third (the stopped window's stop and
  start) broke every memory update on a host with the key unset. A census found
  six consumers: update's capture, mutate, restore, stop and start, doctor's
  inference unit, and install's service names; status and doctor's drift already
  select by the rendered units. So `subsystem.Units` takes the config and returns
  the units the host renders (the reranker only when `RerankOn`), `EveryUnit` is
  the declaration for callers that name services rather than act on a host, and
  update loads the config once and hands it to every seam. No presence check
  remains.

- **Install sizes its pick against the config it will persist.** The gates install
  turns on (memory's reranker, a `--web-search` opt-in) reserve before the fit,
  through `Gates.Persist` and `PlannedReservations`, so the install that first
  enables them does not pick a model and a ctx for an envelope its own services
  then shrink. Before this, a first `install` with memory on fitted without the
  embedding row too.

## Consequences

- A memory-on host that re-runs `villa install` gets the reranker: a 606 MB pull,
  a 2 GiB reservation off the chat-model fit, and one more unit, proved by the
  install readiness probe. Until then nothing changes.
- `reranker = false` by hand turns the gate off and the next stack apply removes
  the env from Open WebUI; the running `villa-rerank` unit stays until stopped,
  which is the shape every optional unit has (nothing removes a unit the config
  stopped rendering). The next `villa install` turns it back on. An explicit
  opt-out flag on install is the operator's call.
- The eval suite moves to version 2, which orphans baselines recorded under
  version 1 until `villa eval --record` runs again (ADR-0018).
- Status and doctor report the unit's health as one more row and one more
  finding; their schemas go to 11 and 9.
