---
status: accepted
---

# The voice proof asks where whisper ran

ADR-0030's proof is a spoken round trip: `villa-tts` speaks a sentence, `villa-stt`
transcribes it, and the words are compared. That proves the pair answers, not where
whisper ran. With its Vulkan device lost (render-node permissions, a host update),
whisper-server falls back to the CPU, hears the sentence anyway, and the proof
passed. ADR-0001 says offload is proven by two signals and a CPU fallback is a FAIL;
the chat model and the image server (ADR-0032) already follow it, voice did not
(#332).

## Decision

- **The round trip is the drive of `residency.Prove`, unchanged.** The image proof
  set the shape: only `Generate` and `Fold` differ from the chat proof. Here
  `Generate` is the whole round trip, so GPU busy is sampled while whisper
  transcribes, and `Fold` is `inference.WhisperOffloadVerdict` over `villa-stt`'s
  invocation journal. Readiness stays the round trip's own bounded retry, so
  `PollHealth` answers ready at once rather than adding a second poll loop.
  Install, `villa verify voice` and `villa update voice` still call one function.
- **The placement signal is whisper's model-load line.** whisper.cpp prints
  `whisper_model_load: <buffer> total size = N MB` once per buffer it loads. A
  `Vulkan0` buffer with N > 0 and no CPU buffer is resident on the iGPU; a CPU
  buffer is the fallback. The software-renderer reject reads the same
  `ggml_vulkan: N = <device>` line the image scrape reads, through the same
  journald-prefix-tolerant `startLogDeviceName` and the Vulkan backend's markers.
  The GTT floor (against the whisper model file's size) and the busy fold are
  `foldFloors`, shared with the chat and image verdicts.
- **The verdict maps onto the verify family's.** A round trip that did not pass is
  returned as it was. Placement PASS is a Pass. FAIL is a Fail carrying the
  remediation. WARN (no journal, no load line) is a Reject: nothing was proven
  either way, the same reading `update image` gives a WARN. A passing round trip
  the fold never judged, because the protocol's deadline came first, is a Reject.
- **`internal/voice` may import the residency protocol.** ADR-0030 kept the package
  a leaf over config and verify. It now also imports `residency`, `inference` and
  `detect`; none imports `voice`, and every host read arrives as a seam, so the
  package still does no I/O. The leaf test lists the new imports.

## Consequences

- A `villa-stt` on the CPU fails `villa verify voice` (exit 1), refuses
  `villa install --voice`, and fails the `update voice` prove step, with the
  backend's Vulkan remediation plus the unit to check.
- An unreadable placement also refuses `install --voice`, because install already
  treats a Reject as a refusal (ADR-0030). An operator could choose instead to let
  install pass with a warning, as the image install does for a WARN; that would need
  a third voice install state and was not worth it for a pinned image whose log line
  is fixed by its digest.
- The placement grammar is pinned to the vetted `main-vulkan` digest like every
  other marker. A rebuilt image that drops the line reads as a Reject on update,
  never as a pass.
