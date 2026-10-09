package orchestrate

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// voiceFixtureInput is fixtureInput with only the voice gate on.
func voiceFixtureInput() RenderInput {
	in := fixtureInput()
	in.Cfg.VoiceEnabled = true
	return in
}

// lines returns every line of text that starts with prefix, in order.
func lines(text, prefix string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

// TestRenderSTTGolden: villa-stt runs whisper-server from the pinned Vulkan image,
// serving the OpenAI transcription route on 8081 from the read-only models store.
func TestRenderSTTGolden(t *testing.T) {
	units, err := Render(voiceFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-stt.container")
	goldenCompare(t, "villa-stt.container.golden", c.Text)

	for _, want := range []string{
		"Entrypoint=/usr/local/bin/whisper-server",
		"Exec=-m /models/ggml-large-v3-turbo.bin --host 0.0.0.0 --port 8081 --inference-path /v1/audio/transcriptions --convert",
		"Volume=villa-models:/models:ro,z",
		"Network=villa.network",
	} {
		if !strings.Contains(c.Text, want) {
			t.Errorf("villa-stt unit missing %q:\n%s", want, c.Text)
		}
	}
}

// TestRenderTTSGolden: villa-tts runs Kokoro-FastAPI's own entrypoint with the model
// download off and the hub client offline; it mounts nothing and needs no device.
func TestRenderTTSGolden(t *testing.T) {
	units, err := Render(voiceFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-tts.container")
	goldenCompare(t, "villa-tts.container.golden", c.Text)

	want := []string{
		"Environment=DOWNLOAD_MODEL=false",
		"Environment=HF_HUB_OFFLINE=1",
		"Environment=HF_HUB_DISABLE_TELEMETRY=1",
	}
	if got := lines(c.Text, "Environment="); !slices.Equal(got, want) {
		t.Errorf("villa-tts env = %q, want %q", got, want)
	}
	for _, absent := range []string{"Exec=", "Volume=", "AddDevice="} {
		if strings.Contains(c.Text, absent) {
			t.Errorf("villa-tts unit carries %q; it runs the image entrypoint with no mount and no device:\n%s", absent, c.Text)
		}
	}
}

// TestRenderByteIdenticalWhenVoiceOff: a text-only stack renders exactly the units
// it rendered before voice existed.
func TestRenderByteIdenticalWhenVoiceOff(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(units) != 6 {
		t.Fatalf("voice off: Render returned %d units, want 6: %v", len(units), unitNames(units))
	}
	for _, u := range units {
		if u.Name == "villa-stt.container" || u.Name == "villa-tts.container" {
			t.Errorf("voice off: Render emitted %q", u.Name)
		}
	}
}

// TestRenderVoiceUnitsAfterInferproxyBeforeSandboxNetwork: the voice pair is
// appended after every other gated unit and before the sandbox network, so no
// existing unit moves.
func TestRenderVoiceUnitsAfterInferproxyBeforeSandboxNetwork(t *testing.T) {
	in := statefulFixtureInput()
	in.Cfg.WorkspaceAgent = true
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := []string{
		"villa-llama.container",
		"villa.network",
		"villa-models.volume",
		"villa-openwebui.container",
		"villa-openwebui.volume",
		"villa-qdrant.container",
		"villa-qdrant.volume",
		"villa-embed.container",
		"villa-rerank.container",
		"villa-extract.container",
		"villa-searxng.container",
		"villa-websafe.container",
		"villa-image.container",
		"villa-inferproxy.container",
		"villa-stt.container",
		"villa-tts.container",
		"villa-sandbox.network",
	}
	if got := unitNames(units); !slices.Equal(got, want) {
		t.Errorf("unit order = %v\nwant           %v", got, want)
	}
}

// TestSTTDeviceLinesAreTheInferenceUnitsOwn: villa-stt's device passthrough is read
// out of the Vulkan inference seam, so it equals the Vulkan inference unit's
// AddDevice lines and stays Vulkan's when the chat model runs on ROCm. No device,
// group or security literal may be typed into the voice render path itself.
func TestSTTDeviceLinesAreTheInferenceUnitsOwn(t *testing.T) {
	vulkan, err := Render(voiceFixtureInput())
	if err != nil {
		t.Fatalf("Render(vulkan): %v", err)
	}
	want := lines(unitByName(t, vulkan, "villa-llama.container").Text, "AddDevice=")
	if len(want) == 0 {
		t.Fatal("the Vulkan inference unit renders no AddDevice line; nothing to compare")
	}
	if got := lines(unitByName(t, vulkan, "villa-stt.container").Text, "AddDevice="); !slices.Equal(got, want) {
		t.Errorf("villa-stt AddDevice = %q, want the Vulkan inference unit's %q", got, want)
	}

	rocmIn := voiceFixtureInput()
	rocm, err := inference.BackendFor("rocm")
	if err != nil {
		t.Fatalf("BackendFor(rocm): %v", err)
	}
	rocmIn.Backend = rocm
	rocmIn.Cfg.Backend = "rocm"
	rocmUnits, err := Render(rocmIn)
	if err != nil {
		t.Fatalf("Render(rocm): %v", err)
	}
	if got := lines(unitByName(t, rocmUnits, "villa-stt.container").Text, "AddDevice="); !slices.Equal(got, want) {
		t.Errorf("with ROCm chat, villa-stt AddDevice = %q, want Vulkan's %q", got, want)
	}

	stt := unitByName(t, vulkan, "villa-stt.container").Text
	for _, key := range []string{"GroupAdd=", "PodmanArgs=", "PublishPort="} {
		if strings.Contains(stt, key) {
			t.Errorf("villa-stt carries %q; it needs the device and nothing more:\n%s", key, stt)
		}
	}

	for _, file := range []string{"voice.go", filepath.Join("quadlet", "stt.container.tmpl"), filepath.Join("quadlet", "tts.container.tmpl")} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, token := range []string{"/dev/", "keep-groups", "seccomp", "--device"} {
			if strings.Contains(string(src), token) {
				t.Errorf("%s types %q; the voice units' device set comes from the inference seam", file, token)
			}
		}
	}
}

// TestVoiceUnitsPublishNoPort: both voice units are reached by container DNS on
// villa.network only.
func TestVoiceUnitsPublishNoPort(t *testing.T) {
	units, err := Render(voiceFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, name := range []string{"villa-stt.container", "villa-tts.container"} {
		if u := unitByName(t, units, name); strings.Contains(u.Text, "PublishPort=") {
			t.Errorf("%s publishes a host port:\n%s", name, u.Text)
		}
	}
}

// TestARecordedVoicePinReachesTheRenderedUnit: an effective pin recorded by
// `villa update` replaces the vetted image in each voice unit.
func TestARecordedVoicePinReachesTheRenderedUnit(t *testing.T) {
	in := voiceFixtureInput()
	in.Pin = func(component string) string {
		switch component {
		case ComponentWhisper:
			return "example.invalid/whisper@sha256:aaaa"
		case ComponentKokoro:
			return "example.invalid/kokoro@sha256:bbbb"
		}
		return ""
	}
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if u := unitByName(t, units, "villa-stt.container"); !strings.Contains(u.Text, "Image=example.invalid/whisper@sha256:aaaa\n") {
		t.Errorf("villa-stt ignored its effective pin:\n%s", u.Text)
	}
	if u := unitByName(t, units, "villa-tts.container"); !strings.Contains(u.Text, "Image=example.invalid/kokoro@sha256:bbbb\n") {
		t.Errorf("villa-tts ignored its effective pin:\n%s", u.Text)
	}
}

// TestRenderOpenWebUIVoiceContainerGolden: with voice on, Open WebUI's audio
// settings point at the two units by container DNS, appended as one block before
// the single trailing ENABLE_PERSISTENT_CONFIG=False.
func TestRenderOpenWebUIVoiceContainerGolden(t *testing.T) {
	units, err := Render(voiceFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	goldenCompare(t, "villa-openwebui.container.voice.golden", c.Text)

	env := lines(c.Text, "Environment=")
	want := []string{
		"Environment=AUDIO_STT_ENGINE=openai",
		"Environment=AUDIO_STT_OPENAI_API_BASE_URL=http://villa-stt:8081/v1",
		"Environment=AUDIO_STT_OPENAI_API_KEY=sk-no-key-required",
		"Environment=AUDIO_STT_MODEL=whisper-1",
		"Environment=AUDIO_TTS_ENGINE=openai",
		"Environment=AUDIO_TTS_OPENAI_API_BASE_URL=http://villa-tts:8880/v1",
		"Environment=AUDIO_TTS_OPENAI_API_KEY=sk-no-key-required",
		"Environment=AUDIO_TTS_MODEL=kokoro",
		"Environment=AUDIO_TTS_VOICE=af_heart",
		"Environment=ENABLE_PERSISTENT_CONFIG=False",
	}
	if len(env) < len(want) || !slices.Equal(env[len(env)-len(want):], want) {
		t.Errorf("Open WebUI env tail = %q\nwant %q", env, want)
	}
}
