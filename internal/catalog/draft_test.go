// draft_test.go exercises the ADR-0009 draft sidecar: it decodes as an optional
// second Sidecar-shaped block, joins AllShards after the projector's, and the
// same fail-closed guard that protects the projector protects it.
package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// draftEntryJSON is a schema-4 entry whose draft block is spliced in per case,
// so each guard is exercised by one differing block and nothing else.
const draftEntryJSON = `{
  "schema_version": 4,
  "catalog_version": "test.invalid-draft",
  "models": [
    {
      "id": "broken-draft-model",
      "display_name": "Broken Draft Model",
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
      "draft": %s
    }
  ]
}`

// validDraftBlock is a complete, passing draft block, used as the JSON base
// each incomplete-field test case mutates one field of.
const validDraftBlock = `{
	"shards": [{"url": "https://example.invalid/mtp.gguf", "filename": "mtp.gguf",
		"sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "size_bytes": 900000000}],
	"weight_bytes": 700000000,
	"provenance": "gfx1151, test",
	"spec_type": "draft-mtp",
	"n_max": 3,
	"p_min": 0,
	"n_layers": 1,
	"n_kv_heads": 4,
	"head_dim": 256,
	"kv_bytes_per_elem": 2
}`

// TestLoadDraftExternal asserts the draft block decodes when present and stays
// absent when it is not: an entry with no draft key must decode to a nil Draft,
// because nil is what the render seam reads as "no draft for this model."
func TestLoadDraftExternal(t *testing.T) {
	c, warnings, err := Load(filepath.Join("testdata", "draft-external.json"))
	if err != nil {
		t.Fatalf("Load(draft-external): unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("Load(draft-external): unexpected warnings: %v", warnings)
	}
	drafted, ok := c.FindByID("external-draft-model")
	if !ok {
		t.Fatalf("external-draft-model not found (got %d models)", len(c.Models))
	}
	if drafted.Draft == nil {
		t.Fatalf("draft block decoded as nil")
	}
	if len(drafted.Draft.Shards) != 1 || drafted.Draft.Shards[0].Filename != "External-Draft-mtp.gguf" {
		t.Errorf("draft shards = %+v, want the one mtp file", drafted.Draft.Shards)
	}
	if drafted.Draft.WeightBytes != 700000000 {
		t.Errorf("draft weight_bytes = %d, want 700000000", drafted.Draft.WeightBytes)
	}
	if !strings.Contains(drafted.Draft.Provenance, "gfx1151") {
		t.Errorf("draft provenance = %q, want the exercise text", drafted.Draft.Provenance)
	}
	if drafted.Draft.SpecType != "draft-mtp" {
		t.Errorf("draft spec_type = %q, want draft-mtp", drafted.Draft.SpecType)
	}
	if drafted.Draft.NMax != 3 {
		t.Errorf("draft n_max = %d, want 3", drafted.Draft.NMax)
	}
	if drafted.Draft.NLayers != 1 || drafted.Draft.NKVHeads != 4 || drafted.Draft.HeadDim != 256 || drafted.Draft.KVBytesPerElem != 2 {
		t.Errorf("draft KV dims = %+v, want n_layers=1 n_kv_heads=4 head_dim=256 kv_bytes_per_elem=2", drafted.Draft)
	}

	plain, ok := c.FindByID("external-nodraft-model")
	if !ok {
		t.Fatalf("external-nodraft-model not found")
	}
	if plain.Draft != nil {
		t.Errorf("an entry with no draft key decoded as %+v, want nil", plain.Draft)
	}
}

// TestLoadDraftValidationRefusesIncomplete asserts the fail-closed guard: a
// draft with nothing to download, no memory to reserve, no exercise behind it,
// an unlisted spec type, an unusable sampling knob, or a missing KV dimension
// invalidates the WHOLE external catalog, because each is a promise villa could
// not keep.
func TestLoadDraftValidationRefusesIncomplete(t *testing.T) {
	cases := map[string]string{
		"no shards": strings.Replace(validDraftBlock, `"shards": [{"url": "https://example.invalid/mtp.gguf", "filename": "mtp.gguf",
		"sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "size_bytes": 900000000}]`, `"shards": []`, 1),
		"zero weight_bytes":  strings.Replace(validDraftBlock, `"weight_bytes": 700000000`, `"weight_bytes": 0`, 1),
		"empty provenance":   strings.Replace(validDraftBlock, `"provenance": "gfx1151, test"`, `"provenance": ""`, 1),
		"unknown spec_type":  strings.Replace(validDraftBlock, `"spec_type": "draft-mtp"`, `"spec_type": "draft-turbo"`, 1),
		"zero n_max":         strings.Replace(validDraftBlock, `"n_max": 3`, `"n_max": 0`, 1),
		"p_min above 1":      strings.Replace(validDraftBlock, `"p_min": 0`, `"p_min": 1.5`, 1),
		"p_min below 0":      strings.Replace(validDraftBlock, `"p_min": 0`, `"p_min": -0.1`, 1),
		"zero n_layers":      strings.Replace(validDraftBlock, `"n_layers": 1`, `"n_layers": 0`, 1),
		"zero n_kv_heads":    strings.Replace(validDraftBlock, `"n_kv_heads": 4`, `"n_kv_heads": 0`, 1),
		"zero head_dim":      strings.Replace(validDraftBlock, `"head_dim": 256`, `"head_dim": 0`, 1),
		"zero kv_bytes_elem": strings.Replace(validDraftBlock, `"kv_bytes_per_elem": 2`, `"kv_bytes_per_elem": 0`, 1),
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.json")
			body := strings.Replace(draftEntryJSON, "%s", block, 1)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			c, warnings, err := Load(path)
			if err != nil {
				t.Fatalf("Load: unexpected error: %v", err)
			}
			if !strings.Contains(strings.Join(warnings, " "), "broken-draft-model") {
				t.Errorf("refusal should name the offending entry, got %v", warnings)
			}
			if _, ok := c.FindByID("broken-draft-model"); ok {
				t.Errorf("an incomplete draft entry must not be used")
			}
			if _, ok := c.FindByID("qwen2.5-1.5b"); !ok {
				t.Errorf("expected fallback to the embedded seed")
			}
		})
	}
}

// TestSeedDraftsCarryProvenanceAndAllowlistedSpecType asserts the seed honours
// the rule the external validator enforces: a shipped draft names its exercise
// and its spec_type is one of the measured ones, never an unvetted value.
func TestSeedDraftsCarryProvenanceAndAllowlistedSpecType(t *testing.T) {
	c, _, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}
	withDraft := 0
	for _, m := range c.Models {
		if m.Draft == nil {
			continue
		}
		withDraft++
		if !strings.Contains(m.Draft.Provenance, "gfx1151") {
			t.Errorf("seed entry %q draft provenance = %q, want a gfx1151 exercise", m.ID, m.Draft.Provenance)
		}
		if !draftSpecTypes[m.Draft.SpecType] {
			t.Errorf("seed entry %q draft spec_type = %q, want one of the allowlist", m.ID, m.Draft.SpecType)
		}
	}
	if withDraft == 0 {
		t.Errorf("no seed entry ships a draft; the exercised entry should")
	}
}

// TestAllShardsAppendsDraftAfterProjector asserts the download manifest order:
// the model's own shards, then its projector's, then its draft's — so a model
// that claims a draft can never be left on disk without it, and the draft never
// races the projector for position.
func TestAllShardsAppendsDraftAfterProjector(t *testing.T) {
	m := Model{
		ID:     "v",
		Shards: []Shard{{Filename: "a.gguf"}},
		Projector: &Sidecar{
			Shards:      []Shard{{Filename: "v-mmproj.gguf"}},
			WeightBytes: 1,
			Provenance:  "gfx1151",
		},
		Draft: &Draft{
			Sidecar: Sidecar{
				Shards:      []Shard{{Filename: "v-draft.gguf"}},
				WeightBytes: 1,
				Provenance:  "gfx1151",
			},
			SpecType: "draft-mtp",
		},
	}
	got := m.AllShards()
	want := []string{"a.gguf", "v-mmproj.gguf", "v-draft.gguf"}
	if len(got) != len(want) {
		t.Fatalf("AllShards() = %+v, want %v", got, want)
	}
	for i, w := range want {
		if got[i].Filename != w {
			t.Errorf("AllShards()[%d] = %q, want %q", i, got[i].Filename, w)
		}
	}

	draftOnly := Model{
		ID:     "d",
		Shards: []Shard{{Filename: "a.gguf"}},
		Draft: &Draft{
			Sidecar: Sidecar{Shards: []Shard{{Filename: "d-draft.gguf"}}, WeightBytes: 1, Provenance: "gfx1151"},
		},
	}
	got = draftOnly.AllShards()
	if len(got) != 2 || got[0].Filename != "a.gguf" || got[1].Filename != "d-draft.gguf" {
		t.Errorf("AllShards() with no projector = %+v, want [a.gguf d-draft.gguf]", got)
	}
}
