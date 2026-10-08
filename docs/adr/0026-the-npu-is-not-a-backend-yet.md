---
status: accepted
---

# The NPU is not a backend yet

Strix Halo carries an XDNA NPU beside the gfx1151 iGPU. The
[qwen38-strix-halo-harness](https://github.com/aic0d3r/qwen38-strix-halo-harness)
(v1.0.0, 2026-10-07) is the first public stack that puts it to work on this chip,
so it was read on 2026-10-08 to see whether villa could do the same. Unlike
ADR-0025, nothing was run: the findings below are read from the harness and from
the engine it drives, except where a line says the dev host was observed.

The harness does not drive the NPU itself. Every NPU feature it has goes through
[halogen-flash-server](https://github.com/peonist-ai/halogen-flash-server) 0.17.1,
and the harness says those features are dead on llama.cpp, where
`/v1/embeddings`, `/v1/rerank` and `/v1/moderations` answer 501. What halogen puts
on the NPU is small:

| model | job | harness measurement |
|-------|-----|---------------------|
| qwen3-embedding-0.6b | embeddings | 70 ms single query |
| qwen3-reranker-0.6b | reranking | 130 ms |
| qwen3guard-gen-0.6b | moderation | 70 to 130 ms; 42% recall, 0% false positives on 30 prompts |
| decider-0.8b | constrained choice | 118 ms; 78% on 18 binary prompts |
| qwen3.5-2b | text generation | 16.6 tok/s; dropped by the harness as slower than its sidecar |

The chat model never runs there. The harness's headline 64 tok/s decode is
halogen's GPU engine. The latencies carry no run count or spread, and the
harness's claim that decode is unchanged while the NPU works is contradicted by
halogen's own `docs/NPU.md`, which says the Flash model runs somewhat slower.

The NPU is not usable from villa as it ships, and each reason is a separate miss:

- **It is reachable only through a closed engine.** Halogen is one binary under
  Peonist's EULA. It is redistributable unmodified and makes no outbound
  connections by default, but it is not open source, and llama.cpp has no NPU path.
- **It comes bundled with a model villa does not serve.** `docs/NPU.md` runs the NPU
  models beside the Flash model, in the same process group, and neither it nor
  `docs/FLAGS.md` documents an NPU-only mode. Using the NPU would mean replacing
  `villa-llama` with halogen and its ~124 GB Qwen3.8-Flash-Next checkpoint, a
  proprietary `.hgn` format with no GGUF header for the fit witness (ADR-0007).
- **Running it beside the GPU needs root.** `docs/NPU.md` says GPU and NPU work at
  the same time can hang the machine and corrupt the NPU's results while the GPU
  fabric clock changes speed, and that this holds for any NPU program beside a GPU
  program. The fix holds `pp_dpm_fclk` at its top level through a root systemd unit.
  Villa's stack is rootless user units.
- **Fedora 44 has no XRT.** Observed on the dev host (kernel 7.2.8, linux-firmware
  20260916): `amdxdna` is loaded and `/dev/accel/accel0` exists, mode 0666, group
  `render`. XRT is not installed, and no `xrt` package is in the enabled Fedora 44
  repositories. Halogen tests Ubuntu and Arch, and mounts the host's XRT libraries
  into a Debian 13 image, so they must not need a newer glibc than 2.41.
- **The container drops isolation villa keeps.** The harness launches with
  `seccomp=unconfined`, `--ipc=host`, an unlimited memlock and `/sys` mounted.

## Decision

- **Villa does not use the NPU.** `BackendFor` gains no name, no unit is rendered
  with `/dev/accel`, and the pins table gains no component.
- **The question reopens when an open NPU server runs on its own**, beside
  llama.cpp rather than inside another inference engine, with an XRT that Fedora
  packages or the image carries. A faster NPU latency or a new halogen release
  alone does not reopen it.
- **When it reopens, the first workload to weigh is `villa-embed`**, which serves
  `nomic-embed-text-v1.5` on the CPU today (its unit passes no device). The fabric
  clock hazard is settled before any unit puts NPU work beside `villa-llama`, and a
  new unit proves its NPU offload the way ADR-0001 proves the GPU's.

## Rejected

**Adopt halogen as the inference backend to get the NPU.** That is an engine swap
with a 124 GB model outside the catalog, decided for four sub-1B helper models. It
fails ADR-0025's contract on the same grounds a GPU engine would be judged on,
before the NPU enters into it.

**Use the NPU guard model in websafe.** ADR-0002 keeps the web guard deterministic
and flagging. A model at 42% recall adds a verdict villa cannot explain, and the
harness itself ships it only as a fail-open tripwire.

**Ship an NPU detect probe now.** A `HostProfile` field with nothing that reads it
is a schema bump for a fact no decision uses.

**Defer without a record.** The harness will be asked about again. This records
why it is not enough and what would change the answer.

## Consequences

- Nothing in the tree changes.
- The fabric clock hazard is the finding that outlives halogen. Any future NPU
  workload in villa inherits it, and its fix is root-owned host preparation, the
  kind `preflight` reports and does not perform.
