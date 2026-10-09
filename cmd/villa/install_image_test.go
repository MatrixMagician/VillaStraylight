package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// TestLiveImageModelPresentChecksEveryFileAtCatalogSize: the image model is present
// only when all three files are on disk at their catalog sizes, so a missing VAE or a
// truncated diffusion file re-pulls through the verified downloader rather than
// starting a server that fails at load.
func TestLiveImageModelPresentChecksEveryFileAtCatalogSize(t *testing.T) {
	m, _ := catalog.Image("z-image-turbo")
	seed := func(t *testing.T, dir string, shards []catalog.Shard) {
		t.Helper()
		for _, sh := range shards {
			f, err := os.Create(filepath.Join(dir, sh.Filename))
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Truncate(int64(sh.SizeBytes)); err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
		}
	}

	dir := t.TempDir()
	if liveImageModelPresent(dir, m) {
		t.Error("an empty models dir reads as present")
	}
	seed(t, dir, m.Shards()[:2])
	if liveImageModelPresent(dir, m) {
		t.Error("two of three files read as present; the VAE is missing")
	}
	seed(t, dir, m.Shards()[2:])
	if !liveImageModelPresent(dir, m) {
		t.Error("all three files at catalog size read as absent")
	}
	if err := os.WriteFile(filepath.Join(dir, m.Diffusion.Filename), []byte("truncated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if liveImageModelPresent(dir, m) {
		t.Error("a truncated diffusion file reads as present")
	}
}

// TestImageGenerationResultCountsImages maps sd-server's txt2img response onto the
// residency protocol's ChatResult: OK with one "token" per decoded image, and a
// failure for an empty or unparseable body, so a server that answered 200 with no
// image is a failed probe rather than a passed one.
func TestImageGenerationResultCountsImages(t *testing.T) {
	ok := imageGenerationResult([]byte(`{"images":["iVBORw0KGgo=","iVBORw0KGgo="],"parameters":{},"info":""}`), nil)
	if !ok.OK || ok.Tokens != 2 {
		t.Errorf("two images = %+v, want OK with Tokens 2", ok)
	}
	empty := imageGenerationResult([]byte(`{"images":[]}`), nil)
	if empty.OK || empty.Tokens != 0 || !strings.Contains(empty.Detail, "no image") {
		t.Errorf("no images = %+v, want a failed probe naming the empty result", empty)
	}
	garbage := imageGenerationResult([]byte(`<html>`), nil)
	if garbage.OK || garbage.Detail == "" {
		t.Errorf("unparseable body = %+v, want a failed probe with detail", garbage)
	}
	errRes := imageGenerationResult(nil, os.ErrDeadlineExceeded)
	if errRes.OK || !strings.Contains(errRes.Detail, os.ErrDeadlineExceeded.Error()) {
		t.Errorf("curl error = %+v, want a failed probe carrying the error", errRes)
	}
}

// TestImageProofDrivesThePresetAt512: the proof's txt2img body is the fixed prompt
// and seed at the entry's steps and cfg scale, but at 512x512 rather than the
// preset size, so install, doctor and update each pay the measured ~20 s rather
// than ~65 s while the reservation stays sized at the preset.
func TestImageProofDrivesThePresetAt512(t *testing.T) {
	m, _ := catalog.Image("z-image-turbo")
	raw, err := imageProofBody(m)
	if err != nil {
		t.Fatalf("imageProofBody: %v", err)
	}
	var body struct {
		Prompt    string  `json:"prompt"`
		Seed      int     `json:"seed"`
		Steps     int     `json:"steps"`
		CfgScale  float64 `json:"cfg_scale"`
		Width     int     `json:"width"`
		Height    int     `json:"height"`
		BatchSize int     `json:"batch_size"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.Prompt != imageProofPrompt || body.Seed != imageProofSeed || body.BatchSize != 1 {
		t.Errorf("body = %+v, want the fixed prompt, seed and one image", body)
	}
	if body.Steps != m.Steps || body.CfgScale != m.CfgScale {
		t.Errorf("steps/cfg = %d/%g, want the entry's %d/%g", body.Steps, body.CfgScale, m.Steps, m.CfgScale)
	}
	if body.Width != imageProofSide || body.Height != imageProofSide || imageProofSide != 512 {
		t.Errorf("drive size = %dx%d, want 512x512", body.Width, body.Height)
	}
}

// TestImageProofTargetIsTheEntryOnVulkan: the proof's target names the image unit's
// service, the entry's id, diffusion file and measured footprint, and the Vulkan
// markers whatever backend the chat model runs on, because villa-image is always
// Vulkan.
func TestImageProofTargetIsTheEntryOnVulkan(t *testing.T) {
	m, _ := catalog.Image("z-image-turbo-q4")
	tgt := imageProofTarget(m)
	if tgt.Service != unitServiceName(orchestrate.ImageContainerUnitName()) {
		t.Errorf("Service = %q, want the image unit's service", tgt.Service)
	}
	if tgt.ModelID != m.ID || tgt.ModelFile != m.Diffusion.Filename || tgt.WeightBytes != m.WeightBytes {
		t.Errorf("target = %+v, want the entry's id, diffusion file and weight", tgt)
	}
	if tgt.Markers != inference.VulkanBackend().ResidencyProof() {
		t.Errorf("Markers = %+v, want the Vulkan markers", tgt.Markers)
	}
	if tgt.ReadyTimeout <= 0 {
		t.Error("the proof has no ready timeout")
	}
}

// TestImageServeTranslatesTheEntry: the status seam hands the renderer the same
// translation stackapply renders every other verb with, and nil when the gate is
// off so an opted-out status run is unchanged.
func TestImageServeTranslatesTheEntry(t *testing.T) {
	on, err := liveImageServe(config.VillaConfig{ImageEnabled: true, ImageModel: "z-image-turbo-q4"})
	if err != nil || on == nil || on.DiffusionFile != "z_image_turbo-Q4_K.gguf" || on.Width != 1024 {
		t.Errorf("image on = %+v, %v; want the Q4 entry's files and preset", on, err)
	}
	if off, err := liveImageServe(config.VillaConfig{ImageModel: "z-image-turbo"}); err != nil || off != nil {
		t.Errorf("image off = %+v, %v; want nil, nil", off, err)
	}
	if _, err := liveImageServe(config.VillaConfig{ImageEnabled: true, ImageModel: "ghost"}); err == nil {
		t.Error("an unknown id did not error")
	}
}
