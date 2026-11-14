package preflight

import (
	"io"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
)

// seedCatalog is the embedded seed, the catalog every origin case perturbs.
func seedCatalog(t *testing.T) catalog.Catalog {
	t.Helper()
	c, err := catalog.Seed()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestCatalogOriginMarksTheConfiguredModel: CAT-02 is how doctor says "this model is
// the operator's" (ADR-0041). It is a PASS, because supplying a model is not a fault,
// and it names what is and is not vetted about it.
func TestCatalogOriginMarksTheConfiguredModel(t *testing.T) {
	c := seedCatalog(t)
	mine := c.Models[0]
	mine.ID = "my-model"
	c.Models = append(c.Models, mine)

	got := RunCatalogOrigin(c, "my-model", "/cfg/catalog-local.json")
	if len(got) != 1 {
		t.Fatalf("results = %+v, want one CAT-02", got)
	}
	r := got[0]
	if r.ID != "CAT-02" || r.Status != StatusPass || !strings.Contains(r.Name, "my-model") {
		t.Fatalf("result = %+v", r)
	}
	for _, want := range []string{"operator-supplied", "not in this release's embedded seed", "CAT-01"} {
		if !strings.Contains(r.Detail, want) {
			t.Errorf("detail lacks %q: %s", want, r.Detail)
		}
	}
}

// TestCatalogOriginDistinguishesAnEditedSeedEntry: the same id with different content
// is `operator-edited`, which is how a stale copy after a re-pin shows up.
func TestCatalogOriginDistinguishesAnEditedSeedEntry(t *testing.T) {
	c := seedCatalog(t)
	c.Models[0].DefaultCtx++
	got := RunCatalogOrigin(c, c.Models[0].ID, "/cfg/c.json")
	if len(got) != 1 || !strings.Contains(got[0].Detail, "operator-edited") {
		t.Fatalf("results = %+v", got)
	}
}

// TestCatalogOriginSaysNothingAboutASeedModel: a configured model that is the seed's
// own entry gets no finding, so a host on the embedded seed or on a faithful copy of
// it keeps the doctor report it had.
func TestCatalogOriginSaysNothingAboutASeedModel(t *testing.T) {
	c := seedCatalog(t)
	if got := RunCatalogOrigin(c, c.Models[0].ID, "/cfg/c.json"); len(got) != 0 {
		t.Fatalf("a seed model produced %+v", got)
	}
	if got := RunCatalogOrigin(c, "not-in-the-catalog", "/cfg/c.json"); len(got) != 0 {
		t.Fatalf("an unresolvable model is another finding's business, got %+v", got)
	}
	if got := RunCatalogOrigin(c, "", "/cfg/c.json"); len(got) != 0 {
		t.Fatalf("no configured model produced %+v", got)
	}
}

// TestCatalogOriginWarnsOnSeedDrift is CAT-03: an external catalog replaces the seed,
// so one that lacks entries this release ships is missing them, and the WARN names
// them and the two ways out. An addition is not drift.
func TestCatalogOriginWarnsOnSeedDrift(t *testing.T) {
	c := seedCatalog(t)
	lost := []string{c.Models[0].ID, c.Models[1].ID}
	c.Models = c.Models[2:]

	got := RunCatalogOrigin(c, "", "/cfg/catalog-local.json")
	if len(got) != 1 || got[0].ID != "CAT-03" || got[0].Status != StatusWarn {
		t.Fatalf("results = %+v, want one CAT-03 WARN", got)
	}
	for _, want := range append(lost, "/cfg/catalog-local.json", "catalog_path=", "seed.json") {
		if !strings.Contains(got[0].Detail+got[0].Remediation, want) {
			t.Errorf("finding lacks %q: %+v", want, got[0])
		}
	}

	full := seedCatalog(t)
	full.Models = append(full.Models, catalog.Model{ID: "extra"})
	if got := RunCatalogOrigin(full, "", "/cfg/c.json"); len(got) != 0 {
		t.Fatalf("an addition read as drift: %+v", got)
	}
}

// TestCatalogOriginOnTheEmbeddedSeedIsSilent: with no external path the seed is in use
// and there is nothing to compare it to.
func TestCatalogOriginOnTheEmbeddedSeedIsSilent(t *testing.T) {
	c := seedCatalog(t)
	c.Models = c.Models[2:]
	if got := RunCatalogOrigin(c, c.Models[0].ID, ""); len(got) != 0 {
		t.Fatalf("results = %+v", got)
	}
}

// TestCatalogGeometryRemediationNamesTheOperatorsFile: CAT-01's fix for a seed entry is
// seed.json, which is wrong advice for an entry the operator supplied.
func TestCatalogGeometryRemediationNamesTheOperatorsFile(t *testing.T) {
	open := func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(qwenHeader()))), nil
	}
	mismatch := func(id string) catalog.Model {
		m := matchingModel()
		m.ID, m.NLayers = id, 48
		return m
	}

	// The seed's first entry is 6/2/256 and qwenHeader says 10/2/256: an unmodified seed
	// entry that disagrees with the file, so its origin is the seed's.
	seedGot := RunCatalogGeometry(catalog.Catalog{Models: seedCatalog(t).Models[:1]}, open)
	if len(seedGot) != 1 || !strings.Contains(seedGot[0].Remediation, "seed.json") {
		t.Fatalf("a seed entry's remediation should still name seed.json: %+v", seedGot)
	}

	got := RunCatalogGeometry(catalog.Catalog{Models: []catalog.Model{mismatch("my-model")}}, open)
	if len(got) != 1 || got[0].Status != StatusFail {
		t.Fatalf("results = %+v", got)
	}
	if strings.Contains(got[0].Remediation, "seed.json") || !strings.Contains(got[0].Remediation, "catalog file") {
		t.Fatalf("an operator entry's remediation must name its catalog file, got %q", got[0].Remediation)
	}
}
