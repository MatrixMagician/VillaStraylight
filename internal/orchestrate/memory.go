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
// rootless-Podman UID / SELinux :Z storage mount stays writable — SC#2). This is the
// manifest-list digest of docker.io/qdrant/qdrant:v1.18.2-unprivileged, ON-HARDWARE
// VERIFIED during Plan 19-03: the dev-box RepoDigest (resolved via `podman pull` +
// `podman image inspect --format '{{index .RepoDigests 0}}'`) matches this digest, and a
// /v1 readiness curl confirmed the running container. The :v1.18.2-unprivileged tag is
// silently rebuilt; the digest is not (reproducibility).
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

	// qdrantVolumeName is the podman NAMED-volume identity (the volume unit name +
	// mount source). The Qdrant CONTAINER-DNS name is NOT a const here — it is
	// config-resolved (cfg.QdrantAddr via memory.RenderView) and threaded into
	// buildQdrantView (WR-01), so config is the single source of truth for the
	// service's network identity.
	qdrantVolumeName = "villa-qdrant"

	// qdrantVolumeMount is the durable named-volume mount with the :Z PRIVATE SELinux
	// label (D-03) at Qdrant's data dir. The unprivileged image (USER_ID=1000) writes
	// here; if a dev-box write-permission proof (Plan 19-03) ever fails, the
	// belt-and-suspenders fix is appending `,U` (chown the volume to the container UID)
	// per Pitfall 1 — do NOT add `,U` now.
	qdrantVolumeMount = qdrantVolumeName + ".volume:/qdrant/storage:Z"

	embedContainerUnitName = "villa-embed.container"
	// The villa-embed CONTAINER-DNS name and its served /v1 --port are NOT consts
	// here — both are config-resolved (cfg.EmbedAddr / cfg.EmbedPort via
	// memory.RenderView) and threaded into buildEmbedView/buildEmbedExec (WR-01), so
	// config is the single source of truth for the embed service's identity AND the
	// port the readiness proof probes. --host 0.0.0.0 stays container-internal only,
	// never a host bind (D-05/D-10, SC#4).
	//
	// embedContextLen is a genuine pinned const (8192, D-07/D-08): there is no
	// config field for the embed context window, so it stays a render-time constant.
	embedContextLen = 8192
	// embedModelMount binds the SHARED villa-models store read-only with the LOWERCASE
	// :z shared label + ro (matching backend_vulkan.go's shared-models convention —
	// NOT :Z). villa-embed serves the pre-staged GGUF; it can never write the store
	// (T-19-05).
	embedModelMount = "villa-models:/models:ro,z"
	// The pinned, LOAD-BEARING embedding dimension (768, D-08) is single-sourced in
	// config.VillaConfig.EmbeddingDim (self-healed default) and carried to downstream
	// consumers via memory.RenderView.EmbeddingDim — never duplicated as a literal here
	// and never Matryoshka-truncated. Phase 23's backup manifest / swap guard reads it
	// from config (the single source of truth), so no orchestrate-local copy is kept.
)

// QdrantVolumeName returns the podman NAMED-volume identity for the Qdrant storage
// volume (mirrors OpenWebUIVolumeName()) so the Phase-23 backup/restore flow reads the
// resolved volume name WITHOUT re-typing the literal.
func QdrantVolumeName() string { return qdrantVolumeName }

// QdrantContainerUnitName / EmbedContainerUnitName return the memory .container unit
// filenames Render appends when memory is enabled. They are EXPORTED so the install flow
// can assert these units are actually present in the written plan BEFORE starting their
// services (WR-04) — gating the memory-service starts on the rendered units, not solely
// on the config flag, so a memory-on install whose units are absent from the plan fails
// closed with a clear message instead of a raw systemd "Unit not found".
func QdrantContainerUnitName() string { return qdrantContainerUnitName }

// EmbedContainerUnitName — see QdrantContainerUnitName.
func EmbedContainerUnitName() string { return embedContainerUnitName }

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

// buildQdrantView assembles the Qdrant container view. ContainerName is the
// config-resolved Qdrant container-DNS name (qdrantAddr, threaded from
// memory.RenderView(in.Cfg) — the single source of truth) so the rendered unit's
// identity derives from config, NEVER an orchestrate-local const (WR-01). The
// image and the :Z volume mount stay genuine pinned managed-service constants:
// there is no config field for them and the :Z SELinux label is a deliberate
// render-time decision (D-03), not a config value.
func buildQdrantView(qdrantAddr string) qdrantView {
	return qdrantView{
		ContainerName: qdrantAddr,
		Image:         qdrantImage,
		Network:       networkAttach,
		Volume:        qdrantVolumeMount,
	}
}

// buildQdrantVolumeView assembles the durable Qdrant named-volume view (D-03).
func buildQdrantVolumeView() qdrantVolumeView {
	return qdrantVolumeView{VolumeName: qdrantVolumeName}
}

// buildEmbedView assembles the villa-embed container view. ContainerName is the
// config-resolved villa-embed container-DNS name (embedAddr) and the served Exec's
// --port is the config-resolved embedPort — both threaded from
// memory.RenderView(in.Cfg) so the unit's identity/port derive from config, the
// single source of truth (WR-01). The ggufFilename is the single source of truth
// (embedGGUFFilename, surfaced via EmbedGGUFFilename()) shared with the Plan-19-02
// pre-stage Shard.Filename (Pitfall 3) — it is threaded as a parameter so both
// ends bind one symbol. The image and the :ro,z shared-models mount stay genuine
// pinned managed-service constants (no config field for them).
func buildEmbedView(ggufFilename, embedAddr string, embedPort int) embedView {
	return embedView{
		ContainerName: embedAddr,
		Image:         embedImage,
		Network:       networkAttach,
		Volume:        embedModelMount,
		Exec:          buildEmbedExec(ggufFilename, embedPort),
	}
}

// buildEmbedExec assembles the embeddings llama-server Exec from FIXED tokens joined on
// a single space (NO shell interpolation, T-19-02/T-03-01). `--embeddings` (trailing s)
// and `--pooling mean` are LOAD-BEARING: the /v1/embeddings endpoint requires a pooling
// mode != none (Pitfall 2). The ggufFilename is the single source shared with the
// Plan-19-02 pre-stage Shard.Filename (Pitfall 3); render.go passes embedGGUFFilename.
// embedPort is the config-resolved villa-embed /v1 port (cfg.EmbedPort via RenderView)
// so the served --port matches the port the proof probes (WR-01). embedContextLen
// stays a genuine pinned const (8192, D-07/D-08 — no config field for it).
func buildEmbedExec(ggufFilename string, embedPort int) string {
	tokens := []string{
		"llama-server",
		"-m", "/models/" + ggufFilename,
		"--embeddings",
		"--pooling", "mean",
		"-c", strconv.Itoa(embedContextLen),
		"--host", "0.0.0.0", // container-internal only; no host bind (D-05/D-10)
		"--port", strconv.Itoa(embedPort),
	}
	return strings.Join(tokens, " ")
}
