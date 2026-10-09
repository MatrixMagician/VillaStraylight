package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// image.go is the image-generation half of the catalog: a COMPILED-IN table of
// image models (images.json), separate from the chat catalog (seed.json).
//
// INVARIANT: an image entry is never a chat entry. A separate type in a separate
// table is how the image role is enforced: nothing that walks Catalog.Models (Pick,
// pickOverride, pickCoder, model swap, resident add, model list, the dashboard
// picker) can see one, so no walker needs a filter and none can try to serve a
// diffusion GGUF through llama-server.
//
// INVARIANT: the table has no external override. catalog_path replaces seed.json,
// never this table. An image entry is a vetted, measured artifact like
// install.NomicEmbedShard; changing it is a code change carrying a fresh measurement
// in Provenance. That is what lets the reservation row, the render, the pre-stage
// and the proof each read one answer with no catalog threaded through their callers.
//
// PURE: go:embed + json.Unmarshal; no filesystem, no network.

//go:embed images.json
var imagesJSON []byte

// ImageModel is one image-generation entry: three named weight files and the
// generation preset they were measured at.
//
// The three files are NAMED fields, not a []Shard, because sd-server takes each
// under its own flag (--diffusion-model, --llm, --vae). A positional convention
// would be one reorder away from a unit that serves the VAE as the diffusion model.
type ImageModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Quant       string `json:"quant"`

	// Diffusion, TextEncoder and VAE are the download manifest. Each Shard's SHA256
	// and SizeBytes come from a HuggingFace HEAD (X-Linked-Etag / X-Linked-Size), and
	// download.PullModel verifies both. The VAE is safetensors, not GGUF, so this
	// checksum is its only witness.
	Diffusion   Shard `json:"diffusion"`
	TextEncoder Shard `json:"text_encoder"`
	VAE         Shard `json:"vae"`

	// WeightBytes is the params footprint sd-server holds on the GPU once loaded:
	// the MEASURED "total params memory size" figure at the pinned image, not the
	// sum of the on-disk files. It is the GTT-floor reference and the witness
	// reference the proof compares the running server's figure against (ADR-0007:
	// catalog truth, runtime witness, disagreement reported, never auto-corrected).
	WeightBytes uint64 `json:"weight_bytes"`
	// ComputeBytes is the working memory the reservation adds to WeightBytes: the
	// MEASURED peak GTT delta during a generation at this preset, plus a 10% margin,
	// minus WeightBytes, rounded up to a MiB. The row is WeightBytes + ComputeBytes.
	ComputeBytes uint64 `json:"compute_bytes"`

	// The preset: sd-server's argv defaults and Open WebUI's IMAGE_STEPS and
	// IMAGE_SIZE. Model facts, not knobs: Z-Image-Turbo is distilled to cfg 1.0 and
	// about 8 steps, and another value is a different, unmeasured footprint.
	Steps    int     `json:"steps"`
	CfgScale float64 `json:"cfg_scale"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`

	// Provenance names the on-hardware measurement that produced WeightBytes and
	// ComputeBytes: host, image digest, date, flags, the placement line.
	Provenance string `json:"provenance"`
}

// imageModels is the decoded table. A malformed embed is a build-time programming
// error, so it panics at init, the agent.LoadCrushPolicy precedent.
var imageModels = mustDecodeImages()

func mustDecodeImages() []ImageModel {
	var table struct {
		Images []ImageModel `json:"images"`
	}
	if err := json.Unmarshal(imagesJSON, &table); err != nil {
		panic(fmt.Sprintf("catalog: embedded images.json failed to decode: %v", err))
	}
	return table.Images
}

// Image resolves an image entry by id from the compiled-in table. False on an
// unknown id, never a zero entry a caller might size or render.
func Image(id string) (ImageModel, bool) {
	for _, m := range imageModels {
		if m.ID == id {
			return m, true
		}
	}
	return ImageModel{}, false
}

// Shards is the entry's full download manifest in flag order: the diffusion
// model, the text encoder, then the VAE. The pre-stage pulls it and the presence
// check stats it.
func (m ImageModel) Shards() []Shard {
	return []Shard{m.Diffusion, m.TextEncoder, m.VAE}
}
