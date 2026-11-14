package catalogadd

import (
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// newModel is a built entry for the merge cases, with one shard whose url and digest
// the cases vary.
func newModel(id, url, file, sha string) catalog.Model {
	return catalog.Model{
		ID: id, DisplayName: id, Quant: "Q4_K_M", WeightBytes: 10,
		NLayers: 4, NKVHeads: 2, HeadDim: 64, KVBytesPerElem: 2, DefaultCtx: 4096,
		Shards: []catalog.Shard{{URL: url, Filename: file, SHA256: sha, SizeBytes: 10}},
	}
}

func baseCatalog(models ...catalog.Model) catalog.Catalog {
	return catalog.Catalog{SchemaVersion: catalog.SupportedSchema, CatalogVersion: "test", Models: models}
}

const (
	urlA = "https://huggingface.co/o/r/resolve/" + commit + "/a.gguf"
	urlB = "https://huggingface.co/o/other/resolve/" + commit + "/a.gguf"
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// TestMergeAddsWithoutTouchingTheRest: the entry is appended and every other entry,
// the catalog version and the schema come through unchanged (ADR-0041: `add` changes
// one entry and never reorders or removes another).
func TestMergeAddsWithoutTouchingTheRest(t *testing.T) {
	keep := newModel("keep", "https://huggingface.co/o/k/resolve/"+commit+"/k.gguf", "k.gguf", shaB)
	base := baseCatalog(keep)
	got, outcome, err := Merge(base, newModel("new", urlA, "a.gguf", shaA))
	if err != nil || outcome != Added {
		t.Fatalf("Merge = %v, %v", outcome, err)
	}
	if len(got.Models) != 2 || got.Models[0].ID != "keep" || got.Models[1].ID != "new" {
		t.Fatalf("models = %v", ids(got))
	}
	if got.CatalogVersion != "test" || got.SchemaVersion != catalog.SupportedSchema {
		t.Fatalf("catalog header changed: %+v", got)
	}
	if len(base.Models) != 1 {
		t.Fatal("Merge mutated its input")
	}
}

func ids(c catalog.Catalog) []string {
	var out []string
	for _, m := range c.Models {
		out = append(out, m.ID)
	}
	return out
}

// TestMergeReAdd is the re-add table: the same source is a no-op that keeps the
// operator's edits, and everything that would put two different files under one name
// refuses. A refusal never changes the catalog.
func TestMergeReAdd(t *testing.T) {
	edited := newModel("m", urlA, "a.gguf", shaA)
	edited.NgramSafe, edited.NgramProvenance = true, "measured by the operator"
	edited.DefaultCtx = 65536
	base := baseCatalog(edited, newModel("other", "https://huggingface.co/o/x/resolve/"+commit+"/x.gguf", "x.gguf", shaB))

	tests := []struct {
		name    string
		m       catalog.Model
		want    Outcome
		wantErr error
	}{
		{"same url and sha is already present", newModel("m", urlA, "a.gguf", shaA), AlreadyPresent, nil},
		{"same id, same url, different sha refuses", newModel("m", urlA, "a.gguf", shaB), 0, ErrShaMismatch},
		{"same id, different url refuses", newModel("m", urlB, "a.gguf", shaA), 0, ErrIDTaken},
		{"another entry's file name refuses", newModel("fresh", urlB, "x.gguf", shaA), 0, ErrFilenameTaken},
		{"a new id and file adds", newModel("fresh", urlB, "fresh.gguf", shaA), Added, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, outcome, err := Merge(base, tt.m)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Merge error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil || outcome != tt.want {
				t.Fatalf("Merge = %v, %v", outcome, err)
			}
			if outcome == AlreadyPresent {
				if got.Models[0].DefaultCtx != 65536 || !got.Models[0].NgramSafe {
					t.Fatal("an already-present entry lost the operator's edits")
				}
				if len(got.Models) != len(base.Models) {
					t.Fatal("an already-present entry changed the catalog")
				}
			}
		})
	}
}

// TestMergeNeverShadowsASeedEntry: the id of a seed entry carried in the file is just
// an id that is taken, whatever the new file is.
func TestMergeNeverShadowsASeedEntry(t *testing.T) {
	seed, err := catalog.Seed()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Merge(seed, newModel(seed.Models[0].ID, urlA, "a.gguf", shaA))
	if !errors.Is(err, ErrIDTaken) {
		t.Fatalf("Merge over a seed id = %v, want ErrIDTaken", err)
	}
}

// TestMergeRefusesWhatLoadWouldReject: the merged catalog is validated before it is
// returned, so a file `add` writes is always one catalog.Load accepts.
func TestMergeRefusesWhatLoadWouldReject(t *testing.T) {
	bad := newModel("bad", urlA, "a.gguf", shaA)
	bad.NgramSafe = true // no provenance: Load refuses the WHOLE catalog for this
	if _, _, err := Merge(baseCatalog(), bad); err == nil || !strings.Contains(err.Error(), "ngram") {
		t.Fatalf("Merge accepted an entry Load would reject: %v", err)
	}
}

// TestPrecheckRunsBeforeTheDownload: the same id and filename rules, decided from the
// URL alone, so a refusal costs no bandwidth.
func TestPrecheckRunsBeforeTheDownload(t *testing.T) {
	base := baseCatalog(newModel("m", urlA, "a.gguf", shaA))
	srcA := Source{URL: urlA, Filename: "a.gguf"}
	srcB := Source{URL: urlB, Filename: "a.gguf"}

	if out, err := Precheck(base, "m", srcA); err != nil || out != AlreadyPresent {
		t.Fatalf("same source = %v, %v", out, err)
	}
	if _, err := Precheck(base, "m", srcB); !errors.Is(err, ErrIDTaken) {
		t.Fatalf("same id, other source = %v", err)
	}
	if _, err := Precheck(base, "fresh", srcB); !errors.Is(err, ErrFilenameTaken) {
		t.Fatalf("same file name, other source = %v", err)
	}
	if out, err := Precheck(base, "fresh", Source{URL: urlB, Filename: "b.gguf"}); err != nil || out != Added {
		t.Fatalf("a new source = %v, %v", out, err)
	}
}
