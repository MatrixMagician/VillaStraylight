package preflight

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
)

// countingCloser records that the caller closed the reader it was handed.
type countingCloser struct {
	io.Reader
	closed *int
}

func (c countingCloser) Close() error { *c.closed++; return nil }

// qwenHeader is a well-formed hybrid header: 40 blocks at an attention interval
// of 4, so ten layers bear a KV cache.
func qwenHeader() []byte {
	return gguf.FixtureForTest("qwen35moe", map[string]uint64{
		"qwen35moe.block_count":             40,
		"qwen35moe.full_attention_interval": 4,
		"qwen35moe.attention.head_count_kv": 2,
		"qwen35moe.attention.key_length":    256,
	})
}

// matchingModel is the catalog entry that agrees with qwenHeader.
func matchingModel() catalog.Model {
	return catalog.Model{
		ID:       "qwen3.6-35b-a3b",
		NLayers:  10,
		NKVHeads: 2,
		HeadDim:  256,
		Shards:   []catalog.Shard{{Filename: "Qwen3.6.gguf"}},
	}
}

// openBytes builds an open seam serving b for every filename, counting closes.
func openBytes(b []byte, closed *int) func(string) (io.ReadCloser, error) {
	return func(string) (io.ReadCloser, error) {
		return countingCloser{Reader: strings.NewReader(string(b)), closed: closed}, nil
	}
}

// onlyResult asserts exactly one CheckResult came back and returns it.
func onlyResult(t *testing.T, got []CheckResult) CheckResult {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("got %d results, want exactly 1: %+v", len(got), got)
	}
	return got[0]
}

// TestCatalogGeometryPass guards the promise that an entry agreeing with its file
// header passes, and that the reader it was handed is closed.
func TestCatalogGeometryPass(t *testing.T) {
	closed := 0
	cat := catalog.Catalog{Models: []catalog.Model{matchingModel()}}
	r := onlyResult(t, RunCatalogGeometry(cat, openBytes(qwenHeader(), &closed)))
	if r.Status != StatusPass {
		t.Errorf("Status = %v, want PASS (detail: %s)", r.Status, r.Detail)
	}
	if r.ID != "CAT-01" {
		t.Errorf("ID = %q, want CAT-01", r.ID)
	}
	if !strings.Contains(r.Name, "qwen3.6-35b-a3b") {
		t.Errorf("Name = %q, want it to name the entry", r.Name)
	}
	if !strings.Contains(r.Provenance, "qwen35moe") {
		t.Errorf("Provenance = %q, want it to name the architecture it read", r.Provenance)
	}
	if closed != 1 {
		t.Errorf("reader closed %d times, want 1", closed)
	}
}

// TestCatalogGeometryMismatchNamesBoth guards the promise that a disagreement is a
// confident FAIL whose detail prints the catalog values AND the header values, so
// the operator can see which one is wrong without opening the file.
func TestCatalogGeometryMismatchNamesBoth(t *testing.T) {
	closed := 0
	m := matchingModel()
	m.NLayers, m.NKVHeads, m.HeadDim = 48, 4, 128
	cat := catalog.Catalog{Models: []catalog.Model{m}}
	r := onlyResult(t, RunCatalogGeometry(cat, openBytes(qwenHeader(), &closed)))
	if r.Status != StatusFail || r.Tier != TierBlock {
		t.Fatalf("Status/Tier = %v/%v, want FAIL/BLOCK", r.Status, r.Tier)
	}
	for _, want := range []string{
		"n_layers=48", "n_kv_heads=4", "head_dim=128",
		"kv_layers=10", "head_count_kv=2", "key_length=256",
		"Qwen3.6.gguf",
	} {
		if !strings.Contains(r.Detail, want) {
			t.Errorf("detail %q is missing %q", r.Detail, want)
		}
	}
	if !strings.Contains(r.Remediation, "seed.json") {
		t.Errorf("remediation %q does not point at the catalog entry", r.Remediation)
	}
	if closed != 1 {
		t.Errorf("reader closed %d times, want 1", closed)
	}
}

// TestCatalogGeometrySkipsAbsentFile guards the promise that a model that is not
// on this host produces NO result: it is not a finding, and a WARN for every
// catalog entry the operator never pulled would bury the ones that matter.
func TestCatalogGeometrySkipsAbsentFile(t *testing.T) {
	cat := catalog.Catalog{Models: []catalog.Model{matchingModel()}}
	got := RunCatalogGeometry(cat, func(string) (io.ReadCloser, error) {
		return nil, fs.ErrNotExist
	})
	if len(got) != 0 {
		t.Fatalf("got %d results for an absent file, want 0: %+v", len(got), got)
	}
}

// TestCatalogGeometrySkipsZeroGeometry guards the promise that an entry declaring
// no fit dimensions is skipped: there is nothing to disagree with, and comparing
// against a zero would fabricate a mismatch.
func TestCatalogGeometrySkipsZeroGeometry(t *testing.T) {
	closed := 0
	cat := catalog.Catalog{Models: []catalog.Model{{ID: "bare"}}}
	got := RunCatalogGeometry(cat, openBytes(qwenHeader(), &closed))
	if len(got) != 0 {
		t.Fatalf("got %d results for a zero-geometry entry, want 0: %+v", len(got), got)
	}
	if closed != 0 {
		t.Errorf("the file was opened for an entry with nothing to check")
	}
}

// TestCatalogGeometryUnreadableHeaderWarns guards the typed-Unknown promise: a
// file villa cannot parse is UNEVALUABLE, so it degrades to a WARN carrying the
// reason, never a confident FAIL fabricated from a signal it could not read.
func TestCatalogGeometryUnreadableHeaderWarns(t *testing.T) {
	closed := 0
	cat := catalog.Catalog{Models: []catalog.Model{matchingModel()}}
	r := onlyResult(t, RunCatalogGeometry(cat, openBytes([]byte("not a gguf file"), &closed)))
	if r.Status != StatusWarn || r.Tier != TierBlock {
		t.Fatalf("Status/Tier = %v/%v, want WARN/BLOCK", r.Status, r.Tier)
	}
	if r.Detail == "" || !strings.Contains(r.Detail, "GGUF") {
		t.Errorf("detail %q does not carry the read error", r.Detail)
	}
	if closed != 1 {
		t.Errorf("reader closed %d times, want 1", closed)
	}
}

// TestCatalogGeometryOpenErrorWarns guards the promise that an open failure that
// is NOT "absent" (a permission error, a bad mount) is surfaced as an unevaluable
// WARN rather than silently skipped like a file that was never pulled.
func TestCatalogGeometryOpenErrorWarns(t *testing.T) {
	cat := catalog.Catalog{Models: []catalog.Model{matchingModel()}}
	r := onlyResult(t, RunCatalogGeometry(cat, func(string) (io.ReadCloser, error) {
		return nil, errors.New("permission denied")
	}))
	if r.Status != StatusWarn {
		t.Fatalf("Status = %v, want WARN", r.Status)
	}
	if !strings.Contains(r.Detail, "permission denied") {
		t.Errorf("detail %q does not carry the open error", r.Detail)
	}
}

// TestCatalogGeometryMissingKeyWarns guards the promise that a header without the
// geometry keys is unevaluable, not a mismatch: the key it could not find is named
// so the operator can tell a stripped header from a wrong catalog entry.
func TestCatalogGeometryMissingKeyWarns(t *testing.T) {
	closed := 0
	partial := gguf.FixtureForTest("qwen35moe", map[string]uint64{
		"qwen35moe.block_count": 40,
	})
	cat := catalog.Catalog{Models: []catalog.Model{matchingModel()}}
	r := onlyResult(t, RunCatalogGeometry(cat, openBytes(partial, &closed)))
	if r.Status != StatusWarn {
		t.Fatalf("Status = %v, want WARN", r.Status)
	}
	if !strings.Contains(r.Detail, "head_count_kv") {
		t.Errorf("detail %q does not name the key it could not find", r.Detail)
	}
}

// TestCatalogGeometryOnePerEntry guards the promise that every checkable entry
// gets its own result, in catalog order, so a stable table and golden can be
// rendered from the slice.
func TestCatalogGeometryOnePerEntry(t *testing.T) {
	closed := 0
	a, b := matchingModel(), matchingModel()
	b.ID = "second"
	cat := catalog.Catalog{Models: []catalog.Model{a, b}}
	got := RunCatalogGeometry(cat, openBytes(qwenHeader(), &closed))
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if !strings.Contains(got[0].Name, a.ID) || !strings.Contains(got[1].Name, b.ID) {
		t.Errorf("results out of catalog order: %q then %q", got[0].Name, got[1].Name)
	}
}
