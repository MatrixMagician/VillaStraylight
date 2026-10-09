package inference

import (
	"slices"
	"strings"
	"testing"
)

// TestVulkanGPUAccessIsWhatTheChatUnitRenders: the device access a managed GPU
// service reads from the seam is byte-for-byte the pair the Vulkan chat unit's
// ContainerArgs emits, so villa-image and villa-llama cannot drift apart on the
// DRI node or the keep-groups detail that makes renderD128 openable rootless.
func TestVulkanGPUAccessIsWhatTheChatUnitRenders(t *testing.T) {
	gpu := VulkanGPUAccess()
	args := VulkanBackend().ContainerArgs(RunSpec{ContainerName: "x", ModelFile: "m.gguf", ModelsDir: "/m", ContextLen: 8})
	for _, dev := range gpu.Devices {
		if !slices.Contains(args, dev) {
			t.Errorf("device %q is not in the chat unit's args", dev)
		}
	}
	for _, g := range gpu.Groups {
		if !slices.Contains(args, g) {
			t.Errorf("group %q is not in the chat unit's args", g)
		}
	}
	if len(gpu.Devices) != 1 || len(gpu.Groups) != 1 {
		t.Errorf("GPUAccess = %+v, want one device and one group", gpu)
	}
}

// TestImageServerArgsPlaceExplicitlyOnTheDevice is the Exec builder's invariant:
// every image run names the Vulkan device token for BOTH --backend and
// --params-backend (explicit placement disables sd-server's auto-fit, which would
// otherwise place params on the CPU by free memory), eager-loads so the footprint
// is held from start, tiles the VAE, and never asks for a CPU offload.
func TestImageServerArgsPlaceExplicitlyOnTheDevice(t *testing.T) {
	args := ImageServerArgs(ImageRunSpec{
		DiffusionFile: "z_image_turbo-Q8_0.gguf", TextEncoderFile: "Qwen3-4B-Instruct-2507-Q4_K_M.gguf", VAEFile: "z-image-turbo-ae.safetensors",
		Steps: 8, CfgScale: 1, Width: 1024, Height: 1024, Port: 1234,
	})
	got := strings.Join(args, " ")
	want := "--listen-ip 0.0.0.0 --listen-port 1234 --diffusion-model /models/z_image_turbo-Q8_0.gguf --llm /models/Qwen3-4B-Instruct-2507-Q4_K_M.gguf --vae /models/z-image-turbo-ae.safetensors --backend Vulkan0 --params-backend Vulkan0 --eager-load --vae-tiling --diffusion-fa --cfg-scale 1 --steps 8 -W 1024 -H 1024"
	if got != want {
		t.Errorf("ImageServerArgs =\n %s\nwant\n %s", got, want)
	}
	token := VulkanBackend().ResidencyProof().DeviceToken
	for _, flag := range []string{"--backend", "--params-backend"} {
		i := slices.Index(args, flag)
		if i < 0 || i+1 >= len(args) || args[i+1] != token {
			t.Errorf("%s is not followed by the Vulkan device token %q", flag, token)
		}
	}
	for _, forbidden := range []string{"--offload-to-cpu", "--auto-fit"} {
		if slices.Contains(args, forbidden) {
			t.Errorf("args carry %s; placement is explicit and never a CPU offload", forbidden)
		}
	}
}
