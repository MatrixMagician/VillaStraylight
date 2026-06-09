// memory_test.go holds the table-driven tests for the pure internal/memory
// decision core: Footprint (typed-Unknown embedding footprint, D-02a), Decide
// (fail-closed enablement-and-fields-valid gate, D-02b), and RenderView (the
// resolved-values-only orchestrate handoff, D-02c). Every test asserts the
// honesty-by-construction invariant: a miss is a typed Unknown (Known=false),
// NEVER a bare zero, and the gate refuses-with-reason rather than silently
// defaulting. The package is PURE — these tests do no host I/O.
package memory

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// validMemoryConfig returns a VillaConfig with memory ON and every required
// memory field present and valid — the baseline the field-omission cases mutate.
func validMemoryConfig() config.VillaConfig {
	return config.VillaConfig{
		MemoryEnabled:  true,
		EmbeddingModel: "nomic-embed-text-v1.5",
		EmbeddingDim:   768,
		QdrantAddr:     "villa-qdrant",
		QdrantPort:     6333,
		EmbedAddr:      "villa-embed",
		EmbedPort:      8080,
	}
}

// reasonsMention reports whether any reason string contains the given substring
// (case-insensitive), so a test can assert a field was named without pinning the
// exact prose.
func reasonsMention(reasons []string, want string) bool {
	for _, r := range reasons {
		if strings.Contains(strings.ToLower(r), strings.ToLower(want)) {
			return true
		}
	}
	return false
}

// TestDecide guards D-02b: the fail-closed enablement-and-fields-valid gate.
// Off is a valid state; on+complete is valid; on+any-missing-field is invalid
// with a reason naming that field; multiple missing fields accumulate reasons.
// Decide never panics and does no I/O.
func TestDecide(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*config.VillaConfig)
		wantEnabled bool
		wantValid   bool
		wantReason  string // substring expected in at least one reason (empty = none)
	}{
		{
			name:        "memory off is a valid state with no reasons",
			mutate:      func(c *config.VillaConfig) { c.MemoryEnabled = false },
			wantEnabled: false,
			wantValid:   true,
		},
		{
			name:        "memory on with all fields valid",
			mutate:      func(c *config.VillaConfig) {},
			wantEnabled: true,
			wantValid:   true,
		},
		{
			name:        "on + empty embedding model is invalid",
			mutate:      func(c *config.VillaConfig) { c.EmbeddingModel = "" },
			wantEnabled: true,
			wantValid:   false,
			wantReason:  "embedding_model",
		},
		{
			name:        "on + non-positive embedding dim is invalid",
			mutate:      func(c *config.VillaConfig) { c.EmbeddingDim = 0 },
			wantEnabled: true,
			wantValid:   false,
			wantReason:  "embedding_dim",
		},
		{
			name:        "on + empty qdrant addr is invalid",
			mutate:      func(c *config.VillaConfig) { c.QdrantAddr = "" },
			wantEnabled: true,
			wantValid:   false,
			wantReason:  "qdrant_addr",
		},
		{
			name:        "on + non-positive qdrant port is invalid",
			mutate:      func(c *config.VillaConfig) { c.QdrantPort = 0 },
			wantEnabled: true,
			wantValid:   false,
			wantReason:  "qdrant_port",
		},
		{
			name:        "on + empty embed addr is invalid",
			mutate:      func(c *config.VillaConfig) { c.EmbedAddr = "" },
			wantEnabled: true,
			wantValid:   false,
			wantReason:  "embed_addr",
		},
		{
			name:        "on + non-positive embed port is invalid",
			mutate:      func(c *config.VillaConfig) { c.EmbedPort = 0 },
			wantEnabled: true,
			wantValid:   false,
			wantReason:  "embed_port",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validMemoryConfig()
			tc.mutate(&cfg)
			got := Decide(cfg)
			if got.Enabled != tc.wantEnabled {
				t.Errorf("Decide().Enabled = %v, want %v", got.Enabled, tc.wantEnabled)
			}
			if got.Valid != tc.wantValid {
				t.Errorf("Decide().Valid = %v, want %v (reasons=%v)", got.Valid, tc.wantValid, got.Reasons)
			}
			if tc.wantValid && len(got.Reasons) != 0 {
				t.Errorf("Decide() valid decision should have no reasons, got %v", got.Reasons)
			}
			if tc.wantReason != "" && !reasonsMention(got.Reasons, tc.wantReason) {
				t.Errorf("Decide() reasons %v should name %q", got.Reasons, tc.wantReason)
			}
		})
	}
}

// TestDecideAccumulatesReasons guards the fail-closed "surface ALL problems"
// behavior: multiple missing fields accumulate multiple reasons in one pass.
func TestDecideAccumulatesReasons(t *testing.T) {
	cfg := validMemoryConfig()
	cfg.EmbeddingModel = ""
	cfg.EmbeddingDim = 0
	cfg.QdrantAddr = ""
	got := Decide(cfg)
	if got.Valid {
		t.Fatalf("Decide() with 3 bad fields should be invalid, got valid")
	}
	if len(got.Reasons) < 3 {
		t.Errorf("Decide() should accumulate >=3 reasons, got %d: %v", len(got.Reasons), got.Reasons)
	}
	for _, want := range []string{"embedding_model", "embedding_dim", "qdrant_addr"} {
		if !reasonsMention(got.Reasons, want) {
			t.Errorf("Decide() reasons %v missing %q", got.Reasons, want)
		}
	}
}

// TestRenderView guards D-02c: RenderView maps the cfg memory fields to
// MemoryRenderInput one-for-one — resolved VALUES only (model id, dim, addr/port
// pieces). It carries NO composed URL and NO image literal (orchestrate adds
// those later per D-10).
func TestRenderView(t *testing.T) {
	cfg := validMemoryConfig()
	got := RenderView(cfg)
	if got.EmbeddingModel != cfg.EmbeddingModel {
		t.Errorf("EmbeddingModel = %q, want %q", got.EmbeddingModel, cfg.EmbeddingModel)
	}
	if got.EmbeddingDim != cfg.EmbeddingDim {
		t.Errorf("EmbeddingDim = %d, want %d", got.EmbeddingDim, cfg.EmbeddingDim)
	}
	if got.QdrantAddr != cfg.QdrantAddr {
		t.Errorf("QdrantAddr = %q, want %q", got.QdrantAddr, cfg.QdrantAddr)
	}
	if got.QdrantPort != cfg.QdrantPort {
		t.Errorf("QdrantPort = %d, want %d", got.QdrantPort, cfg.QdrantPort)
	}
	if got.EmbedAddr != cfg.EmbedAddr {
		t.Errorf("EmbedAddr = %q, want %q", got.EmbedAddr, cfg.EmbedAddr)
	}
	if got.EmbedPort != cfg.EmbedPort {
		t.Errorf("EmbedPort = %d, want %d", got.EmbedPort, cfg.EmbedPort)
	}
	// The addrs are container-DNS pieces, never a composed/routable URL.
	for _, v := range []string{got.QdrantAddr, got.EmbedAddr} {
		if strings.Contains(v, "://") || strings.Contains(v, ":") {
			t.Errorf("RenderView addr %q should be a bare container-DNS name, not a URL/host:port", v)
		}
	}
}

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
