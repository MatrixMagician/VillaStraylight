package orchestrate

import (
	"strings"
	"testing"
)

// TestOpenWebUIImageAccessor asserts the exported OpenWebUIImage() accessor
// returns the same digest-pinned value as the unexported managed-service const,
// so the Phase-16 backup manifest can source the OWUI digest through the seam
// without re-typing the literal (D-10).
func TestOpenWebUIImageAccessor(t *testing.T) {
	got := OpenWebUIImage()
	if got != openWebUIImage {
		t.Fatalf("OpenWebUIImage() = %q, want %q", got, openWebUIImage)
	}
	// Sanity: it is a digest-pinned image (the accessor is the manifest's source).
	if !strings.Contains(got, "@sha256:") {
		t.Errorf("OpenWebUIImage() %q is not digest-pinned", got)
	}
}
