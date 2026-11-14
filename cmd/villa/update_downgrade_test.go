package main

import (
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/manifest"
	"github.com/MatrixMagician/VillaStraylight/internal/manifestverify"
	"github.com/MatrixMagician/VillaStraylight/internal/pinresolve"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/updatecheck"
)

// TestADowngradeOfferIsNeverATarget guards #435: a manifest behind the binary's
// pin yields no work for `villa update`, so the older release is never installed.
func TestADowngradeOfferIsNeverATarget(t *testing.T) {
	doc := manifest.FromTable(11, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	for i := range doc.Components {
		if doc.Components[i].ID == string(pins.Crush) {
			doc.Components[i].Ref = "v0.0.1"
			doc.Components[i].Version = "v0.0.1"
		}
	}
	report := updatecheck.Check(updatecheck.Input{
		Cfg:      config.VillaConfig{AgentEnabled: true},
		Resolver: pinresolve.New(pinstate.State{}),
		Verdict:  manifestverify.Verdict{Outcome: manifestverify.Accepted, Doc: doc},
	})
	if got := targetsFor(report, nil); len(got) != 0 {
		t.Errorf("a downgrade offer produced targets: %v", got)
	}
}
