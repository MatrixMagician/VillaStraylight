package inference

import (
	"strings"
	"testing"
)

// TestRunnerInterface asserts the container runner satisfies Runner at compile time
// (no Vulkan/Linux/podman type leaks to callers — INF-03). A var _ assertion also
// lives in runner_podman.go; this makes the contract explicit in the test surface.
func TestRunnerInterface(t *testing.T) {
	var _ Runner = NewContainerRunner(VulkanBackend(), RunSpec{})
	var _ Backend = VulkanBackend()
}

// TestLoopbackPublish is the INF-02 / T-02-07 bind-address assertion: the Vulkan
// backend's rendered podman arg slice must host-publish 127.0.0.1:8080:8080
// (loopback-only) and must NEVER host-publish a 0.0.0.0:-prefixed mapping. The
// container-internal --host 0.0.0.0 is fine (it is not a host publish); the gate is
// specifically on the host side of every -p / --publish mapping.
func TestLoopbackPublish(t *testing.T) {
	args := VulkanBackend().ContainerArgs(RunSpec{
		ContainerName: "villa-inf-test",
		ModelFile:     "qwen2.5-0.5b-instruct-q4_k_m.gguf",
		ModelsDir:     "/home/user/.local/share/villa/models",
		ContextLen:    4096,
	})

	// (a) The loopback host publish must be present.
	const wantPublish = "127.0.0.1:8080:8080"
	if !containsPublish(args, wantPublish) {
		t.Errorf("ContainerArgs: missing loopback host publish %q in %v", wantPublish, args)
	}

	// (b) No host publish may be 0.0.0.0:-prefixed. Scan the value following every
	// -p / --publish flag; the host side is the segment before the first ':'.
	for i := 0; i < len(args)-1; i++ {
		if args[i] != "-p" && args[i] != "--publish" {
			continue
		}
		mapping := args[i+1]
		hostSide := mapping
		if idx := strings.Index(mapping, ":"); idx >= 0 {
			hostSide = mapping[:idx]
		}
		if hostSide == "0.0.0.0" {
			t.Errorf("ContainerArgs: host publish %q binds 0.0.0.0 (INF-02/T-02-07 violation — must be loopback)", mapping)
		}
	}
}

// containsPublish reports whether args contains a -p/--publish flag with the given
// mapping value.
func containsPublish(args []string, mapping string) bool {
	for i := 0; i < len(args)-1; i++ {
		if (args[i] == "-p" || args[i] == "--publish") && args[i+1] == mapping {
			return true
		}
	}
	return false
}

// TestContainerArgsCarryMandatoryFlags is a light guard that the seam renders the
// CLAUDE.md-mandatory Strix Halo flags and the digest-pinned image, so a future
// edit cannot silently drop them.
func TestContainerArgsCarryMandatoryFlags(t *testing.T) {
	args := VulkanBackend().ContainerArgs(RunSpec{
		ContainerName: "c", ModelFile: "m.gguf", ModelsDir: "/d", ContextLen: 8192,
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{"-ngl 999", "-fa 1", "--no-mmap", "-lv 4", "--device /dev/dri", "--group-add keep-groups"} {
		if !strings.Contains(joined, want) {
			t.Errorf("ContainerArgs missing mandatory %q in: %s", want, joined)
		}
	}
	if !strings.Contains(VulkanBackend().Image(), "@sha256:") {
		t.Errorf("image not digest-pinned: %s", VulkanBackend().Image())
	}
}
