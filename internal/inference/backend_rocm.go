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

// rocmImage is the digest-pinned kyuz0 Strix-Halo ROCm 7.2.4 image (CLAUDE.md
// prescribed; RESEARCH §Package Legitimacy Audit: same provenance as the audited
// Vulkan image, pin the digest — Pitfall 12 / T-6-04: the tag is silently rebuilt,
// the digest is not). NEVER the ROCm nightlies tag — it carries the 64 GB
// allocation-cap bug that blocks large models (D-08; CLAUDE.md "What NOT to Use").
// Resolved on the dev box 2026-06-05 via `skopeo inspect
// docker://docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4`.
const rocmImage = "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89"

// backendROCm is the ROCm 7.2.4 (HIP) Backend implementation. It is stateless. ROCm
// is the opt-in performance backend on gfx1151 (Vulkan RADV is the default); it is
// selected through BackendFor("rocm"), so it needs no exported constructor.
type backendROCm struct{}

// Compile-time assertion that backendROCm satisfies Backend (incl. ResidencyProof).
var _ Backend = backendROCm{}

// Name identifies the backend for provenance/--json.
func (backendROCm) Name() string { return "rocm" }

// Image returns the digest-pinned ROCm 7.2.4 container image.
func (backendROCm) Image() string { return rocmImage }

// ContainerArgs renders the full `podman run` argument slice for one ROCm run. It is
// a DELTA over backendVulkan.ContainerArgs (D-09): in addition to the shared /dev/dri
// device, the keep-groups rootless detail, the seccomp minimum, the loopback host
// publish, the read-only model bind, and the mandatory llama-server flags, ROCm adds:
//
//   - "--device", "/dev/kfd"   — the AMD KFD compute device the HIP runtime opens
//     (in addition to the /dev/dri render node both backends need).
//   - "--group-add", "render"  — the host `render` GID must be inside the container so
//     /dev/kfd + /dev/dri are accessible (A3: STACK.md prescribes the render group;
//     PITFALLS.md notes some hosts also need `video` — the byte-golden of the exact
//     group set is Phase 7's rendered-unit concern, not this encode-only phase).
//   - ordered env "HSA_OVERRIDE_GFX_VERSION=11.5.1" THEN "ROCBLAS_USE_HIPBLASLT=1" —
//     the gfx1151 HSA override (required for ROCm to target RDNA 3.5) and the hipBLASLt
//     opt-in (the long-context throughput win). Order is preserved deliberately.
//
// The model name is the catalog-resolved file joined onto the container models dir;
// it is passed as a fixed exec arg, never interpolated into a shell string (T-02-08).
func (backendROCm) ContainerArgs(spec RunSpec) []string {
	hostPublish := fmt.Sprintf("%s:%d:%d", hostPublishAddr, serverPort, serverPort)
	modelBind := fmt.Sprintf("%s:%s:ro,z", spec.ModelsDir, containerModelsDir)
	containerModelPath := filepath.Join(containerModelsDir, spec.ModelFile)

	args := []string{
		"run", "--rm",
		"--name", spec.ContainerName,
		"--device", "/dev/kfd",
		"--device", "/dev/dri",
		"--group-add", "keep-groups",
		"--group-add", "render",
		"--security-opt", "seccomp=unconfined",
		"--env", "HSA_OVERRIDE_GFX_VERSION=11.5.1",
		"--env", "ROCBLAS_USE_HIPBLASLT=1",
		"-p", hostPublish,
		"-v", modelBind,
		rocmImage,
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
func (backendROCm) ResidencyProof() ResidencyMarkers {
	return ResidencyMarkers{
		DeviceToken:            "ROCm0",
		DeviceLabel:            "- ROCm",
		StartLogDevicePrefix:   "ggml_cuda_init:",
		FaultString:            "Memory access fault by GPU node",
		RejectSoftwareRenderer: false,
	}
}
