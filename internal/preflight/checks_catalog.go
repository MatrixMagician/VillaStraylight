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

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
)

// idCatalogGeometry is the CAT-01 requirement id every result carries.
const idCatalogGeometry = "CAT-01"

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
	if got != want {
		return fail(idCatalogGeometry, name,
			catalogSide+"; "+headerSide,
			"fix the "+m.ID+" entry in internal/catalog/seed.json (n_layers/n_kv_heads/head_dim) to match the GGUF header, or re-pin the shard",
			provenance, ""), true
	}
	return pass(idCatalogGeometry, name, TierBlock, catalogSide+"; "+headerSide, provenance), true
}

// unevaluableGeometry is the typed-Unknown degradation: villa could not read the
// witness, so it reports uncertainty rather than a mismatch it did not observe.
func unevaluableGeometry(name, provenance, detail string) CheckResult {
	return warn(idCatalogGeometry, name, TierBlock, detail,
		"confirm the file downloaded intact (`villa model pull` re-verifies it); a file villa cannot parse is not evidence the catalog is wrong",
		provenance, "")
}
