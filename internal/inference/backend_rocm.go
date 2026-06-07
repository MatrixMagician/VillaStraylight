package inference

import (
	"fmt"
	"path/filepath"
)

// backend_rocm.go is the ROCm SIBLING of the Vulkan seam (backend_vulkan.go): it is
// the ONLY file allowed to know the ROCm container image, the /dev/kfd + /dev/dri
// device passthrough, the render group, and the ordered HSA/HIPBLASLT env. The same
// grep gate (seam_test.go) keeps these imperative ROCm literals out of every caller,
// and the positive TestROCmMarkerPresence asserts the ROCm0/HSA/kfd markers stay HERE.
//
// It is a delta over the Vulkan backend (D-09): same mandatory llama-server flags,
// same loopback host-publish, same read-only model bind — only the image, the device
// args, the render group, and the ROCm env differ. ContainerArgs is encoded ONLY in
// Phase 6 (no unit is rendered or run here — render is Phase 7, run is Phase 8).

// The three digest-pinned kyuz0 Strix-Halo ROCm images this seam can select (CLAUDE.md
// prescribed; RESEARCH §Package Legitimacy Audit: same provenance as the audited Vulkan
// image — pin the digest, Pitfall 12 / T-6-04 / T-12-01: the rolling tag is silently
// rebuilt by kyuz0, the @sha256 digest is not). NEVER the ROCm nightlies tag — it carries
// the 64 GB allocation-cap bug that blocks large models (D-07; CLAUDE.md "What NOT to Use").
//
//   - rocmImage724:     the stable ROCm 7.2.4 image — what BackendFor("rocm") still means
//     (D-02 coexistence, byte-unchanged from v1.1). Resolved on the dev box 2026-06-05.
//   - rocmImage644:     the TG-tuned ROCm 6.4.4 image (D-05); re-verified live 2026-06-07.
//   - rocmImage644wmma: the rocWMMA variant of 6.4.4 (D-04/D-05); re-verified 2026-06-07.
//
// Both 6.4.4 digests were re-confirmed read-only via
// `skopeo inspect --no-tags docker://docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4[-rocwmma]`
// immediately before pinning (Plan 12-01 Task 1); the authoritative pre-live-switch
// re-verify is the on-hardware checkpoint in 12-03.
const (
	rocmImage724     = "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89"
	rocmImage644     = "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4@sha256:c81f30a7fd2641e3ea6ac4c45323ba239dca906ed79cc0dfe5b885f9f150ec62"
	rocmImage644wmma = "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4-rocwmma@sha256:9a97129af2c1a2f0080f234787f6978551a43e354f3eb26a8ebc868f643c0141"
)

// backendROCm is the ROCm (HIP) Backend implementation, parameterized by image (D-06):
// the proven 7.2.4 delta — kfd+dri device passthrough, keep-groups, seccomp, loopback
// host-publish, read-only model bind, ordered HSA/HIPBLASLT env, and the ResidencyProof
// markers — is shared ROCm-family behaviour; only the digest (and the reported Name)
// differ across the three selectable backends. It is stateless apart from its identity.
// ROCm is the opt-in performance backend on gfx1151 (Vulkan RADV is the default); each
// variant is selected through BackendFor, so it needs no exported constructor.
type backendROCm struct {
	name  string
	image string
}

// Compile-time assertion that backendROCm satisfies Backend (incl. ResidencyProof).
// The zero value is a valid Backend value for this assertion (identity is set by BackendFor).
var _ Backend = backendROCm{}

// Name identifies the selected backend for provenance/--json (e.g. "rocm", "rocm-6.4.4").
func (b backendROCm) Name() string { return b.name }

// Image returns the digest-pinned ROCm container image for the selected variant.
func (b backendROCm) Image() string { return b.image }

// ContainerArgs renders the full `podman run` argument slice for one ROCm run. It is
// a DELTA over backendVulkan.ContainerArgs (D-09): in addition to the shared /dev/dri
// device, the keep-groups rootless detail, the seccomp minimum, the loopback host
// publish, the read-only model bind, and the mandatory llama-server flags, ROCm adds:
//
//   - "--device", "/dev/kfd"   — the AMD KFD compute device the HIP runtime opens
//     (in addition to the /dev/dri render node both backends need).
//   - ordered env "HSA_OVERRIDE_GFX_VERSION=11.5.1" THEN "ROCBLAS_USE_HIPBLASLT=1" —
//     the gfx1151 HSA override (required for ROCm to target RDNA 3.5) and the hipBLASLt
//     opt-in (the long-context throughput win). Order is preserved deliberately.
//
// The model name is the catalog-resolved file joined onto the container models dir;
// it is passed as a fixed exec arg, never interpolated into a shell string (T-02-08).
func (b backendROCm) ContainerArgs(spec RunSpec) []string {
	hostPublish := fmt.Sprintf("%s:%d:%d", hostPublishAddr, serverPort, serverPort)
	modelBind := fmt.Sprintf("%s:%s:ro,z", spec.ModelsDir, containerModelsDir)
	containerModelPath := filepath.Join(containerModelsDir, spec.ModelFile)

	args := []string{
		"run", "--rm",
		"--name", spec.ContainerName,
		"--device", "/dev/kfd",
		"--device", "/dev/dri",
		// keep-groups ONLY: it propagates the rootless user's supplementary groups
		// (render/video) into the container, which is what grants /dev/kfd + /dev/dri
		// access. podman REFUSES "--group-add keep-groups" combined with any other
		// "--group-add" (exit 125: "the '--group-add keep-groups' option is not allowed
		// with any other --group-add options"). A redundant "--group-add render" here
		// was an on-hardware blocker (Phase 08 UAT CR-G1) — the render GID already
		// arrives via keep-groups, so do NOT re-add it. Matches backend_vulkan.go.
		"--group-add", "keep-groups",
		"--security-opt", "seccomp=unconfined",
		"--env", "HSA_OVERRIDE_GFX_VERSION=11.5.1",
		"--env", "ROCBLAS_USE_HIPBLASLT=1",
		"-p", hostPublish,
		"-v", modelBind,
		b.image,
		"llama-server",
		"-m", containerModelPath,
		"-c", fmt.Sprintf("%d", spec.ContextLen),
		"--host", "0.0.0.0", // container-internal only; host side is loopback (above)
		"--port", fmt.Sprintf("%d", serverPort),
	}
	args = append(args, llamaServerFlags...)
	return args
}

// ResidencyProof returns the ROCm log/journal markers the offload-assert keys on
// (D-04/D-05) — the ROCm analog of the Vulkan descriptor. The parameterized scrapes
// (offload.go, running_offload.go) consume these without re-rolling the offload math:
//   - DeviceToken "ROCm0" — the load_tensors buffer-line device token (per Pitfall 2
//     it must NOT match "ROCm_Host"; the strings.Contains match stays exact enough).
//   - DeviceLabel "- ROCm" — the device_info enumeration prefix.
//   - StartLogDevicePrefix "ggml_cuda_init:" — HIP reuses the cuda-init prefix in the
//     ggml HIP backend (A2, MEDIUM confidence — the kyuz0 ROCm builds emit it).
//   - FaultString "Memory access fault by GPU node" — the HIP/KFD abort marker that
//     VOIDS residency before any buffer-line PASS (Pitfall 4).
//   - RejectSoftwareRenderer false — ROCm has no software-renderer (llvmpipe) ICD
//     analog, so the start-time scrape's software-renderer reject is skipped.
func (b backendROCm) ResidencyProof() ResidencyMarkers {
	return ResidencyMarkers{
		DeviceToken:            "ROCm0",
		DeviceLabel:            "- ROCm",
		StartLogDevicePrefix:   "ggml_cuda_init:",
		FaultString:            "Memory access fault by GPU node",
		RejectSoftwareRenderer: false,
	}
}
