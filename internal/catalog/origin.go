// origin.go answers "who vouches for this entry?" (ADR-0041): the embedded seed
// carries the entries this release vetted, and an external catalog may add to, edit
// or drop them.
//
// INVARIANT: origin is DERIVED from the embedded seed, never stored on the entry.
// A stored flag would be a schema bump that makes every existing external catalog
// fall back to the seed, and a claim an editor can drop. The derived label also
// marks entries written by hand before `villa catalog add` existed.
package catalog

import (
	"reflect"
	"sync"
)

// Origin is an entry's relation to the embedded seed.
type Origin int

const (
	// OriginSeed means the entry equals the embedded seed's entry of that id.
	OriginSeed Origin = iota
	// OriginOperator means the id is not in the embedded seed: the operator
	// supplied the entry, and nothing in this release vetted it.
	OriginOperator
	// OriginEdited means the id is in the seed but the content differs: a hand
	// edit, or a stale copy after the seed re-pinned the entry.
	OriginEdited
)

// Label is the word every surface shows. The seed's is empty on purpose, so a seed
// entry adds nothing to any --json output or table.
func (o Origin) Label() string {
	switch o {
	case OriginOperator:
		return "operator-supplied"
	case OriginEdited:
		return "operator-edited"
	default:
		return ""
	}
}

var (
	seedOnce sync.Once
	seedByID map[string]Model
	seedIDs  []string
)

// seedIndex decodes the embedded seed once. A seed that fails to decode is a build
// error Load already reports, so here it indexes as empty.
func seedIndex() (map[string]Model, []string) {
	seedOnce.Do(func() {
		seedByID = map[string]Model{}
		c, err := decodeSeed()
		if err != nil {
			return
		}
		for _, m := range c.Models {
			seedByID[m.ID] = m
			seedIDs = append(seedIDs, m.ID)
		}
	})
	return seedByID, seedIDs
}

// OriginOf classifies m against the embedded seed.
func OriginOf(m Model) Origin {
	byID, _ := seedIndex()
	s, ok := byID[m.ID]
	switch {
	case !ok:
		return OriginOperator
	case reflect.DeepEqual(s, m):
		return OriginSeed
	default:
		return OriginEdited
	}
}

// MissingFromSeed lists, in seed order, the seed ids c does not carry. An external
// catalog replaces the seed (Load never merges), so after an upgrade that adds seed
// entries this is what an operator's file is missing; it is the CAT-03 finding.
func MissingFromSeed(c Catalog) []string {
	_, ids := seedIndex()
	have := make(map[string]bool, len(c.Models))
	for _, m := range c.Models {
		have[m.ID] = true
	}
	var missing []string
	for _, id := range ids {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

// Seed returns a fresh copy of the embedded seed catalog, the base a new external
// catalog file starts from (ADR-0041: an external file replaces the seed, so it
// must carry every entry the host still needs).
func Seed() (Catalog, error) {
	return decodeSeed()
}
