package main

// pins_draft_test.go covers liveSpeculation's draft branch (ADR-0009): building the
// full SpeculationSpec from the catalog's draft block, and refusing when the draft
// file the catalog declares is not on disk. TestLiveSpeculationResolvesServedEntry
// (speculation_render_test.go) covers the mode-resolution funnel; this file covers
// only what changes once the resolved mode is "draft".

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// draftCatalogFile writes a schema-4 catalog with one entry carrying a qualified
// draft sidecar and one with none, so liveSpeculation's draft branch is exercised
// without touching the embedded seed.
func draftCatalogFile(t *testing.T) string {
	t.Helper()
	withDraft := `{
      "id": "hasdraft",
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
      "ngram_safe": true, "ngram_provenance": "gfx1151, test probe",
      "draft": {
        "shards": [{"url": "https://example.invalid/draft.gguf", "filename": "draft.gguf", "sha256": "00", "size_bytes": 1}],
        "weight_bytes": 100,
        "provenance": "gfx1151, test probe",
        "spec_type": "draft-mtp",
        "n_max": 3,
        "p_min": 0,
        "n_layers": 1,
        "n_kv_heads": 4,
        "head_dim": 256,
        "kv_bytes_per_elem": 2
      },
      "shards": [{"url": "https://example.invalid/a.gguf", "filename": "a.gguf", "sha256": "00", "size_bytes": 1}]
    }`
	noDraft := `{
      "id": "nodraft",
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
      "shards": [{"url": "https://example.invalid/b.gguf", "filename": "b.gguf", "sha256": "00", "size_bytes": 1}]
    }`
	body := fmt.Sprintf(`{"schema_version": 4, "catalog_version": "test", "models": [%s, %s]}`, withDraft, noDraft)
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	return path
}

// TestLiveSpeculationBuildsDraftSpec asserts a draft-mode resolution against a
// qualified entry, with the draft file present in the models dir, builds the full
// descriptor: DraftFile/SpecType/NMax/PMin from the catalog block, WithNgram from
// the entry's own ngram_safe (ADR-0009's "ngram riding along" clause).
func TestLiveSpeculationBuildsDraftSpec(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := os.MkdirAll(modelsDir(), 0o700); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir(), "draft.gguf"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write draft file: %v", err)
	}

	cfg := config.VillaConfig{Model: "hasdraft", Speculation: config.SpeculationDraft, CatalogPath: draftCatalogFile(t)}
	spec, err := liveSpeculation(cfg, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &inference.SpeculationSpec{
		Mode:      config.SpeculationDraft,
		WithNgram: true,
		DraftFile: "draft.gguf",
		SpecType:  "draft-mtp",
		NMax:      3,
		PMin:      0,
	}
	if *spec != *want {
		t.Errorf("spec = %+v, want %+v", spec, want)
	}
}

// TestLiveSpeculationRefusesMissingDraftFile asserts a qualified draft entry still
// refuses when its declared file is absent from the models dir, naming
// `villa model pull` as the remediation — the one check ResolveSpeculation cannot
// make, because presence-on-disk is host I/O and not catalog/config data.
func TestLiveSpeculationRefusesMissingDraftFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir()) // no draft.gguf written

	cfg := config.VillaConfig{Model: "hasdraft", Speculation: config.SpeculationDraft, CatalogPath: draftCatalogFile(t)}
	spec, err := liveSpeculation(cfg, false)
	if err == nil {
		t.Fatalf("expected a refusal, got spec %+v", spec)
	}
	if !strings.Contains(err.Error(), "villa model pull hasdraft") {
		t.Errorf("error = %v, want it to name `villa model pull hasdraft`", err)
	}
}

// TestLiveSpeculationDraftRequiresADeclaredSidecar asserts requesting draft against
// an entry with no draft block refuses through the ordinary ResolveSpeculation path
// (draftFits is false when Draft is nil), not the on-disk check.
func TestLiveSpeculationDraftRequiresADeclaredSidecar(t *testing.T) {
	cfg := config.VillaConfig{Model: "nodraft", Speculation: config.SpeculationDraft, CatalogPath: draftCatalogFile(t)}
	spec, err := liveSpeculation(cfg, false)
	if err == nil {
		t.Fatalf("expected a refusal, got spec %+v", spec)
	}
	if !strings.Contains(err.Error(), "refusing") {
		t.Errorf("error = %v, want it to mention refusing", err)
	}
	if spec != nil {
		t.Errorf("a refusal returned a descriptor %+v", spec)
	}
}
