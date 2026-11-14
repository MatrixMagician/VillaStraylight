package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// Fetched is what Fetch learned about a file it was given no checksum for: the
// lowercase-hex SHA-256 of the bytes on disk and their count.
type Fetched struct {
	SHA256    string
	SizeBytes uint64
}

// Fetch downloads the file at url into modelsDir under filename and returns its
// SHA-256 and size (ADR-0041). It is the pull path with the checksum learned instead
// of given, for `villa catalog add`: the same HEAD and GET against the same host, the
// same .part resume, bare-filename confinement and atomic rename, so the only
// outbound request is a model pull.
//
// There is nothing to verify the bytes against, so this is trust on first use at a
// pinned URL. The one cross-check is the HEAD: an X-Linked-Etag or X-Linked-Size that
// is present and disagrees with the bytes refuses. Every later pull verifies against
// the digest this returned.
func Fetch(ctx context.Context, url, filename, modelsDir string) (Fetched, error) {
	client := &http.Client{} // no Timeout: multi-GB downloads are bounded by ctx, not a wall clock
	return fetchFile(ctx, client, catalog.Shard{URL: url, Filename: filename}, modelsDir)
}

// fetchFile is Fetch over an injectable client. sh carries only the URL and the
// filename; its SHA256 and SizeBytes are ignored.
//
// A regular file already at the final path is hashed and used rather than fetched
// again, since weights are tens of gigabytes. It is cross-checked against the HEAD
// the same way a download is, but a disagreement never deletes it: it may be
// another entry's weights under the same name, and removing a file villa did not
// just write is not villa's call.
func fetchFile(ctx context.Context, client httpDoer, sh catalog.Shard, modelsDir string) (Fetched, error) {
	finalPath, err := confineShard(sh, modelsDir)
	if err != nil {
		return Fetched{}, err
	}
	partPath := finalPath + partSuffix

	adv, err := headAdvertised(ctx, client, sh)
	if err != nil {
		return Fetched{}, err
	}

	if fi, statErr := os.Stat(finalPath); statErr == nil && fi.Mode().IsRegular() {
		got, hashErr := hashFile(finalPath)
		if hashErr != nil {
			return Fetched{}, fmt.Errorf("download: hash existing %s: %w", sh.Filename, hashErr)
		}
		if err := adv.contradicts(sh.Filename, got); err != nil {
			return Fetched{}, fmt.Errorf("%w (the file already in the models dir is kept)", err)
		}
		return got, nil
	}

	h, resumeFrom := resumeState(partPath)
	written, err := streamToPart(ctx, client, sh, partPath, h, resumeFrom)
	if err != nil {
		// The .part stays so a later add resumes from it, exactly as a pull does.
		return Fetched{}, err
	}
	got := Fetched{SHA256: hex.EncodeToString(h.Sum(nil)), SizeBytes: uint64(written)}
	if err := adv.contradicts(sh.Filename, got); err != nil {
		_ = os.Remove(partPath)
		return Fetched{}, err
	}
	if err := os.Rename(partPath, finalPath); err != nil {
		_ = os.Remove(partPath)
		return Fetched{}, fmt.Errorf("download: rename %s: %w", sh.Filename, err)
	}
	return got, nil
}

// contradicts reports whether the HEAD's etag or size, where it gave one, disagrees
// with what was actually received.
func (a advertised) contradicts(filename string, got Fetched) error {
	if a.etag != "" && !strings.EqualFold(a.etag, got.SHA256) {
		return fmt.Errorf("download: %s upstream X-Linked-Etag %s != received sha256 %s", filename, a.etag, got.SHA256)
	}
	if a.hasSize && a.size != got.SizeBytes {
		return fmt.Errorf("download: %s upstream X-Linked-Size %d != received size %d", filename, a.size, got.SizeBytes)
	}
	return nil
}

// hashFile returns the SHA-256 and size of the file at path.
func hashFile(path string) (Fetched, error) {
	f, err := os.Open(path) //nolint:gosec // path is modelsDir/<bare filename>, confined by confineShard
	if err != nil {
		return Fetched{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return Fetched{}, err
	}
	return Fetched{SHA256: hex.EncodeToString(h.Sum(nil)), SizeBytes: uint64(n)}, nil
}
