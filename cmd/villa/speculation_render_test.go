package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// speculationCatalogFile writes a schema-4 catalog with one qualified and one
// unqualified entry and returns its path, so the funnel's resolution is exercised
// without touching the embedded seed.
func speculationCatalogFile(t *testing.T) string {
	t.Helper()
	entry := func(id string, ngram bool) string {
		extra := ""
		if ngram {
			extra = `"ngram_safe": true, "ngram_provenance": "gfx1151, test probe",`
		}
		return fmt.Sprintf(`{
      "id": %q,
      "display_name": "Fixture",
      "quant": "Q4_K_M",
      "weight_bytes": 5000000000,
      "n_layers": 32,
      "n_kv_heads": 8,
      "head_dim": 128,
      "kv_bytes_per_elem": 2,
      "default_ctx": 16384,
      "min_envelope_bytes": 7000000000,
      "tier_gb": 16,
      "unified_memory_safe": true,
      "backend_default": "rocm",
      "bootstrap": false,
      %s
      "shards": [{"url": "https://example.invalid/a.gguf", "filename": "a.gguf", "sha256": "00", "size_bytes": 1}]
    }`, id, extra)
	}
	body := fmt.Sprintf(`{"schema_version": 4, "catalog_version": "test", "models": [%s, %s]}`,
		entry("qualified", true), entry("unqualified", false))
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	return path
}

// TestLiveSpeculationOffNeedsNoCatalog asserts an unset or off mode resolves to nil
// without consulting the catalog at all: a stack that asked for nothing must render
// even when the configured catalog is unreadable.
func TestLiveSpeculationOffNeedsNoCatalog(t *testing.T) {
	for _, mode := range []string{"", config.SpeculationOff} {
		cfg := config.VillaConfig{Model: "qualified", Speculation: mode, CatalogPath: "/nonexistent/catalog.json"}
		spec, err := liveSpeculation(cfg, false)
		if err != nil {
			t.Errorf("mode %q: unexpected error: %v", mode, err)
		}
		if spec != nil {
			t.Errorf("mode %q: got %+v, want nil", mode, spec)
		}
	}
}

// TestLiveSpeculationResolvesServedEntry asserts the mode is resolved against the
// entry actually served, which is the coder model in coding mode and the chat model
// otherwise.
func TestLiveSpeculationResolvesServedEntry(t *testing.T) {
	path := speculationCatalogFile(t)
	cases := []struct {
		name     string
		cfg      config.VillaConfig
		coding   bool
		wantMode string
		wantErr  string
	}{
		{
			name:     "qualified chat model",
			cfg:      config.VillaConfig{Model: "qualified", Speculation: config.SpeculationNgram, CatalogPath: path},
			wantMode: config.SpeculationNgram,
		},
		{
			name:    "unqualified chat model refuses",
			cfg:     config.VillaConfig{Model: "unqualified", Speculation: config.SpeculationNgram, CatalogPath: path},
			wantErr: "not qualified",
		},
		{
			name:     "coding mode reads the coder model",
			cfg:      config.VillaConfig{Model: "unqualified", CoderModel: "qualified", Speculation: config.SpeculationNgram, CatalogPath: path},
			coding:   true,
			wantMode: config.SpeculationNgram,
		},
		{
			name:    "a model the catalog does not carry",
			cfg:     config.VillaConfig{Model: "absent", Speculation: config.SpeculationNgram, CatalogPath: path},
			wantErr: "absent",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := liveSpeculation(tc.cfg, tc.coding)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error mentioning %q, got spec %+v", tc.wantErr, spec)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
				}
				if spec != nil {
					t.Errorf("a refusal returned a descriptor %+v", spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if spec == nil || spec.Mode != tc.wantMode {
				t.Errorf("spec = %+v, want mode %q", spec, tc.wantMode)
			}
		})
	}
}
