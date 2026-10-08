package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// swaEntryJSON is a schema-5 entry whose swa block is spliced in per case, so each
// guard is exercised by one differing block and nothing else.
const swaEntryJSON = `{
  "schema_version": 5,
  "catalog_version": "test.swa",
  "models": [
    {
      "id": "windowed-model",
      "display_name": "Windowed Model",
      "quant": "Q4_K_M",
      "weight_bytes": 5000000000,
      "n_layers": 10,
      "n_kv_heads": 4,
      "head_dim": 512,
      "kv_bytes_per_elem": 2,
      "default_ctx": 16384,
      "min_envelope_bytes": 7000000000,
      "tier_gb": 16,
      "unified_memory_safe": true,
      "backend_default": "rocm",
      "bootstrap": false,
      "swa": %s
    }
  ]
}`

func writeSWACatalog(t *testing.T, block string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.json")
	body := strings.Replace(swaEntryJSON, "%s", block, 1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// TestLoadSlidingWindowExternal guards the promise that a complete swa block
// decodes into the SlidingWindow the fit reads, and that an entry without one
// decodes to nil, which every downstream reader takes as "reserve nothing".
func TestLoadSlidingWindowExternal(t *testing.T) {
	path := writeSWACatalog(t, `{"n_layers": 50, "n_kv_heads": 16, "head_dim": 256, "window": 1024}`)
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	m, ok := c.FindByID("windowed-model")
	if !ok {
		t.Fatalf("windowed-model not found (got %d models)", len(c.Models))
	}
	want := SlidingWindow{NLayers: 50, NKVHeads: 16, HeadDim: 256, Window: 1024}
	if m.SWA == nil || *m.SWA != want {
		t.Errorf("SWA = %+v, want %+v", m.SWA, want)
	}

	plain, _, err := Load(filepath.Join("testdata", "good-catalog.json"))
	if err != nil {
		t.Fatalf("Load(good): %v", err)
	}
	for _, p := range plain.Models {
		if p.SWA != nil {
			t.Errorf("entry %s with no swa key decoded as %+v, want nil", p.ID, p.SWA)
		}
	}
}

// TestLoadSlidingWindowValidationRefusesNonPositive guards the promise that a
// present swa block with any field at or below zero invalidates the WHOLE external
// catalog: a zero would collapse the sliding-window term to nothing and admit a
// model the fit should have refused.
func TestLoadSlidingWindowValidationRefusesNonPositive(t *testing.T) {
	cases := map[string]string{
		"zero n_layers":    `{"n_layers": 0, "n_kv_heads": 16, "head_dim": 256, "window": 1024}`,
		"zero n_kv_heads":  `{"n_layers": 50, "n_kv_heads": 0, "head_dim": 256, "window": 1024}`,
		"zero head_dim":    `{"n_layers": 50, "n_kv_heads": 16, "head_dim": 0, "window": 1024}`,
		"zero window":      `{"n_layers": 50, "n_kv_heads": 16, "head_dim": 256, "window": 0}`,
		"negative window":  `{"n_layers": 50, "n_kv_heads": 16, "head_dim": 256, "window": -1}`,
		"all fields empty": `{}`,
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			c, warnings, err := Load(writeSWACatalog(t, block))
			if err != nil {
				t.Fatalf("Load: unexpected error: %v", err)
			}
			if !strings.Contains(strings.Join(warnings, " "), "windowed-model") {
				t.Errorf("refusal should name the offending entry, got %v", warnings)
			}
			if _, ok := c.FindByID("windowed-model"); ok {
				t.Errorf("an entry with an invalid swa block must not be used")
			}
			if _, ok := c.FindByID("qwen3.5-2b"); !ok {
				t.Errorf("expected fallback to the embedded seed")
			}
		})
	}
}

// TestLoadSchema4ExternalIsOlder guards the promise that the schema bump for the
// swa block is a fail-closed window: a schema-4 external catalog warns that it is
// older than this binary supports and the embedded seed is used instead.
func TestLoadSchema4ExternalIsOlder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	body := strings.Replace(swaEntryJSON, `"schema_version": 5`, `"schema_version": 4`, 1)
	body = strings.Replace(body, "%s", "null", 1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(strings.Join(warnings, " "), "older than this binary supports (5)") {
		t.Errorf("warnings = %v, want the older-than-supported schema warning", warnings)
	}
	if _, ok := c.FindByID("windowed-model"); ok {
		t.Errorf("a schema-4 external catalog must not be used")
	}
}

// TestSeedGemmaCarriesSlidingWindowBlock guards the promise that the one seed
// entry whose header carries a sliding_window_pattern also carries the swa block
// that reserves its cache, and that no other seed entry claims one.
func TestSeedGemmaCarriesSlidingWindowBlock(t *testing.T) {
	c, _, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := SlidingWindow{NLayers: 50, NKVHeads: 16, HeadDim: 256, Window: 1024}
	for _, m := range c.Models {
		switch {
		case m.ID == "gemma-4-31b":
			if m.SWA == nil || *m.SWA != want {
				t.Errorf("gemma-4-31b SWA = %+v, want %+v", m.SWA, want)
			}
		case m.SWA != nil:
			t.Errorf("seed entry %s carries swa %+v, but only gemma-4-31b has sliding-window layers", m.ID, m.SWA)
		}
	}
}
