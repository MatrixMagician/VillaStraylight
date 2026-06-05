// Package download implements `villa model pull`'s on-disk acquisition of a
// catalog-resolved GGUF (MODEL-02, D-05/D-06). villa downloads the file itself via
// the Go standard library — it does NOT delegate to llama.cpp `-hf` — so the
// resume/atomic/per-shard-checksum guarantees are owned here and are runner-
// agnostic and independently testable.
//
// The core loop (RESEARCH Pattern 2):
//
//  1. HEAD the resolve URL → confirm X-Linked-Size == shard.SizeBytes and
//     X-Linked-Etag == shard.SHA256 (defense-in-depth: catch an upstream re-upload
//     before downloading gigabytes).
//  2. stat <file>.part → if present, set Range: bytes=<len>- to resume.
//  3. GET (http.Client follows the signed-CDN redirect automatically); stream the
//     body into the .part while hashing incrementally, seeding the hash from the
//     existing .part bytes on resume.
//  4. On completion assert total-written == shard.SizeBytes AND hex(sum) ==
//     shard.SHA256.
//  5. On match os.Rename(.part, final) — atomic on the same filesystem (D-06).
//     On mismatch os.Remove(.part) and return an error — never leave a half-written
//     model on disk.
//
// Integrity is verified ONLY against shard.SHA256 (= HF X-Linked-Etag, the git-LFS
// oid), NEVER the CDN/Xet chunk ETag (Pitfall 2). The canonical HF URL is the only
// URL logged; the resolved cas-bridge signed redirect target (which carries a
// signature) is never logged (Pitfall 8 / signed-URL-leak threat T-02-03).
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// partSuffix is appended to the final filename while a download is in flight. The
// final path appears only after SHA256 + size both verify (atomic rename, D-06).
const partSuffix = ".part"

// maxErrBody bounds how much of an unexpected HTTP error body we read into an
// error message, so a hostile/huge response cannot exhaust memory (T-02-05). This
// mirrors the bounded-read discipline in internal/catalog/load.go.
const maxErrBody = 8 << 10 // 8 KiB

// httpDoer is the subset of *http.Client the downloader needs; it lets tests pass
// the httptest server's client.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// PullModel downloads and verifies every shard of m into modelsDir, returning an
// error unless all shards are present and individually checksum+size verified. A
// single-file model is the degenerate one-shard case. modelsDir must already
// exist (the caller creates it 0700).
func PullModel(ctx context.Context, m catalog.CatalogModel, modelsDir string) error {
	client := &http.Client{} // no Timeout: multi-GB downloads are bounded by ctx, not a wall clock
	return pullShards(ctx, client, m, modelsDir)
}

// downloadFile acquires a single shard into modelsDir: HEAD-verify metadata,
// resume from any existing .part via Range, stream+hash, verify SHA256+size, and
// atomically rename on success (deleting the .part and erroring on any mismatch).
func downloadFile(ctx context.Context, client httpDoer, sh catalog.Shard, modelsDir string) (err error) {
	// Confine the final path inside modelsDir BEFORE any write (T-02-02). The
	// catalog filename is untrusted enough to guard against `..`/absolute escapes.
	finalPath := filepath.Join(modelsDir, sh.Filename)
	if confErr := assertInsideDir(finalPath, modelsDir); confErr != nil {
		return confErr
	}
	// A filename that introduces path separators (subdirectories) is also rejected:
	// we write a flat file into modelsDir.
	if strings.ContainsRune(sh.Filename, os.PathSeparator) || sh.Filename != filepath.Base(sh.Filename) {
		return fmt.Errorf("download: shard filename %q must be a bare filename", sh.Filename)
	}
	partPath := finalPath + partSuffix

	// (1) HEAD defense-in-depth: confirm upstream still advertises the size+etag we
	// recorded in the catalog, before pulling gigabytes.
	if headErr := headVerify(ctx, client, sh); headErr != nil {
		return headErr
	}

	// (2) Resume: if a .part exists, seed the hash from it and request the suffix.
	h := sha256.New()
	var resumeFrom int64
	if fi, statErr := os.Stat(partPath); statErr == nil && fi.Size() > 0 {
		seeded, seedErr := seedHashFromPart(h, partPath)
		if seedErr != nil {
			// A corrupt/unreadable .part: discard it and restart clean.
			_ = os.Remove(partPath)
			h = sha256.New()
			resumeFrom = 0
		} else {
			resumeFrom = seeded
		}
	}

	written, dlErr := streamToPart(ctx, client, sh, partPath, h, resumeFrom)
	if dlErr != nil {
		// Leave the .part in place so a later invocation can resume from it; only a
		// verify failure (below) deletes it.
		return dlErr
	}

	// (4) Verify size then checksum. On ANY mismatch, delete the partial (D-06).
	if uint64(written) != sh.SizeBytes {
		_ = os.Remove(partPath)
		return fmt.Errorf("download: %s size mismatch: got %d bytes, want %d", sh.Filename, written, sh.SizeBytes)
	}
	gotSum := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(gotSum, sh.SHA256) {
		_ = os.Remove(partPath)
		return fmt.Errorf("download: %s checksum mismatch: got %s, want %s", sh.Filename, gotSum, sh.SHA256)
	}

	// (5) Atomic rename — the final path appears only now (same filesystem, D-06).
	if rnErr := os.Rename(partPath, finalPath); rnErr != nil {
		_ = os.Remove(partPath)
		return fmt.Errorf("download: rename %s: %w", sh.Filename, rnErr)
	}
	return nil
}

// headVerify issues a HEAD to the canonical URL and confirms the advertised
// X-Linked-Size / X-Linked-Etag match the catalog values. A server that does not
// expose these headers (e.g. a plain fixture) is tolerated — the authoritative
// check is the post-download SHA256+size — but a header that is PRESENT and
// DISAGREES fails fast.
func headVerify(ctx context.Context, client httpDoer, sh catalog.Shard) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, sh.URL, nil)
	if err != nil {
		return fmt.Errorf("download: build HEAD for %s: %w", sh.Filename, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: HEAD %s: %w", sh.Filename, err)
	}
	defer drainClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HEAD %s: unexpected status %d", sh.Filename, resp.StatusCode)
	}
	if etag := resp.Header.Get("X-Linked-Etag"); etag != "" {
		etag = strings.Trim(etag, "\"")
		if !strings.EqualFold(etag, sh.SHA256) {
			return fmt.Errorf("download: %s upstream X-Linked-Etag %s != catalog sha256 %s (re-uploaded?)", sh.Filename, etag, sh.SHA256)
		}
	}
	if sz := resp.Header.Get("X-Linked-Size"); sz != "" {
		n, convErr := strconv.ParseUint(sz, 10, 64)
		if convErr == nil && n != sh.SizeBytes {
			return fmt.Errorf("download: %s upstream X-Linked-Size %d != catalog size %d (re-uploaded?)", sh.Filename, n, sh.SizeBytes)
		}
	}
	return nil
}

// seedHashFromPart feeds the existing .part bytes into h and returns the byte
// count, so a resumed download continues the same rolling SHA256.
func seedHashFromPart(h hash.Hash, partPath string) (int64, error) {
	f, err := os.Open(partPath) //nolint:gosec // partPath is modelsDir/<base>+".part", confined above
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// streamToPart performs the GET (with a Range header when resumeFrom>0), appends
// the body to partPath, and hashes it incrementally. It returns the TOTAL bytes
// in the .part after the transfer (resumeFrom + bytes written this call).
func streamToPart(ctx context.Context, client httpDoer, sh catalog.Shard, partPath string, h hash.Hash, resumeFrom int64) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sh.URL, nil)
	if err != nil {
		return 0, fmt.Errorf("download: build GET for %s: %w", sh.Filename, err)
	}
	if resumeFrom > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeFrom))
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download: GET %s: %w", sh.Filename, err)
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		// Server ignored the Range (or none requested): the body is the WHOLE file,
		// so any seeded hash/offset is invalid — restart the .part from scratch.
		if resumeFrom > 0 {
			h.Reset()
			resumeFrom = 0
		}
	case http.StatusPartialContent:
		// Body is the requested suffix; keep the seeded hash + existing .part bytes.
	default:
		return 0, fmt.Errorf("download: GET %s: unexpected status %d (%s)", sh.Filename, resp.StatusCode, snippet(resp.Body))
	}

	flag := os.O_CREATE | os.O_WRONLY
	if resumeFrom > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(partPath, flag, 0o600) //nolint:gosec // partPath confined to modelsDir above
	if err != nil {
		return 0, fmt.Errorf("download: open part %s: %w", sh.Filename, err)
	}
	written, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return resumeFrom + written, fmt.Errorf("download: stream %s: %w", sh.Filename, copyErr)
	}
	if closeErr != nil {
		return resumeFrom + written, fmt.Errorf("download: close part %s: %w", sh.Filename, closeErr)
	}
	return resumeFrom + written, nil
}

// snippet reads a bounded prefix of an error body for inclusion in an error
// message (T-02-05 — never read an unbounded response into memory).
func snippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, maxErrBody))
	return strings.TrimSpace(string(b))
}

// drainClose drains a bounded amount of the body then closes it, so the
// connection can be reused without reading an unbounded leftover.
func drainClose(rc io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(rc, maxErrBody))
	_ = rc.Close()
}

// assertInsideDir verifies path resolves within dir, rejecting traversal escapes
// (T-02-02). Copied verbatim from internal/config so the downloader has no
// dependency on an unexported config helper; both are cleaned and compared as
// absolute paths.
func assertInsideDir(path, dir string) error {
	absDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("download: refusing to write %q outside models dir %q", absPath, absDir)
	}
	return nil
}

// errNoShards is returned when a model carries no download manifest.
var errNoShards = errors.New("download: model has no shards")
