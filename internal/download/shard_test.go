package download

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
)

// serveOne wires a single-shard httptest.Server that serves body and advertises
// the given metadata.
func serveOne(t *testing.T, body []byte) (*httptest.Server, catalog.Shard) {
	t.Helper()
	srv := rangeServer(t, body, sha256Hex(body), int64(len(body)))
	return srv, makeShard(srv.URL, "shard.gguf", body)
}

// TestShardsAllPresentVerify: a 3-shard set where every shard's bytes match its
// sha256+size succeeds and writes all three final files.
func TestShardsAllPresentVerify(t *testing.T) {
	dir := t.TempDir()
	bodies := [][]byte{[]byte("shard one body"), []byte("shard two body!!"), []byte("third shard here")}
	var shards []catalog.Shard
	for i, b := range bodies {
		b := b
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Linked-Etag", sha256Hex(b))
			w.Header().Set("X-Linked-Size", strconv.Itoa(len(b)))
			w.Header().Set("Accept-Ranges", "bytes")
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}
			_, _ = w.Write(b)
		}))
		t.Cleanup(srv.Close)
		shards = append(shards, catalog.Shard{
			URL:       srv.URL,
			Filename:  filepathName(i+1, 3),
			SHA256:    sha256Hex(b),
			SizeBytes: uint64(len(b)),
		})
	}
	m := catalog.Model{ID: "three-shard", Shards: shards}
	if err := pullShards(t.Context(), http.DefaultClient, m, dir); err != nil {
		t.Fatalf("pullShards: %v", err)
	}
	for i := range bodies {
		p := filepath.Join(dir, filepathName(i+1, 3))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("shard %d final missing: %v", i+1, err)
		}
	}
}

// TestShardsRejectMissing: if any shard's server is unreachable/missing, the whole
// model is rejected (error) and no partials linger.
func TestShardsRejectMissing(t *testing.T) {
	dir := t.TempDir()
	good := []byte("good shard")
	srv, sh1 := serveOne(t, good)
	sh1.Filename = filepathName(1, 2)
	_ = srv

	// Second shard points at a dead URL.
	sh2 := catalog.Shard{
		URL:       "http://127.0.0.1:1/does-not-exist",
		Filename:  filepathName(2, 2),
		SHA256:    sha256Hex([]byte("missing")),
		SizeBytes: 7,
	}
	m := catalog.Model{ID: "two-shard-missing", Shards: []catalog.Shard{sh1, sh2}}
	if err := pullShards(t.Context(), http.DefaultClient, m, dir); err == nil {
		t.Fatal("expected rejection when a shard is missing, got nil")
	}
}

// TestShardsRejectMismatch: if any single shard's bytes mismatch its sha256, the
// whole model is rejected.
func TestShardsRejectMismatch(t *testing.T) {
	dir := t.TempDir()
	good := []byte("good shard body")
	srvGood := rangeServer(t, good, sha256Hex(good), int64(len(good)))
	sh1 := catalog.Shard{URL: srvGood.URL, Filename: filepathName(1, 2), SHA256: sha256Hex(good), SizeBytes: uint64(len(good))}

	bad := []byte("bad shard body")
	wrong := sha256Hex([]byte("expected something else"))
	srvBad := rangeServer(t, bad, wrong, int64(len(bad)))
	sh2 := catalog.Shard{URL: srvBad.URL, Filename: filepathName(2, 2), SHA256: wrong, SizeBytes: uint64(len(bad))}

	m := catalog.Model{ID: "two-shard-mismatch", Shards: []catalog.Shard{sh1, sh2}}
	if err := pullShards(t.Context(), http.DefaultClient, m, dir); err == nil {
		t.Fatal("expected rejection when a shard mismatches, got nil")
	}
}

// TestShardsSingle: the degenerate one-shard case works through pullShards.
func TestShardsSingle(t *testing.T) {
	dir := t.TempDir()
	body := []byte("the only shard")
	srv := rangeServer(t, body, sha256Hex(body), int64(len(body)))
	m := catalog.Model{ID: "single", Shards: []catalog.Shard{makeShard(srv.URL, "only.gguf", body)}}
	if err := pullShards(t.Context(), http.DefaultClient, m, dir); err != nil {
		t.Fatalf("pullShards(single): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "only.gguf")); err != nil {
		t.Errorf("single shard final missing: %v", err)
	}
}

// TestPullModelEmptyRejected: a model with no shards is rejected.
func TestPullModelEmptyRejected(t *testing.T) {
	dir := t.TempDir()
	m := catalog.Model{ID: "no-shards"}
	if err := PullModel(t.Context(), m, dir); err == nil {
		t.Fatal("expected rejection for a model with zero shards")
	}
}

func filepathName(i, n int) string {
	return "Model-0000" + strconv.Itoa(i) + "-of-0000" + strconv.Itoa(n) + ".gguf"
}

// geometryModel serves body as the single shard of an entry declaring the given
// fit dimensions, so a pull can be driven against a real GGUF header.
func geometryModel(t *testing.T, body []byte, layers, kvHeads, headDim int) catalog.Model {
	t.Helper()
	srv := rangeServer(t, body, sha256Hex(body), int64(len(body)))
	return catalog.Model{
		ID:       "geo",
		NLayers:  layers,
		NKVHeads: kvHeads,
		HeadDim:  headDim,
		Shards:   []catalog.Shard{makeShard(srv.URL, "geo.gguf", body)},
	}
}

// denseHeader is a well-formed dense GGUF header: 48 layers, 4 KV heads, key
// length 128, every block attention-bearing.
func denseHeader() []byte {
	return gguf.FixtureForTest("llama", map[string]uint64{
		"llama.block_count":             48,
		"llama.attention.head_count_kv": 4,
		"llama.attention.key_length":    128,
	})
}

// TestPullVerifiesGeometry guards the promise that a pull whose file agrees with
// the catalog entry completes.
func TestPullVerifiesGeometry(t *testing.T) {
	dir := t.TempDir()
	m := geometryModel(t, denseHeader(), 48, 4, 128)
	if err := pullShards(t.Context(), http.DefaultClient, m, dir); err != nil {
		t.Fatalf("pullShards: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "geo.gguf")); err != nil {
		t.Errorf("final file missing after a matching pull: %v", err)
	}
}

// TestPullRefusesGeometryMismatch guards the promise that a checksum-verified file
// whose header disagrees with the catalog entry REFUSES the pull, names both
// values, and LEAVES THE FILE. The bytes are intact; the catalog entry is what
// needs fixing, and deleting a 20 GB download over a metadata disagreement would
// punish the operator for villa's bad data.
func TestPullRefusesGeometryMismatch(t *testing.T) {
	dir := t.TempDir()
	m := geometryModel(t, denseHeader(), 24, 2, 64)
	err := pullShards(t.Context(), http.DefaultClient, m, dir)
	if err == nil {
		t.Fatal("expected a refusal on a geometry mismatch, got nil")
	}
	for _, want := range []string{"n_layers=24", "kv_layers=48", "checksum"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q is missing %q", err, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dir, "geo.gguf")); statErr != nil {
		t.Errorf("the verified file was removed on a geometry mismatch: %v", statErr)
	}
}

// TestPullRefusesUnreadableHeader guards the promise that a file villa cannot
// parse as GGUF is a refusal too: the pull cannot claim the entry is confirmed
// when the witness could not be read at all.
func TestPullRefusesUnreadableHeader(t *testing.T) {
	dir := t.TempDir()
	m := geometryModel(t, []byte("this is not a GGUF file at all"), 48, 4, 128)
	if err := pullShards(t.Context(), http.DefaultClient, m, dir); err == nil {
		t.Fatal("expected a refusal on an unreadable header, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "geo.gguf")); statErr != nil {
		t.Errorf("the verified file was removed on an unreadable header: %v", statErr)
	}
}

// TestPullGeometryIsIdempotent guards the promise that the check runs on the
// already-on-disk short-circuit path too, so a rerun of a good pull stays green
// rather than passing only on the first fetch.
func TestPullGeometryIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	m := geometryModel(t, denseHeader(), 48, 4, 128)
	for i := range 2 {
		if err := pullShards(t.Context(), http.DefaultClient, m, dir); err != nil {
			t.Fatalf("pullShards run %d: %v", i+1, err)
		}
	}
}
