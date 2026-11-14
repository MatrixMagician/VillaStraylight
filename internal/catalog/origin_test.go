package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedModel returns the embedded seed's first entry, the base every origin case
// mutates, so a case differs from the seed by exactly one thing.
func seedModel(t *testing.T) Model {
	t.Helper()
	c, _, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	return c.Models[0]
}

// TestOriginOf guards the derived marking (ADR-0041): an entry's origin is its
// relation to the embedded seed, so an entry nobody stored a flag on is still
// marked, and an editor cannot drop the mark by deleting a key.
func TestOriginOf(t *testing.T) {
	base := seedModel(t)

	edited := base
	edited.DefaultCtx++

	editedShard := base
	editedShard.Shards = append([]Shard{}, base.Shards...)
	editedShard.Shards[0].SHA256 = strings.Repeat("0", 64)

	novel := base
	novel.ID = "not-in-the-seed"

	tests := []struct {
		name string
		m    Model
		want Origin
	}{
		{"identical to the seed entry", base, OriginSeed},
		{"id in the seed, a scalar differs", edited, OriginEdited},
		{"id in the seed, a shard differs", editedShard, OriginEdited},
		{"id absent from the seed", novel, OriginOperator},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OriginOf(tt.m); got != tt.want {
				t.Fatalf("OriginOf = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestOriginLabels pins the two operator labels and the seed's empty one: the empty
// string is what keeps a seed row out of every --json output.
func TestOriginLabels(t *testing.T) {
	if got := OriginSeed.Label(); got != "" {
		t.Fatalf("seed label = %q, want empty (omitted from JSON)", got)
	}
	if got := OriginOperator.Label(); got != "operator-supplied" {
		t.Fatalf("operator label = %q", got)
	}
	if got := OriginEdited.Label(); got != "operator-edited" {
		t.Fatalf("edited label = %q", got)
	}
}

// TestMissingFromSeed is the CAT-03 input: the seed ids an external catalog lacks,
// in seed order, and nothing for a catalog that is a superset.
func TestMissingFromSeed(t *testing.T) {
	seed, _, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if got := MissingFromSeed(seed); len(got) != 0 {
		t.Fatalf("the seed lacks %v of itself", got)
	}

	subset := Catalog{SchemaVersion: SupportedSchema, Models: seed.Models[2:]}
	got := MissingFromSeed(subset)
	want := []string{seed.Models[0].ID, seed.Models[1].ID}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("MissingFromSeed = %v, want %v", got, want)
	}

	extra := Catalog{SchemaVersion: SupportedSchema, Models: append(append([]Model{}, seed.Models...), Model{ID: "mine"})}
	if got := MissingFromSeed(extra); len(got) != 0 {
		t.Fatalf("an addition must not read as drift: %v", got)
	}
}

// TestLoadFileReturnsWhatLoadWarnsAbout is the write-side contract: Load degrades an
// unusable external file to the seed with a warning, which is right for reading and
// wrong for a writer that would then replace the operator's file. LoadFile must
// return the error instead.
func TestLoadFileReturnsWhatLoadWarnsAbout(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	tests := []struct {
		name string
		path string
	}{
		{"missing", filepath.Join(dir, "absent.json")},
		{"malformed", write("bad.json", "{not json")},
		{"older schema", write("old.json", `{"schema_version":3,"catalog_version":"x","models":[]}`)},
		{"newer schema", write("new.json", `{"schema_version":99,"catalog_version":"x","models":[]}`)},
		{"ngram_safe with no provenance", write("ngram.json", `{"schema_version":5,"catalog_version":"x","models":[{"id":"a","ngram_safe":true}]}`)},
		{"unknown key", write("unk.json", `{"schema_version":5,"catalog_version":"x","models":[{"id":"a","surprise":1}]}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := LoadFile(tt.path); err == nil {
				t.Fatal("LoadFile accepted a file Load would replace with the seed")
			}
			if _, warnings, _ := Load(tt.path); len(warnings) == 0 {
				t.Fatal("Load stopped warning; the two entry points have diverged")
			}
		})
	}

	ok := write("ok.json", `{"schema_version":5,"catalog_version":"x","models":[{"id":"a"}]}`)
	c, err := LoadFile(ok)
	if err != nil || len(c.Models) != 1 {
		t.Fatalf("LoadFile(valid) = %v, %v", c, err)
	}
}

// TestValidateIsLoadsOwnChain keeps Validate and Load in step: a catalog Validate
// refuses is one Load rejects, so a writer that validates before writing cannot
// produce a file Load then discards.
func TestValidateIsLoadsOwnChain(t *testing.T) {
	bad := Catalog{SchemaVersion: SupportedSchema, Models: []Model{{ID: "a", NgramSafe: true}}}
	if err := Validate(bad); err == nil {
		t.Fatal("Validate accepted ngram_safe with no provenance")
	}
	if err := Validate(Catalog{SchemaVersion: SupportedSchema, Models: []Model{{ID: "a"}}}); err != nil {
		t.Fatalf("Validate refused a plain entry: %v", err)
	}
}
