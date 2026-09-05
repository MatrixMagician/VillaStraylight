package inference

import (
	"strings"
	"testing"
)

// containerargs_coding_test.go covers the coding-mode render delta appended
// inside ContainerArgs behind the backend seam. The invariants:
// - spec.CodingMode == nil ⇒ args byte-identical to the v1.3 base (off-path);
// - non-nil ⇒ --jinja + the sampling preset + --cache-reuse 256 (gated),
//     appended AFTER llamaServerFlags, with NO second -c (Pitfall 1);
// - CacheReuseSafe == false ⇒ --cache-reuse ABSENT (fail-closed).
// Both backends (Vulkan + ROCm) render the delta — the table runs against each.

// codingBackends is the set of seam backends the delta must be symmetric across.
func codingBackends(t *testing.T) map[string]Backend {
	t.Helper()
	rocm, err := BackendFor("rocm")
	if err != nil {
		t.Fatalf("BackendFor(rocm): %v", err)
	}
	return map[string]Backend{
		"vulkan": VulkanBackend(),
		"rocm":   rocm,
	}
}

// baseSpec is a deterministic off-path RunSpec (CodingMode nil).
func baseSpec() RunSpec {
	return RunSpec{
		ContainerName: "villa-llama",
		ModelFile:     "coder.gguf",
		ModelsDir:     "/models-host",
		ContextLen:    65536,
	}
}

// indexOf returns the first index of tok in args, or -1.
func indexOf(args []string, tok string) int {
	for i, a := range args {
		if a == tok {
			return i
		}
	}
	return -1
}

// countTok counts occurrences of tok in args.
func countTok(args []string, tok string) int {
	n := 0
	for _, a := range args {
		if a == tok {
			n++
		}
	}
	return n
}

// flagValue returns the token following the first occurrence of flag (or "").
func flagValue(args []string, flag string) string {
	i := indexOf(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

// TestCodingModeArgs asserts the off path is byte-identical and the on path appends the
// full delta (jinja + sampling + cache-reuse) with exactly one -c (Pitfall 1).
func TestCodingModeArgs(t *testing.T) {
	for name, b := range codingBackends(t) {
		t.Run(name, func(t *testing.T) {
			// Off path: CodingMode nil ⇒ byte-identical to the base args.
			off := b.ContainerArgs(baseSpec())
			if indexOf(off, "--jinja") != -1 {
				t.Errorf("[%s] off-path args leaked --jinja: %v", name, off)
			}
			if indexOf(off, "--cache-reuse") != -1 {
				t.Errorf("[%s] off-path args leaked --cache-reuse: %v", name, off)
			}

			// On path: full delta, CacheReuseSafe=true.
			onSpec := baseSpec()
			onSpec.CodingMode = &CodingModeSpec{
				Sampling:       &Sampling{Temperature: 0.7, TopP: 0.8, TopK: 20, RepeatPenalty: 1.05},
				CacheReuseSafe: true,
			}
			on := b.ContainerArgs(onSpec)

			// The on path is the off path with the delta appended (off is a prefix).
			if len(on) <= len(off) {
				t.Fatalf("[%s] on-path args not longer than off-path: on=%v off=%v", name, on, off)
			}
			for i := range off {
				if on[i] != off[i] {
					t.Fatalf("[%s] on-path mutated the base args at %d: %q != %q", name, i, on[i], off[i])
				}
			}

			// Exactly one -c (Pitfall 1: the single -c carries the agent ctx).
			if got := countTok(on, "-c"); got != 1 {
				t.Errorf("[%s] expected exactly one -c, got %d: %v", name, got, on)
			}
			if v := flagValue(on, "-c"); v != "65536" {
				t.Errorf("[%s] -c value = %q, want 65536 (the agent ctx carried via ContextLen)", name, v)
			}

			// --jinja present.
			if indexOf(on, "--jinja") < 0 {
				t.Errorf("[%s] on-path missing --jinja: %v", name, on)
			}
			// Sampling flags with correctly formatted values (%g floats, %d int).
			for flag, want := range map[string]string{
				"--temp":           "0.7",
				"--top-p":          "0.8",
				"--top-k":          "20",
				"--repeat-penalty": "1.05",
			} {
				if v := flagValue(on, flag); v != want {
					t.Errorf("[%s] %s = %q, want %q: %v", name, flag, v, want, on)
				}
			}
			// --cache-reuse 256 present when safe.
			if v := flagValue(on, "--cache-reuse"); v != "256" {
				t.Errorf("[%s] --cache-reuse = %q, want 256: %v", name, v, on)
			}
		})
	}
}

// TestCacheReuseGate asserts --cache-reuse is fail-closed (absent when CacheReuseSafe is
// false) and that a nil Sampling omits the sampling flags but keeps --jinja.
func TestCacheReuseGate(t *testing.T) {
	for name, b := range codingBackends(t) {
		t.Run(name, func(t *testing.T) {
			// CacheReuseSafe=false (fail-closed default) ⇒ --cache-reuse ABSENT.
			specNoReuse := baseSpec()
			specNoReuse.CodingMode = &CodingModeSpec{
				Sampling:       &Sampling{Temperature: 0.7, TopP: 0.8, TopK: 20, RepeatPenalty: 1.05},
				CacheReuseSafe: false,
			}
			noReuse := b.ContainerArgs(specNoReuse)
			if indexOf(noReuse, "--cache-reuse") != -1 {
				t.Errorf("[%s] --cache-reuse appended despite CacheReuseSafe=false (D-03 fail-closed): %v", name, noReuse)
			}
			if indexOf(noReuse, "--jinja") < 0 {
				t.Errorf("[%s] --jinja missing with sampling-on/reuse-off: %v", name, noReuse)
			}

			// Sampling=nil ⇒ sampling flags absent, --jinja still present.
			specNoSampling := baseSpec()
			specNoSampling.CodingMode = &CodingModeSpec{Sampling: nil, CacheReuseSafe: true}
			noSampling := b.ContainerArgs(specNoSampling)
			if indexOf(noSampling, "--jinja") < 0 {
				t.Errorf("[%s] --jinja missing when Sampling=nil: %v", name, noSampling)
			}
			for _, flag := range []string{"--temp", "--top-p", "--top-k", "--repeat-penalty"} {
				if indexOf(noSampling, flag) != -1 {
					t.Errorf("[%s] sampling flag %s present despite Sampling=nil: %v", name, flag, noSampling)
				}
			}
			// CacheReuseSafe=true still gates --cache-reuse on (independent of sampling).
			if v := flagValue(noSampling, "--cache-reuse"); v != "256" {
				t.Errorf("[%s] --cache-reuse = %q, want 256 with Sampling=nil/safe=true: %v", name, v, noSampling)
			}
		})
	}
}

// TestSeamGateForbidsCodingFlagsInCmdFixture is a focused assertion that the extended
// seam gate's cmd-tier pattern set forbids --jinja in a cmd/villa-shaped source — the
// real cmd-tier guarantee SC1 requires (the broader gate runs in TestSeamGrepGate).
func TestSeamGateForbidsCodingFlagsInCmdFixture(t *testing.T) {
	fixture := `package main
func leak() []string { return []string{"--jinja", "--cache-reuse", "256"} }
`
	re := seamFlagPattern()
	if !re.MatchString(fixture) {
		t.Errorf("coding-mode flag pattern failed to catch a --jinja/--cache-reuse leak in a cmd-tier fixture: %s", fixture)
	}
	// And it must NOT match a backend-neutral file with no coding flags.
	clean := `package main
func ok() string { return "hello" }
`
	if re.MatchString(clean) {
		t.Errorf("coding-mode flag pattern false-matched a clean file: %s", clean)
	}
	_ = strings.TrimSpace // keep strings import used regardless of build tags
}
