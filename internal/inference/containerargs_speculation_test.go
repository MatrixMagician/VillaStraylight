package inference

import "testing"

// containerargs_speculation_test.go covers the speculation render delta appended
// inside ContainerArgs behind the backend seam. The invariants:
//   - spec.Speculation == nil ⇒ args byte-identical to the base (off-path);
//   - Mode "ngram" ⇒ exactly --spec-type ngram-mod, appended last;
//   - an unrecognised mode ⇒ unchanged (the config boundary already refused it);
//   - coding mode and speculation compose, coding delta first.

// TestSpeculationArgs asserts the off path is byte-identical and the ngram path
// appends exactly the two-token delta with no second -c.
func TestSpeculationArgs(t *testing.T) {
	for name, b := range codingBackends(t) {
		t.Run(name, func(t *testing.T) {
			off := b.ContainerArgs(baseSpec())
			if indexOf(off, "--spec-type") != -1 {
				t.Errorf("[%s] off-path args leaked --spec-type: %v", name, off)
			}

			onSpec := baseSpec()
			onSpec.Speculation = &SpeculationSpec{Mode: "ngram"}
			on := b.ContainerArgs(onSpec)

			if len(on) != len(off)+2 {
				t.Fatalf("[%s] ngram delta is %d tokens, want 2: on=%v off=%v", name, len(on)-len(off), on, off)
			}
			for i := range off {
				if on[i] != off[i] {
					t.Fatalf("[%s] ngram path mutated the base args at %d: %q != %q", name, i, on[i], off[i])
				}
			}
			if on[len(off)] != "--spec-type" || on[len(off)+1] != "ngram-mod" {
				t.Errorf("[%s] tail delta = %v, want [--spec-type ngram-mod]", name, on[len(off):])
			}
			if got := countTok(on, "-c"); got != 1 {
				t.Errorf("[%s] expected exactly one -c, got %d: %v", name, got, on)
			}
		})
	}
}

// TestSpeculationUnknownModeRendersNothing asserts a mode the seam does not know
// renders no flag rather than a guessed one. The config boundary is what refuses
// an unknown mode; the seam simply has nothing to emit for it.
func TestSpeculationUnknownModeRendersNothing(t *testing.T) {
	for name, b := range codingBackends(t) {
		t.Run(name, func(t *testing.T) {
			off := b.ContainerArgs(baseSpec())
			for _, mode := range []string{"", "off", "draft"} {
				spec := baseSpec()
				spec.Speculation = &SpeculationSpec{Mode: mode}
				got := b.ContainerArgs(spec)
				if len(got) != len(off) {
					t.Errorf("[%s] mode %q rendered a delta: %v", name, mode, got[len(off):])
				}
			}
		})
	}
}

// TestSpeculationComposesWithCodingMode asserts the two optional deltas stack in a
// fixed order, coding first, so a coding-mode unit with speculation on is the
// coding-mode unit plus two tokens.
func TestSpeculationComposesWithCodingMode(t *testing.T) {
	for name, b := range codingBackends(t) {
		t.Run(name, func(t *testing.T) {
			coding := &CodingModeSpec{
				Sampling:       &Sampling{Temperature: 0.7, TopP: 0.8, TopK: 20, RepeatPenalty: 1.05},
				CacheReuseSafe: true,
			}
			codingSpec := baseSpec()
			codingSpec.CodingMode = coding
			codingOnly := b.ContainerArgs(codingSpec)

			bothSpec := baseSpec()
			bothSpec.CodingMode = coding
			bothSpec.Speculation = &SpeculationSpec{Mode: "ngram"}
			both := b.ContainerArgs(bothSpec)

			if len(both) != len(codingOnly)+2 {
				t.Fatalf("[%s] composed args = %v, want coding args plus the two-token delta", name, both)
			}
			for i := range codingOnly {
				if both[i] != codingOnly[i] {
					t.Fatalf("[%s] speculation reordered the coding delta at %d: %q != %q", name, i, both[i], codingOnly[i])
				}
			}
			if both[len(codingOnly)] != "--spec-type" || both[len(codingOnly)+1] != "ngram-mod" {
				t.Errorf("[%s] tail delta = %v, want [--spec-type ngram-mod]", name, both[len(codingOnly):])
			}
			if got := countTok(both, "-c"); got != 1 {
				t.Errorf("[%s] expected exactly one -c, got %d: %v", name, got, both)
			}
		})
	}
}
