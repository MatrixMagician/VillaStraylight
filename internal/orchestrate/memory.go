package orchestrate

// memory.go holds the v1.3 MEMORY-STACK managed-service constants and view builders
// (Phase-19 D-02/D-04/D-10). Like openwebui.go, neither villa-qdrant nor villa-embed
// is an inference Backend: they are fixed OSS / pinned-toolbox managed services, so
// their image, volume mount, and Exec are orchestrate-level managed-service constants
// — a DIFFERENT category from the GPU/Vulkan backend literals that
// internal/inference's TestSeamGrepGate guards (same category as openWebUIImage).
// Living here keeps the inference seam gate green while giving Render() a dedicated,
// parseContainerArgs-free render path (Pitfall 4: these views carry no GPU device
// passthrough / supplemental-group args). The two docker.io/ image literals below
// DO trip the "container image literal" regex, so the seam_test.go isSeam allowlist
// is extended for orchestrate/memory.go in the SAME commit (Pitfall 7), mirroring the
// 12-02 ROCm-tag same-commit precedent.

import (
	"strconv"
	"strings"
)

// qdrantImage is the digest-pinned Qdrant vector-store image (D-01: the OFFICIAL
// qdrant/qdrant org, the v1.18.2-UNPRIVILEGED variant that runs as USER_ID=1000 so the
// rootless-Podman UID / SELinux :Z storage mount stays writable — SC#2). The dev-box
// RepoDigest is resolved via `podman pull docker.io/qdrant/qdrant:v1.18.2-unprivileged &&
// podman image inspect docker.io/qdrant/qdrant:v1.18.2-unprivileged --format
// '{{index .RepoDigests 0}}'`. Committed here is the manifest-list digest from 19-RESEARCH
// (resolved 2026-06-09); the on-hardware RepoDigest confirmation + a /v1 readiness curl
// is Plan 19-03's checkpoint:human-verify before the unit is frozen.
// TODO(19-03): confirm dev-box RepoDigest matches this manifest-list digest.
// The :v1.18.2-unprivileged tag is silently rebuilt; the digest is not (reproducibility).
const qdrantImage = "docker.io/qdrant/qdrant:v1.18.2-unprivileged@sha256:b79aaa49ce7a7e5b7e9cf3fe76be400c911457084b4b7af47487c1c9ae5962e5"

// QdrantImage returns the digest-pinned Qdrant image so callers (the Phase-23 backup
// manifest) can record it WITHOUT re-typing the literal. The literal stays behind the
// orchestrate managed-service seam (D-02), mirroring OpenWebUIImage().
func QdrantImage() string { return qdrantImage }

// embedImage is the digest-pinned image villa-embed serves the embeddings llama-server
// from. It is a DELIBERATELY INDEPENDENT const (D-04): the literal is byte-identical to
// the kyuz0 Strix-Halo Vulkan RADV `vulkanImage` in internal/inference/backend_vulkan.go,
// but it is NOT a reference into the inference backend seam — keeping it a separate
// managed-service const keeps TestSeamGrepGate semantics clean (a managed-service image,
// not a GPU-backend token, D-10).
// == vulkanImage (D-04)
const embedImage = "docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555c45864352a4077528836988d448e9f030fbab9f7376ea1c603ac7aad"

// EmbedImage returns the digest-pinned villa-embed image (mirrors QdrantImage()/
// OpenWebUIImage()) so downstream readers never re-type the literal.
func EmbedImage() string { return embedImage }

// embedGGUFFilename is the ONE authoritative GGUF filename shared by the served `-m`
// path (buildEmbedExec, below) and the Plan-19-02 nomic pre-stage Shard.Filename. Both
// ends bind THIS single source of truth through the exported EmbedGGUFFilename()
// accessor so the served model path and the pre-staged file can NEVER drift (Pitfall 3).
const embedGGUFFilename = "nomic-embed-text-v1.5.Q8_0.gguf"

// EmbedGGUFFilename returns the single-source embed GGUF filename. EXPORTED so Plan
// 19-02's cross-package drift test can assert nomicEmbedShard.Filename equals THIS
// value (one symbol, no duplicated literal) — Pitfall 3.
func EmbedGGUFFilename() string { return embedGGUFFilename }

// Qdrant + villa-embed stable Quadlet identities (this project's unit-name / DNS /
// volume contract, asserted by the goldens — they leak no GPU/image assumption).
const (
	qdrantContainerUnitName = "villa-qdrant.container"
	qdrantVolumeUnitName    = "villa-qdrant.volume"

	qdrantContainerName = "villa-qdrant"
	qdrantVolumeName    = "villa-qdrant"

	// qdrantVolumeMount is the durable named-volume mount with the :Z PRIVATE SELinux
	// label (D-03) at Qdrant's data dir. The unprivileged image (USER_ID=1000) writes
	// here; if a dev-box write-permission proof (Plan 19-03) ever fails, the
	// belt-and-suspenders fix is appending `,U` (chown the volume to the container UID)
	// per Pitfall 1 — do NOT add `,U` now.
	qdrantVolumeMount = qdrantVolumeName + ".volume:/qdrant/storage:Z"

	embedContainerUnitName = "villa-embed.container"
	embedContainerName     = "villa-embed"
	// embedContainerPort is the container-internal OpenAI /v1 port (--host 0.0.0.0 on
	// the Exec line, never a host bind — D-05/D-10, SC#4).
	embedContainerPort = 8080
	// embedContextLen is the pinned embed context window (D-07/D-08).
	embedContextLen = 8192
	// embedModelMount binds the SHARED villa-models store read-only with the LOWERCASE
	// :z shared label + ro (matching backend_vulkan.go's shared-models convention —
	// NOT :Z). villa-embed serves the pre-staged GGUF; it can never write the store
	// (T-19-05).
	embedModelMount = "villa-models:/models:ro,z"
	// embedEmbeddingDim is the pinned, LOAD-BEARING embedding dimension (D-08). It is
	// recorded on the rendered service so Phase 23's backup manifest + memory-aware swap
	// guard can detect skew — never Matryoshka-truncated.
	embedEmbeddingDim = 768
)

// QdrantVolumeName returns the podman NAMED-volume identity for the Qdrant storage
// volume (mirrors OpenWebUIVolumeName()) so the Phase-23 backup/restore flow reads the
// resolved volume name WITHOUT re-typing the literal.
func QdrantVolumeName() string { return qdrantVolumeName }

// qdrantView is the data qdrant.container.tmpl renders: no Env, no published host port,
// no Exec (Qdrant runs its image entrypoint with defaults; QDRANT_API_KEY is a Phase-20
// choice). Container-DNS only on villa.network (D-03/D-10, SC#4).
type qdrantView struct {
	ContainerName string
	Image         string
	Network       string
	Volume        string
}

// qdrantVolumeView is the data qdrant.volume.tmpl renders: a plain podman-managed NAMED
// volume (VolumeName + Driver=local), with NO Type=none/Device=/Options=bind fields.
type qdrantVolumeView struct {
	VolumeName string
}

// embedView is the data embed.container.tmpl renders: like qdrantView PLUS a fixed Exec
// line (the embeddings llama-server invocation), with no published host port
// (container-DNS only; --host 0.0.0.0 is container-internal).
type embedView struct {
	ContainerName string
	Image         string
	Network       string
	Volume        string
	Exec          string
}

// buildQdrantView assembles the Qdrant container view from the managed-service consts.
func buildQdrantView() qdrantView {
	return qdrantView{
		ContainerName: qdrantContainerName,
		Image:         qdrantImage,
		Network:       networkAttach,
		Volume:        qdrantVolumeMount,
	}
}

// buildQdrantVolumeView assembles the durable Qdrant named-volume view (D-03).
func buildQdrantVolumeView() qdrantVolumeView {
	return qdrantVolumeView{VolumeName: qdrantVolumeName}
}

// buildEmbedView assembles the villa-embed container view. The ggufFilename is the
// single source of truth (embedGGUFFilename, surfaced via EmbedGGUFFilename()) shared
// with the Plan-19-02 pre-stage Shard.Filename (Pitfall 3) — it is threaded as a
// parameter so both ends bind one symbol.
func buildEmbedView(ggufFilename string) embedView {
	return embedView{
		ContainerName: embedContainerName,
		Image:         embedImage,
		Network:       networkAttach,
		Volume:        embedModelMount,
		Exec:          buildEmbedExec(ggufFilename),
	}
}

// buildEmbedExec assembles the embeddings llama-server Exec from FIXED tokens joined on
// a single space (NO shell interpolation, T-19-02/T-03-01). `--embeddings` (trailing s)
// and `--pooling mean` are LOAD-BEARING: the /v1/embeddings endpoint requires a pooling
// mode != none (Pitfall 2). The ggufFilename is the single source shared with the
// Plan-19-02 pre-stage Shard.Filename (Pitfall 3); render.go passes embedGGUFFilename.
func buildEmbedExec(ggufFilename string) string {
	tokens := []string{
		"llama-server",
		"-m", "/models/" + ggufFilename,
		"--embeddings",
		"--pooling", "mean",
		"-c", strconv.Itoa(embedContextLen),
		"--host", "0.0.0.0", // container-internal only; no host bind (D-05/D-10)
		"--port", strconv.Itoa(embedContainerPort),
	}
	return strings.Join(tokens, " ")
}
