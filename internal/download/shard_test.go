package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
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
	m := catalog.CatalogModel{ID: "three-shard", Shards: shards}
	if err := pullShards(context.Background(), http.DefaultClient, m, dir); err != nil {
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
	m := catalog.CatalogModel{ID: "two-shard-missing", Shards: []catalog.Shard{sh1, sh2}}
	if err := pullShards(context.Background(), http.DefaultClient, m, dir); err == nil {
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

	m := catalog.CatalogModel{ID: "two-shard-mismatch", Shards: []catalog.Shard{sh1, sh2}}
	if err := pullShards(context.Background(), http.DefaultClient, m, dir); err == nil {
		t.Fatal("expected rejection when a shard mismatches, got nil")
	}
}

// TestShardsSingle: the degenerate one-shard case works through pullShards.
func TestShardsSingle(t *testing.T) {
	dir := t.TempDir()
	body := []byte("the only shard")
	srv := rangeServer(t, body, sha256Hex(body), int64(len(body)))
	m := catalog.CatalogModel{ID: "single", Shards: []catalog.Shard{makeShard(srv.URL, "only.gguf", body)}}
	if err := pullShards(context.Background(), http.DefaultClient, m, dir); err != nil {
		t.Fatalf("pullShards(single): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "only.gguf")); err != nil {
		t.Errorf("single shard final missing: %v", err)
	}
}

// TestPullModelEmptyRejected: a model with no shards is rejected.
func TestPullModelEmptyRejected(t *testing.T) {
	dir := t.TempDir()
	m := catalog.CatalogModel{ID: "no-shards"}
	if err := PullModel(context.Background(), m, dir); err == nil {
		t.Fatal("expected rejection for a model with zero shards")
	}
}

func filepathName(i, n int) string {
	return "Model-0000" + strconv.Itoa(i) + "-of-0000" + strconv.Itoa(n) + ".gguf"
}
