package catalogadd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// Outcome is what a merge did.
type Outcome int

const (
	// Added means the entry was appended.
	Added Outcome = iota
	// AlreadyPresent means an entry with this id and this source is already in the
	// catalog; it is left exactly as the operator has it.
	AlreadyPresent
)

// Refusals a caller matches with errors.Is.
var (
	// ErrIDTaken means the id is already used by an entry from a different source.
	ErrIDTaken = errors.New("the id is already used by a different model")
	// ErrShaMismatch means the same source is already catalogued under a different
	// sha256: the bytes changed under a pinned commit, or the entry was edited.
	ErrShaMismatch = errors.New("the model is already catalogued with a different sha256")
	// ErrFilenameTaken means another entry already writes a different file to the
	// same name; the models dir is flat, so two sources cannot share one.
	ErrFilenameTaken = errors.New("another catalog entry already uses that file name for a different file")
)

// firstShard is an entry's primary shard, or the zero Shard for an entry with none.
func firstShard(m catalog.Model) catalog.Shard {
	if len(m.Shards) == 0 {
		return catalog.Shard{}
	}
	return m.Shards[0]
}

// conflict applies the re-add rules (ADR-0041) for an entry of this id, source and
// file name against a catalog. sha is empty before the download, when only the
// source is known; a known sha is compared too.
func conflict(base catalog.Catalog, id, url, filename, sha string) (Outcome, error) {
	if existing, ok := base.FindByID(id); ok {
		got := firstShard(existing)
		switch {
		case got.URL != url:
			return 0, fmt.Errorf("%w: %q is %s; pass --id to add this one beside it", ErrIDTaken, id, orNone(got.URL))
		case sha != "" && !strings.EqualFold(got.SHA256, sha):
			return 0, fmt.Errorf("%w: %q has %s, the file just pulled is %s", ErrShaMismatch, id, got.SHA256, sha)
		default:
			return AlreadyPresent, nil
		}
	}
	for _, m := range base.Models {
		for _, s := range m.AllShards() {
			if s.Filename == filename && (s.URL != url || (sha != "" && !strings.EqualFold(s.SHA256, sha))) {
				return 0, fmt.Errorf("%w: %q is written by entry %q from %s", ErrFilenameTaken, filename, m.ID, orNone(s.URL))
			}
		}
	}
	return Added, nil
}

func orNone(url string) string {
	if url == "" {
		return "an entry with no source"
	}
	return url
}

// Precheck decides from the source alone, before any download, whether adding id
// from src would be a no-op or a refusal, so a refusal costs no bandwidth.
func Precheck(base catalog.Catalog, id string, src Source) (Outcome, error) {
	return conflict(base, id, src.URL, src.Filename, "")
}

// Merge returns base with m appended, or base unchanged when the same source is
// already catalogued. It never modifies, reorders or removes another entry, and it
// validates the result with the checks catalog.Load runs, so a catalog Merge returns
// is one Load accepts.
func Merge(base catalog.Catalog, m catalog.Model) (catalog.Catalog, Outcome, error) {
	s := firstShard(m)
	outcome, err := conflict(base, m.ID, s.URL, s.Filename, s.SHA256)
	if err != nil || outcome == AlreadyPresent {
		return base, outcome, err
	}
	merged := base
	merged.Models = append(slices.Clone(base.Models), m)
	if err := catalog.Validate(merged); err != nil {
		return base, 0, fmt.Errorf("the merged catalog would be rejected by catalog.Load: %w", err)
	}
	return merged, Added, nil
}
