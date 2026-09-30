---
status: accepted
---

# One authenticated client reaches the inference unit

ADR-0011 gave `villa-llama` an api key. Threading it took three commits in
#248, and a read at `2fd9314` found five bearer implementations
(`internal/llm/openai.go`, `internal/metrics/llamacpp.go`'s `authedGet` behind
four `Scrape*Auth` wrappers, `cmd/villa/status.go`'s `liveProps`,
`internal/openwebui/endpoints.go`, and the inferproxy leg) and about twenty
direct reads of `config.InferenceSecret`. Six call sites re-derived the endpoint
from `inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint()`. One
path was still missed: doctor's search-residency drive posted
`/v1/chat/completions` from an in-network curl with no `Authorization` header,
so on a keyed host every round was a 401 and the proof could only ever degrade
to typed-Unknown (#252). `TestInferenceClientsCarryAPIKey` matched three struct
literals and could not see curl argv or a raw `http.Get`. This ADR records the
fix for #255.

llama.cpp leaves `/health` and `/v1/models` public under `--api-key`, and
requires the key on `/v1/chat/completions`, `/props`, `/metrics` and `/slots`.
ADR-0011's list put `/v1/models` among the keyed routes; the client sends the key
there anyway, so the correction changes no request.

## Decision

- **`inference.Client` owns the unit's address, the key and every route the
  control plane calls** (`internal/inference/client.go`): `Health` and
  `PollHealth` (keyless, so readiness reads the same with or without a
  credential), `Models`, `Props`, `Perf`, `Counters`, `CacheCounters`, `Slots`,
  `Chat` (streaming and `Complete`, over `internal/llm`) and `GenerationProbe`.
  It lives in `internal/inference` because that package already owned the host
  endpoint, the readiness poll, the generation probe and `PropsInfo`. Every
  single-shot read is bounded at 3 s and 64 KiB, and a body over the cap is
  refused whole.

- **It is built once, from a loaded config, in `cmd/villa/inference.go`**:
  `inferenceClient(cfg)` for the host-published unit and
  `inNetworkInferenceClient(cfg)` for a probe that runs on `villa.network`.
  Callers hold the value, never an endpoint string plus a key.
  `residency.Deps` binds the client's `PollHealth` and `GenerationProbe`, so
  `residency.Target` no longer names an endpoint or a key; `status.Deps.Props`
  and `GenTokensPerSec` close over it; `inference.ValidateInput` carries it.

- **In-network curl probes take a `CurlRequest` from the client.** Its `Args`
  tell curl to read headers from stdin (`-H @-`), its `Stdin` carries the
  `Authorization` line, and `runProbeCurlIn` pipes it through `podman run -i`.
  The key crosses a pipe, never podman's or curl's argv, which any local user
  can read from `/proc`.

- **`internal/metrics` is parsers only** (`ParsePerf`, `ParseCounters`,
  `ParseCacheCounters`, `ParseSlots`). The `Scrape*Auth` wrappers,
  `inference.PollHealth`, the `inference.GenerationProbe` pass-through and
  `liveProps` are deleted, and every caller is migrated.

- **`TestInferenceReachedOnlyThroughClient` replaces the struct-literal gate.**
  It parses every non-test file under `cmd/villa` and `internal/` and reports
  a read of `InferenceSecret`, a call that yields a llama-server address or
  builds a client from one, and a llama-server route literal, each outside the
  files allowed it with a written reason. A self-test feeds it #252's shape.

## Rejected

**Grow `internal/llm` into the client.** `llm` is the OpenAI wire protocol, and
`internal/inference` imports it. Moving the readiness poll and `PropsInfo` into
`llm` would invert that import for no new boundary; the chat route stays in
`llm`, reached through `Client.Chat`.

**A new `internal/inferclient` package.** It would import `internal/inference`
for `PropsInfo` and `ChatResult` and duplicate its ownership of the endpoint,
adding a package without adding a boundary.

**Pass the key to curl through a bind-mounted header file, or `--env-file` plus
curl's `--variable`.** A header file puts a second copy of the key on disk for
the life of each probe, and `--variable` needs curl 8.3 in the helper image.
Stdin needs neither.

**Keep the literal gate and add curl argv to it.** It would still miss the next
shape (a raw `http.Get`, a new struct), because it matched the key's call sites
rather than the ways to reach the unit.

## Consequences

The client does not deliver the key to processes that call llama-server
themselves: Open WebUI's connection list (`model_resident.go`,
`orchestrate/render.go`), Crush's provider config and Claude Code's environment
(`internal/agent`), and `villa-inferproxy`'s forward leg (ADR-0011), which
relays a task's requests rather than originating them. Each is an allowed entry
in the gate with its reason. So are the key's writers (`lifecycle.go`,
`install/flow.go`) and install's readiness poll, which `install.Deps` hands an
endpoint string; `/health` needs no key, and moving `install.Deps` to the
client is left to a change that owns `internal/install`.

The gate is a syntax check, as strong as `TestSeamGrepGate`: it matches
package-qualified calls by import path and fields by name. A hand-typed
`127.0.0.1:8080` URL is outside what it sees (the agents' provider configs
carry one, drift-guarded by their own test).

A reading that used to be best-effort is now refused when its body is over the
cap. `/props` used to be cut at 8 KiB and then fail to parse when a large chat
template pushed it past that, so it read as Unknown; it is now read whole up to
64 KiB.
