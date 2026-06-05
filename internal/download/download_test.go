package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// sha256Hex returns the lowercase hex SHA256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// rangeServer returns an httptest.Server that serves body for GET (honoring a
// `Range: bytes=N-` request by returning the suffix from N with 206) and answers
// HEAD with X-Linked-Size + X-Linked-Etag + Accept-Ranges. headEtag/headSize let
// a test deliberately advertise mismatched metadata.
func rangeServer(t *testing.T, body []byte, headEtag string, headSize int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Linked-Etag", headEtag)
		w.Header().Set("X-Linked-Size", strconv.FormatInt(headSize, 10))
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		start := 0
		if rng := r.Header.Get("Range"); rng != "" {
			// Expect "bytes=N-"
			spec := strings.TrimPrefix(rng, "bytes=")
			spec = strings.TrimSuffix(spec, "-")
			n, err := strconv.Atoi(spec)
			if err != nil {
				t.Errorf("server: bad Range header %q", rng)
			}
			start = n
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(body)-1, len(body)))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_, _ = w.Write(body[start:])
	}))
	t.Cleanup(srv.Close)
	return srv
}

func makeShard(url, filename string, body []byte) catalog.Shard {
	return catalog.Shard{
		URL:       url,
		Filename:  filename,
		SHA256:    sha256Hex(body),
		SizeBytes: uint64(len(body)),
	}
}

// TestResume: a `.part` already on disk causes a Range request, and the final
// file equals the full content.
func TestResume(t *testing.T) {
	body := []byte("the quick brown fox jumps over the lazy dog, repeatedly and verifiably")
	srv := rangeServer(t, body, sha256Hex(body), int64(len(body)))
	dir := t.TempDir()

	// Pre-seed a partial .part with the first 10 bytes.
	final := filepath.Join(dir, "model.gguf")
	part := final + partSuffix
	if err := os.WriteFile(part, body[:10], 0o600); err != nil {
		t.Fatal(err)
	}

	sh := makeShard(srv.URL, "model.gguf", body)
	if err := downloadFile(context.Background(), srv.Client(), sh, dir); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("final content mismatch after resume:\n got %q\nwant %q", got, body)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Errorf(".part should be gone after success, stat err=%v", err)
	}
}

// TestVerifyRejectsMismatch: a body whose SHA256 != catalog.sha256 deletes the
// .part and errors — no final file.
func TestVerifyRejectsMismatch(t *testing.T) {
	body := []byte("authentic model bytes")
	wrong := sha256Hex([]byte("totally different content"))
	srv := rangeServer(t, body, wrong, int64(len(body)))
	dir := t.TempDir()

	sh := catalog.Shard{
		URL:       srv.URL,
		Filename:  "model.gguf",
		SHA256:    wrong, // catalog expects the wrong hash → downloaded bytes won't match
		SizeBytes: uint64(len(body)),
	}
	err := downloadFile(context.Background(), srv.Client(), sh, dir)
	if err == nil {
		t.Fatal("expected a checksum-mismatch error, got nil")
	}
	final := filepath.Join(dir, "model.gguf")
	if _, statErr := os.Stat(final); !os.IsNotExist(statErr) {
		t.Errorf("final model must NOT exist on mismatch, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(final + partSuffix); !os.IsNotExist(statErr) {
		t.Errorf(".part must be deleted on mismatch, stat err=%v", statErr)
	}
}

// TestVerifyRejectsSizeMismatch: a body whose total bytes != catalog.size is
// rejected even if the (truncated) hash were to coincide; the size guard fires.
func TestVerifyRejectsSizeMismatch(t *testing.T) {
	body := []byte("twelve bytes!") // 13 bytes
	srv := rangeServer(t, body, sha256Hex(body), int64(len(body)))
	dir := t.TempDir()
	sh := catalog.Shard{
		URL:       srv.URL,
		Filename:  "model.gguf",
		SHA256:    sha256Hex(body),
		SizeBytes: uint64(len(body)) + 100, // wrong expected size
	}
	if err := downloadFile(context.Background(), srv.Client(), sh, dir); err == nil {
		t.Fatal("expected a size-mismatch error, got nil")
	}
	if _, err := os.Stat(filepath.Join(dir, "model.gguf")); !os.IsNotExist(err) {
		t.Errorf("final model must NOT exist on size mismatch")
	}
}

// TestAtomicRename: while the download is in flight the final path does NOT exist
// (only .part); it appears only after verify. We assert this by checking that a
// mid-stream observer never sees the final path until success, approximated by
// confirming the final never exists before downloadFile returns on a forced-slow
// body and exists only after.
func TestAtomicRename(t *testing.T) {
	body := []byte(strings.Repeat("abcdefgh", 4096)) // 32 KiB
	final := "model.gguf"
	dir := t.TempDir()
	finalPath := filepath.Join(dir, final)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Linked-Etag", sha256Hex(body))
		w.Header().Set("X-Linked-Size", strconv.Itoa(len(body)))
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		// Write the first half, assert the final path does NOT yet exist, then finish.
		half := len(body) / 2
		_, _ = w.Write(body[:half])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
			t.Errorf("final path existed mid-download (not atomic): stat err=%v", err)
		}
		_, _ = w.Write(body[half:])
	}))
	t.Cleanup(srv.Close)

	sh := makeShard(srv.URL, final, body)
	if err := downloadFile(context.Background(), srv.Client(), sh, dir); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Errorf("final path should exist after success: %v", err)
	}
}

// TestRejectsTraversalFilename: a shard filename containing traversal/absolute
// components is rejected before any write.
func TestRejectsTraversalFilename(t *testing.T) {
	dir := t.TempDir()
	body := []byte("x")
	srv := rangeServer(t, body, sha256Hex(body), 1)
	for _, name := range []string{"../escape.gguf", "../../etc/passwd", "/abs/model.gguf"} {
		sh := catalog.Shard{URL: srv.URL, Filename: name, SHA256: sha256Hex(body), SizeBytes: 1}
		if err := downloadFile(context.Background(), srv.Client(), sh, dir); err == nil {
			t.Errorf("filename %q: expected path-confinement rejection, got nil", name)
		}
	}
}
