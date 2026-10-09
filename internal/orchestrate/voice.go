package orchestrate

// voice.go holds the voice subsystem's managed-service constants and view builders
// (ADR-0030): villa-stt runs whisper-server on the GPU and villa-tts runs
// Kokoro-FastAPI on the CPU. Like memory.go and searxng.go, neither is an inference
// Backend; each is a digest-pinned OSS service with its own template and view.
//
// villa-stt needs the GPU, so its device passthrough is READ from the Vulkan
// inference backend through the same parser the inference unit uses, and only the
// AddDevice values are kept: measured on the host, whisper-server transcribes on
// Vulkan with the device alone, and a service that accepts uploads gets the least
// privilege that works. No device literal is typed in this file or in the two
// templates, and neither image literal matches the seam gate's image regex.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/voice"
)

// whisperImage is ggml-org's published whisper.cpp Vulkan build on the rolling
// main-vulkan channel. ggml-org publishes no ROCm tag, so speech-to-text is Vulkan
// whatever the chat backend is.
const whisperImage = "ghcr.io/ggml-org/whisper.cpp:main-vulkan@sha256:8bbf6a98c256b67fed7664fc379755d724560bcab7714906f449b97b7bc88955"

// kokoroImage is Kokoro-FastAPI's CPU release 0.9.0. The image bakes the weights in:
// it synthesized with no network at all.
const kokoroImage = "ghcr.io/remsky/kokoro-fastapi-cpu:v0.9.0@sha256:7f9a256919eff7a84e9a77b757de0f3fe95c48fcf4fbe1e008deacbc7d9f2985"

// WhisperImage returns the vetted villa-stt pin, so internal/pins records it without
// re-typing the literal.
func WhisperImage() string { return whisperImage }

// KokoroImage returns the vetted villa-tts pin.
func KokoroImage() string { return kokoroImage }

const (
	sttContainerUnitName = "villa-stt.container"
	ttsContainerUnitName = "villa-tts.container"
)

// STTContainerUnitName returns the villa-stt unit filename, so install can gate the
// service start on the unit being in the written plan.
func STTContainerUnitName() string { return sttContainerUnitName }

// TTSContainerUnitName returns the villa-tts unit filename.
func TTSContainerUnitName() string { return ttsContainerUnitName }

// whisperModelFilename is the one model file villa-stt serves; install pre-stages
// the shard under this same name through WhisperModelFilename.
const whisperModelFilename = "ggml-large-v3-turbo.bin"

// WhisperModelFilename returns the model file villa-stt serves.
func WhisperModelFilename() string { return whisperModelFilename }

// sttEntrypoint replaces the image's `bash -c` ENTRYPOINT, which would swallow every
// argument after the first. Nothing passes through a shell.
const sttEntrypoint = "/usr/local/bin/whisper-server"

// The Open WebUI audio identities. The proof's TTS leg reads the same model and
// voice through the accessors, so a voice missing from the image fails the proof
// rather than passing it while read-aloud breaks.
const (
	sttModelName = "whisper-1" // whisper-server ignores it; Open WebUI requires a value
	ttsModelName = "kokoro"
	ttsVoiceName = "af_heart"
)

// TTSModel returns the model name villa-tts is asked for.
func TTSModel() string { return ttsModelName }

// TTSVoice returns the voice villa-tts is asked for.
func TTSVoice() string { return ttsVoiceName }

// ttsEnv is villa-tts's fixed environment. DOWNLOAD_MODEL=false stops the entrypoint
// re-running the model download; the two hub keys keep the hub client offline and
// silent.
var ttsEnv = []envPair{
	{Key: "DOWNLOAD_MODEL", Value: "false"},
	{Key: "HF_HUB_OFFLINE", Value: "1"},
	{Key: "HF_HUB_DISABLE_TELEMETRY", Value: "1"},
}

// sttView is the data stt.container.tmpl renders.
type sttView struct {
	ContainerName string
	Image         string
	Network       string
	AddDevice     []string
	Volume        string
	Entrypoint    string
	Exec          string
}

// ttsView is the data tts.container.tmpl renders.
type ttsView struct {
	ContainerName string
	Image         string
	Network       string
	Env           []envPair
}

// vulkanDeviceSet returns the Vulkan backend's AddDevice values. The RunSpec only
// has to satisfy parseContainerArgs' all-fields-present check; everything else it
// maps is discarded.
func vulkanDeviceSet() ([]string, error) {
	b, err := inference.BackendFor("vulkan")
	if err != nil {
		return nil, fmt.Errorf("orchestrate: villa-stt device set: %w", err)
	}
	cv, err := parseContainerArgs(b.Image(), b.ContainerArgs(inference.RunSpec{
		ContainerName: voice.STT.Host,
		ModelFile:     whisperModelFilename,
		ModelsDir:     "/x",
	}))
	if err != nil {
		return nil, fmt.Errorf("orchestrate: villa-stt device set: %w", err)
	}
	return cv.AddDevice, nil
}

// buildSttView assembles the villa-stt view from the resolved pin. It returns an
// error, unlike the other managed views, because the device set is parsed.
func buildSttView(image string) (sttView, error) {
	devices, err := vulkanDeviceSet()
	if err != nil {
		return sttView{}, err
	}
	return sttView{
		ContainerName: voice.STT.Host,
		Image:         image,
		Network:       networkAttach,
		AddDevice:     devices,
		Volume:        embedModelMount,
		Entrypoint:    sttEntrypoint,
		Exec:          buildSttExec(whisperModelFilename),
	}, nil
}

// buildSttExec assembles whisper-server's arguments from fixed tokens. The port and
// route are voice.STT's, so the unit serves what Open WebUI and the proof call.
// --convert lets the image's ffmpeg turn the browser's webm into the WAV the model
// needs.
func buildSttExec(modelFilename string) string {
	return strings.Join([]string{
		"-m", "/models/" + modelFilename,
		"--host", "0.0.0.0", // container-internal only; no host bind
		"--port", strconv.Itoa(voice.STT.Port),
		"--inference-path", voice.STT.Route,
		"--convert",
	}, " ")
}

// buildTtsView assembles the villa-tts view from the resolved pin.
func buildTtsView(image string) ttsView {
	return ttsView{ContainerName: voice.TTS.Host, Image: image, Network: networkAttach, Env: ttsEnv}
}
