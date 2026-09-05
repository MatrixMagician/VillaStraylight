package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectorEntryJSON is a schema-4 entry whose projector block is spliced in per
// case, so each guard is exercised by one differing block and nothing else.
const projectorEntryJSON = `{
  "schema_version": 4,
  "catalog_version": "test.invalid-projector",
  "models": [
    {
      "id": "broken-vision-model",
      "display_name": "Broken Vision Model",
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
      "projector": %s
    }
  ]
}`

// TestLoadProjectorExternal asserts the sidecar block decodes when present and
// stays absent when it is not: a text-only entry must decode to a nil Projector,
// because nil is what every downstream gate reads as "this model has no vision".
func TestLoadProjectorExternal(t *testing.T) {
	c, warnings, err := Load(filepath.Join("testdata", "projector-external.json"))
	if err != nil {
		t.Fatalf("Load(projector-external): unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Load(projector-external): unexpected warnings: %v", warnings)
	}
	vision, ok := c.FindByID("external-vision-model")
	if !ok {
		t.Fatalf("external-vision-model not found (got %d models)", len(c.Models))
	}
	if vision.Projector == nil {
		t.Fatalf("projector block decoded as nil")
	}
	if len(vision.Projector.Shards) != 1 || vision.Projector.Shards[0].Filename != "External-Vision-mmproj-F16.gguf" {
		t.Errorf("projector shards = %+v, want the one mmproj file", vision.Projector.Shards)
	}
	if vision.Projector.WeightBytes != 1100000000 {
		t.Errorf("projector weight_bytes = %d, want 1100000000", vision.Projector.WeightBytes)
	}
	if !strings.Contains(vision.Projector.Provenance, "gfx1151") {
		t.Errorf("projector provenance = %q, want the exercise text", vision.Projector.Provenance)
	}

	plain, ok := c.FindByID("external-textonly-model")
	if !ok {
		t.Fatalf("external-textonly-model not found")
	}
	if plain.Projector != nil {
		t.Errorf("an entry with no projector key decoded as %+v, want nil", plain.Projector)
	}
}

// TestLoadProjectorValidationRefusesIncomplete asserts the fail-closed sidecar
// guard: a projector with nothing to download, no memory to reserve, or no
// exercise behind it invalidates the WHOLE external catalog, because each of the
// three is a promise of vision villa could not keep.
func TestLoadProjectorValidationRefusesIncomplete(t *testing.T) {
	cases := map[string]string{
		"no shards": `{"shards": [], "weight_bytes": 1100000000, "provenance": "gfx1151, test"}`,
		"zero weight_bytes": `{"shards": [{"url": "https://example.invalid/mmproj.gguf", "filename": "mmproj.gguf",
			"sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "size_bytes": 800000000}],
			"weight_bytes": 0, "provenance": "gfx1151, test"}`,
		"empty provenance": `{"shards": [{"url": "https://example.invalid/mmproj.gguf", "filename": "mmproj.gguf",
			"sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "size_bytes": 800000000}],
			"weight_bytes": 1100000000, "provenance": ""}`,
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.json")
			body := strings.Replace(projectorEntryJSON, "%s", block, 1)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			c, warnings, err := Load(path)
			if err != nil {
				t.Fatalf("Load: unexpected error: %v", err)
			}
			if !strings.Contains(strings.Join(warnings, " "), "broken-vision-model") {
				t.Errorf("refusal should name the offending entry, got %v", warnings)
			}
			if _, ok := c.FindByID("broken-vision-model"); ok {
				t.Errorf("an incomplete projector entry must not be used")
			}
			if _, ok := c.FindByID("qwen2.5-1.5b"); !ok {
				t.Errorf("expected fallback to the embedded seed")
			}
		})
	}
}

// TestSeedProjectorsCarryProvenance asserts the seed honours the rule the
// external validator enforces, and that every projector filename is bare: the
// models dir is flat, so a path separator would escape it and two entries'
// projectors would collide under a shared upstream name.
func TestSeedProjectorsCarryProvenance(t *testing.T) {
	c, _, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	withProjector := 0
	for _, m := range c.Models {
		if m.Projector == nil {
			continue
		}
		withProjector++
		if !strings.Contains(m.Projector.Provenance, "gfx1151") {
			t.Errorf("seed entry %q projector provenance = %q, want a gfx1151 exercise", m.ID, m.Projector.Provenance)
		}
		if m.Projector.WeightBytes == 0 {
			t.Errorf("seed entry %q projector reserves no memory", m.ID)
		}
		for _, sh := range m.Projector.Shards {
			if sh.Filename != filepath.Base(sh.Filename) || strings.ContainsAny(sh.Filename, `/\`) {
				t.Errorf("seed entry %q projector filename %q is not a bare filename", m.ID, sh.Filename)
			}
		}
	}
	if withProjector == 0 {
		t.Errorf("no seed entry ships a projector; the exercised entry should")
	}
}

// TestAllShardsAppendsProjector asserts the download manifest is the model's
// shards followed by the projector's, and that it is nil-safe: a text-only entry
// must yield exactly the shards it already had.
func TestAllShardsAppendsProjector(t *testing.T) {
	textOnly := Model{ID: "t", Shards: []Shard{{Filename: "a.gguf"}}}
	if got := textOnly.AllShards(); len(got) != 1 || got[0].Filename != "a.gguf" {
		t.Errorf("AllShards() on a text-only entry = %+v, want the one model shard", got)
	}

	vision := Model{
		ID:     "v",
		Shards: []Shard{{Filename: "a.gguf"}, {Filename: "b.gguf"}},
		Projector: &Sidecar{
			Shards:      []Shard{{Filename: "v-mmproj.gguf"}},
			WeightBytes: 1,
			Provenance:  "gfx1151",
		},
	}
	got := vision.AllShards()
	want := []string{"a.gguf", "b.gguf", "v-mmproj.gguf"}
	if len(got) != len(want) {
		t.Fatalf("AllShards() = %+v, want %v", got, want)
	}
	for i, w := range want {
		if got[i].Filename != w {
			t.Errorf("AllShards()[%d] = %q, want %q", i, got[i].Filename, w)
		}
	}
	if len(vision.Shards) != 2 {
		t.Errorf("AllShards() mutated the model's own shard slice: %+v", vision.Shards)
	}
}
