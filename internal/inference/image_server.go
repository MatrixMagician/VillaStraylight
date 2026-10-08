package inference

import (
	"path/filepath"
	"strconv"
)

// image_server.go is the sd-server half of the backend seam: the flag vocabulary
// villa-image is rendered with, kept here because the placement pair names the
// Vulkan device token and the seam gate (seam_test.go) forbids the flags outside
// this package. orchestrate joins the slice; it never writes a flag.

// ImageRunSpec is what differs per image run: the three weight files inside the
// models volume, the generation preset, and the in-network listen port.
type ImageRunSpec struct {
	DiffusionFile   string
	TextEncoderFile string
	VAEFile         string
	Steps           int
	CfgScale        float64
	Width           int
	Height          int
	Port            int
}

// ImageServerArgs renders sd-server's argument slice for one run. The placement
// pair is the invariant: --backend and --params-backend both name the Vulkan
// device token, which disables sd-server's auto-fit (its default places params on
// the GPU, RAM or disk by free memory, the silent CPU fallback ADR-0001 forbids)
// and makes the params placement line a start-time fact. --eager-load holds the
// whole footprint from start, so the reservation row is true at every moment and
// the line is in the journal before the proof's first request. --vae-tiling keeps
// the 1024x1024 VAE decode at 416 MB instead of a 5.8 GB buffer the device refuses.
// The preset is argv: Open WebUI's txt2img body carries no cfg_scale, so sd-server's
// default IS the value used. --offload-to-cpu is never emitted.
func ImageServerArgs(spec ImageRunSpec) []string {
	dev := backendVulkan{}.ResidencyProof().DeviceToken
	return []string{
		"--listen-ip", "0.0.0.0", // container-internal only; no host publish
		"--listen-port", strconv.Itoa(spec.Port),
		"--diffusion-model", filepath.Join(containerModelsDir, spec.DiffusionFile),
		"--llm", filepath.Join(containerModelsDir, spec.TextEncoderFile),
		"--vae", filepath.Join(containerModelsDir, spec.VAEFile),
		"--backend", dev,
		"--params-backend", dev,
		"--eager-load",
		"--vae-tiling",
		"--diffusion-fa",
		"--cfg-scale", strconv.FormatFloat(spec.CfgScale, 'g', -1, 64),
		"--steps", strconv.Itoa(spec.Steps),
		"-W", strconv.Itoa(spec.Width),
		"-H", strconv.Itoa(spec.Height),
	}
}
