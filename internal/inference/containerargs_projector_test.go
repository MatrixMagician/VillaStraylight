package inference

import "testing"

// containerargs_projector_test.go covers the vision projector render delta.
// The invariants:
//   - spec.Projector == "" ⇒ args byte-identical to the base (off-path);
//   - a filename ⇒ exactly --mmproj <container models path> --mmproj-offload;
//   - it composes after the coding and speculation deltas, in that order.

// TestProjectorArgs asserts the off path is byte-identical and a named projector
// appends exactly the three-token delta, joined onto the container's models dir
// rather than the host path.
func TestProjectorArgs(t *testing.T) {
	for name, b := range codingBackends(t) {
		t.Run(name, func(t *testing.T) {
			off := b.ContainerArgs(baseSpec())
			if indexOf(off, "--mmproj") != -1 {
				t.Errorf("[%s] off-path args leaked --mmproj: %v", name, off)
			}

			onSpec := baseSpec()
			onSpec.Projector = "vision-mmproj-F16.gguf"
			on := b.ContainerArgs(onSpec)

			if len(on) != len(off)+3 {
				t.Fatalf("[%s] projector delta is %d tokens, want 3: on=%v off=%v", name, len(on)-len(off), on, off)
			}
			for i := range off {
				if on[i] != off[i] {
					t.Fatalf("[%s] projector path mutated the base args at %d: %q != %q", name, i, on[i], off[i])
				}
			}
			want := []string{"--mmproj", "/models/vision-mmproj-F16.gguf", "--mmproj-offload"}
			for i, w := range want {
				if on[len(off)+i] != w {
					t.Fatalf("[%s] tail delta = %v, want %v", name, on[len(off):], want)
				}
			}
			if got := countTok(on, "-c"); got != 1 {
				t.Errorf("[%s] expected exactly one -c, got %d: %v", name, got, on)
			}
		})
	}
}

// TestProjectorComposesWithCodingAndSpeculation asserts the three optional deltas
// stack in a fixed order — coding, speculation, projector — so a unit that turns
// all three on is the coding unit plus each delta in turn.
func TestProjectorComposesWithCodingAndSpeculation(t *testing.T) {
	for name, b := range codingBackends(t) {
		t.Run(name, func(t *testing.T) {
			coding := &CodingModeSpec{
				Sampling:       &Sampling{Temperature: 0.7, TopP: 0.8, TopK: 20, RepeatPenalty: 1.05},
				CacheReuseSafe: true,
			}
			priorSpec := baseSpec()
			priorSpec.CodingMode = coding
			priorSpec.Speculation = &SpeculationSpec{Mode: "ngram"}
			prior := b.ContainerArgs(priorSpec)

			allSpec := priorSpec
			allSpec.Projector = "vision-mmproj-F16.gguf"
			all := b.ContainerArgs(allSpec)

			if len(all) != len(prior)+3 {
				t.Fatalf("[%s] composed args = %v, want the prior args plus the three-token delta", name, all)
			}
			for i := range prior {
				if all[i] != prior[i] {
					t.Fatalf("[%s] the projector reordered an earlier delta at %d: %q != %q", name, i, all[i], prior[i])
				}
			}
			if all[len(prior)] != "--mmproj" || all[len(prior)+2] != "--mmproj-offload" {
				t.Errorf("[%s] tail delta = %v, want the projector delta last", name, all[len(prior):])
			}
		})
	}
}
