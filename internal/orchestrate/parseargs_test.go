package orchestrate

import (
	"reflect"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// fixtureSpec is the deterministic RunSpec the parser tests source ContainerArgs from.
// It mirrors fixtureInput() so the parser sees the same flags the goldens freeze.
func fixtureSpec() inference.RunSpec {
	return inference.RunSpec{
		ContainerName: containerName,
		ModelFile:     "qwen3-35b-a3b-moe-64.gguf",
		ModelsDir:     "/home/villa/.local/share/villa/models",
		ContextLen:    131072,
	}
}

// TestParseContainerArgsMultiValue proves parseContainerArgs collects ALL --device,
// ALL --group-add, and ALL --env tokens from the seam (none silently dropped, order
// preserved) for the ROCm backend, and that the Vulkan single-device/zero-env path
// still parses and passes the defensive all-fields check (RESEARCH Pitfall 1).
func TestParseContainerArgsMultiValue(t *testing.T) {
	rocm, err := inference.BackendFor("rocm")
	if err != nil {
		t.Fatalf("BackendFor(rocm): %v", err)
	}
	cv, err := parseContainerArgs(rocm.Image(), rocm.ContainerArgs(fixtureSpec()))
	if err != nil {
		t.Fatalf("parseContainerArgs(rocm): %v", err)
	}

	wantDev := []string{"/dev/kfd", "/dev/dri"}
	if !reflect.DeepEqual(cv.AddDevice, wantDev) {
		t.Errorf("AddDevice = %v, want %v (both devices, order preserved)", cv.AddDevice, wantDev)
	}
	// keep-groups ONLY (CR-G1): podman rejects keep-groups combined with any other
	// --group-add, so the ROCm seam emits a single group. The parser's multi-value
	// collection (no silent-drop) is still exercised by the two --device and two --env
	// tokens above.
	wantGroup := []string{"keep-groups"}
	if !reflect.DeepEqual(cv.GroupAdd, wantGroup) {
		t.Errorf("GroupAdd = %v, want %v (keep-groups only — render arrives via keep-groups)", cv.GroupAdd, wantGroup)
	}
	wantEnv := []envPair{
		{Key: "HSA_OVERRIDE_GFX_VERSION", Value: "11.5.1"},
		{Key: "ROCBLAS_USE_HIPBLASLT", Value: "1"},
	}
	if !reflect.DeepEqual(cv.Env, wantEnv) {
		t.Errorf("Env = %v, want %v (both env pairs, first-= split, order preserved)", cv.Env, wantEnv)
	}

	// Vulkan: single device, ZERO env, still passes the defensive check.
	vk := inference.VulkanBackend()
	cvv, err := parseContainerArgs(vk.Image(), vk.ContainerArgs(fixtureSpec()))
	if err != nil {
		t.Fatalf("parseContainerArgs(vulkan): %v (Vulkan zero-env must still pass the defensive check)", err)
	}
	wantVkDev := []string{"/dev/dri"}
	if !reflect.DeepEqual(cvv.AddDevice, wantVkDev) {
		t.Errorf("Vulkan AddDevice = %v, want %v (single element)", cvv.AddDevice, wantVkDev)
	}
	if len(cvv.Env) != 0 {
		t.Errorf("Vulkan Env = %v, want empty (no --env case fires for Vulkan)", cvv.Env)
	}
}
