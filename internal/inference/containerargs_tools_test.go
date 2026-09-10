package inference

import "testing"

// containerargs_tools_test.go covers RunSpec.Tools, the ONE place --jinja is
// emitted from. The invariants:
//   - Tools false and CodingMode nil ⇒ no --jinja (the byte-identical off path);
//   - Tools true alone ⇒ --jinja and nothing else (tools mode is not a model swap,
//     so it must not drag the coder's sampling preset or --cache-reuse in);
//   - CodingMode non-nil ⇒ --jinja regardless of Tools, plus the coding delta,
//     because coding mode implies tools mode and no second boolean may drift;
//   - both backends render it identically.

// TestToolsArgs is the ToolsOn truth table at the seam, per backend.
func TestToolsArgs(t *testing.T) {
	cases := []struct {
		name       string
		tools      bool
		coding     *CodingModeSpec
		wantJinja  bool
		wantCoding bool
	}{
		{name: "neither", tools: false, coding: nil, wantJinja: false},
		{name: "tools only", tools: true, coding: nil, wantJinja: true},
		{
			name:       "coding only",
			coding:     &CodingModeSpec{Sampling: &Sampling{Temperature: 0.7, TopP: 0.8, TopK: 20, RepeatPenalty: 1.05}, CacheReuseSafe: true},
			wantJinja:  true,
			wantCoding: true,
		},
		{
			name:       "both",
			tools:      true,
			coding:     &CodingModeSpec{Sampling: &Sampling{Temperature: 0.7, TopP: 0.8, TopK: 20, RepeatPenalty: 1.05}, CacheReuseSafe: true},
			wantJinja:  true,
			wantCoding: true,
		},
	}
	for backendName, b := range codingBackends(t) {
		for _, tc := range cases {
			t.Run(backendName+"/"+tc.name, func(t *testing.T) {
				spec := baseSpec()
				spec.Tools = tc.tools
				spec.CodingMode = tc.coding
				args := b.ContainerArgs(spec)

				if got := indexOf(args, "--jinja") >= 0; got != tc.wantJinja {
					t.Errorf("--jinja present = %v, want %v: %v", got, tc.wantJinja, args)
				}
				if n := countTok(args, "--jinja"); n > 1 {
					t.Errorf("--jinja emitted %d times, want at most one: %v", n, args)
				}
				if got := indexOf(args, "--cache-reuse") >= 0; got != tc.wantCoding {
					t.Errorf("--cache-reuse present = %v, want %v: %v", got, tc.wantCoding, args)
				}
				if got := indexOf(args, "--temp") >= 0; got != tc.wantCoding {
					t.Errorf("--temp present = %v, want %v: %v", got, tc.wantCoding, args)
				}
				if n := countTok(args, "-c"); n != 1 {
					t.Errorf("-c emitted %d times, want exactly one: %v", n, args)
				}
			})
		}
	}
}
