package inference

import (
	"reflect"
	"strings"
	"testing"
)

// TestNewContainerRunnerReturnsTheInterface guards the promise that no concrete
// podman/Vulkan type reaches a caller. The compile-time `var _ Runner` in
// runner_podman.go asserts the other half, that containerRunner satisfies Runner,
// and would keep passing if this constructor went back to returning the concrete
// pointer. Only the declared return type stops the leak, so that is what is checked.
func TestNewContainerRunnerReturnsTheInterface(t *testing.T) {
	out := reflect.TypeOf(NewContainerRunner).Out(0)
	if out.Kind() != reflect.Interface {
		t.Fatalf("NewContainerRunner returns %s (kind %s); it must return the Runner interface so no concrete type leaks to callers", out, out.Kind())
	}
	if out != reflect.TypeFor[Runner]() {
		t.Fatalf("NewContainerRunner returns interface %s, want Runner", out)
	}
}

// TestLoopbackPublish is the bind-address assertion: the Vulkan
// backend's rendered podman arg slice must host-publish 127.0.0.1:8080:8080
// (loopback-only) and must NEVER host-publish a 0.0.0.0:-prefixed mapping. The
// container-internal --host 0.0.0.0 is fine (it is not a host publish); the gate is
// specifically on the host side of every -p / --publish mapping.
func TestLoopbackPublish(t *testing.T) {
	args := VulkanBackend().ContainerArgs(RunSpec{
		ContainerName: "villa-inf-test",
		ModelFile:     "qwen3.5-0.8b-instruct-q4_k_m.gguf",
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
	for i := range len(args) - 1 {
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
	for i := range len(args) - 1 {
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
	for _, want := range []string{"-ngl 999", "-fa 1", "--load-mode none", "-lv 4", "--device /dev/dri", "--group-add keep-groups"} {
		if !strings.Contains(joined, want) {
			t.Errorf("ContainerArgs missing mandatory %q in: %s", want, joined)
		}
	}
	if !strings.Contains(VulkanBackend().Image(), "@sha256:") {
		t.Errorf("image not digest-pinned: %s", VulkanBackend().Image())
	}
}

// TestContainerArgsSecretEnvFile is the GHSA-qxg9 (ADR-0011) transient-container
// half: an empty SecretEnvFile (the pre-existing default, and an unmigrated host
// with no secret yet) renders byte-identical args with no --env-file at all; a
// non-empty one appends `--env-file <path>` — the bearer reaches llama-server
// via the SAME file every rendered Quadlet unit's EnvironmentFile= carries,
// never as a value on the podman command line.
func TestContainerArgsSecretEnvFile(t *testing.T) {
	base := RunSpec{ContainerName: "c", ModelFile: "m.gguf", ModelsDir: "/d", ContextLen: 8192}

	withSecret := base
	withSecret.SecretEnvFile = "/home/user/.config/villa/inference/llama.env"

	for _, backend := range []Backend{VulkanBackend(), backendROCm{name: "rocm", image: "img"}} {
		noSecretArgs := backend.ContainerArgs(base)
		if strings.Contains(strings.Join(noSecretArgs, " "), "--env-file") {
			t.Errorf("%s: empty SecretEnvFile must render no --env-file, got %v", backend.Name(), noSecretArgs)
		}

		withSecretArgs := backend.ContainerArgs(withSecret)
		if !hasFlagValue(withSecretArgs, "--env-file", withSecret.SecretEnvFile) {
			t.Errorf("%s: SecretEnvFile set must render --env-file %q, got %v", backend.Name(), withSecret.SecretEnvFile, withSecretArgs)
		}
		joined := strings.Join(withSecretArgs, " ")
		if strings.Contains(joined, "LLAMA_API_KEY") || strings.Contains(joined, "OPENAI_API_KEY") {
			t.Errorf("%s: the secret VALUE must never reach the podman command line, got %v", backend.Name(), withSecretArgs)
		}
	}
}

// hasFlagValue reports whether args contains flag immediately followed by value.
func hasFlagValue(args []string, flag, value string) bool {
	for i := range len(args) - 1 {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
