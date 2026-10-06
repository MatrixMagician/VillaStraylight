---
status: accepted
---

# Kyojin is not a backend yet

[Kyojin](https://github.com/Yamz-Labs/kyojin) is ExLlamaV3 with a ROCm path for
gfx1151. It serves EXL3 packs over an OpenAI-style HTTP API, so it could sit where
llama-server sits. The #296 prototype ran it on the dev host on 2026-10-05 (Kyojin
at `e5204d1`) against llama.cpp build 11430, the `rocm-10.0` image ADR-0022 made the
default. Both served Qwen3.8-Flash-Next at the same size (EXL3 89 GB, UD-IQ4_XS GGUF
88 GB) at a 64K context, vision off, thinking off, greedy. Speed is Kyojin's own
`tools/bench.py` against both servers, warm run kept. Quality is villa's eval suite
(suite version 1), the cases and graders `villa eval` runs.

| engine | speculation | pp (~2760 tok) | tg prose / chat / code | eval |
|--------|-------------|----------------|------------------------|------|
| llama.cpp b11430 | off | 462 | 21.6 / 21.2 / 21.7 | 19/20 |
| Kyojin | off | 916 | 31.5 / 31.6 / 31.5 | 19/20 |
| Kyojin | MTP + n-gram (its default) | 830 | 47.8 / 42.5 / 49.6 | 19/20 |

The engine is faster at equal quality: twice the prefill, 1.46x the decode plain and
2.0 to 2.3x with its speculation. The failing case is the same on every row
(`code-python-slice`, which the live qwen3.6 stack also fails), and Kyojin's reply
to it is byte-identical with speculation on and off.

The speed is not what decides this. Kyojin as it ships misses the contract every
villa backend meets, and each miss was observed, not read:

- **It has no image and nothing pinnable.** It does not build on Fedora 44 (GCC 16's
  `<format>` uses `[[__gnu__::__noinline__]]`, which HIP's `__noinline__` macro
  breaks); it built in `ubuntu:24.04` with GCC 13. Its runtime came from AMD's
  nightly gfx1151 index: a ROCm 7.13 alpha, a torch 2.12 alpha and a 12 GB SDK. No
  part of that has a digest a pin manifest could carry.
- **It has no auth.** A wrong bearer key gets 200. ADR-0011 makes the api key the
  guard on inference, and here only inferproxy would stand in front of it.
- **It has no `/metrics`.** It returns 404, and status, usage and bench scrape it.
  An upstream PR (Yamz-Labs/kyojin#11) adds llama.cpp-named metrics and was in
  review on 2026-10-06.
- **It serves one request at a time.** Two concurrent requests took 10.7 s against
  5.3 s for one. Open WebUI, a task and the grounding audit share one server.
- **It reads a second model format.** EXL3 has no GGUF header, so the fit witness
  (ADR-0007), the catalog's dimensions and the download would each need a second
  path.
- **It leaves little room.** 113 GB at ready of 125 GiB, against 105.7 GB for
  llama.cpp on the same model. First start took 297 s, 200 s of it kernel warm-up.

## Decision

- **Villa does not add a Kyojin backend.** `BackendFor` gains no name, the pins
  table no component, and the catalog no EXL3 entry.
- **The question reopens when Kyojin meets the contract upstream**, not when villa
  builds around it. That means an image villa can pin by digest on a released ROCm,
  an api key it refuses on, and `/metrics`. A measurement that only repeats the
  speed result does not reopen it.
- **When it reopens, the first shape to weigh is an opt-in engine for named
  packs**, beside llama.cpp and never instead of it: a catalog entry names a pack,
  and a second model format reaches only those entries. One-at-a-time serving is
  settled then, against the units that share the server.

## Rejected

**Build and pin a Kyojin image in this repo.** The Ubuntu build is the seed of one,
but villa would own a ROCm and torch alpha stack it cannot vet the way it vets
kyuz0's images, and it would rebuild that stack every time upstream moves. The
project integrates OSS containers; it does not maintain an inference build.

**Adopt it behind inferproxy plus a scrape shim.** Inferproxy forwards two routes
for task VMs, and every other caller reaches llama-server directly. Putting every caller behind a proxy, and translating a
missing `/metrics` in a shim, adds two first-party layers to cover what upstream
lacks.

**Defer without a record.** The speed result is real and will be asked about again.
This records why it is not enough and what would change the answer.

## Consequences

- Nothing in the tree changes. The render, the pins and the catalog stay as they
  are.
- The llama.cpp finding the same prototype made, that the default pin could not
  load `qwen4exp` models, was split to #297 and closed by ADR-0022.
- llama.cpp's `ngram-mod` hung once on build 11430 with Qwen3.8-Flash-Next during
  this prototype. The ADR-0022 vet re-proved every ngram-qualified catalog entry
  on that build and saw no hang; Qwen3.8-Flash-Next is not in the catalog.
- The full prototype write-up, with the run scripts and raw results it was built
  from, is the
  [issue comment](https://github.com/MatrixMagician/VillaStraylight/issues/296#issuecomment-6003477988)
  copied from commit `1a21d9b` before the branch was deleted.
