package catalog

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
)

// TestModelGeometry guards the promise that a catalog entry projects its three
// fit dimensions into the same shape the GGUF header reader returns, so the two
// are compared as values rather than field by field at each call site.
func TestModelGeometry(t *testing.T) {
	m := Model{ID: "x", NLayers: 48, NKVHeads: 4, HeadDim: 128}
	want := gguf.Geometry{KVLayers: 48, HeadCountKV: 4, KeyLength: 128}
	if got := m.Geometry(); got != want {
		t.Errorf("Geometry() = %+v, want %+v", got, want)
	}
}

// TestModelGeometryZeroWhenUnpopulated guards the promise that an entry carrying
// no fit dimensions projects the zero Geometry, which is the value every caller
// treats as "nothing to cross-check".
func TestModelGeometryZeroWhenUnpopulated(t *testing.T) {
	if got := (Model{ID: "x"}).Geometry(); got != (gguf.Geometry{}) {
		t.Errorf("Geometry() = %+v, want the zero value", got)
	}
}

// TestPrimaryFile guards the promise that the on-disk GGUF filename is the first
// shard's filename, with the <id>.gguf fallback for an entry carrying no download
// manifest.
func TestPrimaryFile(t *testing.T) {
	withShards := Model{ID: "big", Shards: []Shard{
		{Filename: "Big-00001-of-00003.gguf"},
		{Filename: "Big-00002-of-00003.gguf"},
	}}
	if got := withShards.PrimaryFile(); got != "Big-00001-of-00003.gguf" {
		t.Errorf("PrimaryFile() = %q, want the first shard's filename", got)
	}
	if got := (Model{ID: "bare"}).PrimaryFile(); got != "bare.gguf" {
		t.Errorf("PrimaryFile() = %q, want %q", got, "bare.gguf")
	}
	blank := Model{ID: "blank", Shards: []Shard{{Filename: ""}}}
	if got := blank.PrimaryFile(); got != "blank.gguf" {
		t.Errorf("PrimaryFile() with an empty shard filename = %q, want %q", got, "blank.gguf")
	}
}

// TestSeedEntriesCarryGeometry guards the promise that every seed entry declares
// the three fit dimensions. An entry with a zero dimension is skipped by the
// cross-check, so a missing value would silently opt that entry out of the very
// gate meant to catch a typo in it.
func TestSeedEntriesCarryGeometry(t *testing.T) {
	cat, _, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, m := range cat.Models {
		if m.Geometry() == (gguf.Geometry{}) {
			t.Errorf("seed entry %s carries no geometry, so the GGUF cross-check skips it", m.ID)
		}
	}
}

// TestPrimaryFileStaysBare guards the promise that neither an id nor a shard
// filename from an untrusted external catalog can hand a caller a separator to
// follow out of the models dir.
func TestPrimaryFileStaysBare(t *testing.T) {
	if got := (Model{ID: "../../etc/passwd"}).PrimaryFile(); got != "passwd.gguf" {
		t.Errorf("PrimaryFile() = %q, want a bare filename", got)
	}
	esc := Model{ID: "x", Shards: []Shard{{Filename: "../evil.gguf"}}}
	if got := esc.PrimaryFile(); got != "evil.gguf" {
		t.Errorf("PrimaryFile() = %q, want a bare filename", got)
	}
}
