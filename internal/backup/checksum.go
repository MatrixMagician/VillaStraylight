package backup

// checksum.go is the pure per-entry SHA-256 integrity primitive for the backup
// manifest (D-09) and the restore verify gate (D-08). SHA-256 is used for
// INTEGRITY, not secrecy — encryption-at-rest is deferred (RESEARCH §Security).
// No host I/O: it operates over an io.Reader so the caller owns the file/byte
// handle (the seam).

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// ErrChecksumMismatch is the sentinel a failed verify wraps so callers can
// classify archive corruption as a fail-closed BLOCK (D-08) rather than a
// generic error.
var ErrChecksumMismatch = errors.New("backup: checksum mismatch")

// sum returns the lowercase-hex SHA-256 of everything read from r. It is the
// single hashing primitive used both when recording per-entry checksums into the
// manifest and when verifying them on restore, so the two sides cannot diverge
// (RESEARCH §Code Examples).
func sum(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("backup: sha256: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verify computes the SHA-256 of r and reports a typed mismatch (wrapping
// ErrChecksumMismatch with the want/got context) when it does not equal want.
// A mismatch is archive corruption — the caller MUST treat it as a fail-closed
// BLOCK with zero side effects (D-08).
func verify(r io.Reader, want string) error {
	got, err := sum(r)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%w: want %s, got %s", ErrChecksumMismatch, want, got)
	}
	return nil
}
