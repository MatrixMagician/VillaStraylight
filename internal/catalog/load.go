package catalog

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// seedJSON is the embedded default catalog (D-09). It is a single file, not a
// tree, so we embed it directly rather than wrapping an fs.FS.
//
//go:embed seed.json
var seedJSON []byte

// maxCatalogBytes bounds how much of an external catalog file we will read, to
// defend against a maliciously huge file (DoS, T-02-01 / Security V5). The seed
// catalog is a few KB; 1 MiB is a generous ceiling for a hand-curated model list.
const maxCatalogBytes = 1 << 20 // 1 MiB

// Load returns the catalog to use plus any non-fatal warnings.
//
// When externalPath is empty, the embedded seed is decoded and returned. When
// externalPath is set, the file is validated (path-traversal guard, V12),
// read with a bounded reader (V5), decoded, and its schema_version is checked
// against SupportedSchema. On ANY problem with the external file — bad path,
// unreadable, malformed JSON, or a schema_version mismatch — Load appends a clear
// warning string and FALLS BACK to the embedded seed; it never returns an error
// for these cases and never panics (D-11/D-13). An error is only returned in the
// (should-not-happen) event that the embedded seed itself fails to decode.
func Load(externalPath string) (Catalog, []string, error) {
	var warnings []string

	if externalPath != "" {
		ext, err := loadExternal(externalPath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("catalog: external catalog %q unusable (%v) — using embedded seed", externalPath, err))
			// fall through to embedded seed below
		} else if ext.SchemaVersion != SupportedSchema {
			warnings = append(warnings, schemaMismatchWarning(externalPath, ext.SchemaVersion))
			// fall through to embedded seed below
		} else {
			return ext, warnings, nil
		}
	}

	seed, err := decodeSeed()
	if err != nil {
		// The embedded seed is compiled in and tested; a failure here is a
		// build/programming error, not a runtime data problem.
		return Catalog{}, warnings, fmt.Errorf("catalog: embedded seed failed to decode: %w", err)
	}
	return seed, warnings, nil
}

// schemaMismatchWarning produces a clear, direction-aware warning for a
// schema_version that this binary does not support.
func schemaMismatchWarning(path string, got int) string {
	switch {
	case got > SupportedSchema:
		return fmt.Sprintf("catalog: external catalog %q has schema_version %d, newer than this binary supports (%d) — using embedded seed", path, got, SupportedSchema)
	default:
		return fmt.Sprintf("catalog: external catalog %q has schema_version %d, older than this binary supports (%d) — using embedded seed", path, got, SupportedSchema)
	}
}

// loadExternal cleans and validates the external path, reads it with a bounded
// reader, and decodes it. It does NOT check the schema_version (the caller does)
// so that a schema mismatch is reported distinctly from a parse error.
func loadExternal(path string) (Catalog, error) {
	clean, err := validateExternalPath(path)
	if err != nil {
		return Catalog{}, err
	}

	f, err := os.Open(clean) //nolint:gosec // path validated by validateExternalPath
	if err != nil {
		return Catalog{}, err
	}
	defer f.Close()

	// Bound the read so a maliciously huge file cannot exhaust memory (V5).
	lr := io.LimitReader(f, maxCatalogBytes+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return Catalog{}, err
	}
	if int64(len(data)) > maxCatalogBytes {
		return Catalog{}, fmt.Errorf("catalog file exceeds %d byte limit", maxCatalogBytes)
	}

	var c Catalog
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields() // reject unexpected keys defensively
	if err := dec.Decode(&c); err != nil {
		return Catalog{}, fmt.Errorf("decode catalog: %w", err)
	}
	return c, nil
}

// validateExternalPath rejects empty and traversal-prone paths and returns a
// cleaned absolute path (V12 path-traversal guard). The external catalog is an
// explicitly user-supplied diagnostic input, so any existing readable file is
// permitted; we only guard against unexpanded/relative traversal surprises by
// resolving to an absolute, cleaned path.
func validateExternalPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("empty catalog path")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("catalog path %q is a symlink (refused)", path)
	}
	if info.IsDir() {
		return "", fmt.Errorf("catalog path %q is a directory", path)
	}
	return abs, nil
}

// decodeSeed decodes the embedded seed catalog.
func decodeSeed() (Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(seedJSON, &c); err != nil {
		return Catalog{}, err
	}
	return c, nil
}
