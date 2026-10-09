package orchestrate

// image.go holds the villa-image MANAGED-SERVICE constants and view builder (#312),
// the memory.go shape. sd-server is not an inference Backend: it is a digest-pinned
// OSS service, so its image literal is an orchestrate managed-service constant like
// searxngImage, allowlisted in TestSeamGrepGate in the same commit. What differs
// from every other managed service is the GPU: villa-image is the first one that
// needs the iGPU, and its device access and its flag vocabulary both come from the
// inference seam (VulkanGPUAccess, ImageServerArgs), never a literal here.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// imageServerImage is the digest-pinned stable-diffusion.cpp Vulkan image.
// master-vulkan is a ROLLING tag rebuilt on every upstream push; the digest is not.
// Resolved on the gfx1151 dev host 2026-10-08 (commit e16d26a, ubuntu 24.04 runtime)
// and measured there: see internal/catalog/images.json's provenance.
const imageServerImage = "ghcr.io/leejet/stable-diffusion.cpp:master-vulkan@sha256:367ccc4e2b09e3c9948cb3b93bd530466547416722171409f968241c50659181"

// ImageServerImage returns the vetted sd.cpp image (the pin table's accessor).
func ImageServerImage() string { return imageServerImage }

// ComponentImage is the pin-table component id for the image server.
const ComponentImage = "image-server"

const (
	imageContainerUnitName = "villa-image.container"
	// The image's ENTRYPOINT is /sd-cli; the server is /sd-server.
	imageEntrypoint = "/sd-server"
)

// ImageContainerUnitName is the unit Render appends when image generation is on,
// exported so install gates the start on the unit being in the written plan and
// status/doctor derive the service name (the EmbedContainerUnitName precedent).
func ImageContainerUnitName() string { return imageContainerUnitName }

// ImageServe is what villa-image serves, translated by the caller from
// catalog.ImageModel (ADR-0013: the renderer never imports the catalog). Filenames
// are bare names inside the shared models volume; the preset is the entry's, and it
// feeds both the Exec and Open WebUI's env so the two cannot disagree.
type ImageServe struct {
	DiffusionFile   string
	TextEncoderFile string
	VAEFile         string
	Steps           int
	CfgScale        float64
	Width           int
	Height          int
}

// imageView is what image.container.tmpl renders. No PublishPort: container DNS on
// villa.network only. No EnvironmentFile: sd-server takes no key, and only
// villa.network members (Open WebUI, the probe helper) can reach it.
type imageView struct {
	ContainerName string
	Image         string
	Network       string
	AddDevice     []string
	GroupAdd      []string
	PodmanArgs    string
	Volume        string
	Entrypoint    string
	Exec          string
}

// buildImageView assembles the view. image is the RESOLVED pin; the device access
// is the seam's; the models mount is the embedder's read-only one, shared rather
// than re-declared; the container-DNS name and port are config's constants so the
// unit, Open WebUI's env and the proof's probe target cannot diverge.
func buildImageView(image string, s ImageServe, gpu inference.GPUAccess, addr string, port int) imageView {
	var podmanArgs []string
	for _, opt := range gpu.SecurityOpts {
		podmanArgs = append(podmanArgs, "--security-opt", opt)
	}
	return imageView{
		ContainerName: addr,
		Image:         image,
		Network:       networkAttach,
		AddDevice:     gpu.Devices,
		GroupAdd:      gpu.Groups,
		PodmanArgs:    strings.Join(podmanArgs, " "),
		Volume:        embedModelMount,
		Entrypoint:    imageEntrypoint,
		Exec:          buildImageExec(s, port),
	}
}

// buildImageExec joins the seam's sd-server argv on a single space (no shell).
func buildImageExec(s ImageServe, port int) string {
	return strings.Join(inference.ImageServerArgs(inference.ImageRunSpec{
		DiffusionFile: s.DiffusionFile, TextEncoderFile: s.TextEncoderFile, VAEFile: s.VAEFile,
		Steps: s.Steps, CfgScale: s.CfgScale, Width: s.Width, Height: s.Height,
		Port: port,
	}), " ")
}

// imageOpenWebUIEnv is the ONE ordered env group buildOpenWebUIView appends when
// image generation is on (frozen by villa-openwebui.container.image.golden). The
// automatic1111 engine verifies with a real GET /sdapi/v1/options, lists the real
// file stem as the model and generates through POST /sdapi/v1/txt2img.
// IMAGE_GENERATION_MODEL stays unset so Open WebUI never POSTs a checkpoint switch
// to a single-model server; AUTOMATIC1111_PARAMS stays unset because cfg_scale is
// sd-server's argv default.
func imageOpenWebUIEnv(s ImageServe, addr string, port int) []envPair {
	return []envPair{
		{Key: "ENABLE_IMAGE_GENERATION", Value: "True"},
		{Key: "IMAGE_GENERATION_ENGINE", Value: "automatic1111"},
		{Key: "AUTOMATIC1111_BASE_URL", Value: fmt.Sprintf("http://%s:%d", addr, port)},
		{Key: "IMAGE_SIZE", Value: strconv.Itoa(s.Width) + "x" + strconv.Itoa(s.Height)},
		{Key: "IMAGE_STEPS", Value: strconv.Itoa(s.Steps)},
	}
}

// ImageInNetworkEndpoint is sd-server's base URL on villa.network, for the
// in-network probes (readiness and the proof's generation).
func ImageInNetworkEndpoint() string {
	return fmt.Sprintf("http://%s:%d", config.ImageAddr, config.ImagePort)
}
