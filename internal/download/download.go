// Package download implements `villa model pull`'s on-disk acquisition of a
// catalog-resolved GGUF (MODEL-02). villa downloads the file itself via
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
//  5. On match os.Rename(.part, final) — atomic on the same filesystem.
//     On mismatch os.Remove(.part) and return an error — never leave a half-written
//     model on disk.
//
// Integrity is verified ONLY against shard.SHA256 (= HF X-Linked-Etag, the git-LFS
// oid), NEVER the CDN/Xet chunk ETag (Pitfall 2). The canonical HF URL is the only
// URL logged; the resolved cas-bridge signed redirect target (which carries a
// signature) is never logged (Pitfall 8 / signed-URL-leak threat).
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
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
)

// partSuffix is appended to the final filename while a download is in flight. The
// final path appears only after SHA256 + size both verify (atomic rename).
const partSuffix = ".part"

// maxErrBody bounds how much of an unexpected HTTP error body we read into an
// error message, so a hostile/huge response cannot exhaust memory. This
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
func PullModel(ctx context.Context, m catalog.Model, modelsDir string) error {
	client := &http.Client{} // no Timeout: multi-GB downloads are bounded by ctx, not a wall clock
	return pullShards(ctx, client, m, modelsDir)
}

// confineShard resolves the final on-disk path of sh inside modelsDir, refusing
// anything but a bare filename. The catalog filename is untrusted enough to guard
// against `..`/absolute escapes, and a name with separators would write into a
// subdirectory: the models dir is flat.
func confineShard(sh catalog.Shard, modelsDir string) (string, error) {
	finalPath := filepath.Join(modelsDir, sh.Filename)
	if confErr := assertInsideDir(finalPath, modelsDir); confErr != nil {
		return "", confErr
	}
	if strings.ContainsRune(sh.Filename, os.PathSeparator) || sh.Filename != filepath.Base(sh.Filename) {
		return "", fmt.Errorf("download: shard filename %q must be a bare filename", sh.Filename)
	}
	return finalPath, nil
}

// downloadFile acquires a single shard into modelsDir: HEAD-verify metadata,
// resume from any existing .part via Range, stream+hash, verify SHA256+size, and
// atomically rename on success (deleting the .part and erroring on any mismatch).
func downloadFile(ctx context.Context, client httpDoer, sh catalog.Shard, modelsDir string) (err error) {
	// Confine the final path inside modelsDir BEFORE any write.
	finalPath, err := confineShard(sh, modelsDir)
	if err != nil {
		return err
	}
	partPath := finalPath + partSuffix

	// A shard already on disk at the catalog's size is not fetched again. The
	// sidecar made "some of the entry's files are present" a normal state, and
	// the primary shard is 20+ GB.
	if fi, statErr := os.Stat(finalPath); statErr == nil && uint64(fi.Size()) == sh.SizeBytes {
		return nil
	}

	// (1) HEAD defense-in-depth: confirm upstream still advertises the size+etag we
	// recorded in the catalog, before pulling gigabytes.
	if headErr := headVerify(ctx, client, sh); headErr != nil {
		return headErr
	}

	// (2) Resume: if a .part exists, seed the hash from it and request the suffix.
	h, resumeFrom := resumeState(partPath)

	// (3) Stream the body.
	written, dlErr := streamToPart(ctx, client, sh, partPath, h, resumeFrom)
	if dlErr != nil {
		// Leave the .part in place so a later invocation can resume from it; only a
		// verify failure (below) deletes it.
		return dlErr
	}

	// (4) Verify size then checksum. On ANY mismatch, delete the partial.
	if uint64(written) != sh.SizeBytes {
		_ = os.Remove(partPath)
		return fmt.Errorf("download: %s size mismatch: got %d bytes, want %d", sh.Filename, written, sh.SizeBytes)
	}
	gotSum := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(gotSum, sh.SHA256) {
		_ = os.Remove(partPath)
		return fmt.Errorf("download: %s checksum mismatch: got %s, want %s", sh.Filename, gotSum, sh.SHA256)
	}

	// (5) Atomic rename — the final path appears only now (same filesystem).
	if rnErr := os.Rename(partPath, finalPath); rnErr != nil {
		_ = os.Remove(partPath)
		return fmt.Errorf("download: rename %s: %w", sh.Filename, rnErr)
	}
	return nil
}

// advertised is what an upstream HEAD says about a file. Each field is optional: a
// server that does not expose X-Linked-Etag or X-Linked-Size (a plain fixture, or a
// CDN hop that dropped them) advertises nothing, and the hash of the bytes is then
// the only authority.
type advertised struct {
	etag    string
	size    uint64
	hasSize bool
}

// headAdvertised issues a HEAD to the canonical URL and returns what it exposes.
func headAdvertised(ctx context.Context, client httpDoer, sh catalog.Shard) (advertised, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, sh.URL, nil)
	if err != nil {
		return advertised{}, fmt.Errorf("download: build HEAD for %s: %w", sh.Filename, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return advertised{}, fmt.Errorf("download: HEAD %s: %w", sh.Filename, err)
	}
	defer drainClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return advertised{}, fmt.Errorf("download: HEAD %s: unexpected status %d", sh.Filename, resp.StatusCode)
	}
	a := advertised{etag: strings.Trim(resp.Header.Get("X-Linked-Etag"), "\"")}
	if sz := resp.Header.Get("X-Linked-Size"); sz != "" {
		if n, convErr := strconv.ParseUint(sz, 10, 64); convErr == nil {
			a.size, a.hasSize = n, true
		}
	}
	return a, nil
}

// headVerify confirms the advertised X-Linked-Size / X-Linked-Etag match the
// catalog values. A header that is PRESENT and DISAGREES fails fast, before
// gigabytes are pulled; the authoritative check is still the post-download
// SHA256+size.
func headVerify(ctx context.Context, client httpDoer, sh catalog.Shard) error {
	a, err := headAdvertised(ctx, client, sh)
	if err != nil {
		return err
	}
	if a.etag != "" && !strings.EqualFold(a.etag, sh.SHA256) {
		return fmt.Errorf("download: %s upstream X-Linked-Etag %s != catalog sha256 %s (re-uploaded?)", sh.Filename, a.etag, sh.SHA256)
	}
	if a.hasSize && a.size != sh.SizeBytes {
		return fmt.Errorf("download: %s upstream X-Linked-Size %d != catalog size %d (re-uploaded?)", sh.Filename, a.size, sh.SizeBytes)
	}
	return nil
}

// resumeState returns the rolling hash and byte offset a transfer into partPath
// continues from: seeded from an existing .part, or fresh when there is none or it
// cannot be read (a corrupt .part is discarded and the transfer restarts clean).
func resumeState(partPath string) (hash.Hash, int64) {
	h := sha256.New()
	fi, statErr := os.Stat(partPath)
	if statErr != nil || fi.Size() == 0 {
		return h, 0
	}
	seeded, seedErr := seedHashFromPart(h, partPath)
	if seedErr != nil {
		_ = os.Remove(partPath)
		return sha256.New(), 0
	}
	return h, seeded
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
// message (never read an unbounded response into memory).
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
// Copied verbatim from internal/config so the downloader has no
// dependency on an unexported config helper; both are cleaned and compared as
// absolute paths.
func assertInsideDir(path, dir string) error {
	if err := pathsafe.Inside(path, dir); err != nil {
		return fmt.Errorf("download: refusing to write outside the models dir %q: %w", dir, err)
	}
	return nil
}

// errNoShards is returned when a model carries no download manifest.
var errNoShards = errors.New("download: model has no shards")
