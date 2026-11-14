package download

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// fetchShard is the input Fetch is given: a URL and a name, and deliberately no
// checksum or size, which is the whole difference from the pull path.
func fetchShard(url string) catalog.Shard {
	return catalog.Shard{URL: url, Filename: "added.gguf"}
}

// TestFetchLearnsTheChecksum guards the point of Fetch (ADR-0041): with nothing to
// verify against, the file is hashed as it arrives and the digest and size are
// returned, so `catalog add` records what villa actually received.
func TestFetchLearnsTheChecksum(t *testing.T) {
	body := []byte("a model nobody has pinned a checksum for yet")
	srv := rangeServer(t, body, sha256Hex(body), int64(len(body)))
	dir := t.TempDir()

	got, err := fetchFile(t.Context(), srv.Client(), fetchShard(srv.URL), dir)
	if err != nil {
		t.Fatalf("fetchFile: %v", err)
	}
	if got.SHA256 != sha256Hex(body) || got.SizeBytes != uint64(len(body)) {
		t.Fatalf("Fetched = %+v, want sha %s size %d", got, sha256Hex(body), len(body))
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, "added.gguf"))
	if err != nil || string(onDisk) != string(body) {
		t.Fatalf("final file = %q, %v", onDisk, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "added.gguf"+partSuffix)); !os.IsNotExist(err) {
		t.Fatalf(".part must be gone after success: %v", err)
	}
}

// TestFetchToleratesAnUnadvertisedServer keeps the pull path's tolerance: a server
// that exposes neither X-Linked-Etag nor X-Linked-Size is not a failure, because the
// hash of the bytes is the authority.
func TestFetchToleratesAnUnadvertisedServer(t *testing.T) {
	body := []byte("plain server")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(srv.Close)

	got, err := fetchFile(t.Context(), srv.Client(), fetchShard(srv.URL), t.TempDir())
	if err != nil || got.SHA256 != sha256Hex(body) {
		t.Fatalf("fetchFile = %+v, %v", got, err)
	}
}

// TestFetchRefusesAdvertisedDisagreement: a header that is PRESENT and disagrees
// with the hashed bytes refuses and leaves no final file and no .part, as the pull
// path does on a mismatch.
func TestFetchRefusesAdvertisedDisagreement(t *testing.T) {
	body := []byte("what the server really sent")
	tests := []struct {
		name string
		etag string
		size int64
	}{
		{"etag disagrees", sha256Hex([]byte("something else")), int64(len(body))},
		{"size disagrees", sha256Hex(body), int64(len(body)) + 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := rangeServer(t, body, tt.etag, tt.size)
			dir := t.TempDir()
			if _, err := fetchFile(t.Context(), srv.Client(), fetchShard(srv.URL), dir); err == nil {
				t.Fatal("fetchFile accepted a file its server's headers contradict")
			}
			for _, name := range []string{"added.gguf", "added.gguf" + partSuffix} {
				if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Errorf("%s must not remain after a refusal: %v", name, err)
				}
			}
		})
	}
}

// TestFetchRefusesAFailedTransfer: a non-2xx GET writes no final file.
func TestFetchRefusesAFailedTransfer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			return
		}
		http.Error(w, "gone", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()

	if _, err := fetchFile(t.Context(), srv.Client(), fetchShard(srv.URL), dir); err == nil {
		t.Fatal("a 404 body was accepted as a model")
	}
	if _, err := os.Stat(filepath.Join(dir, "added.gguf")); !os.IsNotExist(err) {
		t.Fatalf("final file written after a failed GET: %v", err)
	}
}

// TestFetchHashesAFileAlreadyOnDisk: weights are tens of gigabytes, so a file already
// in the models dir under that name is hashed and used and the body is not fetched
// again. The HEAD still runs, because it is the only cross-check an existing file gets.
func TestFetchHashesAFileAlreadyOnDisk(t *testing.T) {
	body := []byte("already pulled by an earlier `model pull`")
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Linked-Etag", sha256Hex(body))
		w.Header().Set("X-Linked-Size", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			gets.Add(1)
		}
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "added.gguf"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := fetchFile(t.Context(), srv.Client(), fetchShard(srv.URL), dir)
	if err != nil || got.SHA256 != sha256Hex(body) || got.SizeBytes != uint64(len(body)) {
		t.Fatalf("fetchFile = %+v, %v", got, err)
	}
	if gets.Load() != 0 {
		t.Fatalf("a present file was downloaded again (%d GET)", gets.Load())
	}
}

// TestFetchKeepsAPresentFileItDisagreesWith: an on-disk file whose hash the server's
// etag contradicts is refused but never deleted, because it may be another entry's
// weights under the same name and is not villa's to remove.
func TestFetchKeepsAPresentFileItDisagreesWith(t *testing.T) {
	body := []byte("a different model that happens to share the file name")
	srv := rangeServer(t, []byte("the url's real content"), sha256Hex([]byte("the url's real content")), 22)
	dir := t.TempDir()
	path := filepath.Join(dir, "added.gguf")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := fetchFile(t.Context(), srv.Client(), fetchShard(srv.URL), dir); err == nil {
		t.Fatal("accepted a present file the server's etag contradicts")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(body) {
		t.Fatalf("the present file was touched: %q, %v", got, err)
	}
}

// TestFetchResumesAPart: the .part left by an interrupted add continues by Range and
// the digest covers the whole file, not just the resumed suffix.
func TestFetchResumesAPart(t *testing.T) {
	body := []byte("0123456789 the rest of the model arrives after the interruption")
	srv := rangeServer(t, body, sha256Hex(body), int64(len(body)))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "added.gguf"+partSuffix), body[:10], 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := fetchFile(t.Context(), srv.Client(), fetchShard(srv.URL), dir)
	if err != nil || got.SHA256 != sha256Hex(body) {
		t.Fatalf("fetchFile = %+v, %v", got, err)
	}
}

// TestFetchConfinesTheFilename: the file name comes from a URL the operator typed,
// so the traversal guard the pull path has applies here unchanged.
func TestFetchConfinesTheFilename(t *testing.T) {
	for _, name := range []string{"../escape.gguf", "sub/dir.gguf", "/abs.gguf"} {
		sh := catalog.Shard{URL: "http://unreachable.invalid/x", Filename: name}
		if _, err := fetchFile(t.Context(), roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("a request was made for an unconfined name")
			return nil, os.ErrInvalid
		}), sh, t.TempDir()); err == nil {
			t.Errorf("accepted filename %q", name)
		}
	}
}
