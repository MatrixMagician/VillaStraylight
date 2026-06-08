package backup

// tarutil.go is the pure outer-tar (single plain POSIX .tar, D-03) assembly and
// extraction primitive, plus the tar-slip traversal guard (D-11). It operates
// over an injected io.Writer / io.Reader so the file handles stay a cmd-tier
// seam; the tar logic itself is deterministic and host-I/O-free. The
// assertInsideDir guard is cloned (NOT imported) from config.assertInsideDir —
// importing config solely for an unexported guard would widen this pure core's
// deps, exactly as internal/usage cloned the same shape.

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// archiveFileMode / archiveDirMode are the owner-only modes for written archive
// entries and any created extraction dir (D-11), mirroring
// usage.storeFileMode/storeDirMode.
const (
	archiveFileMode os.FileMode = 0o600
	archiveDirMode  os.FileMode = 0o700
)

// Bounded-read caps for the UNTRUSTED read path (WR-04). readArchive holds every
// entry in memory (config/usage/bench are small; the Open WebUI volume tar can be
// large but is bounded), so an attacker-crafted or accidentally-huge .tar must NOT
// be read unboundedly into RAM — an OOM is an availability failure, not mere
// slowness. These mirror the bounded-read discipline benchstore.Load uses (its
// 1 MiB/line scanner cap, T-14-03), sized GENEROUSLY here because the OWUI volume
// entry legitimately holds a real chat database:
//   - maxEntryBytes caps a SINGLE entry body.
//   - maxArchiveBytes caps the SUM of all entry bodies.
//   - maxEntryCount caps the number of members (manifest + 4 data entries today;
//     the cap leaves generous slack for future entries while refusing an absurd
//     member count).
//
// Exceeding any cap is a fail-closed refusal BEFORE the bytes are handed to fn, so
// nothing downstream ever sees an over-bound archive.
const (
	maxEntryBytes   int64 = 8 << 30  // 8 GiB per entry (the OWUI volume tar)
	maxArchiveBytes int64 = 16 << 30 // 16 GiB total across all entries
	maxEntryCount         = 64       // generous slack over today's 5 entries
)

// archiveEntry is one in-memory outer-tar member: its archive name and its
// bytes. The whole archive is small (model weights are excluded — BAK-01), so
// entries are held in memory; assembly is deterministic.
type archiveEntry struct {
	name string
	data []byte
}

// writeArchive emits entries to w as a single plain POSIX tar. The caller MUST
// pass entries in the deterministic order with manifest.json FIRST (so a reader
// can parse the manifest before validating the rest); writeArchive preserves the
// given order and does not sort. Each header uses tar.FormatPAX and Mode 0o600
// (D-03/D-11).
func writeArchive(w io.Writer, entries []archiveEntry) error {
	tw := tar.NewWriter(w)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:   e.name,
			Mode:   int64(archiveFileMode),
			Size:   int64(len(e.data)),
			Format: tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("backup: write tar header %q: %w", e.name, err)
		}
		if _, err := tw.Write(e.data); err != nil {
			return fmt.Errorf("backup: write tar body %q: %w", e.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("backup: close tar: %w", err)
	}
	return nil
}

// readArchive iterates the tar in r in stream order, validating EVERY entry name
// with the tar-slip guard (relative to a notional extraction root) BEFORE
// invoking fn — so a malicious "../escape" or absolute-path entry is refused with
// an error naming the entry, before any caller side effect (D-11). fn receives
// the validated name and the entry bytes; an error from fn (or the guard) aborts
// the iteration.
func readArchive(r io.Reader, fn func(name string, data []byte) error) error {
	tr := tar.NewReader(r)
	var (
		count      int
		totalBytes int64
	)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("backup: read tar: %w", err)
		}
		// Entry-count cap (WR-04): refuse an absurd member count before reading bodies.
		count++
		if count > maxEntryCount {
			return fmt.Errorf("backup: archive has more than %d entries — refusing (possible hostile or malformed tar)", maxEntryCount)
		}
		// Validate the entry name against a notional extraction dir so the same
		// filepath.Rel escape check the live extractor uses also fails here, with the
		// archive even partially trusted (D-11). "." stands in for "the extraction
		// root"; the joined dst must resolve inside it.
		if err := assertEntryInside(hdr.Name, "."); err != nil {
			return fmt.Errorf("backup: refusing tar entry %q: %w", hdr.Name, err)
		}
		// Bounded per-entry read (WR-04): cap a single body at maxEntryBytes via an
		// io.LimitReader of maxEntryBytes+1 — if the read yields more than
		// maxEntryBytes, the entry is over-bound and we refuse. This caps memory even
		// for a header that lies about Size (we never trust hdr.Size for allocation).
		data, err := io.ReadAll(io.LimitReader(tr, maxEntryBytes+1))
		if err != nil {
			return fmt.Errorf("backup: read tar body %q: %w", hdr.Name, err)
		}
		if int64(len(data)) > maxEntryBytes {
			return fmt.Errorf("backup: tar entry %q exceeds the %d-byte per-entry cap — refusing", hdr.Name, maxEntryBytes)
		}
		// Total-archive cap (WR-04): the SUM of all entry bodies must stay bounded.
		totalBytes += int64(len(data))
		if totalBytes > maxArchiveBytes {
			return fmt.Errorf("backup: archive exceeds the %d-byte total cap — refusing", maxArchiveBytes)
		}
		if err := fn(hdr.Name, data); err != nil {
			return err
		}
	}
}

// assertEntryInside validates that an archive entry NAME, when joined under dir,
// resolves inside dir — refusing traversal escapes ("../x") and absolute paths.
// It is the per-entry tar-slip guard (D-11), cloned from config.assertInsideDir.
//
// An absolute entry name is rejected explicitly FIRST: filepath.Join cleans a
// leading separator away ("/etc/passwd" → "etc/passwd"), so without this guard
// an absolute-path entry would silently be treated as relative and pass.
func assertEntryInside(name, dir string) error {
	if filepath.IsAbs(name) {
		return fmt.Errorf("backup: refusing absolute entry name %q", name)
	}
	dst := filepath.Join(dir, name)
	return assertInsideDir(dst, dir)
}

// assertInsideDir verifies path resolves within dir, rejecting traversal escapes
// (D-11). Cloned (not imported) from config.assertInsideDir — config's is
// unexported and importing config solely for it would widen this pure core's
// deps (same rationale as usage.assertInsideDir).
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
		return fmt.Errorf("backup: refusing %q outside dir %q", absPath, absDir)
	}
	return nil
}
