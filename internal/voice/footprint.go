package voice

// footprint.go holds the two reservation rows recommend subtracts from the envelope
// before the chat-model fit (ADR-0027, ADR-0029). They are constants because villa
// pins exactly one speech model and one TTS image, so the footprint is a fact about
// a pin rather than a function of config. Both were measured on the gfx1151 dev host
// and rounded up; under-reserving would let the chat model claim memory the voice
// units hold.

// sttReserveBytes is 2.5 GiB: ggml-large-v3-turbo held 1.80 GiB of GTT after a
// request plus 0.2 GiB of process memory. GTT is shared with the chat model, which is
// why it is reserved rather than ignored as GPU memory.
const sttReserveBytes uint64 = 5 << 29

// ttsReserveBytes is 2 GiB: Kokoro-FastAPI's CPU image held 1.65 GB after a
// paragraph of synthesis.
const ttsReserveBytes uint64 = 2 << 30

// STTFootprintBytes returns the villa-stt reservation.
func STTFootprintBytes() uint64 { return sttReserveBytes }

// TTSFootprintBytes returns the villa-tts reservation.
func TTSFootprintBytes() uint64 { return ttsReserveBytes }
