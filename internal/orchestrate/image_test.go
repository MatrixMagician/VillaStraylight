package orchestrate

import (
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// imageFixtureInput is the deterministic RenderInput the image goldens are frozen
// against: fixtureInput() with image generation on and the Q8_0 entry resolved
// into an ImageServe, the way stackapply.input hands it over.
func imageFixtureInput() RenderInput {
	in := fixtureInput()
	in.Cfg.ImageEnabled = true
	in.Cfg.ImageModel = "z-image-turbo"
	in.Image = &ImageServe{
		DiffusionFile: "z_image_turbo-Q8_0.gguf", TextEncoderFile: "Qwen3-4B-Instruct-2507-Q4_K_M.gguf", VAEFile: "z-image-turbo-ae.safetensors",
		Steps: 8, CfgScale: 1, Width: 1024, Height: 1024,
	}
	return in
}

// TestRenderImageUnit: with image generation on, villa-image.container matches its
// golden byte-for-byte and carries the measured Exec: explicit Vulkan placement,
// eager load, VAE tiling, the preset as argv, the sd-server entrypoint, the device
// access the chat unit uses, the read-only models mount and no host port.
func TestRenderImageUnit(t *testing.T) {
	units, err := Render(imageFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-image.container")
	goldenCompare(t, "villa-image.container.golden", c.Text)

	wantExec := "Exec=--listen-ip 0.0.0.0 --listen-port 1234 --diffusion-model /models/z_image_turbo-Q8_0.gguf --llm /models/Qwen3-4B-Instruct-2507-Q4_K_M.gguf --vae /models/z-image-turbo-ae.safetensors --backend Vulkan0 --params-backend Vulkan0 --eager-load --vae-tiling --diffusion-fa --cfg-scale 1 --steps 8 -W 1024 -H 1024"
	for _, want := range []string{
		wantExec,
		"ContainerName=villa-image",
		"Image=" + ImageServerImage(),
		"Entrypoint=/sd-server",
		"Volume=villa-models:/models:ro,z",
		"Network=villa-closed.network",
	} {
		if !strings.Contains(c.Text, want) {
			t.Errorf("image unit missing %q:\n%s", want, c.Text)
		}
	}
	gpu := inference.VulkanGPUAccess()
	for _, dev := range gpu.Devices {
		if !strings.Contains(c.Text, "AddDevice="+dev) {
			t.Errorf("image unit missing AddDevice=%s:\n%s", dev, c.Text)
		}
	}
	for _, g := range gpu.Groups {
		if !strings.Contains(c.Text, "GroupAdd="+g) {
			t.Errorf("image unit missing GroupAdd=%s:\n%s", g, c.Text)
		}
	}
	for _, forbidden := range []string{"PublishPort=", "EnvironmentFile=", "--offload-to-cpu"} {
		if strings.Contains(c.Text, forbidden) {
			t.Errorf("image unit must not carry %q:\n%s", forbidden, c.Text)
		}
	}
}

// TestRenderImageUnitSharesTheChatUnitsDeviceAccess: the image unit's AddDevice,
// GroupAdd and PodmanArgs lines are the chat unit's own, so the two GPU consumers
// cannot drift apart on the rootless device contract.
func TestRenderImageUnitSharesTheChatUnitsDeviceAccess(t *testing.T) {
	units, err := Render(imageFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	img := unitByName(t, units, "villa-image.container").Text
	chat := unitByName(t, units, "villa-llama.container").Text
	for _, line := range strings.Split(chat, "\n") {
		if strings.HasPrefix(line, "AddDevice=") || strings.HasPrefix(line, "GroupAdd=") || strings.HasPrefix(line, "PodmanArgs=") {
			if !strings.Contains(img, line+"\n") {
				t.Errorf("chat unit line %q is absent from the image unit", line)
			}
		}
	}
}

// TestRenderOpenWebUIImageGroup: with image generation on, Open WebUI gains ONE
// ordered env group wiring its automatic1111 engine at villa-image's in-network
// address with the preset size and steps, and the trailing
// ENABLE_PERSISTENT_CONFIG=False gate is emitted exactly once, last, so the keys
// are read from env rather than the DB.
func TestRenderOpenWebUIImageGroup(t *testing.T) {
	units, err := Render(imageFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	goldenCompare(t, "villa-openwebui.container.image.golden", c.Text)

	wantGroup := "Environment=ENABLE_IMAGE_GENERATION=True\n" +
		"Environment=IMAGE_GENERATION_ENGINE=automatic1111\n" +
		"Environment=AUTOMATIC1111_BASE_URL=http://villa-image:1234\n" +
		"Environment=IMAGE_SIZE=1024x1024\n" +
		"Environment=IMAGE_STEPS=8\n" +
		"Environment=ENABLE_PERSISTENT_CONFIG=False\n"
	if !strings.Contains(c.Text, wantGroup) {
		t.Errorf("Open WebUI unit missing the image env group followed by the persistent-config gate:\n%s", c.Text)
	}
	if n := strings.Count(c.Text, "ENABLE_PERSISTENT_CONFIG=False"); n != 1 {
		t.Errorf("ENABLE_PERSISTENT_CONFIG=False emitted %d times, want exactly once", n)
	}
	for _, forbidden := range []string{"IMAGE_GENERATION_MODEL", "AUTOMATIC1111_PARAMS"} {
		if strings.Contains(c.Text, forbidden) {
			t.Errorf("Open WebUI unit carries %s, which the design leaves unset:\n%s", forbidden, c.Text)
		}
	}
}

// TestRenderRefusesImageOnWithoutAResolvedModel: image generation on with no
// resolved ImageServe is a refusal, never a unit whose -m paths are empty.
func TestRenderRefusesImageOnWithoutAResolvedModel(t *testing.T) {
	in := imageFixtureInput()
	in.Image = nil
	if _, err := Render(in); err == nil || !strings.Contains(err.Error(), "image") {
		t.Fatalf("Render with image on and no model = %v, want a refusal naming image generation", err)
	}
}

// TestRenderImageOffIsByteIdentical: with image generation off, no image unit is
// rendered and the Open WebUI unit is the existing golden, so an opted-out stack
// is unchanged by this feature.
func TestRenderImageOffIsByteIdentical(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, u := range units {
		if u.Name == "villa-image.container" {
			t.Errorf("image off: Render emitted %q", u.Name)
		}
	}
	goldenCompare(t, "villa-openwebui.container.golden", unitByName(t, units, "villa-openwebui.container").Text)
}

// TestImageUnitSitsBeforeInferproxyAndTheNetworksStayLast pins the unit order
// with every gate on: the image unit follows the web-search block and precedes
// villa-inferproxy, and the sandbox and closed networks stay last.
func TestImageUnitSitsBeforeInferproxyAndTheNetworksStayLast(t *testing.T) {
	in := imageFixtureInput()
	in.Cfg.WebSearchEnabled = true
	in.Cfg.WorkspaceAgent = true
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	names := unitNames(units)
	idx := func(name string) int {
		for i, n := range names {
			if n == name {
				return i
			}
		}
		t.Fatalf("%s not rendered: %v", name, names)
		return -1
	}
	websafe, image, inferproxy := idx("villa-websafe.container"), idx("villa-image.container"), idx("villa-inferproxy.container")
	if websafe > image || image > inferproxy {
		t.Errorf("unit order = %v, want websafe < image < inferproxy", names)
	}
	if tail := names[len(names)-2:]; !slices.Equal(tail, []string{"villa-sandbox.network", "villa-closed.network"}) {
		t.Errorf("last units = %v, want [villa-sandbox.network villa-closed.network]", tail)
	}
}
