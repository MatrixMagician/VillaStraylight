package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/install"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/residency"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// install_image.go is the image-generation wiring (#312), the install_memory.go
// shape: presence and pull of the three weight files, the in-network health probe,
// and THE image offload proof that install, doctor and `villa update image` share.
// The proof is residency.Prove unchanged: only Generate (one txt2img) and Fold
// (inference.ImageOffloadVerdict) differ from the chat proof, and the probes reach
// villa-image over villa.network through the same curl helper every managed-service
// probe uses, so no host port is opened.

// The proof's drive: a fixed prompt and seed at the entry's steps and cfg scale,
// but at 512x512 rather than the preset. The reservation is sized at the preset;
// the proof only has to show where the params sit and that the GPU does the work,
// and 512x512 does that in the measured ~20 s instead of ~65 s.
const (
	imageProofPrompt = "a red bicycle leaning on a blue wall, photo"
	imageProofSeed   = 42
	imageProofSide   = 512
	// imageReadyTimeout bounds the readiness poll and the whole proof: an image
	// pull on first start, an eager load of about 9 GB, then one 512x512 drive.
	imageReadyTimeout = 10 * time.Minute
)

// imageReadyRoute is sd-server's cheap readiness GET; it has no /health route.
const imageReadyRoute = "/sdapi/v1/options"

// liveImageModelPresent reports whether every file of the entry is on disk at its
// catalog size (the liveEmbedModelPresent stat guard, per file): a missing VAE or a
// truncated diffusion file reads as absent and is re-pulled through the verified
// downloader rather than trusted.
func liveImageModelPresent(modelsDir string, m catalog.ImageModel) bool {
	for _, sh := range m.Shards() {
		fi, err := os.Stat(filepath.Join(modelsDir, sh.Filename))
		if err != nil || fi.Size() < 0 || uint64(fi.Size()) != sh.SizeBytes {
			return false
		}
	}
	return true
}

// liveEnsureImageModel pulls the entry's three files through the verified
// downloader (HEAD size/etag, stream, SHA-256 + size check, atomic rename per
// shard), wrapped as one catalog.Model the way the embed pre-stage wraps its shard.
func liveEnsureImageModel(ctx context.Context, modelsDir string, m catalog.ImageModel) error {
	if mkErr := os.MkdirAll(modelsDir, 0o700); mkErr != nil {
		return mkErr
	}
	return pullFn(ctx, catalog.Model{ID: m.ID, Shards: m.Shards()}, modelsDir)
}

// liveImageServe is the status read-model's image seam: the same translation
// every other verb renders through.
func liveImageServe(cfg config.VillaConfig) (*orchestrate.ImageServe, error) {
	return stackapply.ImageServe(cfg)
}

// liveImageHealth probes sd-server's readiness route in-network through the
// shared status prober (200 ready, 503 loading, refused down).
func liveImageHealth(config.VillaConfig) status.HealthState {
	return statusProber().Coded(orchestrate.ImageInNetworkEndpoint() + imageReadyRoute)
}

// liveImageProof is THE image offload proof. It is residency.Prove with the image
// drive and the sd-server fold; villa-image is always Vulkan, whatever backend the
// chat model runs on, so the markers are the Vulkan backend's.
func liveImageProof(ctx context.Context, cfg config.VillaConfig) inference.Verdict {
	img, ok := catalog.Image(cfg.ImageModel)
	if !ok {
		return inference.Verdict{Status: inference.StatusWarn, Detail: fmt.Sprintf("image model %q is not in the image table — nothing to prove against", cfg.ImageModel)}
	}
	return residency.Prove(ctx, residency.Deps{
		PollHealth: imagePollReady,
		Generate:   imageGenerate(img),
		GPUBusy:    detect.GPUBusyPercent,
		GTTUsed:    detect.GTTUsedBytes,
		Journal:    orchestrate.NewSystemd().ResidencyJournal,
		Fold:       inference.ImageOffloadVerdict,
	}, imageProofTarget(img))
}

// imageProofTarget names what the proof drives: the image unit's invocation
// journal, the entry's id and diffusion file, and its measured footprint as the GTT
// floor and witness reference.
func imageProofTarget(m catalog.ImageModel) residency.Target {
	return residency.Target{
		Service:      unitServiceName(orchestrate.ImageContainerUnitName()),
		ModelID:      m.ID,
		ModelFile:    m.Diffusion.Filename,
		WeightBytes:  m.WeightBytes,
		Markers:      inference.VulkanBackend().ResidencyProof(),
		ReadyTimeout: imageReadyTimeout,
	}
}

// imagePollReady polls sd-server's readiness route in-network until it answers 200
// or the deadline passes. The deadline is a confident not-ready, the protocol's own
// rule for a server that never comes up.
func imagePollReady(ctx context.Context, timeout time.Duration) detect.Bool {
	deadline := time.Now().Add(timeout)
	for {
		if liveImageHealth(config.VillaConfig{}) == status.HealthReady {
			return detect.KnownBool(true, imageReadyRoute+" 200")
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return detect.KnownBool(false, imageReadyRoute+" not 200 before timeout")
		}
		select {
		case <-ctx.Done():
			return detect.KnownBool(false, imageReadyRoute+" not 200 before timeout")
		case <-time.After(readinessInterval):
		}
	}
}

// imageGenerate drives ONE real txt2img in-network and maps the response onto the
// protocol's ChatResult: OK with one "token" per decoded image. The body is JSON,
// never interpolated into a command string.
func imageGenerate(m catalog.ImageModel) func(ctx context.Context, modelID string) inference.ChatResult {
	return func(ctx context.Context, _ string) inference.ChatResult {
		body, err := imageProofBody(m)
		if err != nil {
			return inference.ChatResult{Detail: "build txt2img body: " + err.Error()}
		}
		out, err := runProbeCurl(ctx, orchestrate.EmbedImage(),
			"-sf", "-X", "POST", orchestrate.ImageInNetworkEndpoint()+"/sdapi/v1/txt2img",
			"-H", "Content-Type: application/json",
			"-d", string(body),
		)
		return imageGenerationResult(out, err)
	}
}

// imageProofBody is the fixed txt2img request at the entry's steps and cfg scale.
func imageProofBody(m catalog.ImageModel) ([]byte, error) {
	return json.Marshal(map[string]any{
		"prompt":     imageProofPrompt,
		"seed":       imageProofSeed,
		"steps":      m.Steps,
		"cfg_scale":  m.CfgScale,
		"width":      imageProofSide,
		"height":     imageProofSide,
		"batch_size": 1,
	})
}

// imageGenerationResult maps curl's outcome onto ChatResult: a decoded images array
// with at least one entry is a successful drive; anything else is a failed probe
// with the reason, so a 200 with no image never passes.
func imageGenerationResult(out []byte, err error) inference.ChatResult {
	if err != nil {
		return inference.ChatResult{Detail: "txt2img: " + err.Error()}
	}
	var resp struct {
		Images []string `json:"images"`
	}
	if jerr := json.Unmarshal(out, &resp); jerr != nil {
		return inference.ChatResult{Detail: "decode txt2img response: " + jerr.Error()}
	}
	if len(resp.Images) == 0 {
		return inference.ChatResult{Detail: "txt2img answered with no image"}
	}
	return inference.ChatResult{OK: true, Tokens: len(resp.Images)}
}

// liveImageResidency builds doctor's IMG-DOC-residency seam. Doctor is read-only
// and never starts a service, so an inactive image unit is an unevaluable WARN
// naming the precondition rather than a ten-minute poll that ends in a FAIL.
func liveImageResidency(ctx context.Context, cfg config.VillaConfig, sd *status.Deps) func() inference.Verdict {
	return func() inference.Verdict {
		svc := unitServiceName(orchestrate.ImageContainerUnitName())
		if state, err := sd.IsActive(svc); err != nil || state != "active" {
			return residency.Unevaluable(
				fmt.Sprintf("could not evaluate the image server's offload — %s is not active", svc),
				"start the stack (`villa up`), then re-run `villa doctor`")
		}
		return liveImageProof(ctx, cfg)
	}
}

// proofFromVerdict maps the tri-state residency verdict onto install's Proof
// verbatim: PASS, WARN and FAIL each keep their meaning.
func proofFromVerdict(v inference.Verdict) install.Proof {
	switch v.Status {
	case inference.StatusPass:
		return install.Proof{Status: preflight.StatusPass, Detail: v.Detail}
	case inference.StatusFail:
		return install.Proof{Status: preflight.StatusFail, Detail: v.Detail}
	default:
		return install.Proof{Status: preflight.StatusWarn, Detail: v.Detail}
	}
}
