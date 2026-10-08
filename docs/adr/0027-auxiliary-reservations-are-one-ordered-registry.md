---
status: accepted
---

# Auxiliary reservations are one ordered registry

`recommend.Pick` takes memory off the envelope, before the chat-model fit, for the
services that share the host with the chat model. There were two: the embedding model
when memory is on, and the web-search injection budget when web search is on. Each
had its own input struct (`MemoryInputs`, `WebSearchInputs`), its own positional
`Pick` parameter, its own `recommend --json` key and its own line in
`finalizeRecommendation`. The cmd tier built the two structs at nine live call sites
in three spellings: through the subsystem gate, from the raw config field, and through
`liveLoadedMemoryInputs` / `liveLoadedWebSearchInputs`. Each spelling gave the same
answer, but they agreed only by convention.

The versatility roadmap (#306) adds services that each hold memory beside the chat
model: a reranker, speech-to-text, text-to-speech, image generation and document
extraction. Added the same way, each one would be another `Pick` parameter, another
key, another builder in the cmd tier and another term in install's resource floor.
The floor had already drifted. `install.ResourceFit` counted the embedding
reservation and not the web-search one (#307).

## Decision

- **One ordered registry in `internal/recommend`.** `ReservationsFor(cfg)` returns
  `[]Reservation{Name, Bytes, Notes}`, one row for each service whose gate is on, in
  a fixed order: embedding, then web search. A row carries no `subsystem.Kind`: the
  zero `Kind` is `Memory`, so a hand-built row without one would pass as the
  embedding row. The name is the row's identity. Each row's size comes from its
  own pure function, so the embedding footprint still comes only from
  `internal/memory`. The gates are read through `internal/subsystem`, so the registry
  answers "is this on?" the same way as every other caller.
- **`Pick(p, c, ov, res)`.** The envelope shrinks by the saturating sum of every row,
  and each row's notes are appended in registry order. The cmd tier passes
  `ReservationsFor(cfg)` from a config it already holds, or `liveLoadedReservations()`
  where it holds none. A config that fails to load gives no rows, as before. The two
  callers that size weights only (`proveTarget`, the status weight figure) pass `nil`
  on purpose, as they passed empty inputs before. `swapFit` builds the rows from the
  config it is asked to fit, rather than reading `config.toml` a second time.
- **The contract grows append-only, and the schema goes 8 to 9.**
  `Recommendation.Reservations` is a `reservations` array of `{name, bytes}`, always
  present (`[]` when nothing is reserved). `embedding_reservation_bytes`,
  `web_search_reservation_bytes` and `memory_considered` stay, and are now derived
  from the array. Their values don't change, so a reader of the v8 keys sees the same
  numbers.
- **Every consumer of the total reads the registry.** `Recommendation.ReservedBytes()`
  is the saturating sum Pick subtracted, and `install.ResourceFit` adds it, so the
  install floor now counts web search. The `recommend` fit table prints one
  `− <name> reservation` row for each row with bytes, with underscores shown as
  spaces, so the table shows `web search` too. The embedding row's label goes from
  `embed` to `embedding`, to match the JSON name.
- **The resident set needs nothing.** `residentset.Admit` already fits against
  `rec.UsableEnvelopeBytes`, which is the envelope after the reservations.

## Not decided here

Where a new service's footprint comes from: a pinned constant in its own package,
like the embedder's, or a catalog entry with a GGUF witness, like chat models
(ADR-0007). The registry doesn't care, and the first service that needs the answer
(#308, the reranker) decides it.

## Consequences

- A new service is one row function and one line in `ReservationsFor`. `Pick`, its
  call sites, the JSON contract and install's floor need no change.
- Install's memory floor grows by the web-search budget when web search is on. At
  the defaults, that is about 6 MiB.
- `memory_considered` is true exactly when the embedding row is present, which is
  exactly when memory is on.
