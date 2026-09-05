package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// visionCatalogFile writes a catalog with one entry that ships a projector and one
// that does not, so the funnel's resolution is exercised without the embedded seed.
func visionCatalogFile(t *testing.T) string {
	t.Helper()
	entry := func(id string, projector bool) string {
		extra := ""
		if projector {
			extra = `"projector": {"shards": [{"url": "https://example.invalid/mmproj.gguf", "filename": "fixture-mmproj-F16.gguf", "sha256": "00", "size_bytes": 1}], "weight_bytes": 1100000000, "provenance": "gfx1151, test"},`
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
		entry("has-projector", true), entry("no-projector", false))
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	return path
}

// TestLiveProjectorOffNeedsNoCatalog asserts vision off resolves to "" without
// consulting the catalog: a stack that asked for nothing must render even when the
// configured catalog is unreadable.
func TestLiveProjectorOffNeedsNoCatalog(t *testing.T) {
	cfg := config.VillaConfig{Model: "has-projector", CatalogPath: "/nonexistent/catalog.json"}
	got, err := liveProjector(cfg, false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want \"\"", got)
	}
}

// TestLiveProjector covers the funnel's resolution: the served entry's projector
// filename, an empty answer in coding mode, and a refusal when the config claims
// vision the served entry cannot deliver.
func TestLiveProjector(t *testing.T) {
	path := visionCatalogFile(t)
	cases := []struct {
		name    string
		cfg     config.VillaConfig
		coding  bool
		want    string
		wantErr string
	}{
		{
			name: "the served entry's projector",
			cfg:  config.VillaConfig{Model: "has-projector", Vision: true, CatalogPath: path},
			want: "fixture-mmproj-F16.gguf",
		},
		{
			name:   "coding mode is text-only",
			cfg:    config.VillaConfig{Model: "has-projector", CoderModel: "has-projector", Vision: true, CatalogPath: path, CodingMode: true},
			coding: true,
			want:   "",
		},
		{
			name:    "vision on, entry ships none",
			cfg:     config.VillaConfig{Model: "no-projector", Vision: true, CatalogPath: path},
			wantErr: "ships no projector",
		},
		{
			name:    "a model the catalog does not carry",
			cfg:     config.VillaConfig{Model: "absent", Vision: true, CatalogPath: path},
			wantErr: "absent",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := liveProjector(tc.cfg, tc.coding)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error mentioning %q, got %q", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
