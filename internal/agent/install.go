package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// install.go is the checksum-BEFORE-extract install seam (AGENT-01 live half, D-03)
// for the pinned Crush binary. It is the package-local half of the install flow:
// it streams the downloaded asset bytes while rolling a SHA-256, asserts the
// pinned size AND checksum (via the pure VerifyTarball gate) BEFORE any extraction,
// then extracts ONLY the `crush` binary entry into the villa-owned bin dir, confined
// by a traversal guard. It NEVER shells out to `tar` (T-26-08) and NEVER places an
// unverified binary on disk (T-26-07). Phase 27 wires this into the `villa install`
// addon; Plan 26-02 ships the verified seam + tests.
//
// It clones internal/download's discipline (size-then-EqualFold-checksum fail-closed
// verify; assertInsideDir traversal confinement) without taking a host dependency on
// the downloader package. The streaming read is bounded by the pinned asset.Size so a
// hostile/oversized payload cannot exhaust memory before the size check trips.

// crushBinaryName is the single tar entry the install seam extracts — the Crush
// binary itself. Every other entry (LICENSE, README, completions) is skipped; only
// the executable is placed at binDir/crush.
const crushBinaryName = "crush"

// binaryFileMode is the mode the extracted binary is written with (executable,
// owner-only write); binDirMode is the owner-only dir mode for the villa bin dir.
const (
	binaryFileMode = 0o755
	binDirMode     = 0o700
)

// Install streams the downloaded tarball bytes from r, verifies them against the
// pinned asset (size THEN SHA-256, via VerifyTarball) BEFORE any extraction, and on
// a verify-pass extracts ONLY the `crush` binary entry into binDir as binDir/crush
// (0755), confined by an assertInsideDir traversal guard. It refuses-with-remediation
// on a checksum/size mismatch and NEVER extracts unverified bytes (D-03/T-26-07).
//
// The whole tarball is buffered (bounded by asset.Size + a small slack) so the
// checksum can be asserted over the COMPLETE payload before a single byte is
// extracted — checksum-before-place, never a streaming-extract that could place a
// partial/forged binary. Returns the absolute path to the placed binary on success.
func Install(asset CrushAsset, r io.Reader, binDir string) (string, error) {
	// Bound the read: a payload larger than the pinned size is rejected before we
	// buffer it all. +1 byte lets us DETECT an oversized stream (read returns the
	// extra byte → size mismatch in VerifyTarball) without unbounded memory growth.
	limited := io.LimitReader(r, int64(asset.Size)+1)
	got, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("agent: read Crush tarball: %w", err)
	}

	// (1) VERIFY BEFORE EXTRACT (D-03). VerifyTarball asserts size then SHA-256,
	// refusing-with-remediation on any mismatch. No extraction happens until this
	// passes — an unverified/forged tarball never reaches the tar reader (T-26-07).
	if err := VerifyTarball(asset, got); err != nil {
		return "", err
	}

	// (2) Extract ONLY the crush binary, confined to binDir (T-26-08).
	if err := os.MkdirAll(binDir, binDirMode); err != nil {
		return "", fmt.Errorf("agent: create bin dir %q: %w", binDir, err)
	}
	binPath, err := extractCrushBinary(got, binDir)
	if err != nil {
		return "", err
	}
	return binPath, nil
}

// extractCrushBinary reads the verified gzip-tar bytes and writes ONLY the `crush`
// binary entry to binDir/crush (0755). Every tar entry path is confined by
// assertInsideBinDir (rejecting `..`/absolute escapes, T-26-08) and only a regular
// file whose base name is `crush` is placed; all other entries are skipped. It uses
// stdlib archive/tar + compress/gzip, NEVER a shell `tar`.
func extractCrushBinary(tarball []byte, binDir string) (string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return "", fmt.Errorf("agent: open Crush tarball gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("agent: read Crush tarball entry: %w", err)
		}

		// Confine EVERY entry name before acting on it — reject `..`/absolute escapes
		// even for entries we ultimately skip (defense-in-depth, T-26-08).
		cleanName := filepath.Clean(hdr.Name)
		target := filepath.Join(binDir, cleanName)
		if err := assertInsideBinDir(target, binDir); err != nil {
			return "", err
		}

		// Place ONLY the regular-file crush binary entry; skip everything else.
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(cleanName) != crushBinaryName {
			continue
		}

		binPath := filepath.Join(binDir, crushBinaryName)
		if err := writeBinary(binPath, tr); err != nil {
			return "", err
		}
		return binPath, nil
	}
	return "", fmt.Errorf("agent: Crush tarball did not contain a %q binary entry", crushBinaryName)
}

// writeBinary streams the tar entry body into binPath (0755, truncating any prior
// install). It bounds the copy with the same care as the download seam: the body is
// the already-verified tarball's content, so an unbounded io.Copy is safe here.
func writeBinary(binPath string, r io.Reader) error {
	f, err := os.OpenFile(binPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, binaryFileMode) //nolint:gosec // binPath confined to binDir above
	if err != nil {
		return fmt.Errorf("agent: open %q for write: %w", binPath, err)
	}
	if _, err := io.Copy(f, r); err != nil { //nolint:gosec // body is the verified tarball content
		_ = f.Close()
		_ = os.Remove(binPath)
		return fmt.Errorf("agent: write Crush binary: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(binPath)
		return fmt.Errorf("agent: close Crush binary: %w", err)
	}
	// Ensure the executable bit survives any restrictive umask.
	if err := os.Chmod(binPath, binaryFileMode); err != nil {
		return fmt.Errorf("agent: chmod Crush binary: %w", err)
	}
	return nil
}

// assertInsideBinDir verifies path resolves within dir, rejecting traversal escapes
// (T-26-08). Cloned verbatim from internal/download.assertInsideDir so the install
// seam has no dependency on an unexported downloader helper; both are cleaned and
// compared as absolute paths.
func assertInsideBinDir(path, dir string) error {
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
		return fmt.Errorf("agent: refusing to extract %q outside the villa bin dir %q (path traversal)", absPath, absDir)
	}
	return nil
}

// sha256Hex returns the lowercase hex SHA-256 of b — a small helper the tests use to
// construct a matching pinned asset for a synthetic tarball. Kept here (not test-only)
// so the package owns its checksum-encoding convention in one place.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
