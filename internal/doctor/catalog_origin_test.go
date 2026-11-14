package doctor

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// TestCatalogOriginNilSeamEmitsNothing: a doctor with no provenance seam (the embedded
// seed in use, or a catalog it could not load) emits no CAT-02 or CAT-03 finding, so
// a default host's report is what it was before ADR-0041.
func TestCatalogOriginNilSeamEmitsNothing(t *testing.T) {
	r := newDoctorDeps().aggregate()
	if hasFinding(r, "CAT-02") || hasFinding(r, "CAT-03") {
		t.Error("a nil CatalogOrigin seam emitted a provenance finding")
	}
}

// TestCatalogOriginPassMarksTheModelWithoutRaisingOverall: CAT-02 is a PASS finding,
// folded through findingFromCheck like every other check. Supplying a model is not a
// fault, so it must not move the verdict.
func TestCatalogOriginPassMarksTheModelWithoutRaisingOverall(t *testing.T) {
	base := rocmDoctorDeps().aggregate()
	d := rocmDoctorDeps()
	d.CatalogOrigin = func() []preflight.CheckResult {
		return []preflight.CheckResult{{ID: "CAT-02", Name: "catalog origin: mine", Tier: preflight.TierWarn,
			Status: preflight.StatusPass, Detail: "mine is operator-supplied", Provenance: "test"}}
	}
	r := d.aggregate()
	if !hasFinding(r, "CAT-02") {
		t.Fatal("the CAT-02 finding was not folded")
	}
	if r.Overall != base.Overall {
		t.Fatalf("Overall = %q, was %q: an operator-supplied model is not a fault", r.Overall, base.Overall)
	}
}

// TestCatalogOriginDriftWarnsAndKeepsItsRemediation: CAT-03 is a WARN with the fix
// attached, and it ranks like any other WARN.
func TestCatalogOriginDriftWarnsAndKeepsItsRemediation(t *testing.T) {
	d := rocmDoctorDeps()
	d.CatalogOrigin = func() []preflight.CheckResult {
		return []preflight.CheckResult{{ID: "CAT-03", Name: "catalog seed drift", Tier: preflight.TierWarn,
			Status: preflight.StatusWarn, Detail: "lacks 2 entries", Remediation: "copy them from seed.json", Provenance: "test"}}
	}
	r := d.aggregate()
	found := false
	for _, f := range r.Findings {
		if f.ID == "CAT-03" {
			found = true
			if f.Status != "WARN" || f.Remediation == "" {
				t.Errorf("CAT-03 folded as %+v", f)
			}
		}
	}
	if !found {
		t.Fatal("the CAT-03 finding was not folded")
	}
	if r.Overall == "PASS" {
		t.Fatalf("Overall = %q, a seed-drift WARN must not read as a clean report", r.Overall)
	}
}
