package inference

import (
	"fmt"
	"path/filepath"
)

// This file is the BACKEND SEAM: it is the ONLY file in internal/inference allowed
// to know the container image, the /dev/dri device passthrough, the keep-groups /
// seccomp args, and the loopback host-publish literal. A grep gate (seam_test.go,
// TestSeamGrepGate) asserts these imperative backend tokens appear nowhere else in
// internal/ (outside this package and internal/detect/gpu_amd.go). A future ROCm or
// Metal Backend slots in as a sibling file without reshaping Runner/RunSpec/Verdict.

// vulkanImage is the digest-pinned kyuz0 Strix-Halo Vulkan RADV image (CLAUDE.md
// prescribed; RESEARCH §Package Legitimacy Audit: approved, pin digest — Pitfall 12 /
// T-02-10: the tag is silently rebuilt, the digest is not). Resolved on the dev box
// 2026-06-04 via `podman pull docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv`.
const vulkanImage = "docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555c45864352a4077528836988d448e9f030fbab9f7376ea1c603ac7aad"

// containerModelsDir is the path the host models dir is bind-mounted at, read-only.
const containerModelsDir = "/models"

// loopbackHostPublish is the host-published port mapping: loopback-only, NEVER
// 0.0.0.0 (INF-02 / T-02-07). The container side is 0.0.0.0:8080 (container-internal
// only); only 127.0.0.1 is reachable from the host network. TestLoopbackPublish
// asserts the rendered args contain this and no 0.0.0.0:-prefixed host publish.
const (
	hostPublishAddr = "127.0.0.1"
	serverPort      = 8080
)

// Mandatory Strix Halo llama-server runtime flags (CLAUDE.md "What NOT to Use":
// always --no-mmap -fa 1 -ngl 999). -ngl 999 offloads all layers (free on unified
// memory), -fa 1 is flash-attention (stability + KV memory), --no-mmap keeps
// weights resident in unified memory.
//
// `-lv 4` raises the llama-server log verbosity (F-3 / WR-05): the kyuz0 vulkan-radv
// image suppresses the load-bearing residency lines below its default threshold, so
// at the default verbosity only the `device_info`/`- Vulkan0` enumeration prints and
// the running-server offload assert (cmd/villa status → inference.RunningOffloadVerdict)
// never sees the `load_tensors: Vulkan0 model buffer size = N MiB` / `offloaded N/N`
// proof → it degrades to a typed-Unknown WARN. `-lv 4` is the EMPIRICALLY-determined
// minimal level (verified on-hardware 2026-06-04, gfx1151) that emits those lines at
// info WITHOUT the per-tensor debug flood `-lv 5` pulls in (~1500 create_tensor/
// assigned-to-device lines + a misleading first-pass `0.00 MiB` estimate).
//
// `--metrics` exposes the llama-server Prometheus `/metrics` endpoint (Phase-5 DASH-02
// perf panel). Without it `/metrics` 404s; it is a fixed exec arg (no shell, no
// injection surface) published only on the existing loopback PublishPort (T-05-01:
// no new port/bind). Adding it is a DELIBERATE rendered-unit golden change (Pitfall 2)
// — DISTINCT from the byte-frozen status --json golden, which must NOT change.
var llamaServerFlags = []string{"-ngl", "999", "-fa", "1", "--no-mmap", "-lv", "4", "--metrics"}

// backendVulkan is the Vulkan RADV Backend implementation. It is stateless.
type backendVulkan struct{}

// Compile-time assertion that backendVulkan satisfies Backend.
var _ Backend = backendVulkan{}

// VulkanBackend returns the Vulkan RADV backend (the only Phase-2 backend; ROCm is
// admitted by the interface but not built here).
func VulkanBackend() Backend { return backendVulkan{} }

// Name identifies the backend for provenance/--json.
func (backendVulkan) Name() string { return "vulkan" }

// Image returns the digest-pinned container image.
func (backendVulkan) Image() string { return vulkanImage }

// ContainerArgs renders the full `podman run` argument slice for one run. It is the
// single assembly point for every backend literal: the /dev/dri device passthrough,
// --group-add keep-groups (the load-bearing rootless detail that keeps the host
// render/video GIDs inside the container so renderD128 is accessible), the
// kyuz0-documented seccomp=unconfined minimum (T-02-06), the loopback host publish
// (T-02-07), the read-only model bind (:ro,z — z for the dedicated XDG models dir,
// never :Z on a shared path), and the mandatory llama-server flags.
//
// The model name is the catalog-resolved file joined onto the container models dir;
// it is passed as a fixed exec arg, never interpolated into a shell string (T-02-08).
func (backendVulkan) ContainerArgs(spec RunSpec) []string {
	hostPublish := fmt.Sprintf("%s:%d:%d", hostPublishAddr, serverPort, serverPort)
	modelBind := fmt.Sprintf("%s:%s:ro,z", spec.ModelsDir, containerModelsDir)
	containerModelPath := filepath.Join(containerModelsDir, spec.ModelFile)

	args := []string{
		"run", "--rm",
		"--name", spec.ContainerName,
		"--device", "/dev/dri",
		"--group-add", "keep-groups",
		"--security-opt", "seccomp=unconfined",
		"-p", hostPublish,
		"-v", modelBind,
		vulkanImage,
		"llama-server",
		"-m", containerModelPath,
		"-c", fmt.Sprintf("%d", spec.ContextLen),
		"--host", "0.0.0.0", // container-internal only; host side is loopback (above)
		"--port", fmt.Sprintf("%d", serverPort),
	}
	args = append(args, llamaServerFlags...)
	return args
}

// endpointURL is the loopback base URL the OpenAI-compatible API is published on.
func endpointURL() string {
	return fmt.Sprintf("http://%s:%d", hostPublishAddr, serverPort)
}
