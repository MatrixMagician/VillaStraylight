package catalog

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadEmbeddedSeed asserts Load("") returns the embedded seed with the
// supported schema, at least three tier seeds plus exactly one bootstrap entry,
// and that every model carries the dimensions the fit math needs.
func TestLoadEmbeddedSeed(t *testing.T) {
	c, warnings, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Load(\"\"): unexpected warnings: %v", warnings)
	}
	if c.SchemaVersion != SupportedSchema {
		t.Errorf("embedded seed schema_version = %d, want %d", c.SchemaVersion, SupportedSchema)
	}

	var tiers, bootstraps int
	for _, m := range c.Models {
		if m.Bootstrap {
			bootstraps++
		} else {
			tiers++
		}
		if m.NLayers <= 0 || m.NKVHeads <= 0 || m.HeadDim <= 0 || m.KVBytesPerElem <= 0 {
			t.Errorf("model %q missing a required KV dimension (layers=%d kv_heads=%d head_dim=%d bytes_per_elem=%d)",
				m.ID, m.NLayers, m.NKVHeads, m.HeadDim, m.KVBytesPerElem)
		}
		if m.WeightBytes == 0 {
			t.Errorf("model %q has zero weight_bytes", m.ID)
		}
	}
	if tiers < 3 {
		t.Errorf("embedded seed has %d tier entries, want >= 3", tiers)
	}
	if bootstraps != 1 {
		t.Errorf("embedded seed has %d bootstrap entries, want exactly 1", bootstraps)
	}
}

// TestLoadSeedDownloadMetadata asserts the schema-v2 seed carries real download
// metadata: every tier/bootstrap entry exposes at least one shard, and each shard
// carries a resolve URL + a non-empty expected SHA256 + a non-zero expected size.
// This is the MODEL-02 contract the downloader consumes.
func TestLoadSeedDownloadMetadata(t *testing.T) {
	c, _, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	if SupportedSchema != 2 {
		t.Fatalf("SupportedSchema = %d, want 2 (schema bumped for download fields)", SupportedSchema)
	}
	if c.SchemaVersion != 2 {
		t.Errorf("embedded seed schema_version = %d, want 2", c.SchemaVersion)
	}
	for _, m := range c.Models {
		if len(m.Shards) == 0 {
			t.Errorf("model %q has no shards — download metadata missing", m.ID)
			continue
		}
		for i, s := range m.Shards {
			if s.URL == "" {
				t.Errorf("model %q shard %d has empty URL", m.ID, i)
			}
			if s.Filename == "" {
				t.Errorf("model %q shard %d has empty filename", m.ID, i)
			}
			if len(s.SHA256) != 64 {
				t.Errorf("model %q shard %d sha256 = %q, want 64 hex chars", m.ID, i, s.SHA256)
			}
			if s.SizeBytes == 0 {
				t.Errorf("model %q shard %d has zero size_bytes", m.ID, i)
			}
		}
	}
}

// TestLoadSeedVerifiedDims asserts the Pitfall-6 corrected dimensions made it into
// the seed for the validated entries (0.5B 24L/2KV/64; 1.5B 28L/2KV/128;
// 30B-A3B 48L/4KV/128 — n_kv_heads=4 NOT 8).
func TestLoadSeedVerifiedDims(t *testing.T) {
	c, _, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	want := map[string]struct{ layers, kv, head int }{
		"qwen2.5-0.5b": {24, 2, 64},
		"qwen2.5-1.5b": {28, 2, 128},
		"qwen3-30b-a3b": {48, 4, 128},
	}
	for id, w := range want {
		m, ok := c.FindByID(id)
		if !ok {
			t.Errorf("seed missing expected entry %q", id)
			continue
		}
		if m.NLayers != w.layers || m.NKVHeads != w.kv || m.HeadDim != w.head {
			t.Errorf("model %q dims = %dL/%dKV/%d, want %dL/%dKV/%d",
				id, m.NLayers, m.NKVHeads, m.HeadDim, w.layers, w.kv, w.head)
		}
	}
}

// TestLoadMultiShardParses asserts a multi-shard external catalog (using the
// -00001-of-0000N naming convention) parses and each shard carries its own
// per-shard sha256 + size.
func TestLoadMultiShardParses(t *testing.T) {
	path := filepath.Join("testdata", "multishard-catalog.json")
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load(multishard): unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Load(multishard): unexpected warnings: %v", warnings)
	}
	m, ok := c.FindByID("sharded-235b")
	if !ok {
		t.Fatalf("Load(multishard): sharded-235b not found (got %d models)", len(c.Models))
	}
	if len(m.Shards) != 3 {
		t.Fatalf("sharded-235b has %d shards, want 3", len(m.Shards))
	}
	for i, s := range m.Shards {
		if len(s.SHA256) != 64 {
			t.Errorf("shard %d sha256 = %q, want 64 hex chars", i, s.SHA256)
		}
		if s.SizeBytes == 0 {
			t.Errorf("shard %d has zero size_bytes", i)
		}
		want := fmt.Sprintf("-0000%d-of-00003.gguf", i+1)
		if !strings.HasSuffix(s.Filename, want) {
			t.Errorf("shard %d filename = %q, want suffix %q", i, s.Filename, want)
		}
	}
}

// TestLoadPrefersExternal asserts a valid external catalog is preferred over the
// embedded seed.
func TestLoadPrefersExternal(t *testing.T) {
	path := filepath.Join("testdata", "good-catalog.json")
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load(good): unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Load(good): unexpected warnings: %v", warnings)
	}
	if _, ok := c.FindByID("external-only-model"); !ok {
		t.Errorf("Load(good): expected external catalog to be used (external-only-model not found); got %d models", len(c.Models))
	}
}

// TestLoadVersionMismatchFallsBack asserts that too-new and too-old external
// catalogs warn and fall back to the embedded seed without an error.
func TestLoadVersionMismatchFallsBack(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{"too-new", "too-new-catalog.json"},
		{"too-old", "too-old-catalog.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("testdata", tc.file)
			c, warnings, err := Load(path)
			if err != nil {
				t.Fatalf("Load(%s): unexpected error: %v", tc.name, err)
			}
			if len(warnings) == 0 {
				t.Errorf("Load(%s): expected a schema-mismatch warning, got none", tc.name)
			}
			if !strings.Contains(strings.Join(warnings, " "), "schema_version") {
				t.Errorf("Load(%s): warning should mention schema_version, got %v", tc.name, warnings)
			}
			// Fell back to embedded seed (has the bootstrap entry).
			if _, ok := c.FindByID("qwen2.5-1.5b"); !ok {
				t.Errorf("Load(%s): expected fallback to embedded seed, but qwen2.5-1.5b not present", tc.name)
			}
		})
	}
}

// TestLoadMalformedFallsBack asserts an invalid-JSON external catalog warns and
// falls back to the embedded seed — never a panic, never a returned error.
func TestLoadMalformedFallsBack(t *testing.T) {
	path := filepath.Join("testdata", "malformed-catalog.json")
	c, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load(malformed): unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Errorf("Load(malformed): expected a warning, got none")
	}
	if _, ok := c.FindByID("qwen2.5-1.5b"); !ok {
		t.Errorf("Load(malformed): expected fallback to embedded seed")
	}
}

// TestLoadRejectsTraversalDir asserts a directory path is rejected (and falls
// back to the seed) rather than read.
func TestLoadMissingExternalFallsBack(t *testing.T) {
	c, warnings, err := Load(filepath.Join("testdata", "does-not-exist.json"))
	if err != nil {
		t.Fatalf("Load(missing): unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Errorf("Load(missing): expected a warning about the unusable external file")
	}
	if len(c.Models) == 0 {
		t.Errorf("Load(missing): expected fallback to embedded seed (non-empty)")
	}
}
