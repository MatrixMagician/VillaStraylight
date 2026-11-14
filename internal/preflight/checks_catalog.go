// checks_catalog.go is the CAT-01 gate: does the vetted catalog entry still
// describe the file on disk?
//
// The catalog hand-carries the fit dimensions that drive the KV-cache inequality,
// and nothing else in villa reads them back off the weights. A typo, or a re-quant
// published under the same name with a different geometry, produced a confident
// and wrong fit that no gate could see.
//
// INVARIANT: the catalog is the truth and the header is its witness. This check
// never rewrites an entry and never feeds a header value into the fit — a
// disagreement is a FINDING for the operator to resolve. Opening the file is an
// injected seam, so this package still touches no filesystem of its own.
package preflight

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
)

// idCatalogGeometry is the CAT-01 requirement id every result carries.
const idCatalogGeometry = "CAT-01"

// The requirement ids of the two catalog-provenance findings (ADR-0041).
const (
	idCatalogOrigin = "CAT-02"
	idCatalogDrift  = "CAT-03"
)

// RunCatalogGeometry cross-checks every catalog entry that is BOTH on this host
// and carries declared fit dimensions against the GGUF header of its primary
// shard, returning one CheckResult per checked entry in catalog order.
//
// open is the filesystem seam: it resolves a bare filename to a reader, and the
// caller is what confines the path. Two absences are deliberately distinct. A file
// that is not here (fs.ErrNotExist) yields NO result, because a WARN for every
// catalog entry the operator never pulled would bury the ones that matter; any
// other open failure IS surfaced, as an unevaluable WARN.
func RunCatalogGeometry(cat catalog.Catalog, open func(filename string) (io.ReadCloser, error)) []CheckResult {
	var out []CheckResult
	for _, m := range cat.Models {
		want := m.Geometry()
		if want == (gguf.Geometry{}) {
			continue
		}
		if r, checked := checkOneModelGeometry(m, want, open); checked {
			out = append(out, r)
		}
	}
	return out
}

// checkOneModelGeometry is RunCatalogGeometry for a single entry. The bool is
// false when the model is simply not on this host, which is not a finding.
func checkOneModelGeometry(m catalog.Model, want gguf.Geometry, open func(string) (io.ReadCloser, error)) (CheckResult, bool) {
	file := m.PrimaryFile()
	name := "catalog geometry: " + m.ID
	provenance := "gguf header " + file

	rc, err := open(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return CheckResult{}, false
		}
		return unevaluableGeometry(name, provenance, fmt.Sprintf("cannot open %s: %v", file, err)), true
	}
	defer rc.Close()

	h, err := gguf.ReadHeader(rc)
	if err != nil {
		return unevaluableGeometry(name, provenance, fmt.Sprintf("cannot read the GGUF header of %s: %v", file, err)), true
	}
	provenance = fmt.Sprintf("gguf header %s (general.architecture=%s)", file, h.Arch())

	got, err := h.Geometry()
	if err != nil {
		return unevaluableGeometry(name, provenance, fmt.Sprintf("cannot read the geometry of %s: %v", file, err)), true
	}

	catalogSide := fmt.Sprintf("catalog n_layers=%d n_kv_heads=%d head_dim=%d", m.NLayers, m.NKVHeads, m.HeadDim)
	headerSide := fmt.Sprintf("%s header kv_layers=%d head_count_kv=%d key_length=%d", file, got.KVLayers, got.HeadCountKV, got.KeyLength)
	if want.HasSlidingWindow() || got.HasSlidingWindow() {
		catalogSide += fmt.Sprintf(" swa_layers=%d swa_kv_heads=%d swa_head_dim=%d swa_window=%d",
			want.SWALayers, want.SWAHeadCountKV, want.SWAKeyLength, want.SWAWindow)
		headerSide += fmt.Sprintf(" swa_layers=%d swa_head_count_kv=%d swa_key_length=%d swa_window=%d",
			got.SWALayers, got.SWAHeadCountKV, got.SWAKeyLength, got.SWAWindow)
	}
	if got != want {
		return fail(idCatalogGeometry, name,
			catalogSide+"; "+headerSide,
			"fix the "+m.ID+" entry in "+whereEntryLives(m)+" (n_layers/n_kv_heads/head_dim and the swa block) to match the GGUF header, or re-pin the shard",
			provenance, ""), true
	}
	return pass(idCatalogGeometry, name, TierBlock, catalogSide+"; "+headerSide, provenance), true
}

// whereEntryLives names the file an entry is fixed in: the seed for an entry that
// is the seed's own, and the operator's catalog file for any other, because telling
// an operator to edit internal/catalog/seed.json is advice about a file they do not own.
func whereEntryLives(m catalog.Model) string {
	if catalog.OriginOf(m) == catalog.OriginSeed {
		return "internal/catalog/seed.json"
	}
	return "your catalog file (catalog_path)"
}

// unevaluableGeometry is the typed-Unknown degradation: villa could not read the
// witness, so it reports uncertainty rather than a mismatch it did not observe.
func unevaluableGeometry(name, provenance, detail string) CheckResult {
	return warn(idCatalogGeometry, name, TierBlock, detail,
		"confirm the file downloaded intact (`villa model pull` re-verifies it); a file villa cannot parse is not evidence the catalog is wrong",
		provenance, "")
}

// RunCatalogOrigin reports on the provenance of an external catalog (ADR-0041), and
// says nothing at all when externalPath is empty: the embedded seed is in use and
// there is nothing to compare it to.
//
// CAT-02 is a PASS naming the configured model when it is not the seed's own entry:
// supplying a model is not a fault, but the operator should see on every doctor run
// that nothing in this release vetted it. CAT-03 is a WARN when the catalog lacks
// entries the embedded seed ships. An external catalog REPLACES the seed and is never
// merged, so after an upgrade that adds seed entries the operator's file is missing
// them and no other surface says so. An addition is not drift.
func RunCatalogOrigin(cat catalog.Catalog, model, externalPath string) []CheckResult {
	if externalPath == "" {
		return nil
	}
	var out []CheckResult
	if m, ok := cat.FindByID(model); ok && model != "" {
		if o := catalog.OriginOf(m); o != catalog.OriginSeed {
			out = append(out, pass(idCatalogOrigin, "catalog origin: "+m.ID, TierWarn, originDetail(m.ID, o),
				"catalog.OriginOf against the embedded seed ("+externalPath+")"))
		}
	}
	if missing := catalog.MissingFromSeed(cat); len(missing) > 0 {
		out = append(out, warn(idCatalogDrift, "catalog seed drift", TierWarn,
			fmt.Sprintf("%s lacks %d entr%s this release's seed ships: %s", externalPath, len(missing), pluralY(len(missing)), strings.Join(missing, ", ")),
			"copy the missing entries from this release's internal/catalog/seed.json into "+externalPath+", or run `villa config set catalog_path=` to use the embedded seed (an external catalog replaces the seed and is never merged)",
			"catalog.MissingFromSeed", strings.Join(missing, ",")))
	}
	return out
}

func originDetail(id string, o catalog.Origin) string {
	switch o {
	case catalog.OriginEdited:
		return id + " is operator-edited: it differs from the embedded seed's entry of the same id (a hand edit, or a stale copy after the seed re-pinned it); CAT-01 re-checks its geometry against the file"
	default:
		return id + " is operator-supplied: it is not in this release's embedded seed, so nothing in villa vetted it; its fit dimensions came from its GGUF header when it was added, CAT-01 re-checks them against the file, and its speculation and vision qualifications are whatever the entry says"
	}
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
