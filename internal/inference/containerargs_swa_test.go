package inference

import "testing"

// TestContainerArgsLeaveSWASizingToDefaults guards the assumption under the fit's
// sliding-window term (recommend.swaCells, ADR-0029): llama-server sizes that
// cache from n_parallel, kv_unified and n_ubatch, and the fit assumes the
// defaults it logged on the dev host (four sequences, unified, 512). Rendering
// any flag that moves them, or one that changes how the sliding cache is kept,
// would make the reservation wrong without a test noticing, so this fails the
// build the day a render path adds one.
func TestContainerArgsLeaveSWASizingToDefaults(t *testing.T) {
	forbidden := []string{
		"-np", "--parallel",
		"-ub", "--ubatch-size",
		"-kvu", "--kv-unified", "--no-kv-unified",
		"--swa-full",
		"-ctxcp", "--ctx-checkpoints", "--swa-checkpoints",
	}
	names := []string{"", "rocm", "rocm-10.0", "rocm-7.2.4", "rocm-6.4.4", "rocm-6.4.4-rocwmma", "vulkan"}
	for _, name := range names {
		t.Run("backend="+name, func(t *testing.T) {
			b, err := BackendFor(name)
			if err != nil {
				t.Fatalf("BackendFor(%q): %v", name, err)
			}
			spec := baseSpec()
			spec.Tools = true
			spec.Projector = "vision-mmproj-F16.gguf"
			spec.Speculation = &SpeculationSpec{Mode: "ngram"}
			args := b.ContainerArgs(spec)
			for _, flag := range forbidden {
				if indexOf(args, flag) >= 0 {
					t.Errorf("args carry %s, which moves the sliding-window cache the fit assumes is at llama-server's default: %v", flag, args)
				}
			}
		})
	}
}
