---
status: accepted
---

# Voice is one subsystem with two units

Open WebUI's voice features are speech-to-text for voice input and text-to-speech
for read-aloud, and its call mode needs both. Villa had neither. The roadmap (#306)
filed them as two issues, #310 for whisper.cpp and #311 for Kokoro, and #311 asked
whether they are one subsystem or two.

## Decision

- **One subsystem, `subsystem.Voice`, gated by `voice_enabled`, rendering two
  units.** `villa-stt` runs `whisper-server` from
  `ghcr.io/ggml-org/whisper.cpp:main-vulkan` on the GPU, and `villa-tts` runs
  Kokoro-FastAPI's CPU image. The precedent is the existing pairs: memory is Qdrant
  plus the embedder, web search is SearXNG plus the web guard. A subsystem is the
  proof unit (`CONTEXT.md`), and one proof covers both halves here: a sentence is
  synthesized by `villa-tts`, the audio is transcribed by `villa-stt`, and the words
  that come back are compared with the words that went in. Two subsystems would have
  needed two proofs that each prove less, and two of every surface (install flag,
  status section, doctor fold, pin subsystem, update target, verify verb).
- **No translating shim.** `whisper-server` already serves the OpenAI route by
  configuration: `--inference-path /v1/audio/transcriptions` moves its one inference
  handler to that path, the handler reads the multipart `file` field that Open WebUI
  sends, and its JSON reply is `{"text": ...}`, which is the field Open WebUI reads.
  `--convert` lets the image's ffmpeg turn the browser's webm into the WAV the model
  needs. A shim shaped like `villa-inferproxy` would have forwarded bytes unchanged.
- **The STT image is the published Vulkan build, pinned by digest, rolling shape.**
  ggml-org publishes no ROCm tag (its docker workflow matrix is main, musa, intel,
  cuda, vulkan), lemonade-sdk publishes release tarballs and no image, and kyuz0's
  toolboxes carry llama.cpp only. The two backends were measured on this host; the
  numbers are in the section below. The image's `ENTRYPOINT` is `bash -c`, which
  would swallow every argument after the first, so the unit sets `Entrypoint=` to
  the server binary and `Exec=` to its arguments; nothing passes through a shell.
- **The TTS image is Kokoro-FastAPI's CPU release, pinned by digest, version-tag
  shape.** Its entrypoint re-runs the model download unless `DOWNLOAD_MODEL=false`,
  so the unit sets that and `HF_HUB_OFFLINE=1`. The image bakes the weights in: it
  started and synthesized with `--network none`, and on a bridged network an nft
  counter on every packet leaving the subnet stayed at zero through startup and
  synthesis. The ROCm Kokoro image is marked experimental upstream and PyTorch on
  gfx1151 needs per-model MIOpen setup; an 82M-parameter model does not need the GPU.
- **The whisper model is pre-staged like the embedder's.** `ggml-large-v3-turbo.bin`
  (1,624,555,275 bytes, sha256 `1fc70f77…`, the Hugging Face LFS oid) is a
  `catalog.Shard` beside `install.NomicEmbedShard`, pulled by `villa install --voice`
  into the models dir and served read-only from `villa-models`. It is a ggml `.bin`,
  not a GGUF, so ADR-0007's header witness does not apply to it.
- **The two reservation rows are pinned constants in `internal/voice`**, the
  embedder's precedent, because ADR-0027 left the footprint source to #308 and that
  has not landed. They are sized from this host's measurements, rounded up.
- **The STT unit takes its device line from the inference seam, and only that.** The
  `AddDevice=` value is read out of `inference.BackendFor("vulkan")`'s `ContainerArgs`
  through the same parser the inference unit uses, so no device literal appears
  outside `internal/inference` and the grep gate stays green. The unit carries no
  `GroupAdd=` and no `seccomp=unconfined`: measured on the host, whisper-server loads
  on Vulkan0 and transcribes with the device alone, and a service that accepts
  uploads and runs ffmpeg gets the least privilege that works. STT is always Vulkan
  regardless of the chat backend, because no ROCm whisper image exists to pin. It
  listens on 8081, not whisper's default 8080, because a gate refuses the `:8080`
  literal outside the inference client.
- **The proof's verdict is a pure core.** `internal/voice` owns the sentence, the word
  normalizer, the agreement measure (2·LCS over the two word counts, pass at 0.8), the
  bounded cold-start retry and the three verdicts; the command tier owns only the two
  curl legs. Install, `verify voice` and `update voice` call the same function.
- **Status shows two service rows and nothing else.** No `voice` section, no schema
  bump, no persisted `verify voice` record. The web-search section exists because a
  security property can only be shown from a recorded proof with a freshness window;
  the memory section shows an identity the rows cannot. Voice has neither: a
  round-trip result is a readiness claim that goes stale when a unit restarts, so
  recording it would show a value fresher than it is. Doctor folds the two health
  rows and the unit drift it already reads.
- **Not in the manifest yet.** The two components join `pins.Table` and so the
  allowlist, but the signed manifest (serial 2) does not name them; a re-sign is an
  offline step (`docs/RELEASING.md`), as it was for `backend-rocm-10.0` (ADR-0022).

## Measurements

On the dev host (gfx1151, 32 cores), large-v3-turbo, one 58.6 s clip, five interleaved
timed runs per side after a warm-up, every transcript identical to the 174-word
reference: Vulkan (lemonade v1.8.4 host build) median 28.6x realtime, range 27.9 to
35.3; the shipped ggml-org Vulkan image 28.4x, range 27.8 to 32.7; ROCm (lemonade
v1.8.4 gfx1151 build, bundled ROCm 7.12 runtime) 19.6x, range 18.0 to 22.6. The gap
exceeds the spread. The server used under half a core during a run, so the GPU is the
limiter; the host was carrying another owner's probe traffic, so the absolute numbers
are lower bounds. The model holds 1.80 GiB of GTT after a request plus 0.2 GiB of
process memory, so the STT row reserves 2.5 GiB. Kokoro held 1.65 GB after the
paragraph, so the TTS row reserves 2 GiB. The PR for #310 and #311 carries the method.

## Consequences

- `voice_enabled` is one more optional gate: `subsystem.All` and `Every` grow by one,
  `install --voice` turns it on and persists it, `verify voice` proves it, `update
  voice` moves its two pins together, status shows two service rows, doctor folds
  their health and their unit drift.
- `install` now sizes the fit against the config it is about to persist: the
  reservation rows are built from the loaded config with this run's opt-in flags
  applied, before the first pick, so a first `install --voice` subtracts the two voice
  rows, and a first `install --web-search` subtracts its row too (the gap the #317
  review found; it was shared by every opt-in).
- Both voice units are always rendered when the gate is on, so `subsystem.Voice.Units()`
  never names a unit that is absent from disk; the trap #317 hit with an optional unit
  inside an existing Kind does not apply here.
- Open WebUI's audio settings are rendered as environment, under the existing
  `ENABLE_PERSISTENT_CONFIG=False` rule, so the operator's chat UI reaches the two
  units by container DNS with no admin-API reconciliation.
- A text-only stack renders byte-identical units; the voice block is appended after
  the inferproxy unit and before the sandbox network.
- The operator could instead ask for two gates (dictation without read-aloud), a
  villa-built ROCm whisper image, or the larger `large-v3` model; each is a
  follow-up, not a reversal.
