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

// seedJSON is the embedded default catalog. It is a single file, not a
// tree, so we embed it directly rather than wrapping an fs.FS.
//
//go:embed seed.json
var seedJSON []byte

// maxCatalogBytes bounds how much of an external catalog file we will read, to
// defend against a maliciously huge file (DoS, / Security V5). The seed
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
// for these cases and never panics. An error is only returned in the
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
		} else if verr := validateNgramEntries(ext); verr != nil {
			warnings = append(warnings, fmt.Sprintf("catalog: external catalog %q rejected (%v) — using embedded seed", externalPath, verr))
			// fall through to embedded seed below
		} else if verr := validateSidecars(ext); verr != nil {
			warnings = append(warnings, fmt.Sprintf("catalog: external catalog %q rejected (%v) — using embedded seed", externalPath, verr))
			// fall through to embedded seed below
		} else if verr := validateCoderEntries(ext); verr != nil {
			warnings = append(warnings, fmt.Sprintf("catalog: external catalog %q rejected (%v) — using embedded seed", externalPath, verr))
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

// validateCoderEntries is the input-validation pass for role:"coder" entries on
// the external-catalog trust boundary (ASVS V5). A coder entry with a
// non-positive agent_ctx, a non-positive KV-cache dimension, or an out-of-range
// sampling value invalidates the WHOLE external catalog: the caller refuses it
// with a warning naming the offending entry and falls back to the embedded seed.
// Out-of-range values are NEVER silently coerced into range, and the catalog is
// never partially accepted (refuse whole, fail-closed). The embedded
// seed is exempt — it is compiled in and guarded by its own tests, not by
// runtime validation.
func validateCoderEntries(c Catalog) error {
	for _, m := range c.Models {
		if m.Role != "coder" {
			continue
		}
		if m.AgentCtx <= 0 {
			return fmt.Errorf("coder entry %q: agent_ctx %d out of range (must be > 0)", m.ID, m.AgentCtx)
		}
		// The KV-cache sizing integers (n_layers / n_kv_heads / head_dim /
		// kv_bytes_per_elem) are the terms that actually size KV memory in the
		// coder fit math (internal/recommend/kv.go). A zeroed/omitted dimension
		// collapses the KV term to 0 and optimistically over-qualifies the entry
		// a negative int decodes via uint64 into a huge value that
		// saturates the product to MaxUint64. The single <= 0 guard
		// catches both — refuse whole and name the offending values, never
		// silently accept.
		if m.NLayers <= 0 || m.NKVHeads <= 0 || m.HeadDim <= 0 || m.KVBytesPerElem <= 0 {
			return fmt.Errorf("coder entry %q: missing/invalid KV dimension (n_layers=%d n_kv_heads=%d head_dim=%d kv_bytes_per_elem=%d — all must be > 0)",
				m.ID, m.NLayers, m.NKVHeads, m.HeadDim, m.KVBytesPerElem)
		}
		if s := m.AgentSampling; s != nil {
			switch {
			// temperature == 0 is greedy/deterministic decoding (llama.cpp treats
			// temp <= 0 as greedy) — a legitimate coder preset. Only
			// reject negative or > 2; the inclusive lower bound is [0, 2].
			case s.Temperature < 0 || s.Temperature > 2:
				return fmt.Errorf("coder entry %q: agent_sampling temperature %g out of range [0, 2]", m.ID, s.Temperature)
			case s.TopP <= 0 || s.TopP > 1:
				return fmt.Errorf("coder entry %q: agent_sampling top_p %g out of range (0, 1]", m.ID, s.TopP)
			case s.TopK < 0:
				return fmt.Errorf("coder entry %q: agent_sampling top_k %d out of range (must be >= 0)", m.ID, s.TopK)
			case s.RepeatPenalty <= 0 || s.RepeatPenalty > 3:
				return fmt.Errorf("coder entry %q: agent_sampling repeat_penalty %g out of range (0, 3]", m.ID, s.RepeatPenalty)
			}
		}
	}
	return nil
}

// validateNgramEntries is the fail-closed qualification check on the same trust
// boundary: ngram_safe is a claim about a measurement, so an entry that declares
// it without naming one invalidates the WHOLE external catalog. Refusing beats
// accepting a qualification nobody took, because the qualification is what decides
// whether villa will render a speculation flag at all.
func validateNgramEntries(c Catalog) error {
	for _, m := range c.Models {
		if m.NgramSafe && m.NgramProvenance == "" {
			return fmt.Errorf("entry %q: ngram_safe is set with no ngram_provenance (the measurement that licensed it)", m.ID)
		}
	}
	return nil
}

// validateSidecars is the fail-closed guard on the same trust boundary for a
// declared sidecar. Each of the three refusals is a promise villa could not keep:
// no shards means nothing to pull, a zero weight_bytes means the fit reserves
// nothing for a projector the server will still allocate for, and an empty
// provenance means nobody exercised it on this hardware.
func validateSidecars(c Catalog) error {
	for _, m := range c.Models {
		p := m.Projector
		if p == nil {
			continue
		}
		switch {
		case len(p.Shards) == 0:
			return fmt.Errorf("entry %q: projector declares no shards to download", m.ID)
		case p.WeightBytes == 0:
			return fmt.Errorf("entry %q: projector weight_bytes is 0 (the fit would reserve nothing for it)", m.ID)
		case p.Provenance == "":
			return fmt.Errorf("entry %q: projector has no provenance (the on-hardware exercise that licensed it)", m.ID)
		}
	}
	return nil
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
