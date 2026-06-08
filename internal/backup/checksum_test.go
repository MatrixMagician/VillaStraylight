package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// TestChecksumSumDeterministic asserts sum returns the lowercase-hex SHA-256 of
// the input and is deterministic for the same bytes (D-09).
func TestChecksumSumDeterministic(t *testing.T) {
	payload := []byte("villa backup entry bytes")
	want := hex.EncodeToString(func() []byte { h := sha256.Sum256(payload); return h[:] }())

	got1, err := sum(strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	got2, err := sum(strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("sum (2nd): %v", err)
	}
	if got1 != want {
		t.Fatalf("sum = %s, want %s", got1, want)
	}
	if got1 != got2 {
		t.Fatalf("sum not deterministic: %s vs %s", got1, got2)
	}
	if got1 != strings.ToLower(got1) {
		t.Fatalf("sum is not lowercase hex: %s", got1)
	}
}

// TestChecksumVerifyMatch asserts verify accepts a reader whose hash equals the
// recorded one (the restore happy path, D-08).
func TestChecksumVerifyMatch(t *testing.T) {
	payload := "round-trips deterministically"
	want, err := sum(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if err := verify(strings.NewReader(payload), want); err != nil {
		t.Fatalf("verify on matching content errored: %v", err)
	}
}

// TestChecksumVerifyMismatch asserts verify reports a typed ErrChecksumMismatch
// when the content does not match the recorded checksum — the fail-closed BLOCK
// signal on archive corruption (D-08).
func TestChecksumVerifyMismatch(t *testing.T) {
	err := verify(strings.NewReader("tampered bytes"), "0000")
	if err == nil {
		t.Fatalf("verify accepted mismatched content")
	}
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("verify error = %v, want ErrChecksumMismatch", err)
	}
}
