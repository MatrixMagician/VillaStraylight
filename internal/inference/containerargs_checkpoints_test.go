package inference

import (
	"reflect"
	"testing"
)

// TestCtxCheckpointsArgs guards the one sliding-window flag villa renders
// (ADR-0034): a spec for a model with sliding-window layers ends in
// --ctx-checkpoints 1, after every other delta, and a spec without them renders
// no such flag, so every unit for a model without sliding-window layers stays
// byte-identical.
func TestCtxCheckpointsArgs(t *testing.T) {
	for name, b := range codingBackends(t) {
		t.Run(name, func(t *testing.T) {
			full := baseSpec()
			full.Tools = true
			full.Speculation = &SpeculationSpec{Mode: "ngram"}
			full.Projector = "vision-mmproj-F16.gguf"
			off := b.ContainerArgs(full)
			if indexOf(off, "--ctx-checkpoints") != -1 {
				t.Errorf("args without sliding-window layers carry --ctx-checkpoints: %v", off)
			}

			full.SlidingWindow = true
			on := b.ContainerArgs(full)
			want := append(append([]string{}, off...), "--ctx-checkpoints", "1")
			if !reflect.DeepEqual(on, want) {
				t.Errorf("args =\n%v\nwant\n%v", on, want)
			}
		})
	}
}
