package inference

import (
	"regexp"
	"strings"
	"testing"
)

// backend_rocm_test.go covers the ROCm backend (backend_rocm.go) and the BackendFor
// resolver (backend.go): the kfd+dri/render/ordered-env ContainerArgs delta (D-09),
// the digest-pinned rocm-7.2.4 image (T-6-04), and the fail-closed resolver (D-01/D-02,
// T-6-03). These are Contains-style guards, NOT a byte-golden — the rendered-unit
// byte-golden is Phase 7.

// TestROCmContainerArgs asserts the ROCm ContainerArgs carry the full delta over
// Vulkan: both devices, keep-groups (the render GID arrives via keep-groups — NOT a
// second --group-add, which podman rejects; CR-G1), the ordered HSA/HIPBLASLT env,
// and the shared mandatory llama-server flags.
func TestROCmContainerArgs(t *testing.T) {
	b, err := BackendFor("rocm")
	if err != nil {
		t.Fatalf("BackendFor(rocm): unexpected error %v", err)
	}
	args := b.ContainerArgs(RunSpec{
		ContainerName: "c", ModelFile: "m.gguf", ModelsDir: "/d", ContextLen: 8192,
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--device /dev/kfd",
		"--device /dev/dri",
		"--group-add keep-groups",
		"HSA_OVERRIDE_GFX_VERSION=11.5.1",
		"ROCBLAS_USE_HIPBLASLT=1",
		"-ngl 999",
		"-fa 1",
		"--no-mmap",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("ROCm ContainerArgs missing %q in: %s", want, joined)
		}
	}

	// The HSA override must precede the hipBLASLt opt-in (ordered env, D-09).
	hsaIdx := strings.Index(joined, "HSA_OVERRIDE_GFX_VERSION=11.5.1")
	hipIdx := strings.Index(joined, "ROCBLAS_USE_HIPBLASLT=1")
	if hsaIdx < 0 || hipIdx < 0 || hsaIdx > hipIdx {
		t.Errorf("ROCm env order wrong: HSA at %d, HIPBLASLT at %d (HSA must come first)", hsaIdx, hipIdx)
	}
}

// TestROCmImageDigestPinned asserts the ROCm image is digest-pinned: an `@sha256:`
// prefix followed by a 64-hex-char digest (T-6-04 supply-chain pin). Passes now —
// the digest is the resolved real one, no placeholder.
func TestROCmImageDigestPinned(t *testing.T) {
	b, err := BackendFor("rocm")
	if err != nil {
		t.Fatalf("BackendFor(rocm): unexpected error %v", err)
	}
	img := b.Image()
	if !strings.Contains(img, "@sha256:") {
		t.Fatalf("ROCm image not digest-pinned (no @sha256:): %s", img)
	}
	digestRe := regexp.MustCompile(`@sha256:[0-9a-f]{64}\b`)
	if !digestRe.MatchString(img) {
		t.Fatalf("ROCm image digest is not a 64-hex sha256 pin: %s", img)
	}
}

// TestBackendFor asserts the resolver maps "" and "vulkan" to the Vulkan backend,
// "rocm" to the ROCm 7.2.4 backend (unchanged digest — D-02 coexistence), the two new
// "rocm-6.4.4"/"rocm-6.4.4-rocwmma" keys to their digest-pinned ROCm backends (D-01),
// and FAILS CLOSED on an unknown value: a nil Backend + a non-nil error naming the bad
// value, NEVER a silent Vulkan fallback (D-03, T-6-03 / T-12-03).
func TestBackendFor(t *testing.T) {
	ok := []struct {
		name        string
		wantName    string
		wantImgFrag string // a digest substring the resolved backend's Image() must contain
	}{
		{"", "vulkan", ""},
		{"vulkan", "vulkan", ""},
		// rocm STILL means the unchanged 7.2.4 digest (D-02 coexistence).
		{"rocm", "rocm", "2da150c1"},
		// The two new digest-pinned ROCm backends (D-01/D-05).
		{"rocm-6.4.4", "rocm-6.4.4", "sha256:c81f30a7"},
		{"rocm-6.4.4-rocwmma", "rocm-6.4.4-rocwmma", "sha256:9a97129a"},
	}
	for _, tc := range ok {
		t.Run("resolves "+tc.name, func(t *testing.T) {
			b, err := BackendFor(tc.name)
			if err != nil {
				t.Fatalf("BackendFor(%q): unexpected error %v", tc.name, err)
			}
			if b == nil {
				t.Fatalf("BackendFor(%q): nil backend", tc.name)
			}
			if b.Name() != tc.wantName {
				t.Errorf("BackendFor(%q).Name() = %q, want %q", tc.name, b.Name(), tc.wantName)
			}
			if tc.wantImgFrag != "" && !strings.Contains(b.Image(), tc.wantImgFrag) {
				t.Errorf("BackendFor(%q).Image() = %q, want it to contain %q", tc.name, b.Image(), tc.wantImgFrag)
			}
		})
	}

	// Fail-closed: unknown → (nil, error) whose message names the bad value AND the
	// four valid options (D-03 fail-closed; the widened error must still name them all).
	t.Run("unknown fails closed", func(t *testing.T) {
		b, err := BackendFor("cuda")
		if err == nil {
			t.Fatal("BackendFor(\"cuda\"): want a non-nil error (fail-closed), got nil")
		}
		if b != nil {
			t.Fatalf("BackendFor(\"cuda\"): want a nil Backend on error (never a silent fallback), got %T", b)
		}
		if !strings.Contains(err.Error(), "cuda") {
			t.Errorf("BackendFor(\"cuda\") error %q should name the bad value", err.Error())
		}
		for _, opt := range []string{"vulkan", "rocm", "rocm-6.4.4", "rocm-6.4.4-rocwmma"} {
			if !strings.Contains(err.Error(), opt) {
				t.Errorf("BackendFor(\"cuda\") error %q should name the valid option %q", err.Error(), opt)
			}
		}
	})

	// The new digests stay seam-locked AND digest-pinned (@sha256: + 64 hex).
	t.Run("new backends digest-pinned", func(t *testing.T) {
		digestRe := regexp.MustCompile(`@sha256:[0-9a-f]{64}\b`)
		for _, name := range []string{"rocm-6.4.4", "rocm-6.4.4-rocwmma"} {
			b, err := BackendFor(name)
			if err != nil {
				t.Fatalf("BackendFor(%q): unexpected error %v", name, err)
			}
			if !digestRe.MatchString(b.Image()) {
				t.Errorf("BackendFor(%q).Image() not digest-pinned: %s", name, b.Image())
			}
		}
	})
}
