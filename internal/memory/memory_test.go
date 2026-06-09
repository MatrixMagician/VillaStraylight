// memory_test.go holds the table-driven tests for the pure internal/memory
// decision core: Footprint (typed-Unknown embedding footprint, D-02a), Decide
// (fail-closed enablement-and-fields-valid gate, D-02b), and RenderView (the
// resolved-values-only orchestrate handoff, D-02c). Every test asserts the
// honesty-by-construction invariant: a miss is a typed Unknown (Known=false),
// NEVER a bare zero, and the gate refuses-with-reason rather than silently
// defaulting. The package is PURE — these tests do no host I/O.
package memory

import "testing"

// TestFootprint guards D-02a: the pinned embedding model resolves to a Known
// byte reservation with provenance, and ANY miss (unknown id or empty string)
// is a typed Unknown (Known=false) — never a bare-zero sentinel.
func TestFootprint(t *testing.T) {
	const wantBytes = uint64(512) << 20 // 512 MiB conservative reservation (D-08)

	tests := []struct {
		name      string
		modelID   string
		wantKnown bool
		wantValue uint64
	}{
		{
			name:      "pinned embedding model is Known with 512 MiB",
			modelID:   "nomic-embed-text-v1.5",
			wantKnown: true,
			wantValue: wantBytes,
		},
		{
			name:      "unknown model id is typed-Unknown",
			modelID:   "does-not-exist",
			wantKnown: false,
		},
		{
			name:      "empty model id is typed-Unknown (no silent default)",
			modelID:   "",
			wantKnown: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Footprint(tc.modelID)
			if got.Known != tc.wantKnown {
				t.Fatalf("Footprint(%q).Known = %v, want %v", tc.modelID, got.Known, tc.wantKnown)
			}
			if tc.wantKnown {
				if got.Value != tc.wantValue {
					t.Errorf("Footprint(%q).Value = %d, want %d", tc.modelID, got.Value, tc.wantValue)
				}
				if got.Source == "" {
					t.Errorf("Footprint(%q).Source is empty, want non-empty provenance", tc.modelID)
				}
			} else {
				// A typed Unknown must carry a reason, not impersonate a real zero.
				if got.Value != 0 {
					t.Errorf("Footprint(%q) Unknown should have zero Value, got %d", tc.modelID, got.Value)
				}
				if got.Source == "" {
					t.Errorf("Footprint(%q) Unknown should carry a reason in Source", tc.modelID)
				}
			}
		})
	}
}
