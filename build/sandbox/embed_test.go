package sandbox_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	sandboxctx "github.com/MatrixMagician/VillaStraylight/build/sandbox"
)

// TestEmbeddedContextMatchesDisk guards the promise that the image `villa
// sandbox build` produces is the image this directory describes. The embed is a
// compile-time copy, so a script edited without a rebuild, or an embed pattern
// that quietly stopped matching a file, would ship a build context that differs
// from the one a reviewer read.
func TestEmbeddedContextMatchesDisk(t *testing.T) {
	want := map[string]bool{
		"Containerfile":          true,
		"requirements.txt":       true,
		"scripts/villa-recalc":   true,
		"scripts/villa-render":   true,
		"scripts/villa-readback": true,
	}

	seen := map[string]bool{}
	err := fs.WalkDir(sandboxctx.Context, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		seen[path] = true
		embedded, readErr := fs.ReadFile(sandboxctx.Context, path)
		if readErr != nil {
			return readErr
		}
		onDisk, readErr := os.ReadFile(filepath.FromSlash(path))
		if readErr != nil {
			return readErr
		}
		if string(embedded) != string(onDisk) {
			t.Errorf("%s: the embedded copy differs from the file on disk", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the embedded context: %v", err)
	}

	for path := range want {
		if !seen[path] {
			t.Errorf("%s is missing from the embedded build context", path)
		}
	}
	for path := range seen {
		if !want[path] {
			t.Errorf("%s is embedded but is not part of the build context", path)
		}
	}
}
