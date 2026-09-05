package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// renderFixtureTable renders the fixture recommendation with f applied, as the
// default human table.
func renderFixtureTable(t *testing.T, f func(*recommend.Recommendation)) string {
	t.Helper()
	rec := fixtureRecommendation()
	f(&rec)
	var buf bytes.Buffer
	if err := renderRecommend(&buf, rec, nil, false /*table*/, false); err != nil {
		t.Fatalf("renderRecommend: %v", err)
	}
	return buf.String()
}

// TestRecommendTableShowsProjectorTerm asserts a reserved projector is a visible
// row of the fit inequality and that the verdict says vision is on. Its memory
// cost is the user's to see, not an invisible subtraction from the envelope.
func TestRecommendTableShowsProjectorTerm(t *testing.T) {
	out := renderFixtureTable(t, func(r *recommend.Recommendation) {
		r.ProjectorBytes = 1185583104
		r.Vision = true
		r.TotalBytes += 1185583104
	})
	if !strings.Contains(out, "vision projector") {
		t.Errorf("table should carry the projector fit row, got:\n%s", out)
	}
	if !strings.Contains(out, "Vision: yes") {
		t.Errorf("table should report vision on, got:\n%s", out)
	}
}

// TestRecommendTableSaysVisionNoWhenDropped asserts a dropped projector is
// reported as a NO rather than by silence: a text-only stack must never be
// presented as vision-capable.
func TestRecommendTableSaysVisionNoWhenDropped(t *testing.T) {
	out := renderFixtureTable(t, func(r *recommend.Recommendation) {
		r.Notes = []string{"vision: projector (1.10 GiB) dropped — 63.00 GiB needed vs 62.00 GiB usable; this pick runs text-only"}
	})
	if !strings.Contains(out, "Vision: no") {
		t.Errorf("a dropped projector must report Vision: no, got:\n%s", out)
	}
	if strings.Contains(out, "+ vision projector") {
		t.Errorf("a dropped projector must not render a fit row, got:\n%s", out)
	}
}

// TestRecommendTableOmitsVisionForTextOnly asserts an entry that never had a
// projector renders exactly as it does today — no vision line at all.
func TestRecommendTableOmitsVisionForTextOnly(t *testing.T) {
	out := renderFixtureTable(t, func(*recommend.Recommendation) {})
	if strings.Contains(out, "Vision:") {
		t.Errorf("a text-only pick should say nothing about vision, got:\n%s", out)
	}
}

// TestSaveRecommendationPersistsVision asserts `--save` writes the resolved
// decision, so the render funnel reads back the vision the recommendation showed.
func TestSaveRecommendationPersistsVision(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := fixtureRecommendation()
	rec.Vision = true
	rec.ProjectorBytes = 1185583104
	var buf bytes.Buffer
	if err := saveRecommendation(&buf, rec, ""); err != nil {
		t.Fatalf("saveRecommendation: %v", err)
	}
	cfg, err := config.LoadVilla()
	if err != nil {
		t.Fatalf("LoadVilla: %v", err)
	}
	if !cfg.Vision {
		t.Errorf("persisted vision = false, want true")
	}
}
